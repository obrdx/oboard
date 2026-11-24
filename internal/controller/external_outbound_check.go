package controller

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/security"
	"github.com/OboardProject/oboard/internal/store"
)

var errControllerProbeUnavailable = errors.New("controller probe executable unavailable")

type externalOutboundCheck struct {
	ExternalOutboundID int64     `json:"external_outbound_id"`
	Status             string    `json:"status"`
	ServerID           int64     `json:"server_id,omitempty"`
	ServerName         string    `json:"server_name,omitempty"`
	Target             string    `json:"target"`
	ExitIP             string    `json:"exit_ip,omitempty"`
	RegionCode         string    `json:"region_code,omitempty"`
	RegionStatus       string    `json:"region_status,omitempty"`
	ConnectMS          float64   `json:"connect_ms,omitempty"`
	Error              string    `json:"error,omitempty"`
	CheckedAt          time.Time `json:"checked_at"`
}

type externalOutboundCheckRequest struct {
	ExternalOutboundID int64 `json:"external_outbound_id,omitempty"`
}

func (s *Server) checkExternalOutboundHTTP(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		method(w)
		return
	}
	var req externalOutboundCheckRequest
	if r.ContentLength != 0 {
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			fail(w, errors.New("invalid node check request"), http.StatusBadRequest)
			return
		}
	}
	external, err := s.store.GetExternalOutbound(r.Context(), id)
	if err != nil {
		fail(w, err, http.StatusNotFound)
		return
	}
	result, status, err := s.checkExternalOutbound(r.Context(), *external, nil)
	if err != nil {
		fail(w, err, status)
		return
	}
	auditReq(s, r, "check", "external-outbound", strconv.FormatInt(id, 10))
	write(w, http.StatusOK, map[string]any{"check": result})
}

// checkExternalOutbound probes a third-party node from the Controller first.
// When that fails, the probe is repeated by the Agent of an online server
// (the node's own server for a server-scoped node): many proxies only accept
// the servers' addresses, so a Controller failure alone is not conclusive.
// allow restricts which servers may run the fallback; nil allows all.
func (s *Server) checkExternalOutbound(ctx context.Context, external model.ExternalOutbound, allow func(int64) bool) (externalOutboundCheck, int, error) {
	result := externalOutboundCheck{ExternalOutboundID: external.ID, Target: net.JoinHostPort(strings.Trim(external.TargetAddress, "[]"), strconv.Itoa(external.TargetPort)), CheckedAt: time.Now().UTC(), Status: "failed"}
	if !s.externalCheckMu.TryLock() {
		return result, http.StatusConflict, errors.New("主控正在检测第三方节点，请稍后再试")
	}
	defer s.externalCheckMu.Unlock()
	if externalOutboundServerLocal(external.TargetAddress) {
		// A loopback or private proxy lives next to each server (a local client
		// such as 127.0.0.1:1080). Probing it here would test the Controller's
		// own network, so the check is skipped and any earlier Controller result
		// is dropped; the exit region then comes from the deployed path probe.
		if err := s.store.ClearExternalOutboundProbe(ctx, external.ID); err != nil {
			return result, 500, err
		}
		result.Status, result.RegionStatus, result.Error = "skipped", "", externalOutboundServerLocalMessage
		return result, http.StatusOK, nil
	}
	outbound, err := core.ExternalOutboundProbeConfig(external)
	if err != nil {
		return result, http.StatusBadRequest, err
	}
	fingerprint := core.ExternalOutboundFingerprint(external)
	if err := s.store.BeginControllerExternalOutboundProbe(ctx, external.ID, fingerprint); err != nil {
		return result, 500, err
	}
	started := time.Now()
	run := s.externalCheckRunner
	if run == nil {
		run = s.runControllerOutboundProbe
	}
	exitIP, probeErr := run(ctx, outbound)
	latest, err := s.store.GetExternalOutbound(ctx, external.ID)
	if err != nil || core.ExternalOutboundFingerprint(*latest) != fingerprint {
		return result, http.StatusConflict, errors.New("检测期间节点已删除或配置已修改，请重新检测")
	}
	result.Status, result.RegionStatus, result.Error = "unreachable", "failed", "主控未能通过代理获取出口 IP，请检查节点配置和主控网络"
	if errors.Is(probeErr, errControllerProbeUnavailable) {
		result.Status, result.Error = "failed", "主控检测程序不可用，请更新主控后重试"
	}
	if probeErr == nil {
		if ip, err := parsePublicEgressIP(exitIP); err == nil {
			result.ExitIP, result.Status, result.RegionStatus, result.Error = ip, "reachable", "incomplete", "出口 IP 已确认，地区未识别"
			if s.geoIP != nil {
				geo, err := s.geoIP.Lookup(ip)
				code := core.NormalizeRegionCode(geo.CountryCode)
				if err == nil && code != "" && code != "AQ" {
					result.RegionCode, result.RegionStatus, result.Error = code, "resolved", ""
				}
			}
		}
	}
	result.CheckedAt, result.ConnectMS = time.Now().UTC(), float64(time.Since(started).Milliseconds())
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := s.store.CompleteExternalOutboundProbe(saveCtx, external.ID, fingerprint, 0, result.RegionStatus, result.ExitIP, result.RegionCode, result.Error); err != nil {
		return result, 500, err
	}
	if result.Status == "reachable" {
		return result, http.StatusOK, nil
	}
	controllerError := result.Error
	agentResult, err := s.checkExternalOutboundViaAgent(ctx, external, fingerprint, outbound, allow)
	if err != nil {
		result.Error = controllerError + "；" + err.Error()
		return result, http.StatusOK, nil
	}
	return agentResult, http.StatusOK, nil
}

const externalOutboundCheckWait = 30 * time.Second

// checkExternalOutboundViaAgent runs the signed standalone probe on the first
// ready server and waits for its callback. The error explains why no server
// could run it; a probe that ran and failed is reported in the result.
func (s *Server) checkExternalOutboundViaAgent(ctx context.Context, external model.ExternalOutbound, fingerprint string, outbound json.RawMessage, allow func(int64) bool) (externalOutboundCheck, error) {
	result := externalOutboundCheck{ExternalOutboundID: external.ID, Target: net.JoinHostPort(strings.Trim(external.TargetAddress, "[]"), strconv.Itoa(external.TargetPort)), CheckedAt: time.Now().UTC(), Status: "unreachable", RegionStatus: "failed"}
	servers, err := s.externalOutboundCheckServers(ctx, external, allow)
	if err != nil {
		return result, err
	}
	plan := model.ExternalEgressProbePlan{TimeoutMS: 10000, Standalone: &model.ExternalOutboundProbe{ExternalOutboundID: external.ID, Fingerprint: fingerprint, Outbound: outbound}}
	raw, _ := json.Marshal(plan)
	var task model.AgentTask
	var server model.Server
	busy := false
	for _, candidate := range servers {
		nonce, err := security.RandomToken(12)
		if err != nil {
			return result, err
		}
		task = model.AgentTask{ServerID: candidate.ID, Type: model.AgentTaskTypeProbeExternalEgress, PayloadJSON: string(raw), Status: "pending", ResultJSON: "{}", Nonce: nonce}
		if err := s.store.CreateExternalOutboundCheckTask(ctx, candidate, &task); err != nil {
			if errors.Is(err, store.ErrNetworkDiagnosticBusy) {
				busy = true
				continue
			}
			return result, err
		}
		server = candidate
		break
	}
	if task.ID == 0 {
		if busy {
			return result, errors.New("可用服务器正在执行其他网络诊断，请稍后重试")
		}
		return result, errors.New("没有可用于检测的在线服务器")
	}
	result.ServerID, result.ServerName = server.ID, server.Name
	if s.tasks != nil {
		s.tasks.wake(server.ID)
	}
	started := time.Now()
	waitCtx, cancel := context.WithTimeout(ctx, externalOutboundCheckWait)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := s.store.GetTask(waitCtx, task.ID)
		if err == nil && (current.Status == "succeeded" || current.Status == "failed" || current.Status == "superseded" || current.Status == "rollback_failed") {
			latest, err := s.store.GetExternalOutbound(ctx, external.ID)
			if err != nil || core.ExternalOutboundFingerprint(*latest) != fingerprint {
				result.Status, result.Error = "failed", "检测期间节点配置已修改，请重新检测"
				return result, nil
			}
			if latest.ProbeTaskID == task.ID && latest.ProbeStatus != "pending" {
				result.ExitIP, result.RegionCode, result.RegionStatus, result.Error = latest.ProbeExitIP, latest.ProbeRegionCode, latest.ProbeStatus, latest.ProbeError
				if latest.ProbeExitIP != "" && current.Status == "succeeded" {
					result.Status = "reachable"
				} else {
					result.Error = fmt.Sprintf("主控和服务器 %s 均未能通过代理获取出口 IP，请检查节点地址、凭证和 TLS 设置", server.Name)
				}
				result.ConnectMS = float64(time.Since(started).Milliseconds())
				result.CheckedAt = time.Now().UTC()
				return result, nil
			}
		}
		select {
		case <-waitCtx.Done():
			result.Error = fmt.Sprintf("服务器 %s 未在 %d 秒内返回检测结果", server.Name, int(externalOutboundCheckWait/time.Second))
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer stop()
			_ = s.store.SupersedePendingTask(cleanup, task.ID, "节点检测已超时")
			_ = s.store.CompleteExternalOutboundProbe(cleanup, external.ID, fingerprint, task.ID, "failed", "", "", result.Error)
			result.CheckedAt = time.Now().UTC()
			return result, nil
		case <-ticker.C:
		}
	}
}

// externalOutboundCheckServers lists the online servers whose Agent and kernel
// support the standalone probe, ordered by ID.
func (s *Server) externalOutboundCheckServers(ctx context.Context, external model.ExternalOutbound, allow func(int64) bool) ([]model.Server, error) {
	servers, err := s.store.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(servers, func(i, j int) bool { return servers[i].ID < servers[j].ID })
	var ready []model.Server
	var firstErr error
	for _, server := range servers {
		if external.ServerID != nil && *external.ServerID != 0 && *external.ServerID != server.ID || allow != nil && !allow(server.ID) {
			continue
		}
		if err := externalOutboundCheckServerReady(server, external.Protocol); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ready = append(ready, server)
	}
	if len(ready) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, errors.New("没有可用于检测的授权服务器")
	}
	return ready, nil
}

func externalOutboundCheckServerReady(server model.Server, protocol model.Protocol) error {
	if reason := agentTaskImmediateFailure(&server); reason != "" {
		return fmt.Errorf("服务器 %s：%s", server.Name, reason)
	}
	if !serverSupportsCapability(server, "external_outbound_probe_v1") || !serverSupportsCapability(server, "external_outbound_check_v1") {
		return fmt.Errorf("服务器 %s 的 Agent/内核不支持独立代理出口检测，请先更新", server.Name)
	}
	if protocol == model.ProtocolWireGuard && !serverSupportsCapability(server, core.KernelCapabilityExternalWireGuardProbe) {
		return fmt.Errorf("服务器 %s 的内核不支持 WireGuard 节点检测，请先更新", server.Name)
	}
	return nil
}

const externalOutboundServerLocalMessage = "本机或内网地址只能从服务器访问，主控不检测；加入代理路径并部署后由路径出口探测确定地区"

// externalOutboundServerLocal reports whether a third-party node address is
// only meaningful from the servers themselves: localhost names and loopback,
// private, link-local, shared (CGNAT) or unspecified IP literals.
func externalOutboundServerLocal(address string) bool {
	host := strings.ToLower(strings.TrimSuffix(strings.Trim(strings.TrimSpace(address), "[]"), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if zone := strings.IndexByte(host, '%'); zone >= 0 {
		host = host[:zone]
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() || netip.MustParsePrefix("100.64.0.0/10").Contains(addr)
}

func (s *Server) runControllerOutboundProbe(ctx context.Context, outbound json.RawMessage) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	binary, err := filepath.Abs(filepath.Join(downloadsDir(s.staticDir), "oboard-sb-"+runtime.GOOS+"-"+runtime.GOARCH))
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, binary, "-check-outbound")
	cmd.Stdin = bytes.NewReader(outbound)
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	output := &boundedProbeOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return "", errControllerProbeUnavailable
		}
		return "", errors.New("controller proxy probe failed")
	}
	var result struct {
		ExitIP string `json:"exit_ip"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		return "", errors.New("invalid proxy probe output")
	}
	return parsePublicEgressIP(result.ExitIP)
}

type boundedProbeOutput struct{ bytes.Buffer }

func (b *boundedProbeOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1024 {
		return 0, errors.New("proxy probe output too large")
	}
	return b.Buffer.Write(p)
}

func (s *Server) applyStandaloneExternalProbe(ctx context.Context, task model.AgentTask, probe model.ExternalOutboundProbe, status string, result model.ExternalEgressProbeResult) error {
	if probe.ExternalOutboundID <= 0 || len(probe.Fingerprint) != 64 || len(result.Items) > 1 || len(result.Items) == 1 && result.Items[0].ProbeID != probe.Fingerprint {
		return errors.New("invalid standalone probe result identity")
	}
	current, err := s.store.GetExternalOutbound(ctx, probe.ExternalOutboundID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if core.ExternalOutboundFingerprint(*current) != probe.Fingerprint {
		return nil
	}
	probeStatus, exitIP, region, message := "failed", "", "", "代理连接或出口查询失败，请检查地址、凭证及 TLS 设置"
	if len(result.Items) == 1 && result.Items[0].ProbeID == probe.Fingerprint && result.Items[0].Status == "succeeded" && status == "succeeded" {
		if ip, err := parsePublicEgressIP(result.Items[0].ExitIP); err == nil {
			exitIP = ip
			message = "出口 IP 已确认，地区未识别"
			if s.geoIP != nil {
				geo, err := s.geoIP.Lookup(ip)
				if err == nil {
					region = core.NormalizeRegionCode(geo.CountryCode)
				}
			}
			if region != "" && region != "AQ" {
				probeStatus, message = "resolved", ""
			} else {
				region = ""
				probeStatus = "incomplete"
			}
		}
	}
	return s.store.CompleteExternalOutboundProbe(ctx, probe.ExternalOutboundID, probe.Fingerprint, task.ID, probeStatus, exitIP, region, message)
}

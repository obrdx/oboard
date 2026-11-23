package controller

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
)

var errControllerProbeUnavailable = errors.New("controller probe executable unavailable")

type externalOutboundCheck struct {
	ExternalOutboundID int64     `json:"external_outbound_id"`
	Status             string    `json:"status"`
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
	result, status, err := s.checkExternalOutbound(r.Context(), *external)
	if err != nil {
		fail(w, err, status)
		return
	}
	auditReq(s, r, "check", "external-outbound", strconv.FormatInt(id, 10))
	write(w, http.StatusOK, map[string]any{"check": result})
}

func (s *Server) checkExternalOutbound(ctx context.Context, external model.ExternalOutbound) (externalOutboundCheck, int, error) {
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
	return result, http.StatusOK, nil
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

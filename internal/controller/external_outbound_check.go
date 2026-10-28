package controller

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/security"
)

const externalOutboundProbeCapability = "external_outbound_probe_v1"

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
	ExternalOutboundID int64  `json:"external_outbound_id,omitempty"`
	ServerID           *int64 `json:"server_id,omitempty"`
}

func (s *Server) checkExternalOutboundHTTP(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		method(w)
		return
	}
	var req externalOutboundCheckRequest
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	external, err := s.store.GetExternalOutbound(r.Context(), id)
	if err != nil {
		fail(w, err, http.StatusNotFound)
		return
	}
	result, status, err := s.checkExternalOutbound(r.Context(), *external, req.ServerID, nil)
	if err != nil {
		fail(w, err, status)
		return
	}
	auditReq(s, r, "check", "external-outbound", strconv.FormatInt(id, 10))
	write(w, http.StatusOK, map[string]any{"check": result})
}

func (s *Server) checkExternalOutbound(ctx context.Context, external model.ExternalOutbound, requested *int64, allow func(int64) bool) (externalOutboundCheck, int, error) {
	result := externalOutboundCheck{ExternalOutboundID: external.ID, Target: net.JoinHostPort(strings.Trim(external.TargetAddress, "[]"), strconv.Itoa(external.TargetPort)), CheckedAt: time.Now().UTC(), Status: "failed"}
	server, err := s.externalOutboundCheckServer(ctx, external, requested, allow)
	if err != nil {
		return result, http.StatusConflict, err
	}
	result.ServerID, result.ServerName = server.ID, server.Name
	outbound, err := core.ExternalOutboundProbeConfig(external)
	if err != nil {
		return result, http.StatusBadRequest, err
	}
	fingerprint := core.ExternalOutboundFingerprint(external)
	plan := model.ExternalEgressProbePlan{TimeoutMS: 10000, Standalone: &model.ExternalOutboundProbe{ExternalOutboundID: external.ID, Fingerprint: fingerprint, Outbound: outbound}}
	raw, _ := json.Marshal(plan)
	nonce, err := security.RandomToken(12)
	if err != nil {
		return result, 500, err
	}
	task := model.AgentTask{ServerID: server.ID, Type: model.AgentTaskTypeProbeExternalEgress, PayloadJSON: string(raw), Status: "pending", ResultJSON: "{}", Nonce: nonce}
	if err := s.store.CreateExternalOutboundCheckTask(ctx, *server, &task); err != nil {
		return result, http.StatusConflict, err
	}
	s.tasks.wake(server.ID)
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	started := time.Now()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := s.store.GetTask(waitCtx, task.ID)
		if err != nil {
			result.Error = "读取检测结果失败"
			break
		}
		if current.Status == "succeeded" || current.Status == "failed" || current.Status == "superseded" || current.Status == "rollback_failed" {
			latest, err := s.store.GetExternalOutbound(ctx, external.ID)
			if err != nil || core.ExternalOutboundFingerprint(*latest) != fingerprint {
				result.Error = "检测期间节点配置已修改，请重新检测"
				break
			}
			if latest.ProbeTaskID != task.ID {
				result.Error = "已有更新的检测，请查看最新结果"
				return result, http.StatusOK, nil
			}
			if latest.ProbeStatus == "pending" { // Callback persistence may follow task completion.
				select {
				case <-waitCtx.Done():
					result.Error = "检测超时"
				case <-ticker.C:
					continue
				}
				break
			}
			result.ExitIP, result.RegionCode, result.RegionStatus, result.Error = latest.ProbeExitIP, latest.ProbeRegionCode, latest.ProbeStatus, latest.ProbeError
			if latest.ProbeExitIP != "" && current.Status == "succeeded" {
				result.Status = "reachable"
			} else {
				result.Status = "unreachable"
			}
			result.ConnectMS = float64(time.Since(started).Milliseconds())
			result.CheckedAt = time.Now().UTC()
			return result, http.StatusOK, nil
		}
		select {
		case <-waitCtx.Done():
			result.Error = "检测超时，服务器未返回代理出口结果"
		case <-ticker.C:
			continue
		}
		break
	}
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer stop()
	_ = s.store.SupersedePendingTask(cleanup, task.ID, "节点检测已结束或超时")
	_ = s.store.CompleteExternalOutboundProbe(cleanup, external.ID, fingerprint, task.ID, "failed", "", "", result.Error)
	return result, http.StatusOK, nil
}

func (s *Server) externalOutboundCheckServer(ctx context.Context, external model.ExternalOutbound, requested *int64, allow func(int64) bool) (*model.Server, error) {
	servers, err := s.store.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(servers, func(i, j int) bool { return servers[i].ID < servers[j].ID })
	var firstErr error
	for i := range servers {
		server := &servers[i]
		if requested != nil && *requested > 0 && server.ID != *requested {
			continue
		}
		if external.ServerID != nil && *external.ServerID != 0 && *external.ServerID != server.ID || allow != nil && !allow(server.ID) {
			continue
		}
		err := externalOutboundCheckServerReady(*server)
		if err == nil {
			return server, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, errors.New("没有可用于检测的授权服务器")
}

func externalOutboundCheckServerReady(server model.Server) error {
	if reason := agentTaskImmediateFailure(&server); reason != "" {
		return fmt.Errorf("服务器 %s：%s", server.Name, reason)
	}
	if !serverSupportsCapability(server, externalOutboundProbeCapability) || !serverSupportsCapability(server, "external_outbound_check_v1") {
		return fmt.Errorf("服务器 %s 的 Agent/内核不支持独立代理出口检测，请先更新", server.Name)
	}
	return nil
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

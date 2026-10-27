package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/security"
	"github.com/OboardProject/oboard/internal/store"
)

const externalOutboundCheckTimeoutMS = 5000

// externalOutboundCheck is the result of one third-party node availability
// check: a native TCP connect from a server that can dial the node. It proves
// the node's port is reachable from that server, not that the proxy
// credentials are accepted; the deployed-branch egress probe covers that.
type externalOutboundCheck struct {
	ExternalOutboundID int64     `json:"external_outbound_id"`
	Status             string    `json:"status"`
	ServerID           int64     `json:"server_id,omitempty"`
	ServerName         string    `json:"server_name,omitempty"`
	Target             string    `json:"target"`
	ResolvedIP         string    `json:"resolved_ip,omitempty"`
	ConnectMS          float64   `json:"connect_ms,omitempty"`
	ErrorClass         string    `json:"error_class,omitempty"`
	Error              string    `json:"error,omitempty"`
	CheckedAt          time.Time `json:"checked_at"`
}

const (
	externalCheckReachable     = "reachable"
	externalCheckUnreachable   = "unreachable"
	externalCheckNotApplicable = "not_applicable"
	externalCheckFailed        = "failed"
)

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

// checkExternalOutbound picks the dialing server and runs the probe. allow
// narrows the candidate servers for callers bound to a resource boundary.
func (s *Server) checkExternalOutbound(ctx context.Context, external model.ExternalOutbound, requested *int64, allow func(int64) bool) (externalOutboundCheck, int, error) {
	result := externalOutboundCheck{ExternalOutboundID: external.ID, Target: net.JoinHostPort(strings.Trim(external.TargetAddress, "[]"), strconv.Itoa(external.TargetPort)), CheckedAt: time.Now().UTC()}
	if reason := externalOutboundCheckNotApplicable(external); reason != "" {
		result.Status, result.Error = externalCheckNotApplicable, reason
		return result, http.StatusOK, nil
	}
	server, err := s.externalOutboundCheckServer(ctx, external, requested, allow)
	if err != nil {
		return result, http.StatusConflict, err
	}
	result.ServerID, result.ServerName = server.ID, server.Name
	operationID, err := security.RandomToken(18)
	if err != nil {
		return result, http.StatusInternalServerError, err
	}
	now := time.Now().UTC()
	payload := model.NetworkTCPProbeTaskPayload{
		NetworkDiagnosticEnvelope: model.NetworkDiagnosticEnvelope{ProtocolVersion: model.NetworkDiagnosticProtocolVersion, OperationID: operationID, Origin: model.NetworkDiagnosticOriginMonitor, IssuedAt: now, ExpiresAt: now.Add(15 * time.Second)},
		Host:                      strings.Trim(external.TargetAddress, "[]"),
		Port:                      external.TargetPort,
		IPFamily:                  model.NetworkFamilyAuto,
		TimeoutMS:                 externalOutboundCheckTimeoutMS,
	}
	raw, err := s.runExternalOutboundCheckTask(ctx, *server, payload)
	if err != nil {
		if errors.Is(err, store.ErrNetworkDiagnosticBusy) {
			return result, http.StatusConflict, err
		}
		result.Status, result.Error = externalCheckFailed, err.Error()
		return result, http.StatusOK, nil
	}
	var probe model.NetworkTCPProbeResult
	if len(raw) > 4*1024 || json.Unmarshal([]byte(raw), &probe) != nil || probe.OperationID != operationID {
		result.Status, result.Error = externalCheckFailed, "检测结果与请求不匹配"
		return result, http.StatusOK, nil
	}
	result.ResolvedIP = probe.ResolvedIP
	if probe.Connected {
		result.Status, result.ConnectMS = externalCheckReachable, probe.ConnectMS
		return result, http.StatusOK, nil
	}
	result.Status, result.ErrorClass, result.Error = externalCheckUnreachable, probe.ErrorClass, externalCheckErrorText(probe.ErrorClass)
	return result, http.StatusOK, nil
}

// externalOutboundCheckNotApplicable names protocols whose data path is UDP,
// where a TCP connect proves nothing.
func externalOutboundCheckNotApplicable(external model.ExternalOutbound) string {
	switch external.Protocol {
	case model.ProtocolHY2:
		return "HY2 使用 UDP，无法用 TCP 连接检测；部署后可用出口探测确认"
	case model.ProtocolMieru:
		var cfg map[string]any
		if json.Unmarshal([]byte(external.ConfigJSON), &cfg) == nil && strings.EqualFold(stringFromMap(cfg, "transport"), "UDP") {
			return "UDP 传输的 Mieru 无法用 TCP 连接检测；部署后可用出口探测确认"
		}
	}
	return ""
}

func externalCheckErrorText(class string) string {
	switch class {
	case "timeout":
		return "连接超时"
	case "refused":
		return "端口拒绝连接"
	case "reset":
		return "连接被重置"
	case "unreachable":
		return "网络不可达"
	case "nxdomain":
		return "域名不存在"
	case "dns_failed":
		return "域名解析失败"
	case "":
		return "连接失败"
	default:
		return "连接失败（" + class + "）"
	}
}

// externalOutboundCheckServer returns the server that will dial the node: the
// requested one, the bound server of a server-scoped node, or the first online
// capable server.
func (s *Server) externalOutboundCheckServer(ctx context.Context, external model.ExternalOutbound, requested *int64, allow func(int64) bool) (*model.Server, error) {
	servers, err := s.store.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(servers, func(i, j int) bool { return servers[i].ID < servers[j].ID })
	usable := func(server model.Server) bool {
		if external.ServerID != nil && *external.ServerID != 0 && *external.ServerID != server.ID {
			return false
		}
		return allow == nil || allow(server.ID)
	}
	if requested != nil && *requested > 0 {
		for i := range servers {
			if servers[i].ID != *requested {
				continue
			}
			if !usable(servers[i]) {
				return nil, errors.New("该第三方节点只能在绑定的服务器上使用，请选择对应服务器检测")
			}
			if err := externalOutboundCheckServerReady(servers[i]); err != nil {
				return nil, err
			}
			return &servers[i], nil
		}
		return nil, errors.New("检测服务器不存在")
	}
	var firstErr error
	for i := range servers {
		if !usable(servers[i]) {
			continue
		}
		if err := externalOutboundCheckServerReady(servers[i]); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		return &servers[i], nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, errors.New("没有可用于检测的服务器")
}

func externalOutboundCheckServerReady(server model.Server) error {
	if reason := agentTaskImmediateFailure(&server); reason != "" {
		return fmt.Errorf("服务器 %s：%s", server.Name, reason)
	}
	if !serverSupportsCapability(server, model.AgentCapabilityNetworkTCPMonitor) {
		return fmt.Errorf("服务器 %s 的 Agent 不支持节点可用性检测；请先更新 Agent", server.Name)
	}
	return nil
}

// runExternalOutboundCheckTask queues the signed probe and waits for its
// result JSON. A task that could not finish is superseded on return.
func (s *Server) runExternalOutboundCheckTask(ctx context.Context, server model.Server, payload model.NetworkTCPProbeTaskPayload) (string, error) {
	raw, _ := json.Marshal(payload)
	nonce, err := security.RandomToken(12)
	if err != nil {
		return "", err
	}
	task := model.AgentTask{ServerID: server.ID, Type: model.AgentTaskTypeNetworkTCP, PayloadJSON: string(raw), Status: "pending", ResultJSON: "{}", Nonce: nonce}
	if err = s.store.CreateExternalOutboundCheckTask(ctx, server, &task); err != nil {
		return "", err
	}
	s.tasks.wake(server.ID)
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer stop()
		_ = s.store.SupersedePendingTask(cleanup, task.ID, "节点检测已结束或超时")
	}()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := s.store.GetTask(waitCtx, task.ID)
		if err != nil {
			return "", errors.New("读取检测结果失败")
		}
		switch current.Status {
		case "succeeded":
			return current.ResultJSON, nil
		case "failed", "superseded", "rollback_failed":
			return "", externalCheckTaskFailure(current.ResultJSON)
		}
		select {
		case <-waitCtx.Done():
			return "", errors.New("检测超时，服务器未在 30 秒内返回结果")
		case <-ticker.C:
		}
	}
}

func externalCheckTaskFailure(raw string) error {
	var failure struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal([]byte(raw), &failure)
	switch failure.Code {
	case model.NetworkDiagnosticCodeTargetNotAllowed:
		return errors.New("节点地址解析到私网、回环或保留地址，可用性检测只支持公网节点")
	case model.NetworkDiagnosticCodeInvalidInput:
		return errors.New("节点地址无法检测：" + failure.Error)
	case model.NetworkDiagnosticCodeExpired:
		return errors.New("服务器忙，检测未能按时开始")
	case "":
		return errors.New("检测失败")
	default:
		return errors.New("检测失败：" + failure.Error)
	}
}

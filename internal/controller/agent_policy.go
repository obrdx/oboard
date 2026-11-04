package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/OboardProject/oboard/internal/application"
	"github.com/OboardProject/oboard/internal/automation"
	"github.com/OboardProject/oboard/internal/model"
)

const agentPolicySetCapability = "servers.agent_policy.set"

type agentPolicySetOperation struct {
	ServerID int64  `json:"server_id"`
	Feature  string `json:"feature"`
	Allow    *bool  `json:"allow"`
}

func decodeAgentPolicySet(input json.RawMessage) (agentPolicySetOperation, error) {
	var request agentPolicySetOperation
	if err := strictAutomationInput(input, &request); err != nil {
		return request, err
	}
	if request.ServerID <= 0 {
		return request, errors.New("positive server_id is required")
	}
	if request.Feature != model.LocalPolicyFeaturePlugins && request.Feature != model.LocalPolicyFeatureLAN {
		return request, errors.New("feature must be plugins or lan")
	}
	if request.Allow == nil {
		return request, errors.New("allow is required")
	}
	return request, nil
}

// agentPolicyView is the shared read shape for REST and MCP.
func (s *Server) agentPolicyView(ctx context.Context, server *model.Server) (map[string]any, error) {
	status, err := s.store.GetServerRemoteAccessStatus(ctx, server.ID)
	if err != nil {
		return nil, err
	}
	mode := status.LocalMode
	if mode == "" {
		mode = model.RemoteAccessModeStandard
	}
	return map[string]any{
		"server_id": server.ID, "local_mode": mode,
		"local_allow": map[string]any{
			"plugins_enabled": status.LocalAllow.PluginsEnabled, "lan_requests_enabled": status.LocalAllow.PluginsEnabled && status.LocalAllow.LANRequests,
			"remote_terminal": status.LocalAllow.RemoteTerminal, "mcp_enabled": status.LocalAllow.MCPEnabled,
		},
		"supported": slices.Contains(server.KernelCapabilities, model.AgentCapabilityLocalPolicyRemote),
	}, nil
}

// checkAgentPolicySet validates that the change can be sent now. The Agent
// enforces the Hardened rule itself; refusing here only avoids a task that
// would fail.
func (s *Server) checkAgentPolicySet(ctx context.Context, principal application.Principal, request agentPolicySetOperation) (*model.Server, error) {
	if principal.Role != model.RoleAdmin {
		return nil, errors.New("forbidden: administrator role is required")
	}
	if !principal.AllowsInt64("server_ids", request.ServerID) {
		return nil, errors.New("authorized server_id is required")
	}
	server, err := s.store.GetServer(ctx, request.ServerID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(server.AgentID) == "" {
		return nil, errors.New("server_not_enrolled: 服务器还没有接入 Agent")
	}
	if !slices.Contains(server.KernelCapabilities, model.AgentCapabilityLocalPolicyRemote) {
		return nil, errors.New("agent_upgrade_required: 该节点的 Agent 不支持主控下发本地开关，请先更新 Agent")
	}
	if *request.Allow {
		status, err := s.store.GetServerRemoteAccessStatus(ctx, server.ID)
		if err != nil {
			return nil, err
		}
		open := status.LocalAllow.PluginsEnabled
		if request.Feature == model.LocalPolicyFeatureLAN {
			open = open && status.LocalAllow.LANRequests
		}
		if status.LocalMode == model.RemoteAccessModeHardened && !open {
			return nil, fmt.Errorf("%s: 节点处于 Hardened 模式，主控只能关闭开关；请在节点执行 obag remote-access allow %s", model.LocalPolicyErrorHardened, request.Feature)
		}
	}
	if !s.agentControlOnline(server.ID) {
		return nil, errors.New("server_offline: 节点离线，无法下发")
	}
	return server, nil
}

func (s *Server) recordAgentPolicyAudit(ctx context.Context, principal application.Principal, server *model.Server, request agentPolicySetOperation, result string, metadata map[string]any) {
	actorType := "machine"
	if principal.Interactive {
		actorType = "user"
	}
	metadata["feature"], metadata["allow"], metadata["principal_id"], metadata["principal_type"] = request.Feature, *request.Allow, principal.ID, string(principal.Type)
	if principal.ClientName != "" {
		metadata["client_name"] = principal.ClientName
	}
	raw, _ := json.Marshal(metadata)
	serverID := server.ID
	sourceIP := ""
	if principal.SourceIP.IsValid() {
		sourceIP = principal.SourceIP.String()
	}
	s.recordRemoteAccessAuditWithContext(ctx, sourceIP, model.RemoteAccessAuditEvent{
		EventType: "agent_local_policy_set", ActorType: actorType, ActorUserID: principal.UserID, OAuthGrantID: principal.GrantID,
		ServerID: &serverID, Capability: agentPolicySetCapability, Result: result, MetadataJSON: raw,
	})
}

func (s *Server) registerAgentPolicyOperation() {
	s.automation.RegisterValidator(agentPolicySetCapability, func(ctx context.Context, principal application.Principal, input json.RawMessage) (any, error) {
		request, err := decodeAgentPolicySet(input)
		if err != nil {
			return nil, err
		}
		server, err := s.checkAgentPolicySet(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		return s.agentPolicyView(ctx, server)
	})
	s.automation.Register(agentPolicySetCapability, func(ctx context.Context, principal application.Principal, input json.RawMessage) (any, error) {
		request, err := decodeAgentPolicySet(input)
		if err != nil {
			return nil, err
		}
		server, err := s.checkAgentPolicySet(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		before, _ := s.agentPolicyView(ctx, server)
		task, err := s.queueAgentTask(ctx, server.ID, model.AgentTaskTypeApplyLocalPolicy, model.AgentLocalPolicyPlan{Feature: request.Feature, Allow: *request.Allow}, 0)
		if err != nil {
			s.recordAgentPolicyAudit(ctx, principal, server, request, "failed", map[string]any{"error": err.Error(), "before": before["local_allow"]})
			return nil, err
		}
		s.recordAgentPolicyAudit(ctx, principal, server, request, "queued", map[string]any{"task_id": task.ID, "before": before["local_allow"], "local_mode": before["local_mode"]})
		view, err := s.agentPolicyView(ctx, server)
		if err != nil {
			return nil, err
		}
		view["feature"], view["allow"], view["task_id"], view["task_status"] = request.Feature, *request.Allow, task.ID, task.Status
		return view, nil
	})
}

// applyAgentLocalPolicyTaskResult stores the gates the node reports enforcing
// so the plugin gateway uses them before the next heartbeat arrives.
func (s *Server) applyAgentLocalPolicyTaskResult(ctx context.Context, server *model.Server, resultJSON string) {
	var result model.AgentLocalPolicyResult
	if json.Unmarshal([]byte(resultJSON), &result) != nil || strings.TrimSpace(result.LocalMode) == "" {
		return
	}
	status, err := s.store.GetServerRemoteAccessStatus(ctx, server.ID)
	if err != nil {
		return
	}
	report := model.RemoteAccessReport{Capabilities: status.Capabilities, LocalMode: result.LocalMode, LocalAllow: result.LocalAllow}
	_ = s.persistRemoteAccessStatus(ctx, server.ID, server.AgentID, report)
}

// apiAgentPolicy serves GET/POST /api/v1/servers/:id/agent-policy.
func (s *Server) apiAgentPolicy(w http.ResponseWriter, r *http.Request, p application.Principal, id int64) {
	if r.Method == http.MethodGet {
		if _, ok := s.capabilities.Authorize(p, "servers.agent_policy.read"); !ok || !p.AllowsInt64("server_ids", id) {
			v2Error(w, r, 403, "capability_denied", "没有节点本地开关的读取权限")
			return
		}
		server, err := s.store.GetServer(r.Context(), id)
		if err != nil {
			v2HandleError(w, r, err)
			return
		}
		view, err := s.agentPolicyView(r.Context(), server)
		if err != nil {
			v2HandleError(w, r, err)
			return
		}
		v2Write(w, r, 200, view, nil)
		return
	}
	if r.Method != http.MethodPost {
		v2Error(w, r, 405, "method_not_allowed", "不支持该请求方法")
		return
	}
	if !p.Interactive {
		v2Error(w, r, 403, "changeset_required", "自动化写入请使用对应能力和 Changeset")
		return
	}
	var input struct {
		Feature   string `json:"feature"`
		Allow     *bool  `json:"allow"`
		RequestID string `json:"request_id"`
	}
	if !decodeV2(w, r, &input) {
		return
	}
	if len(input.RequestID) < 8 || len(input.RequestID) > 80 {
		v2Error(w, r, 400, "request_id_required", "需要请求标识")
		return
	}
	if _, ok := s.capabilities.Authorize(p, agentPolicySetCapability); !ok {
		v2Error(w, r, 403, "capability_denied", "没有节点本地开关的管理权限")
		return
	}
	raw, _ := json.Marshal(agentPolicySetOperation{ServerID: id, Feature: input.Feature, Allow: input.Allow})
	cs, err := s.applyConfirmedChangeset(r.Context(), p, []automation.OperationRequest{{Capability: agentPolicySetCapability, Input: raw, ResourceRefs: json.RawMessage("{}")}}, fmt.Sprintf("agent-policy:%d:%s", id, input.RequestID), "设置节点本地开关")
	if err != nil {
		v2HandleError(w, r, err)
		return
	}
	if cs == nil || cs.Status != model.ChangesetSucceeded {
		v2Error(w, r, 409, "changeset_not_applied", "节点本地开关尚未下发")
		return
	}
	server, err := s.store.GetServer(r.Context(), id)
	if err != nil {
		v2HandleError(w, r, err)
		return
	}
	view, err := s.agentPolicyView(r.Context(), server)
	if err != nil {
		v2HandleError(w, r, err)
		return
	}
	view["updated_at"] = time.Now().UTC()
	v2Write(w, r, 202, view, nil)
}

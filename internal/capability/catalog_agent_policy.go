package capability

import "github.com/OboardProject/oboard/internal/mcpauth"

// agentPolicyDescriptors exposes the Agent-local plugins and lan gates.
// Setting them is an administrator operation the administrator's own OAuth
// grant may perform directly: the signed apply_agent_local_policy task is
// queued immediately and the node applies it. A Hardened node accepts only
// closing a gate; opening one there stays a local `obag` decision.
func agentPolicyDescriptors(id, text, boolean map[string]any) []Descriptor {
	allow := closedObject(map[string]any{"plugins_enabled": boolean, "lan_requests_enabled": boolean, "remote_terminal": boolean, "mcp_enabled": boolean})
	output := schemaObject(map[string]any{
		"server_id": id, "local_mode": text, "local_allow": allow, "supported": boolean,
		"feature": text, "allow": boolean, "task_id": id, "task_status": text,
	})
	read := Descriptor{
		Name: "servers.agent_policy.read", Description: "读取节点本地实际生效的插件与内网请求开关、本地模式，以及是否支持主控下发",
		InputSchema: schemaObject(map[string]any{"server_id": id}, "server_id"), OutputSchema: output,
		RequiredScopes: []string{"servers:read"}, ResourceTypes: []string{"server"}, ResourceEvaluator: "server_ids",
		ReadOnly: true, Idempotent: true, DataClassification: DataInternal, MCPEnabled: true,
		MinimumAccess: mcpauth.AccessRead, RBACPermission: "admin.settings", AdminOnly: true, ResolveResourceRefs: serverRefFromServerID,
	}
	set := Descriptor{
		Name: "servers.agent_policy.set", Description: "开关节点本地的插件（plugins）或内网请求（lan）权限，立即下发签名任务，节点照做；打开 lan 会同时打开 plugins，关闭 plugins 会同时关闭 lan。Hardened 节点只接受关闭，打开需在节点执行 obag remote-access allow（返回 hardened_local_only）。管理员账号及其授权的 MCP 可直接执行，写入审计",
		InputSchema: schemaObject(map[string]any{
			"server_id": id, "feature": map[string]any{"type": "string", "enum": []string{"plugins", "lan"}}, "allow": boolean,
		}, "server_id", "feature", "allow"),
		OutputSchema:   output,
		RequiredScopes: []string{"servers:write"}, ResourceTypes: []string{"server"}, ResourceEvaluator: "server_ids",
		RiskClass: 3, Idempotent: true, DataClassification: DataInternal, MCPEnabled: true, Executable: true,
		MinimumAccess: mcpauth.AccessOperate, RBACPermission: "admin.settings", AdminOnly: true, ResolveResourceRefs: serverRefFromServerID,
	}
	return []Descriptor{read, set}
}

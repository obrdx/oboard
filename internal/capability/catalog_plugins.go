package capability

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/OboardProject/oboard/internal/mcpauth"
)

// pluginDescriptors is the single management contract for plugins across
// Web, REST and MCP. Publishing a draft, granting, revoking, enabling and
// trial-running are administrator operations an administrator account may
// delegate to its own OAuth MCP grant, together with installing, updating,
// switching versions, uninstalling and writing instance secrets: they apply
// immediately, can never grant beyond the manifest, and are recorded in the
// instance audit with the permission diff. Plugin-wide enable, instance
// deletion and runtime settings stay interactive-admin only.
func pluginDescriptors() []Descriptor {
	text := map[string]any{"type": "string"}
	boolean := map[string]any{"type": "boolean"}
	integer := map[string]any{"type": "integer"}
	id := map[string]any{"type": "string", "pattern": "^[1-9][0-9]{0,18}$"}
	jsonValue := map[string]any{"type": []string{"string", "number", "integer", "boolean", "array", "object", "null"}}
	freeObject := map[string]any{"type": "object", "additionalProperties": jsonValue}
	freeArray := map[string]any{"type": "array", "items": jsonValue}
	confirmed := map[string]any{"type": "boolean", "const": true}
	digest := map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"}
	idempotency := map[string]any{"type": "string", "minLength": 1, "maxLength": 128}
	customVar := closedObject(map[string]any{"name": text, "type": text, "value": jsonValue}, "name", "type")
	scheduleInput := map[string]any{
		"kind": map[string]any{"type": "string", "enum": []string{"interval", "cron", "event"}}, "interval": text, "cron": text,
		"timezone": text, "event": map[string]any{"type": "string", "enum": []string{"server.online", "server.offline"}}, "enabled": boolean,
	}
	pluginRef := refResolver("plugin_id", "plugin")
	instanceRef := refResolver("instance_id", "plugin_instance")
	runRef := refResolver("run_id", "plugin_run")
	scheduleRef := refResolver("schedule_id", "plugin_schedule")
	source := map[string]any{"type": "object", "additionalProperties": jsonValue, "description": "{kind:\"upload\",package_base64} 或 {kind:\"github\",repository_url,ref|commit}"}
	read := func(name, description, permission string, input map[string]any, required []string, output map[string]any, refs func(context.Context, any) ([]mcpauth.ResourceRef, error)) Descriptor {
		return Descriptor{Name: name, Description: description, InputSchema: schemaObject(input, required...), OutputSchema: rawSchema(output), RequiredScopes: []string{"plugins:read"}, ResourceTypes: []string{"plugin"}, ReadOnly: true, Idempotent: true, DataClassification: DataInternal, MCPEnabled: true, MinimumAccess: mcpauth.AccessRead, RBACPermission: permission, ResolveResourceRefs: refs}
	}
	write := func(name, description, permission, scope string, risk int, input map[string]any, required []string, output map[string]any, refs func(context.Context, any) ([]mcpauth.ResourceRef, error)) Descriptor {
		return Descriptor{Name: name, Description: description, InputSchema: schemaObject(input, required...), OutputSchema: rawSchema(output), RequiredScopes: []string{scope}, ResourceTypes: []string{"plugin"}, RiskClass: risk, ApprovalPolicy: "required", Idempotent: true, DataClassification: DataInternal, MCPEnabled: true, Executable: true, MinimumAccess: mcpauth.AccessOperate, RBACPermission: permission, ResolveResourceRefs: refs}
	}
	admin := func(name, description, permission, scope string, input map[string]any, required []string, output map[string]any) Descriptor {
		return Descriptor{Name: name, Description: description + "。仅交互式管理员可在面板执行，MCP 不可调用", InputSchema: schemaObject(input, required...), OutputSchema: rawSchema(output), RequiredScopes: []string{scope}, ResourceTypes: []string{"plugin"}, RiskClass: 4, ApprovalPolicy: "required", DataClassification: DataSensitive, MCPEnabled: false, MinimumAccess: mcpauth.AccessOperate, RBACPermission: permission, AdminOnly: true, ResolveResourceRefs: noRefs}
	}
	// delegated operations apply immediately for the administrator and the
	// administrator's own OAuth grant; no approval step is involved.
	delegated := func(name, description, permission, scope string, risk int, input map[string]any, required []string, output map[string]any, refs func(context.Context, any) ([]mcpauth.ResourceRef, error)) Descriptor {
		return Descriptor{Name: name, Description: description + "。管理员账号及其授权的 MCP 可直接执行，立即生效，写入审计", InputSchema: schemaObject(input, required...), OutputSchema: rawSchema(output), RequiredScopes: []string{scope}, ResourceTypes: []string{"plugin"}, RiskClass: risk, Idempotent: true, DataClassification: DataInternal, MCPEnabled: true, Executable: true, MinimumAccess: mcpauth.AccessOperate, RBACPermission: permission, AdminOnly: true, ResolveResourceRefs: refs}
	}
	positive := map[string]any{"type": "integer", "minimum": 1}
	capabilityScope := closedObject(map[string]any{
		"servers": arrayOf(positive), "hosts": arrayOf(text), "channels": arrayOf(positive), "users": arrayOf(positive), "plans": arrayOf(positive), "targets": arrayOf(text),
	})
	grantCapabilities := map[string]any{
		"description": "能力名数组，或 {能力名: {servers, hosts, channels, users, plans, targets}}；只能是清单声明过的能力与目标",
		"oneOf":       []any{arrayOf(text), map[string]any{"type": "object", "additionalProperties": capabilityScope}},
	}
	descriptors := []Descriptor{
		read("plugins.list", "列出已安装插件及各实例状态", "plugins.read", nil, nil, closedObject(map[string]any{"plugins": arrayOf(freeObject)}, "plugins"), noRefs),
		read("plugins.get", "读取插件详情：当前版本清单、权限、版本历史与各实例配置状态；不返回密钥", "plugins.read", map[string]any{"plugin_id": id}, []string{"plugin_id"}, freeObject, pluginRef),
		read("plugins.catalog", "读取插件能力目录、Environment 类型与永久禁止的能力类别", "plugins.read", nil, nil, freeObject, noRefs),
		read("plugins.runtime.status", "读取插件运行环境：安装、Worker、隔离、队列与全局限额；未安装时返回主机安装命令", "plugins.read", nil, nil, freeObject, noRefs),
		read("plugins.servers", "列出可在插件 server 环境变量中选择的服务器（稳定 ID，仅作选择器，不是授权）", "plugins.read", nil, nil, closedObject(map[string]any{"servers": arrayOf(freeObject)}, "servers"), noRefs),
		read("plugin_instances.get", "读取一个插件实例的环境配置、密钥是否已配置、授权范围、触发器与状态", "plugins.read", map[string]any{"instance_id": id}, []string{"instance_id"}, freeObject, instanceRef),
		read("plugin_instances.state", "读取插件实例的私有状态键值与配额占用", "plugins.read", map[string]any{"instance_id": id}, []string{"instance_id"}, closedObject(map[string]any{"entries": freeArray, "usage": freeObject}, "entries", "usage"), instanceRef),
		read("plugin_instances.audit", "读取插件实例的敏感能力调用审计（不含请求/响应内容与密钥）", "plugins.read", map[string]any{"instance_id": id, "before_id": id, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 200}}, []string{"instance_id"}, closedObject(map[string]any{"events": freeArray}, "events"), instanceRef),
		read("plugin_runs.list", "按插件或实例列出执行记录", "plugins.read", map[string]any{"plugin_id": id, "instance_id": id, "before_id": id, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 200}}, nil, closedObject(map[string]any{"runs": freeArray}, "runs"), noRefs),
		read("plugin_runs.get", "读取一次执行的状态、触发来源、错误码与结果", "plugins.read", map[string]any{"run_id": id}, []string{"run_id"}, closedObject(map[string]any{"run": freeObject}, "run"), runRef),
		read("plugin_runs.logs", "读取一次执行的日志（已按密钥脱敏）", "plugins.read", map[string]any{"run_id": id, "after_seq": integer}, []string{"run_id"}, closedObject(map[string]any{"logs": freeArray}, "logs"), runRef),
		write("plugin_instances.create", "为已安装插件新增一个停用、未授权的实例", "plugins.configure", "plugins:configure", 2, map[string]any{"plugin_id": id, "name": text}, []string{"plugin_id", "name"}, freeObject, pluginRef),
		write("plugin_instances.update", "重命名实例或解除自动暂停；启用与停用请用 plugin_instances.enable / plugin_instances.disable", "plugins.configure", "plugins:configure", 2, map[string]any{"instance_id": id, "name": text, "resume": boolean}, []string{"instance_id"}, freeObject, instanceRef),
		write("plugin_instances.environment.update", "按清单 Schema 校验并保存实例环境变量与自定义变量；环境变量只是配置，不扩大任何权限。密钥不能经此写入", "plugins.configure", "plugins:configure", 2, map[string]any{"instance_id": id, "expected_revision": map[string]any{"type": "integer", "minimum": 1}, "values": freeObject, "custom": arrayOf(customVar)}, []string{"instance_id", "expected_revision"}, freeObject, instanceRef),
		write("plugin_instances.run", "手动执行一次实例；需已启用、已授权且配置完整。每次 SDK 调用仍复核调用者权限", "plugins.execute", "plugins:execute", 3, map[string]any{"instance_id": id, "idempotency_key": idempotency}, []string{"instance_id", "idempotency_key"}, closedObject(map[string]any{"run": freeObject}, "run"), instanceRef),
		write("plugin_runs.cancel", "取消排队或执行中的插件运行，撤销租约并终止 Runner", "plugins.execute", "plugins:execute", 2, map[string]any{"run_id": id}, []string{"run_id"}, closedObject(map[string]any{"run": freeObject}, "run"), runRef),
		write("plugin_schedules.create", "为实例新增 interval、cron 或 event 触发器；同一实例并发为 1，重叠时合并跳过", "plugins.configure", "plugins:configure", 3, mergeProperties(map[string]any{"instance_id": id}, scheduleInput), []string{"instance_id", "kind"}, freeObject, instanceRef),
		write("plugin_schedules.update", "修改或启停触发器", "plugins.configure", "plugins:configure", 3, mergeProperties(map[string]any{"schedule_id": id}, scheduleInput), []string{"schedule_id", "kind"}, freeObject, scheduleRef),
		write("plugin_schedules.delete", "删除触发器", "plugins.configure", "plugins:configure", 2, map[string]any{"schedule_id": id}, []string{"schedule_id"}, closedObject(map[string]any{"deleted": boolean}, "deleted"), scheduleRef),
		write("plugin_instances.state.delete", "删除插件实例的一个私有状态键", "plugins.configure", "plugins:configure", 2, map[string]any{"instance_id": id, "key": text}, []string{"instance_id", "key"}, closedObject(map[string]any{"deleted": boolean}, "deleted"), instanceRef),
		write("plugins.drafts.create", "在编辑器中新建本地插件草稿（不可执行，发布需管理员）", "plugins.develop", "plugins:develop", 2, map[string]any{"manifest": freeObject, "source": text}, []string{"manifest", "source"}, freeObject, noRefs),
		write("plugins.drafts.save", "保存本地插件的清单与源码草稿；不执行代码，不影响已发布版本", "plugins.develop", "plugins:develop", 2, map[string]any{"plugin_id": id, "manifest": freeObject, "source": text}, []string{"plugin_id", "manifest", "source"}, closedObject(map[string]any{"saved": boolean}, "saved"), pluginRef),
		{Name: "plugins.drafts.diagnose", Description: "静态检查清单、Environment Schema 与源码语法，并提示调用了未声明能力；从不执行插件代码", InputSchema: schemaObject(map[string]any{"manifest": freeObject, "source": text}, "manifest", "source"), OutputSchema: rawSchema(freeObject), RequiredScopes: []string{"plugins:read"}, ResourceTypes: []string{"plugin"}, ReadOnly: true, Idempotent: true, DataClassification: DataInternal, MCPEnabled: true, MinimumAccess: mcpauth.AccessRead, RBACPermission: "plugins.read", ResolveResourceRefs: noRefs},
		delegated("plugins.packages.preview", "校验插件包或 GitHub 来源并预览发布者、签名、权限与更新差异；不执行代码", "plugins.install", "plugins:install", 4, map[string]any{"source": source}, []string{"source"}, freeObject, noRefs),
		delegated("plugins.packages.install", "按已预览摘要安装或更新插件；新权限不会自动授予，发布者变化会被拒绝", "plugins.install", "plugins:install", 4, map[string]any{"source": source, "expected_sha256": digest, "confirm": confirmed}, []string{"source", "expected_sha256", "confirm"}, freeObject, noRefs),
		delegated("plugins.source.check", "检查 GitHub 来源的插件是否有新版本并预览权限差异；只读取来源，不安装、不执行代码", "plugins.install", "plugins:install", 4, map[string]any{"plugin_id": id}, []string{"plugin_id"}, freeObject, pluginRef),
		delegated("plugins.source.update", "从插件安装时记录的 GitHub 仓库与分支更新到已检查的提交；新权限不会自动授予，来源不能更改", "plugins.install", "plugins:install", 4, map[string]any{"plugin_id": id, "commit": text, "expected_sha256": digest, "confirm": confirmed}, []string{"plugin_id", "commit", "expected_sha256", "confirm"}, freeObject, pluginRef),
		delegated("plugins.versions.activate", "切换到已保存的其他版本，适用同样的权限差异规则", "plugins.install", "plugins:install", 4, map[string]any{"plugin_id": id, "package_id": id, "confirm": confirmed}, []string{"plugin_id", "package_id", "confirm"}, freeObject, pluginRef),
		delegated("plugins.publish", "把本地草稿发布为新的不可变版本（本地 / 未签名）；draft_version 取自 plugins.get 的 draft.version，草稿在此之后被修改则拒绝。新增权限不会自动授予", "plugins.install", "plugins:install", 3, map[string]any{"plugin_id": id, "draft_version": digest}, []string{"plugin_id", "draft_version"}, freeObject, pluginRef),
		delegated("plugins.drafts.test_run", "在已发布并授权的实例上试运行一次本地草稿；沿用已发布版本的授权与配置并只取与草稿声明的交集。标记 side_effect 的内网目标与通知类能力在试运行中被拦截（DRAFT_SIDE_EFFECT_BLOCKED）；结果只记在草稿运行记录中，不计入失败暂停", "plugins.install", "plugins:install", 3, map[string]any{"plugin_id": id, "instance_id": id, "idempotency_key": idempotency}, []string{"plugin_id", "instance_id", "idempotency_key"}, closedObject(map[string]any{"run": freeObject}, "run"), pluginRef),
		delegated("plugin_instances.enable", "启用插件实例", "plugins.install", "plugins:install", 3, map[string]any{"instance_id": id}, []string{"instance_id"}, freeObject, instanceRef),
		delegated("plugin_instances.disable", "停用插件实例；取消排队并终止运行", "plugins.install", "plugins:install", 2, map[string]any{"instance_id": id}, []string{"instance_id"}, freeObject, instanceRef),
		delegated("plugin_instances.grant", "替换实例对当前版本的授权。capabilities 为能力名数组或按能力的范围对象；servers 为所有服务器类能力的默认服务器，lan_targets 为 lan.request 的目标。超出清单声明的能力、主机或目标直接报错；审计记录新增与移除项及上一版授权，可用上一版授权再次调用以撤回", "plugins.authorize", "plugins:authorize", 3, map[string]any{"instance_id": id, "capabilities": grantCapabilities, "servers": arrayOf(positive), "lan_targets": arrayOf(text), "expected_revision": map[string]any{"type": "integer", "minimum": 0}}, []string{"instance_id", "capabilities"}, freeObject, instanceRef),
		delegated("plugin_instances.grant.revoke", "撤销实例全部授权，运行中的插件下一次调用即被拒绝", "plugins.authorize", "plugins:authorize", 2, map[string]any{"instance_id": id}, []string{"instance_id"}, closedObject(map[string]any{"revoked": boolean}, "revoked"), instanceRef),
		admin("plugins.set_enabled", "启用或停用整个插件；停用会取消排队并终止运行", "plugins.install", "plugins:install", map[string]any{"plugin_id": id, "enabled": boolean}, []string{"plugin_id", "enabled"}, freeObject),
		delegated("plugins.uninstall", "卸载插件：删除代码、实例配置、授权、密钥、状态与触发器，取消运行；保留执行历史与审计", "plugins.install", "plugins:install", 4, map[string]any{"plugin_id": id, "confirm": confirmed}, []string{"plugin_id", "confirm"}, closedObject(map[string]any{"uninstalled": boolean}, "uninstalled"), pluginRef),
		admin("plugin_instances.delete", "删除插件实例及其配置、授权、密钥、状态与触发器", "plugins.install", "plugins:install", map[string]any{"instance_id": id, "confirm": confirmed}, []string{"instance_id", "confirm"}, closedObject(map[string]any{"deleted": boolean}, "deleted")),
		delegated("plugin_instances.secrets.update", "写入或清除实例密钥；只写不读，明文永不返回", "plugins.authorize", "plugins:authorize", 4, map[string]any{"instance_id": id, "name": text, "value": text}, []string{"instance_id", "name", "value"}, closedObject(map[string]any{"configured": boolean}, "configured"), instanceRef),
		admin("plugins.runtime.settings.update", "启停插件执行、暂停调度或调整全局限额与保留期；未安装运行环境时不能启用", "plugins.settings", "plugins:settings", map[string]any{"enabled": boolean, "scheduler_paused": boolean, "max_concurrency": map[string]any{"type": "integer", "minimum": 1, "maximum": 8}, "max_timeout_seconds": map[string]any{"type": "integer", "minimum": 5, "maximum": 120}, "log_retention_days": map[string]any{"type": "integer", "minimum": 1, "maximum": 365}, "run_retention_days": map[string]any{"type": "integer", "minimum": 1, "maximum": 365}}, nil, freeObject),
	}
	for i := range descriptors {
		if descriptors[i].Name == "plugin_instances.secrets.update" {
			// The secret value is sealed before any changeset is stored.
			descriptors[i].DataClassification = DataSensitive
			descriptors[i].SensitiveInput = []string{"value"}
			descriptors[i].SealedInput = []string{"value"}
		}
	}
	// Resource types follow the reference each descriptor resolves, so an
	// MCP boundary always knows every plugin resource type it may meet.
	for i := range descriptors {
		switch name := descriptors[i].Name; {
		case strings.HasPrefix(name, "plugin_runs."):
			descriptors[i].ResourceTypes = []string{"plugin_run"}
		case strings.HasPrefix(name, "plugin_schedules."):
			descriptors[i].ResourceTypes = []string{"plugin_schedule", "plugin_instance"}
		case strings.HasPrefix(name, "plugin_instances."):
			descriptors[i].ResourceTypes = []string{"plugin_instance", "plugin"}
		}
	}
	return descriptors
}

func mergeProperties(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range a {
		out[key] = value
	}
	for key, value := range b {
		out[key] = value
	}
	return out
}

func refResolver(field, resourceType string) func(context.Context, any) ([]mcpauth.ResourceRef, error) {
	return func(_ context.Context, input any) ([]mcpauth.ResourceRef, error) {
		values, err := canonicalMap(input)
		if err != nil {
			return nil, err
		}
		raw, ok := values[field].(string)
		if !ok {
			return nil, errors.New(field + " must be a positive integer string")
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 {
			return nil, errors.New("invalid " + field)
		}
		return []mcpauth.ResourceRef{{Type: resourceType, ID: strconv.FormatInt(n, 10)}}, nil
	}
}

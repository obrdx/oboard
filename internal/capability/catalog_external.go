package capability

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/OboardProject/oboard/internal/mcpauth"
)

// externalOutboundDescriptors builds the imported third-party node
// (external outbound) capability set mirroring the panel's 导入节点 tab.

func externalOutboundDescriptors(positiveID map[string]any, stringValue, boolValue map[string]any, nullableString, nullableInteger func() map[string]any) []Descriptor {
	externalOutbound := closedObject(map[string]any{
		"id": positiveID, "revision": stringValue, "server_id": nullableInteger(), "name": stringValue,
		"protocol": stringValue, "scope": stringValue, "target_address": stringValue,
		"target_port": map[string]any{"type": "integer"},
		"region_mode": stringValue, "region_code": stringValue, "effective_region_code": stringValue,
		"region_status": stringValue, "expose_to_users": boolValue, "enabled": boolValue,
		"advanced_configured": boolValue, "created_at": stringValue, "updated_at": stringValue,
	})
	fields := closedObject(map[string]any{
		"name":            map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
		"protocol":        map[string]any{"type": "string", "enum": []string{"vless", "hysteria2", "anytls", "shadowsocks", "mieru", "socks", "http", "wireguard"}},
		"scope":           map[string]any{"type": "string", "enum": []string{"global", "server"}},
		"server_id":       nullableInteger(),
		"target_address":  map[string]any{"type": "string", "maxLength": 255},
		"target_port":     map[string]any{"type": "integer", "minimum": 1, "maximum": 65535},
		"config_json":     map[string]any{"type": "string", "maxLength": 65536},
		"region_mode":     map[string]any{"type": "string", "enum": []string{"auto", "manual"}},
		"region_code":     map[string]any{"type": "string", "maxLength": 2},
		"expose_to_users": boolValue,
		"enabled":         boolValue,
	})
	reads := []Descriptor{
		{Name: "external_outbounds.list", Description: "列出导入节点（第三方出口）及其地域信息", InputSchema: schemaObject(nil), OutputSchema: rawSchema(arrayOf(externalOutbound)), RequiredScopes: []string{"external_outbounds:read"}, ResourceTypes: []string{"server", "external_outbound"}, ResourceEvaluator: "server_ids", ReadOnly: true, Idempotent: true, DataClassification: DataInternal, MCPEnabled: true, MinimumAccess: mcpauth.AccessRead, ResolveResourceRefs: noRefs},
	}
	writes := []struct {
		name, description string
		input, output     json.RawMessage
		risk              int
		destructive       bool
	}{
		{"external_outbounds.import", "从订阅文本、节点 URI（含 socks5h://、socks5://、wireguard:// / wg://）或单个 WireGuard 配置文件（[Interface] + 一个 [Peer]）批量导入第三方节点；SOCKS5 默认启用远端 DNS（SOCKS5H），HTTP/HTTPS 与 WireGuard 节点只能作为代理链路出口，不会下发到订阅", schemaObject(map[string]any{"content": map[string]any{"type": "string", "minLength": 1, "maxLength": 262144}, "scope": map[string]any{"type": "string", "enum": []string{"global", "server"}}, "server_id": nullableInteger(), "expose_to_users": boolValue}, "content"), schemaObject(map[string]any{"external_outbounds": arrayOf(externalOutbound), "created_count": map[string]any{"type": "integer"}}, "created_count"), 2, false},
		{"external_outbounds.create", "手动创建导入节点；SOCKS 节点可通过 config_json.remote_dns 控制解析方式（默认 true 为 SOCKS5H 远端 DNS，设为 false 则在本机解析且需内核具备 socks5_dns_mode_v1 能力）", schemaObject(map[string]any{"external_outbound": fields}, "external_outbound"), schemaObject(map[string]any{"external_outbound": externalOutbound}, "external_outbound"), 2, false},
		{"external_outbounds.update", "修改导入节点；SOCKS 节点可通过 config_json.remote_dns 切换远端 DNS（SOCKS5H，默认）或本地 DNS（需内核 socks5_dns_mode_v1）", schemaObject(map[string]any{"external_outbound_id": positiveID, "changes": fields}, "external_outbound_id", "changes"), schemaObject(map[string]any{"external_outbound": externalOutbound, "changed_fields": stringArray(1, 32)}, "external_outbound"), 2, false},
		{"external_outbounds.check", "通过第三方代理访问固定 HTTPS IP 查询服务，一次验证代理凭证、可用性和真实出口地区；无需创建或部署链路。结果按节点配置保存，已有和新建链路复用地区；修改连接配置后失效。先在主控本机检测；主控失败时由一台在线且支持独立检测的服务器 Agent 复检（服务器级节点只用其所属服务器，MCP 调用只使用授权范围内的服务器），返回 server_id/server_name 表示检测位置。本机或内网地址（如 127.0.0.1）的节点只能从服务器访问，返回 status=skipped 且不保存结果，地区由已部署路径的出口探测确定", schemaObject(map[string]any{"external_outbound_id": positiveID}, "external_outbound_id"), schemaObject(map[string]any{"check": closedObject(map[string]any{"external_outbound_id": positiveID, "status": map[string]any{"type": "string", "enum": []string{"reachable", "unreachable", "failed", "skipped"}}, "server_id": positiveID, "server_name": stringValue, "target": stringValue, "exit_ip": stringValue, "region_code": stringValue, "region_status": stringValue, "connect_ms": map[string]any{"type": "number"}, "error": stringValue, "checked_at": stringValue})}, "check"), 1, false},
		{"external_outbounds.delete", "删除导入节点，并自动从所有订阅套餐移除该节点", schemaObject(map[string]any{"external_outbound_id": positiveID, "confirm": map[string]any{"type": "boolean", "const": true}}, "external_outbound_id", "confirm"), schemaObject(map[string]any{"deleted": boolValue, "external_outbound_id": positiveID}, "deleted"), 3, true},
	}
	for _, write := range writes {
		reads = append(reads, Descriptor{
			Name: write.name, Description: write.description, InputSchema: write.input, OutputSchema: write.output,
			RequiredScopes: []string{trafficScopeFor(write.name)}, ResourceTypes: []string{"server", "external_outbound"},
			ResourceEvaluator: "server_ids", RiskClass: write.risk, ApprovalPolicy: "required", Idempotent: true,
			DataClassification: DataInternal, Destructive: write.destructive, MCPEnabled: true, Executable: true,
			MinimumAccess: mcpauth.AccessOperate, ResolveResourceRefs: externalOutboundResolver(write.name),
		})
	}
	return reads
}

func externalOutboundResolver(name string) func(context.Context, any) ([]mcpauth.ResourceRef, error) {
	return func(_ context.Context, input any) ([]mcpauth.ResourceRef, error) {
		object, err := canonicalMap(input)
		if err != nil {
			return nil, err
		}
		refs := []mcpauth.ResourceRef{}
		addServer := func(nested map[string]any) {
			if id, ok := int64Value(nested["server_id"]); ok && id > 0 {
				refs = append(refs, mcpauth.ResourceRef{Type: "server", ID: strconv.FormatInt(id, 10)})
			}
		}
		switch name {
		case "external_outbounds.import", "external_outbounds.create":
			nested, _ := object["external_outbound"].(map[string]any)
			if name == "external_outbounds.import" {
				nested = object
			}
			addServer(nested)
		case "external_outbounds.check":
			if id, ok := int64Value(object["external_outbound_id"]); ok && id > 0 {
				refs = append(refs, mcpauth.ResourceRef{Type: "external_outbound", ID: strconv.FormatInt(id, 10)})
			}
		case "external_outbounds.update":
			if id, ok := int64Value(object["external_outbound_id"]); ok && id > 0 {
				refs = append(refs, mcpauth.ResourceRef{Type: "external_outbound", ID: strconv.FormatInt(id, 10)})
			}
			changes, _ := object["changes"].(map[string]any)
			addServer(changes)
		case "external_outbounds.delete":
			if id, ok := int64Value(object["external_outbound_id"]); ok && id > 0 {
				refs = append(refs, mcpauth.ResourceRef{Type: "external_outbound", ID: strconv.FormatInt(id, 10)})
			}
		}
		return refs, nil
	}
}

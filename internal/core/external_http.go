package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/OboardProject/oboard/internal/model"
)

// KernelCapabilityHTTPOutbound is advertised by oboard-sb builds that register
// the HTTP CONNECT outbound used by imported HTTP/HTTPS nodes.
const KernelCapabilityHTTPOutbound = "http_outbound_v1"

const (
	httpExternalMaxHeaders     = 16
	httpExternalMaxHeaderBytes = 1024
)

var httpExternalConfigKeys = map[string]bool{
	"type": true, "tag": true, "server": true, "server_port": true,
	"username": true, "password": true, "path": true, "headers": true, "tls": true, "tcp_fast_open": true,
}

var httpExternalTLSKeys = map[string]bool{"enabled": true, "server_name": true, "insecure": true}

// HTTPExternalConfig is the closed field set of an imported HTTP/HTTPS proxy.
type HTTPExternalConfig struct {
	Username     string
	Password     string
	Path         string
	Headers      map[string]string
	TLS          bool
	ServerName   string
	Insecure     bool
	TCPFastOpen  bool
	HasFastOpen  bool
	HasTLSObject bool
}

// ParseHTTPExternalConfig validates an imported HTTP proxy config_json. Only
// the listed fields are accepted so no unknown upstream option reaches the
// kernel.
func ParseHTTPExternalConfig(raw string) (HTTPExternalConfig, error) {
	var out HTTPExternalConfig
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return out, fmt.Errorf("http config_json must be a JSON object: %w", err)
	}
	for key := range cfg {
		if !httpExternalConfigKeys[key] {
			return out, fmt.Errorf("http 节点不支持字段 %q", key)
		}
	}
	if value, ok := cfg["type"]; ok && value != "http" {
		return out, errors.New("http 节点的 type 必须是 http")
	}
	var ok bool
	if out.Username, ok = optionalString(cfg, "username"); !ok {
		return out, errors.New("username 必须是字符串")
	}
	if out.Password, ok = optionalString(cfg, "password"); !ok {
		return out, errors.New("password 必须是字符串")
	}
	if len(out.Username) > 255 || len(out.Password) > 255 {
		return out, errors.New("用户名和密码最长 255 字节")
	}
	if strings.ContainsAny(out.Username, ":\r\n") {
		return out, errors.New("用户名不能包含冒号或换行")
	}
	if strings.ContainsAny(out.Password, "\r\n") {
		return out, errors.New("密码不能包含换行")
	}
	if out.Path, ok = optionalString(cfg, "path"); !ok {
		return out, errors.New("path 必须是字符串")
	}
	if out.Path != "" && (!strings.HasPrefix(out.Path, "/") || len(out.Path) > 255 || strings.ContainsAny(out.Path, " \r\n")) {
		return out, errors.New("path 必须以 / 开头且不含空白，最长 255 字节")
	}
	if rawHeaders, exists := cfg["headers"]; exists && rawHeaders != nil {
		headers, isMap := rawHeaders.(map[string]any)
		if !isMap {
			return out, errors.New("headers 必须是对象")
		}
		if len(headers) > httpExternalMaxHeaders {
			return out, fmt.Errorf("headers 最多 %d 项", httpExternalMaxHeaders)
		}
		out.Headers = make(map[string]string, len(headers))
		for name, value := range headers {
			text, isString := value.(string)
			if !isString || !validHTTPHeaderName(name) || len(name)+len(text) > httpExternalMaxHeaderBytes || strings.ContainsAny(text, "\r\n") {
				return out, fmt.Errorf("header %q 无效", name)
			}
			switch strings.ToLower(name) {
			case "host", "proxy-authorization", "content-length", "transfer-encoding", "connection":
				return out, fmt.Errorf("header %q 由内核生成，不能自定义", name)
			}
			out.Headers[name] = text
		}
	}
	if rawTLS, exists := cfg["tls"]; exists && rawTLS != nil {
		tlsObject, isMap := rawTLS.(map[string]any)
		if !isMap {
			return out, errors.New("tls 必须是对象")
		}
		out.HasTLSObject = true
		for key := range tlsObject {
			if !httpExternalTLSKeys[key] {
				return out, fmt.Errorf("https 节点的 tls 不支持字段 %q", key)
			}
		}
		if value, exists := tlsObject["enabled"]; exists {
			enabled, isBool := value.(bool)
			if !isBool {
				return out, errors.New("tls.enabled 必须是布尔值")
			}
			out.TLS = enabled
		}
		if out.ServerName, ok = optionalString(tlsObject, "server_name"); !ok || len(out.ServerName) > 253 || strings.ContainsAny(out.ServerName, " /\r\n") {
			return out, errors.New("tls.server_name 无效")
		}
		if value, exists := tlsObject["insecure"]; exists {
			insecure, isBool := value.(bool)
			if !isBool {
				return out, errors.New("tls.insecure 必须是布尔值")
			}
			out.Insecure = insecure
		}
		if !out.TLS && (out.ServerName != "" || out.Insecure) {
			return out, errors.New("未启用 TLS 时不能设置 server_name 或 insecure")
		}
	}
	if value, exists := cfg["tcp_fast_open"]; exists {
		enabled, isBool := value.(bool)
		if !isBool {
			return out, errors.New("tcp_fast_open 必须是布尔值")
		}
		out.TCPFastOpen, out.HasFastOpen = enabled, true
	}
	return out, nil
}

// NormalizeHTTPExternalConfigJSON rewrites an imported HTTP proxy config to
// its canonical stored form: the closed field set without type/tag/server.
func NormalizeHTTPExternalConfigJSON(raw string) (string, error) {
	cfg, err := ParseHTTPExternalConfig(raw)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(httpExternalConfigFields(cfg))
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func httpExternalConfigFields(cfg HTTPExternalConfig) map[string]any {
	out := map[string]any{}
	if cfg.Username != "" {
		out["username"] = cfg.Username
	}
	if cfg.Password != "" {
		out["password"] = cfg.Password
	}
	if cfg.Path != "" {
		out["path"] = cfg.Path
	}
	if len(cfg.Headers) > 0 {
		headers := make(map[string]any, len(cfg.Headers))
		for name, value := range cfg.Headers {
			headers[name] = value
		}
		out["headers"] = headers
	}
	if cfg.TLS {
		tls := map[string]any{"enabled": true}
		if cfg.ServerName != "" {
			tls["server_name"] = cfg.ServerName
		}
		if cfg.Insecure {
			tls["insecure"] = true
		}
		out["tls"] = tls
	}
	if cfg.HasFastOpen {
		out["tcp_fast_open"] = cfg.TCPFastOpen
	}
	return out
}

// HTTPExternalUsesTLS reports whether an imported HTTP node is HTTPS.
func HTTPExternalUsesTLS(v model.ExternalOutbound) bool {
	cfg, err := ParseHTTPExternalConfig(v.ConfigJSON)
	return err == nil && cfg.TLS
}

func httpExternalOutboundToSingBox(v model.ExternalOutbound, server model.Server, outboundTag string) (map[string]any, error) {
	if strings.TrimSpace(server.AgentID) != "" && len(server.KernelCapabilities) > 0 && !stringSliceContains(server.KernelCapabilities, KernelCapabilityHTTPOutbound) {
		return nil, fmt.Errorf("服务器 %s 的内核缺少 %s 能力，不能使用 HTTP/HTTPS 第三方节点；请先更新 Agent/内核", server.Name, KernelCapabilityHTTPOutbound)
	}
	cfg, err := ParseHTTPExternalConfig(v.ConfigJSON)
	if err != nil {
		return nil, err
	}
	item := httpExternalConfigFields(cfg)
	item["type"] = "http"
	item["tag"] = outboundTag
	item["server"] = v.TargetAddress
	item["server_port"] = v.TargetPort
	if cfg.TLS && cfg.ServerName == "" {
		if _, ipErr := netip.ParseAddr(strings.Trim(v.TargetAddress, "[]")); ipErr != nil {
			item["tls"].(map[string]any)["server_name"] = v.TargetAddress
		}
	}
	applyServerNetworkPolicy(item, server, model.ProtocolHTTP, false)
	omitUnsupportedDialTCPFastOpen(item)
	return item, nil
}

func optionalString(cfg map[string]any, key string) (string, bool) {
	value, exists := cfg[key]
	if !exists || value == nil {
		return "", true
	}
	text, ok := value.(string)
	return strings.TrimSpace(text), ok
}

func validHTTPHeaderName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if !(r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}

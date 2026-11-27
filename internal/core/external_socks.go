package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/OboardProject/oboard/internal/model"
)

// KernelCapabilitySocks5DNSMode is advertised by oboard-sb builds whose SOCKS
// outbound honors remote_dns=false (local DNS resolution before dialing SOCKS5).
// Default SOCKS5h (remote DNS) omits remote_dns and works on all kernels.
const KernelCapabilitySocks5DNSMode = "socks5_dns_mode_v1"

// SocksExternalConfig holds the validated version and DNS resolution mode of an
// imported SOCKS third-party node.
type SocksExternalConfig struct {
	Version      string
	RemoteDNS    bool
	HasRemoteDNS bool
}

// ParseSocksExternalConfig validates a SOCKS external outbound's config_json.
// Version defaults to "5" and remote_dns defaults to true (SOCKS5h / remote
// DNS) for SOCKS5 and SOCKS4a, and false for SOCKS4.
func ParseSocksExternalConfig(raw string) (SocksExternalConfig, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return SocksExternalConfig{}, fmt.Errorf("socks config_json must be a JSON object: %w", err)
	}
	version := strings.TrimSpace(stringFromAny(cfg["version"]))
	if version == "" {
		version = "5"
	}
	switch version {
	case "4", "4a", "5":
	default:
		return SocksExternalConfig{}, errors.New("socks version must be 4, 4a or 5")
	}
	out := SocksExternalConfig{Version: version, RemoteDNS: version != "4"}
	if rawRemoteDNS, exists := cfg["remote_dns"]; exists && rawRemoteDNS != nil {
		remoteDNS, ok := rawRemoteDNS.(bool)
		if !ok {
			return SocksExternalConfig{}, errors.New("remote_dns 必须是布尔值")
		}
		if version == "4" && remoteDNS {
			return SocksExternalConfig{}, errors.New("SOCKS4 不支持远端 DNS，请改用 SOCKS5（SOCKS5H）或 SOCKS4a")
		}
		out.RemoteDNS = remoteDNS
		out.HasRemoteDNS = true
	}
	if err := validateTransportOptions(model.ProtocolSocks, cfg, transportSideOutbound); err != nil {
		return SocksExternalConfig{}, err
	}
	return out, nil
}

// SocksExternalRemoteDNS reports whether an imported SOCKS node resolves target
// domains on the remote SOCKS proxy (SOCKS5h / SOCKS4a).
func SocksExternalRemoteDNS(v model.ExternalOutbound) bool {
	cfg, err := ParseSocksExternalConfig(v.ConfigJSON)
	if err != nil {
		return true
	}
	return cfg.RemoteDNS
}

// SocksExternalRequiresDNSModeCapability reports whether rendering this SOCKS
// node requires the kernel socks5_dns_mode_v1 capability (local DNS on v5/v4a).
func SocksExternalRequiresDNSModeCapability(v model.ExternalOutbound) bool {
	if v.Protocol != model.ProtocolSocks {
		return false
	}
	cfg, err := ParseSocksExternalConfig(v.ConfigJSON)
	return err == nil && cfg.Version != "4" && !cfg.RemoteDNS
}

func socksExternalOutboundToSingBox(v model.ExternalOutbound, server model.Server, outboundTag string) (map[string]any, error) {
	cfg, err := ParseSocksExternalConfig(v.ConfigJSON)
	if err != nil {
		return nil, err
	}
	if cfg.Version != "4" && !cfg.RemoteDNS {
		if strings.TrimSpace(server.AgentID) != "" && len(server.KernelCapabilities) > 0 && !stringSliceContains(server.KernelCapabilities, KernelCapabilitySocks5DNSMode) {
			return nil, fmt.Errorf("服务器 %s 的内核缺少 %s 能力，不能使用本地 DNS 模式（remote_dns=false）的 SOCKS5 第三方节点；请开启远端 DNS（SOCKS5H）或先更新 Agent/内核", server.Name, KernelCapabilitySocks5DNSMode)
		}
	}
	extra := parseExtra(v.ConfigJSON)
	item := map[string]any{
		"type":        "socks",
		"tag":         outboundTag,
		"server":      v.TargetAddress,
		"server_port": v.TargetPort,
		"version":     cfg.Version,
	}
	applyAllowed(item, extra, "username", "password", "network", "udp_over_tcp", "tcp_fast_open", "domain_resolver", "network_strategy", "fallback_delay")
	if cfg.Version != "4" && !cfg.RemoteDNS {
		item["remote_dns"] = false
	}
	applyServerNetworkPolicy(item, server, model.ProtocolSocks, false)
	omitUnsupportedDialTCPFastOpen(item)
	return item, nil
}

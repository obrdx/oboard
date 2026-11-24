package core

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/OboardProject/oboard/internal/model"
)

// KernelCapabilityExternalWireGuardProbe is advertised by oboard-sb builds
// whose standalone outbound check accepts an imported WireGuard endpoint.
const KernelCapabilityExternalWireGuardProbe = "external_wireguard_probe_v1"

const (
	wireGuardExternalMaxAddresses  = 8
	wireGuardExternalMaxAllowedIPs = 32
	wireGuardExternalDefaultMTU    = 1408
)

var wireGuardExternalConfigKeys = map[string]bool{
	"type": true, "tag": true, "server": true, "server_port": true,
	"private_key": true, "address": true, "local_address": true, "peer_public_key": true, "public_key": true,
	"pre_shared_key": true, "reserved": true, "mtu": true, "persistent_keepalive_interval": true,
	"allowed_ips": true, "peers": true,
}

var wireGuardExternalPeerKeys = map[string]bool{
	"address": true, "server": true, "port": true, "server_port": true, "public_key": true, "peer_public_key": true,
	"pre_shared_key": true, "allowed_ips": true, "persistent_keepalive_interval": true, "reserved": true,
}

// WireGuardExternalConfig is the closed field set of an imported WireGuard
// peer. OBoard only dials it as a single-peer client endpoint.
type WireGuardExternalConfig struct {
	PrivateKey    string
	Address       []string
	PeerPublicKey string
	PreSharedKey  string
	Reserved      []int
	MTU           int
	Keepalive     int
	AllowedIPs    []string
	Server        string
	ServerPort    int
}

// ExternalProtocolChainOnly reports whether a third-party protocol is only a
// proxy-path exit and is never rendered into a user subscription.
func ExternalProtocolChainOnly(protocol model.Protocol) bool {
	return protocol == model.ProtocolHTTP || protocol == model.ProtocolWireGuard
}

// ParseWireGuardExternalConfig validates an imported WireGuard config_json.
// It accepts the stored canonical form, the sing-box endpoint form with one
// peer and the legacy sing-box outbound form; anything else is rejected so no
// unknown upstream option reaches the kernel.
func ParseWireGuardExternalConfig(raw string) (WireGuardExternalConfig, error) {
	var out WireGuardExternalConfig
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return out, fmt.Errorf("wireguard config_json must be a JSON object: %w", err)
	}
	for key := range cfg {
		if !wireGuardExternalConfigKeys[key] {
			return out, fmt.Errorf("WireGuard 节点不支持字段 %q", key)
		}
	}
	if value, ok := cfg["type"]; ok && value != "wireguard" {
		return out, errors.New("WireGuard 节点的 type 必须是 wireguard")
	}
	peer := map[string]any{}
	for _, key := range []string{"server", "server_port", "peer_public_key", "public_key", "pre_shared_key", "reserved", "allowed_ips", "persistent_keepalive_interval"} {
		if value, ok := cfg[key]; ok {
			peer[key] = value
		}
	}
	if rawPeers, ok := cfg["peers"]; ok && rawPeers != nil {
		peers, isList := rawPeers.([]any)
		if !isList || len(peers) != 1 {
			return out, errors.New("WireGuard 节点必须且只能包含一个 peer")
		}
		item, isMap := peers[0].(map[string]any)
		if !isMap {
			return out, errors.New("peers[0] 必须是对象")
		}
		for key, value := range item {
			if !wireGuardExternalPeerKeys[key] {
				return out, fmt.Errorf("WireGuard peer 不支持字段 %q", key)
			}
			peer[key] = value
		}
	}
	var ok bool
	if out.PrivateKey, ok = optionalString(cfg, "private_key"); !ok || !validWireGuardKey(out.PrivateKey) {
		return out, errors.New("private_key 必须是 32 字节 Base64 密钥")
	}
	publicKey, ok := firstString(peer, "public_key", "peer_public_key")
	if !ok || !validWireGuardKey(publicKey) {
		return out, errors.New("peer 公钥必须是 32 字节 Base64 密钥")
	}
	out.PeerPublicKey = publicKey
	if out.PreSharedKey, ok = optionalString(peer, "pre_shared_key"); !ok || out.PreSharedKey != "" && !validWireGuardKey(out.PreSharedKey) {
		return out, errors.New("pre_shared_key 必须是 32 字节 Base64 密钥")
	}
	addressValue := cfg["address"]
	if addressValue == nil {
		addressValue = cfg["local_address"]
	}
	addresses, err := wireGuardPrefixList(addressValue, true)
	if err != nil {
		return out, fmt.Errorf("address 无效：%w", err)
	}
	if len(addresses) == 0 || len(addresses) > wireGuardExternalMaxAddresses {
		return out, fmt.Errorf("address 需要 1-%d 个本地隧道地址", wireGuardExternalMaxAddresses)
	}
	out.Address = addresses
	if peer["allowed_ips"] != nil {
		allowed, err := wireGuardPrefixList(peer["allowed_ips"], false)
		if err != nil {
			return out, fmt.Errorf("allowed_ips 无效：%w", err)
		}
		if len(allowed) > wireGuardExternalMaxAllowedIPs {
			return out, fmt.Errorf("allowed_ips 最多 %d 项", wireGuardExternalMaxAllowedIPs)
		}
		if !(len(allowed) == 2 && allowed[0] == "0.0.0.0/0" && allowed[1] == "::/0") {
			out.AllowedIPs = allowed
		}
	}
	if out.Reserved, err = wireGuardReserved(peer["reserved"]); err != nil {
		return out, err
	}
	if value, exists := cfg["mtu"]; exists && value != nil {
		mtu, isInt := wholeNumber(value)
		if !isInt || mtu < 576 || mtu > 9000 {
			return out, errors.New("mtu 必须是 576-9000 的整数")
		}
		out.MTU = mtu
	}
	if value, exists := peer["persistent_keepalive_interval"]; exists && value != nil {
		keepalive, isInt := wholeNumber(value)
		if !isInt || keepalive < 0 || keepalive > 65535 {
			return out, errors.New("persistent_keepalive_interval 必须是 0-65535 的整数")
		}
		out.Keepalive = keepalive
	}
	if out.Server, ok = firstString(peer, "address", "server"); !ok {
		return out, errors.New("peer 地址必须是字符串")
	}
	if value, exists := peer["port"]; exists {
		out.ServerPort, _ = wholeNumber(value)
	} else if value, exists := peer["server_port"]; exists {
		out.ServerPort, _ = wholeNumber(value)
	}
	return out, nil
}

// NormalizeWireGuardExternalConfigJSON rewrites an imported WireGuard config to
// its canonical stored form: the closed field set without type/tag/server.
func NormalizeWireGuardExternalConfigJSON(raw string) (string, error) {
	cfg, err := ParseWireGuardExternalConfig(raw)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(wireGuardExternalConfigFields(cfg))
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func wireGuardExternalConfigFields(cfg WireGuardExternalConfig) map[string]any {
	out := map[string]any{"private_key": cfg.PrivateKey, "address": cfg.Address, "peer_public_key": cfg.PeerPublicKey}
	if cfg.PreSharedKey != "" {
		out["pre_shared_key"] = cfg.PreSharedKey
	}
	if len(cfg.Reserved) > 0 {
		out["reserved"] = cfg.Reserved
	}
	if cfg.MTU > 0 {
		out["mtu"] = cfg.MTU
	}
	if cfg.Keepalive > 0 {
		out["persistent_keepalive_interval"] = cfg.Keepalive
	}
	if len(cfg.AllowedIPs) > 0 {
		out["allowed_ips"] = cfg.AllowedIPs
	}
	return out
}

// wireGuardExternalToSingBox renders an imported WireGuard peer as a sing-box
// endpoint. sing-box has no WireGuard outbound, so BuildConfig moves every
// rendered item of this type into the endpoints list.
func wireGuardExternalToSingBox(v model.ExternalOutbound, server model.Server, outboundTag string) (map[string]any, error) {
	cfg, err := ParseWireGuardExternalConfig(v.ConfigJSON)
	if err != nil {
		return nil, err
	}
	mtu := cfg.MTU
	if mtu == 0 {
		mtu = wireGuardExternalDefaultMTU
	}
	allowed := cfg.AllowedIPs
	if len(allowed) == 0 {
		allowed = []string{"0.0.0.0/0", "::/0"}
	}
	peer := map[string]any{"address": v.TargetAddress, "port": v.TargetPort, "public_key": cfg.PeerPublicKey, "allowed_ips": allowed}
	if cfg.PreSharedKey != "" {
		peer["pre_shared_key"] = cfg.PreSharedKey
	}
	if len(cfg.Reserved) > 0 {
		peer["reserved"] = cfg.Reserved
	}
	if cfg.Keepalive > 0 {
		peer["persistent_keepalive_interval"] = cfg.Keepalive
	}
	item := map[string]any{"type": "wireguard", "tag": outboundTag, "address": cfg.Address, "private_key": cfg.PrivateKey, "mtu": mtu, "peers": []any{peer}}
	applyServerNetworkPolicy(item, server, model.ProtocolWireGuard, false)
	return item, nil
}

// relocateWireGuardOutbounds moves rendered WireGuard items from the outbound
// list into the endpoint list, where sing-box registers them. Endpoints are
// looked up by the same tag as outbounds, so routes and detours still resolve.
func relocateWireGuardOutbounds(config *SingBoxConfig) {
	if config == nil {
		return
	}
	kept := config.Outbounds[:0]
	for _, item := range config.Outbounds {
		if stringFromAny(item["type"]) == "wireguard" {
			config.Endpoints = append(config.Endpoints, item)
			continue
		}
		kept = append(kept, item)
	}
	config.Outbounds = kept
}

func validWireGuardKey(value string) bool {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	return err == nil && len(decoded) == 32
}

func firstString(values map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		value, exists := values[key]
		if !exists || value == nil {
			continue
		}
		text, ok := value.(string)
		return strings.TrimSpace(text), ok
	}
	return "", true
}

func wholeNumber(value any) (int, bool) {
	switch number := value.(type) {
	case float64:
		if number != float64(int(number)) {
			return 0, false
		}
		return int(number), true
	case int:
		return number, true
	case json.Number:
		parsed, err := strconv.Atoi(number.String())
		return parsed, err == nil
	}
	return 0, false
}

// wireGuardPrefixList accepts a list or a comma-separated string of CIDR
// prefixes. With hostAddress a bare IP becomes a single-host prefix.
func wireGuardPrefixList(value any, hostAddress bool) ([]string, error) {
	var items []string
	switch list := value.(type) {
	case nil:
		return nil, nil
	case string:
		items = strings.Split(list, ",")
	case []any:
		for _, entry := range list {
			text, ok := entry.(string)
			if !ok {
				return nil, errors.New("必须是字符串")
			}
			items = append(items, text)
		}
	case []string:
		items = list
	default:
		return nil, errors.New("必须是字符串或字符串数组")
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if hostAddress && !strings.Contains(item, "/") {
			addr, err := netip.ParseAddr(item)
			if err != nil {
				return nil, fmt.Errorf("%q 不是 IP 地址", item)
			}
			out = append(out, netip.PrefixFrom(addr, addr.BitLen()).String())
			continue
		}
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("%q 不是 CIDR", item)
		}
		if !hostAddress {
			prefix = prefix.Masked()
		}
		out = append(out, prefix.String())
	}
	return out, nil
}

// wireGuardReserved accepts the three reserved bytes as a number list, a
// comma-separated string or a Base64 string.
func wireGuardReserved(value any) ([]int, error) {
	invalid := errors.New("reserved 必须是 3 个 0-255 的整数")
	var out []int
	switch reserved := value.(type) {
	case nil:
		return nil, nil
	case []any:
		for _, entry := range reserved {
			number, ok := wholeNumber(entry)
			if !ok {
				return nil, invalid
			}
			out = append(out, number)
		}
	case []int:
		out = reserved
	case string:
		reserved = strings.TrimSpace(reserved)
		if reserved == "" {
			return nil, nil
		}
		if strings.Contains(reserved, ",") {
			for _, part := range strings.Split(reserved, ",") {
				number, err := strconv.Atoi(strings.TrimSpace(part))
				if err != nil {
					return nil, invalid
				}
				out = append(out, number)
			}
		} else {
			decoded, err := base64.StdEncoding.DecodeString(reserved)
			if err != nil {
				return nil, invalid
			}
			for _, b := range decoded {
				out = append(out, int(b))
			}
		}
	default:
		return nil, invalid
	}
	if len(out) != 3 {
		return nil, invalid
	}
	for _, number := range out {
		if number < 0 || number > 255 {
			return nil, invalid
		}
	}
	if out[0] == 0 && out[1] == 0 && out[2] == 0 {
		return nil, nil
	}
	return out, nil
}

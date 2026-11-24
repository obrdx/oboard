package controller

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
)

// isWireGuardINI reports whether import content is a wg-quick style
// configuration file ([Interface] + [Peer] sections).
func isWireGuardINI(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		return strings.EqualFold(line, "[Interface]")
	}
	return false
}

// parseWireGuardINIImport converts one wg-quick configuration into a
// third-party WireGuard node. Host-side directives (DNS, Table, PostUp and the
// like) are ignored: OBoard never runs wg-quick, it dials the peer from the
// kernel endpoint only.
func parseWireGuardINIImport(content string) (model.ExternalOutbound, error) {
	cfg := map[string]any{"type": "wireguard"}
	peer := map[string]any{}
	section := ""
	peers := 0
	var addresses, allowed []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			switch section {
			case "interface":
			case "peer":
				peers++
				if peers > 1 {
					return model.ExternalOutbound{}, errors.New("WireGuard 配置只能包含一个 [Peer]")
				}
			default:
				return model.ExternalOutbound{}, fmt.Errorf("WireGuard 配置不支持段 [%s]", section)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return model.ExternalOutbound{}, fmt.Errorf("WireGuard 配置行无效：%q", line)
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch section {
		case "interface":
			switch key {
			case "privatekey":
				cfg["private_key"] = value
			case "address":
				addresses = append(addresses, splitWireGuardList(value)...)
			case "mtu":
				mtu, err := strconv.Atoi(value)
				if err != nil {
					return model.ExternalOutbound{}, errors.New("MTU 必须是整数")
				}
				cfg["mtu"] = mtu
			}
		case "peer":
			switch key {
			case "publickey":
				peer["public_key"] = value
			case "presharedkey":
				peer["pre_shared_key"] = value
			case "allowedips":
				allowed = append(allowed, splitWireGuardList(value)...)
			case "persistentkeepalive":
				if strings.EqualFold(value, "off") {
					continue
				}
				keepalive, err := strconv.Atoi(value)
				if err != nil {
					return model.ExternalOutbound{}, errors.New("PersistentKeepalive 必须是整数")
				}
				peer["persistent_keepalive_interval"] = keepalive
			case "reserved":
				peer["reserved"] = value
			case "endpoint":
				host, portText, err := net.SplitHostPort(value)
				if err != nil {
					return model.ExternalOutbound{}, fmt.Errorf("Endpoint 无效：%q", value)
				}
				port, err := strconv.Atoi(portText)
				if err != nil {
					return model.ExternalOutbound{}, fmt.Errorf("Endpoint 端口无效：%q", value)
				}
				cfg["server"], cfg["server_port"] = host, port
			}
		default:
			return model.ExternalOutbound{}, errors.New("WireGuard 配置必须以 [Interface] 开头")
		}
	}
	if err := scanner.Err(); err != nil {
		return model.ExternalOutbound{}, err
	}
	if peers != 1 {
		return model.ExternalOutbound{}, errors.New("WireGuard 配置缺少 [Peer]")
	}
	if cfg["server"] == nil {
		return model.ExternalOutbound{}, errors.New("WireGuard 配置的 [Peer] 缺少 Endpoint")
	}
	cfg["address"] = addresses
	if len(allowed) > 0 {
		peer["allowed_ips"] = allowed
	}
	for key, value := range peer {
		cfg[key] = value
	}
	host, _ := cfg["server"].(string)
	cfg["tag"] = "WireGuard " + host
	return externalOutboundFromRawMap(cfg, "wireguard-import")
}

// parseWireGuardImportURI accepts the common wireguard:// and wg:// share
// forms: the private key in the user info (or a privatekey query), and the
// peer public key, tunnel addresses, MTU, reserved bytes, pre-shared key,
// keepalive and allowed IPs as query parameters. The URI is split by hand
// because Base64 keys routinely carry unescaped "/" and "+".
func parseWireGuardImportURI(line string) (model.ExternalOutbound, error) {
	_, rest, ok := strings.Cut(line, "://")
	if !ok {
		return model.ExternalOutbound{}, errors.New("invalid wireguard URI")
	}
	fragment := ""
	if index := strings.Index(rest, "#"); index >= 0 {
		rest, fragment = rest[:index], rest[index+1:]
	}
	rawQuery := ""
	if index := strings.Index(rest, "?"); index >= 0 {
		rest, rawQuery = rest[:index], rest[index+1:]
	}
	rest = strings.TrimSuffix(rest, "/")
	userinfo, hostport := "", rest
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		userinfo, hostport = rest[:at], rest[at+1:]
	}
	query, err := parseWireGuardQuery(rawQuery)
	if err != nil {
		return model.ExternalOutbound{}, err
	}
	privateKey, err := url.PathUnescape(userinfo)
	if err != nil {
		return model.ExternalOutbound{}, errors.New("invalid wireguard URI private key")
	}
	if privateKey == "" {
		privateKey = wireGuardQuery(query, "privatekey", "private-key", "private_key")
	}
	host, portText, err := net.SplitHostPort(hostport)
	if err != nil {
		return model.ExternalOutbound{}, errors.New("invalid wireguard URI: host and port required")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return model.ExternalOutbound{}, errors.New("invalid wireguard URI port")
	}
	addresses := splitWireGuardList(wireGuardQuery(query, "address", "ip", "local_address", "local-address"))
	addresses = append(addresses, splitWireGuardList(wireGuardQuery(query, "ipv6"))...)
	cfg := map[string]any{
		"type":            "wireguard",
		"server":          host,
		"server_port":     port,
		"private_key":     privateKey,
		"peer_public_key": wireGuardQuery(query, "publickey", "public-key", "public_key", "peer", "peer-public-key", "peer_public_key"),
		"address":         addresses,
	}
	if psk := wireGuardQuery(query, "presharedkey", "pre-shared-key", "pre_shared_key", "psk"); psk != "" {
		cfg["pre_shared_key"] = psk
	}
	if reserved := wireGuardQuery(query, "reserved"); reserved != "" {
		cfg["reserved"] = reserved
	}
	if allowed := splitWireGuardList(wireGuardQuery(query, "allowedips", "allowed-ips", "allowed_ips")); len(allowed) > 0 {
		cfg["allowed_ips"] = allowed
	}
	for key, names := range map[string][]string{"mtu": {"mtu"}, "persistent_keepalive_interval": {"keepalive", "persistent-keepalive", "persistentkeepalive", "persistent_keepalive_interval"}} {
		if text := wireGuardQuery(query, names...); text != "" {
			number, err := strconv.Atoi(text)
			if err != nil {
				return model.ExternalOutbound{}, fmt.Errorf("invalid wireguard URI %s", names[0])
			}
			cfg[key] = number
		}
	}
	name := "WireGuard " + strings.Trim(host, "[]")
	if decoded, err := url.PathUnescape(fragment); err == nil && strings.TrimSpace(decoded) != "" {
		name = strings.TrimSpace(decoded)
	}
	cfg["tag"] = name
	return externalOutboundFromRawMap(cfg, "wireguard-import")
}

// parseWireGuardQuery decodes the query without turning "+" into a space, so
// unescaped Base64 keys survive.
func parseWireGuardQuery(raw string) (map[string]string, error) {
	out := map[string]string{}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		key, value, _ := strings.Cut(pair, "=")
		key, err := url.PathUnescape(key)
		if err != nil {
			return nil, errors.New("invalid wireguard URI query")
		}
		value, err = url.PathUnescape(value)
		if err != nil {
			return nil, errors.New("invalid wireguard URI query")
		}
		out[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return out, nil
}

func wireGuardQuery(query map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := query[key]; value != "" {
			return value
		}
	}
	return ""
}

func splitWireGuardList(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// normalizeWireGuardExternal moves the peer endpoint of an imported WireGuard
// config into target_address/target_port and stores the closed field set.
func normalizeWireGuardExternal(v *model.ExternalOutbound) error {
	cfg, err := core.ParseWireGuardExternalConfig(v.ConfigJSON)
	if err != nil {
		return err
	}
	if v.TargetAddress == "" {
		v.TargetAddress = cfg.Server
		if v.TargetPort == 0 {
			v.TargetPort = cfg.ServerPort
		}
	}
	normalized, err := core.NormalizeWireGuardExternalConfigJSON(v.ConfigJSON)
	if err != nil {
		return err
	}
	v.ConfigJSON = normalized
	return nil
}

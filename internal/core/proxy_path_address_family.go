package core

import (
	"fmt"
	"strings"

	"github.com/OboardProject/oboard/internal/model"
)

// Per-step address family: how the previous hop dials the node this step
// describes. auto keeps the server-wide entry policy; the other values pick a
// detected public address of one family for this segment only.
const (
	ProxyPathAddressFamilyAuto       = "auto"
	ProxyPathAddressFamilyPreferIPv4 = "prefer_ipv4"
	ProxyPathAddressFamilyPreferIPv6 = "prefer_ipv6"
	ProxyPathAddressFamilyIPv4Only   = "ipv4_only"
	ProxyPathAddressFamilyIPv6Only   = "ipv6_only"
)

// ProxyPathAddressFamilyConfigKey is the step config_json key. It is omitted
// when the value is auto.
const ProxyPathAddressFamilyConfigKey = "address_family"

// NormalizeProxyPathAddressFamily validates a stored or requested value. An
// empty value is auto.
func NormalizeProxyPathAddressFamily(value string) (string, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case "", ProxyPathAddressFamilyAuto:
		return ProxyPathAddressFamilyAuto, nil
	case ProxyPathAddressFamilyPreferIPv4, ProxyPathAddressFamilyPreferIPv6, ProxyPathAddressFamilyIPv4Only, ProxyPathAddressFamilyIPv6Only:
		return normalized, nil
	default:
		return "", fmt.Errorf("address_family 必须是 auto、prefer_ipv4、prefer_ipv6、ipv4_only 或 ipv6_only")
	}
}

// ProxyPathStepAddressFamily returns the step's normalized address family.
// An unreadable value falls back to auto; writes are validated separately.
func ProxyPathStepAddressFamily(step model.ProxyPathStep) string {
	family, err := NormalizeProxyPathAddressFamily(stringValue(parseStepConfig(step.ConfigJSON), ProxyPathAddressFamilyConfigKey, ""))
	if err != nil {
		return ProxyPathAddressFamilyAuto
	}
	return family
}

func proxyPathAddressFamilyOrder(family string) (order []string, strict bool) {
	switch family {
	case ProxyPathAddressFamilyIPv4Only:
		return []string{"ipv4"}, true
	case ProxyPathAddressFamilyIPv6Only:
		return []string{"ipv6"}, true
	case ProxyPathAddressFamilyPreferIPv6:
		return []string{"ipv6", "ipv4"}, false
	case ProxyPathAddressFamilyPreferIPv4:
		return []string{"ipv4", "ipv6"}, false
	default:
		return nil, false
	}
}

// resolveProxyPathStepAddress picks the address the source dials to reach the
// target of one path segment. inbound is the target listener when the segment
// dials a protocol inbound directly (sing-box chain); forwards and tunnels pass
// nil because their target-side listener is managed separately.
func resolveProxyPathStepAddress(source, target model.Server, inbound *model.Inbound, family string) (string, error) {
	address, _, err := resolveProxyPathStepDial(source, target, inbound, family)
	return address, err
}

// resolveProxyPathStepDial also returns the resolver strategy a sing-box
// outbound must use when the chosen address is a domain (custom entry domain).
func resolveProxyPathStepDial(source, target model.Server, inbound *model.Inbound, family string) (string, string, error) {
	order, strict := proxyPathAddressFamilyOrder(family)
	if len(order) == 0 {
		if inbound != nil {
			address, err := ResolveReachableEntryAddress(source, *inbound, target)
			return address, "", err
		}
		address, err := ResolveReachableServerEntryAddress(source, target)
		return address, "", err
	}
	mode := target.EntryIPMode
	externalIP := ""
	if inbound != nil {
		externalIP = inbound.ExternalIP
		if inbound.EntryIPMode != "" && inbound.EntryIPMode != model.EntryIPModeAuto {
			mode = inbound.EntryIPMode
		}
	}
	if mode == model.EntryIPModeCustom {
		address := entryAddressForMode(mode, externalIP, target)
		addressFamily := AddressFamily(address)
		if addressFamily == "domain" {
			// The domain is kept; the family becomes the outbound's resolver
			// strategy. Forwards and tunnels resolve on the Agent and cannot.
			if inbound == nil {
				return "", "", markInvalidDesiredState(fmt.Errorf("目标服务器 %s 的入口是自定义域名 %s，端口转发和隧道无法为它指定地址族；请改用 sing-box 出站链，或把入口地址改为 IP", target.Name, address))
			}
			if strict {
				if listenIP := EffectiveListenIP(target, inbound.ListenIP); !ListenIPSupportsFamily(listenIP, order[0]) {
					return "", "", markInvalidDesiredState(fmt.Errorf("目标入口监听地址 %q 不接受 %s 连接", listenIP, strings.ToUpper(order[0])))
				}
				if (order[0] == "ipv6" && EffectiveIPStack(source) == model.IPStackIPv4Only) || (order[0] == "ipv4" && EffectiveIPStack(source) == model.IPStackIPv6Only) {
					return "", "", markInvalidDesiredState(fmt.Errorf("源服务器 %s（%s）无法连接 %s", source.Name, EffectiveIPStack(source), strings.ToUpper(order[0])))
				}
			}
			return address, family, nil
		}
		if strict && addressFamily != order[0] {
			return "", "", markInvalidDesiredState(fmt.Errorf("目标服务器 %s 的自定义入口地址 %q 不是 %s，不能使用“仅 %s”", target.Name, address, strings.ToUpper(order[0]), strings.ToUpper(order[0])))
		}
		resolved, err := validateReachableServerAddress(source, target, address)
		return resolved, "", err
	}
	reasons := make([]string, 0, len(order))
	for _, candidate := range order {
		address := strings.TrimSpace(target.PublicIPv4)
		if candidate == "ipv6" {
			address = ServerEntryIPv6(target)
		}
		label := strings.ToUpper(candidate)
		if address == "" {
			reasons = append(reasons, fmt.Sprintf("目标服务器 %s 没有 %s 地址", target.Name, label))
			continue
		}
		if inbound != nil {
			if listenIP := EffectiveListenIP(target, inbound.ListenIP); !ListenIPSupportsFamily(listenIP, candidate) {
				reasons = append(reasons, fmt.Sprintf("目标入口监听地址 %q 不接受 %s 连接", listenIP, label))
				continue
			}
		}
		if ValidateAddressForIPStack(EffectiveIPStack(source), address) != nil {
			reasons = append(reasons, fmt.Sprintf("源服务器 %s（%s）无法连接 %s", source.Name, EffectiveIPStack(source), label))
			continue
		}
		return address, "", nil
	}
	return "", "", markInvalidDesiredState(fmt.Errorf("这一段的地址族设置（%s）无法满足：%s", family, strings.Join(reasons, "；")))
}

// applyProxyPathImportedAddressFamily applies the segment family to a
// third-party node outbound. A domain target gets a resolver strategy; a
// literal target only has to match a strict setting.
func applyProxyPathImportedAddressFamily(item map[string]any, external model.ExternalOutbound, family string) error {
	order, strict := proxyPathAddressFamilyOrder(family)
	if len(order) == 0 {
		return nil
	}
	addressFamily := AddressFamily(external.TargetAddress)
	if addressFamily != "domain" {
		if strict && addressFamily != order[0] {
			return markInvalidDesiredState(fmt.Errorf("导入节点 %s 的地址 %q 不是 %s，不能使用“仅 %s”", external.Name, external.TargetAddress, strings.ToUpper(order[0]), strings.ToUpper(order[0])))
		}
		return nil
	}
	resolver := domainResolverMap(item["domain_resolver"])
	resolver["strategy"] = family
	item["domain_resolver"] = resolver
	return nil
}

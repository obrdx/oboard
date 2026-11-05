package core

import (
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
)

func addressFamilyTestServers() (model.Server, model.Server) {
	source := model.Server{ID: 1, Name: "A", ChainSecret: "chain-a", PublicIPv4: "203.0.113.1", PublicIPv6: "2001:db8::1", ListenIP: "0.0.0.0", ListenMode: model.ListenModeDual, PortRangeStart: 30000, PortRangeEnd: 30100}
	target := model.Server{ID: 2, Name: "B", ChainSecret: "chain-b", PublicIPv4: "203.0.113.2", PublicIPv6: "2001:db8::2", ListenIP: "0.0.0.0", ListenMode: model.ListenModeDual, PortRangeStart: 31000, PortRangeEnd: 31100}
	return source, target
}

func TestResolveProxyPathStepAddressFamilies(t *testing.T) {
	source, target := addressFamilyTestServers()
	dual := model.Inbound{ID: 20, ServerID: target.ID, ListenIP: "::", Port: 443}
	cases := []struct {
		family string
		want   string
	}{
		{ProxyPathAddressFamilyAuto, "203.0.113.2"},
		{ProxyPathAddressFamilyPreferIPv4, "203.0.113.2"},
		{ProxyPathAddressFamilyPreferIPv6, "2001:db8::2"},
		{ProxyPathAddressFamilyIPv4Only, "203.0.113.2"},
		{ProxyPathAddressFamilyIPv6Only, "2001:db8::2"},
	}
	for _, tc := range cases {
		got, err := resolveProxyPathStepAddress(source, target, &dual, tc.family)
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %q err=%v, want %q", tc.family, got, err, tc.want)
		}
	}

	// A DNS-synced dual-stack entry keeps its name on auto but dials the chosen
	// literal once the segment pins a family.
	synced := dual
	synced.DNSSyncEnabled = true
	synced.DNSDomain = "b.example.com"
	if got, _ := resolveProxyPathStepAddress(source, target, &synced, ProxyPathAddressFamilyAuto); got != "b.example.com" {
		t.Fatalf("auto synced entry = %q", got)
	}
	if got, _ := resolveProxyPathStepAddress(source, target, &synced, ProxyPathAddressFamilyIPv6Only); got != "2001:db8::2" {
		t.Fatalf("ipv6_only synced entry = %q", got)
	}

	// prefer falls back when the target lacks the family; only refuses.
	v4Target := target
	v4Target.PublicIPv6 = ""
	if got, err := resolveProxyPathStepAddress(source, v4Target, nil, ProxyPathAddressFamilyPreferIPv6); err != nil || got != "203.0.113.2" {
		t.Fatalf("prefer_ipv6 fallback = %q err=%v", got, err)
	}
	if _, err := resolveProxyPathStepAddress(source, v4Target, nil, ProxyPathAddressFamilyIPv6Only); err == nil {
		t.Fatal("ipv6_only to an IPv4-only target must fail")
	}

	// A listener bound to IPv4 only cannot take an IPv6-only segment.
	v4Listener := dual
	v4Listener.ListenIP = "203.0.113.2"
	if _, err := resolveProxyPathStepAddress(source, target, &v4Listener, ProxyPathAddressFamilyIPv6Only); err == nil || !strings.Contains(err.Error(), "监听地址") {
		t.Fatalf("ipv6_only to IPv4 listener err=%v", err)
	}

	// An IPv4-only source cannot dial IPv6.
	v4Source := source
	v4Source.IPStack = model.IPStackIPv4Only
	if _, err := resolveProxyPathStepAddress(v4Source, target, nil, ProxyPathAddressFamilyIPv6Only); err == nil {
		t.Fatal("ipv6_only from an IPv4-only source must fail")
	}

	// A custom domain entry cannot be pinned to a family.
	custom := target
	custom.EntryIPMode = model.EntryIPModeCustom
	custom.EntryAddress = "entry.example.com"
	if _, err := resolveProxyPathStepAddress(source, custom, nil, ProxyPathAddressFamilyPreferIPv6); err == nil || !strings.Contains(err.Error(), "sing-box") {
		t.Fatalf("forward/tunnel to a custom domain entry with a family must fail: %v", err)
	}
	address, strategy, err := resolveProxyPathStepDial(source, custom, &dual, ProxyPathAddressFamilyPreferIPv6)
	if err != nil || address != "entry.example.com" || strategy != "prefer_ipv6" {
		t.Fatalf("chain to custom domain = %q %q err=%v", address, strategy, err)
	}
	if _, _, err := resolveProxyPathStepDial(source, custom, &v4Listener, ProxyPathAddressFamilyIPv6Only); err == nil {
		t.Fatal("ipv6_only custom domain to an IPv4 listener must fail")
	}
}

func TestProxyPathAddressFamilyAppliesToTunnelAndChain(t *testing.T) {
	source, target := addressFamilyTestServers()
	root := model.Inbound{ID: 10, ServerID: source.ID, Name: "entry", Protocol: model.ProtocolVLESS, ListenIP: "0.0.0.0", Port: 443, ConfigJSON: `{}`, Enabled: true}
	targetID := target.ID
	paths := []model.ProxyPath{{ID: 10, Name: "wg", InboundID: root.ID, Enabled: true}}
	auto := []model.ProxyPathStep{{ID: 11, PathID: 10, Position: 1, NodeType: model.ProxyPathStepServerInbound, ServerID: &targetID, TransportMode: model.ProxyPathTransportTunnel, ConfigJSON: `{"type":"wireguard","persistent_keepalive":25}`}}
	pinned := []model.ProxyPathStep{auto[0]}
	pinned[0].ConfigJSON = `{"type":"wireguard","persistent_keepalive":25,"address_family":"ipv6_only"}`

	autoPlans, err := BuildProxyPathPlans(paths, auto, []model.Server{source, target}, []model.Inbound{root})
	if err != nil {
		t.Fatal(err)
	}
	pinnedPlans, err := BuildProxyPathPlans(paths, pinned, []model.Server{source, target}, []model.Inbound{root})
	if err != nil {
		t.Fatal(err)
	}
	autoTunnel, pinnedTunnel := autoPlans[0].Tunnels[0], pinnedPlans[0].Tunnels[0]
	if autoTunnel.TargetEndpoint != "203.0.113.2" || pinnedTunnel.TargetEndpoint != "2001:db8::2" {
		t.Fatalf("tunnel endpoints auto=%q pinned=%q", autoTunnel.TargetEndpoint, pinnedTunnel.TargetEndpoint)
	}
	if autoTunnel.ID == pinnedTunnel.ID {
		t.Fatal("a pinned-family tunnel must not share identity with the auto tunnel")
	}
	if autoTunnel.ID != stableProxyPathResourceID("proxy-path-tunnel", proxyPathTunnelReuseKey(auto[0], source.ID, target.ID, model.Inbound{})) {
		t.Fatal("auto tunnel identity changed")
	}

	inbound := model.Inbound{ID: 20, ServerID: target.ID, Name: "b-vless", Protocol: model.ProtocolVLESS, ListenIP: "::", Port: 8443, ConfigJSON: `{}`, Enabled: true}
	inboundID := inbound.ID
	chain := model.ProxyPathStep{ID: 12, PathID: 10, Position: 1, NodeType: model.ProxyPathStepServerInbound, ServerID: &targetID, InboundID: &inboundID, TransportMode: model.ProxyPathTransportSingBox, ConfigJSON: `{"address_family":"prefer_ipv6"}`}
	item, err := proxyPathStepOutbound(paths[0], chain, source, map[int64]model.Server{source.ID: source, target.ID: target}, map[int64]model.Inbound{inbound.ID: inbound}, nil, nil, "path-10-step-1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if item["server"] != "2001:db8::2" {
		t.Fatalf("chain outbound server = %v", item["server"])
	}

	custom := target
	custom.EntryIPMode = model.EntryIPModeCustom
	custom.EntryAddress = "cogent.example.com"
	item, err = proxyPathStepOutbound(paths[0], chain, source, map[int64]model.Server{source.ID: source, custom.ID: custom}, map[int64]model.Inbound{inbound.ID: inbound}, nil, nil, "path-10-step-1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolver, _ := item["domain_resolver"].(map[string]any)
	if item["server"] != "cogent.example.com" || resolver["strategy"] != "prefer_ipv6" {
		t.Fatalf("custom domain chain outbound = %v resolver=%v", item["server"], resolver)
	}
}

func TestApplyProxyPathImportedAddressFamily(t *testing.T) {
	item := map[string]any{"domain_resolver": map[string]any{"server": "bootstrap-primary", "strategy": "prefer_ipv4"}}
	if err := applyProxyPathImportedAddressFamily(item, model.ExternalOutbound{Name: "x", TargetAddress: "node.example.com"}, ProxyPathAddressFamilyIPv6Only); err != nil {
		t.Fatal(err)
	}
	resolver := item["domain_resolver"].(map[string]any)
	if resolver["strategy"] != "ipv6_only" || resolver["server"] != "bootstrap-primary" {
		t.Fatalf("resolver = %#v", resolver)
	}
	if err := applyProxyPathImportedAddressFamily(map[string]any{}, model.ExternalOutbound{Name: "x", TargetAddress: "198.51.100.1"}, ProxyPathAddressFamilyIPv6Only); err == nil {
		t.Fatal("ipv6_only on an IPv4 literal node must fail")
	}
	if err := applyProxyPathImportedAddressFamily(map[string]any{}, model.ExternalOutbound{Name: "x", TargetAddress: "198.51.100.1"}, ProxyPathAddressFamilyPreferIPv6); err != nil {
		t.Fatalf("prefer on a literal must pass: %v", err)
	}
}

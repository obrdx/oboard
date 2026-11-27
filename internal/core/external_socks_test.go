package core

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
)

func TestParseSocksExternalConfig(t *testing.T) {
	cfg, err := ParseSocksExternalConfig(`{}`)
	if err != nil || cfg.Version != "5" || !cfg.RemoteDNS {
		t.Fatalf("default socks config = %#v, err = %v", cfg, err)
	}
	cfg, err = ParseSocksExternalConfig(`{"type":"socks","version":"5","remote_dns":true,"username":"u","password":"p"}`)
	if err != nil || cfg.Version != "5" || !cfg.RemoteDNS || !cfg.HasRemoteDNS {
		t.Fatalf("explicit socks5h config = %#v, err = %v", cfg, err)
	}
	cfg, err = ParseSocksExternalConfig(`{"version":"5","remote_dns":false}`)
	if err != nil || cfg.Version != "5" || cfg.RemoteDNS || !cfg.HasRemoteDNS {
		t.Fatalf("local dns socks5 config = %#v, err = %v", cfg, err)
	}
	cfg, err = ParseSocksExternalConfig(`{"version":"4"}`)
	if err != nil || cfg.Version != "4" || cfg.RemoteDNS {
		t.Fatalf("socks4 config = %#v, err = %v", cfg, err)
	}
	if _, err := ParseSocksExternalConfig(`{"version":"4","remote_dns":true}`); err == nil {
		t.Fatal("expected socks4 with remote_dns=true to fail")
	}
	if _, err := ParseSocksExternalConfig(`{"version":"5","remote_dns":"yes"}`); err == nil {
		t.Fatal("expected non-boolean remote_dns to fail")
	}
	if _, err := ParseSocksExternalConfig(`{"version":"6"}`); err == nil {
		t.Fatal("expected invalid socks version to fail")
	}
}

func TestSocksExternalOutboundToSingBoxDNSModeAndCapability(t *testing.T) {
	legacyServer := model.Server{ID: 1, Name: "old-node", AgentID: "agent-1", PublicIPv4: "203.0.113.1", KernelCapabilities: []string{"dns_group_v1"}}
	capableServer := model.Server{ID: 2, Name: "new-node", AgentID: "agent-2", PublicIPv4: "203.0.113.2", KernelCapabilities: []string{"dns_group_v1", KernelCapabilitySocks5DNSMode}}

	socks5h := model.ExternalOutbound{
		ID: 10, Name: "RemoteDNS", Protocol: model.ProtocolSocks,
		TargetAddress: "socks.example.com", TargetPort: 1080,
		ConfigJSON: `{"type":"socks","version":"5","remote_dns":true,"username":"alice","password":"secret"}`,
		Enabled:    true,
	}
	// Default SOCKS5H omits remote_dns in kernel JSON and works on legacy kernels.
	item, err := externalOutboundToSingBoxWithTag(socks5h, legacyServer, nil, "ext-10")
	if err != nil {
		t.Fatalf("socks5h on legacy server failed: %v", err)
	}
	if item["type"] != "socks" || item["version"] != "5" || item["username"] != "alice" || item["password"] != "secret" {
		t.Fatalf("unexpected socks5h outbound: %#v", item)
	}
	if _, exists := item["remote_dns"]; exists {
		t.Fatalf("socks5h outbound must omit remote_dns for kernel compatibility: %#v", item)
	}

	localDNS := model.ExternalOutbound{
		ID: 11, Name: "LocalDNS", Protocol: model.ProtocolSocks,
		TargetAddress: "socks.example.com", TargetPort: 1080,
		ConfigJSON: `{"type":"socks","version":"5","remote_dns":false,"username":"alice","password":"secret"}`,
		Enabled:    true,
	}
	if _, err := externalOutboundToSingBoxWithTag(localDNS, legacyServer, nil, "ext-11"); err == nil || !strings.Contains(err.Error(), KernelCapabilitySocks5DNSMode) {
		t.Fatalf("expected capability error on legacy server, got %v", err)
	}
	item, err = externalOutboundToSingBoxWithTag(localDNS, capableServer, nil, "ext-11")
	if err != nil {
		t.Fatalf("local DNS socks5 on capable server failed: %v", err)
	}
	if item["remote_dns"] != false || item["version"] != "5" {
		t.Fatalf("expected remote_dns=false, got %#v", item)
	}
}

func TestDetouredChainHopsDeferDomainResolutionToPreviousHop(t *testing.T) {
	serverA := model.Server{
		ID: 1, Name: "A", PublicIPv4: "203.0.113.1", ListenIP: "0.0.0.0",
		IPStack: model.IPStackIPv4Only, PortRangeStart: 30000, PortRangeEnd: 30100,
	}
	root := model.Inbound{
		ID: 10, ServerID: serverA.ID, Name: "entry", Protocol: model.ProtocolVLESS,
		ListenIP: "0.0.0.0", Port: 443, ConfigJSON: `{}`, Enabled: true,
	}
	hop1 := model.ExternalOutbound{
		ID: 101, Name: "Hop1", Protocol: model.ProtocolSocks, Scope: model.ExternalOutboundScopeGlobal,
		TargetAddress: "hop1.example.com", TargetPort: 1080,
		ConfigJSON: `{"version":"5","remote_dns":true,"username":"u1","password":"p1"}`, Enabled: true,
	}
	hop2Auto := model.ExternalOutbound{
		ID: 102, Name: "Hop2Auto", Protocol: model.ProtocolSocks, Scope: model.ExternalOutboundScopeGlobal,
		TargetAddress: "hop2.example.com", TargetPort: 1080,
		ConfigJSON: `{"version":"5","remote_dns":true,"username":"u2","password":"p2"}`, Enabled: true,
	}
	hop2PinnedFamily := model.ExternalOutbound{
		ID: 103, Name: "Hop2Pinned", Protocol: model.ProtocolSocks, Scope: model.ExternalOutboundScopeGlobal,
		TargetAddress: "hop2-v4.example.com", TargetPort: 1080,
		ConfigJSON: `{"version":"5","remote_dns":true}`, Enabled: true,
	}

	pathAuto := model.ProxyPath{ID: 50, Name: "auto-detour", InboundID: root.ID, Secret: "s1", Enabled: true}
	pathPinned := model.ProxyPath{ID: 51, Name: "pinned-detour", InboundID: root.ID, Secret: "s2", Enabled: true}
	hop1ID, hop2AutoID, hop2PinnedID := hop1.ID, hop2Auto.ID, hop2PinnedFamily.ID

	opts := ConfigOptions{
		Servers:           []model.Server{serverA},
		Inbounds:          []model.Inbound{root},
		ExternalOutbounds: []model.ExternalOutbound{hop1, hop2Auto, hop2PinnedFamily},
		ProxyPaths:        []model.ProxyPath{pathAuto, pathPinned},
		ProxyPathSteps: []model.ProxyPathStep{
			{ID: 1, PathID: pathAuto.ID, Position: 1, NodeType: model.ProxyPathStepImported, ExternalOutboundID: &hop1ID, TransportMode: model.ProxyPathTransportSingBox, ConfigJSON: `{}`},
			{ID: 2, PathID: pathAuto.ID, Position: 2, NodeType: model.ProxyPathStepImported, ExternalOutboundID: &hop2AutoID, TransportMode: model.ProxyPathTransportSingBox, ConfigJSON: `{}`},
			{ID: 3, PathID: pathPinned.ID, Position: 1, NodeType: model.ProxyPathStepImported, ExternalOutboundID: &hop1ID, TransportMode: model.ProxyPathTransportSingBox, ConfigJSON: `{}`},
			{ID: 4, PathID: pathPinned.ID, Position: 2, NodeType: model.ProxyPathStepImported, ExternalOutboundID: &hop2PinnedID, TransportMode: model.ProxyPathTransportSingBox, ConfigJSON: `{"address_family":"ipv4_only"}`},
		},
	}

	raw := mustServerConfig(t, serverA, opts.Inbounds, nil, opts)
	var parsed SingBoxConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGeneratedSingBoxConfig(parsed); err != nil {
		t.Fatalf("generated config invalid: %v", err)
	}

	step1 := findOutbound(raw, proxyPathStepTag(pathAuto.ID, 1))
	if step1["domain_resolver"] == nil || step1["detour"] != nil {
		t.Fatalf("physical first hop must keep bootstrap domain_resolver and no detour: %#v", step1)
	}
	step2Auto := findOutbound(raw, proxyPathStepTag(pathAuto.ID, 2))
	if step2Auto["detour"] != proxyPathStepTag(pathAuto.ID, 1) {
		t.Fatalf("step 2 detour = %#v", step2Auto)
	}
	if _, exists := step2Auto["domain_resolver"]; exists {
		t.Fatalf("detoured step with auto address_family must omit domain_resolver so hop-1 resolves hostname remotely: %#v", step2Auto)
	}

	step2Pinned := findOutbound(raw, proxyPathStepTag(pathPinned.ID, 2))
	resolver, ok := step2Pinned["domain_resolver"].(map[string]any)
	if !ok || resolver["strategy"] != "ipv4_only" {
		t.Fatalf("detoured step with explicit address_family must keep its resolver strategy: %#v", step2Pinned)
	}
}

func TestResolveProxyPathExitRegionsRefusesLocalDNSSocks5OnLegacyKernel(t *testing.T) {
	server := model.Server{ID: 1, Name: "Legacy", AgentID: "agent-1", PublicIPv4: "203.0.113.1", KernelCapabilities: []string{"dns_group_v1"}}
	root := model.Inbound{ID: 10, ServerID: server.ID, Name: "entry", Protocol: model.ProtocolVLESS, Port: 443, Enabled: true}
	ext := model.ExternalOutbound{ID: 20, Name: "LocalSOCKS", Protocol: model.ProtocolSocks, TargetAddress: "127.0.0.1", TargetPort: 1080, ConfigJSON: `{"version":"5","remote_dns":false}`, Enabled: true}
	extID := ext.ID
	paths := []model.ProxyPath{{ID: 30, Name: "socks-local", InboundID: root.ID, Enabled: true}}
	steps := []model.ProxyPathStep{{ID: 31, PathID: 30, Position: 1, NodeType: model.ProxyPathStepImported, ExternalOutboundID: &extID, TransportMode: model.ProxyPathTransportSingBox}}

	resolved, _ := ResolveProxyPathExitRegions(paths, steps, []model.Server{server}, []model.Inbound{root}, []model.ExternalOutbound{ext}, nil)
	if got := resolved[0]; got.ExitRegionStatus != RegionStatusFailed || !strings.Contains(got.ExitRegionError, "SOCKS5") {
		t.Fatalf("old kernel local-dns socks5 exit = %#v", got)
	}

	server.KernelCapabilities = append(server.KernelCapabilities, KernelCapabilitySocks5DNSMode)
	resolved, _ = ResolveProxyPathExitRegions(paths, steps, []model.Server{server}, []model.Inbound{root}, []model.ExternalOutbound{ext}, nil)
	if got := resolved[0]; got.ExitRegionStatus != RegionStatusPending {
		t.Fatalf("capable kernel local-dns socks5 exit = %#v", got)
	}
}

func TestParsePrivateSubscriptionSupportsSocks5h(t *testing.T) {
	userinfo := base64.StdEncoding.EncodeToString([]byte("bob:secret"))
	content := "socks5h://alice:pw@socks.example.com:1080#RemoteDNS\n" +
		"socks5h://" + userinfo + "@203.0.113.9:1081#Encoded5h"
	res, err := ParsePrivateSubscription(content)
	if err != nil {
		t.Fatalf("ParsePrivateSubscription failed: %v", err)
	}
	if len(res.Nodes) != 2 || len(res.Issues) != 0 {
		t.Fatalf("unexpected parse result: %#v", res)
	}
	if res.Nodes[0].Protocol != model.PrivateProtocolSOCKS5 || res.Nodes[0].Name != "RemoteDNS" || res.Nodes[0].Raw["username"] != "alice" || res.Nodes[0].Raw["password"] != "pw" {
		t.Fatalf("node 0 = %#v", res.Nodes[0])
	}
	if res.Nodes[1].Protocol != model.PrivateProtocolSOCKS5 || res.Nodes[1].Name != "Encoded5h" || res.Nodes[1].Raw["username"] != "bob" || res.Nodes[1].Raw["password"] != "secret" {
		t.Fatalf("node 1 = %#v", res.Nodes[1])
	}
}

func TestValidateGeneratedSingBoxConfigRejectsInvalidSocksRemoteDNS(t *testing.T) {
	cfg := SingBoxConfig{
		DNS: map[string]any{"servers": []any{map[string]any{"type": "local", "tag": "local"}}},
		Outbounds: []map[string]any{
			{"type": "direct", "tag": "direct"},
			{"type": "socks", "tag": "bad", "server": "203.0.113.5", "server_port": 1080, "version": "4", "remote_dns": true},
		},
	}
	if err := ValidateGeneratedSingBoxConfig(cfg); err == nil {
		t.Fatal("expected socks4 with remote_dns=true to fail validation")
	}
}

func TestSingBoxSubscriptionOmitsRemoteDNSForSocks5(t *testing.T) {
	raw := map[string]any{"type": "socks5h", "tag": "S5H", "server": "socks.example.com", "server_port": 1080, "version": "5", "username": "u", "password": "p", "remote_dns": true}
	proxy, err := normalizeSubscriptionNode(SubscriptionNode{Name: "S5H", Raw: raw})
	if err != nil {
		t.Fatalf("normalizeSubscriptionNode: %v", err)
	}
	out := sanitizeSingBoxSubscriptionOutbound(raw, proxy)
	if out["type"] != "socks" || out["version"] != "5" {
		t.Fatalf("unexpected sing-box outbound: %#v", out)
	}
	if _, exists := out["remote_dns"]; exists {
		t.Fatalf("upstream sing-box subscription must not include remote_dns: %#v", out)
	}
}

package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
)

func TestHTTPExternalOutboundRendersClosedFieldSet(t *testing.T) {
	external := model.ExternalOutbound{ID: 7, Protocol: model.ProtocolHTTP, TargetAddress: "proxy.example.com", TargetPort: 8443, ConfigJSON: `{"username":"a","password":"b","headers":{"X-Token":"t"},"tls":{"enabled":true}}`}
	server := model.Server{ID: 1, Name: "edge", AgentID: "agent", KernelCapabilities: []string{KernelCapabilityHTTPOutbound}}
	item, err := externalOutboundToSingBoxWithTag(external, server, nil, "path-1-step-2")
	if err != nil {
		t.Fatal(err)
	}
	if item["type"] != "http" || item["tag"] != "path-1-step-2" || item["server"] != "proxy.example.com" || item["server_port"] != 8443 || item["username"] != "a" {
		t.Fatalf("rendered = %#v", item)
	}
	tls, _ := item["tls"].(map[string]any)
	if tls["enabled"] != true || tls["server_name"] != "proxy.example.com" {
		t.Fatalf("https outbound did not default SNI to the host: %#v", tls)
	}
}

func TestHTTPExternalOutboundRequiresKernelCapability(t *testing.T) {
	external := model.ExternalOutbound{ID: 7, Protocol: model.ProtocolHTTP, TargetAddress: "203.0.113.5", TargetPort: 3128, ConfigJSON: `{}`}
	old := model.Server{ID: 1, Name: "old", AgentID: "agent", KernelCapabilities: []string{"runtime_users_v1"}}
	if _, err := externalOutboundToSingBoxWithTag(external, old, nil, "ext-7"); err == nil || !strings.Contains(err.Error(), KernelCapabilityHTTPOutbound) {
		t.Fatalf("kernel without http outbound accepted: %v", err)
	}
	if _, err := ParseHTTPExternalConfig(`{"tls":{"enabled":false,"insecure":true}}`); err == nil {
		t.Fatal("insecure without TLS accepted")
	}
	if _, err := ParseHTTPExternalConfig(`{"headers":{"Proxy-Authorization":"x"}}`); err == nil {
		t.Fatal("kernel-owned header accepted")
	}
}

// TestGeneratedConfigWithHTTPExitPassesValidation runs a full server config
// with an HTTP exit through the generated-config validator, which rejected the
// type and blocked every server's desired-state preparation.
func TestGeneratedConfigWithHTTPExitPassesValidation(t *testing.T) {
	server := model.Server{ID: 1, Name: "edge", PublicIPv4: "203.0.113.1", IPStack: model.IPStackPreferIPv4}
	root := model.Inbound{ID: 10, ServerID: 1, Name: "entry", Protocol: model.ProtocolVLESS, ListenIP: "0.0.0.0", Port: 443, ConfigJSON: `{}`, Enabled: true}
	external := model.ExternalOutbound{ID: 30, Name: "ntt", Protocol: model.ProtocolHTTP, TargetAddress: "proxy.example.com", TargetPort: 8080, ConfigJSON: `{"username":"u","password":"p","tls":{"enabled":true}}`, Enabled: true}
	path := model.ProxyPath{ID: 40, Name: "entry-via-http", InboundID: root.ID, Secret: "path-secret", Enabled: true}
	externalID := external.ID
	opts := ConfigOptions{
		Servers:           []model.Server{server},
		Inbounds:          []model.Inbound{root},
		ExternalOutbounds: []model.ExternalOutbound{external},
		ProxyPaths:        []model.ProxyPath{path},
		ProxyPathSteps:    []model.ProxyPathStep{{ID: 1, PathID: path.ID, Position: 1, NodeType: model.ProxyPathStepImported, ExternalOutboundID: &externalID}},
		InboundUsers:      []model.InboundUser{{InboundID: root.ID, UserID: 1, Enabled: true}},
	}
	config, err := generateFixtureConfig(server, []model.Inbound{root}, nil, nil, []model.User{{ID: 1, Username: "alice", Status: "active", ProxyUUID: "11111111-1111-4111-8111-111111111111", ProxyPassword: "pass-a"}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	var parsed SingBoxConfig
	if err := json.Unmarshal([]byte(config), &parsed); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGeneratedSingBoxConfig(parsed); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, outbound := range parsed.Outbounds {
		found = found || outbound["tag"] == "path-40-step-1" && outbound["type"] == "http"
	}
	if !found {
		t.Fatalf("http exit missing from generated config: %s", config)
	}
}

func TestStandaloneExternalRegionSurvivesLinkingAndInvalidatesOnConfigChange(t *testing.T) {
	external := model.ExternalOutbound{ID: 2, Protocol: model.ProtocolHTTP, TargetAddress: "1.1.1.1", TargetPort: 8080, ConfigJSON: `{"username":"a","password":"b"}`, Enabled: true, ProbeStatus: "resolved", ProbeExitIP: "8.8.8.8", ProbeRegionCode: "JP"}
	external.ProbeFingerprint = ExternalOutboundFingerprint(external)
	_, nodes := ResolveProxyPathExitRegions(nil, nil, nil, nil, []model.ExternalOutbound{external}, nil)
	if nodes[0].EffectiveRegionCode != "JP" {
		t.Fatal("unlinked node lost measured region")
	}
	inbound := model.Inbound{ID: 10, ServerID: 1, Enabled: true}
	path := model.ProxyPath{ID: 113, InboundID: 10, Enabled: true}
	step := model.ProxyPathStep{ID: 1, PathID: 113, Position: 1, NodeType: model.ProxyPathStepImported, ExternalOutboundID: &external.ID}
	paths, nodes := ResolveProxyPathExitRegions([]model.ProxyPath{path}, []model.ProxyPathStep{step}, []model.Server{{ID: 1}}, []model.Inbound{inbound}, []model.ExternalOutbound{external}, nil)
	if paths[0].EffectiveExitRegionCode != "JP" || nodes[0].EffectiveRegionCode != "JP" {
		t.Fatal("link did not reuse measured region")
	}
	secondPath, secondStep := path, step
	secondPath.ID, secondStep.PathID = 114, 114
	shared, _ := ResolveProxyPathExitRegions([]model.ProxyPath{path, secondPath}, []model.ProxyPathStep{step, secondStep}, []model.Server{{ID: 1}}, []model.Inbound{inbound}, []model.ExternalOutbound{external}, nil)
	for _, item := range shared {
		if item.EffectiveExitRegionCode != "JP" {
			t.Fatal("shared path did not reuse node result")
		}
	}
	external.Name = "renamed"
	if !ExternalOutboundProbeCurrent(external) {
		t.Fatal("rename invalidated connection probe")
	}
	external.ConfigJSON = `{"username":"a","password":"new"}`
	paths, nodes = ResolveProxyPathExitRegions([]model.ProxyPath{path}, []model.ProxyPathStep{step}, []model.Server{{ID: 1}}, []model.Inbound{inbound}, []model.ExternalOutbound{external}, nil)
	if nodes[0].RegionStatus != RegionStatusStale || nodes[0].EffectiveRegionCode != "" || paths[0].EffectiveExitRegionCode != "" {
		t.Fatal("changed credentials retained stale region")
	}
}

package core

import (
	"encoding/json"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
)

const wireGuardTestKey = "YNXtAzepDqRv9H52osJVDQnznT5AL11IUmHwhGvYPXE="

func TestWireGuardExternalConfigAcceptsSingBoxFormsAndRejectsUnknownFields(t *testing.T) {
	legacy := `{"type":"wireguard","server":"wg.example.com","server_port":51820,"local_address":["172.16.0.2","fd00::2/128"],"private_key":"` + wireGuardTestKey + `","peer_public_key":"` + wireGuardTestKey + `","reserved":"AQID","mtu":1280}`
	cfg, err := ParseWireGuardExternalConfig(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "wg.example.com" || cfg.ServerPort != 51820 || len(cfg.Address) != 2 || cfg.Address[0] != "172.16.0.2/32" || cfg.MTU != 1280 || len(cfg.Reserved) != 3 || cfg.Reserved[2] != 3 {
		t.Fatalf("legacy form = %+v", cfg)
	}
	endpoint := `{"type":"wireguard","address":["10.0.0.2/32"],"private_key":"` + wireGuardTestKey + `","peers":[{"address":"203.0.113.9","port":443,"public_key":"` + wireGuardTestKey + `","allowed_ips":["0.0.0.0/0","::/0"],"persistent_keepalive_interval":25}]}`
	normalized, err := NormalizeWireGuardExternalConfigJSON(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"address":["10.0.0.2/32"],"peer_public_key":"` + wireGuardTestKey + `","persistent_keepalive_interval":25,"private_key":"` + wireGuardTestKey + `"}`
	if normalized != want {
		t.Fatalf("normalized = %s", normalized)
	}
	for _, raw := range []string{
		`{"address":["10.0.0.2/32"],"private_key":"` + wireGuardTestKey + `","peer_public_key":"` + wireGuardTestKey + `","system":true}`,
		`{"address":["10.0.0.2/32"],"private_key":"short","peer_public_key":"` + wireGuardTestKey + `"}`,
		`{"private_key":"` + wireGuardTestKey + `","peer_public_key":"` + wireGuardTestKey + `"}`,
		`{"address":["10.0.0.2/32"],"private_key":"` + wireGuardTestKey + `","peers":[{"public_key":"` + wireGuardTestKey + `"},{"public_key":"` + wireGuardTestKey + `"}]}`,
		`{"address":["10.0.0.2/32"],"private_key":"` + wireGuardTestKey + `","peer_public_key":"` + wireGuardTestKey + `","reserved":[1,2]}`,
		`{"address":["10.0.0.2/32"],"private_key":"` + wireGuardTestKey + `","peers":[{"public_key":"` + wireGuardTestKey + `","detour":"x"}]}`,
	} {
		if _, err := ParseWireGuardExternalConfig(raw); err == nil {
			t.Fatalf("invalid WireGuard config accepted: %s", raw)
		}
	}
	if !ExternalProtocolChainOnly(model.ProtocolWireGuard) || ExternalProtocolChainOnly(model.ProtocolSocks) {
		t.Fatal("chain-only protocol set is wrong")
	}
	if ProtocolTransportCapability(model.ProtocolWireGuard).TFO != TFOUnsupported {
		t.Fatal("WireGuard must not accept TCP Fast Open")
	}
}

// TestGeneratedConfigRendersWireGuardExitAsEndpoint covers a proxy path whose
// exit is an imported WireGuard node: sing-box only knows WireGuard as an
// endpoint, so the path step must leave the outbound list.
func TestGeneratedConfigRendersWireGuardExitAsEndpoint(t *testing.T) {
	server := model.Server{ID: 1, Name: "edge", PublicIPv4: "203.0.113.1", IPStack: model.IPStackPreferIPv4}
	root := model.Inbound{ID: 10, ServerID: 1, Name: "entry", Protocol: model.ProtocolVLESS, ListenIP: "0.0.0.0", Port: 443, ConfigJSON: `{}`, Enabled: true}
	external := model.ExternalOutbound{ID: 30, Name: "wg", Protocol: model.ProtocolWireGuard, TargetAddress: "wg.example.com", TargetPort: 51820, ConfigJSON: `{"address":["172.16.0.2/32"],"private_key":"` + wireGuardTestKey + `","peer_public_key":"` + wireGuardTestKey + `","reserved":[1,2,3]}`, Enabled: true}
	path := model.ProxyPath{ID: 40, Name: "entry-via-wg", InboundID: root.ID, Secret: "path-secret", Enabled: true}
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
	for _, outbound := range parsed.Outbounds {
		if outbound["type"] == "wireguard" {
			t.Fatalf("WireGuard rendered as an outbound: %s", config)
		}
	}
	tags := map[string]bool{}
	for _, endpoint := range parsed.Endpoints {
		if endpoint["type"] != "wireguard" {
			continue
		}
		tags[stringFromAny(endpoint["tag"])] = true
		if _, present := endpoint["tcp_fast_open"]; present {
			t.Fatalf("WireGuard endpoint carries tcp_fast_open: %#v", endpoint)
		}
		peers, _ := endpoint["peers"].([]any)
		peer, _ := peers[0].(map[string]any)
		if len(peers) != 1 || peer["address"] != "wg.example.com" || peer["port"] != float64(51820) || peer["public_key"] != wireGuardTestKey {
			t.Fatalf("endpoint peer = %#v", endpoint)
		}
	}
	if !tags["path-40-step-1"] {
		t.Fatalf("WireGuard exit endpoint missing: %s", config)
	}
	probe, err := ExternalOutboundProbeConfig(external)
	if err != nil {
		t.Fatal(err)
	}
	var probeItem map[string]any
	_ = json.Unmarshal(probe, &probeItem)
	if probeItem["type"] != "wireguard" || probeItem["tag"] != "external-probe" || probeItem["server"] != nil || probeItem["domain_resolver"] != nil {
		t.Fatalf("probe config = %s", probe)
	}
}

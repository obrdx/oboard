package controller

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
)

const wireGuardImportKey = "YNXtAzepDqRv9H52osJVDQnznT5AL11IUmHwhGvYPXE="
const wireGuardImportPeer = "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo="

func validatedWireGuardImport(t *testing.T, content string) model.ExternalOutbound {
	t.Helper()
	items, err := parseExternalOutboundImport(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Protocol != model.ProtocolWireGuard {
		t.Fatalf("items = %+v", items)
	}
	item := items[0]
	if err := (&Server{}).validateExternalOutbound(t.Context(), &item); err != nil {
		t.Fatal(err)
	}
	return item
}

func TestImportWireGuardINIConfig(t *testing.T) {
	item := validatedWireGuardImport(t, `
# exported by a provider
[Interface]
PrivateKey = `+wireGuardImportKey+`
Address = 172.16.0.2/32, 2606:4700:110:8a36::2/128
DNS = 1.1.1.1
MTU = 1280
PostUp = iptables -A FORWARD

[Peer]
PublicKey = `+wireGuardImportPeer+`
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = [2606:4700:d0::a29f:c001]:2408
PersistentKeepalive = 25
`)
	if item.TargetAddress != "2606:4700:d0::a29f:c001" || item.TargetPort != 2408 || item.Name != "WireGuard 2606:4700:d0::a29f:c001" {
		t.Fatalf("ini import = %+v", item)
	}
	var cfg map[string]any
	_ = json.Unmarshal([]byte(item.ConfigJSON), &cfg)
	if cfg["peer_public_key"] != wireGuardImportPeer || cfg["mtu"] != float64(1280) || cfg["persistent_keepalive_interval"] != float64(25) || cfg["server"] != nil || cfg["allowed_ips"] != nil {
		t.Fatalf("stored config = %s", item.ConfigJSON)
	}
	if addresses, _ := cfg["address"].([]any); len(addresses) != 2 {
		t.Fatalf("stored addresses = %s", item.ConfigJSON)
	}
}

func TestImportWireGuardShareURIKeepsUnescapedBase64Keys(t *testing.T) {
	item := validatedWireGuardImport(t, "wireguard://"+wireGuardImportKey+"@wg.example.com:51820?publickey="+wireGuardImportPeer+"&address=10.0.0.2/32,fd00::2/128&reserved=1,2,3&mtu=1420#%E9%A6%99%E6%B8%AF%20WG")
	if item.Name != "香港 WG" || item.TargetAddress != "wg.example.com" || item.TargetPort != 51820 || item.ExposeToUsers {
		t.Fatalf("uri import = %+v", item)
	}
	if !strings.Contains(item.ConfigJSON, `"peer_public_key":"`+wireGuardImportPeer+`"`) || !strings.Contains(item.ConfigJSON, `"reserved":[1,2,3]`) || !strings.Contains(item.ConfigJSON, `"mtu":1420`) {
		t.Fatalf("stored config = %s", item.ConfigJSON)
	}
	escaped := validatedWireGuardImport(t, "wg://"+strings.NewReplacer("+", "%2B", "/", "%2F", "=", "%3D").Replace(wireGuardImportKey)+"@203.0.113.7:443?public-key="+strings.NewReplacer("+", "%2B", "/", "%2F").Replace(wireGuardImportPeer)+"&ip=10.0.0.2")
	if !strings.Contains(escaped.ConfigJSON, `"private_key":"`+wireGuardImportKey+`"`) || !strings.Contains(escaped.ConfigJSON, `"address":["10.0.0.2/32"]`) {
		t.Fatalf("escaped uri config = %s", escaped.ConfigJSON)
	}
}

func TestImportWireGuardRejectsIncompleteConfigsAndSubscriptionExposure(t *testing.T) {
	for _, content := range []string{
		"[Interface]\nPrivateKey = " + wireGuardImportKey + "\nAddress = 10.0.0.2/32\n",
		"[Interface]\nPrivateKey = " + wireGuardImportKey + "\nAddress = 10.0.0.2/32\n[Peer]\nPublicKey = " + wireGuardImportPeer + "\nEndpoint = 1.2.3.4:1\n[Peer]\nPublicKey = " + wireGuardImportPeer + "\nEndpoint = 1.2.3.5:1\n",
	} {
		if _, err := parseExternalOutboundImport(content); err == nil {
			t.Fatalf("incomplete WireGuard config imported: %q", content)
		}
	}
	items, err := parseExternalOutboundImport("wireguard://" + wireGuardImportKey + "@wg.example.com:51820?address=10.0.0.2/32")
	if err == nil {
		item := items[0]
		if err := (&Server{}).validateExternalOutbound(t.Context(), &item); err == nil {
			t.Fatal("WireGuard URI without peer public key accepted")
		}
	}
	item := model.ExternalOutbound{Name: "wg", Protocol: model.ProtocolWireGuard, TargetAddress: "wg.example.com", TargetPort: 51820, ConfigJSON: `{"address":["10.0.0.2/32"],"private_key":"` + wireGuardImportKey + `","peer_public_key":"` + wireGuardImportPeer + `"}`, ExposeToUsers: true}
	if err := (&Server{}).validateExternalOutbound(t.Context(), &item); err == nil || !strings.Contains(err.Error(), "订阅") {
		t.Fatalf("exposed WireGuard node accepted: %v", err)
	}
}

func TestWireGuardAgentCheckRequiresKernelCapability(t *testing.T) {
	server := model.Server{ID: 1, Name: "edge", AgentID: "agent", Status: model.ServerOnline, KernelCapabilities: []string{"external_outbound_check_v1", "external_outbound_probe_v1"}}
	if reason := agentTaskImmediateFailure(&server); reason != "" {
		t.Skipf("fixture server is not task-ready: %s", reason)
	}
	if err := externalOutboundCheckServerReady(server, model.ExternalOutbound{Protocol: model.ProtocolSocks}); err != nil {
		t.Fatal(err)
	}
	if err := externalOutboundCheckServerReady(server, model.ExternalOutbound{Protocol: model.ProtocolWireGuard}); err == nil {
		t.Fatal("kernel without WireGuard probe support selected")
	}
}

package controller

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
)

func TestParseExternalOutboundImportSocksBase64ShareLinks(t *testing.T) {
	whole := base64.RawURLEncoding.EncodeToString([]byte("carol:example-password@203.0.113.7:1080"))
	userinfo := base64.StdEncoding.EncodeToString([]byte("bob:p@ss:w"))
	content := "socks://" + whole + "?udp=1&method=auto&dialer-proxy=%F0%9F%87%AF%F0%9F%87%B5%20Fdr%EF%BD%9CCogent\n" +
		"socks5://" + userinfo + "@[2001:db8::1]:1081#Edge\n" +
		"socks5://alice:secret@socks.example.com:1082\n" +
		"socks5h://dave:topsecret@remote-dns.example.com:1083#RemoteDNS\n" +
		"socks5h://" + userinfo + "@203.0.113.9:1084#Encoded5h"
	items, err := parseExternalOutboundImport(content)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		host           string
		port           int
		user, password string
	}{
		{"203.0.113.7", 1080, "carol", "example-password"},
		{"2001:db8::1", 1081, "bob", "p@ss:w"},
		{"socks.example.com", 1082, "alice", "secret"},
		{"remote-dns.example.com", 1083, "dave", "topsecret"},
		{"203.0.113.9", 1084, "bob", "p@ss:w"},
	}
	if len(items) != len(want) {
		t.Fatalf("got %d items", len(items))
	}
	for i, w := range want {
		item := items[i]
		var cfg map[string]any
		if err := json.Unmarshal([]byte(item.ConfigJSON), &cfg); err != nil {
			t.Fatal(err)
		}
		if item.Protocol != model.ProtocolSocks || item.TargetAddress != w.host || item.TargetPort != w.port || cfg["username"] != w.user || cfg["password"] != w.password || cfg["version"] != "5" || cfg["remote_dns"] != true {
			t.Fatalf("item %d: %#v %#v", i, item, cfg)
		}
		if _, ok := cfg["detour"]; ok {
			t.Fatalf("item %d must not carry dialer-proxy as detour: %#v", i, cfg)
		}
	}
	if items[1].Name != "Edge" || items[3].Name != "RemoteDNS" || items[4].Name != "Encoded5h" {
		t.Fatalf("names = %q, %q, %q", items[1].Name, items[3].Name, items[4].Name)
	}
}

func TestValidateSocksExternalConfigAndCheckCapability(t *testing.T) {
	if err := validateSocksExternalConfig(`{"version":"5","remote_dns":false}`); err != nil {
		t.Fatalf("local dns socks5 should be valid: %v", err)
	}
	if err := validateSocksExternalConfig(`{"version":"4","remote_dns":true}`); err == nil {
		t.Fatal("socks4 with remote_dns=true must fail validation")
	}
	baseServer := model.Server{
		ID: 1, Name: "Edge-1", Status: model.ServerOnline, AgentID: "agent-1",
		KernelCapabilities: []string{"external_outbound_check_v1", "external_outbound_probe_v1"},
	}
	localNode := model.ExternalOutbound{Protocol: model.ProtocolSocks, ConfigJSON: `{"version":"5","remote_dns":false}`}
	remoteNode := model.ExternalOutbound{Protocol: model.ProtocolSocks, ConfigJSON: `{"version":"5","remote_dns":true}`}
	if err := externalOutboundCheckServerReady(baseServer, remoteNode); err != nil {
		t.Fatalf("default socks5h check should work without socks5_dns_mode_v1: %v", err)
	}
	if err := externalOutboundCheckServerReady(baseServer, localNode); err == nil {
		t.Fatal("local-dns socks5 check must require socks5_dns_mode_v1")
	}
	baseServer.KernelCapabilities = append(baseServer.KernelCapabilities, core.KernelCapabilitySocks5DNSMode)
	if err := externalOutboundCheckServerReady(baseServer, localNode); err != nil {
		t.Fatalf("capable server should check local-dns socks5: %v", err)
	}
}

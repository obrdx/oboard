package controller

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
)

func TestParseHTTPImportURIs(t *testing.T) {
	items, err := parseExternalOutboundImport("http://alice:secret@proxy.example.com:8080#Office\nhttps://bob:pw@secure.example.com?sni=edge.example.com&insecure=1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	plain, secure := items[0], items[1]
	if plain.Protocol != model.ProtocolHTTP || plain.Name != "Office" || plain.TargetAddress != "proxy.example.com" || plain.TargetPort != 8080 {
		t.Fatalf("plain http = %+v", plain)
	}
	if secure.Protocol != model.ProtocolHTTP || secure.TargetPort != 443 || secure.Name != "HTTPS secure.example.com" {
		t.Fatalf("https = %+v", secure)
	}
	normalized, err := core.NormalizeHTTPExternalConfigJSON(secure.ConfigJSON)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	_ = json.Unmarshal([]byte(normalized), &cfg)
	tls, _ := cfg["tls"].(map[string]any)
	if cfg["username"] != "bob" || cfg["password"] != "pw" || tls["server_name"] != "edge.example.com" || tls["insecure"] != true {
		t.Fatalf("normalized https config = %s", normalized)
	}
	if _, present := cfg["server"]; present {
		t.Fatalf("normalized config keeps dial target: %s", normalized)
	}
}

func TestParseHTTPImportRejectsWebAddresses(t *testing.T) {
	for _, line := range []string{
		"https://sub.example.com/api/v1/client/subscribe?token=abc",
		"http://example.com/index.html",
		"https://example.com?token=abc",
	} {
		if _, err := parseExternalOutboundImport(line); err == nil {
			t.Fatalf("%s was imported as an HTTP proxy", line)
		}
	}
}

func TestValidateHTTPExternalOutboundRejectsSubscriptionExposure(t *testing.T) {
	s := &Server{}
	item := model.ExternalOutbound{Name: "http", Protocol: model.ProtocolHTTP, TargetAddress: "proxy.example.com", TargetPort: 8080, ConfigJSON: `{"username":"a","password":"b"}`, ExposeToUsers: true}
	if err := s.validateExternalOutbound(t.Context(), &item); err == nil || !strings.Contains(err.Error(), "订阅") {
		t.Fatalf("exposed HTTP node accepted: %v", err)
	}
	item.ExposeToUsers = false
	item.ConfigJSON = `{"username":"a","password":"b","udp_over_tcp":true}`
	if err := s.validateExternalOutbound(t.Context(), &item); err == nil {
		t.Fatal("unknown HTTP field accepted")
	}
	item.ConfigJSON = `{"type":"http","server":"proxy.example.com","server_port":8080,"username":"a","password":"b","tls":{"enabled":true}}`
	if err := s.validateExternalOutbound(t.Context(), &item); err != nil {
		t.Fatal(err)
	}
	if item.ConfigJSON != `{"password":"b","tls":{"enabled":true},"username":"a"}` {
		t.Fatalf("stored config = %s", item.ConfigJSON)
	}
}

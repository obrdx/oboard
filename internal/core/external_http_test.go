package core

import (
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

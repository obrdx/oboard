package core

import (
	"encoding/json"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
)

func TestPhysicalDialResolverSingleStackAndDetours(t *testing.T) {
	for _, tc := range []struct {
		name    string
		server  model.Server
		binding map[string]any
		want    string
	}{
		{"auto IPv4", model.Server{PublicIPv4: "192.0.2.1"}, nil, "ipv4_only"},
		{"auto IPv6", model.Server{PublicIPv6: "2001:db8::1"}, nil, "ipv6_only"},
		{"dual", model.Server{PublicIPv4: "192.0.2.1", PublicIPv6: "2001:db8::1"}, nil, "prefer_ipv6"},
		{"unknown", model.Server{}, nil, "prefer_ipv6"},
		{"remote dial", model.Server{PublicIPv4: "192.0.2.1"}, map[string]any{"detour": "proxy"}, "prefer_ipv6"},
		{"tunnel interface", model.Server{PublicIPv4: "192.0.2.1"}, map[string]any{"bind_interface": "he-ipv6"}, "prefer_ipv6"},
		{"source address", model.Server{PublicIPv4: "192.0.2.1"}, map[string]any{"inet6_bind_address": "2001:db8::2"}, "prefer_ipv6"},
		{"source prefix", model.Server{PublicIPv4: "192.0.2.1"}, map[string]any{"type": "source-prefix"}, "prefer_ipv6"},
		{"family selector", model.Server{PublicIPv4: "192.0.2.1"}, map[string]any{"type": "family-selector"}, "prefer_ipv6"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := map[string]any{"server": primaryBootstrapDNSTag, "strategy": "prefer_ipv6", "disable_cache": true}
			item := map[string]any{"domain_resolver": original}
			for k, v := range tc.binding {
				item[k] = v
			}
			applyPhysicalDialResolver(item, tc.server, bootstrapDNSGroupTag, "")
			got := item["domain_resolver"].(map[string]any)
			if got["strategy"] != tc.want || got["server"] != bootstrapDNSGroupTag || got["disable_cache"] != true {
				t.Fatalf("resolver = %#v", got)
			}
			if original["strategy"] != "prefer_ipv6" || original["server"] != primaryBootstrapDNSTag {
				t.Fatal("mutated shared resolver")
			}
		})
	}
}

func TestGeneratedSingleStackPhysicalDNS(t *testing.T) {
	server := model.Server{ID: 1, PublicIPv4: "192.0.2.1"}
	raw, err := GenerateServerConfig(server, nil, []model.Outbound{{ID: 1, ServerID: 1, Protocol: model.ProtocolAnyTLS, TargetAddress: "relay.example.com", TargetPort: 443, ConfigJSON: `{"password":"fixture","tls":{"enabled":true,"server_name":"relay.example.com"}}`, Enabled: true}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var config SingBoxConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatal(err)
	}
	for _, item := range config.Outbounds {
		if item["type"] != "direct" && item["type"] != "anytls" {
			continue
		}
		resolver := item["domain_resolver"].(map[string]any)
		if resolver["strategy"] != "ipv4_only" {
			t.Fatalf("outbound = %#v", item)
		}
		if item["type"] == "anytls" && resolver["server"] != bootstrapDNSGroupTag {
			t.Fatalf("next-hop resolver = %#v", resolver)
		}
	}
	if config.DNS["strategy"] != "prefer_ipv4" {
		t.Fatal("business DNS lost dual-stack answers")
	}
	for _, item := range dnsServerItems(config.DNS["servers"]) {
		if item["domain_resolver"] == nil {
			continue
		}
		resolver := item["domain_resolver"].(map[string]any)
		if resolver["server"] != bootstrapDNSGroupTag || resolver["strategy"] != "ipv4_only" {
			t.Fatalf("DNS transport resolver = %#v", resolver)
		}
	}
	if err := ValidateGeneratedSingBoxConfig(config); err != nil {
		t.Fatal(err)
	}
}

func TestManagedDualStackDomainFromIPv4Source(t *testing.T) {
	source := model.Server{ID: 1, PublicIPv4: "192.0.2.1"}
	target := model.Server{ID: 2, PublicIPv4: "192.0.2.2", PublicIPv6: "2001:db8::2"}
	inbound := model.Inbound{ID: 21, ServerID: 2, Protocol: model.ProtocolAnyTLS, Port: 11777, DNSSyncEnabled: true, DNSDomain: "relay.example.com", ConfigJSON: `{"tls":{"enabled":true,"server_name":"certificate.example.com"}}`, Enabled: true}
	item, err := proxyPathStepOutbound(model.ProxyPath{ID: 1, Secret: "fixture"}, model.ProxyPathStep{NodeType: model.ProxyPathStepServerInbound, InboundID: &inbound.ID}, source, map[int64]model.Server{2: target}, map[int64]model.Inbound{21: inbound}, nil, nil, "path-1-step-1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dns, err := BuildDNSConfig(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	config := SingBoxConfig{DNS: dns, Outbounds: []map[string]any{item}}
	applyPhysicalDialResolvers(&config, source)
	resolver := item["domain_resolver"].(map[string]any)
	if resolver["strategy"] != "ipv4_only" || resolver["server"] != bootstrapDNSGroupTag {
		t.Fatalf("managed next-hop resolver = %#v", resolver)
	}
	if item["server"] != inbound.DNSDomain || item["tls"].(map[string]any)["server_name"] != "certificate.example.com" {
		t.Fatal("managed DNS or TLS identity changed")
	}
}

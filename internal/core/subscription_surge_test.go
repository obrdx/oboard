package core

import (
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
)

func TestSurgeNativeVLESSTransports(t *testing.T) {
	tests := []struct {
		name       string
		transport  map[string]any
		tls        map[string]any
		flow       string
		encryption string
		want       []string
	}{
		{name: "RAW"},
		{name: "TLS Vision", tls: map[string]any{"enabled": true, "server_name": "edge.example.com", "insecure": true, "alpn": []string{"h2", "http/1.1"}}, flow: "xtls-rprx-vision", want: []string{"tls=true", `sni="edge.example.com"`, "skip-cert-verify=true", `alpn="h2,http/1.1"`, "flow=xtls-rprx-vision"}},
		{name: "REALITY Vision", tls: map[string]any{"enabled": true, "server_name": "edge.example.com", "insecure": true, "reality": map[string]any{"public_key": "public-key", "short_id": "abcd"}}, flow: "xtls-rprx-vision-udp443", want: []string{`reality-public-key="public-key"`, `reality-short-id="abcd"`, `sni="edge.example.com"`, "flow=xtls-rprx-vision-udp443"}},
		{name: "WebSocket", transport: map[string]any{"type": "ws", "path": "/vless?ed=2048", "headers": map[string]any{"Host": "cdn.example.com"}}, want: []string{"ws=true", `ws-path="/vless?ed=2048"`, `ws-headers="Host:cdn.example.com"`}},
		{name: "HTTPUpgrade", transport: map[string]any{"type": "httpupgrade", "path": "/up", "host": "cdn.example.com"}, tls: map[string]any{"enabled": true}, want: []string{"tls=true", "ws=true", "ws-http-upgrade=true", `ws-path="/up"`, `ws-headers="Host:cdn.example.com"`}},
		{name: "gRPC TLS", transport: map[string]any{"type": "grpc", "service_name": "/my/path/tun", "mode": "multi", "authority": "cdn.example.com"}, tls: map[string]any{"enabled": true}, want: []string{"tls=true", "grpc=true", `grpc-service-name="/my/path/tun"`, `grpc-mode="multi"`, `grpc-authority="cdn.example.com"`}},
		{name: "gRPC REALITY", transport: map[string]any{"type": "grpc", "service_name": "edge"}, tls: map[string]any{"reality": map[string]any{"public_key": "public-key"}}, want: []string{"grpc=true", `reality-public-key="public-key"`}},
		{name: "XHTTP", transport: map[string]any{"type": "xhttp", "path": "/xhttp", "host": "cdn.example.com", "mode": "packet-up"}, want: []string{"xhttp=true", `xhttp-path="/xhttp"`, `xhttp-host="cdn.example.com"`, `xhttp-mode="packet-up"`}},
		{name: "Encryption Vision", encryption: "mlkem768x25519plus.native.0rtt.test-key", flow: "xtls-rprx-vision", want: []string{`encryption="mlkem768x25519plus.native.0rtt.test-key"`, "flow=xtls-rprx-vision"}},
		{name: "Encryption WebSocket Vision", transport: map[string]any{"type": "ws"}, encryption: "mlkem768x25519plus.native.0rtt.test-key", flow: "xtls-rprx-vision", want: []string{"ws=true", `encryption="mlkem768x25519plus.native.0rtt.test-key"`}},
		{name: "Encryption XHTTP Vision", transport: map[string]any{"type": "xhttp"}, encryption: "mlkem768x25519plus.native.0rtt.test-key", flow: "xtls-rprx-vision", want: []string{"xhttp=true", `encryption="mlkem768x25519plus.native.0rtt.test-key"`}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := SubscriptionNode{Name: "VLESS", Group: "g", Raw: map[string]any{
				"type": "vless", "server": "2001:db8::10", "server_port": 443,
				"uuid": "11111111-1111-4111-8111-111111111111", "flow": test.flow,
				"encryption": test.encryption,
				"transport":  test.transport, "tls": test.tls, "tcp_fast_open": true,
			}}
			for _, format := range []model.SubscriptionFormat{model.SubscriptionFormatSurge, model.SubscriptionFormatSurgeMac} {
				for _, mode := range []SurgeMacMihomoMode{SurgeMacMihomoAuto, SurgeMacMihomoAlways, SurgeMacMihomoOff} {
					preview, err := PreviewSubscriptionNodesWithOptions([]SubscriptionNode{node}, format, SubscriptionRenderOptions{SurgeMac: SurgeMacOptions{Mode: mode}})
					if err != nil {
						t.Fatal(err)
					}
					if len(preview.Nodes) != 1 || preview.FilteredCount != 0 {
						t.Fatalf("%s/%s filtered native VLESS: %#v", format, mode, preview)
					}
					for _, want := range append([]string{`VLESS=vless,[2001:db8::10],443,username="11111111-1111-4111-8111-111111111111"`, "tfo=true"}, test.want...) {
						if !strings.Contains(preview.Content, want) {
							t.Fatalf("%s/%s missing %s:\n%s", format, mode, want, preview.Content)
						}
					}
					for _, unwanted := range []string{"=external,", "=socks5,", "fingerprint=chrome", "utls"} {
						if strings.Contains(preview.Content, unwanted) {
							t.Fatalf("unexpected %s:\n%s", unwanted, preview.Content)
						}
					}
					if test.tls != nil && test.tls["reality"] != nil && strings.Contains(preview.Content, "skip-cert-verify=") {
						t.Fatalf("REALITY must use handshake verification:\n%s", preview.Content)
					}
				}
			}
		})
	}
}

func TestSurgeVLESSFiltersUnsupportedCombinationsWithoutBridge(t *testing.T) {
	for _, test := range []struct {
		network string
		secure  bool
		reality bool
		flow    string
	}{
		{network: "kcp"}, {network: "quic"}, {network: "http"},
		{network: "grpc"},
		{network: "ws", reality: true}, {network: "httpupgrade", reality: true},
		{network: "tcp", flow: "xtls-rprx-vision"},
		{network: "ws", secure: true, flow: "xtls-rprx-vision"},
		{network: "grpc", secure: true, flow: "xtls-rprx-vision"},
		{network: "xhttp", secure: true, flow: "xtls-rprx-vision"},
		{network: "tcp", secure: true, flow: "unknown"},
	} {
		proxy := subscriptionProxy{Type: "vless", Network: test.network, Flow: test.flow, TLS: subscriptionTLS{Enabled: test.secure}}
		if test.reality {
			proxy.TLS.RealityPublicKey = "public-key"
		}
		for _, format := range []model.SubscriptionFormat{model.SubscriptionFormatSurge, model.SubscriptionFormatSurgeMac, model.SubscriptionFormatSurfboard} {
			for _, mode := range []SurgeMacMihomoMode{SurgeMacMihomoAuto, SurgeMacMihomoAlways, SurgeMacMihomoOff} {
				capability := subscriptionCapability(format, proxy, SubscriptionRenderOptions{SurgeMac: SurgeMacOptions{Mode: mode}})
				if capability.Supported || capability.Route != SubscriptionRouteUnsupported {
					t.Fatalf("%s/%s accepted unsupported VLESS %+v: %+v", format, mode, test, capability)
				}
			}
		}
	}
}

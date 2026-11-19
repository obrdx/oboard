package controller

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/store"
)

func TestTrafficForwardingPageCombinesForwardsAndTunnels(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "oboard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := newTestServer(db, "test-secret", "").Handler()
	request(t, h, http.MethodPost, "/api/v1/ui/auth/bootstrap", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusCreated)
	login := request(t, h, http.MethodPost, "/api/v1/ui/auth/login", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusOK)
	token := login["token"].(string)

	first := request(t, h, http.MethodPost, "/api/v1/ui/servers", token, map[string]any{"name": "s1", "public_ipv4": "203.0.113.10", "listen_ip": "0.0.0.0", "port_range_start": 10000, "port_range_end": 10010, "renewal_cycle": "monthly"}, http.StatusCreated)
	firstID := int64(first["server"].(map[string]any)["id"].(float64))
	second := request(t, h, http.MethodPost, "/api/v1/ui/servers", token, map[string]any{"name": "s2", "entry_ip_mode": "custom", "entry_address": "203.0.113.2", "listen_ip": "0.0.0.0", "port_range_start": 20000, "port_range_end": 20010, "renewal_cycle": "monthly"}, http.StatusCreated)
	secondID := int64(second["server"].(map[string]any)["id"].(float64))
	request(t, h, http.MethodPost, "/api/v1/ui/port-forwards", token, map[string]any{"name": "s1-to-s2", "source_server_id": firstID, "target_server_id": secondID, "listen_ip": "0.0.0.0", "listen_port": 443, "target_port": 8443, "protocol": "tcp", "backend": "auto", "priority": 10, "config_json": "{}"}, http.StatusCreated)
	request(t, h, http.MethodPost, "/api/v1/ui/tunnels", token, map[string]any{"name": "ssh-s1-to-s2", "source_server_id": firstID, "target_server_id": secondID, "type": "ssh", "priority": 20, "config_json": `{"user":"root"}`}, http.StatusCreated)

	page := request(t, h, http.MethodGet, "/api/v1/ui/page-data?page=traffic-forwarding", token, nil, http.StatusOK)
	for _, key := range []string{"servers", "port_forwards", "port_forward_probes", "tunnels"} {
		if _, ok := page[key]; !ok {
			t.Fatalf("traffic-forwarding page is missing %s: %#v", key, page)
		}
	}
	if len(page["port_forwards"].([]any)) != 1 || len(page["tunnels"].([]any)) != 1 {
		t.Fatalf("traffic-forwarding page lists forwards=%#v tunnels=%#v", page["port_forwards"], page["tunnels"])
	}
}

func TestTunnelUpdateRestoresRedactedSecrets(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "oboard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := newTestServer(db, "test-secret", "").Handler()
	request(t, h, http.MethodPost, "/api/v1/ui/auth/bootstrap", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusCreated)
	login := request(t, h, http.MethodPost, "/api/v1/ui/auth/login", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusOK)
	token := login["token"].(string)
	first := request(t, h, http.MethodPost, "/api/v1/ui/servers", token, map[string]any{"name": "s1", "public_ipv4": "203.0.113.10", "listen_ip": "0.0.0.0", "port_range_start": 10000, "port_range_end": 10010, "renewal_cycle": "monthly"}, http.StatusCreated)
	firstID := int64(first["server"].(map[string]any)["id"].(float64))
	second := request(t, h, http.MethodPost, "/api/v1/ui/servers", token, map[string]any{"name": "s2", "public_ipv4": "203.0.113.20", "listen_ip": "0.0.0.0", "port_range_start": 20000, "port_range_end": 20010, "renewal_cycle": "monthly"}, http.StatusCreated)
	secondID := int64(second["server"].(map[string]any)["id"].(float64))
	privateKey := "YH4mA6h2nqQ0gJ0b9j2nM0Zl6kZl2T1h3fM4m6Q8n0Y="
	peerKey := "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg="
	created := request(t, h, http.MethodPost, "/api/v1/ui/tunnels", token, map[string]any{"name": "wg", "source_server_id": firstID, "target_server_id": secondID, "type": "wireguard", "priority": 100, "config_json": `{"private_key":"` + privateKey + `","peer_public_key":"` + peerKey + `","allowed_ips":[],"persistent_keepalive":25}`}, http.StatusCreated)
	public := created["tunnel"].(map[string]any)
	var publicConfig map[string]any
	if err := json.Unmarshal([]byte(public["config_json"].(string)), &publicConfig); err != nil || publicConfig["private_key"] != "<redacted>" {
		t.Fatalf("tunnel secrets were not redacted: %#v", public)
	}
	tunnelID := int64(public["id"].(float64))
	public["enabled"] = false
	request(t, h, http.MethodPatch, "/api/v1/ui/tunnels/"+itoa(tunnelID), token, public, http.StatusOK)
	stored, err := db.GetTunnel(context.Background(), tunnelID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Enabled || !strings.Contains(stored.ConfigJSON, privateKey) || !strings.Contains(stored.ConfigJSON, peerKey) || strings.Contains(stored.ConfigJSON, "<redacted>") {
		t.Fatalf("redacted update did not keep stored secrets: enabled=%v config=%s", stored.Enabled, stored.ConfigJSON)
	}
}

func TestCreateTunnelGeneratesWireGuardKeysForBothAgents(t *testing.T) {
	db := openControllerAutomationTestStore(t)
	ctx := context.Background()
	servers := []model.Server{
		{Name: "source", PublicIPv4: "203.0.113.10", ListenIP: "0.0.0.0", PortRangeStart: 10000, PortRangeEnd: 10010},
		{Name: "target", PublicIPv4: "203.0.113.20", ListenIP: "0.0.0.0", PortRangeStart: 20000, PortRangeEnd: 20010},
	}
	for i := range servers {
		if err := db.CreateServer(ctx, &servers[i]); err != nil {
			t.Fatal(err)
		}
	}
	h := newTestServer(db, "test-secret", "").Handler()
	request(t, h, http.MethodPost, "/api/v1/ui/auth/bootstrap", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusCreated)
	login := request(t, h, http.MethodPost, "/api/v1/ui/auth/login", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusOK)
	token := login["token"].(string)
	body := map[string]any{
		"name": "managed", "type": "wireguard", "source_server_id": servers[0].ID, "target_server_id": servers[1].ID,
		"local_address": "fd00:1234::1/64", "peer_address": "fd00:1234::2/64", "generate_keys": true,
	}
	request(t, h, http.MethodPost, "/api/v1/ui/tunnels", token, body, http.StatusBadRequest)
	body["target_port"] = 20001
	body["config_json"] = `{"private_key":"manual"}`
	request(t, h, http.MethodPost, "/api/v1/ui/tunnels", token, body, http.StatusBadRequest)
	delete(body, "config_json")
	created := request(t, h, http.MethodPost, "/api/v1/ui/tunnels", token, body, http.StatusCreated)
	public := created["tunnel"].(map[string]any)
	var config map[string]any
	if err := json.Unmarshal([]byte(public["config_json"].(string)), &config); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"source_private_key", "source_public_key", "target_private_key", "target_public_key"} {
		if config[key] != "<redacted>" {
			t.Fatalf("%s was not redacted", key)
		}
	}
	id := int64(public["id"].(float64))
	stored, err := db.GetTunnel(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	sourcePlan, err := core.BuildTunnelPlan(1, servers[0], servers, []model.Tunnel{*stored})
	if err != nil {
		t.Fatal(err)
	}
	targetPlan, err := core.BuildTunnelPlan(1, servers[1], servers, []model.Tunnel{*stored})
	if err != nil {
		t.Fatal(err)
	}
	if len(sourcePlan.Tunnels) != 1 || len(targetPlan.Tunnels) != 1 {
		t.Fatal("expected a projected tunnel for each server")
	}
	source, target := sourcePlan.Tunnels[0], targetPlan.Tunnels[0]
	if source.TargetEndpoint != servers[1].PublicIPv4 || source.TargetPort != 20001 || source.PeerAddress != "fd00:1234::2/128" ||
		target.ListenPort != 20001 || target.TargetEndpoint != "" || target.TargetPort != 0 || target.PeerAddress != "fd00:1234::1/128" {
		t.Fatal("the two projected endpoints do not match")
	}
	type agentKeys struct {
		Private string `json:"private_key"`
		Peer    string `json:"peer_public_key"`
	}
	decodeKeys := func(tunnel model.Tunnel) (key agentKeys, public string) {
		t.Helper()
		if err := json.Unmarshal([]byte(tunnel.ConfigJSON), &key); err != nil {
			t.Fatal(err)
		}
		raw, err := base64.StdEncoding.DecodeString(key.Private)
		if err != nil {
			t.Fatal("invalid private key encoding")
		}
		private, err := ecdh.X25519().NewPrivateKey(raw)
		if err != nil {
			t.Fatal("invalid X25519 private key")
		}
		return key, base64.StdEncoding.EncodeToString(private.PublicKey().Bytes())
	}
	sourceKeys, sourcePublic := decodeKeys(source)
	targetKeys, targetPublic := decodeKeys(target)
	if sourceKeys.Private == targetKeys.Private || sourceKeys.Peer != targetPublic || targetKeys.Peer != sourcePublic {
		t.Fatal("Agent plans must send distinct private keys and the opposite public key")
	}
	public["name"] = "renamed"
	request(t, h, http.MethodPatch, "/api/v1/ui/tunnels/"+itoa(id), token, public, http.StatusOK)
	updated, err := db.GetTunnel(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var beforeKeys, afterKeys map[string]any
	if json.Unmarshal([]byte(stored.ConfigJSON), &beforeKeys) != nil || json.Unmarshal([]byte(updated.ConfigJSON), &afterKeys) != nil {
		t.Fatal("invalid stored managed configuration")
	}
	for _, key := range []string{"source_private_key", "source_public_key", "target_private_key", "target_public_key"} {
		if beforeKeys[key] != afterKeys[key] {
			t.Fatalf("updating the tunnel changed %s", key)
		}
	}
}

package controller

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

func TestServerDefaultEntryDDNS(t *testing.T) {
	db := openControllerAutomationTestStore(t)
	ctx := context.Background()
	server := newTestServer(db, "test-secret", "")
	handler := server.Handler()
	request(t, handler, http.MethodPost, "/api/v1/ui/auth/bootstrap", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusCreated)
	token := request(t, handler, http.MethodPost, "/api/v1/ui/auth/login", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusOK)["token"].(string)

	node := &model.Server{Name: "dual-edge", PublicIPv4: "203.0.113.10", PublicIPv6: "2001:db8::10", ListenIP: "0.0.0.0", PortRangeStart: 10000, PortRangeEnd: 11000, Status: model.ServerOnline}
	other := &model.Server{Name: "other-edge", PublicIPv4: "203.0.113.20", ListenIP: "0.0.0.0", PortRangeStart: 10000, PortRangeEnd: 11000, Status: model.ServerOnline}
	for _, item := range []*model.Server{node, other} {
		if err := db.CreateServer(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	verified := time.Now()
	credential := &model.DNSCredential{Name: "cf", Provider: model.DNSProviderCloudflare, ZoneName: "example.com", Enabled: true, VerifiedAt: &verified, ConfigEncrypted: "enc", Zones: []model.DNSCredentialZone{{ZoneName: "example.com", ProviderZoneID: "zone"}}}
	if err := db.CreateDNSCredential(ctx, credential); err != nil {
		t.Fatal(err)
	}
	record := model.ProbeDDNSRecord{ServerID: node.ID, Name: "dual", DNSCredentialID: &credential.ID, Domain: "edge.example.com", RecordTypes: "both", IntervalSeconds: 300, Enabled: true}
	if err := db.SaveProbeDDNS(ctx, *node, &record); err != nil {
		t.Fatal(err)
	}
	foreign := model.ProbeDDNSRecord{ServerID: other.ID, Name: "other", DNSCredentialID: &credential.ID, Domain: "other.example.com", RecordTypes: "a", IntervalSeconds: 300, Enabled: true}
	if err := db.SaveProbeDDNS(ctx, *other, &foreign); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/ui/servers/%d", node.ID)

	request(t, handler, http.MethodPatch, path, token, map[string]any{"entry_ip_mode": "ddns"}, http.StatusBadRequest)
	request(t, handler, http.MethodPatch, path, token, map[string]any{"entry_ip_mode": "ddns", "entry_ddns_record_id": foreign.ID}, http.StatusBadRequest)
	request(t, handler, http.MethodPatch, path, token, map[string]any{"entry_ip_mode": "ddns", "entry_ddns_record_id": record.ID}, http.StatusOK)
	saved, err := db.GetServer(ctx, node.ID)
	if err != nil || saved.EntryIPMode != model.EntryIPModeDDNS || saved.EntryDDNSDomain != "edge.example.com" {
		t.Fatalf("saved = %+v err=%v", saved, err)
	}

	// A TLS inbound that follows the server inherits the DDNS domain as SNI.
	inbound := request(t, handler, http.MethodPost, "/api/v1/ui/inbounds", token, map[string]any{
		"server_id": node.ID, "name": "AnyTLS", "kind": "anytls-basic", "listen_ip": "0.0.0.0", "port": 10443,
		"entry_ip_mode": "auto", "certificate_mode": "auto", "enabled": true,
	}, http.StatusCreated)["inbound"].(map[string]any)
	if inbound["certificate_domain"] != "edge.example.com" {
		t.Fatalf("certificate_domain = %#v", inbound["certificate_domain"])
	}

	if err := server.store.CheckProbeDDNSServerEntry(ctx, record.ID); err == nil {
		t.Fatal("default entry record must not be removable")
	}
	request(t, handler, http.MethodPatch, path, token, map[string]any{"entry_ip_mode": "auto"}, http.StatusOK)
	saved, err = db.GetServer(ctx, node.ID)
	if err != nil || saved.EntryDDNSRecordID != nil {
		t.Fatalf("released = %+v err=%v", saved, err)
	}
	if err := server.store.CheckProbeDDNSServerEntry(ctx, record.ID); err != nil {
		t.Fatalf("released record still guarded: %v", err)
	}
}

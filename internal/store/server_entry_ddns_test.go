package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
)

func TestServerEntryDDNSPreviousSchemaAndDeleteGuard(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "previous.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	node := &model.Server{Name: "edge", PublicIPv4: "203.0.113.10", EntryIPMode: model.EntryIPModeIPv4}
	if err = db.CreateServer(ctx, node); err != nil {
		t.Fatal(err)
	}
	// Previous release: no column, and the servers revision trigger does not
	// reference it.
	for _, statement := range []string{
		`drop trigger if exists config_rev_servers_update`,
		`alter table servers drop column entry_ddns_record_id`,
	} {
		if _, err = db.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if db, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	current, err := db.GetServer(ctx, node.ID)
	if err != nil || current.EntryIPMode != model.EntryIPModeIPv4 || current.EntryDDNSRecordID != nil {
		t.Fatalf("upgraded server = %+v err=%v", current, err)
	}

	cred := &model.DNSCredential{Name: "cf", Provider: model.DNSProviderCloudflare, Enabled: true, ConfigEncrypted: "enc", Zones: []model.DNSCredentialZone{{ZoneName: "example.com", ProviderZoneID: "zone"}}}
	if err = db.CreateDNSCredential(ctx, cred); err != nil {
		t.Fatal(err)
	}
	record := model.ProbeDDNSRecord{ServerID: node.ID, Name: "dual", DNSCredentialID: &cred.ID, Domain: "edge.example.com", RecordTypes: "both", IntervalSeconds: 300, Enabled: true}
	if err = db.SaveProbeDDNS(ctx, *current, &record); err != nil {
		t.Fatal(err)
	}
	current.EntryIPMode = model.EntryIPModeDDNS
	current.EntryDDNSRecordID = &record.ID
	if err = db.UpdateServerSettings(ctx, current, ServerUpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	current, err = db.GetServer(ctx, node.ID)
	if err != nil || current.EntryIPMode != model.EntryIPModeDDNS || current.EntryDDNSRecordID == nil || *current.EntryDDNSRecordID != record.ID || current.EntryDDNSDomain != "edge.example.com" {
		t.Fatalf("ddns entry = %+v err=%v", current, err)
	}

	if err = db.DeleteProbeDDNS(ctx, record.ID, record.Revision); !errors.Is(err, ErrProbeDDNSServerEntry) {
		t.Fatalf("delete default entry err = %v", err)
	}
	disabled := record
	disabled.Enabled = false
	if err = db.SaveProbeDDNS(ctx, *current, &disabled); !errors.Is(err, ErrProbeDDNSServerEntry) {
		t.Fatalf("disable default entry err = %v", err)
	}

	current.EntryIPMode = model.EntryIPModeAuto
	if err = db.UpdateServerSettings(ctx, current, ServerUpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	current, err = db.GetServer(ctx, node.ID)
	if err != nil || current.EntryDDNSRecordID != nil || current.EntryDDNSDomain != "" {
		t.Fatalf("released entry = %+v err=%v", current, err)
	}
	if err = db.DeleteProbeDDNS(ctx, record.ID, record.Revision); err != nil {
		t.Fatalf("delete released record: %v", err)
	}
}

package store

import (
	"context"
	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
	"path/filepath"
	"testing"
)

func TestExternalProbeUpgradePreservesExistingNodesAndPersistsResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	node := &model.ExternalOutbound{Name: "existing", Protocol: model.ProtocolHTTP, Scope: model.ExternalOutboundScopeGlobal, TargetAddress: "1.1.1.1", TargetPort: 8080, ConfigJSON: `{}`, Enabled: true}
	if err := db.CreateExternalOutbound(ctx, node); err != nil {
		t.Fatal(err)
	}
	// The preceding schema had external nodes but no independent probe-result table.
	if _, err := db.db.ExecContext(ctx, `drop table external_outbound_probe_results`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := core.ExternalOutboundFingerprint(*node)
	if _, err := db.db.ExecContext(ctx, `insert into external_outbound_probe_results(external_outbound_id,fingerprint,task_id,status,checked_at) values(?,?,10,'pending',?)`, node.ID, fingerprint, now()); err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteExternalOutboundProbe(ctx, node.ID, fingerprint, 9, "resolved", "9.9.9.9", "US", ""); err != nil {
		t.Fatal(err)
	}
	saved, err := db.GetExternalOutbound(ctx, node.ID)
	if err != nil || saved.ProbeStatus != "pending" {
		t.Fatal("older result overwrote current attempt")
	}
	if err := db.CompleteExternalOutboundProbe(ctx, node.ID, fingerprint, 10, "resolved", "8.8.8.8", "JP", ""); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	saved, err = db.GetExternalOutbound(ctx, node.ID)
	if err != nil || saved.Name != "existing" || saved.ProbeRegionCode != "JP" || saved.ProbeExitIP != "8.8.8.8" {
		t.Fatalf("saved=%#v err=%v", saved, err)
	}
}

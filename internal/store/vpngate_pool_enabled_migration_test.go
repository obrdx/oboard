package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestVPNGatePoolEnabledColumnIsDropped(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "oboard.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx, `alter table vpngate_pools add column enabled integer not null default 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx, `insert into vpngate_pools(name,policy_json,enabled,created_at,updated_at) values('paused','{}',0,?,?)`, now(), now()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		db, err := Open(path)
		if err != nil {
			t.Fatalf("open attempt %d: %v", attempt, err)
		}
		present, err := db.tableHasColumn(ctx, "vpngate_pools", "enabled")
		if err != nil || present {
			t.Fatalf("attempt %d: enabled column present=%v err=%v", attempt, present, err)
		}
		pools, err := db.ListVPNGatePools(ctx)
		if err != nil || len(pools) != 1 || pools[0].Name != "paused" {
			t.Fatalf("attempt %d: pools %+v err=%v", attempt, pools, err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

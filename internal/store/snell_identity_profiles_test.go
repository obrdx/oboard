package store

import (
	"context"
	"encoding/json"
	"github.com/OboardProject/oboard/internal/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnellV6IdentityProfilesUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oboard.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if db != nil {
			db.Close()
		}
	}()
	ctx := context.Background()
	fixture, err := os.ReadFile("testdata/snell_profiles_46215e8a.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.ExecContext(ctx, `delete from snell_profiles;`+string(fixture)); err != nil {
		t.Fatal(err)
	}
	profiles, err := db.ListSnellProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	server := model.Server{Name: "profile-upgrade", Status: model.ServerOffline}
	if err = db.CreateServer(ctx, &server); err != nil {
		t.Fatal(err)
	}
	originals := map[int64]map[string]any{}
	for _, p := range profiles {
		if !strings.Contains(p.Name, "加固") {
			continue
		}
		cfg := map[string]any{"version": float64(6), "listener_mode": "shared_port", "snell_profile_id": float64(p.ID), "psk": p.PSK, "mode": "default", "reuse": true, "tcp_fast_open": false}
		raw, _ := json.Marshal(cfg)
		in := model.Inbound{Name: p.Name, ServerID: server.ID, Protocol: model.ProtocolSnell, ListenIP: "0.0.0.0", Port: 7000 + int(p.ID), Enabled: true, ConfigJSON: string(raw)}
		if err = db.CreateInbound(ctx, &in); err != nil {
			t.Fatal(err)
		}
		delete(cfg, "snell_profile_id")
		originals[in.ID] = cfg
	}
	custom := model.SnellProfile{Name: "自定义加固参数", Version: 6, PSK: "custom-private-template", Mode: "default", Remark: "custom remark", Enabled: true}
	if err = db.CreateSnellProfile(ctx, &custom); err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.ExecContext(ctx, `create trigger reject_profile_retirement before delete on snell_profiles begin select raise(abort, 'test rollback'); end`); err != nil {
		t.Fatal(err)
	}
	if err = db.migrateSnellV6IdentityProfiles(ctx); err == nil {
		t.Fatal("expected retirement failure")
	}
	for id := range originals {
		var reference int64
		if err = db.db.QueryRowContext(ctx, `select json_extract(config_json,'$.snell_profile_id') from inbounds where id=?`, id).Scan(&reference); err != nil || reference <= 0 {
			t.Fatalf("partial migration: ref=%d err=%v", reference, err)
		}
	}
	if _, err = db.db.ExecContext(ctx, `drop trigger reject_profile_retirement`); err != nil {
		t.Fatal(err)
	}
	for reopen := 0; reopen < 2; reopen++ {
		if err = db.Close(); err != nil {
			t.Fatal(err)
		}
		db = nil
		db, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := db.ListSnellProfiles(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 6 {
			t.Fatalf("profiles=%d", len(got))
		}
		for _, p := range got {
			if p.Builtin && strings.Contains(p.Name, "加固") {
				t.Fatalf("retired preset survived: %s", p.Name)
			}
			if p.Builtin && p.Version == 6 && p.Mode == "default" && !strings.Contains(p.Remark, "独立 PSK") {
				t.Fatalf("stale remark: %s", p.Remark)
			}
		}
		for id, want := range originals {
			in, err := db.GetInbound(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			var cfg map[string]any
			if err = json.Unmarshal([]byte(in.ConfigJSON), &cfg); err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(cfg)
			b, _ := json.Marshal(want)
			if string(a) != string(b) {
				t.Fatalf("entry parameters changed: %s != %s", a, b)
			}
		}
		saved, err := db.GetSnellProfile(ctx, custom.ID)
		if err != nil {
			t.Fatal(err)
		}
		if saved.PSK != custom.PSK || saved.Remark != custom.Remark {
			t.Fatal("custom profile changed")
		}
	}
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
)

var snellAvoidBandPolicy = model.SnellShapingPolicy{FirstRecordDensity: model.SnellDensityBand{Mode: core.SnellDensityOutside, Min: 0.36, Max: 0.65}}

func TestReconcileSelectsAndRotatesShapedSnellPSKs(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "controller.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const secret = "test-credential-encryption-secret"
	credentials := func(userID int64) map[int64]model.ProxyCredential {
		t.Helper()
		users, err := s.LoadProxyCredentials(ctx, secret, []model.User{{ID: userID}})
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]model.ProxyCredential{}
		for _, c := range users[0].ProxyCredentials {
			out[c.InboundID] = c
		}
		return out
	}
	// Inbound 1 is a shaped Snell entry; inbound 2 is a VLESS entry for the
	// same account; user 2 is under device-retirement review.
	plain := []model.ProxyCredential{
		{UserID: 1, InboundID: 1, Protocol: model.ProtocolSnell},
		{UserID: 1, InboundID: 2, Protocol: model.ProtocolVLESS},
		{UserID: 2, InboundID: 1, Protocol: model.ProtocolSnell},
	}
	if err := s.ReconcileProxyCredentials(ctx, secret, plain); err != nil {
		t.Fatal(err)
	}
	before, held := credentials(1), credentials(2)[1]
	if _, err := s.db.ExecContext(ctx, `insert into device_retirement_reviews(user_id,reason_code,created_at) values(2,'legacy_device_restriction',?)`, now()); err != nil {
		t.Fatal(err)
	}

	shaped := append([]model.ProxyCredential(nil), plain...)
	shaped[0].SnellShaping = snellAvoidBandPolicy
	shaped[2].SnellShaping = snellAvoidBandPolicy
	for pass := range 2 {
		if err := s.ReconcileProxyCredentials(ctx, secret, shaped); err != nil {
			t.Fatal(err)
		}
		after := credentials(1)
		snell := after[1]
		if !core.SnellPSKMeetsShaping(snell.Password, snellAvoidBandPolicy) {
			t.Fatalf("pass %d: Snell PSK does not meet the policy", pass)
		}
		if core.SnellPSKMeetsShaping(before[1].Password, snellAvoidBandPolicy) != (snell.ID == before[1].ID) {
			t.Fatalf("pass %d: a conforming PSK was rotated or a failing one kept", pass)
		}
		if after[2] != before[2] {
			t.Fatalf("pass %d: VLESS credential changed", pass)
		}
		if credentials(2)[1] != held {
			t.Fatalf("pass %d: held account rotated", pass)
		}
		before = after
	}

	// A new scope under the policy is issued a conforming PSK directly.
	shaped = append(shaped, model.ProxyCredential{UserID: 3, InboundID: 1, Protocol: model.ProtocolSnell, SnellShaping: snellAvoidBandPolicy})
	if err := s.ReconcileProxyCredentials(ctx, secret, shaped); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := s.db.QueryRowContext(ctx, `select count(*) from proxy_credentials where user_id=3`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("new shaped scope issued %d rows: %v", rows, err)
	}
	if !core.SnellPSKMeetsShaping(credentials(3)[1].Password, snellAvoidBandPolicy) {
		t.Fatal("new shaped scope did not get a conforming PSK")
	}
}

// Starts from the real e0b10551 schema and seeds: no shaping_json column and a
// built-in unsafe-raw parameter set that shared Snell inbounds reject.
func TestSnellV6ShapingProfilesUpgrade(t *testing.T) {
	ctx := context.Background()
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
	fixture, err := os.ReadFile("testdata/snell_profiles_e0b10551.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.ExecContext(ctx, `drop table snell_profiles; delete from sqlite_sequence where name='snell_profiles';`+string(fixture)); err != nil {
		t.Fatal(err)
	}
	server := model.Server{Name: "shaping-upgrade", Status: model.ServerOffline}
	if err = db.CreateServer(ctx, &server); err != nil {
		t.Fatal(err)
	}
	var unsafeID int64
	if err = db.db.QueryRowContext(ctx, `select id from snell_profiles where builtin=1 and mode='unsafe-raw'`).Scan(&unsafeID); err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{"version": float64(6), "listener_mode": "shared_port", "snell_profile_id": float64(unsafeID), "psk": "entry-private-psk-value", "mode": "default", "reuse": true}
	raw, _ := json.Marshal(entry)
	inbound := model.Inbound{Name: "unsafe-template-entry", ServerID: server.ID, Protocol: model.ProtocolSnell, ListenIP: "0.0.0.0", Port: 7100, Enabled: true, ConfigJSON: string(raw)}
	if _, err = db.db.ExecContext(ctx, `insert into inbounds(server_id,name,protocol,listen_ip,port,enabled,config_json,created_at,updated_at) values(?,?,?,?,?,1,?,?,?)`, inbound.ServerID, inbound.Name, inbound.Protocol, inbound.ListenIP, inbound.Port, inbound.ConfigJSON, now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.ExecContext(ctx, `insert into snell_profiles(name,version,psk,mode,remark,builtin,enabled,created_at,updated_at) values('自定义 raw',6,'custom-template-psk','unsafe-raw','custom remark',0,1,?,?)`, now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.ExecContext(ctx, `create trigger reject_unsafe_retirement before delete on snell_profiles begin select raise(abort, 'test rollback'); end`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db = nil
	if db, err = Open(path); err == nil {
		t.Fatal("open succeeded although the retirement failed")
	}
	db = nil
	raw2, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var reference sql.NullInt64
	if err = raw2.QueryRowContext(ctx, `select json_extract(config_json,'$.snell_profile_id') from inbounds where name='unsafe-template-entry'`).Scan(&reference); err != nil || reference.Int64 != unsafeID {
		t.Fatalf("partial retirement: ref=%v err=%v", reference, err)
	}
	if _, err = raw2.ExecContext(ctx, `drop trigger reject_unsafe_retirement`); err != nil {
		t.Fatal(err)
	}
	raw2.Close()

	for reopen := range 2 {
		if db, err = Open(path); err != nil {
			t.Fatalf("reopen %d: %v", reopen, err)
		}
		profiles, err := db.ListSnellProfiles(ctx)
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]model.SnellProfile{}
		for _, p := range profiles {
			names[p.Name] = p
			if p.Builtin && p.Mode == "unsafe-raw" {
				t.Fatalf("built-in unsafe-raw survived: %s", p.Name)
			}
		}
		if len(profiles) != 6 {
			t.Fatalf("profiles=%d", len(profiles))
		}
		preset, ok := names["Snell v6 首包密度避让"]
		if !ok || !preset.Builtin || preset.Shaping != snellAvoidBandPolicy || preset.Mode != "default" || preset.Version != 6 {
			t.Fatalf("shaping preset missing or wrong: %+v", preset)
		}
		if err := core.ValidateSnellShaping(preset.Version, preset.Mode, preset.Shaping); err != nil {
			t.Fatalf("seeded preset is not feasible: %v", err)
		}
		if custom := names["自定义 raw"]; custom.PSK != "custom-template-psk" || custom.Mode != "unsafe-raw" || custom.Remark != "custom remark" {
			t.Fatalf("custom profile changed: %+v", custom)
		}
		var config string
		if err := db.db.QueryRowContext(ctx, `select config_json from inbounds where name='unsafe-template-entry'`).Scan(&config); err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(config), &got); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{}
		for k, v := range entry {
			if k != "snell_profile_id" {
				want[k] = v
			}
		}
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		if string(a) != string(b) {
			t.Fatalf("entry parameters changed: %s != %s", a, b)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		db = nil
	}
}

func TestSnellProfileShapingRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	profile := model.SnellProfile{Name: "custom shaping", Version: 6, Mode: "default", Enabled: true, Shaping: model.SnellShapingPolicy{PaddingStyle: core.SnellPaddingBitBiased, FirstRecordDensity: model.SnellDensityBand{Mode: core.SnellDensityInside, Min: 0.650000001, Max: 1}}}
	if err := db.CreateSnellProfile(ctx, &profile); err != nil {
		t.Fatal(err)
	}
	saved, err := db.GetSnellProfile(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := model.SnellShapingPolicy{PaddingStyle: core.SnellPaddingBitBiased, FirstRecordDensity: model.SnellDensityBand{Mode: core.SnellDensityInside, Min: 0.65, Max: 1}}
	if saved.Shaping != want {
		t.Fatalf("stored %+v", saved.Shaping)
	}
	saved.Shaping = model.SnellShapingPolicy{}
	changed, err := db.UpdateSnellProfile(ctx, saved)
	if err != nil || !changed {
		t.Fatalf("clearing shaping: changed=%v err=%v", changed, err)
	}
	if again, _ := db.GetSnellProfile(ctx, profile.ID); !again.Shaping.IsZero() {
		t.Fatalf("shaping not cleared: %+v", again.Shaping)
	}
}

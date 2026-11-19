package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

func proxyTestWindow(key string) model.ServerTrafficWindow {
	start, _ := time.Parse("2006-01-02", key)
	return model.ServerTrafficWindow{Key: key, Start: start, End: start.AddDate(0, 1, 0)}
}

func TestServerProxyTrafficAccumulatesFromLedgerSeparatelyFromHost(t *testing.T) {
	s, ctx, user, server := openTrafficLedgerFixture(t)
	defer s.Close()
	if err := s.SetServerTrafficUsed(ctx, server.ID, 5_000, proxyTestWindow("2026-11-01")); err != nil {
		t.Fatal(err)
	}
	period := model.TrafficPeriod{UserID: user.ID, PeriodKey: "2026-08", StartedAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(time.Hour), Limit: 1 << 30}
	firstReport := v2Report("proxy-1", server.ID, user.ID, 0, 600, 0, 1_800)
	firstReport.StartedAt = firstReport.EndedAt.Add(-time.Minute)
	first, err := s.CommitTrafficLedger(ctx, TrafficLedgerCommit{ServerID: server.ID, Periods: TrafficPeriods(period), Reports: []model.TrafficReport{firstReport}})
	if err != nil {
		t.Fatal(err)
	}
	// First commit has no previous one, so the rate uses the report's 60 s window.
	if p := first.ProxyTraffic; p == nil || p.UploadBytes != 600 || p.DownloadBytes != 1_800 || p.UploadBPS != 10 || p.DownloadBPS != 30 {
		t.Fatalf("first proxy traffic = %+v", first.ProxyTraffic)
	}
	second, err := s.CommitTrafficLedger(ctx, TrafficLedgerCommit{ServerID: server.ID, Periods: TrafficPeriods(period), Reports: []model.TrafficReport{v2Report("proxy-2", server.ID, user.ID, 600, 650, 1_800, 2_300)}})
	if err != nil {
		t.Fatal(err)
	}
	// Back-to-back commits clamp the span to the 5 s floor instead of spiking.
	if p := second.ProxyTraffic; p == nil || p.UploadBytes != 650 || p.DownloadBytes != 2_300 || p.UploadBPS != 10 || p.DownloadBPS != 100 {
		t.Fatalf("second proxy traffic = %+v", second.ProxyTraffic)
	}
	got, err := s.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProxyTrafficUploadBytes != 650 || got.ProxyTrafficDownloadBytes != 2_300 || got.ProxyDownloadBPS != 100 {
		t.Fatalf("server proxy = %d/%d %d bps", got.ProxyTrafficUploadBytes, got.ProxyTrafficDownloadBytes, got.ProxyDownloadBPS)
	}
	if got.TrafficUploadBytes+got.TrafficDownloadBytes != 5_000 {
		t.Fatalf("host counter changed by proxy accounting: %d", got.TrafficUploadBytes+got.TrafficDownloadBytes)
	}

	// The host period rolls over first; the old proxy total must not leak into it.
	if err := s.SetServerTrafficUsed(ctx, server.ID, 10, proxyTestWindow("2026-12-01")); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetServer(ctx, server.ID); got.ProxyTrafficUploadBytes != 0 || got.ProxyTrafficDownloadBytes != 0 {
		t.Fatalf("previous period proxy leaked: %d/%d", got.ProxyTrafficUploadBytes, got.ProxyTrafficDownloadBytes)
	}
	third, err := s.CommitTrafficLedger(ctx, TrafficLedgerCommit{ServerID: server.ID, Periods: TrafficPeriods(period), Reports: []model.TrafficReport{v2Report("proxy-3", server.ID, user.ID, 650, 700, 2_300, 2_400)}})
	if err != nil {
		t.Fatal(err)
	}
	if p := third.ProxyTraffic; p == nil || p.UploadBytes != 50 || p.DownloadBytes != 100 {
		t.Fatalf("new period proxy traffic = %+v", third.ProxyTraffic)
	}

	// Resetting the period clears both counters.
	if err := s.SetServerTrafficUsed(ctx, server.ID, 0, proxyTestWindow("2026-12-01")); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetServer(ctx, server.ID); got.ProxyTrafficUploadBytes != 0 || got.ProxyTrafficDownloadBytes != 0 {
		t.Fatalf("reset kept proxy traffic: %d/%d", got.ProxyTrafficUploadBytes, got.ProxyTrafficDownloadBytes)
	}
}

func TestProxyRateReadsZeroWhenStale(t *testing.T) {
	now := time.Now()
	recent := now.Add(-30 * time.Second)
	stale := now.Add(-2 * time.Minute)
	if up, down := proxyRate(&recent, 5, 7, now); up != 5 || down != 7 {
		t.Fatalf("recent rate = %d/%d", up, down)
	}
	if up, down := proxyRate(&stale, 5, 7, now); up != 0 || down != 0 {
		t.Fatalf("stale rate = %d/%d", up, down)
	}
	if up, down := proxyRate(nil, 5, 7, now); up != 0 || down != 0 {
		t.Fatalf("missing rate = %d/%d", up, down)
	}
}

// TestServerProxyTrafficColumnsAddedToPreviousSchema starts from a
// server_telemetry table without the proxy columns and checks that opening
// the database adds them and seeds the current period from traffic_stats.
func TestServerProxyTrafficColumnsAddedToPreviousSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oboard.sqlite")
	ctx := context.Background()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	server := &model.Server{Name: "proxy-migrate", ListenIP: "0.0.0.0"}
	if err := s.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	user := &model.User{Username: "proxy-migrate-user", PasswordHash: "hash", Role: model.RoleViewer, Status: "active", ProxyUUID: "11111111-1111-4111-8111-111111111188", ProxyPassword: "pass", SubscriptionToken: "proxy-migrate-sub"}
	if err := s.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := s.SetServerTrafficUsed(ctx, server.ID, 9_999, proxyTestWindow("2026-11-01")); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		at       string
		up, down int64
	}{
		{"2026-10-31T23:59:59Z", 1_000, 1_000}, // previous period
		{"2026-11-02T08:00:00Z", 10, 90},
		{"2026-11-19T08:00:00.5Z", 5, 45},
	} {
		if _, err := s.db.ExecContext(ctx, `insert into traffic_stats(server_id,user_id,upload_bytes,download_bytes,created_at) values(?,?,?,?,?)`, server.ID, user.ID, row.up, row.down, row.at); err != nil {
			t.Fatal(err)
		}
	}
	for i := len(serverProxyTrafficColumns) - 1; i >= 0; i-- {
		if _, err := s.db.ExecContext(ctx, `alter table server_telemetry drop column `+serverProxyTrafficColumns[i].name); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		reopened, err := Open(path)
		if err != nil {
			t.Fatalf("reopen %d: %v", attempt, err)
		}
		got, err := reopened.GetServer(ctx, server.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ProxyTrafficUploadBytes != 15 || got.ProxyTrafficDownloadBytes != 135 {
			t.Fatalf("reopen %d proxy = %d/%d, want 15/135", attempt, got.ProxyTrafficUploadBytes, got.ProxyTrafficDownloadBytes)
		}
		if got.TrafficUploadBytes+got.TrafficDownloadBytes != 9_999 {
			t.Fatalf("reopen %d host counter = %d", attempt, got.TrafficUploadBytes+got.TrafficDownloadBytes)
		}
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

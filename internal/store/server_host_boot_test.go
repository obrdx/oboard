package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

func TestServerHostBootedAtMigrateFromPreviousSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "host-boot.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	server := &model.Server{Name: "boot-node", AgentID: "boot-agent", AgentTokenHash: "boot-token", ChainSecret: "chain-secret", ListenIP: "0.0.0.0", Status: model.ServerOnline}
	if err := s.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`alter table servers drop column host_booted_at`); err != nil {
		t.Fatalf("drop host_booted_at: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(path)
	if err != nil {
		t.Fatalf("open with previous schema: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	stored, err := s.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.HostBootedAt != nil || stored.Name != "boot-node" {
		t.Fatalf("migrated row: booted=%v name=%q", stored.HostBootedAt, stored.Name)
	}
	booted := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	stored.HostBootedAt = &booted
	if err := s.UpdateServerRuntimeState(ctx, stored); err != nil {
		t.Fatal(err)
	}
	reloaded, err := s.GetServer(ctx, stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.HostBootedAt == nil || !reloaded.HostBootedAt.Equal(booted) {
		t.Fatalf("reloaded host_booted_at = %v, want %v", reloaded.HostBootedAt, booted)
	}
}

func TestApplyHealthReportTracksHostReboot(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "oboard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	server := &model.Server{Name: "reboot", AgentID: "agent-reboot", ListenIP: "0.0.0.0", PortRangeStart: 10000, PortRangeEnd: 20000, Status: model.ServerUnknown}
	if err := s.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	first := healthReportFixture(server.AgentID, at)
	first.HostUptimeSeconds = 3 * 24 * 3600
	applied, err := s.ApplyHealthReport(ctx, server.ID, first, healthReportWindow(at))
	if err != nil || applied.Curr.HostBootedAt == nil {
		t.Fatalf("first report booted=%v err=%v", applied.Curr.HostBootedAt, err)
	}
	booted := *applied.Curr.HostBootedAt
	if age := time.Since(booted); age < 71*time.Hour || age > 73*time.Hour {
		t.Fatalf("derived boot is %v ago, want about 72h", age)
	}

	// Later reports keep the same boot even though uptime and latency drift.
	steady := healthReportFixture(server.AgentID, at.Add(time.Minute))
	steady.HostUptimeSeconds = first.HostUptimeSeconds + 60 + 30
	kept, err := s.ApplyHealthReport(ctx, server.ID, steady, healthReportWindow(at.Add(time.Minute)))
	if err != nil || kept.Curr.HostBootedAt == nil || !kept.Curr.HostBootedAt.Equal(booted) {
		t.Fatalf("steady report moved boot: %v err=%v", kept.Curr.HostBootedAt, err)
	}

	// An Agent that does not report uptime keeps the last known boot.
	omitted := healthReportFixture(server.AgentID, at.Add(2*time.Minute))
	omitted.HostUptimeSeconds = 0
	kept, err = s.ApplyHealthReport(ctx, server.ID, omitted, healthReportWindow(at.Add(2*time.Minute)))
	if err != nil || kept.Curr.HostBootedAt == nil || !kept.Curr.HostBootedAt.Equal(booted) {
		t.Fatalf("omitted uptime cleared boot: %v err=%v", kept.Curr.HostBootedAt, err)
	}

	rebooted := healthReportFixture(server.AgentID, at.Add(3*time.Minute))
	rebooted.HostUptimeSeconds = 45
	result, err := s.ApplyHealthReport(ctx, server.ID, rebooted, healthReportWindow(at.Add(3*time.Minute)))
	if err != nil || result.Coalesced {
		t.Fatalf("reboot must persist: coalesced=%t err=%v", result.Coalesced, err)
	}
	stored, err := s.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.HostBootedAt == nil || time.Since(*stored.HostBootedAt) > 5*time.Minute {
		t.Fatalf("reboot did not reset host_booted_at: %v", stored.HostBootedAt)
	}
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

// connectionAuditParityFixture seeds several users whose histories exercise
// every risk input shape: ordinary traffic, anomaly spikes, insufficient
// robust-Z history, internal probes, confirmed and candidate probe episodes,
// dropped buckets, shared routes, multiple source IPs and countries, and a
// presence-only user with no reports.
func connectionAuditParityFixture(t *testing.T, s *Store, server *model.Server) ([]*model.User, time.Time) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	users := []*model.User{}
	for index := 0; index < 4; index++ {
		user := &model.User{Username: "parity-user-" + string(rune('A'+index)), PasswordHash: "x", Role: model.RoleViewer, Status: "active"}
		if err := s.CreateUser(ctx, user); err != nil {
			t.Fatal(err)
		}
		users = append(users, user)
	}
	reports := []model.ConnectionAuditReport{}
	at := now.Add(-10 * time.Minute)
	// User A: ordinary traffic across several weeks for a healthy robust-Z
	// baseline, plus a mild current hour.
	for week := 0; week < 4; week++ {
		started := now.Add(-time.Duration(week+1) * 7 * 24 * time.Hour)
		reports = append(reports, robustZReport("parity-a-history-"+string(rune('0'+week)), server.ID, users[0].ID, started, 100))
	}
	reports = append(reports, robustZReport("parity-a-current", server.ID, users[0].ID, now, 105))
	// User B: anomaly spike in the current hour.
	for week := 0; week < 4; week++ {
		started := now.Add(-time.Duration(week+1) * 7 * 24 * time.Hour)
		reports = append(reports, robustZReport("parity-b-history-"+string(rune('0'+week)), server.ID, users[1].ID, started, 100))
	}
	reports = append(reports, robustZReport("parity-b-spike", server.ID, users[1].ID, now, 1000))
	// User C: clone signal on one device across two independent networks and
	// countries, plus candidate and confirmed probe episodes and a dropped
	// bucket report.
	left := meaningfulConnectionReport("parity-c-left", server.ID, users[2].ID, "parity-c-device", "1.1.1.1", "CN", "ISP-A", at, at.Add(90*time.Second))
	right := meaningfulConnectionReport("parity-c-right", server.ID, users[2].ID, "parity-c-device", "8.8.8.8", "US", "ISP-B", at.Add(5*time.Second), at.Add(90*time.Second))
	reports = append(reports, left, right)
	dropped := meaningfulConnectionReport("parity-c-dropped", server.ID, users[2].ID, "parity-c-device", "1.1.1.1", "CN", "ISP-A", at.Add(95*time.Second), at.Add(96*time.Second))
	dropped.DroppedBucketCount = 3
	dropped.ProbeState = "confirmed"
	reports = append(reports, dropped)
	inboundIDs := make([]int64, 4)
	for i := range inboundIDs {
		inboundIDs[i] = *newAuditTestInbound(t, s, server.ID, i)
	}
	reports = append(reports, probeParityReports(inboundIDs, server.ID, users[2].ID, "parity-c-probe", 4, 4, at.Add(-30*time.Second), "confirmed")...)
	reports = append(reports, probeParityReports(inboundIDs, server.ID, users[2].ID, "parity-c-candidate", 4, 4, at.Add(-5*time.Second), "candidate")...)
	// User D: shared route with user A, multiple source IPs, plus an internal
	// probe that must never count as a device.
	sharedLeft := meaningfulConnectionReport("parity-d-left", server.ID, users[3].ID, "parity-d-device", "203.0.113.7", "JP", "ISP-J", at, at.Add(30*time.Second))
	sharedLeft.RouteID = "route-shared"
	reports = append(reports, sharedLeft)
	internal := meaningfulConnectionReport("parity-d-internal", server.ID, users[3].ID, "", "127.0.0.1", "", "", at, at.Add(5*time.Second))
	internal.InternalProbe = true
	reports = append(reports, internal)
	// User A shares the route with user D.
	sharedRight := meaningfulConnectionReport("parity-a-shared", server.ID, users[0].ID, "parity-a-device", "203.0.113.7", "JP", "ISP-J", at.Add(2*time.Second), at.Add(30*time.Second))
	sharedRight.RouteID = "route-shared"
	reports = append(reports, sharedRight)
	if _, err := s.AddConnectionAuditReports(ctx, reports); err != nil {
		t.Fatal(err)
	}
	// Presence-only user E: no reports at all.
	presenceUser := &model.User{Username: "parity-user-presence", PasswordHash: "x", Role: model.RoleViewer, Status: "active"}
	if err := s.CreateUser(ctx, presenceUser); err != nil {
		t.Fatal(err)
	}
	users = append(users, presenceUser)
	presence := model.ConnectionPresenceEvent{
		Sequence: 1, ServerID: server.ID, UserID: presenceUser.ID, DeviceIDHash: "presence-only", CredentialEpoch: 1,
		SourceIP: "9.9.9.9", RouteID: "route-presence", Network: "tcp", Event: "first_meaningful_payload",
		State: "active", ActiveConnections: 1, Meaningful: true, PayloadLastAt: now, At: now,
	}
	if _, err := s.ApplyConnectionPresenceEvents(ctx, "parity-agent", server.ID, 0, []model.ConnectionPresenceEvent{presence}); err != nil {
		t.Fatal(err)
	}
	return users, now
}

// TestConnectionAuditOverviewBatchParity proves the batched overview produces
// exactly the same per-user summaries as the per-user reference loop across
// every risk input shape.
func TestConnectionAuditOverviewBatchParity(t *testing.T) {
	s, server, _ := newMaintenanceTestStore(t)
	ctx := context.Background()
	connectionAuditParityFixture(t, s, server)

	got, err := s.ConnectionAuditOverview(ctx, 24, true, DefaultAuditPolicy())
	if err != nil {
		t.Fatal(err)
	}
	want := legacyConnectionAuditOverviewPerUser(ctx, t, s, 24, true, DefaultAuditPolicy())
	// The two runs happen at slightly different wall-clock instants.
	got.GeneratedAt = time.Time{}
	want.GeneratedAt = time.Time{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("batch overview differs from per-user reference:\n got=%+v\nwant=%+v", got, want)
	}
}

// TestConnectionAuditOverviewBatchParityDisabledGate keeps the batch path on
// the disabled branch.
func TestConnectionAuditOverviewBatchParityDisabledGate(t *testing.T) {
	s, server, _ := newMaintenanceTestStore(t)
	ctx := context.Background()
	connectionAuditParityFixture(t, s, server)
	got, err := s.ConnectionAuditOverview(ctx, 24, false, DefaultAuditPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if got.EnabledServerCount != 0 || got.ElevatedRiskCount != 0 {
		t.Fatalf("disabled batch overview must be empty: %+v", got)
	}
}

// legacyConnectionAuditOverviewPerUser is the pre-batch per-user overview loop
// kept as the reference implementation for the parity test: every shared query
// plus one report, episode, and robust-Z query per user, evaluated through the
// same evaluateConnectionAuditUser helper as the batch driver.
func legacyConnectionAuditOverviewPerUser(ctx context.Context, t testing.TB, s *Store, windowHours int, connectionAuditEnabled bool, policy model.AuditPolicy) model.ConnectionAuditOverview {
	t.Helper()
	if windowHours < 1 {
		windowHours = 24
	}
	nowTime := time.Now().UTC()
	if ValidateAuditPolicy(policy) != nil {
		policy = DefaultAuditPolicy()
	}
	overview := model.ConnectionAuditOverview{WindowHours: windowHours, EvidenceWindowHours: windowHours, RiskWindowMinutes: int(connectionAuditRiskWindow / time.Minute), GeneratedAt: nowTime, Policy: policy, Users: []model.ConnectionAuditUserSummary{}}
	if err := s.db.QueryRowContext(ctx, `select count(*) from servers where connection_audit_enabled=1`).Scan(&overview.EnabledServerCount); err != nil {
		t.Fatal(err)
	}
	if !connectionAuditEnabled {
		overview.EnabledServerCount = 0
	}
	rows, err := s.db.QueryContext(ctx, `select id from users order by id`)
	if err != nil {
		t.Fatal(err)
	}
	userIDs := []int64{}
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			t.Fatal(err)
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, userID := range userIDs {
		item, err := s.ConnectionAuditUserRisk(ctx, userID, windowHours, policy, nowTime)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		overview.Users = append(overview.Users, *item)
		overview.TotalConnections += item.ConnectionCount
		if item.RiskScore >= 55 {
			overview.ElevatedRiskCount++
		}
	}
	since := nowTime.Add(-time.Duration(windowHours) * time.Hour).Format(time.RFC3339Nano)
	if err := s.history.QueryRowContext(ctx, `select count(distinct source_ip) from connection_audit_reports where ended_at>=?`, since).Scan(&overview.UniqueSourceIPs); err != nil {
		t.Fatal(err)
	}
	overview.ReportingUserCount = len(overview.Users)
	sort.SliceStable(overview.Users, func(i, j int) bool {
		if overview.Users[i].RiskScore != overview.Users[j].RiskScore {
			return overview.Users[i].RiskScore > overview.Users[j].RiskScore
		}
		return overview.Users[i].LastSeenAt.After(overview.Users[j].LastSeenAt)
	})
	return overview
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OboardProject/oboard/internal/model"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTrafficLedgerMetrics(t *testing.T) {
	s, ctx, user, server := openTrafficLedgerFixture(t)
	defer s.Close()
	period := model.TrafficPeriod{UserID: user.ID, PeriodKey: "2026-08", StartedAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(time.Hour), Limit: 1 << 30}
	for batch := 0; batch < 30; batch++ {
		reports := make([]model.TrafficReport, 100)
		for i := range reports {
			from := int64(batch*100 + i)
			reports[i] = v2Report(fmt.Sprintf("metrics-%d-%d", batch, i), server.ID, user.ID, from, from+1, from, from+1)
		}
		if _, err := s.CommitTrafficLedger(ctx, TrafficLedgerCommit{ServerID: server.ID, Periods: TrafficPeriods(period), Reports: reports}); err != nil {
			t.Fatal(err)
		}
	}
	perf := s.databaseMetrics.snapshot("core", 0, 0, 0, 0).TrafficLedger
	if perf.SampleCount != 30 || perf.Reports != 3000 || perf.Accepted != 3000 || perf.Failures != 0 || perf.Writes == 0 || perf.Queries == 0 {
		t.Fatalf("metrics=%+v", perf)
	}
	raw, _ := json.Marshal(perf)
	t.Log(string(raw))
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.CommitTrafficLedger(canceled, TrafficLedgerCommit{ServerID: server.ID}); err == nil {
		t.Fatal("expected canceled acquisition")
	}
	if got := s.databaseMetrics.snapshot("core", 0, 0, 0, 0).TrafficLedger; got.Failures != 1 || got.Canceled != 1 {
		t.Fatalf("failure metrics=%+v", got)
	}
}

func TestLedgerMetricsBoundedWindow(t *testing.T) {
	m := newDatabaseMetrics()
	at := time.Now()
	for i := 0; i < 600; i++ {
		m.observeLedger(ledgerSample{at: at.Add(time.Duration(i) * time.Second), reports: int64(i), durations: [ledgerStageCount]time.Duration{ledgerTotal: time.Millisecond}})
	}
	got := m.snapshot("core", 0, 0, 0, 0).TrafficLedger
	if got.Count != 600 || got.SampleCount != 512 || !got.WindowStart.Equal(at.Add(88*time.Second)) || got.MaxReports != 599 {
		t.Fatalf("window=%+v", got)
	}
}

func TestTrafficLedgerMixedPeriodsPanelValueIgnoresReportOrder(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprint(reverse), func(t *testing.T) {
			s, ctx, user, server := openTrafficLedgerFixture(t)
			defer s.Close()
			at := time.Now()
			current := model.TrafficPeriod{UserID: user.ID, PeriodKey: "current", StartedAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour), Limit: 1000}
			previous := model.TrafficPeriod{UserID: user.ID, PeriodKey: "previous", StartedAt: at.Add(-3 * time.Hour), EndsAt: at.Add(-time.Hour), Limit: 1000}
			newer := v2Report("new", server.ID, user.ID, 0, 10, 0, 20)
			newer.PeriodKey = current.PeriodKey
			older := v2Report("old", server.ID, user.ID, 0, 100, 0, 200)
			older.PeriodKey = previous.PeriodKey
			reports := []model.TrafficReport{newer, older}
			if reverse {
				reports[0], reports[1] = reports[1], reports[0]
			}
			if _, err := s.CommitTrafficLedger(ctx, TrafficLedgerCommit{ServerID: server.ID, Periods: TrafficPeriods(current, previous), Reports: reports}); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetUser(ctx, user.ID)
			if err != nil || got.TrafficUsedBytes != 30 {
				t.Fatalf("panel value=%+v err=%v", got, err)
			}
			// A later replay/tail in an older period cannot replace the current value.
			older.ReportID = "old-tail"
			older.FromUploadBytes = 100
			older.ToUploadBytes = 110
			older.FromDownloadBytes = 200
			older.ToDownloadBytes = 210
			if _, err := s.CommitTrafficLedger(ctx, TrafficLedgerCommit{ServerID: server.ID, Periods: TrafficPeriods(previous), Reports: []model.TrafficReport{older}}); err != nil {
				t.Fatal(err)
			}
			got, _ = s.GetUser(ctx, user.ID)
			if got.TrafficUsedBytes != 30 {
				t.Fatalf("old tail changed panel to %d", got.TrafficUsedBytes)
			}
		})
	}
}

func TestTrafficLedgerRollbackMetricsAndReplay(t *testing.T) {
	s, ctx, user, server := openTrafficLedgerFixture(t)
	defer s.Close()
	period := model.TrafficPeriod{UserID: user.ID, PeriodKey: "2026-08", StartedAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(time.Hour), Limit: 3}
	reports := []model.TrafficReport{v2Report("first", server.ID, user.ID, 0, 1, 0, 1), v2Report("second", server.ID, user.ID, 1, 2, 1, 2)}
	if _, err := s.db.ExecContext(ctx, `create trigger fail_ledger before insert on traffic_reports when new.report_id='second' begin select raise(abort,'injected failure'); end`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitTrafficLedger(ctx, TrafficLedgerCommit{ServerID: server.ID, Periods: TrafficPeriods(period), Reports: reports}); err == nil {
		t.Fatal("expected rollback")
	}
	if _, err := s.GetTrafficPeriod(ctx, user.ID, period.PeriodKey); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("failed transaction left period: %v", err)
	}
	perf := s.databaseMetrics.snapshot("core", 0, 0, 0, 0).TrafficLedger
	if perf.Failures != 1 || perf.Stages[ledgerRollback].Count != 1 || perf.Stages[ledgerReport].Count != 1 {
		t.Fatalf("failed metrics=%+v", perf)
	}
	raw, _ := json.Marshal(perf)
	t.Logf("rollback sample=%s", raw)
	if _, err := s.db.ExecContext(ctx, `drop trigger fail_ledger`); err != nil {
		t.Fatal(err)
	}
	result, err := s.CommitTrafficLedger(ctx, TrafficLedgerCommit{ServerID: server.ID, Periods: TrafficPeriods(period), Reports: reports})
	if err != nil || len(result.AcceptedReports) != 2 {
		t.Fatalf("retry=%+v err=%v", result, err)
	}
	stored, err := s.GetTrafficPeriod(ctx, user.ID, period.PeriodKey)
	if err != nil || stored.Upload != 2 || stored.Download != 2 || stored.State != "quota_exceeded" {
		t.Fatalf("period=%+v err=%v", stored, err)
	}
}

func TestTrafficLedgerConnectionWaitDeadline(t *testing.T) {
	s, ctx, _, server := openTrafficLedgerFixture(t)
	defer s.Close()
	held, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	timed, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := s.CommitTrafficLedger(timed, TrafficLedgerCommit{ServerID: server.ID}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline=%v", err)
	}
	perf := s.databaseMetrics.snapshot("core", 0, 0, 0, 0).TrafficLedger
	if perf.Timeouts != 1 || perf.Failures != 1 || perf.Queries != 0 || perf.Writes != 0 || perf.Stages[ledgerBegin].Count != 0 || perf.Stages[ledgerConnection].Count != 1 {
		t.Fatalf("wait metrics=%+v", perf)
	}
	raw, _ := json.Marshal(perf)
	t.Logf("deadline sample=%s", raw)
}

func TestTrafficLedgerLookupPlansUseExistingIndexes(t *testing.T) {
	s, ctx, user, server := openTrafficLedgerFixture(t)
	defer s.Close()
	queries := []struct {
		query string
		args  []any
	}{
		{`select report_id from traffic_reports where report_id=?`, []any{"r"}},
		{`select report_id from traffic_reports where server_id=? and counter_source=? and stream_id=? and counter_epoch=? and period_key=? and from_upload_bytes=? and to_upload_bytes=? and from_download_bytes=? and to_download_bytes=? and stream_id<>''`, []any{server.ID, "core", "s", "e", "p", 0, 1, 0, 1}},
		{`select accepted_upload_bytes from traffic_counter_streams where server_id=? and counter_source=? and stream_id=? and counter_epoch=? and period_key=?`, []any{server.ID, "core", "s", "e", "p"}},
		{`select upload_bytes from traffic_periods where user_id=? and period_key=?`, []any{user.ID, "p"}},
	}
	for _, query := range queries {
		rows, err := s.db.QueryContext(ctx, "explain query plan "+query.query, query.args...)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			t.Log(detail)
			if !strings.Contains(detail, "USING INDEX") && !strings.Contains(detail, "USING COVERING INDEX") {
				t.Errorf("unindexed query: %s", detail)
			}
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestTrafficLedgerConcurrentAgentsRestartAndLostACK(t *testing.T) {
	s, ctx, user, server := openTrafficLedgerFixture(t)
	defer func() { _ = s.Close() }()
	servers := []*model.Server{server}
	for i := 1; i < 35; i++ {
		node := &model.Server{Name: fmt.Sprintf("concurrent-%d", i), Status: model.ServerOnline, ListenIP: "0.0.0.0", PortRangeStart: 10000, PortRangeEnd: 10010}
		if err := s.CreateServer(ctx, node); err != nil {
			t.Fatal(err)
		}
		servers = append(servers, node)
	}
	period := model.TrafficPeriod{UserID: user.ID, PeriodKey: "2026-08", StartedAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(time.Hour), Limit: 35 * 300}
	configBefore, err := s.ConfigurationRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	routingBefore, err := s.RoutingCacheRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	run := func(wantStatus string) {
		t.Helper()
		errs := make(chan error, len(servers))
		var wg sync.WaitGroup
		for i, node := range servers {
			wg.Add(1)
			go func(i int, node *model.Server) {
				defer wg.Done()
				report := v2Report(fmt.Sprintf("concurrent-%d", i), node.ID, user.ID, 0, 100, 0, 200)
				result, err := s.CommitTrafficLedger(ctx, TrafficLedgerCommit{ServerID: node.ID, Periods: TrafficPeriods(period), Reports: []model.TrafficReport{report}})
				if err == nil && (len(result.AcceptedReports) != 1 || result.AcceptedReports[0].Status != wantStatus) {
					err = fmt.Errorf("result=%+v", result)
				}
				errs <- err
			}(i, node)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	run(trafficAcceptAccepted)
	configAfter, err := s.ConfigurationRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	routingAfter, err := s.RoutingCacheRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if configBefore != configAfter || routingBefore != routingAfter {
		t.Fatal("accepted traffic changed configuration/routing revision")
	}
	path := s.path
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	configBefore, err = s.ConfigurationRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	routingBefore, err = s.RoutingCacheRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	run(trafficAcceptDuplicate)
	stored, err := s.GetTrafficPeriod(ctx, user.ID, period.PeriodKey)
	if err != nil || stored.Upload != 3500 || stored.Download != 7000 || stored.State != "quota_exceeded" {
		t.Fatalf("totals=%+v err=%v", stored, err)
	}
	for _, node := range servers {
		lease, err := s.GetTrafficLease(ctx, node.ID, user.ID, period.PeriodKey)
		if err != nil || lease.ConsumedBytes != 300 {
			t.Fatalf("lease=%+v err=%v", lease, err)
		}
	}
	configAfter, err = s.ConfigurationRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	routingAfter, err = s.RoutingCacheRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if configBefore != configAfter || routingBefore != routingAfter {
		t.Fatal("traffic changed configuration/routing revision")
	}
}

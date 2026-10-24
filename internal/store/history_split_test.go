package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

func TestFreshInstallCreatesSeparateDatabases(t *testing.T) {
	for _, memory := range []string{":memory:", "file::memory:", "file::memory:?cache=shared"} {
		if got := HistoryPathFor(memory); got != ":memory:" {
			t.Fatalf("HistoryPathFor(%q) = %q", memory, got)
		}
	}
	corePath := filepath.Join(t.TempDir(), "oboard.sqlite")
	s, err := Open(corePath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.HistoryPath() == corePath {
		t.Fatal("history path equals core path")
	}
	info, err := os.Stat(s.HistoryPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("history file mode = %v", info.Mode().Perm())
	}
	for _, table := range migratedHistoryTables {
		var coreCount, historyCount int
		if err := s.db.QueryRow(`select count(*) from sqlite_master where type='table' and name=?`, table).Scan(&coreCount); err != nil {
			t.Fatal(err)
		}
		if err := s.history.QueryRow(`select count(*) from sqlite_master where type='table' and name=?`, table).Scan(&historyCount); err != nil {
			t.Fatal(err)
		}
		if coreCount != 0 || historyCount != 1 {
			t.Fatalf("table %s core=%d history=%d", table, coreCount, historyCount)
		}
	}
	var usersInCore, usersInHistory int
	if err := s.db.QueryRow(`select count(*) from sqlite_master where type='table' and name='users'`).Scan(&usersInCore); err != nil {
		t.Fatal(err)
	}
	if err := s.history.QueryRow(`select count(*) from sqlite_master where type='table' and name='users'`).Scan(&usersInHistory); err != nil {
		t.Fatal(err)
	}
	if usersInCore != 1 || usersInHistory != 0 {
		t.Fatalf("users core=%d history=%d", usersInCore, usersInHistory)
	}
}

func TestHistoryMigrationResumesAfterInterruption(t *testing.T) {
	corePath := filepath.Join(t.TempDir(), "oboard.sqlite")
	s, sampleAt := seedHistoryMetric(t, corePath)
	historyPath := s.HistoryPath()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	moveMetricBackToLegacyCore(t, corePath, historyPath, false)

	reopened, err := Open(corePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var count int
	if err := reopened.history.QueryRow(`select count(*) from server_metric_samples where sampled_at=?`, sampleAt.Format(time.RFC3339Nano)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("history metric rows = %d", count)
	}
	var coreTable int
	if err := reopened.db.db.QueryRow(`select count(*) from sqlite_master where type='table' and name='server_metric_samples'`).Scan(&coreTable); err != nil {
		t.Fatal(err)
	}
	if coreTable != 0 {
		t.Fatal("legacy core history table was not removed after verification")
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(corePath)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if err := again.history.QueryRow(`select count(*) from server_metric_samples`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rows after second open = %d", count)
	}
}

func TestHistoryMigrationReconcilesSourceChangedAfterRollback(t *testing.T) {
	corePath := filepath.Join(t.TempDir(), "oboard.sqlite")
	s, _ := seedHistoryMetric(t, corePath)
	historyPath := s.HistoryPath()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	moveMetricBackToLegacyCore(t, corePath, historyPath, true)
	reopened, err := Open(corePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var cpu float64
	if err := reopened.history.QueryRow("select cpu_usage_percent from server_metric_samples").Scan(&cpu); err != nil {
		t.Fatal(err)
	}
	if cpu != 42 {
		t.Fatalf("source value not restored: %v", cpu)
	}
}

func TestMissingHistoryDatabaseKeepsCoreUsable(t *testing.T) {
	corePath := filepath.Join(t.TempDir(), "oboard.sqlite")
	s, err := Open(corePath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	server := &model.Server{Name: "history-down", ListenIP: "0.0.0.0", Status: model.ServerOnline}
	if err := s.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	historyPath := s.HistoryPath()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(historyPath, historyPath+".offline"); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(historyPath+".offline", historyPath)
	reopened, err := Open(corePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.HistoryAvailable() {
		t.Fatal("missing history database reported available")
	}
	stored, err := reopened.GetServer(ctx, server.ID)
	if err != nil || stored.Name != server.Name {
		t.Fatalf("core server read failed: %#v %v", stored, err)
	}
	if _, err := reopened.ListServerMetricSamples(ctx, server.ID, 10); !errors.Is(err, ErrHistoryUnavailable) && !strings.Contains(err.Error(), "database is closed") {
		t.Fatalf("history read error = %v", err)
	}
}

func TestCoreEntityDeletionQueuesHistoryCleanupWhileHistoryIsOffline(t *testing.T) {
	corePath := filepath.Join(t.TempDir(), "oboard.sqlite")
	s, err := Open(corePath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	user := &model.User{Username: "cleanup-user", PasswordHash: "h", Role: model.RoleViewer, Status: "active"}
	if err := s.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	historyPath := s.HistoryPath()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(historyPath, historyPath+".offline"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(corePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Delete(ctx, "users", user.ID); err != nil {
		t.Fatal(err)
	}
	var queued int
	if err := reopened.db.QueryRowContext(ctx, `select count(*) from history_cleanup_queue where kind='user' and object_id=?`, user.ID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("queued user cleanup = %d err=%v", queued, err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(historyPath+".offline", historyPath); err != nil {
		t.Fatal(err)
	}
	recovered, err := Open(corePath)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if _, err := recovered.RunMaintenance(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := recovered.db.QueryRowContext(ctx, `select count(*) from history_cleanup_queue`).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("remaining cleanup queue = %d err=%v", queued, err)
	}
}

func seedHistoryMetric(t *testing.T, corePath string) (*Store, time.Time) {
	t.Helper()
	s, err := Open(corePath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	server := &model.Server{Name: "migrate", ListenIP: "0.0.0.0", Status: model.ServerOnline}
	if err := s.CreateServer(ctx, server); err != nil {
		s.Close()
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Minute)
	if _, err := s.SaveMetricReport(ctx, server.ID, model.MetricReport{ReportID: "migrate-metric", SampledAt: at, CPUUsagePercent: 42, MemoryUsedBytes: 4096, MemoryTotalBytes: 8192}); err != nil {
		s.Close()
		t.Fatal(err)
	}
	return s, at
}

func moveMetricBackToLegacyCore(t *testing.T, corePath, historyPath string, conflict bool) {
	t.Helper()
	db, err := sql.Open("sqlite", corePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	escaped := strings.ReplaceAll(historyPath, "'", "''")
	if _, err := db.Exec(`attach database '` + escaped + `' as hist`); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`detach database hist`)
	var schema string
	if err := db.QueryRow("select sql from hist.sqlite_master where name='server_metric_samples'").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("insert into main.server_metric_samples select * from hist.server_metric_samples"); err != nil {
		t.Fatal(err)
	}
	if conflict {
		if _, err := db.Exec(`update hist.server_metric_samples set cpu_usage_percent=cpu_usage_percent+1`); err != nil {
			t.Fatal(err)
		}
	} else if _, err := db.Exec(`delete from hist.server_metric_samples`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`update main.oboard_history_migration set status='copying' where id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`delete from main.oboard_history_migration_tables where table_name='server_metric_samples'`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`select count(*) from main.server_metric_samples`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("legacy metric rows = %d", count)
	}
}

func TestHistoryMigrationPreviousSchemaAndRollback(t *testing.T) {
	for _, completed := range []int{0, 1} {
		t.Run(strconv.Itoa(completed), func(t *testing.T) {
			corePath := filepath.Join(t.TempDir(), "oboard.sqlite")
			s, _ := seedHistoryMetric(t, corePath)
			hp := s.HistoryPath()
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", corePath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			schema, err := os.ReadFile("testdata/history_previous_schema.sql")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(string(schema)); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec("attach database ? as hist", hp); err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{
				"insert into main.server_metric_samples select * from hist.server_metric_samples",
				"update main.server_metric_samples set cpu_usage_percent=77",
				"insert into hist.server_metric_samples(id,server_id,sampled_at) values(9999,1,'2020-01-01T00:00:00Z')",
				"insert into main.latency_rollup_state(id,schema_version,generation,live_cursor,updated_at) values(1,1,23,300,'2026-10-24T00:00:00Z')",
				"insert into main.audit_rollup_state values(1,'cursor',1,'hourly',1,'2026-10-24T00:00:00Z')",
				"update main.oboard_history_migration set status='failed'",
			} {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec("update main.oboard_history_migration_tables set cursor=99999,copied=99999,complete=?", completed); err != nil {
				t.Fatal(err)
			}
			db.Close()
			reopened, err := Open(corePath)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			var n int
			var cpu float64
			if err := reopened.history.QueryRow("select count(*),max(cpu_usage_percent) from server_metric_samples").Scan(&n, &cpu); err != nil {
				t.Fatal(err)
			}
			if n != 1 || cpu != 77 {
				t.Fatalf("reconciled rows=%d cpu=%v", n, cpu)
			}
			var generation, cursor int
			if err := reopened.history.QueryRow("select generation,live_cursor from latency_rollup_state").Scan(&generation, &cursor); err != nil {
				t.Fatal(err)
			}
			if generation != 23 || cursor != 300 {
				t.Fatalf("lost latency state %d/%d", generation, cursor)
			}
			var backfill string
			if err := reopened.history.QueryRow("select backfill_cursor from audit_rollup_state").Scan(&backfill); err != nil {
				t.Fatal(err)
			}
			if backfill != "cursor" {
				t.Fatalf("lost audit state %q", backfill)
			}
		})
	}
}

func TestHistoryFailureDoesNotRollbackHealth(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(strconv.FormatBool(missing), func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "oboard.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx := context.Background()
			server := &model.Server{Name: "health", Status: model.ServerUnknown}
			if err := s.CreateServer(ctx, server); err != nil {
				t.Fatal(err)
			}
			if missing {
				s.disableHistory(ErrHistoryUnavailable)
			} else {
				if _, err := s.history.Exec("create trigger reject_metric before insert on server_metric_samples begin select raise(abort,'history write failed'); end"); err != nil {
					t.Fatal(err)
				}
			}
			at := time.Now().UTC()
			result, err := s.ApplyHealthReport(ctx, server.ID, healthReportFixture("", at), healthReportWindow(at))
			if err != nil {
				t.Fatal(err)
			}
			if result.SampleInserted {
				t.Fatal("failed sample reported inserted")
			}
			stored, err := s.GetServer(ctx, server.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != model.ServerOnline || stored.LastSeenAt == nil {
				t.Fatalf("health rolled back: %#v", stored)
			}
		})
	}
}

func TestServerIDReservedUntilHistoryCleanup(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "oboard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := &model.Server{Name: "old"}
	if err := s.CreateServer(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteServer(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	fresh := &model.Server{Name: "fresh"}
	if err := s.CreateServer(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if fresh.ID == old.ID {
		t.Fatal("reused pending history ID")
	}
	if _, err := s.SaveMetricReport(ctx, fresh.ID, model.MetricReport{ReportID: "new", SampledAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.drainHistoryCleanupQueue(ctx, 8); err != nil {
		t.Fatal(err)
	}
	points, err := s.ListServerMetricSamples(ctx, fresh.ID, 10)
	if err != nil || len(points) != 1 {
		t.Fatalf("new history lost %d %v", len(points), err)
	}
	replacement := &model.Server{Name: "replacement"}
	if err := s.CreateServer(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	if replacement.ID != old.ID {
		t.Fatalf("cleaned ID not reusable: %d", replacement.ID)
	}
}

func TestHistoryMigrationRecoversAfterBatchFailure(t *testing.T) {
	corePath := filepath.Join(t.TempDir(), "oboard.sqlite")
	s, _ := seedHistoryMetric(t, corePath)
	hp := s.HistoryPath()
	s.Close()
	moveMetricBackToLegacyCore(t, corePath, hp, false)
	db, err := sql.Open("sqlite", corePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("attach database ? as hist", hp); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"with recursive ids(id) as (select 2 union all select id+1 from ids where id<2505) insert into main.server_metric_samples(id,server_id,sampled_at) select id,1,'2026-10-24T00:00:00Z' from ids",
		"create trigger hist.interrupt_copy before insert on server_metric_samples when new.id=1500 begin select raise(abort,'simulated interruption'); end",
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	if broken, err := Open(corePath); err == nil {
		broken.Close()
		t.Fatal("expected interrupted migration")
	} else if !strings.Contains(err.Error(), "simulated interruption") {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", corePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("attach database ? as hist", hp); err != nil {
		t.Fatal(err)
	}
	var count, cursor int
	if err := db.QueryRow("select count(*) from hist.server_metric_samples").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("select cursor from main.oboard_history_migration_tables where table_name='server_metric_samples'").Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if count != 1000 || cursor != 1000 {
		t.Fatalf("partial progress count=%d cursor=%d", count, cursor)
	}
	// Model the old binary running retention after updater rollback.
	if _, err = db.Exec("delete from main.server_metric_samples where id<=100"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("drop trigger hist.interrupt_copy"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	recovered, err := Open(corePath)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if err := recovered.history.QueryRow("select count(*) from server_metric_samples").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2405 {
		t.Fatalf("recovered count=%d", count)
	}
}

func TestHistoryCleanupSkipsAlreadyReusedServer(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "oboard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	live := &model.Server{Name: "already-recycled"}
	if err := s.CreateServer(ctx, live); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveMetricReport(ctx, live.ID, model.MetricReport{ReportID: "new", SampledAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := s.QueueHistoryCleanup(ctx, "server", live.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.drainHistoryCleanupQueue(ctx, 8); err != nil {
		t.Fatal(err)
	}
	samples, err := s.ListServerMetricSamples(ctx, live.ID, 10)
	if err != nil || len(samples) != 1 {
		t.Fatalf("live server history changed: %d %v", len(samples), err)
	}
}

func BenchmarkHistoryMigrationLegacyTelemetry(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		corePath := filepath.Join(b.TempDir(), "oboard.sqlite")
		s, err := Open(corePath)
		if err != nil {
			b.Fatal(err)
		}
		server := &model.Server{Name: "migration-benchmark"}
		if err := s.CreateServer(context.Background(), server); err != nil {
			b.Fatal(err)
		}
		s.Close()
		db, err := sql.Open("sqlite", corePath)
		if err != nil {
			b.Fatal(err)
		}
		schema, err := os.ReadFile("testdata/history_previous_schema.sql")
		if err != nil {
			b.Fatal(err)
		}
		if _, err = db.Exec(string(schema)); err != nil {
			b.Fatal(err)
		}
		for _, q := range []string{
			"with recursive ids(id) as (select 1 union all select id+1 from ids where id<348778) insert into server_metric_samples(id,server_id,sampled_at) select id,1,'2026-10-24T00:00:00Z' from ids",
			"with recursive ids(id) as (select 1 union all select id+1 from ids where id<551678) insert into server_latency_probe_results(id,server_id,resource_version,probe_id,province,carrier,ip,checked_at,created_at) select id,1,'legacy',cast(id as text),'','','192.0.2.1','2026-10-24T00:00:00Z','2026-10-24T00:00:00Z' from ids",
			"update oboard_history_migration set status='pending'",
		} {
			if _, err = db.Exec(q); err != nil {
				b.Fatal(err)
			}
		}
		db.Close()
		b.StartTimer()
		s, err = Open(corePath)
		if err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		var count int
		if err := s.history.QueryRow("select count(*) from server_latency_probe_results").Scan(&count); err != nil {
			b.Fatal(err)
		}
		if count != 551678 {
			b.Fatalf("migrated %d", count)
		}
		s.Close()
	}
}

func TestHistoryCutoverCommitsSourceRemovalWithStatus(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "oboard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, q := range []string{
		"create table server_metric_samples(id integer primary key)",
		"update oboard_history_migration set status='copying'",
		"create trigger reject_cutover before update on oboard_history_migration when new.status='complete' begin select raise(abort,'cutover interrupted'); end",
	} {
		if _, err := s.db.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.finishHistoryMigration(ctx); err == nil {
		t.Fatal("expected cutover failure")
	}
	var n int
	if err := s.db.db.QueryRow("select count(*) from sqlite_master where type='table' and name='server_metric_samples'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("failed cutover removed source")
	}
	state, err := s.historyMigrationState(ctx)
	if err != nil || state.Status != "copying" {
		t.Fatalf("state=%v error=%v", state, err)
	}
	if _, err := s.db.db.Exec("drop trigger reject_cutover"); err != nil {
		t.Fatal(err)
	}
	if err := s.finishHistoryMigration(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.db.db.QueryRow("select count(*) from sqlite_master where type='table' and name='server_metric_samples'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	state, err = s.historyMigrationState(ctx)
	if err != nil || state.Status != "complete" || n != 0 {
		t.Fatalf("cutover state=%v source=%d error=%v", state, n, err)
	}
}

func TestProbeRefreshDoesNotHoldHistoryWhileWaitingForCore(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "oboard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	core, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before := s.db.db.Stats().WaitCount
	done := make(chan error, 1)
	go func() { done <- s.RefreshConnectionProbeEpisodes(ctx, 1, time.Now().UTC()) }()
	defer func() {
		core.Close()
		if err := <-done; err != nil {
			t.Errorf("refresh failed: %v", err)
		}
	}()
	for s.db.db.Stats().WaitCount == before {
		select {
		case <-ctx.Done():
			t.Fatal("refresh did not wait for core")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	historyCtx, historyCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer historyCancel()
	tx, err := s.history.BeginTx(historyCtx, nil)
	if err != nil {
		t.Fatalf("refresh held history writer while waiting for core: %v", err)
	}
	tx.Rollback()
}

func TestHistoryMigrationEscapesFilesystemPaths(t *testing.T) {
	corePath := filepath.Join(t.TempDir(), "core # 100%.sqlite")
	s, at := seedHistoryMetric(t, corePath)
	hp := s.HistoryPath()
	s.Close()
	moveMetricBackToLegacyCore(t, corePath, hp, false)
	s, err := Open(corePath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if sqliteFilesystemPath(s.path) == sqliteFilesystemPath(s.historyPath) {
		t.Fatal("history aliases core")
	}
	samples, err := s.ListServerMetricSamples(context.Background(), 1, 10)
	if err != nil || len(samples) != 1 || !samples[0].SampledAt.Equal(at) {
		t.Fatalf("lost samples: %v %v", samples, err)
	}
}

func TestCompletedCutoverWithLegacyTablesPreservesBothDatabases(t *testing.T) {
	corePath := filepath.Join(t.TempDir(), "oboard.sqlite")
	s, _ := seedHistoryMetric(t, corePath)
	hp := s.HistoryPath()
	s.Close()
	moveMetricBackToLegacyCore(t, corePath, hp, false)
	db, err := sql.Open("sqlite", corePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("update oboard_history_migration set status='complete'"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if reopened, err := Open(corePath); err == nil {
		reopened.Close()
		t.Fatal("ambiguous cutover accepted")
	} else if !strings.Contains(err.Error(), "preserve both databases") {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", corePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("select count(*) from server_metric_samples").Scan(&count); err != nil || count != 1 {
		t.Fatalf("source altered: count=%d err=%v", count, err)
	}
}

func TestStoreCloseReleasesHistoryConnections(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "oboard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.history.db.Ping(); err == nil {
		t.Fatal("history writer remains open")
	}
	if err := s.history.reader.Ping(); err == nil {
		t.Fatal("history reader remains open")
	}
}

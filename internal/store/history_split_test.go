package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
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

func TestHistoryMigrationRejectsSameIDDifferentContent(t *testing.T) {
	corePath := filepath.Join(t.TempDir(), "oboard.sqlite")
	s, _ := seedHistoryMetric(t, corePath)
	historyPath := s.HistoryPath()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	moveMetricBackToLegacyCore(t, corePath, historyPath, true)
	_, err := Open(corePath)
	if err == nil || !strings.Contains(err.Error(), "primary key exists with different content") {
		t.Fatalf("migration conflict error = %v", err)
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
	if _, err := db.Exec(`create table main.server_metric_samples as select * from hist.server_metric_samples`); err != nil {
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

package backup

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/store"
)

func TestBackupIncludesAndRestoresHistoryDatabase(t *testing.T) {
	root := t.TempDir()
	corePath := filepath.Join(root, "source.sqlite")
	source, err := store.Open(corePath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	server := &model.Server{Name: "history-backup", ListenIP: "0.0.0.0", Status: model.ServerOnline}
	if err := source.CreateServer(ctx, server); err != nil {
		source.Close()
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Minute)
	if _, err := source.SaveMetricReport(ctx, server.ID, model.MetricReport{ReportID: "backup-metric", SampledAt: at, CPUUsagePercent: 37}); err != nil {
		source.Close()
		t.Fatal(err)
	}
	manager, err := New(Config{
		Root: filepath.Join(root, "backups"), DatabasePath: corePath, HistoryDatabasePath: source.HistoryPath(),
		MasterSecret: "history-backup-secret-with-enough-length", SourceVersion: "test",
		Snapshot: func(ctx context.Context, dest string) error { return source.Backup(ctx, dest, store.BackupOptions{}) },
		HistorySnapshot: func(ctx context.Context, dest string) error {
			return source.BackupHistory(ctx, dest, store.BackupOptions{})
		},
	})
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	created, err := manager.Create(ctx, "recovery-password")
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if len(created.Manifest.Databases) != 2 || created.Manifest.Databases[0].Role != "core" || created.Manifest.Databases[1].Role != "history" {
		t.Fatalf("manifest databases = %#v", created.Manifest.Databases)
	}
	staged, err := manager.StageRestore(ctx, created.Path, "recovery-password", "test")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := store.OpenForRestorePair(filepath.Join(staged.StageRoot, "database.sqlite"), filepath.Join(staged.StageRoot, "history.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	samples, err := restored.ListServerMetricSamples(ctx, server.ID, 10)
	if err != nil || len(samples) != 1 || samples[0].CPUUsagePercent != 37 {
		t.Fatalf("restored history samples = %#v err=%v", samples, err)
	}
	if err := restored.CheckIntegrity(ctx); err != nil {
		t.Fatal(err)
	}
}

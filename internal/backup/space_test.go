package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestBackupSpaceRejectsBeforeSnapshotAndPreservesFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.sqlite")
	if err := os.WriteFile(source, []byte("source sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(root, "backups")
	called := false
	m, err := New(Config{Root: backups, DatabasePath: source, MasterSecret: "synthetic", Snapshot: func(context.Context, string) error { called = true; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(backups, "existing.obk")
	_ = os.WriteFile(existing, []byte("existing backup"), 0600)
	m.spaceProbe = func(string) (backupVolume, error) { return backupVolume{1, 1}, nil }
	if _, err := m.Create(context.Background(), "password"); err == nil || !strings.Contains(err.Error(), "磁盘空间不足") {
		t.Fatalf("err=%v", err)
	}
	if called {
		t.Fatal("snapshot began before preflight")
	}
	entries, _ := os.ReadDir(backups)
	if len(entries) != 1 {
		t.Fatal("preflight created temporary files")
	}
	raw, _ := os.ReadFile(source)
	old, _ := os.ReadFile(existing)
	if string(raw) != "source sentinel" || string(old) != "existing backup" {
		t.Fatal("preflight changed existing data")
	}
}
func TestBackupSpaceAccountsForDistinctVolumesAndPeakPhases(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source", "core.sqlite")
	history := filepath.Join(root, "source", "history.sqlite")
	_ = os.MkdirAll(filepath.Dir(source), 0700)
	for _, path := range []string{source, history} {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		err = file.Truncate(1 << 30)
		_ = file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	m := &Manager{config: Config{Root: filepath.Join(root, "output"), DatabasePath: source, HistoryDatabasePath: history, HistorySnapshot: func(context.Context, string) error { return nil }}}
	testProbe := func(sourceFree, outputFree, tempFree int64) func(string) (backupVolume, error) {
		return func(path string) (backupVolume, error) {
			if path == m.config.Root {
				return backupVolume{1, outputFree}, nil
			}
			if path == os.TempDir() {
				return backupVolume{2, tempFree}, nil
			}
			return backupVolume{3, sourceFree}, nil
		}
	}
	if err := m.checkFreeSpace(testProbe(1<<30, 5<<30, 2<<30)); err != nil {
		t.Fatal(err)
	}
	for _, probe := range []func(string) (backupVolume, error){testProbe(1, 5<<30, 2<<30), testProbe(1<<30, 3<<30, 2<<30), testProbe(1<<30, 5<<30, 1)} {
		if err := m.checkFreeSpace(probe); err == nil {
			t.Fatal("insufficient volume accepted")
		}
	}
	// On one volume, use max(archive peak, compaction peak), not their sum.
	if err := m.checkFreeSpace(func(string) (backupVolume, error) { return backupVolume{1, 5 << 30}, nil }); err != nil {
		t.Fatal(err)
	}
}
func TestBackupSnapshotENOSPCCleansOnlyCurrentWork(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.sqlite")
	_ = os.WriteFile(source, []byte("source"), 0600)
	m, err := New(Config{Root: filepath.Join(root, "backups"), DatabasePath: source, MasterSecret: "synthetic", Snapshot: func(_ context.Context, path string) error {
		_ = os.WriteFile(path, []byte("partial"), 0600)
		return syscall.ENOSPC
	}})
	if err != nil {
		t.Fatal(err)
	}
	m.spaceProbe = func(string) (backupVolume, error) { return backupVolume{1, 10 << 30}, nil }
	old := filepath.Join(m.Root(), "old.obk")
	_ = os.WriteFile(old, []byte("old"), 0600)
	if _, err := m.Create(context.Background(), "password"); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("err=%v", err)
	}
	entries, _ := os.ReadDir(m.Root())
	if len(entries) != 1 || entries[0].Name() != "old.obk" {
		t.Fatalf("leftovers=%v", entries)
	}
	raw, _ := os.ReadFile(source)
	if string(raw) != "source" {
		t.Fatal("source changed")
	}
}

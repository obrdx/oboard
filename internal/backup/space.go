package backup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/OboardProject/oboard/internal/store"
)

type backupVolume struct {
	id   uint64
	free int64
}
type backupSpaceNeed struct {
	path  string
	bytes int64
}

func probeBackupVolume(path string) (backupVolume, error) {
	for {
		var stat syscall.Stat_t
		err := syscall.Stat(path, &stat)
		if os.IsNotExist(err) {
			parent := filepath.Dir(path)
			if parent == path {
				return backupVolume{}, err
			}
			path = parent
			continue
		}
		if err != nil {
			return backupVolume{}, err
		}
		free, err := store.DiskFreeBytes(path)
		return backupVolume{id: uint64(stat.Dev), free: free}, err
	}
}

// Snapshots and the streamed age/gzip/tar envelope coexist in Root; rename
// publishes that same envelope, without a second compressed or encrypted copy.
// Compaction can also hold a vacuum database and rollback journal. Source
// volumes reserve WAL growth independently, then needs on one volume are summed.
func (m *Manager) ensureFreeSpace() error {
	probe := m.spaceProbe
	if probe == nil {
		probe = probeBackupVolume
	}
	return m.checkFreeSpace(probe)
}
func (m *Manager) checkFreeSpace(probe func(string) (backupVolume, error)) error {
	var snapshots, largest int64
	needs := []backupSpaceNeed{}
	paths := []string{m.config.DatabasePath}
	if m.config.HistorySnapshot != nil {
		paths = append(paths, m.config.HistoryDatabasePath)
	}
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			path = resolved
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("无法解析备份源文件：%w", err)
		}
		var size int64
		for _, name := range []string{path, path + "-wal"} {
			info, err := os.Stat(name)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("无法估算备份源文件空间：%w", err)
			}
			size += info.Size()
		}
		snapshots += size
		largest = max(largest, size)
		needs = append(needs, backupSpaceNeed{filepath.Dir(path), max(64<<20, size/10)})
	}
	var certificates int64
	if strings.TrimSpace(m.config.ACMEHome) != "" {
		err := filepath.WalkDir(m.config.ACMEHome, func(path string, entry fs.DirEntry, err error) error {
			if os.IsNotExist(err) && path == m.config.ACMEHome {
				return nil
			}
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				certificates += ((info.Size()+511)/512)*512 + 2048
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("无法估算证书备份空间：%w", err)
		}
	}
	payload := snapshots + certificates + 64<<10
	// Bound incompressible gzip/age framing rather than assuming compression.
	archive := payload + payload/100 + 1<<20
	phases := [][]backupSpaceNeed{
		{{m.config.Root, snapshots + archive}},
		{{m.config.Root, snapshots + largest}, {sqliteTempDirectory(), largest}},
	}
	type budget struct {
		path       string
		need, free int64
	}
	for _, phase := range phases {
		volumes := map[uint64]budget{}
		for _, need := range append(append([]backupSpaceNeed{}, needs...), phase...) {
			if need.bytes == 0 {
				continue
			}
			volume, err := probe(need.path)
			if err != nil {
				return fmt.Errorf("无法检查备份磁盘空间 %s：%w", need.path, err)
			}
			item := volumes[volume.id]
			item.path = need.path
			item.need += need.bytes
			item.free = volume.free
			volumes[volume.id] = item
		}
		for _, item := range volumes {
			required := item.need + max(128<<20, item.need/20)
			if item.free < required {
				return fmt.Errorf("磁盘空间不足：文件系统 %s 创建双库备份峰值需额外 %d 字节（含快照、归档、整理及增长余量），当前可用 %d 字节", item.path, required, item.free)
			}
		}
	}
	return nil
}

func sqliteTempDirectory() string {
	if directory := strings.TrimSpace(os.Getenv("SQLITE_TMPDIR")); directory != "" {
		return directory
	}
	return os.TempDir()
}

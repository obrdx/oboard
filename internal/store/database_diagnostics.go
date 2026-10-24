package store

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

const tableSpaceCacheTTL = 5 * time.Minute

type DatabaseHealth struct {
	Role              string         `json:"role"`
	Status            string         `json:"status"`
	Path              string         `json:"path"`
	SchemaVersion     int            `json:"schema_version"`
	DBBytes           int64          `json:"db_bytes"`
	WALBytes          int64          `json:"wal_bytes"`
	SHMBytes          int64          `json:"shm_bytes"`
	PageCount         int64          `json:"page_count"`
	PageSize          int64          `json:"page_size"`
	FreelistCount     int64          `json:"freelist_count"`
	Growth24hBytes    int64          `json:"growth_24h_bytes"`
	LastMaintenanceAt string         `json:"last_maintenance_at,omitempty"`
	LastMaintenance   map[string]any `json:"last_maintenance,omitempty"`
	Checkpoint        map[string]any `json:"checkpoint,omitempty"`
	LastError         string         `json:"last_error,omitempty"`
	OpenedAt          string         `json:"opened_at,omitempty"`
}

func (s *Store) databaseHealth(ctx context.Context, role, path string, db *countingDB, schemaVersion int, maintenanceAt string, maintenance map[string]any) DatabaseHealth {
	health := DatabaseHealth{Role: role, Status: "normal", Path: sqliteFilesystemPath(path), SchemaVersion: schemaVersion, LastMaintenanceAt: maintenanceAt, LastMaintenance: maintenance}
	if strings.TrimSpace(health.Path) != "" && !sqliteMemoryDatabase(path) {
		health.DBBytes = fileSizeOrZero(health.Path)
		health.WALBytes = fileSizeOrZero(health.Path + "-wal")
		health.SHMBytes = fileSizeOrZero(health.Path + "-shm")
	}
	if db != nil {
		if err := db.QueryRowContext(ctx, `pragma page_count`).Scan(&health.PageCount); err != nil {
			health.Status = "error"
			health.LastError = err.Error()
		}
		if err := db.QueryRowContext(ctx, `pragma page_size`).Scan(&health.PageSize); err != nil {
			health.Status = "error"
			health.LastError = err.Error()
		}
		if err := db.QueryRowContext(ctx, `pragma freelist_count`).Scan(&health.FreelistCount); err != nil {
			health.Status = "error"
			health.LastError = err.Error()
		}
	}
	if role == "history" {
		if !s.HistoryAvailable() {
			health.Status = "error"
			if err := s.lastHistoryError(); err != nil {
				health.LastError = err.Error()
			}
		} else if health.LastError == "" {
			var value int
			if err := s.history.QueryRowContext(ctx, `select 1 from oboard_history_meta where id=1`).Scan(&value); err != nil {
				health.Status = "error"
				health.LastError = err.Error()
			}
		}
	}
	health.Growth24hBytes = s.databaseGrowth24h(ctx, role, health.DBBytes+health.WALBytes)
	return health
}

func (s *Store) databaseGrowth24h(ctx context.Context, role string, current int64) int64 {
	if current <= 0 {
		return 0
	}
	nowTime := time.Now().UTC()
	var lastAt string
	_ = s.db.QueryRowContext(ctx, `select sampled_at from database_diagnostics_samples where role=? and db_bytes>0 order by sampled_at desc limit 1`, role).Scan(&lastAt)
	if lastAt == "" || nowTime.Sub(parseTime(lastAt)) >= time.Hour {
		_, _ = s.db.ExecContext(ctx, `insert into database_diagnostics_samples(sampled_at,role,db_bytes,wal_bytes) values(?,?,?,?)`, nowTime.Format(time.RFC3339Nano), role, current, 0)
		_, _ = s.db.ExecContext(ctx, `delete from database_diagnostics_samples where sampled_at<?`, nowTime.Add(-8*24*time.Hour).Format(time.RFC3339Nano))
	}
	var baseline int64
	err := s.db.QueryRowContext(ctx, `select db_bytes+wal_bytes from database_diagnostics_samples where role=? and db_bytes>0 and sampled_at<=? order by sampled_at desc limit 1`, role, nowTime.Add(-24*time.Hour).Format(time.RFC3339Nano)).Scan(&baseline)
	if err != nil || baseline <= 0 {
		return 0
	}
	return current - baseline
}

func (s *Store) collectTableSpace(ctx context.Context) ([]HistoryTableSpace, bool) {
	s.historySpaceMu.Lock()
	defer s.historySpaceMu.Unlock()
	if !s.coreSpaceAt.IsZero() && time.Since(s.coreSpaceAt) < tableSpaceCacheTTL {
		out := append([]HistoryTableSpace(nil), s.coreSpace...)
		out = append(out, s.historySpace...)
		return out, s.tableSpaceAvailable
	}
	core, coreOK := collectDatabaseTableSpace(ctx, s.db, "core")
	history := []HistoryTableSpace{}
	historyOK := false
	if s.history != nil {
		history, historyOK = collectDatabaseTableSpace(ctx, s.history, "history")
	}
	s.coreSpace = core
	s.historySpace = history
	s.coreSpaceAt = time.Now()
	s.tableSpaceAvailable = coreOK && (s.history == nil || historyOK)
	out := append([]HistoryTableSpace(nil), core...)
	out = append(out, history...)
	s.recordTableSpaceSnapshot(ctx, "core", core)
	s.recordTableSpaceSnapshot(ctx, "history", history)
	s.applyTableGrowth(ctx, "core", out)
	s.applyTableGrowth(ctx, "history", out)
	sort.Slice(out, func(i, j int) bool {
		return out[i].DataBytes+out[i].IndexBytes > out[j].DataBytes+out[j].IndexBytes
	})
	if len(out) > 30 {
		out = out[:30]
	}
	return out, s.tableSpaceAvailable
}

func (s *Store) recordTableSpaceSnapshot(ctx context.Context, role string, tables []HistoryTableSpace) {
	if len(tables) == 0 {
		return
	}
	nowTime := time.Now().UTC()
	var lastAt string
	_ = s.db.QueryRowContext(ctx, `select sampled_at from database_diagnostics_samples where role=? and table_stats_json<>'' order by sampled_at desc limit 1`, role).Scan(&lastAt)
	if lastAt != "" && nowTime.Sub(parseTime(lastAt)) < time.Hour {
		return
	}
	values := map[string]int64{}
	for _, item := range tables {
		values[item.Table] = item.DataBytes + item.IndexBytes
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return
	}
	_, _ = s.db.ExecContext(ctx, `insert into database_diagnostics_samples(sampled_at,role,table_stats_json) values(?,?,?)`, nowTime.Format(time.RFC3339Nano), role, string(raw))
}

func (s *Store) applyTableGrowth(ctx context.Context, role string, tables []HistoryTableSpace) {
	var raw string
	cutoff := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	if err := s.db.QueryRowContext(ctx, `select table_stats_json from database_diagnostics_samples where role=? and table_stats_json<>'' and sampled_at<=? order by sampled_at desc limit 1`, role, cutoff).Scan(&raw); err != nil || raw == "" {
		return
	}
	baseline := map[string]int64{}
	if json.Unmarshal([]byte(raw), &baseline) != nil {
		return
	}
	for i := range tables {
		if tables[i].Database != role {
			continue
		}
		if before, ok := baseline[tables[i].Table]; ok {
			tables[i].Growth24h = tables[i].DataBytes + tables[i].IndexBytes - before
		}
	}
}

func collectDatabaseTableSpace(ctx context.Context, db *countingDB, role string) ([]HistoryTableSpace, bool) {
	rows, err := db.QueryContext(ctx, `select name,sum(pgsize) from dbstat group by name`)
	if err != nil {
		return []HistoryTableSpace{}, false
	}
	defer rows.Close()
	sizes := map[string]int64{}
	for rows.Next() {
		var name string
		var bytes int64
		if err := rows.Scan(&name, &bytes); err != nil {
			return []HistoryTableSpace{}, false
		}
		sizes[name] = bytes
	}
	if err := rows.Err(); err != nil {
		return []HistoryTableSpace{}, false
	}
	metaRows, err := db.QueryContext(ctx, `select type,name,tbl_name from sqlite_master where type in ('table','index') and name not like 'sqlite_%'`)
	if err != nil {
		return []HistoryTableSpace{}, false
	}
	defer metaRows.Close()
	type object struct {
		kind, name, table string
	}
	objects := []object{}
	for metaRows.Next() {
		var item object
		if err := metaRows.Scan(&item.kind, &item.name, &item.table); err != nil {
			return []HistoryTableSpace{}, false
		}
		objects = append(objects, item)
	}
	if err := metaRows.Err(); err != nil {
		return []HistoryTableSpace{}, false
	}
	byTable := map[string]*HistoryTableSpace{}
	for _, item := range objects {
		if item.kind == "table" {
			byTable[item.name] = &HistoryTableSpace{Database: role, Table: item.name, DataBytes: sizes[item.name], Exact: false}
		}
	}
	for _, item := range objects {
		if item.kind != "index" {
			continue
		}
		if table := byTable[item.table]; table != nil {
			table.IndexBytes += sizes[item.name]
		}
	}
	out := make([]HistoryTableSpace, 0, len(byTable))
	for _, item := range byTable {
		item.RowCount = estimateTableRows(ctx, db, item.Table)
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].DataBytes+out[i].IndexBytes > out[j].DataBytes+out[j].IndexBytes
	})
	if len(out) > 30 {
		out = out[:30]
	}
	return out, true
}

func estimateTableRows(ctx context.Context, db *countingDB, table string) int64 {
	var count int64
	query := `select coalesce(max(rowid),0) from "` + strings.ReplaceAll(table, `"`, ``) + ``
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0
	}
	return count
}

func (s *Store) databasePerformance(role string, db *countingDB) DatabasePerformance {
	if db == nil {
		return DatabasePerformance{Role: role}
	}
	stats := db.Stats()
	return s.databaseMetrics.snapshot(role, stats.WaitCount, stats.WaitDuration, stats.OpenConnections, stats.InUse)
}

func maintenanceSnapshot(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out map[string]any
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

func databaseCheckpointSummary(settings map[string]string) map[string]any {
	if raw := strings.TrimSpace(settings[`database_last_maintenance_summary`]); raw != "" {
		return maintenanceSnapshot(raw)
	}
	return nil
}

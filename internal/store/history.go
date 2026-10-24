package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	historySchemaVersion        = 1
	historyMigrationBatchSize   = 1000
	historyCacheKB              = 4096
	historyMigrationStatusID    = 1
	historyMigrationTablePrefix = "oboard_history_migration"
)

var ErrHistoryUnavailable = errors.New("history database unavailable")

type HistoryTableSpace struct {
	Database   string `json:"database"`
	Table      string `json:"table"`
	DataBytes  int64  `json:"data_bytes"`
	IndexBytes int64  `json:"index_bytes"`
	RowCount   int64  `json:"row_count"`
	Exact      bool   `json:"exact"`
	Growth24h  int64  `json:"growth_24h_bytes"`
}

var historyTables = []string{
	"server_metric_samples",
	"server_metric_hourly",
	"server_metric_daily",
	"server_latency_probe_results",
	"latency_rollup_buckets",
	"latency_rollup_state",
	"connection_audit_reports",
	"connection_audit_hourly",
	"connection_audit_hourly_dimensions",
	"connection_audit_hourly_dirty",
	"audit_rollup_state",
}

var migratedHistoryTables = []string{
	"server_metric_samples",
	"server_latency_probe_results",
	"latency_rollup_buckets",
	"latency_rollup_state",
	"connection_audit_reports",
	"connection_audit_hourly",
	"connection_audit_hourly_dimensions",
	"connection_audit_hourly_dirty",
	"audit_rollup_state",
}

const historyMetadataSchema = `create table if not exists oboard_history_meta (
	id integer primary key check(id=1),
	schema_version integer not null,
	created_at text not null,
	updated_at text not null
)`

const historyMigrationSchema = `create table if not exists oboard_history_migration (
	id integer primary key check(id=1),
	schema_version integer not null,
	status text not null,
	started_at text not null default '',
	completed_at text not null default '',
	last_error text not null default '',
	updated_at text not null
)`

const historyMigrationTableSchema = `create table if not exists oboard_history_migration_tables (
	table_name text primary key,
	cursor integer not null default 0,
	copied integer not null default 0,
	verified integer not null default 0,
	complete integer not null default 0,
	last_error text not null default '',
	updated_at text not null
)`

var historySchemaStatements = []string{
	historyMetadataSchema,
	`create table if not exists server_metric_samples (
		id integer primary key autoincrement,
		server_id integer not null,
		cpu_usage_percent real not null default 0,
		memory_used_bytes integer not null default 0,
		memory_total_bytes integer not null default 0,
		disk_used_bytes integer not null default 0,
		disk_total_bytes integer not null default 0,
		tcp_connection_count integer not null default 0,
		udp_connection_count integer not null default 0,
		process_count integer not null default 0,
		resource_recorded integer not null default 1,
		network_upload_bps integer not null default 0,
		network_download_bps integer not null default 0,
		traffic_upload_bytes integer not null default 0,
		traffic_download_bytes integer not null default 0,
		connectivity_available integer not null default -1,
		connectivity_latency_ms integer not null default 0,
		sampled_at text not null
	)`,
	`create index if not exists idx_server_metric_samples_server_time on server_metric_samples(server_id, sampled_at desc)`,
	`create index if not exists idx_server_metric_samples_time on server_metric_samples(sampled_at)`,
	`create table if not exists server_metric_hourly (
		server_id integer not null,
		bucket_start text not null,
		sample_count integer not null default 0,
		resource_sample_count integer not null default 0,
		cpu_avg real not null default 0,
		cpu_min real,
		cpu_max real,
		memory_used_avg integer not null default 0,
		memory_used_min integer,
		memory_used_max integer,
		memory_total_avg integer not null default 0,
		memory_total_min integer,
		memory_total_max integer,
		disk_used_avg integer not null default 0,
		disk_used_min integer,
		disk_used_max integer,
		disk_total_avg integer not null default 0,
		disk_total_min integer,
		disk_total_max integer,
		tcp_avg integer not null default 0,
		tcp_min integer,
		tcp_max integer,
		udp_avg integer not null default 0,
		udp_min integer,
		udp_max integer,
		process_avg integer not null default 0,
		process_min integer,
		process_max integer,
		network_upload_avg integer not null default 0,
		network_upload_max integer not null default 0,
		network_download_avg integer not null default 0,
		network_download_max integer not null default 0,
		connectivity_known_count integer not null default 0,
		connectivity_available_count integer not null default 0,
		connectivity_unknown_count integer not null default 0,
		connectivity_latency_avg integer not null default 0,
		connectivity_latency_min integer,
		connectivity_latency_max integer,
		traffic_upload_last integer not null default 0,
		traffic_download_last integer not null default 0,
		last_sampled_at text not null default '',
		updated_at text not null,
		primary key(server_id, bucket_start)
	)`,
	`create index if not exists idx_server_metric_hourly_bucket on server_metric_hourly(bucket_start)`,
	`create table if not exists server_metric_daily (
		server_id integer not null,
		bucket_start text not null,
		sample_count integer not null default 0,
		resource_sample_count integer not null default 0,
		cpu_avg real not null default 0,
		cpu_min real,
		cpu_max real,
		memory_used_avg integer not null default 0,
		memory_used_min integer,
		memory_used_max integer,
		memory_total_avg integer not null default 0,
		memory_total_min integer,
		memory_total_max integer,
		disk_used_avg integer not null default 0,
		disk_used_min integer,
		disk_used_max integer,
		disk_total_avg integer not null default 0,
		disk_total_min integer,
		disk_total_max integer,
		tcp_avg integer not null default 0,
		tcp_min integer,
		tcp_max integer,
		udp_avg integer not null default 0,
		udp_min integer,
		udp_max integer,
		process_avg integer not null default 0,
		process_min integer,
		process_max integer,
		network_upload_avg integer not null default 0,
		network_upload_max integer not null default 0,
		network_download_avg integer not null default 0,
		network_download_max integer not null default 0,
		connectivity_known_count integer not null default 0,
		connectivity_available_count integer not null default 0,
		connectivity_unknown_count integer not null default 0,
		connectivity_latency_avg integer not null default 0,
		connectivity_latency_min integer,
		connectivity_latency_max integer,
		traffic_upload_last integer not null default 0,
		traffic_download_last integer not null default 0,
		last_sampled_at text not null default '',
		updated_at text not null,
		primary key(server_id, bucket_start)
	)`,
	`create index if not exists idx_server_metric_daily_bucket on server_metric_daily(bucket_start)`,
	`create table if not exists server_latency_probe_results (
		id integer primary key autoincrement,
		server_id integer not null,
		report_id text not null default '',
		resource_version text not null,
		probe_id text not null,
		kind text not null default 'regional',
		mode text not null default 'icmp',
		province text not null,
		carrier text not null,
		host text not null default '',
		ip text not null,
		port integer not null default 0,
		available integer not null default 0,
		latency_ms integer not null default 0,
		min_latency_ms integer not null default 0,
		p95_latency_ms integer not null default 0,
		jitter_ms integer not null default 0,
		sample_count integer not null default 0,
		success_count integer not null default 0,
		error text not null default '',
		checked_at text not null,
		created_at text not null,
		task_id integer not null default 0,
		task_name text not null default '',
		measurement_revision text not null default ''
	)`,
	`create unique index if not exists idx_server_latency_probe_results_report_target on server_latency_probe_results(server_id,report_id,probe_id) where report_id<>''`,
	`create index if not exists idx_server_latency_probe_results_server_checked on server_latency_probe_results(server_id,checked_at desc)`,
	`create index if not exists idx_server_latency_probe_results_checked on server_latency_probe_results(checked_at)`,
	`create index if not exists idx_server_latency_probe_results_server_kind_checked on server_latency_probe_results(server_id,kind,checked_at)`,
	`create table if not exists latency_rollup_state (
		id integer primary key check(id=1),
		schema_version integer not null,
		generation integer not null default 1,
		live_cursor integer not null default -1,
		historical_boundary integer not null default -1,
		backfill_cursor integer not null default -1,
		expired_unprocessed integer not null default 0,
		retention_floor integer not null default 0,
		initialized_at text not null default '',
		updated_at text not null default ''
	)`,
	`insert into latency_rollup_state(id,schema_version) values(1,1) on conflict(id) do nothing`,
	`create table if not exists latency_rollup_buckets (
		server_id integer not null,
		series_key text not null,
		measurement_revision text not null,
		revision_basis text not null default 'reported_endpoint_v1',
		resolution_seconds integer not null check(resolution_seconds in (300,3600)),
		bucket_start integer not null,
		kind text not null,
		task_id integer not null,
		probe_id text not null,
		mode text not null,
		task_name text not null,
		province text not null,
		carrier text not null,
		latency_sum integer not null,
		latency_value_count integer not null,
		latency_min integer,
		latency_max integer,
		latency_min_at integer,
		latency_max_at integer,
		curve_sum integer not null,
		curve_value_count integer not null,
		curve_min integer,
		curve_max integer,
		curve_min_at integer,
		curve_max_at integer,
		attempt_count integer not null,
		success_count integer not null,
		report_count integer not null,
		available_report_count integer not null,
		first_sample_at integer not null,
		last_sample_at integer not null,
		summary_schema_version integer not null,
		updated_at text not null,
		primary key(server_id,series_key,measurement_revision,resolution_seconds,bucket_start)
	)`,
	`create index if not exists idx_latency_rollup_bucket_time on latency_rollup_buckets(bucket_start)`,
	`create index if not exists idx_latency_rollup_task on latency_rollup_buckets(task_id,server_id)`,
	`create table if not exists connection_audit_reports (
		report_id text primary key,
		server_id integer not null,
		user_id integer not null,
		inbound_id integer,
		path_id integer,
		device_id_hash text not null default '',
		credential_epoch integer not null default 0,
		client_instance_id_hash text not null default '',
		source_ip text not null,
		route_id text not null default '',
		source_geo_code text not null default '',
		source_country_code text not null default '',
		source_country text not null default '',
		source_province text not null default '',
		source_city text not null default '',
		source_isp text not null default '',
		geo_database_revision text not null default '',
		network text not null,
		destination text not null default '',
		destination_port integer not null default 0,
		outbound_tag text not null default '',
		outbound_type text not null default '',
		connection_count integer not null,
		closed_count integer not null default 0,
		duration_total_ms integer not null default 0,
		duration_max_ms integer not null default 0,
		upload_bytes integer not null default 0,
		download_bytes integer not null default 0,
		payload_first_at text,
		payload_last_at text,
		duration_le_1s_count integer not null default 0,
		duration_le_5s_count integer not null default 0,
		duration_le_20s_count integer not null default 0,
		duration_gt_20s_count integer not null default 0,
		probe_state text not null default '',
		internal_probe integer not null default 0,
		presence_sequence integer not null default 0,
		active_peak integer not null default 0,
		active_at_end integer not null default 0,
		collection_generation integer not null default 0,
		bucket_capacity integer not null default 1,
		dropped_bucket_count integer not null default 0,
		collection_started_at text not null,
		collection_ended_at text not null,
		started_at text not null,
		ended_at text not null,
		created_at text not null
	)`,
	`create index if not exists idx_connection_audit_server_time on connection_audit_reports(server_id, ended_at desc)`,
	`create index if not exists idx_connection_audit_time on connection_audit_reports(ended_at desc)`,
	`create index if not exists idx_connection_audit_route_time on connection_audit_reports(route_id,ended_at desc)`,
	`create index if not exists idx_connection_audit_inbound on connection_audit_reports(inbound_id) where inbound_id is not null`,
	`create index if not exists idx_connection_audit_path on connection_audit_reports(path_id) where path_id is not null`,
	`create index if not exists idx_connection_audit_user_window on connection_audit_reports(user_id, ended_at desc, source_ip, server_id, connection_count, active_peak)`,
	`create index if not exists idx_connection_audit_source_window on connection_audit_reports(source_ip, ended_at desc, user_id)`,
	`create table if not exists connection_audit_hourly (
		user_id integer not null,
		utc_hour text not null,
		connection_count integer not null default 0,
		algorithm_version integer not null default 1,
		source_watermark text not null default '',
		updated_at text not null,
		report_count integer not null default 0,
		active_peak integer not null default 0,
		upload_bytes integer not null default 0,
		download_bytes integer not null default 0,
		last_ended_at text not null default '',
		collection_count integer not null default 0,
		dropped_bucket_count integer not null default 0,
		capacity_total integer not null default 0,
		dimensions_complete integer not null default 1,
		totals_version integer not null default 0,
		primary key(user_id, utc_hour)
	)`,
	`create index if not exists idx_connection_audit_hourly_hour on connection_audit_hourly(utc_hour)`,
	`create table if not exists connection_audit_hourly_dimensions (
		user_id integer not null,
		utc_hour text not null,
		dimension text not null,
		value text not null,
		primary key(user_id, utc_hour, dimension, value)
	)`,
	`create index if not exists idx_connection_audit_hourly_dimensions_window on connection_audit_hourly_dimensions(user_id, dimension, utc_hour)`,
	`create table if not exists connection_audit_hourly_dirty (
		user_id integer not null,
		utc_hour text not null,
		dirty_at text not null,
		primary key(user_id, utc_hour)
	)`,
	`create index if not exists idx_connection_audit_hourly_dirty_at on connection_audit_hourly_dirty(dirty_at)`,
	`create table if not exists audit_rollup_state (
		id integer primary key check(id=1),
		backfill_cursor text not null default '',
		backfill_complete integer not null default 0,
		read_path text not null default 'raw',
		algorithm_version integer not null default 1,
		updated_at text not null
	)`,
}

var historyColumnMigrations = []struct {
	table, name, statement string
}{
	{"server_latency_probe_results", "report_id", `alter table server_latency_probe_results add column report_id text not null default ''`},
	{"server_latency_probe_results", "kind", `alter table server_latency_probe_results add column kind text not null default 'regional'`},
	{"server_latency_probe_results", "mode", `alter table server_latency_probe_results add column mode text not null default 'icmp'`},
	{"server_latency_probe_results", "host", `alter table server_latency_probe_results add column host text not null default ''`},
	{"server_latency_probe_results", "port", `alter table server_latency_probe_results add column port integer not null default 0`},
	{"server_latency_probe_results", "task_id", `alter table server_latency_probe_results add column task_id integer not null default 0`},
	{"server_latency_probe_results", "task_name", `alter table server_latency_probe_results add column task_name text not null default ''`},
	{"server_latency_probe_results", "measurement_revision", `alter table server_latency_probe_results add column measurement_revision text not null default ''`},
	{"connection_audit_hourly", "report_count", `alter table connection_audit_hourly add column report_count integer not null default 0`},
	{"connection_audit_hourly", "active_peak", `alter table connection_audit_hourly add column active_peak integer not null default 0`},
	{"connection_audit_hourly", "upload_bytes", `alter table connection_audit_hourly add column upload_bytes integer not null default 0`},
	{"connection_audit_hourly", "download_bytes", `alter table connection_audit_hourly add column download_bytes integer not null default 0`},
	{"connection_audit_hourly", "last_ended_at", `alter table connection_audit_hourly add column last_ended_at text not null default ''`},
	{"connection_audit_hourly", "collection_count", `alter table connection_audit_hourly add column collection_count integer not null default 0`},
	{"connection_audit_hourly", "dropped_bucket_count", `alter table connection_audit_hourly add column dropped_bucket_count integer not null default 0`},
	{"connection_audit_hourly", "capacity_total", `alter table connection_audit_hourly add column capacity_total integer not null default 0`},
	{"connection_audit_hourly", "dimensions_complete", `alter table connection_audit_hourly add column dimensions_complete integer not null default 1`},
	{"connection_audit_hourly", "totals_version", `alter table connection_audit_hourly add column totals_version integer not null default 0`},
}

// HistoryPathFor returns the adjacent history database for a Controller core
// database path. A custom core path keeps the same directory and filename stem.
func HistoryPathFor(corePath string) string {
	corePath = strings.TrimSpace(corePath)
	if corePath == ":memory:" || strings.Contains(corePath, ":memory:") || strings.Contains(corePath, "mode=memory") {
		return ":memory:"
	}
	raw := corePath
	query := ""
	if strings.HasPrefix(raw, "file:") {
		if parsed, err := url.Parse(raw); err == nil {
			raw = parsed.Path
			if raw == "" {
				raw = parsed.Opaque
			}
			query = parsed.RawQuery
		}
	}
	ext := filepath.Ext(raw)
	stem := strings.TrimSuffix(raw, ext)
	if ext == "" {
		ext = ".sqlite"
	}
	history := stem + "-history" + ext
	if query != "" {
		return "file:" + history + "?" + query
	}
	return history
}

func historySQLiteOptions(base SQLiteOptions) SQLiteOptions {
	opts := base
	opts.MaxOpenConns = 2
	opts.MaxIdleConns = 2
	opts.CacheKB = historyCacheKB
	return opts
}

func (s *Store) ensureHistoryMetadata(ctx context.Context) error {
	for _, statement := range []string{historyMigrationSchema, historyMigrationTableSchema} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	nowText := now()
	_, err := s.db.ExecContext(ctx, `insert into oboard_history_migration(id,schema_version,status,updated_at) values(? ,?, 'pending', ?) on conflict(id) do nothing`, historyMigrationStatusID, historySchemaVersion, nowText)
	return err
}

type historyMigrationState struct {
	SchemaVersion int
	Status        string
	StartedAt     string
	CompletedAt   string
	LastError     string
}

func (s *Store) historyMigrationState(ctx context.Context) (historyMigrationState, error) {
	var state historyMigrationState
	err := s.db.QueryRowContext(ctx, `select schema_version,status,started_at,completed_at,last_error from oboard_history_migration where id=?`, historyMigrationStatusID).
		Scan(&state.SchemaVersion, &state.Status, &state.StartedAt, &state.CompletedAt, &state.LastError)
	return state, err
}

func (s *Store) setHistoryMigrationStatus(ctx context.Context, status, message string) error {
	nowText := now()
	startedAt := ""
	completedAt := ""
	if status == "copying" || status == "verifying" {
		startedAt = nowText
	}
	if status == "complete" {
		completedAt = nowText
	}
	_, err := s.db.ExecContext(ctx, `insert into oboard_history_migration(id,schema_version,status,started_at,completed_at,last_error,updated_at)
		values(?,?,?,?,?,?,?)
		on conflict(id) do update set schema_version=excluded.schema_version,status=excluded.status,
		started_at=case when oboard_history_migration.started_at='' then excluded.started_at else oboard_history_migration.started_at end,
		completed_at=case when excluded.completed_at<>'' then excluded.completed_at else oboard_history_migration.completed_at end,
		last_error=excluded.last_error,updated_at=excluded.updated_at`,
		historyMigrationStatusID, historySchemaVersion, status, startedAt, completedAt, message, nowText)
	return err
}

func (s *Store) legacyHistoryTablesPresent(ctx context.Context) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `select name from sqlite_master where type='table' and name in (`+inClause(len(migratedHistoryTables))+`)`, seenAnyString(migratedHistoryTables)...)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), nil
}

func seenAnyString(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

func isHistoryTable(table string) bool {
	for _, candidate := range historyTables {
		if candidate == table {
			return true
		}
	}
	return false
}

func queryInt64sDB(ctx context.Context, db latencyHistoryQueryer, query string, args ...any) ([]int64, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var value int64
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) maintenanceDB(query string) *countingDB {
	for _, table := range historyTables {
		if strings.Contains(query, " "+table) {
			return s.history
		}
	}
	return s.db
}

func (s *Store) dropLegacyHistoryTables(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range migratedHistoryTables {
		if _, err := tx.ExecContext(ctx, `drop table if exists `+table); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) openHistory(ctx context.Context, base SQLiteOptions, restore, allowHistoryReset bool) error {
	if strings.TrimSpace(s.historyPath) == "" {
		s.recordHistoryError(errors.New("history database path is empty"))
		if restore {
			return ErrHistoryUnavailable
		}
		return nil
	}
	state, err := s.historyMigrationState(ctx)
	if err != nil {
		s.recordHistoryError(err)
		if restore {
			return err
		}
		return err
	}
	complete := state.Status == "complete"
	if complete {
		if !historyFileExists(s.historyPath) && !sqliteMemoryDatabase(s.historyPath) {
			if restore && allowHistoryReset {
				complete = false
			} else {
				s.disableHistory(errors.New("history database file is missing after migration completion"))
				if restore {
					return s.lastHistoryError()
				}
				log.Printf("history database unavailable: file is missing; historical features are disabled")
				return nil
			}
		}
		if complete {
			if err := s.openHistoryExisting(ctx, base, restore); err != nil {
				s.disableHistory(err)
				if restore {
					return err
				}
				log.Printf("history database unavailable: %v; historical features are disabled", err)
				return nil
			}
			if err := s.dropLegacyHistoryTables(ctx); err != nil {
				return err
			}
			if err := s.retireAuditDestinations(ctx); err != nil {
				return err
			}
			return nil
		}
	}

	history, err := openHistorySQLite(s.historyPath, historySQLiteOptions(base), restore)
	if err != nil {
		s.disableHistory(err)
		return fmt.Errorf("open history database: %w", err)
	}
	s.history = history
	s.history.role = "history"
	s.history.metrics = s.databaseMetrics
	if err := s.ensureHistorySchema(ctx); err != nil {
		_ = history.Close()
		s.history = nil
		s.recordHistoryError(err)
		return fmt.Errorf("initialize history database: %w", err)
	}
	if err := s.setHistoryMigrationStatus(ctx, "copying", ""); err != nil {
		return err
	}
	if err := s.migrateHistoryTables(ctx); err != nil {
		_ = s.setHistoryMigrationStatus(ctx, "failed", err.Error())
		s.recordHistoryError(err)
		return fmt.Errorf("migrate history database: %w", err)
	}
	var check string
	if err := s.history.QueryRowContext(ctx, `pragma quick_check`).Scan(&check); err != nil {
		_ = s.setHistoryMigrationStatus(ctx, "failed", err.Error())
		return fmt.Errorf("verify history database: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(check), "ok") {
		err := fmt.Errorf("history database quick_check failed: %s", check)
		_ = s.setHistoryMigrationStatus(ctx, "failed", err.Error())
		return err
	}
	if err := s.setHistoryMigrationStatus(ctx, "complete", ""); err != nil {
		return err
	}
	if err := s.dropLegacyHistoryTables(ctx); err != nil {
		return err
	}
	if err := s.attachHistoryReader(base); err != nil {
		_ = history.Close()
		s.history = nil
		s.recordHistoryError(err)
		return err
	}
	s.db.history = s.history
	s.historyReady = true
	if err := s.retireAuditDestinations(ctx); err != nil {
		return err
	}
	s.historySchemaVersion = historySchemaVersion
	s.historyOpenedAt = time.Now().UTC()
	return nil
}

func (s *Store) openHistoryExisting(ctx context.Context, base SQLiteOptions, restore bool) error {
	history, err := openHistorySQLite(s.historyPath, historySQLiteOptions(base), restore)
	if err != nil {
		return err
	}
	s.history = history
	s.history.role = "history"
	s.history.metrics = s.databaseMetrics
	s.db.history = history
	var version int
	if err := history.QueryRowContext(ctx, `select schema_version from oboard_history_meta where id=1`).Scan(&version); err != nil {
		_ = history.Close()
		s.history = nil
		return fmt.Errorf("read history schema version: %w", err)
	}
	if version > historySchemaVersion {
		_ = history.Close()
		s.history = nil
		return fmt.Errorf("history schema version %d is newer than this Controller supports (%d)", version, historySchemaVersion)
	}
	missing, err := s.missingHistoryTables(ctx)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		_ = history.Close()
		s.history = nil
		return fmt.Errorf("history database is missing required tables: %s", strings.Join(missing, ", "))
	}
	if err := s.ensureHistorySchema(ctx); err != nil {
		_ = history.Close()
		s.history = nil
		return err
	}
	var check string
	if err := history.QueryRowContext(ctx, `pragma quick_check`).Scan(&check); err != nil {
		_ = history.Close()
		s.history = nil
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(check), "ok") {
		_ = history.Close()
		s.history = nil
		return fmt.Errorf("history database quick_check failed: %s", check)
	}
	if err := s.attachHistoryReader(base); err != nil {
		_ = history.Close()
		s.history = nil
		return err
	}
	s.historySchemaVersion = version
	s.historyOpenedAt = time.Now().UTC()
	s.historyReady = true
	return nil
}

func (s *Store) missingHistoryTables(ctx context.Context) ([]string, error) {
	if s.history == nil {
		return nil, ErrHistoryUnavailable
	}
	rows, err := s.history.QueryContext(ctx, `select name from sqlite_master where type='table'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	present := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	missing := []string{}
	required := []string{"oboard_history_meta", "server_metric_samples", "server_latency_probe_results", "connection_audit_reports"}
	for _, table := range required {
		if !present[table] {
			missing = append(missing, table)
		}
	}
	return missing, nil
}

func (s *Store) ensureHistorySchema(ctx context.Context) error {
	if s.history == nil {
		return ErrHistoryUnavailable
	}
	for _, column := range historyColumnMigrations {
		exists, err := s.tableExistsIn(ctx, s.history, column.table)
		if err != nil {
			return err
		}
		if exists {
			if err := s.ensureHistoryColumn(ctx, column.table, column.name, column.statement); err != nil {
				return err
			}
		}
	}
	for _, statement := range historySchemaStatements {
		if _, err := s.history.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for _, column := range historyColumnMigrations {
		if err := s.ensureHistoryColumn(ctx, column.table, column.name, column.statement); err != nil {
			return err
		}
	}
	nowText := now()
	_, err := s.history.ExecContext(ctx, `insert into oboard_history_meta(id,schema_version,created_at,updated_at) values(1,?,?,?)
		on conflict(id) do update set schema_version=excluded.schema_version,updated_at=excluded.updated_at`, historySchemaVersion, nowText, nowText)
	if err != nil {
		return err
	}
	_, err = s.history.ExecContext(ctx, `insert into audit_rollup_state(id,backfill_cursor,backfill_complete,read_path,algorithm_version,updated_at)
		values(1,'',0,'raw',?,?) on conflict(id) do nothing`, connectionAuditHourlyAlgorithm, nowText)
	return err
}

func (s *Store) ensureHistoryColumn(ctx context.Context, table, column, statement string) error {
	var count int
	if err := s.history.QueryRowContext(ctx, `select count(*) from pragma_table_info(?) where name=?`, table, column).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := s.history.ExecContext(ctx, statement)
	return err
}

func (s *Store) attachHistoryReader(base SQLiteOptions) error {
	if s.history == nil || sqliteMemoryDatabase(s.historyPath) {
		return nil
	}
	opts := historySQLiteOptions(base)
	dsn, err := sqliteDSN(s.historyPath, opts.BusyTimeout, opts.CacheKB, false)
	if err != nil {
		return err
	}
	reader, err := sql.Open("sqlite", dsn+`&_pragma=query_only(1)`)
	if err != nil {
		return err
	}
	reader.SetMaxOpenConns(opts.MaxOpenConns)
	reader.SetMaxIdleConns(opts.MaxIdleConns)
	if err := reader.PingContext(context.Background()); err != nil {
		_ = reader.Close()
		return err
	}
	s.history.reader = reader
	return nil
}

func openHistorySQLite(path string, opts SQLiteOptions, restore bool) (*countingDB, error) {
	if !sqliteMemoryDatabase(path) {
		if dir := filepath.Dir(path); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, err
			}
		}
	}
	dsn, err := sqliteDSN(path, opts.BusyTimeout, opts.CacheKB, restore)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if !sqliteMemoryDatabase(path) {
		journalMode := "WAL"
		if restore {
			journalMode = "DELETE"
		}
		var active string
		if err := db.QueryRowContext(ctx, `pragma journal_mode=`+journalMode).Scan(&active); err != nil {
			_ = db.Close()
			return nil, err
		}
		if !strings.EqualFold(active, journalMode) {
			_ = db.Close()
			return nil, fmt.Errorf("set history journal mode %s: active mode is %s", journalMode, active)
		}
		if !restore {
			if err := enableIncrementalVacuum(ctx, db); err != nil {
				log.Printf("history incremental vacuum unavailable: %v", err)
			}
		}
	}
	if err := secureSQLiteFile(path); err != nil {
		_ = db.Close()
		return nil, err
	}
	return newCountingDB(db), nil
}

type historyTableColumn struct {
	name string
	pk   int
}

func (s *Store) migrateHistoryTables(ctx context.Context) error {
	for _, table := range migratedHistoryTables {
		if err := s.migrateHistoryTable(ctx, table); err != nil {
			return err
		}
	}
	return s.setHistoryMigrationStatus(ctx, "verifying", "")
}

func (s *Store) migrateHistoryTable(ctx context.Context, table string) error {
	exists, err := s.tableExists(ctx, table)
	if err != nil {
		return err
	}
	if !exists {
		_, err := s.db.ExecContext(ctx, `insert into oboard_history_migration_tables(table_name,complete,updated_at) values(?,1,?)
			on conflict(table_name) do update set complete=1,last_error='',updated_at=excluded.updated_at`, table, now())
		return err
	}
	columns, err := s.tableColumns(ctx, s.db, table)
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		return fmt.Errorf("history migration source table %s has no columns", table)
	}
	destColumns, err := s.tableColumns(ctx, s.history, table)
	if err != nil {
		return err
	}
	destNames := make(map[string]bool, len(destColumns))
	for _, column := range destColumns {
		destNames[column.name] = true
	}
	for _, column := range columns {
		if !destNames[column.name] {
			return fmt.Errorf("history migration schema mismatch for %s: target is missing column %s", table, column.name)
		}
	}
	var cursor int64
	var complete int
	err = s.db.QueryRowContext(ctx, `select cursor,complete from oboard_history_migration_tables where table_name=?`, table).Scan(&cursor, &complete)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := s.db.ExecContext(ctx, `insert into oboard_history_migration_tables(table_name,updated_at) values(?,?)`, table, now()); err != nil {
			return err
		}
		complete = 0
	} else if err != nil {
		return err
	}
	if complete != 0 {
		return nil
	}
	columnNames := make([]string, len(columns))
	for i := range columns {
		columnNames[i] = columns[i].name
	}
	selectSQL := `select rowid,` + strings.Join(columnNames, ",") + ` from ` + table + ` where rowid>? order by rowid limit ?`
	insertSQL := `insert or ignore into ` + table + `(` + strings.Join(columnNames, ",") + `) values(` + strings.TrimSuffix(strings.Repeat("?,", len(columnNames)), ",") + `)`
	verifySQL := historyVerifySQL(table, columns)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := s.db.QueryContext(ctx, selectSQL, cursor, historyMigrationBatchSize)
		if err != nil {
			return err
		}
		tx, err := s.history.BeginTx(ctx, nil)
		if err != nil {
			_ = rows.Close()
			return err
		}
		lastCursor := cursor
		batchRows := 0
		for rows.Next() {
			values := make([]any, len(columns)+1)
			pointers := make([]any, len(values))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				_ = rows.Close()
				_ = tx.Rollback()
				return err
			}
			lastCursor = values[0].(int64)
			row := values[1:]
			if _, err := tx.ExecContext(ctx, insertSQL, row...); err != nil {
				_ = rows.Close()
				_ = tx.Rollback()
				return fmt.Errorf("copy %s rowid %d: %w", table, lastCursor, err)
			}
			verifyArgs := append(append([]any(nil), row...), row...)
			var matches int
			if err := tx.QueryRowContext(ctx, verifySQL, verifyArgs...).Scan(&matches); err != nil {
				_ = rows.Close()
				_ = tx.Rollback()
				return fmt.Errorf("verify %s rowid %d: %w", table, lastCursor, err)
			}
			if matches != 1 {
				_ = rows.Close()
				_ = tx.Rollback()
				return fmt.Errorf("history migration conflict in %s at rowid %d: primary key exists with different content", table, lastCursor)
			}
			batchRows++
		}
		readErr := rows.Err()
		_ = rows.Close()
		if readErr != nil {
			_ = tx.Rollback()
			return readErr
		}
		if batchRows == 0 {
			_ = tx.Rollback()
			break
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		cursor = lastCursor
		if _, err := s.db.ExecContext(ctx, `update oboard_history_migration_tables set cursor=?,copied=copied+?,updated_at=? where table_name=?`, cursor, batchRows, now(), table); err != nil {
			return err
		}
	}
	var sourceCount, targetCount int64
	if err := s.db.QueryRowContext(ctx, `select count(*) from `+table).Scan(&sourceCount); err != nil {
		return err
	}
	if err := s.history.QueryRowContext(ctx, `select count(*) from `+table).Scan(&targetCount); err != nil {
		return err
	}
	if sourceCount != targetCount {
		return fmt.Errorf("history migration count mismatch for %s: source=%d target=%d", table, sourceCount, targetCount)
	}
	_, err = s.db.ExecContext(ctx, `update oboard_history_migration_tables set complete=1,verified=?,last_error='',updated_at=? where table_name=?`, sourceCount, now(), table)
	return err
}

func historyVerifySQL(table string, columns []historyTableColumn) string {
	parts := make([]string, len(columns))
	for i := range columns {
		parts[i] = columns[i].name + ` is ?`
	}
	return `select count(*) from ` + table + ` where ` + strings.Join(parts, ` and `)
}

func (s *Store) tableExistsIn(ctx context.Context, db *countingDB, table string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `select count(*) from sqlite_master where type='table' and name=?`, table).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) tableColumns(ctx context.Context, db *countingDB, table string) ([]historyTableColumn, error) {
	rows, err := db.QueryContext(ctx, `pragma table_info(`+strconv.Quote(table)+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []historyTableColumn{}
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		out = append(out, historyTableColumn{name: name, pk: pk})
	}
	return out, rows.Err()
}

func (s *Store) recordHistoryError(err error) {
	if err == nil {
		return
	}
	s.historyLastErrorMu.Lock()
	s.historyLastError = err.Error()
	s.historyLastErrorAt = time.Now().UTC()
	s.historyLastErrorMu.Unlock()
}

func (s *Store) lastHistoryError() error {
	s.historyLastErrorMu.RLock()
	defer s.historyLastErrorMu.RUnlock()
	if strings.TrimSpace(s.historyLastError) == "" {
		return ErrHistoryUnavailable
	}
	return fmt.Errorf("%w: %s", ErrHistoryUnavailable, s.historyLastError)
}

func (s *Store) historyDB() (*countingDB, error) {
	if s.history == nil || !s.historyReady {
		return nil, s.lastHistoryError()
	}
	return s.history, nil
}

func (s *Store) HistoryPath() string {
	return s.historyPath
}

func (s *Store) HistoryAvailable() bool {
	return s.history != nil && s.historyReady
}

func (s *Store) disableHistory(err error) {
	s.recordHistoryError(err)
	if s.history != nil {
		_ = s.history.Close()
	}
	closed, openErr := sql.Open("sqlite", ":memory:")
	if openErr == nil {
		_ = closed.Close()
		s.history = newCountingDB(closed)
		s.history.role = "history"
		s.history.metrics = s.databaseMetrics
	}
	if s.db != nil {
		s.db.history = s.history
	}
	s.historyReady = false
}

func sqliteFileExists(path string) bool {
	return historyFileExists(path)
}

func historyFileExists(path string) bool {
	info, err := os.Stat(sqliteFilesystemPath(path))
	return err == nil && info.Mode().IsRegular()
}

func (s *Store) deleteUserAuditHistory(ctx context.Context, userID int64) error {
	history, err := s.historyDB()
	if err != nil {
		return err
	}
	tx, err := history.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`delete from connection_audit_reports where user_id=?`,
		`delete from connection_audit_hourly_dimensions where user_id=?`,
		`delete from connection_audit_hourly where user_id=?`,
		`delete from connection_audit_hourly_dirty where user_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) QueueHistoryCleanup(ctx context.Context, kind string, objectID int64) error {
	kind = strings.TrimSpace(kind)
	if kind == "" || objectID <= 0 {
		return errors.New("invalid history cleanup target")
	}
	_, err := s.db.ExecContext(ctx, `insert into history_cleanup_queue(kind,object_id,created_at) values(?,?,?)
		on conflict(kind,object_id) do update set last_error=''`, kind, objectID, now())
	return err
}

func (s *Store) drainHistoryCleanupQueue(ctx context.Context, limit int) (int, error) {
	history, err := s.historyDB()
	if err != nil {
		return 0, err
	}
	if limit <= 0 {
		limit = 8
	}
	rows, err := s.db.QueryContext(ctx, `select kind,object_id from history_cleanup_queue order by created_at limit ?`, limit)
	if err != nil {
		return 0, err
	}
	type cleanupTarget struct {
		kind string
		id   int64
	}
	targets := []cleanupTarget{}
	for rows.Next() {
		var item cleanupTarget
		if err := rows.Scan(&item.kind, &item.id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		targets = append(targets, item)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	processed := 0
	for _, target := range targets {
		done := false
		var cleanupErr error
		switch target.kind {
		case "server":
			removed := 0
			for batch := 0; batch < 8; batch++ {
				n, err := s.PurgeServerHistoryBatch(ctx, target.id, 2000)
				if err != nil {
					cleanupErr = err
					break
				}
				removed += n
				if n == 0 {
					done = true
					break
				}
			}
			_ = removed
		case "user":
			cleanupErr = s.deleteUserAuditHistory(ctx, target.id)
			done = cleanupErr == nil
		case "inbound":
			_, cleanupErr = history.ExecContext(ctx, `update connection_audit_reports set inbound_id=null where inbound_id=?`, target.id)
			done = cleanupErr == nil
		case "proxy_path":
			_, cleanupErr = history.ExecContext(ctx, `update connection_audit_reports set path_id=null where path_id=?`, target.id)
			done = cleanupErr == nil
		default:
			done = true
		}
		if done {
			if _, err := s.db.ExecContext(ctx, `delete from history_cleanup_queue where kind=? and object_id=?`, target.kind, target.id); err != nil {
				return processed, err
			}
			processed++
			continue
		}
		message := ""
		if cleanupErr != nil {
			message = cleanupErr.Error()
		}
		if _, err := s.db.ExecContext(ctx, `update history_cleanup_queue set attempts=attempts+1,last_error=? where kind=? and object_id=?`, message, target.kind, target.id); err != nil {
			return processed, err
		}
	}
	return processed, nil
}

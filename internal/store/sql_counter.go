package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// countingDB tracks top-level Store operations. Health-report transactions use
// countingTx below so their statements are included without replacing the
// registered SQLite driver.
type countingDB struct {
	db      *sql.DB
	reader  *sql.DB
	history *countingDB
	role    string
	metrics *databaseMetrics
	stmts   atomic.Int64
	txs     atomic.Int64
}

func newCountingDB(db *sql.DB) *countingDB { return &countingDB{db: db} }

func (d *countingDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	d.stmts.Add(1)
	if target := d.historyTarget(query); target != nil {
		return target.ExecContext(ctx, query, args...)
	}
	started := time.Now()
	result, err := d.db.ExecContext(ctx, query, args...)
	d.metrics.observe(d.role, query, time.Since(started), err)
	return result, err
}

func (d *countingDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	d.stmts.Add(1)
	if target := d.historyTarget(query); target != nil {
		return target.QueryContext(ctx, query, args...)
	}
	started := time.Now()
	result, err := d.readDB().QueryContext(ctx, query, args...)
	d.metrics.observe(d.role, query, time.Since(started), err)
	return result, err
}

func (d *countingDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	d.stmts.Add(1)
	if target := d.historyTarget(query); target != nil {
		return target.QueryRowContext(ctx, query, args...)
	}
	started := time.Now()
	result := d.readDB().QueryRowContext(ctx, query, args...)
	d.metrics.observe(d.role, query, time.Since(started), nil)
	return result
}

func (d *countingDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	d.stmts.Add(1)
	d.txs.Add(1)
	if opts != nil && opts.ReadOnly {
		return d.readDB().BeginTx(ctx, opts)
	}
	return d.db.BeginTx(ctx, opts)
}

func (d *countingDB) BeginCountedTx(ctx context.Context, opts *sql.TxOptions) (*countingTx, error) {
	tx, err := d.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &countingTx{Tx: tx, statements: &d.stmts}, nil
}

func (d *countingDB) Begin() (*sql.Tx, error) { return d.BeginTx(context.Background(), nil) }

func (d *countingDB) Exec(query string, args ...any) (sql.Result, error) {
	d.stmts.Add(1)
	if target := d.historyTarget(query); target != nil {
		return target.Exec(query, args...)
	}
	started := time.Now()
	result, err := d.db.Exec(query, args...)
	d.metrics.observe(d.role, query, time.Since(started), err)
	return result, err
}

func (d *countingDB) Query(query string, args ...any) (*sql.Rows, error) {
	d.stmts.Add(1)
	if target := d.historyTarget(query); target != nil {
		return target.Query(query, args...)
	}
	started := time.Now()
	result, err := d.readDB().Query(query, args...)
	d.metrics.observe(d.role, query, time.Since(started), err)
	return result, err
}

func (d *countingDB) QueryRow(query string, args ...any) *sql.Row {
	d.stmts.Add(1)
	if target := d.historyTarget(query); target != nil {
		return target.QueryRow(query, args...)
	}
	started := time.Now()
	result := d.readDB().QueryRow(query, args...)
	d.metrics.observe(d.role, query, time.Since(started), nil)
	return result
}

var historyTableReference = regexp.MustCompile(`(?i)\b(?:server_metric_samples|server_metric_hourly|server_metric_daily|server_latency_probe_results|latency_rollup_buckets|latency_rollup_state|connection_audit_reports|connection_audit_hourly|connection_audit_hourly_dimensions|connection_audit_hourly_dirty|audit_rollup_state)\b`)

var coreTableReference = regexp.MustCompile(`(?i)\b(?:servers|users|user_devices|server_telemetry|app_settings|server_connectivity_events|latency_probe_tasks|latency_probe_task_servers|connection_presence_states|connection_presence_events|connection_probe_episodes|subscription_pull_audits|audit_feature_snapshots|audit_incidents|ai_audit_reviews|ai_audit_review_evidence|ai_audit_review_jobs|oboard_history_migration|oboard_history_migration_tables|agent_tasks)\b`)

var historyObjectReference = regexp.MustCompile(`(?i)\b(?:idx_server_metric_samples_[a-z_]+|idx_server_latency_probe_results_[a-z_]+|idx_connection_audit_[a-z_]+|idx_latency_rollup_[a-z_]+|rollup_fail)\b`)

func (d *countingDB) historyTarget(query string) *countingDB {
	if d.history == nil || d.history == d {
		return nil
	}
	upper := strings.ToUpper(strings.TrimSpace(query))
	if !strings.Contains(upper, "SELECT") && !strings.Contains(upper, "INSERT") && !strings.Contains(upper, "UPDATE") && !strings.Contains(upper, "DELETE") && !strings.Contains(upper, "REPLACE") && !strings.Contains(upper, "CREATE") && !strings.Contains(upper, "DROP") && !strings.Contains(upper, "ALTER") && !strings.Contains(upper, "EXPLAIN") {
		return nil
	}
	if (!historyTableReference.MatchString(query) && !historyObjectReference.MatchString(query)) || coreTableReference.MatchString(query) {
		return nil
	}
	return d.history
}

func (d *countingDB) Conn(ctx context.Context) (*sql.Conn, error) {
	d.stmts.Add(1)
	return d.db.Conn(ctx)
}

func (d *countingDB) readDB() *sql.DB {
	if d.reader != nil {
		return d.reader
	}
	return d.db
}

// WriteQuery methods keep statements with RETURNING on the serialized writer.
func (d *countingDB) WriteQueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	d.stmts.Add(1)
	return d.db.QueryContext(ctx, query, args...)
}
func (d *countingDB) WriteQueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	d.stmts.Add(1)
	return d.db.QueryRowContext(ctx, query, args...)
}
func (d *countingDB) ReadConn(ctx context.Context) (*sql.Conn, error) {
	d.stmts.Add(1)
	return d.readDB().Conn(ctx)
}
func (d *countingDB) Stats() sql.DBStats {
	stats := d.db.Stats()
	if d.reader != nil {
		r := d.reader.Stats()
		stats.MaxOpenConnections += r.MaxOpenConnections
		stats.OpenConnections += r.OpenConnections
		stats.InUse += r.InUse
		stats.Idle += r.Idle
		stats.WaitCount += r.WaitCount
		stats.WaitDuration += r.WaitDuration
		stats.MaxIdleClosed += r.MaxIdleClosed
		stats.MaxIdleTimeClosed += r.MaxIdleTimeClosed
		stats.MaxLifetimeClosed += r.MaxLifetimeClosed
	}
	return stats
}

func (d *countingDB) Close() error {
	var err error
	if d.reader != nil {
		err = d.reader.Close()
	}
	return errors.Join(err, d.db.Close())
}

type countingTx struct {
	*sql.Tx
	statements *atomic.Int64
}

func (tx *countingTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	tx.statements.Add(1)
	return tx.Tx.ExecContext(ctx, query, args...)
}

func (tx *countingTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	tx.statements.Add(1)
	return tx.Tx.QueryRowContext(ctx, query, args...)
}

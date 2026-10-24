package store

import (
	"context"
	"database/sql"
	"github.com/OboardProject/oboard/internal/model"
	"sort"
	"strings"
	"time"
)

const (
	ledgerConnection = iota
	ledgerBegin
	ledgerStream
	ledgerReport
	ledgerFinalize
	ledgerCommit
	ledgerRollback
	ledgerTotal
	ledgerSQL
	ledgerHold
	ledgerStageCount
)

var ledgerStages = [ledgerStageCount]string{"connection_wait", "begin_duration", "stream_duration", "report_duration", "finalize_duration", "commit_duration", "rollback_duration", "total_duration", "sql_duration", "transaction_duration"}

type ledgerSample struct {
	at                                                                                        time.Time
	durations                                                                                 [ledgerStageCount]time.Duration
	reports, streams, users, periods, accepted, duplicate, covered, rejected, queries, writes int64
	failed, timeout, canceled                                                                 bool
}
type ledgerMetrics struct {
	count   int64
	next    int
	samples [512]ledgerSample
}
type TrafficLedgerStage struct {
	Stage string  `json:"stage"`
	Count int     `json:"count"`
	AvgMS float64 `json:"avg_ms"`
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
	MaxMS float64 `json:"max_ms"`
}

// All aggregates describe the same bounded window; Count alone is process lifetime.
type TrafficLedgerPerformance struct {
	Count       int64                `json:"count"`
	SampleCount int                  `json:"sample_count"`
	WindowStart time.Time            `json:"window_start"`
	WindowEnd   time.Time            `json:"window_end"`
	Stages      []TrafficLedgerStage `json:"stages"`
	Reports     int64                `json:"reports"`
	Streams     int64                `json:"streams"`
	Users       int64                `json:"users"`
	Periods     int64                `json:"periods"`
	Accepted    int64                `json:"accepted"`
	Duplicate   int64                `json:"duplicate"`
	Covered     int64                `json:"covered"`
	Rejected    int64                `json:"rejected"`
	Queries     int64                `json:"queries"`
	Writes      int64                `json:"writes"`
	Failures    int64                `json:"failures"`
	Timeouts    int64                `json:"timeouts"`
	Canceled    int64                `json:"canceled"`
	MaxReports  int64                `json:"max_reports"`
}

func (m *databaseMetrics) observeLedger(sample ledgerSample) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ledger.samples[m.ledger.next] = sample
	m.ledger.next = (m.ledger.next + 1) % len(m.ledger.samples)
	m.ledger.count++
}
func (m *ledgerMetrics) snapshot() *TrafficLedgerPerformance {
	n := int(min(m.count, int64(len(m.samples))))
	out := &TrafficLedgerPerformance{Count: m.count, SampleCount: n}
	for i := 0; i < n; i++ {
		s := m.samples[i]
		if out.WindowStart.IsZero() || s.at.Before(out.WindowStart) {
			out.WindowStart = s.at
		}
		end := s.at.Add(s.durations[ledgerTotal])
		if end.After(out.WindowEnd) {
			out.WindowEnd = end
		}
		out.Reports += s.reports
		out.Streams += s.streams
		out.Users += s.users
		out.Periods += s.periods
		out.Accepted += s.accepted
		out.Duplicate += s.duplicate
		out.Covered += s.covered
		out.Rejected += s.rejected
		out.Queries += s.queries
		out.Writes += s.writes
		if s.failed {
			out.Failures++
		}
		if s.timeout {
			out.Timeouts++
		}
		if s.canceled {
			out.Canceled++
		}
		out.MaxReports = max(out.MaxReports, s.reports)
	}
	for stage, name := range ledgerStages {
		values := []time.Duration{}
		var total time.Duration
		for i := 0; i < n; i++ {
			d := m.samples[i].durations[stage]
			if d > 0 {
				values = append(values, d)
				total += d
			}
		}
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		metric := TrafficLedgerStage{Stage: name, Count: len(values)}
		if len(values) > 0 {
			metric.AvgMS = float64(total) / float64(len(values)) / float64(time.Millisecond)
			metric.MaxMS = float64(values[len(values)-1]) / float64(time.Millisecond)
			metric.P50MS = percentileDuration(values, .5)
			metric.P95MS = percentileDuration(values, .95)
			metric.P99MS = percentileDuration(values, .99)
		}
		out.Stages = append(out.Stages, metric)
	}
	return out
}

type trafficRow interface{ Scan(...any) error }
type ledgerTx struct {
	streams map[string]model.TrafficCounterStream
	conn    *sql.Conn
	sample  *ledgerSample
	metrics *databaseMetrics
}

func (t *ledgerTx) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	started := time.Now()
	result, err := t.conn.ExecContext(ctx, q, args...)
	t.sample.durations[ledgerSQL] += time.Since(started)
	t.sample.writes++
	t.metrics.observeOperation("core", ledgerSQLKind(q), time.Since(started), err)
	return result, err
}
func (t *ledgerTx) QueryRowContext(ctx context.Context, q string, args ...any) trafficRow {
	started := time.Now()
	if classifySQLOperation(q) == "write" {
		t.sample.writes++
	} else {
		t.sample.queries++
	}
	return &ledgerRow{row: t.conn.QueryRowContext(ctx, q, args...), started: started, query: q, metrics: t.metrics, sample: t.sample}
}

type ledgerRow struct {
	sample  *ledgerSample
	row     *sql.Row
	started time.Time
	query   string
	metrics *databaseMetrics
}

func (r *ledgerRow) Scan(args ...any) error {
	err := r.row.Scan(args...)
	r.sample.durations[ledgerSQL] += time.Since(r.started)
	r.metrics.observeOperation("core", ledgerSQLKind(r.query), time.Since(r.started), err)
	return err
}

func ledgerSQLKind(query string) string {
	fields := strings.Fields(strings.ToLower(query))
	var target string
	for i, word := range fields {
		if (word == "into" || word == "from" || word == "update") && i+1 < len(fields) {
			target = strings.SplitN(fields[i+1], "(", 2)[0]
			break
		}
	}
	for _, table := range []string{"traffic_reports", "traffic_counter_streams", "traffic_period_transitions", "traffic_periods", "traffic_leases", "traffic_stats", "traffic_policy_revision", "traffic_reconciliation_events", "users"} {
		if target == table {
			return "ledger_" + table + "_" + classifySQLOperation(query)
		}
	}
	return "ledger_other"
}

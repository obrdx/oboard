package store

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type databaseMetrics struct {
	mu     sync.Mutex
	ops    map[string]*sqlOperationMetric
	busy   map[string]int64
	locked map[string]int64
	ledger ledgerMetrics
}

type sqlOperationMetric struct {
	count       int64
	sampleTimes [512]time.Time
	samples     [512]time.Duration
	next        int
}

type DatabaseOperationPerformance struct {
	SampleCount int       `json:"sample_count"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	P50MS       float64   `json:"p50_ms"`
	Role        string    `json:"role"`
	Kind        string    `json:"kind"`
	Count       int64     `json:"count"`
	AvgMS       float64   `json:"avg_ms"`
	P95MS       float64   `json:"p95_ms"`
	P99MS       float64   `json:"p99_ms"`
	MaxMS       float64   `json:"max_ms"`
}

type DatabasePerformance struct {
	TrafficLedger   *TrafficLedgerPerformance      `json:"traffic_ledger,omitempty"`
	Role            string                         `json:"role"`
	WaitCount       int64                          `json:"wait_count"`
	WaitDurationMS  float64                        `json:"wait_duration_ms"`
	OpenConnections int                            `json:"open_connections"`
	InUse           int                            `json:"in_use"`
	BusyErrors      int64                          `json:"busy_errors"`
	LockedErrors    int64                          `json:"locked_errors"`
	Operations      []DatabaseOperationPerformance `json:"operations"`
}

func newDatabaseMetrics() *databaseMetrics {
	return &databaseMetrics{ops: map[string]*sqlOperationMetric{}, busy: map[string]int64{}, locked: map[string]int64{}}
}

func (m *databaseMetrics) observe(role, query string, elapsed time.Duration, err error) {
	m.observeOperation(role, classifySQLOperation(query), elapsed, err)
}

func (m *databaseMetrics) observeOperation(role, kind string, elapsed time.Duration, err error) {
	if m == nil {
		return
	}
	key := role + "/" + kind
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.ops[key]
	if item == nil {
		item = &sqlOperationMetric{}
		m.ops[key] = item
	}
	item.count++
	item.samples[item.next%len(item.samples)] = elapsed
	item.sampleTimes[item.next%len(item.samples)] = time.Now()
	item.next++
	if err != nil {
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "sqlite_busy") || strings.Contains(lower, "database is locked (5)") {
			m.busy[role]++
		}
		if strings.Contains(lower, "sqlite_locked") || strings.Contains(lower, "database table is locked (6)") {
			m.locked[role]++
		}
	}
}

func (m *databaseMetrics) snapshot(role string, waits int64, waitDuration time.Duration, open, inUse int) DatabasePerformance {
	out := DatabasePerformance{Role: role, WaitCount: waits, WaitDurationMS: float64(waitDuration) / float64(time.Millisecond), OpenConnections: open, InUse: inUse}
	if m == nil {
		return out
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out.BusyErrors = m.busy[role]
	if role == "core" {
		out.TrafficLedger = m.ledger.snapshot()
	}
	out.LockedErrors = m.locked[role]
	keys := []string{}
	for key := range m.ops {
		if strings.HasPrefix(key, role+"/") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		item := m.ops[key]
		var windowStart, windowEnd time.Time
		var windowTotal time.Duration
		values := make([]time.Duration, 0, minMetricInt(item.next, len(item.samples)))
		for i := 0; i < minMetricInt(item.next, len(item.samples)); i++ {
			values = append(values, item.samples[i])
			windowTotal += item.samples[i]
			at := item.sampleTimes[i]
			if windowStart.IsZero() || at.Before(windowStart) {
				windowStart = at
			}
			if at.After(windowEnd) {
				windowEnd = at
			}
		}
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		out.Operations = append(out.Operations, DatabaseOperationPerformance{
			SampleCount: len(values), WindowStart: windowStart, WindowEnd: windowEnd, P50MS: percentileDuration(values, .5),
			Role:  role,
			Kind:  strings.TrimPrefix(key, role+"/"),
			Count: item.count,
			AvgMS: float64(windowTotal) / float64(len(values)) / float64(time.Millisecond),
			P95MS: percentileDuration(values, 0.95),
			P99MS: percentileDuration(values, 0.99),
			MaxMS: float64(values[len(values)-1]) / float64(time.Millisecond),
		})
	}
	return out
}

func percentileDuration(sorted []time.Duration, percentile float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)-1) * percentile)
	return float64(sorted[index]) / float64(time.Millisecond)
}

func classifySQLOperation(query string) string {
	query = strings.TrimSpace(strings.ToLower(query))
	for _, item := range []struct {
		prefix string
		kind   string
	}{
		{"select", "read"},
		{"with", "read"},
		{"insert", "write"},
		{"update", "write"},
		{"delete", "write"},
		{"replace", "write"},
		{"pragma", "pragma"},
		{"create", "schema"},
		{"alter", "schema"},
		{"drop", "schema"},
	} {
		if strings.HasPrefix(query, item.prefix) {
			return item.kind
		}
	}
	return "other"
}

func minMetricInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

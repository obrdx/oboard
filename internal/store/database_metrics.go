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
}

type sqlOperationMetric struct {
	count   int64
	total   time.Duration
	max     time.Duration
	samples [512]time.Duration
	next    int
}

type DatabaseOperationPerformance struct {
	Role  string  `json:"role"`
	Kind  string  `json:"kind"`
	Count int64   `json:"count"`
	AvgMS float64 `json:"avg_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
	MaxMS float64 `json:"max_ms"`
}

type DatabasePerformance struct {
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
	if m == nil {
		return
	}
	kind := classifySQLOperation(query)
	key := role + "/" + kind
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.ops[key]
	if item == nil {
		item = &sqlOperationMetric{}
		m.ops[key] = item
	}
	item.count++
	item.total += elapsed
	if elapsed > item.max {
		item.max = elapsed
	}
	item.samples[item.next%len(item.samples)] = elapsed
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
		values := make([]time.Duration, 0, minMetricInt(item.next, len(item.samples)))
		for i := 0; i < minMetricInt(item.next, len(item.samples)); i++ {
			values = append(values, item.samples[i])
		}
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		out.Operations = append(out.Operations, DatabaseOperationPerformance{
			Role:  role,
			Kind:  strings.TrimPrefix(key, role+"/"),
			Count: item.count,
			AvgMS: float64(item.total) / float64(item.count) / float64(time.Millisecond),
			P95MS: percentileDuration(values, 0.95),
			P99MS: percentileDuration(values, 0.99),
			MaxMS: float64(item.max) / float64(time.Millisecond),
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

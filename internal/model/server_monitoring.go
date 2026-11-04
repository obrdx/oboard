package model

import "time"

type ServerMonitoringSample struct {
	Available    bool      `json:"available"`
	LatencyMS    *float64  `json:"latency_ms"`
	SampleCount  int64     `json:"sample_count"`
	SuccessCount int64     `json:"success_count"`
	CheckedAt    time.Time `json:"checked_at"`
}

// LatencyGrade is the latency scale one probe series is judged on. Samples at
// or below ExcellentMS are excellent, at or below FairMS are acceptable, and
// anything slower is poor. Basis "route" means the scale was derived from the
// server region and the target province; "fixed" is the region-independent
// fallback.
type LatencyGrade struct {
	ExcellentMS int    `json:"excellent_ms"`
	FairMS      int    `json:"fair_ms"`
	Basis       string `json:"basis"`
}

const (
	LatencyGradeBasisRoute = "route"
	LatencyGradeBasisFixed = "fixed"
)

// ServerMonitoringSeries is the latest probe rounds of one target of the
// selected monitoring task, for example the IPv4 and the IPv6 address of one
// carrier preset.
type ServerMonitoringSeries struct {
	ProbeID string                   `json:"probe_id"`
	Label   string                   `json:"label"`
	Family  string                   `json:"family,omitempty"`
	Grade   LatencyGrade             `json:"grade"`
	Samples []ServerMonitoringSample `json:"samples"`
}

type ServerMonitoringDisplay struct {
	Name    string                   `json:"name"`
	Enabled bool                     `json:"enabled"`
	Series  []ServerMonitoringSeries `json:"series"`
}

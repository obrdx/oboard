// Package pathready decides when a proxy path may be shown as available.
// A downstream HTTP 204 is not a path probe: the entry configuration, every
// hop handshake, a full-path probe and current-plan membership each have to
// pass on their own.
package pathready

// Hop is one controlled or terminal step.
type Hop struct {
	Position             int
	ServerID             int64
	ServerName           string
	HeartbeatOnline      bool
	TaskChannelAvailable bool
	ConfigApplied        bool
	AppliedConfigVersion int64
	WantedConfigVersion  int64
	TaskID               int64
	LastError            string
	HandshakeOK          bool
	// ProbeScope is "path" only when the sample traversed this whole path.
	// "downstream" is a single-target check and does not count.
	ProbeScope string
	ProbeOK    bool
}

// Facts is everything the readiness rule is allowed to see.
type Facts struct {
	PathID                int64
	CorrelationID         string
	EntryConfigApplied    bool
	IncludedInCurrentPlan bool
	Hops                  []Hop
}

// HopView is one hop on the diagnostic page.
type HopView struct {
	Position             int    `json:"position"`
	ServerID             int64  `json:"server_id,omitempty"`
	ServerName           string `json:"server_name,omitempty"`
	HeartbeatOnline      bool   `json:"heartbeat_online"`
	TaskChannelAvailable bool   `json:"task_channel_available"`
	ConfigApplied        bool   `json:"config_applied"`
	AppliedConfigVersion int64  `json:"applied_config_version,omitempty"`
	WantedConfigVersion  int64  `json:"wanted_config_version,omitempty"`
	TaskID               int64  `json:"task_id,omitempty"`
	LastError            string `json:"last_error,omitempty"`
	HandshakeOK          bool   `json:"handshake_ok"`
	ProbeOK              bool   `json:"probe_ok"`
	ProbeScope           string `json:"probe_scope,omitempty"`
}

// Result is the path diagnostic.
type Result struct {
	Ready                 bool      `json:"ready"`
	Reason                string    `json:"reason,omitempty"`
	CorrelationID         string    `json:"correlation_id,omitempty"`
	IncludedInCurrentPlan bool      `json:"included_in_current_plan"`
	ChainProbe            string    `json:"chain_probe"`
	Hops                  []HopView `json:"hops"`
}

// Evaluate returns ready only when the entry configuration is applied, every
// hop handshake succeeded, a full-path probe succeeded, and a current plan
// revision contains the path.
func Evaluate(facts Facts) Result {
	result := Result{
		CorrelationID:         facts.CorrelationID,
		IncludedInCurrentPlan: facts.IncludedInCurrentPlan,
		ChainProbe:            "not_run",
		Hops:                  make([]HopView, 0, len(facts.Hops)),
	}
	chainOK := len(facts.Hops) > 0
	for _, hop := range facts.Hops {
		result.Hops = append(result.Hops, HopView{
			Position: hop.Position, ServerID: hop.ServerID, ServerName: hop.ServerName,
			HeartbeatOnline: hop.HeartbeatOnline, TaskChannelAvailable: hop.TaskChannelAvailable,
			ConfigApplied: hop.ConfigApplied, AppliedConfigVersion: hop.AppliedConfigVersion,
			WantedConfigVersion: hop.WantedConfigVersion, TaskID: hop.TaskID, LastError: hop.LastError,
			HandshakeOK: hop.HandshakeOK, ProbeOK: hop.ProbeScope == "path" && hop.ProbeOK, ProbeScope: hop.ProbeScope,
		})
		if hop.ProbeScope != "path" || !hop.ProbeOK {
			chainOK = false
		}
		if !hop.HandshakeOK {
			chainOK = false
		}
	}
	if chainOK {
		result.ChainProbe = "passed"
	} else if downstreamOnly(facts.Hops) {
		result.ChainProbe = "downstream_only"
	}
	switch {
	case !facts.EntryConfigApplied:
		result.Reason = "入口 Agent 尚未应用当前配置"
	case !handshakesPassed(facts.Hops):
		result.Reason = "还有跳点没有完成握手"
	case result.ChainProbe != "passed":
		result.Reason = "还没有通过整条路径的代理探测"
	case !facts.IncludedInCurrentPlan:
		result.Reason = "当前已发布套餐不包含这条路径"
	default:
		result.Ready = true
	}
	return result
}

func handshakesPassed(hops []Hop) bool {
	if len(hops) == 0 {
		return false
	}
	for _, hop := range hops {
		if !hop.HandshakeOK {
			return false
		}
	}
	return true
}

func downstreamOnly(hops []Hop) bool {
	sawDownstream := false
	for _, hop := range hops {
		if hop.ProbeScope == "path" {
			return false
		}
		if hop.ProbeScope == "downstream" && hop.ProbeOK {
			sawDownstream = true
		}
	}
	return sawDownstream
}

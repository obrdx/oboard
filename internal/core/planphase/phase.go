// Package planphase names where a subscription plan revision sits. The
// published revision stays the current one until a later revision is
// activated; a stuck prepare becomes blocked instead of holding the lock.
package planphase

import "time"

const StaleApplyAfter = 15 * time.Minute

const (
	PhasePublished = "published"
	PhaseDraft     = "draft"
	PhasePreparing = "preparing"
	PhaseBlocked   = "blocked"
)

// Facts is the plan pointer plus the reconcile row.
type Facts struct {
	CurrentRevisionID int64
	LatestRevisionID  int64
	PendingRevisionID int64
	ReconcileStatus   string
	BlockedReason     string
	OpenChangeStatus  string
	OpenChangeAge     time.Duration
	BlockedServerID   int64
	BlockedServerName string
}

// Phase is the operator-facing revision state.
type Phase struct {
	Phase             string `json:"phase"`
	Detail            string `json:"detail,omitempty"`
	CurrentRevisionID int64  `json:"current_revision_id"`
	LatestRevisionID  int64  `json:"latest_revision_id"`
	BlockedServerID   int64  `json:"blocked_server_id,omitempty"`
	BlockedServerName string `json:"blocked_server_name,omitempty"`
	BlockedError      string `json:"blocked_error,omitempty"`
}

// Evaluate keeps the published revision distinct from a draft, an in-flight
// prepare, and a blocked prepare. A prepare that has not moved for
// StaleApplyAfter is blocked so a later save is not locked forever.
func Evaluate(facts Facts) Phase {
	phase := Phase{
		CurrentRevisionID: facts.CurrentRevisionID,
		LatestRevisionID:  facts.LatestRevisionID,
		BlockedServerID:   facts.BlockedServerID,
		BlockedServerName: facts.BlockedServerName,
	}
	open := facts.OpenChangeStatus == "preparing" || facts.OpenChangeStatus == "activating" || facts.OpenChangeStatus == "finalizing"
	stale := open && facts.OpenChangeAge >= StaleApplyAfter
	switch {
	case stale || facts.ReconcileStatus == "failed" || facts.ReconcileStatus == "waiting_dependency" || facts.ReconcileStatus == "blocked":
		phase.Phase = PhaseBlocked
		phase.BlockedError = facts.BlockedReason
		phase.Detail = facts.BlockedReason
		if phase.Detail == "" && stale {
			phase.Detail = "应用超过 15 分钟没有进展，已停止占用后续更新"
			phase.BlockedError = phase.Detail
		}
		if phase.BlockedServerName != "" && phase.Detail != "" {
			phase.Detail = phase.BlockedServerName + "：" + phase.Detail
		}
	case open || facts.ReconcileStatus == "preparing" || facts.ReconcileStatus == "activating" || facts.ReconcileStatus == "finalizing":
		phase.Phase = PhasePreparing
		phase.Detail = "正在把最新保存版本应用到节点"
	case facts.LatestRevisionID != 0 && facts.LatestRevisionID != facts.CurrentRevisionID:
		phase.Phase = PhaseDraft
		phase.Detail = "最新保存版本尚未发布，当前生效版本保持不变"
	default:
		phase.Phase = PhasePublished
		phase.Detail = "当前版本已发布"
	}
	return phase
}

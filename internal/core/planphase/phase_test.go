package planphase

import "testing"
import "time"

func TestPublishedStaysCurrentUntilActivation(t *testing.T) {
	phase := Evaluate(Facts{CurrentRevisionID: 4, LatestRevisionID: 9})
	if phase.Phase != PhaseDraft || phase.CurrentRevisionID != 4 {
		t.Fatalf("unpublished latest = %#v", phase)
	}
}

func TestStalePrepareBecomesBlocked(t *testing.T) {
	phase := Evaluate(Facts{
		CurrentRevisionID: 4,
		LatestRevisionID:  9,
		OpenChangeStatus:  "preparing",
		OpenChangeAge:     StaleApplyAfter,
		BlockedServerName: "广州轻量",
		BlockedReason:     "心跳未更新",
	})
	if phase.Phase != PhaseBlocked || phase.CurrentRevisionID != 4 {
		t.Fatalf("stale prepare = %#v", phase)
	}
	if phase.Detail == "" || phase.BlockedServerName != "广州轻量" {
		t.Fatalf("blocked detail = %#v", phase)
	}
}

func TestFreshPrepareIsNotBlocked(t *testing.T) {
	phase := Evaluate(Facts{CurrentRevisionID: 4, LatestRevisionID: 9, OpenChangeStatus: "preparing", OpenChangeAge: time.Minute})
	if phase.Phase != PhasePreparing {
		t.Fatalf("fresh prepare = %#v", phase)
	}
}

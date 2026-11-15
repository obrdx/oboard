package pathready

import "testing"

func TestDownstream204DoesNotMakeThePathReady(t *testing.T) {
	result := Evaluate(Facts{
		PathID:                7,
		EntryConfigApplied:    true,
		IncludedInCurrentPlan: true,
		Hops: []Hop{{
			Position: 1, ServerName: "广州", HandshakeOK: true, ConfigApplied: true,
			ProbeScope: "downstream", ProbeOK: true,
		}},
	})
	if result.Ready || result.ChainProbe != "downstream_only" {
		t.Fatalf("downstream probe counted as the path: %#v", result)
	}
}

func TestReadyRequiresEveryGate(t *testing.T) {
	result := Evaluate(Facts{
		PathID:                7,
		CorrelationID:         "path-7-rev-3-task-9",
		EntryConfigApplied:    true,
		IncludedInCurrentPlan: true,
		Hops: []Hop{{
			Position: 1, HandshakeOK: true, ConfigApplied: true, ProbeScope: "path", ProbeOK: true,
		}},
	})
	if !result.Ready || result.CorrelationID != "path-7-rev-3-task-9" {
		t.Fatalf("complete path = %#v", result)
	}
}

func TestMissingPlanMembershipBlocksReady(t *testing.T) {
	result := Evaluate(Facts{
		EntryConfigApplied: true,
		Hops:               []Hop{{Position: 1, HandshakeOK: true, ProbeScope: "path", ProbeOK: true}},
	})
	if result.Ready || result.Reason == "" {
		t.Fatalf("path outside the current plan = %#v", result)
	}
}

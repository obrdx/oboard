package agentpresence

import "testing"
import "time"

func TestEvaluateSeparatesHeartbeatChannelAndConfig(t *testing.T) {
	now := time.Date(2026, 11, 16, 2, 0, 0, 0, time.UTC)
	view := Evaluate(Facts{
		Now:                  now,
		HeartbeatAt:          now.Add(-3 * time.Minute),
		ChannelLive:          true,
		SessionCount:         1,
		OfflineAfter:         2 * time.Minute,
		AppliedConfigVersion: 10,
		WantedConfigVersion:  12,
		SyncState:            "queued",
	})
	if view.HeartbeatOnline || !view.TaskChannelAvailable || view.ConfigApplied || view.Dispatch != DispatchBlockedOffline {
		t.Fatalf("stale heartbeat with a live channel = %#v", view)
	}
}

func TestFirstContactOnALiveChannelCanTakeWork(t *testing.T) {
	view := Evaluate(Facts{Now: time.Now(), ChannelLive: true})
	if view.HeartbeatOnline || view.Dispatch != DispatchReady {
		t.Fatalf("first contact = %#v", view)
	}
}

func TestEvaluateBlocksIdentityBeforeChannel(t *testing.T) {
	now := time.Now()
	view := Evaluate(Facts{
		Now:         now,
		HeartbeatAt: now,
		ChannelLive: true,
		AuthCode:    AuthTokenMismatch,
		SyncState:   "synced",
	})
	if view.Dispatch != DispatchBlockedAuth || view.TaskChannelAvailable || view.AuthReason == "" {
		t.Fatalf("token mismatch = %#v", view)
	}
}

func TestResourceForbiddenIsNotAnIdentityCode(t *testing.T) {
	if IsIdentityCode("forbidden") || IsIdentityCode("") {
		t.Fatal("a resource refusal must not be treated as an identity rejection")
	}
}

func TestOfflineBackoffGrowsAndCaps(t *testing.T) {
	if got := OfflineBackoff(0); got != 30*time.Second {
		t.Fatalf("first backoff = %s", got)
	}
	if got := OfflineBackoff(2); got != 2*time.Minute {
		t.Fatalf("third backoff = %s", got)
	}
	if got := OfflineBackoff(20); got != 15*time.Minute {
		t.Fatalf("capped backoff = %s", got)
	}
}

// Package agentpresence is the one reading of whether an Agent can be treated
// as present. Pages, the API and the task dispatcher all call Evaluate so a
// live socket, a stale heartbeat and an applied configuration cannot be
// reported as three different answers.
package agentpresence

import "time"

const (
	// DefaultOfflineAfter matches the panel default for marking a server
	// offline when no accepted heartbeat has arrived.
	DefaultOfflineAfter = 2 * time.Minute

	DispatchReady          = "ready"
	DispatchBlockedOffline = "blocked_offline"
	DispatchBlockedChannel = "blocked_channel"
	DispatchBlockedAuth    = "blocked_auth"

	// Identity rejection codes. Controller and Agent both match these literals;
	// a resource 403 that does not carry one of them is not an identity failure.
	AuthIdentityUnknown  = "identity_unknown"
	AuthTokenMismatch    = "token_mismatch"
	AuthIdentityReplaced = "identity_replaced"

	AuthHeader = "X-Oboard-Agent-Auth"
)

// Facts is the single timestamped observation Evaluate reads.
type Facts struct {
	Now                  time.Time
	HeartbeatAt          time.Time
	ChannelLive          bool
	SessionCount         int
	OfflineAfter         time.Duration
	AppliedConfigVersion int64
	WantedConfigVersion  int64
	SyncState            string
	AuthCode             string
}

// View is what every surface shows and what the dispatcher obeys.
type View struct {
	HeartbeatOnline      bool      `json:"heartbeat_online"`
	HeartbeatAt          time.Time `json:"heartbeat_at,omitempty"`
	TaskChannelAvailable bool      `json:"task_channel_available"`
	ConfigApplied        bool      `json:"config_applied"`
	AppliedConfigVersion int64     `json:"applied_config_version,omitempty"`
	WantedConfigVersion  int64     `json:"wanted_config_version,omitempty"`
	Dispatch             string    `json:"dispatch"`
	AuthCode             string    `json:"auth_code,omitempty"`
	AuthReason           string    `json:"auth_reason,omitempty"`
	DuplicateSession     bool      `json:"duplicate_session,omitempty"`
	ObservedAt           time.Time `json:"observed_at"`
}

// Evaluate derives heartbeat, task channel, configuration and the dispatch
// gate from one observation. Dispatch is ready only when the heartbeat is
// fresh, the control channel is up, and the Controller has not rejected the
// identity.
func Evaluate(facts Facts) View {
	now := facts.Now
	if now.IsZero() {
		now = time.Now()
	}
	offlineAfter := facts.OfflineAfter
	if offlineAfter <= 0 {
		offlineAfter = DefaultOfflineAfter
	}
	heartbeatOnline := !facts.HeartbeatAt.IsZero() && now.Sub(facts.HeartbeatAt) < offlineAfter
	authCode := facts.AuthCode
	if !IsIdentityCode(authCode) {
		authCode = ""
	}
	channel := facts.ChannelLive && authCode == ""
	applied := facts.SyncState == "synced" || (facts.SyncState == "" && facts.WantedConfigVersion == 0 && facts.AppliedConfigVersion == 0)
	if facts.SyncState != "" && facts.SyncState != "synced" {
		applied = false
	}
	dispatch := DispatchReady
	switch {
	case authCode != "":
		dispatch = DispatchBlockedAuth
	case !channel:
		dispatch = DispatchBlockedChannel
	case !heartbeatOnline && !facts.HeartbeatAt.IsZero():
		// A live socket whose last accepted heartbeat is already stale must
		// not take more work. A channel that has never reported is still
		// allowed one delivery so enrollment can land.
		dispatch = DispatchBlockedOffline
	}
	view := View{
		HeartbeatOnline:      heartbeatOnline,
		TaskChannelAvailable: channel,
		ConfigApplied:        applied,
		AppliedConfigVersion: facts.AppliedConfigVersion,
		WantedConfigVersion:  facts.WantedConfigVersion,
		Dispatch:             dispatch,
		AuthCode:             authCode,
		AuthReason:           AuthReason(authCode),
		DuplicateSession:     facts.SessionCount > 1,
		ObservedAt:           now,
	}
	if !facts.HeartbeatAt.IsZero() {
		view.HeartbeatAt = facts.HeartbeatAt
	}
	return view
}

// IsIdentityCode reports whether a Controller rejection is this Agent's
// binding, rather than a resource rule such as a foreign inbound.
func IsIdentityCode(code string) bool {
	switch code {
	case AuthIdentityUnknown, AuthTokenMismatch, AuthIdentityReplaced:
		return true
	default:
		return false
	}
}

// AuthReason is the operator-facing sentence for an identity code.
func AuthReason(code string) string {
	switch code {
	case AuthIdentityUnknown:
		return "主控没有这台 Agent 的身份记录"
	case AuthTokenMismatch:
		return "本机凭证与主控绑定不一致"
	case AuthIdentityReplaced:
		return "这台服务器已重新注册，当前凭证已失效"
	default:
		return ""
	}
}

// OfflineBackoff is the wait before another configuration attempt while the
// Agent heartbeat is not accepted. It grows from 30s and stops at 15 minutes.
func OfflineBackoff(retry int) time.Duration {
	if retry < 0 {
		retry = 0
	}
	if retry > 5 {
		retry = 5
	}
	delay := 30 * time.Second * time.Duration(uint(1)<<uint(retry))
	if delay > 15*time.Minute {
		return 15 * time.Minute
	}
	return delay
}

// DispatchMessage is stored on the configuration sync row so the panel can
// show why a task was not queued.
func DispatchMessage(view View) string {
	switch view.Dispatch {
	case DispatchBlockedAuth:
		if view.AuthReason != "" {
			return view.AuthReason
		}
		return "主控拒绝了 Agent 身份，配置暂不下发"
	case DispatchBlockedOffline:
		return "心跳未更新，配置任务已暂停并等待 Agent 重新上报"
	case DispatchBlockedChannel:
		return "任务通道不可用，配置任务已暂停"
	default:
		return ""
	}
}

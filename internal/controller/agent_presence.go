package controller

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/OboardProject/oboard/internal/agentlink"
	"github.com/OboardProject/oboard/internal/core/agentpresence"
	"github.com/OboardProject/oboard/internal/core/pathready"
	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/store"
)

func (s *Server) noteAgentIdentityRejection(serverID int64, code string) {
	if s == nil || serverID <= 0 || !agentpresence.IsIdentityCode(code) {
		return
	}
	s.agentAuthMu.Lock()
	if s.agentAuthCode == nil {
		s.agentAuthCode = map[int64]string{}
	}
	s.agentAuthCode[serverID] = code
	s.agentAuthMu.Unlock()
	log.Printf("agent identity rejected code=%s server=%d reason=%s", code, serverID, agentpresence.AuthReason(code))
}

func (s *Server) clearAgentIdentityRejection(serverID int64) {
	if s == nil || serverID <= 0 {
		return
	}
	s.agentAuthMu.Lock()
	delete(s.agentAuthCode, serverID)
	s.agentAuthMu.Unlock()
}

func (s *Server) agentIdentityCode(serverID int64) string {
	if s == nil {
		return ""
	}
	s.agentAuthMu.Lock()
	defer s.agentAuthMu.Unlock()
	return s.agentAuthCode[serverID]
}

func (s *Server) agentControlSessions(serverID int64) int {
	if s == nil {
		return 0
	}
	s.agentLiveMu.Lock()
	defer s.agentLiveMu.Unlock()
	return len(s.agentLive[serverID])
}

func agentIdentityFrame(id int64, code, message string) *agentlink.ResponseFrame {
	body, _ := json.Marshal(map[string]any{"error": message, "code": code})
	return &agentlink.ResponseFrame{
		ID: id, Status: http.StatusUnauthorized, Error: code + ": " + message, Body: body,
		Headers: map[string][]string{agentpresence.AuthHeader: {code}},
	}
}

func (s *Server) rejectAgentAuth(w http.ResponseWriter, ip string, serverID int64, code, message string) {
	if serverID > 0 {
		s.noteAgentIdentityRejection(serverID, code)
	} else {
		log.Printf("agent identity rejected code=%s remote=%s reason=%s", code, safeLogField(ip), agentpresence.AuthReason(code))
	}
	s.noteAgentAuthFailure(ip)
	if w != nil {
		w.Header().Set(agentpresence.AuthHeader, code)
		failCode(w, code, message, http.StatusUnauthorized)
	}
}

func (s *Server) offlineAfterFor(ctx context.Context, server *model.Server) time.Duration {
	seconds := defaultNotificationOfflineAfterSeconds
	if s != nil {
		seconds = settingInt(s.runtimeSettings(ctx), settingNotificationServerOfflineAfter, defaultNotificationOfflineAfterSeconds, 30, 86400)
	}
	if server != nil && server.OfflineAfterSeconds > 0 {
		seconds = server.OfflineAfterSeconds
	}
	if seconds <= 0 {
		seconds = defaultNotificationOfflineAfterSeconds
	}
	return time.Duration(seconds) * time.Second
}

func (s *Server) agentHealthView(ctx context.Context, server *model.Server, sync *store.ConfigurationSyncState) model.AgentHealth {
	if server == nil {
		return model.AgentHealth{Dispatch: agentpresence.DispatchBlockedOffline, ObservedAt: time.Now().UTC()}
	}
	var heartbeat time.Time
	if server.LastSeenAt != nil {
		heartbeat = server.LastSeenAt.UTC()
	}
	facts := agentpresence.Facts{
		Now:          time.Now().UTC(),
		HeartbeatAt:  heartbeat,
		ChannelLive:  s.agentControlOnline(server.ID),
		SessionCount: s.agentControlSessions(server.ID),
		OfflineAfter: s.offlineAfterFor(ctx, server),
		AuthCode:     s.agentIdentityCode(server.ID),
	}
	if sync != nil {
		facts.SyncState = sync.State
		facts.AppliedConfigVersion = sync.LastConfigVersion
		facts.WantedConfigVersion = int64(sync.WantedRevision)
	}
	return agentHealthModel(agentpresence.Evaluate(facts))
}

func agentHealthModel(view agentpresence.View) model.AgentHealth {
	return model.AgentHealth{
		HeartbeatOnline: view.HeartbeatOnline, HeartbeatAt: view.HeartbeatAt,
		TaskChannelAvailable: view.TaskChannelAvailable, ConfigApplied: view.ConfigApplied,
		AppliedConfigVersion: view.AppliedConfigVersion, WantedConfigVersion: view.WantedConfigVersion,
		Dispatch: view.Dispatch, AuthCode: view.AuthCode, AuthReason: view.AuthReason,
		DuplicateSession: view.DuplicateSession, ObservedAt: view.ObservedAt,
	}
}

func (s *Server) attachAgentHealth(ctx context.Context, items []model.Server) {
	if s == nil || len(items) == 0 {
		return
	}
	states, err := s.store.ListAllConfigurationSyncStates(ctx)
	if err != nil {
		states = nil
	}
	byServer := make(map[int64]store.ConfigurationSyncState, len(states))
	for _, state := range states {
		byServer[state.ServerID] = state
	}
	for i := range items {
		state, ok := byServer[items[i].ID]
		var sync *store.ConfigurationSyncState
		if ok {
			sync = &state
		}
		health := s.agentHealthView(ctx, &items[i], sync)
		items[i].AgentHealth = &health
	}
}

// deferOfflineConfiguration keeps one wanted revision and does not queue a
// task while the shared presence view says the Agent cannot run it.
func (s *Server) deferOfflineConfiguration(ctx context.Context, state store.ConfigurationSyncState) bool {
	server, err := s.store.GetServer(ctx, state.ServerID)
	if err != nil || server == nil {
		return false
	}
	health := s.agentHealthView(ctx, server, &state)
	if health.Dispatch == agentpresence.DispatchReady {
		return false
	}
	delay := agentpresence.OfflineBackoff(state.RetryCount)
	if err := s.store.DeferConfigurationSync(ctx, state.ServerID, state.WantedRevision, time.Now().UTC().Add(delay), health.Dispatch, agentpresence.DispatchMessage(agentpresence.View{Dispatch: health.Dispatch, AuthReason: health.AuthReason})); err != nil {
		log.Printf("defer offline configuration server=%d: %v", state.ServerID, err)
		return false
	}
	log.Printf("configuration deferred server=%d dispatch=%s retry_in=%s wanted_revision=%d", state.ServerID, health.Dispatch, delay, state.WantedRevision)
	return true
}

func (s *Server) pathReadiness(ctx context.Context, path model.ProxyPath, plan model.ProxyPathPlan, servers []model.Server, inbounds []model.Inbound) *model.PathReadiness {
	byID := make(map[int64]*model.Server, len(servers))
	for i := range servers {
		byID[servers[i].ID] = &servers[i]
	}
	syncStates, err := s.store.ListAllConfigurationSyncStates(ctx)
	syncByID := map[int64]store.ConfigurationSyncState{}
	if err == nil {
		for _, state := range syncStates {
			syncByID[state.ServerID] = state
		}
	}
	var entryServer *model.Server
	for _, inbound := range inbounds {
		if inbound.ID == path.InboundID {
			entryServer = byID[inbound.ServerID]
			break
		}
	}
	entryApplied := false
	if entryServer != nil {
		state := syncByID[entryServer.ID]
		entryApplied = s.agentHealthView(ctx, entryServer, &state).ConfigApplied
	}
	hops := make([]pathready.Hop, 0, len(plan.Steps)+1)
	var newestTask int64
	var wanted int64
	if entryServer != nil {
		hops = append(hops, s.pathHop(ctx, 0, entryServer, syncByID[entryServer.ID]))
	}
	for _, step := range plan.Steps {
		if step.ServerID == nil {
			hops = append(hops, pathready.Hop{Position: step.Position, ServerName: "外部节点"})
			continue
		}
		server := byID[*step.ServerID]
		state := syncByID[*step.ServerID]
		hop := s.pathHop(ctx, step.Position, server, state)
		if hop.TaskID > newestTask {
			newestTask = hop.TaskID
		}
		if int64(state.WantedRevision) > wanted {
			wanted = int64(state.WantedRevision)
		}
		hops = append(hops, hop)
	}
	included, err := s.store.ProxyPathInCurrentPlan(ctx, path.ID)
	if err != nil {
		included = false
	}
	evaluated := pathready.Evaluate(pathready.Facts{
		PathID: path.ID, CorrelationID: pathCorrelation(path.ID, wanted, newestTask),
		EntryConfigApplied: entryApplied, IncludedInCurrentPlan: included, Hops: hops,
	})
	return pathReadinessModel(evaluated)
}

func (s *Server) pathHop(ctx context.Context, position int, server *model.Server, state store.ConfigurationSyncState) pathready.Hop {
	hop := pathready.Hop{Position: position, ProbeScope: "not_run"}
	if server == nil {
		return hop
	}
	health := s.agentHealthView(ctx, server, &state)
	hop.ServerID = server.ID
	hop.ServerName = server.Name
	hop.HeartbeatOnline = health.HeartbeatOnline
	hop.TaskChannelAvailable = health.TaskChannelAvailable
	hop.ConfigApplied = health.ConfigApplied
	hop.AppliedConfigVersion = health.AppliedConfigVersion
	hop.WantedConfigVersion = health.WantedConfigVersion
	hop.TaskID = state.LastTaskID
	hop.LastError = state.LastError
	if health.AuthReason != "" {
		hop.LastError = health.AuthReason
	}
	// A stored hop handshake is not inferred from a downstream status probe.
	hop.HandshakeOK = false
	return hop
}

func pathReadinessModel(result pathready.Result) *model.PathReadiness {
	out := &model.PathReadiness{
		Ready: result.Ready, Reason: result.Reason, CorrelationID: result.CorrelationID,
		IncludedInCurrentPlan: result.IncludedInCurrentPlan, ChainProbe: result.ChainProbe,
		Hops: make([]model.PathHopReading, 0, len(result.Hops)),
	}
	for _, hop := range result.Hops {
		out.Hops = append(out.Hops, model.PathHopReading{
			Position: hop.Position, ServerID: hop.ServerID, ServerName: hop.ServerName,
			HeartbeatOnline: hop.HeartbeatOnline, TaskChannelAvailable: hop.TaskChannelAvailable,
			ConfigApplied: hop.ConfigApplied, AppliedConfigVersion: hop.AppliedConfigVersion,
			WantedConfigVersion: hop.WantedConfigVersion, TaskID: hop.TaskID, LastError: hop.LastError,
			HandshakeOK: hop.HandshakeOK, ProbeOK: hop.ProbeOK, ProbeScope: hop.ProbeScope,
		})
	}
	return out
}

func pathCorrelation(pathID, revision, taskID int64) string {
	return "path-" + strconv.FormatInt(pathID, 10) + "-rev-" + strconv.FormatInt(revision, 10) + "-task-" + strconv.FormatInt(taskID, 10)
}

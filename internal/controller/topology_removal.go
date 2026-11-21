package controller

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/OboardProject/oboard/internal/application"
	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/store"
)

// Topology removal impact is what a delete or detach will really do, computed
// from the same rules the write applies. Previews used to be assembled in the
// browser and skipped disabled paths, branches and the difference between
// deleting a path and cutting it, so the confirmation did not match the result.

const (
	topologyEffectDeleted    = "deleted"
	topologyEffectTruncated  = "truncated"
	topologyEffectKeptDirect = "kept_direct"

	topologyReasonRoot   = "root"
	topologyReasonHop    = "hop"
	topologyReasonBranch = "branch"
)

type topologyNamedRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type topologyPathImpact struct {
	PathID         int64              `json:"path_id"`
	Name           string             `json:"name"`
	RootInboundID  int64              `json:"root_inbound_id,omitempty"`
	RootServerID   int64              `json:"root_server_id,omitempty"`
	RootServerName string             `json:"root_server_name,omitempty"`
	Enabled        bool               `json:"enabled"`
	Effect         string             `json:"effect"`
	Reason         string             `json:"reason"`
	FromPosition   int                `json:"from_position,omitempty"`
	RemovedSteps   int                `json:"removed_steps"`
	RemainingSteps int                `json:"remaining_steps"`
	Disabled       bool               `json:"disabled,omitempty"`
	Plans          []topologyNamedRef `json:"plans,omitempty"`
}

type topologyRemovalImpact struct {
	Paths              []topologyPathImpact `json:"paths"`
	DeletedPathCount   int                  `json:"deleted_path_count"`
	TruncatedPathCount int                  `json:"truncated_path_count"`
	KeptDirectCount    int                  `json:"kept_direct_count"`
	OtherRootPathCount int                  `json:"other_root_path_count"`
	RemovedStepCount   int                  `json:"removed_step_count"`
	RoutingRuleCount   int                  `json:"routing_rule_count"`
	Plans              []topologyNamedRef   `json:"plans"`
	ExceptionCount     int                  `json:"exception_count"`
	Inbounds           []topologyNamedRef   `json:"inbounds,omitempty"`
	PortForwards       []topologyNamedRef   `json:"port_forwards"`
	Tunnels            []topologyNamedRef   `json:"tunnels"`
	// ForwardsDeleted is true when the listed port forwards and tunnels are
	// removed by the operation; false means they are kept and only reported.
	ForwardsDeleted bool `json:"forwards_deleted"`
}

type topologySnapshot struct {
	paths                map[int64]model.ProxyPath
	stepsByPath          map[int64][]model.ProxyPathStep
	inbounds             map[int64]model.Inbound
	serverNames          map[int64]string
	childrenBySourceStep map[int64][]int64
	rules                []model.RoutingRule
	portForwards         []model.PortForward
	tunnels              []model.Tunnel
}

func (s *Server) loadTopologySnapshot(ctx context.Context) (*topologySnapshot, error) {
	paths, err := s.store.ListProxyPaths(ctx)
	if err != nil {
		return nil, err
	}
	steps, err := s.store.ListProxyPathSteps(ctx)
	if err != nil {
		return nil, err
	}
	inbounds, err := s.store.ListInbounds(ctx)
	if err != nil {
		return nil, err
	}
	servers, err := s.store.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := s.store.ListRoutingRules(ctx)
	if err != nil {
		return nil, err
	}
	forwards, err := s.store.ListPortForwards(ctx)
	if err != nil {
		return nil, err
	}
	tunnels, err := s.store.ListTunnels(ctx)
	if err != nil {
		return nil, err
	}
	snap := &topologySnapshot{
		paths: map[int64]model.ProxyPath{}, stepsByPath: map[int64][]model.ProxyPathStep{},
		inbounds: map[int64]model.Inbound{}, serverNames: map[int64]string{}, childrenBySourceStep: map[int64][]int64{},
		rules: rules, portForwards: forwards, tunnels: tunnels,
	}
	for _, path := range paths {
		snap.paths[path.ID] = path
		if path.BranchSourceStepID != nil {
			snap.childrenBySourceStep[*path.BranchSourceStepID] = append(snap.childrenBySourceStep[*path.BranchSourceStepID], path.ID)
		}
	}
	for _, step := range steps {
		snap.stepsByPath[step.PathID] = append(snap.stepsByPath[step.PathID], step)
	}
	for pathID := range snap.stepsByPath {
		list := snap.stepsByPath[pathID]
		sort.SliceStable(list, func(i, j int) bool { return list[i].Position < list[j].Position })
	}
	for _, inbound := range inbounds {
		snap.inbounds[inbound.ID] = inbound
	}
	for _, server := range servers {
		snap.serverNames[server.ID] = firstNonEmpty(strings.TrimSpace(server.Name), "服务器 #"+strconv.FormatInt(server.ID, 10))
	}
	return snap, nil
}

func (snap *topologySnapshot) sortedPathIDs() []int64 {
	ids := make([]int64, 0, len(snap.paths))
	for id := range snap.paths {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// stepServerID is the server a step runs on: its own server, or the server of
// the inbound it targets.
func (snap *topologySnapshot) stepServerID(step model.ProxyPathStep) int64 {
	if step.ServerID != nil {
		return *step.ServerID
	}
	if step.InboundID != nil {
		return snap.inbounds[*step.InboundID].ServerID
	}
	return 0
}

func (snap *topologySnapshot) pathRootServerID(path model.ProxyPath) int64 {
	if path.InboundID <= 0 {
		return 0
	}
	return snap.inbounds[path.InboundID].ServerID
}

type topologyImpactBuilder struct {
	snap         *topologySnapshot
	paths        map[int64]*topologyPathImpact
	removedSteps map[int64]bool
}

func newTopologyImpactBuilder(snap *topologySnapshot) *topologyImpactBuilder {
	return &topologyImpactBuilder{snap: snap, paths: map[int64]*topologyPathImpact{}, removedSteps: map[int64]bool{}}
}

func (b *topologyImpactBuilder) entry(pathID int64) *topologyPathImpact {
	if item, ok := b.paths[pathID]; ok {
		return item
	}
	path := b.snap.paths[pathID]
	rootServerID := b.snap.pathRootServerID(path)
	name := strings.TrimSpace(path.Name)
	if name == "" {
		name = "路径 " + strconv.FormatInt(path.ID, 10)
	}
	item := &topologyPathImpact{
		PathID: path.ID, Name: name, RootInboundID: path.InboundID, RootServerID: rootServerID,
		RootServerName: b.snap.serverNames[rootServerID], Enabled: path.Enabled,
		RemainingSteps: len(b.snap.stepsByPath[pathID]),
	}
	b.paths[pathID] = item
	return item
}

// deletePath records a path that disappears entirely. Branches copied from
// any of its steps go with it.
func (b *topologyImpactBuilder) deletePath(pathID int64, reason string) {
	if _, ok := b.snap.paths[pathID]; !ok {
		return
	}
	item := b.entry(pathID)
	if item.Effect == topologyEffectDeleted {
		return
	}
	if item.Effect == "" {
		item.Reason = reason
	}
	item.Effect = topologyEffectDeleted
	item.FromPosition = 0
	steps := b.snap.stepsByPath[pathID]
	item.RemovedSteps = len(steps)
	item.RemainingSteps = 0
	ids := make([]int64, 0, len(steps))
	for _, step := range steps {
		ids = append(ids, step.ID)
	}
	b.cascade(ids)
}

// deleteLeafPath records the shared-prefix leaf deletion: the path goes, but
// only branches copied from the removed leaf go with it; the prefix copies'
// branch sources are cleared instead.
func (b *topologyImpactBuilder) deleteLeafPath(pathID int64, leaf model.ProxyPathStep, reason string) {
	item := b.entry(pathID)
	if item.Effect == topologyEffectDeleted {
		return
	}
	if item.Effect == "" {
		item.Reason = reason
	}
	item.Effect = topologyEffectDeleted
	item.FromPosition = leaf.Position
	item.RemovedSteps = len(b.snap.stepsByPath[pathID])
	item.RemainingSteps = 0
	b.cascade([]int64{leaf.ID})
}

// truncate records a cut from position onward. emptyDeletes selects the
// store-level dependency semantics (an emptied path is deleted); otherwise an
// emptied path is kept as a direct exit because it has entry routing rules.
func (b *topologyImpactBuilder) truncate(pathID int64, position int, reason string, disable bool, emptyDeletes bool) {
	item := b.entry(pathID)
	if item.Effect == topologyEffectDeleted {
		return
	}
	steps := b.snap.stepsByPath[pathID]
	removed := []int64{}
	remaining := 0
	for _, step := range steps {
		if step.Position >= position {
			removed = append(removed, step.ID)
		} else {
			remaining++
		}
	}
	if remaining == 0 && emptyDeletes {
		b.deletePath(pathID, reason)
		return
	}
	if item.Effect != "" && item.FromPosition > 0 && item.FromPosition <= position {
		return
	}
	if item.Effect == "" {
		item.Reason = reason
	}
	item.Effect = topologyEffectTruncated
	if remaining == 0 {
		item.Effect = topologyEffectKeptDirect
	}
	item.FromPosition = position
	item.RemovedSteps = len(removed)
	item.RemainingSteps = remaining
	if disable && item.Enabled {
		item.Disabled = true
	}
	b.cascade(removed)
}

func (b *topologyImpactBuilder) cascade(stepIDs []int64) {
	for _, stepID := range stepIDs {
		b.removedSteps[stepID] = true
		for _, childID := range b.snap.childrenBySourceStep[stepID] {
			b.deletePath(childID, topologyReasonBranch)
		}
	}
}

// stepRemoval mirrors planProxyPathStepRemoval / applyProxyPathStepRemoval.
func (s *Server) recordStepRemoval(ctx context.Context, b *topologyImpactBuilder, step model.ProxyPathStep, reason string) error {
	_, pathDeleted, err := s.planProxyPathStepRemoval(ctx, step)
	if err != nil {
		path := b.snap.paths[step.PathID]
		return fmt.Errorf("%s：%w", firstNonEmpty(strings.TrimSpace(path.Name), "路径 "+strconv.FormatInt(step.PathID, 10)), err)
	}
	steps := b.snap.stepsByPath[step.PathID]
	if pathDeleted {
		if len(steps) > 0 && steps[0].Position < step.Position {
			b.deleteLeafPath(step.PathID, step, reason)
		} else {
			b.deletePath(step.PathID, reason)
		}
		return nil
	}
	b.truncate(step.PathID, step.Position, reason, false, false)
	return nil
}

func (s *Server) finishTopologyImpact(ctx context.Context, b *topologyImpactBuilder, extraRule func(model.RoutingRule) bool) (topologyRemovalImpact, error) {
	impact := topologyRemovalImpact{Paths: []topologyPathImpact{}, Plans: []topologyNamedRef{}, PortForwards: []topologyNamedRef{}, Tunnels: []topologyNamedRef{}}
	planSeen := map[int64]bool{}
	deletedPaths := map[int64]bool{}
	for _, item := range b.paths {
		if item.Effect == "" {
			continue
		}
		if item.Effect == topologyEffectDeleted {
			deletedPaths[item.PathID] = true
			refs, err := s.store.PlanNodeReferences(ctx, model.AssignableNodeProxyPath, item.PathID)
			if err != nil {
				return impact, err
			}
			impact.ExceptionCount += refs.Exceptions
			for _, group := range [][]store.PlanNodeReference{refs.Active, refs.Draft, refs.Pending} {
				for _, ref := range group {
					if containsPlanRef(item.Plans, ref.PlanID) {
						continue
					}
					item.Plans = append(item.Plans, topologyNamedRef{ID: ref.PlanID, Name: ref.Name})
					if !planSeen[ref.PlanID] {
						planSeen[ref.PlanID] = true
						impact.Plans = append(impact.Plans, topologyNamedRef{ID: ref.PlanID, Name: ref.Name})
					}
				}
			}
		}
		switch item.Effect {
		case topologyEffectDeleted:
			impact.DeletedPathCount++
		case topologyEffectTruncated:
			impact.TruncatedPathCount++
		case topologyEffectKeptDirect:
			impact.KeptDirectCount++
		}
		impact.RemovedStepCount += item.RemovedSteps
		impact.Paths = append(impact.Paths, *item)
	}
	sort.Slice(impact.Paths, func(i, j int) bool {
		if impact.Paths[i].RootServerID != impact.Paths[j].RootServerID {
			return impact.Paths[i].RootServerID < impact.Paths[j].RootServerID
		}
		return impact.Paths[i].PathID < impact.Paths[j].PathID
	})
	for _, rule := range b.snap.rules {
		if rule.StageStepID != nil && b.removedSteps[*rule.StageStepID] ||
			rule.ProxyPathID != nil && deletedPaths[*rule.ProxyPathID] ||
			extraRule != nil && extraRule(rule) {
			impact.RoutingRuleCount++
		}
	}
	return impact, nil
}

func containsPlanRef(items []topologyNamedRef, id int64) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func (s *Server) addInboundPlanImpact(ctx context.Context, impact *topologyRemovalImpact, inboundID int64) error {
	refs, err := s.store.PlanNodeReferences(ctx, model.AssignableNodeInbound, inboundID)
	if err != nil {
		return err
	}
	impact.ExceptionCount += refs.Exceptions
	for _, group := range [][]store.PlanNodeReference{refs.Active, refs.Draft, refs.Pending} {
		for _, ref := range group {
			if !containsPlanRef(impact.Plans, ref.PlanID) {
				impact.Plans = append(impact.Plans, topologyNamedRef{ID: ref.PlanID, Name: ref.Name})
			}
		}
	}
	return nil
}

func forwardsForServer(snap *topologySnapshot, serverID int64) ([]topologyNamedRef, []topologyNamedRef) {
	forwards := []topologyNamedRef{}
	for _, item := range snap.portForwards {
		if item.SourceServerID == serverID || item.TargetServerID == serverID {
			forwards = append(forwards, topologyNamedRef{ID: item.ID, Name: firstNonEmpty(strings.TrimSpace(item.Name), "端口转发 "+strconv.FormatInt(item.ID, 10))})
		}
	}
	tunnels := []topologyNamedRef{}
	for _, item := range snap.tunnels {
		if item.SourceServerID == serverID || item.TargetServerID == serverID {
			tunnels = append(tunnels, topologyNamedRef{ID: item.ID, Name: firstNonEmpty(strings.TrimSpace(item.Name), "隧道 "+strconv.FormatInt(item.ID, 10))})
		}
	}
	return forwards, tunnels
}

// serverDeletionImpact mirrors store.DeleteServer: every path that traverses
// the server anywhere is disabled and cut at its first step on it (an emptied
// path is deleted), and every path rooted at one of its inbounds is deleted.
func (s *Server) serverDeletionImpact(ctx context.Context, serverID int64) (topologyRemovalImpact, error) {
	snap, err := s.loadTopologySnapshot(ctx)
	if err != nil {
		return topologyRemovalImpact{}, err
	}
	b := newTopologyImpactBuilder(snap)
	for _, pathID := range snap.sortedPathIDs() {
		for _, step := range snap.stepsByPath[pathID] {
			if snap.stepServerID(step) == serverID {
				b.truncate(pathID, step.Position, topologyReasonHop, true, true)
				break
			}
		}
	}
	inboundIDs := []int64{}
	for _, inbound := range snap.inbounds {
		if inbound.ServerID == serverID {
			inboundIDs = append(inboundIDs, inbound.ID)
		}
	}
	sort.Slice(inboundIDs, func(i, j int) bool { return inboundIDs[i] < inboundIDs[j] })
	for _, pathID := range snap.sortedPathIDs() {
		if path := snap.paths[pathID]; path.InboundID > 0 && snap.inbounds[path.InboundID].ServerID == serverID {
			b.deletePath(pathID, topologyReasonRoot)
		}
	}
	impact, err := s.finishTopologyImpact(ctx, b, func(rule model.RoutingRule) bool {
		return rule.ServerID == serverID || rule.TargetServerID != nil && *rule.TargetServerID == serverID
	})
	if err != nil {
		return impact, err
	}
	impact.Inbounds = []topologyNamedRef{}
	for _, id := range inboundIDs {
		inbound := snap.inbounds[id]
		impact.Inbounds = append(impact.Inbounds, topologyNamedRef{ID: id, Name: firstNonEmpty(strings.TrimSpace(inbound.Name), "入口 "+strconv.FormatInt(id, 10))})
		if err := s.addInboundPlanImpact(ctx, &impact, id); err != nil {
			return impact, err
		}
	}
	for _, item := range impact.Paths {
		if item.RootServerID != serverID {
			impact.OtherRootPathCount++
		}
	}
	impact.PortForwards, impact.Tunnels = forwardsForServer(snap, serverID)
	impact.ForwardsDeleted = true
	return impact, nil
}

func (s *Server) proxyPathStepRemovalImpact(ctx context.Context, step model.ProxyPathStep) (topologyRemovalImpact, error) {
	snap, err := s.loadTopologySnapshot(ctx)
	if err != nil {
		return topologyRemovalImpact{}, err
	}
	b := newTopologyImpactBuilder(snap)
	if err := s.recordStepRemoval(ctx, b, step, topologyReasonHop); err != nil {
		return topologyRemovalImpact{}, err
	}
	return s.finishTopologyImpact(ctx, b, nil)
}

const (
	topologyTargetServer           = "server"
	topologyTargetInbound          = "inbound"
	topologyTargetExternalOutbound = "external_outbound"
)

type topologyDetachRequest struct {
	RootServerID int64  `json:"root_server_id"`
	TargetType   string `json:"target_type"`
	TargetID     int64  `json:"target_id"`
	Confirm      bool   `json:"confirm"`
}

type topologyDetachPlan struct {
	Impact      topologyRemovalImpact
	RootPathIDs []int64 // paths deleted because they are rooted at the target inbound
	Steps       []model.ProxyPathStep
}

func (s *Server) validateTopologyDetachTarget(ctx context.Context, request topologyDetachRequest) error {
	if request.RootServerID <= 0 || request.TargetID <= 0 {
		return errors.New("root_server_id and target_id are required")
	}
	if _, err := s.store.GetServer(ctx, request.RootServerID); err != nil {
		return fmt.Errorf("root server: %w", err)
	}
	switch request.TargetType {
	case topologyTargetServer:
		if request.TargetID == request.RootServerID {
			return errors.New("一级服务器不能从自己的拓扑中移除；如需删除服务器注册，请到服务器页面操作")
		}
		if _, err := s.store.GetServer(ctx, request.TargetID); err != nil {
			return fmt.Errorf("target server: %w", err)
		}
	case topologyTargetInbound:
		if _, err := s.store.GetInbound(ctx, request.TargetID); err != nil {
			return fmt.Errorf("target inbound: %w", err)
		}
	case topologyTargetExternalOutbound:
		if _, err := s.store.GetExternalOutbound(ctx, request.TargetID); err != nil {
			return fmt.Errorf("target external outbound: %w", err)
		}
	default:
		return errors.New("target_type must be server, inbound or external_outbound")
	}
	return nil
}

func topologyStepMatches(snap *topologySnapshot, step model.ProxyPathStep, targetType string, targetID int64) bool {
	switch targetType {
	case topologyTargetServer:
		return snap.stepServerID(step) == targetID
	case topologyTargetInbound:
		return step.InboundID != nil && *step.InboundID == targetID
	case topologyTargetExternalOutbound:
		return step.ExternalOutboundID != nil && *step.ExternalOutboundID == targetID
	}
	return false
}

// planTopologyDetach removes the target only from the paths of one topology
// (paths rooted at an inbound of root_server_id). The server, inbound or
// imported node itself, its other topologies, port forwards and tunnels stay.
func (s *Server) planTopologyDetach(ctx context.Context, request topologyDetachRequest) (topologyDetachPlan, error) {
	if err := s.validateTopologyDetachTarget(ctx, request); err != nil {
		return topologyDetachPlan{}, err
	}
	snap, err := s.loadTopologySnapshot(ctx)
	if err != nil {
		return topologyDetachPlan{}, err
	}
	plan := topologyDetachPlan{}
	b := newTopologyImpactBuilder(snap)
	for _, pathID := range snap.sortedPathIDs() {
		path := snap.paths[pathID]
		if path.InboundID <= 0 || snap.pathRootServerID(path) != request.RootServerID {
			continue
		}
		if request.TargetType == topologyTargetInbound && path.InboundID == request.TargetID {
			if _, err := s.guardAssignableNodeDelete(ctx, model.AssignableNodeProxyPath, pathID); err != nil {
				return topologyDetachPlan{}, err
			}
			plan.RootPathIDs = append(plan.RootPathIDs, pathID)
			b.deletePath(pathID, topologyReasonRoot)
			continue
		}
		for _, step := range snap.stepsByPath[pathID] {
			if topologyStepMatches(snap, step, request.TargetType, request.TargetID) {
				plan.Steps = append(plan.Steps, step)
				break
			}
		}
	}
	for _, step := range plan.Steps {
		if item, ok := b.paths[step.PathID]; ok && item.Effect == topologyEffectDeleted {
			continue
		}
		if err := s.recordStepRemoval(ctx, b, step, topologyReasonHop); err != nil {
			return topologyDetachPlan{}, err
		}
	}
	impact, err := s.finishTopologyImpact(ctx, b, nil)
	if err != nil {
		return topologyDetachPlan{}, err
	}
	if request.TargetType == topologyTargetServer {
		impact.PortForwards, impact.Tunnels = forwardsForServer(snap, request.TargetID)
	}
	plan.Impact = impact
	return plan, nil
}

// applyTopologyDetach executes a plan. Each path is re-read and re-planned
// right before it is cut, because removing one branch can already have removed
// a path copied from it.
func (s *Server) applyTopologyDetach(ctx context.Context, plan topologyDetachPlan) error {
	for _, pathID := range plan.RootPathIDs {
		if _, err := s.store.GetProxyPath(ctx, pathID); errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return err
		}
		if err := s.store.DeleteProxyPath(ctx, pathID); err != nil {
			return err
		}
	}
	for _, planned := range plan.Steps {
		current, err := s.store.GetProxyPathStep(ctx, planned.ID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		_, deletePath, err := s.planProxyPathStepRemoval(ctx, *current)
		if err != nil {
			return err
		}
		if _, _, err := s.applyProxyPathStepRemoval(ctx, *current, deletePath); err != nil {
			return err
		}
	}
	return s.reconcileProxyPathNameTemplates(ctx)
}

func topologyDetachAuditDetail(request topologyDetachRequest, impact topologyRemovalImpact, source string) string {
	return fmt.Sprintf("root_server_id=%d target=%s:%d deleted_paths=%d truncated_paths=%d source=%s", request.RootServerID, request.TargetType, request.TargetID, impact.DeletedPathCount, impact.TruncatedPathCount+impact.KeptDirectCount, source)
}

// topologyDetachHandler serves /api/v1/topology/detach: GET with
// root_server_id, target_type and target_id previews the impact, POST applies.
func (s *Server) topologyDetachHandler(w http.ResponseWriter, r *http.Request) {
	var request topologyDetachRequest
	switch r.Method {
	case http.MethodGet:
		query := r.URL.Query()
		request.RootServerID, _ = strconv.ParseInt(query.Get("root_server_id"), 10, 64)
		request.TargetID, _ = strconv.ParseInt(query.Get("target_id"), 10, 64)
		request.TargetType = query.Get("target_type")
	case http.MethodPost:
		if !decode(w, r, &request) {
			return
		}
		if !request.Confirm {
			fail(w, errors.New("confirm=true is required"), 400)
			return
		}
	default:
		method(w)
		return
	}
	plan, err := s.planTopologyDetach(r.Context(), request)
	if err != nil {
		fail(w, err, topologyErrorStatus(err))
		return
	}
	if r.Method == http.MethodGet {
		write(w, 200, map[string]any{"impact": plan.Impact})
		return
	}
	if err := s.applyTopologyDetach(r.Context(), plan); err != nil {
		fail(w, err, 500)
		return
	}
	auditReq(s, r, "detach", "topology", topologyDetachAuditDetail(request, plan.Impact, "topology"))
	write(w, 200, map[string]any{"detached": true, "impact": plan.Impact, "requires_deployment": true})
}

func topologyErrorStatus(err error) int {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return http.StatusNotFound
	case strings.Contains(err.Error(), "still applying"):
		return http.StatusConflict
	}
	return http.StatusBadRequest
}

func (s *Server) registerTopologyRemovalAutomation() {
	candidate := func(ctx context.Context, principal application.Principal, input json.RawMessage) (topologyDetachRequest, topologyDetachPlan, error) {
		var request topologyDetachRequest
		if err := strictAutomationInput(input, &request); err != nil {
			return request, topologyDetachPlan{}, err
		}
		if !request.Confirm {
			return request, topologyDetachPlan{}, errors.New("confirm=true is required")
		}
		if !principal.AllowsInt64("server_ids", request.RootServerID) {
			return request, topologyDetachPlan{}, errors.New("authorized root_server_id is required")
		}
		if request.TargetType == topologyTargetServer && !principal.AllowsInt64("server_ids", request.TargetID) {
			return request, topologyDetachPlan{}, errors.New("authorized target server is required")
		}
		plan, err := s.planTopologyDetach(ctx, request)
		return request, plan, err
	}
	s.automation.RegisterValidator("topology.detach", func(ctx context.Context, principal application.Principal, input json.RawMessage) (any, error) {
		_, plan, err := candidate(ctx, principal, input)
		if err != nil {
			return nil, err
		}
		return map[string]any{"impact": plan.Impact, "requires_deployment": true}, nil
	})
	s.automation.RegisterRevisionResolver("topology.detach", func(ctx context.Context, principal application.Principal, input json.RawMessage) (map[string]string, error) {
		if _, _, err := candidate(ctx, principal, input); err != nil {
			return nil, err
		}
		return s.routingTopologyAutomationRevision(ctx)
	})
	s.automation.Register("topology.detach", func(ctx context.Context, principal application.Principal, input json.RawMessage) (any, error) {
		request, plan, err := candidate(ctx, principal, input)
		if err != nil {
			return nil, err
		}
		if err := s.applyTopologyDetach(ctx, plan); err != nil {
			return nil, err
		}
		_ = s.store.AddAudit(ctx, model.AuditLog{ActorID: principal.UserID, Action: "detach", Target: "topology", Detail: topologyDetachAuditDetail(request, plan.Impact, "mcp"), IP: "mcp"})
		return map[string]any{"detached": true, "impact": plan.Impact, "requires_deployment": true}, nil
	})
}

// deleteInboundRecord is the one inbound deletion used by REST and MCP.
// Provider records go first: a provider failure leaves the inbound and its
// plan membership untouched instead of a half-deleted inbound.
func (s *Server) deleteInboundRecord(ctx context.Context, inbound model.Inbound) (int, error) {
	if _, err := s.guardAssignableNodeDelete(ctx, model.AssignableNodeInbound, inbound.ID); err != nil {
		return http.StatusConflict, err
	}
	paths, err := s.store.ListProxyPaths(ctx)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	for _, path := range paths {
		if path.InboundID == inbound.ID {
			if _, err := s.guardAssignableNodeDelete(ctx, model.AssignableNodeProxyPath, path.ID); err != nil {
				return http.StatusConflict, err
			}
		}
	}
	if err := s.deleteDNSInboundRecords(ctx, inbound); err != nil {
		return http.StatusBadGateway, err
	}
	if err := s.store.DeleteInbound(ctx, inbound.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return http.StatusNotFound, err
		}
		return http.StatusInternalServerError, err
	}
	if err := s.reconcileProxyPathNameTemplates(ctx); err != nil {
		return http.StatusInternalServerError, err
	}
	return 0, nil
}

func (s *Server) deleteExternalOutboundRecord(ctx context.Context, externalID int64) (int, error) {
	if _, err := s.guardAssignableNodeDelete(ctx, model.AssignableNodeExternalOutbound, externalID); err != nil {
		return http.StatusConflict, err
	}
	if err := s.store.DeleteExternalOutbound(ctx, externalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return http.StatusNotFound, err
		}
		return http.StatusInternalServerError, err
	}
	if err := s.reconcileProxyPathNameTemplates(ctx); err != nil {
		return http.StatusInternalServerError, err
	}
	return 0, nil
}

// serverDeleteConfirmation is the typed confirmation a destructive server
// deletion needs: the server's name (or #id when it has none).
func serverDeleteConfirmationName(server model.Server) string {
	if name := strings.TrimSpace(server.Name); name != "" {
		return name
	}
	return "#" + strconv.FormatInt(server.ID, 10)
}

func checkServerDeleteConfirmation(server model.Server, confirmName string) error {
	if strings.TrimSpace(confirmName) != serverDeleteConfirmationName(server) {
		return fmt.Errorf("confirm_name must equal the server name %q", serverDeleteConfirmationName(server))
	}
	return nil
}

var serverDeleteSources = map[string]bool{"servers": true, "agent_uninstall": true}

func serverDeleteAuditDetail(id int64, name, source string) string {
	return fmt.Sprintf("id=%d name=%s source=%s", id, strconv.Quote(name), source)
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

// Builds before in-transaction cleanup left exceptions pointing at deleted
// paths, inbounds and imported nodes. The sweep removes exactly those rows.
func TestPruneDanglingUserNodeExceptionsFromPreviousState(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()
	user := &model.User{Username: "member", PasswordHash: "unused", Role: model.RoleViewer, Status: "active", ProxyUUID: "33333333-3333-4333-8333-333333333333", ProxyPassword: "unused"}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	server := &model.Server{Name: "edge", ListenIP: "0.0.0.0", PortRangeStart: 10000, PortRangeEnd: 10010}
	if err := db.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	inbound := &model.Inbound{ServerID: server.ID, Name: "entry", Protocol: model.Protocol("vless"), ListenIP: "0.0.0.0", Port: 443, ConfigJSON: "{}", Enabled: true}
	if err := db.CreateInbound(ctx, inbound); err != nil {
		t.Fatal(err)
	}
	ts := now()
	for _, row := range []struct {
		nodeType string
		nodeID   int64
	}{{"inbound", inbound.ID}, {"inbound", 9001}, {"proxy_path", 9002}, {"external_outbound", 9003}} {
		if _, err := db.db.ExecContext(ctx, `insert into user_node_exceptions(user_id,node_type,node_id,effect,status,created_at) values(?,?,?,'allow','active',?)`, user.ID, row.nodeType, row.nodeID, ts); err != nil {
			t.Fatal(err)
		}
	}
	for attempt, want := range []int64{3, 0} {
		removed, err := db.PruneDanglingUserNodeExceptions(ctx)
		if err != nil || removed != want {
			t.Fatalf("attempt %d removed=%d err=%v, want %d", attempt, removed, err, want)
		}
	}
	left, err := db.ListUserNodeExceptionsForUser(ctx, user.ID)
	if err != nil || len(left) != 1 || left[0].NodeID != inbound.ID {
		t.Fatalf("remaining=%#v err=%v", left, err)
	}
	if err := db.DeleteInbound(ctx, inbound.ID); err != nil {
		t.Fatal(err)
	}
	if left, err := db.ListUserNodeExceptionsForUser(ctx, user.ID); err != nil || len(left) != 0 {
		t.Fatalf("inbound deletion kept exceptions: %#v %v", left, err)
	}
}

// Earlier builds left disabled proxy paths behind (for example truncated paths
// after server deletion, disabled zero-step paths, or aborted drafts), still
// attached to subscription plans and user node exceptions. PruneDisabledProxyPaths
// sweeps them cleanly while keeping enabled paths, live detached branches,
// template-owned family_branch rows, fresh in-flight drafts, and paths on plans
// currently applying an access change.
func TestPruneDisabledProxyPathsFromPreviousState(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()

	user := &model.User{Username: "member", PasswordHash: "unused", Role: model.RoleViewer, Status: "active", ProxyUUID: "55555555-5555-4555-8555-555555555555", ProxyPassword: "unused"}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	serverA := &model.Server{Name: "CDT", ListenIP: "0.0.0.0", PortRangeStart: 10000, PortRangeEnd: 10100}
	serverB := &model.Server{Name: "HKT", ListenIP: "0.0.0.0", PortRangeStart: 10200, PortRangeEnd: 10300}
	if err := db.CreateServer(ctx, serverA); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateServer(ctx, serverB); err != nil {
		t.Fatal(err)
	}
	inbound := &model.Inbound{ServerID: serverA.ID, Name: "cdt-anytls", Protocol: model.ProtocolAnyTLS, ListenIP: "0.0.0.0", Port: 10777, ConfigJSON: "{}", Enabled: true}
	if err := db.CreateInbound(ctx, inbound); err != nil {
		t.Fatal(err)
	}

	livePath := &model.ProxyPath{InboundID: inbound.ID, Kind: model.ProxyPathKindDirect, Secret: "live", Enabled: true}
	disabledZeroStep := &model.ProxyPath{InboundID: inbound.ID, Kind: model.ProxyPathKindDirect, Secret: "dead-direct", Enabled: false}
	disabledWithStep := &model.ProxyPath{InboundID: inbound.ID, Kind: model.ProxyPathKindChain, Secret: "dead-chain", Enabled: false}
	freshDraft := &model.ProxyPath{InboundID: inbound.ID, Kind: model.ProxyPathKindChain, Secret: "fresh-draft", Enabled: false}
	busyPlanDisabled := &model.ProxyPath{InboundID: inbound.ID, Kind: model.ProxyPathKindDirect, Secret: "busy-plan", Enabled: false}
	for _, p := range []*model.ProxyPath{livePath, disabledZeroStep, disabledWithStep, freshDraft, busyPlanDisabled} {
		if err := db.CreateProxyPath(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	deadStep := &model.ProxyPathStep{PathID: disabledWithStep.ID, Position: 1, NodeType: model.ProxyPathStepServerInbound, TransportMode: model.ProxyPathTransportSingBox, ServerID: &serverB.ID, ConfigJSON: "{}"}
	if err := db.CreateProxyPathStep(ctx, deadStep); err != nil {
		t.Fatal(err)
	}
	// A live direct branch referencing deadStep must be detached, never deleted.
	liveChildBranch := &model.ProxyPath{InboundID: inbound.ID, Kind: model.ProxyPathKindDirect, BranchSourceStepID: &deadStep.ID, Secret: "live-child", Enabled: true}
	if err := db.CreateProxyPath(ctx, liveChildBranch); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateProxyPathStep(ctx, &model.ProxyPathStep{PathID: liveChildBranch.ID, Position: 1, NodeType: model.ProxyPathStepServerInbound, TransportMode: model.ProxyPathTransportSingBox, ServerID: &serverB.ID, ConfigJSON: "{}"}); err != nil {
		t.Fatal(err)
	}
	// Template-owned family_branch rows start disabled and must never be pruned.
	tpl := &model.FamilySplitTemplate{Name: "unused-dual-stack"}
	if err := db.CreateFamilySplitTemplate(ctx, tpl, "v4-sec", "v6-sec"); err != nil {
		t.Fatal(err)
	}

	plan := &model.SubscriptionPlan{Name: "管理员", Enabled: true}
	if err := db.CreateSubscriptionPlan(ctx, plan, []model.SubscriptionPlanNode{
		{NodeType: model.AssignableNodeProxyPath, NodeID: livePath.ID, Enabled: true},
		{NodeType: model.AssignableNodeProxyPath, NodeID: disabledZeroStep.ID, Enabled: true},
		{NodeType: model.AssignableNodeProxyPath, NodeID: disabledWithStep.ID, Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	busyPlan := &model.SubscriptionPlan{Name: "applying-plan", Enabled: true}
	if err := db.CreateSubscriptionPlan(ctx, busyPlan, []model.SubscriptionPlanNode{
		{NodeType: model.AssignableNodeProxyPath, NodeID: busyPlanDisabled.ID, Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	// Simulate an in-flight access change applying on busyPlan.
	busyCandidate, err := db.CreatePlanVersion(ctx, busyPlan.ID, PlanVersionMutation{
		ExpectedLockVersion: busyPlan.LockVersion,
		Nodes:               &PlanNodesMutation{Op: "add", Nodes: []model.SubscriptionPlanNode{{NodeType: model.AssignableNodeProxyPath, NodeID: livePath.ID, Enabled: true}}},
		ChangeKind:          model.PlanChangeKindNodes,
	})
	if err != nil {
		t.Fatal(err)
	}
	applyingChange := &model.AccessChange{
		ChangeType: model.AccessChangePlanPublish, SourcePlanID: busyPlan.ID, CandidateRevisionID: busyCandidate.Revision.ID,
		ExpectedActiveRevisionID: busyPlan.CurrentRevisionID, Status: model.AccessChangePreparing,
		PayloadJSON: "{}", PrepareProjectionJSON: "{}", FinalizeProjectionJSON: "{}",
	}
	if _, err := db.CreateAccessChange(ctx, applyingChange, nil); err != nil {
		t.Fatal(err)
	}

	for _, nodeID := range []int64{livePath.ID, disabledZeroStep.ID, disabledWithStep.ID} {
		if err := db.CreateUserNodeException(ctx, &model.UserNodeException{UserID: user.ID, NodeType: model.AssignableNodeProxyPath, NodeID: nodeID, Effect: model.UserNodeExceptionAllow, Status: model.UserNodeExceptionActive}); err != nil {
			t.Fatal(err)
		}
	}
	override := "旧名称"
	if _, err := db.UpsertNodeMetadata(ctx, model.AssignableNodeProxyPath, disabledZeroStep.ID, &override, 0, nil); err != nil {
		t.Fatal(err)
	}

	// Age the old disabled rows past the cutoff while keeping freshDraft recent.
	oldTS := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339Nano)
	for _, id := range []int64{disabledZeroStep.ID, disabledWithStep.ID, busyPlanDisabled.ID, tpl.IPv4PathID, tpl.IPv6PathID} {
		if _, err := db.db.ExecContext(ctx, `update proxy_paths set updated_at=? where id=?`, oldTS, id); err != nil {
			t.Fatal(err)
		}
	}

	cutoff := time.Now().UTC().Add(-time.Minute)
	res, err := db.PruneDisabledProxyPaths(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.DeletedPathIDs) != 2 || res.DeletedPathIDs[0] != disabledZeroStep.ID || res.DeletedPathIDs[1] != disabledWithStep.ID {
		t.Fatalf("deleted=%v, want [%d %d]", res.DeletedPathIDs, disabledZeroStep.ID, disabledWithStep.ID)
	}
	if len(res.SkippedPathIDs) != 1 || res.SkippedPathIDs[0] != busyPlanDisabled.ID {
		t.Fatalf("skipped=%v, want [%d]", res.SkippedPathIDs, busyPlanDisabled.ID)
	}

	// Deleted paths, their plan membership, exceptions and metadata are gone.
	for _, id := range []int64{disabledZeroStep.ID, disabledWithStep.ID} {
		if _, err := db.GetProxyPath(ctx, id); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("disabled path %d still exists: %v", id, err)
		}
		if refs, err := db.PlanNodeReferences(ctx, model.AssignableNodeProxyPath, id); err != nil || len(refs.Active) != 0 || refs.Exceptions != 0 {
			t.Fatalf("disabled path %d left refs=%#v err=%v", id, refs, err)
		}
	}
	if meta, err := db.GetNodeMetadata(ctx, model.AssignableNodeProxyPath, disabledZeroStep.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("disabled path metadata remained: %#v err=%v", meta, err)
	}
	planNodes, err := db.ListActivePlanNodes(ctx, plan.ID)
	if err != nil || len(planNodes) != 1 || planNodes[0].NodeID != livePath.ID {
		t.Fatalf("plan active nodes = %#v err=%v", planNodes, err)
	}
	exceptions, err := db.ListUserNodeExceptionsForUser(ctx, user.ID)
	if err != nil || len(exceptions) != 1 || exceptions[0].NodeID != livePath.ID {
		t.Fatalf("remaining exceptions = %#v err=%v", exceptions, err)
	}

	// Live path, live detached child branch, fresh draft, and template branches survive.
	if _, err := db.GetProxyPath(ctx, livePath.ID); err != nil {
		t.Fatalf("live path disappeared: %v", err)
	}
	child, err := db.GetProxyPath(ctx, liveChildBranch.ID)
	if err != nil || !child.Enabled || child.BranchSourceStepID != nil {
		t.Fatalf("live child branch should survive detached: %#v err=%v", child, err)
	}
	if _, err := db.GetProxyPath(ctx, freshDraft.ID); err != nil {
		t.Fatalf("fresh draft within grace window should survive: %v", err)
	}
	for _, id := range []int64{tpl.IPv4PathID, tpl.IPv6PathID} {
		if _, err := db.GetProxyPath(ctx, id); err != nil {
			t.Fatalf("family_branch %d disappeared: %v", id, err)
		}
	}

	// Once the applying access change finishes and cutoff is zero (startup sweep),
	// both busyPlanDisabled and freshDraft are pruned, and a repeat sweep is a no-op.
	if _, err := db.db.ExecContext(ctx, `update access_changes set status='completed' where id=?`, applyingChange.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx, `update subscription_plans set pending_revision_id=null where id=?`, busyPlan.ID); err != nil {
		t.Fatal(err)
	}
	res2, err := db.PruneDisabledProxyPaths(ctx, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.DeletedPathIDs) != 2 || len(res2.SkippedPathIDs) != 0 {
		t.Fatalf("second sweep deleted=%v skipped=%v, want 2 deleted and 0 skipped", res2.DeletedPathIDs, res2.SkippedPathIDs)
	}
	res3, err := db.PruneDisabledProxyPaths(ctx, time.Time{})
	if err != nil || len(res3.DeletedPathIDs) != 0 || len(res3.SkippedPathIDs) != 0 {
		t.Fatalf("idempotent sweep = %#v err=%v", res3, err)
	}
	for _, id := range []int64{tpl.IPv4PathID, tpl.IPv6PathID} {
		if _, err := db.GetProxyPath(ctx, id); err != nil {
			t.Fatalf("family_branch %d disappeared after zero-cutoff sweep: %v", id, err)
		}
	}
}

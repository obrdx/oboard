package controller

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/store"
)

type topologyFixture struct {
	h                       http.Handler
	db                      *store.Store
	token                   string
	serverA, serverB        int64
	serverC, serverD        int64
	pathA, pathD            int64
	pathAFirst, pathASecond int64
	inboundA, inboundD      int64
}

// Two entry servers (A, D) both route through B; A continues to C.
func newTopologyFixture(t *testing.T) topologyFixture {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "oboard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := newTestServer(db, "test-secret", "").Handler()
	request(t, h, http.MethodPost, "/api/v1/ui/auth/bootstrap", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusCreated)
	token := request(t, h, http.MethodPost, "/api/v1/ui/auth/login", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusOK)["token"].(string)
	id := func(v map[string]any, key string) int64 { return int64(v[key].(map[string]any)["id"].(float64)) }
	server := func(name, address string, port int) int64 {
		return id(request(t, h, http.MethodPost, "/api/v1/ui/servers", token, map[string]any{"name": name, "entry_ip_mode": "custom", "entry_address": address, "listen_ip": "0.0.0.0", "port_range_start": port, "port_range_end": port + 100}, http.StatusCreated), "server")
	}
	f := topologyFixture{h: h, db: db, token: token}
	f.serverA = server("A", "203.0.113.1", 30000)
	f.serverB = server("B", "203.0.113.2", 31000)
	f.serverC = server("C", "203.0.113.3", 32000)
	f.serverD = server("D", "203.0.113.4", 33000)
	inbound := func(serverID int64) int64 {
		return id(request(t, h, http.MethodPost, "/api/v1/ui/inbounds", token, map[string]any{"server_id": serverID, "name": "entry", "protocol": "vless", "listen_ip": "0.0.0.0", "port": 443, "config_json": `{}`, "enabled": true}, http.StatusCreated), "inbound")
	}
	path := func(name string, inboundID int64) int64 {
		return id(request(t, h, http.MethodPost, "/api/v1/ui/proxy-paths", token, map[string]any{"name": name, "inbound_id": inboundID, "enabled": true}, http.StatusCreated), "proxy_path")
	}
	step := func(pathID int64, position int, serverID int64) int64 {
		return id(request(t, h, http.MethodPost, "/api/v1/ui/proxy-path-steps", token, map[string]any{"path_id": pathID, "position": position, "node_type": "server_inbound", "server_id": serverID, "transport_mode": "singbox", "config_json": `{}`}, http.StatusCreated), "proxy_path_step")
	}
	f.inboundA = inbound(f.serverA)
	f.inboundD = inbound(f.serverD)
	f.pathA = path("A-C-B", f.inboundA)
	f.pathAFirst = step(f.pathA, 1, f.serverC)
	f.pathASecond = step(f.pathA, 2, f.serverB)
	f.pathD = path("D-B", f.inboundD)
	step(f.pathD, 1, f.serverB)
	return f
}

func impactPaths(t *testing.T, body map[string]any) map[int64]map[string]any {
	t.Helper()
	out := map[int64]map[string]any{}
	for _, raw := range body["impact"].(map[string]any)["paths"].([]any) {
		item := raw.(map[string]any)
		out[int64(item["path_id"].(float64))] = item
	}
	return out
}

func TestTopologyDetachServerOnlyCutsCurrentTopology(t *testing.T) {
	f := newTopologyFixture(t)
	query := "/api/v1/ui/topology/detach?root_server_id=" + itoa(f.serverA) + "&target_type=server&target_id=" + itoa(f.serverB)
	preview := request(t, f.h, http.MethodGet, query, f.token, nil, http.StatusOK)
	paths := impactPaths(t, preview)
	if len(paths) != 1 || paths[f.pathA]["effect"] != topologyEffectTruncated || paths[f.pathA]["removed_steps"].(float64) != 1 {
		t.Fatalf("preview = %#v", preview)
	}
	// Preview does not write.
	request(t, f.h, http.MethodGet, "/api/v1/ui/proxy-path-steps/"+itoa(f.pathASecond), f.token, nil, http.StatusOK)

	request(t, f.h, http.MethodPost, "/api/v1/ui/topology/detach", f.token, map[string]any{"root_server_id": f.serverA, "target_type": "server", "target_id": f.serverB}, http.StatusBadRequest)
	request(t, f.h, http.MethodPost, "/api/v1/ui/topology/detach", f.token, map[string]any{"root_server_id": f.serverA, "target_type": "server", "target_id": f.serverB, "confirm": true}, http.StatusOK)

	request(t, f.h, http.MethodGet, "/api/v1/ui/servers/"+itoa(f.serverB), f.token, nil, http.StatusOK)
	request(t, f.h, http.MethodGet, "/api/v1/ui/proxy-path-steps/"+itoa(f.pathASecond), f.token, nil, http.StatusNotFound)
	request(t, f.h, http.MethodGet, "/api/v1/ui/proxy-path-steps/"+itoa(f.pathAFirst), f.token, nil, http.StatusOK)
	steps, err := f.db.ListProxyPathStepsForPath(context.Background(), f.pathD)
	if err != nil || len(steps) != 1 {
		t.Fatalf("other topology changed: %#v %v", steps, err)
	}
	stored, err := f.db.GetProxyPath(context.Background(), f.pathA)
	if err != nil || !stored.Enabled {
		t.Fatalf("detached path should stay enabled: %#v %v", stored, err)
	}
}

func TestTopologyDetachRejectsRootServer(t *testing.T) {
	f := newTopologyFixture(t)
	request(t, f.h, http.MethodPost, "/api/v1/ui/topology/detach", f.token, map[string]any{"root_server_id": f.serverA, "target_type": "server", "target_id": f.serverA, "confirm": true}, http.StatusBadRequest)
	request(t, f.h, http.MethodGet, "/api/v1/ui/servers/"+itoa(f.serverA), f.token, nil, http.StatusOK)
}

func TestTopologyDetachRootInboundDeletesOnlyItsPaths(t *testing.T) {
	f := newTopologyFixture(t)
	request(t, f.h, http.MethodPost, "/api/v1/ui/topology/detach", f.token, map[string]any{"root_server_id": f.serverA, "target_type": "inbound", "target_id": f.inboundA, "confirm": true}, http.StatusOK)
	request(t, f.h, http.MethodGet, "/api/v1/ui/proxy-paths/"+itoa(f.pathA), f.token, nil, http.StatusNotFound)
	request(t, f.h, http.MethodGet, "/api/v1/ui/inbounds/"+itoa(f.inboundA), f.token, nil, http.StatusOK)
	request(t, f.h, http.MethodGet, "/api/v1/ui/proxy-paths/"+itoa(f.pathD), f.token, nil, http.StatusOK)
}

func TestServerDeletionPreviewIncludesDisabledAndForeignPaths(t *testing.T) {
	f := newTopologyFixture(t)
	if err := f.db.SetProxyPathEnabledForRepair(context.Background(), f.pathD, false); err != nil {
		t.Fatal(err)
	}
	// A direct branch copied from C (step 1 of pathA) does not traverse B and
	// must survive deleting B, while pathA (A->C->B) and disabled pathD (D->B)
	// are both deleted completely instead of leaving disabled prefixes.
	prefixBranch := request(t, f.h, http.MethodPost, "/api/v1/ui/proxy-paths/direct-branches", f.token, map[string]any{"source_step_id": f.pathAFirst}, http.StatusCreated)["proxy_path"].(map[string]any)
	prefixBranchID := int64(prefixBranch["id"].(float64))

	preview := request(t, f.h, http.MethodGet, "/api/v1/ui/servers/"+itoa(f.serverB)+"/deletion-preview", f.token, nil, http.StatusOK)
	if preview["confirm_name"] != "B" {
		t.Fatalf("confirm_name = %#v", preview["confirm_name"])
	}
	paths := impactPaths(t, preview)
	a, d := paths[f.pathA], paths[f.pathD]
	if a == nil || a["effect"] != topologyEffectDeleted {
		t.Fatalf("path A impact = %#v", a)
	}
	if d == nil || d["effect"] != topologyEffectDeleted || d["enabled"] != false {
		t.Fatalf("disabled path D impact = %#v", d)
	}
	if paths[prefixBranchID] != nil {
		t.Fatalf("unaffected prefix branch should not be impacted: %#v", paths[prefixBranchID])
	}
	if preview["impact"].(map[string]any)["other_root_path_count"].(float64) != 2 {
		t.Fatalf("other_root_path_count = %#v", preview["impact"])
	}

	deleteServerConfirmed(t, f.h, f.token, f.serverB, http.StatusOK)
	request(t, f.h, http.MethodGet, "/api/v1/ui/proxy-paths/"+itoa(f.pathD), f.token, nil, http.StatusNotFound)
	request(t, f.h, http.MethodGet, "/api/v1/ui/proxy-paths/"+itoa(f.pathA), f.token, nil, http.StatusNotFound)
	kept, err := f.db.GetProxyPath(context.Background(), prefixBranchID)
	if err != nil || !kept.Enabled || kept.BranchSourceStepID != nil {
		t.Fatalf("unaffected prefix branch should survive detached: %#v err=%v", kept, err)
	}
}

func TestControllerPruneDisabledProxyPathsCleansCatalogAndPlans(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "oboard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv := newTestServer(db, "test-secret", "")
	h := srv.Handler()
	request(t, h, http.MethodPost, "/api/v1/ui/auth/bootstrap", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusCreated)
	token := request(t, h, http.MethodPost, "/api/v1/ui/auth/login", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusOK)["token"].(string)
	ctx := context.Background()

	server := &model.Server{Name: "CDT", EntryAddress: "203.0.113.10", ListenIP: "0.0.0.0", PortRangeStart: 30000, PortRangeEnd: 30100, Status: model.ServerOnline}
	if err := db.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	inbound := &model.Inbound{ServerID: server.ID, Name: "cdt-anytls", Protocol: model.ProtocolAnyTLS, ListenIP: "0.0.0.0", Port: 10777, ConfigJSON: `{}`, Enabled: true}
	if err := db.CreateInbound(ctx, inbound); err != nil {
		t.Fatal(err)
	}
	live := &model.ProxyPath{InboundID: inbound.ID, Kind: model.ProxyPathKindDirect, Secret: "live", Enabled: true}
	disabled := &model.ProxyPath{InboundID: inbound.ID, Kind: model.ProxyPathKindDirect, Secret: "disabled", Enabled: false}
	if err := db.CreateProxyPath(ctx, live); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateProxyPath(ctx, disabled); err != nil {
		t.Fatal(err)
	}
	tpl := &model.FamilySplitTemplate{Name: "keep-template"}
	if err := db.CreateFamilySplitTemplate(ctx, tpl, "v4", "v6"); err != nil {
		t.Fatal(err)
	}
	plan := &model.SubscriptionPlan{Name: "管理员", Enabled: true}
	if err := db.CreateSubscriptionPlan(ctx, plan, []model.SubscriptionPlanNode{
		{NodeType: model.AssignableNodeProxyPath, NodeID: live.ID, Enabled: true},
		{NodeType: model.AssignableNodeProxyPath, NodeID: disabled.ID, Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	user := &model.User{Username: "u1", PasswordHash: "unused", Role: model.RoleViewer, Status: "active", ProxyUUID: "66666666-6666-4666-8666-666666666666", ProxyPassword: "unused"}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUserNodeException(ctx, &model.UserNodeException{UserID: user.ID, NodeType: model.AssignableNodeProxyPath, NodeID: disabled.ID, Effect: model.UserNodeExceptionAllow, Status: model.UserNodeExceptionActive}); err != nil {
		t.Fatal(err)
	}

	before := request(t, h, http.MethodGet, "/api/v1/ui/assignable-nodes?status=disabled", token, nil, http.StatusOK)["nodes"].([]any)
	if len(before) != 1 {
		t.Fatalf("expected 1 disabled node before prune, got %#v", before)
	}

	res, err := srv.PruneDisabledProxyPaths(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.DeletedPathIDs) != 1 || res.DeletedPathIDs[0] != disabled.ID {
		t.Fatalf("pruned = %#v, want [%d]", res, disabled.ID)
	}

	after := request(t, h, http.MethodGet, "/api/v1/ui/assignable-nodes?status=disabled", token, nil, http.StatusOK)["nodes"].([]any)
	if len(after) != 0 {
		t.Fatalf("expected 0 disabled nodes after prune, got %#v", after)
	}
	nodes, err := db.ListActivePlanNodes(ctx, plan.ID)
	if err != nil || len(nodes) != 1 || nodes[0].NodeID != live.ID {
		t.Fatalf("plan nodes after prune = %#v err=%v", nodes, err)
	}
	exceptions, err := db.ListUserNodeExceptionsForUser(ctx, user.ID)
	if err != nil || len(exceptions) != 0 {
		t.Fatalf("exceptions after prune = %#v err=%v", exceptions, err)
	}
	if _, err := db.GetFamilySplitTemplate(ctx, tpl.ID); err != nil {
		t.Fatalf("family split template affected: %v", err)
	}
}

func TestServerDeleteRequiresTypedName(t *testing.T) {
	f := newTopologyFixture(t)
	path := "/api/v1/ui/servers/" + itoa(f.serverB)
	request(t, f.h, http.MethodDelete, path, f.token, map[string]any{}, http.StatusBadRequest)
	request(t, f.h, http.MethodDelete, path, f.token, map[string]any{"confirm_name": "b"}, http.StatusBadRequest)
	request(t, f.h, http.MethodGet, path, f.token, nil, http.StatusOK)
	deleteServerConfirmed(t, f.h, f.token, f.serverB, http.StatusOK)
	logs, err := f.db.ListAuditPage(context.Background(), 50, 0, "delete")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range logs {
		if entry.Action == "delete" && entry.Target == "server" {
			if entry.Detail != serverDeleteAuditDetail(f.serverB, "B", "servers") {
				t.Fatalf("audit detail = %q", entry.Detail)
			}
			return
		}
	}
	t.Fatalf("server deletion was not audited: %#v", logs)
}

func TestProxyPathStepRemovalPreviewMatchesTruncation(t *testing.T) {
	f := newTopologyFixture(t)
	preview := request(t, f.h, http.MethodGet, "/api/v1/ui/proxy-path-steps/"+itoa(f.pathAFirst)+"/removal-preview", f.token, nil, http.StatusOK)
	paths := impactPaths(t, preview)
	if paths[f.pathA]["effect"] != topologyEffectDeleted || paths[f.pathA]["removed_steps"].(float64) != 2 {
		t.Fatalf("preview = %#v", preview)
	}
	result := request(t, f.h, http.MethodDelete, "/api/v1/ui/proxy-path-steps/"+itoa(f.pathAFirst), f.token, nil, http.StatusOK)
	if result["path_deleted"] != true {
		t.Fatalf("delete result = %#v", result)
	}
}

func TestDeletingInboundRemovesItsAccessExceptions(t *testing.T) {
	f := newTopologyFixture(t)
	ctx := context.Background()
	user := &model.User{Username: "member", PasswordHash: "unused", Role: model.RoleViewer, Status: "active", ProxyUUID: "22222222-2222-4222-8222-222222222222", ProxyPassword: "unused"}
	if err := f.db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	for _, node := range []model.UserNodeException{
		{UserID: user.ID, NodeType: model.AssignableNodeProxyPath, NodeID: f.pathA, Effect: model.UserNodeExceptionAllow},
		{UserID: user.ID, NodeType: model.AssignableNodeInbound, NodeID: f.inboundA, Effect: model.UserNodeExceptionAllow},
		{UserID: user.ID, NodeType: model.AssignableNodeProxyPath, NodeID: f.pathD, Effect: model.UserNodeExceptionAllow},
	} {
		node := node
		if err := f.db.CreateUserNodeException(ctx, &node); err != nil {
			t.Fatal(err)
		}
	}
	request(t, f.h, http.MethodDelete, "/api/v1/ui/inbounds/"+itoa(f.inboundA), f.token, nil, http.StatusOK)
	left, err := f.db.ListUserNodeExceptionsForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].NodeID != f.pathD {
		t.Fatalf("remaining exceptions = %#v", left)
	}
}

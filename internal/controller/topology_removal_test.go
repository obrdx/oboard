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
	preview := request(t, f.h, http.MethodGet, "/api/v1/ui/servers/"+itoa(f.serverB)+"/deletion-preview", f.token, nil, http.StatusOK)
	if preview["confirm_name"] != "B" {
		t.Fatalf("confirm_name = %#v", preview["confirm_name"])
	}
	paths := impactPaths(t, preview)
	a, d := paths[f.pathA], paths[f.pathD]
	if a == nil || a["effect"] != topologyEffectTruncated || a["disabled"] != true {
		t.Fatalf("path A impact = %#v", a)
	}
	// D's only step is on B, so the store deletes the emptied path even though
	// it is disabled; the old browser preview skipped disabled paths entirely.
	if d == nil || d["effect"] != topologyEffectDeleted || d["enabled"] != false {
		t.Fatalf("disabled path D impact = %#v", d)
	}
	if preview["impact"].(map[string]any)["other_root_path_count"].(float64) != 2 {
		t.Fatalf("other_root_path_count = %#v", preview["impact"])
	}

	deleteServerConfirmed(t, f.h, f.token, f.serverB, http.StatusOK)
	request(t, f.h, http.MethodGet, "/api/v1/ui/proxy-paths/"+itoa(f.pathD), f.token, nil, http.StatusNotFound)
	stored, err := f.db.GetProxyPath(context.Background(), f.pathA)
	if err != nil || stored.Enabled {
		t.Fatalf("path A should be cut and disabled as previewed: %#v %v", stored, err)
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

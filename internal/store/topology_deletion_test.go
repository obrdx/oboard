package store

import (
	"context"
	"testing"

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

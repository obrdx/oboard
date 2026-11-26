package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

func TestServerUpdateTrafficAndTimeCheckAreAtomic(t *testing.T) {
	for _, stage := range []string{"traffic", "time_check"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			db, err := Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			server := &model.Server{Name: "before", TimeCorrectionMode: model.TimeCorrectionOff}
			if err := db.CreateServer(ctx, server); err != nil {
				t.Fatal(err)
			}
			window := model.ServerTrafficWindow{Key: "2026-09", Start: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
			if err := db.SetServerTrafficUsed(ctx, server.ID, 10, window); err != nil {
				t.Fatal(err)
			}
			if _, err := db.db.ExecContext(ctx, `update server_telemetry set time_check_status='ok' where server_id=?`, server.ID); err != nil {
				t.Fatal(err)
			}
			before, err := db.GetServer(ctx, server.ID)
			if err != nil {
				t.Fatal(err)
			}
			revision, err := db.ConfigurationRevision(ctx)
			if err != nil {
				t.Fatal(err)
			}
			column := "period_key"
			if stage == "time_check" {
				column = "time_check_status"
			}
			if _, err := db.db.ExecContext(ctx, `create trigger reject_extra_update before update of `+column+` on server_telemetry begin select raise(abort,'injected failure'); end`); err != nil {
				t.Fatal(err)
			}
			candidate := *before
			candidate.Name = "after"
			candidate.TimeCorrectionMode = model.TimeCorrectionAuto
			if err := db.UpdateServerWithTraffic(ctx, &candidate, 20, window); err == nil {
				t.Fatal("injected failure ignored")
			}
			after, err := db.GetServer(ctx, server.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.Name != before.Name || after.TimeCorrectionMode != before.TimeCorrectionMode || after.TimeCheckStatus != before.TimeCheckStatus || after.TrafficUploadBytes != 10 || !after.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatalf("partial update: name=%s mode=%s check=%s traffic=%d", after.Name, after.TimeCorrectionMode, after.TimeCheckStatus, after.TrafficUploadBytes)
			}
			if next, err := db.ConfigurationRevision(ctx); err != nil || next != revision {
				t.Fatalf("failed update advanced revision: %d -> %d %v", revision, next, err)
			}
			if _, err := db.db.ExecContext(ctx, `drop trigger reject_extra_update`); err != nil {
				t.Fatal(err)
			}
			if err := db.UpdateServerWithTraffic(ctx, &candidate, 20, window); err != nil {
				t.Fatal(err)
			}
			after, err = db.GetServer(ctx, server.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.Name != "after" || after.TrafficUploadBytes != 20 || after.TimeCheckStatus != "pending" {
				t.Fatalf("retry did not commit all changes: %s %d %s", after.Name, after.TrafficUploadBytes, after.TimeCheckStatus)
			}
			if _, err := db.db.ExecContext(ctx, `update server_telemetry set time_check_status='ok' where server_id=?`, server.ID); err != nil {
				t.Fatal(err)
			}
			if err := db.UpdateServer(ctx, after); err != nil {
				t.Fatal(err)
			}
			after, err = db.GetServer(ctx, server.ID)
			if err != nil || after.TimeCheckStatus != "ok" {
				t.Fatalf("unchanged mode reset verification: %v %v", after, err)
			}
		})
	}
}

func TestUpdateServerRollsBackAllSettingsAndIntent(t *testing.T) {
	for _, stage := range []string{"display_tags", "server_telemetry", "server_latency_probe_settings"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			db, err := Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			server := &model.Server{Name: "before", DisplayTags: []model.ServerDisplayTag{{Text: "before"}}, TrafficLimitBytes: 1000}
			if err := db.CreateServer(ctx, server); err != nil {
				t.Fatal(err)
			}
			before, err := db.GetServer(ctx, server.ID)
			if err != nil {
				t.Fatal(err)
			}
			revision, err := db.ConfigurationRevision(ctx)
			if err != nil {
				t.Fatal(err)
			}
			query := `create trigger fail_server_update before insert on ` + stage + ` begin select raise(abort,'injected failure'); end`
			if stage == "display_tags" {
				query = `create trigger fail_server_update before update of display_tags_json on servers begin select raise(abort,'injected failure'); end`
			}
			if _, err := db.db.ExecContext(ctx, query); err != nil {
				t.Fatal(err)
			}
			changed := *before
			changed.Name = "after"
			changed.DisplayTags = []model.ServerDisplayTag{{Text: "after"}}
			changed.TrafficLimitBytes = 2000
			changed.LatencyProbeEnabled = !before.LatencyProbeEnabled
			if err := db.UpdateServer(ctx, &changed); err == nil {
				t.Fatal("injected failure ignored")
			}
			after, err := db.GetServer(ctx, server.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.Name != before.Name || after.DisplayTags[0].Text != before.DisplayTags[0].Text || after.TrafficLimitBytes != before.TrafficLimitBytes || after.LatencyProbeEnabled != before.LatencyProbeEnabled || !after.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatalf("failed update left partially committed settings: name=%s tags=%v limit=%d probe=%v updated=%s", after.Name, after.DisplayTags, after.TrafficLimitBytes, after.LatencyProbeEnabled, after.UpdatedAt)
			}
			if next, err := db.ConfigurationRevision(ctx); err != nil || next != revision {
				t.Fatalf("failed update advanced desired revision: %d -> %d: %v", revision, next, err)
			}
			if !changed.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatal("failed update exposed an uncommitted timestamp")
			}
			if _, err := db.db.ExecContext(ctx, `drop trigger fail_server_update`); err != nil {
				t.Fatal(err)
			}
			if err := db.UpdateServer(ctx, &changed); err != nil {
				t.Fatal(err)
			}
			after, err = db.GetServer(ctx, server.ID)
			if err != nil || after.Name != changed.Name || after.TrafficLimitBytes != changed.TrafficLimitBytes || after.LatencyProbeEnabled != changed.LatencyProbeEnabled {
				t.Fatalf("retry did not commit settings: %v %v", after, err)
			}
		})
	}
}

func TestCreateServerRollsBackInitializationFailure(t *testing.T) {
	for _, stage := range []string{"server_telemetry", "server_latency_probe_settings", "server_dns_policies", "initial_traffic", "missing_dns_default"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			db, err := Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			query := `create trigger fail_create before insert on ` + stage + ` begin select raise(abort, 'injected failure'); end`
			if stage == "initial_traffic" {
				query = `create trigger fail_create before update of period_key on server_telemetry begin select raise(abort, 'injected failure'); end`
			} else if stage == "missing_dns_default" {
				query = `update dns_lists set enabled=0 where protected=1`
			}
			if _, err := db.db.ExecContext(ctx, query); err != nil {
				t.Fatal(err)
			}
			revision, _ := db.ConfigurationRevision(ctx)
			server := model.Server{Name: "rollback", LatencyProbeEnabled: true}
			window := model.ServerTrafficWindow{Key: "2026-09-01", Start: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
			if err := db.CreateServerWithTraffic(ctx, &server, 1234, window); err == nil {
				t.Fatal("expected initialization failure")
			}
			if server.ID != 0 {
				t.Fatalf("failed create returned id %d", server.ID)
			}
			for _, table := range []string{"servers", "server_telemetry", "server_latency_probe_settings", "server_connectivity_events", "server_dns_policies"} {
				var count int
				if err := db.db.QueryRowContext(ctx, `select count(*) from `+table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("partial creation in %s: count=%d err=%v", table, count, err)
				}
			}
			if after, _ := db.ConfigurationRevision(ctx); after != revision {
				t.Fatalf("failed creation advanced revision from %d to %d", revision, after)
			}
			if stage == "missing_dns_default" {
				_, err = db.db.ExecContext(ctx, `update dns_lists set enabled=1 where protected=1`)
			} else {
				_, err = db.db.ExecContext(ctx, `drop trigger fail_create`)
			}
			if err != nil {
				t.Fatal(err)
			}
			before := db.SQLWriteTransactionCount()
			if err := db.CreateServerWithTraffic(ctx, &server, 1234, window); err != nil {
				t.Fatal(err)
			}
			if count := db.SQLWriteTransactionCount() - before; count != 1 {
				t.Fatalf("creation used %d transactions, want 1", count)
			}
			stored, err := db.GetServer(ctx, server.ID)
			if err != nil || stored.TrafficUploadBytes != 1234 || !stored.LatencyProbeEnabled {
				t.Fatalf("created server=%+v err=%v", stored, err)
			}
		})
	}
}

func TestDeleteServerRollsBackRoutingAndTelemetry(t *testing.T) {
	ctx := context.Background()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, inboundID, _, _ := proxyPathTruncationFixture(t, db)
	inbound, err := db.GetInbound(ctx, inboundID)
	if err != nil {
		t.Fatal(err)
	}
	serverID := inbound.ServerID
	if _, err := db.db.ExecContext(ctx, `create trigger fail_delete before delete on servers begin select raise(abort, 'injected failure'); end`); err != nil {
		t.Fatal(err)
	}
	revision, _ := db.ConfigurationRevision(ctx)
	if err := db.DeleteServer(ctx, serverID); err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("delete error=%v", err)
	}
	steps, err := db.ListProxyPathSteps(ctx)
	if err != nil || len(steps) != 2 {
		t.Fatalf("failed delete truncated routing: steps=%d err=%v", len(steps), err)
	}
	for _, table := range []string{"server_telemetry", "server_latency_probe_settings", "server_dns_policies", "inbounds"} {
		var count int
		if err := db.db.QueryRowContext(ctx, `select count(*) from `+table+` where server_id=?`, serverID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("failed delete changed %s: count=%d err=%v", table, count, err)
		}
	}
	if after, _ := db.ConfigurationRevision(ctx); after != revision {
		t.Fatalf("failed delete advanced revision from %d to %d", revision, after)
	}
	if _, err := db.db.ExecContext(ctx, `drop trigger fail_delete`); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteServer(ctx, serverID); err != nil {
		t.Fatal(err)
	}
	steps, err = db.ListProxyPathSteps(ctx)
	if err != nil || len(steps) != 0 {
		t.Fatalf("deleted server left path steps=%d err=%v", len(steps), err)
	}
	if _, err := db.GetServer(ctx, serverID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("deleted server still exists")
	}
	if err := db.DeleteServer(ctx, serverID); err != nil {
		t.Fatalf("retry deletion: %v", err)
	}
}

func TestServerUpdateExpectedRevisionRejectsStaleFieldsAndEffects(t *testing.T) {
	ctx := context.Background()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	server := &model.Server{Name: "initial"}
	if err := db.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	stale := *server
	first := *server
	first.Name = "first"
	expected := first.UpdatedAt
	if err := db.UpdateServerSettings(ctx, &first, ServerUpdateOptions{ExpectedUpdatedAt: &expected}); err != nil {
		t.Fatal(err)
	}
	revision, err := db.ConfigurationRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	off := false
	stale.Name = "stale"
	if err := db.UpdateServerSettings(ctx, &stale, ServerUpdateOptions{ExpectedUpdatedAt: &expected, RuntimeUsersEnabled: &off}); !errors.Is(err, ErrServerRevisionConflict) {
		t.Fatalf("err=%v", err)
	}
	actual, err := db.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	flags, err := db.ServerDeliveryFlags(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := db.ConfigurationRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Name != "first" || !actual.UpdatedAt.Equal(first.UpdatedAt) || !flags.RuntimeUsersEnabled || after != revision {
		t.Fatal("stale write changed server or effects")
	}
}

func TestDeletedExitRemovesInvalidatedPathAndKeepsPrefixBranch(t *testing.T) {
	ctx := context.Background()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pathID, _, midStepID, exitStepID := proxyPathTruncationFixture(t, db)
	path, err := db.GetProxyPath(ctx, pathID)
	if err != nil {
		t.Fatal(err)
	}
	steps, err := db.ListProxyPathStepsForPath(ctx, pathID)
	if err != nil {
		t.Fatal(err)
	}
	var midServer, exitServer int64
	for _, step := range steps {
		if step.ID == midStepID && step.ServerID != nil {
			midServer = *step.ServerID
		}
		if step.ID == exitStepID && step.ServerID != nil {
			exitServer = *step.ServerID
		}
	}
	if midServer == 0 || exitServer == 0 {
		t.Fatal("missing mid or exit server")
	}
	// A direct branch copied from the surviving prefix hop does not use exitServer
	// and must survive with branch_source_step_id cleared; a branch copied from
	// exitStepID must cascade-delete with the broken hop.
	prefixBranch := &model.ProxyPath{Kind: model.ProxyPathKindDirect, BranchSourceStepID: &midStepID, InboundID: path.InboundID, Secret: "prefix-direct", Enabled: true}
	if err := db.CreateProxyPath(ctx, prefixBranch); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateProxyPathStep(ctx, &model.ProxyPathStep{PathID: prefixBranch.ID, Position: 1, NodeType: model.ProxyPathStepServerInbound, TransportMode: model.ProxyPathTransportSingBox, ServerID: &midServer, ConfigJSON: "{}"}); err != nil {
		t.Fatal(err)
	}
	brokenBranch := &model.ProxyPath{Kind: model.ProxyPathKindDirect, BranchSourceStepID: &exitStepID, InboundID: path.InboundID, Secret: "exit-direct", Enabled: true}
	if err := db.CreateProxyPath(ctx, brokenBranch); err != nil {
		t.Fatal(err)
	}
	// Attach the invalidated path to a plan, user exception, and node metadata.
	user := &model.User{Username: "dep-user", PasswordHash: "unused", Role: model.RoleViewer, Status: "active", ProxyUUID: "44444444-4444-4444-8444-444444444444", ProxyPassword: "unused"}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	plan := &model.SubscriptionPlan{Name: "dep-plan", Enabled: true}
	if err := db.CreateSubscriptionPlan(ctx, plan, []model.SubscriptionPlanNode{
		{NodeType: model.AssignableNodeProxyPath, NodeID: pathID, Enabled: true},
		{NodeType: model.AssignableNodeProxyPath, NodeID: prefixBranch.ID, Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUserNodeException(ctx, &model.UserNodeException{UserID: user.ID, NodeType: model.AssignableNodeProxyPath, NodeID: pathID, Effect: model.UserNodeExceptionAllow, Status: model.UserNodeExceptionActive}); err != nil {
		t.Fatal(err)
	}
	override := "custom-name"
	if _, err := db.UpsertNodeMetadata(ctx, model.AssignableNodeProxyPath, pathID, &override, 0, nil); err != nil {
		t.Fatal(err)
	}
	// A template-owned family_branch referencing exitServer keeps its row and
	// only truncates steps from the broken hop onward.
	tpl := &model.FamilySplitTemplate{Name: "dep-tpl"}
	if err := db.CreateFamilySplitTemplate(ctx, tpl, "v4-secret", "v6-secret"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateProxyPathStep(ctx, &model.ProxyPathStep{PathID: tpl.IPv4PathID, Position: 1, NodeType: model.ProxyPathStepServerInbound, TransportMode: model.ProxyPathTransportSingBox, ServerID: &exitServer, ConfigJSON: "{}"}); err != nil {
		t.Fatal(err)
	}

	if err := db.CleanupRoutingForServer(ctx, exitServer); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetProxyPath(ctx, pathID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("invalidated path %d should be deleted, got err=%v", pathID, err)
	}
	if _, err := db.GetProxyPath(ctx, brokenBranch.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("branch copied from broken hop should be deleted, got err=%v", err)
	}
	kept, err := db.GetProxyPath(ctx, prefixBranch.ID)
	if err != nil || !kept.Enabled || kept.BranchSourceStepID != nil {
		t.Fatalf("unaffected prefix branch should survive detached: %#v err=%v", kept, err)
	}
	if familyPath, err := db.GetProxyPath(ctx, tpl.IPv4PathID); err != nil || familyPath.Kind != model.ProxyPathKindFamilyBranch {
		t.Fatalf("family_branch row must remain after hop removal: %#v err=%v", familyPath, err)
	}
	if familySteps, err := db.ListProxyPathStepsForPath(ctx, tpl.IPv4PathID); err != nil || len(familySteps) != 0 {
		t.Fatalf("family_branch broken step was not truncated: %#v err=%v", familySteps, err)
	}
	refs, err := db.PlanNodeReferences(ctx, model.AssignableNodeProxyPath, pathID)
	if err != nil || len(refs.Active) != 0 || len(refs.Draft) != 0 || refs.Exceptions != 0 {
		t.Fatalf("deleted path retained plan/exception references: %#v err=%v", refs, err)
	}
	if meta, err := db.GetNodeMetadata(ctx, model.AssignableNodeProxyPath, pathID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted path retained node metadata: %#v err=%v", meta, err)
	}
	var audits int
	if err := db.db.QueryRowContext(ctx, `select count(*) from audit_logs where action='dependency_invalidated' and target='proxy_path'`).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("diagnostic audits=%d err=%v, want 2 (ordinary path + family branch)", audits, err)
	}
}

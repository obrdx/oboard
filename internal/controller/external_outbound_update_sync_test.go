package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/OboardProject/oboard/internal/automation"
	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
)

func TestExternalOutboundUpdateSynchronizesExistingPaths(t *testing.T) {
	for _, surface := range []string{"web", "mcp"} {
		t.Run(surface, func(t *testing.T) {
			f := newExternalEgressControllerFixture(t)
			ctx := context.Background()
			second := &model.ProxyPath{Kind: model.ProxyPathKindChain, NameMode: model.ProxyPathNameAuto, InboundID: f.inbound.ID, Secret: "second-secret", Enabled: true}
			if err := f.db.CreateProxyPath(ctx, second); err != nil {
				t.Fatal(err)
			}
			step := &model.ProxyPathStep{PathID: second.ID, Position: 1, NodeType: model.ProxyPathStepImported, TransportMode: model.ProxyPathTransportSingBox, ExternalOutboundID: &f.external.ID, ConfigJSON: "{}"}
			if err := f.db.CreateProxyPathStep(ctx, step); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.DrainConfigurationSyncIntents(ctx); err != nil {
				t.Fatal(err)
			}
			before, err := f.db.ConfigurationRevision(ctx)
			if err != nil {
				t.Fatal(err)
			}
			changes := map[string]any{"name": "updated-proxy", "protocol": "http", "target_address": "9.9.9.9", "target_port": 8080, "config_json": `{"username":"updated-user","password":"updated-password","tls":{"enabled":true}}`}
			if surface == "web" {
				handler := f.srv.Handler()
				request(t, handler, http.MethodPost, "/api/v1/ui/auth/bootstrap", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusCreated)
				token := request(t, handler, http.MethodPost, "/api/v1/ui/auth/login", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusOK)["token"].(string)
				request(t, handler, http.MethodPatch, fmt.Sprintf("/api/v1/ui/external-outbounds/%d", f.external.ID), token, changes, http.StatusOK)
			} else {
				operator := &model.User{Username: "operator", PasswordHash: "unused", Role: model.RoleOperator, Status: "active", ProxyUUID: "11111111-1111-4111-8111-111111111111", ProxyPassword: "unused"}
				if err := f.db.CreateUser(ctx, operator); err != nil {
					t.Fatal(err)
				}
				principal := trafficAutomationPrincipal(t, f.srv, "operator")
				input, _ := json.Marshal(map[string]any{"external_outbound_id": f.external.ID, "changes": changes})
				applyAutomationChangeset(t, f.srv, principal, "external-update", automation.OperationRequest{Capability: "external_outbounds.update", Input: input})
			}
			revision, err := f.db.ConfigurationRevision(ctx)
			if err != nil || revision <= before {
				t.Fatalf("revision=%d before=%d err=%v", revision, before, err)
			}
			if _, err := f.db.DrainConfigurationSyncIntents(ctx); err != nil {
				t.Fatal(err)
			}
			state, err := f.db.ConfigurationSyncState(ctx, f.server.ID)
			if err != nil || state.WantedRevision != revision {
				t.Fatalf("update did not schedule sync: %#v err=%v", state, err)
			}
			f.srv.reconcileConfiguration(ctx)
			state, err = f.db.ConfigurationSyncState(ctx, f.server.ID)
			if err != nil || state.State != "queued" || state.LastTaskID == 0 {
				t.Fatalf("sync=%#v err=%v", state, err)
			}
			task, err := f.db.GetTask(ctx, state.LastTaskID)
			if err != nil {
				t.Fatal(err)
			}
			var payload model.DeploymentTaskPayload
			if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
				t.Fatal(err)
			}
			var config core.SingBoxConfig
			if err := json.Unmarshal([]byte(payload.Config.Config), &config); err != nil {
				t.Fatal(err)
			}
			for _, pathID := range []int64{f.path.ID, second.ID} {
				tag := fmt.Sprintf("path-%d-step-1", pathID)
				found := false
				for _, outbound := range config.Outbounds {
					if outbound["tag"] != tag {
						continue
					}
					found = true
					if outbound["type"] != "http" || outbound["server"] != "9.9.9.9" || outbound["server_port"] != float64(8080) || outbound["password"] != "updated-password" {
						t.Fatalf("path %d did not receive updated config", pathID)
					}
				}
				if !found {
					t.Fatalf("existing path %d missing from deployment", pathID)
				}
			}
			steps, err := f.db.ListProxyPathSteps(ctx)
			if err != nil || len(steps) != 2 {
				t.Fatalf("steps=%d err=%v", len(steps), err)
			}
			for _, current := range steps {
				if current.ExternalOutboundID == nil || *current.ExternalOutboundID != f.external.ID {
					t.Fatal("shared node reference was replaced")
				}
			}
		})
	}
}

func TestStandaloneExternalCheckPersistsRegionWithoutAnyPath(t *testing.T) {
	for _, surface := range []string{"web", "mcp"} {
		t.Run(surface, func(t *testing.T) {
			f := newExternalEgressControllerFixture(t)
			ctx := context.Background()
			if err := f.db.Delete(ctx, "proxy_paths", f.path.ID); err != nil {
				t.Fatal(err)
			}
			f.srv.geoIP = &fakeConnectionAuditGeoResolver{geo: model.IPGeography{CountryCode: "JP", Revision: "test-geo"}}
			f.srv.externalCheckRunner = func(context.Context, json.RawMessage) (string, error) { return "8.8.8.8", nil }
			if surface == "web" {
				handler := f.srv.Handler()
				request(t, handler, http.MethodPost, "/api/v1/ui/auth/bootstrap", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusCreated)
				token := request(t, handler, http.MethodPost, "/api/v1/ui/auth/login", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusOK)["token"].(string)
				request(t, handler, http.MethodPost, fmt.Sprintf("/api/v1/ui/external-outbounds/%d/check", f.external.ID), token, map[string]any{"server_id": f.server.ID}, http.StatusBadRequest)
				response := request(t, handler, http.MethodPost, fmt.Sprintf("/api/v1/ui/external-outbounds/%d/check", f.external.ID), token, map[string]any{}, http.StatusOK)
				check := response["check"].(map[string]any)
				if check["status"] != "reachable" || check["exit_ip"] != "8.8.8.8" || check["region_code"] != "JP" {
					t.Fatalf("check=%#v", check)
				}
				assertCapabilityOutputSchema(t, f.srv, "external_outbounds.check", map[string]any{"check": check})
			} else {
				operator := &model.User{Username: "operator", PasswordHash: "unused", Role: model.RoleOperator, Status: "active", ProxyUUID: "11111111-1111-4111-8111-111111111111", ProxyPassword: "unused"}
				if err := f.db.CreateUser(ctx, operator); err != nil {
					t.Fatal(err)
				}
				principal := trafficAutomationPrincipal(t, f.srv, "operator")
				input, _ := json.Marshal(map[string]any{"external_outbound_id": f.external.ID})
				applyAutomationChangeset(t, f.srv, principal, "external-check", automation.OperationRequest{Capability: "external_outbounds.check", Input: input})
			}
			tasks, err := f.db.ListTasksByServer(ctx, f.server.ID, 20)
			if err != nil || len(tasks) != 0 {
				t.Fatalf("controller probe queued Agent tasks: %v %v", tasks, err)
			}
			saved, err := f.db.GetExternalOutbound(ctx, f.external.ID)
			if err != nil || saved.ProbeRegionCode != "JP" || saved.ProbeExitIP != "8.8.8.8" {
				t.Fatalf("probe not persisted: %#v %v", saved, err)
			}
			resolved := f.srv.resolvedExternalOutbound(ctx, *saved)
			if resolved.EffectiveRegionCode != "JP" {
				t.Fatal("unlinked node lost detected region")
			}
		})
	}
}

func TestControllerExternalCheckDoesNotRequireOnlineAgent(t *testing.T) {
	f := newExternalEgressControllerFixture(t)
	f.srv.externalCheckRunner = func(context.Context, json.RawMessage) (string, error) { return "8.8.8.8", nil }
	f.server.Status = "offline"
	if err := f.db.UpdateServer(context.Background(), f.server); err != nil {
		t.Fatal(err)
	}
	result, status, err := f.srv.checkExternalOutbound(context.Background(), *f.external)
	if err != nil || status != 200 || result.Status != "reachable" {
		t.Fatalf("%#v %d %v", result, status, err)
	}
}

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

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
			f.server.KernelCapabilities = []string{"external_outbound_check_v1", externalOutboundProbeCapability}
			if err := f.db.UpdateServer(ctx, f.server); err != nil {
				t.Fatal(err)
			}
			f.srv.geoIP = &fakeConnectionAuditGeoResolver{geo: model.IPGeography{CountryCode: "JP", Revision: "test-geo"}}
			workerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for {
					tasks, err := f.db.ListTasksByServer(workerCtx, f.server.ID, 20)
					if err != nil {
						done <- err
						return
					}
					for _, task := range tasks {
						if task.Type != model.AgentTaskTypeProbeExternalEgress || task.Status != "pending" {
							continue
						}
						var plan model.ExternalEgressProbePlan
						if err := json.Unmarshal([]byte(task.PayloadJSON), &plan); err != nil {
							done <- err
							return
						}
						if plan.Standalone == nil || len(plan.Targets) != 0 || plan.ExpectedConfigVersion != 0 {
							done <- fmt.Errorf("not an independent probe")
							return
						}
						raw, _ := json.Marshal(model.ExternalEgressProbeResult{Items: []model.ExternalEgressProbeItem{{ProbeID: plan.Standalone.Fingerprint, Status: "succeeded", ExitIP: "8.8.8.8"}}})
						if err := f.srv.applyExternalEgressTaskResults(workerCtx, f.server.ID, task, "succeeded", string(raw)); err != nil {
							done <- err
							return
						}
						done <- f.db.CompleteTask(workerCtx, task.ID, "succeeded", string(raw))
						return
					}
					select {
					case <-workerCtx.Done():
						done <- workerCtx.Err()
						return
					case <-ticker.C:
					}
				}
			}()
			if surface == "web" {
				handler := f.srv.Handler()
				request(t, handler, http.MethodPost, "/api/v1/ui/auth/bootstrap", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusCreated)
				token := request(t, handler, http.MethodPost, "/api/v1/ui/auth/login", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusOK)["token"].(string)
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
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
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

func TestStandaloneExternalCheckRejectsMissingCapabilityAndUnauthorizedServer(t *testing.T) {
	f := newExternalEgressControllerFixture(t)
	if _, _, err := f.srv.checkExternalOutbound(context.Background(), *f.external, nil, nil); err == nil {
		t.Fatal("old Agent/kernel accepted")
	}
	f.server.KernelCapabilities = []string{"external_outbound_check_v1", externalOutboundProbeCapability}
	if err := f.db.UpdateServer(context.Background(), f.server); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.srv.checkExternalOutbound(context.Background(), *f.external, nil, func(int64) bool { return false }); err == nil {
		t.Fatal("resource boundary ignored")
	}
	tasks, err := f.db.ListTasksByServer(context.Background(), f.server.ID, 20)
	if err != nil || len(tasks) != 0 {
		t.Fatal("unauthorized check queued a task")
	}
}

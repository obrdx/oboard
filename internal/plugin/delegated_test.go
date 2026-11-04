package plugin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/application"
	"github.com/OboardProject/oboard/internal/mcpauth"
	"github.com/OboardProject/oboard/internal/model"
)

func delegatedAdminPrincipal() application.Principal {
	id := int64(1)
	return application.Principal{ID: "oauth_admin", GrantID: "grt_1", UserID: &id, Type: model.APIPrincipalOAuth, Role: model.RoleAdmin, AccessLevel: mcpauth.AccessOperate, ClientName: "hermes"}
}

func serviceAccountAdminPrincipal() application.Principal {
	return application.Principal{ID: "sa_1", Type: model.APIPrincipalServiceAccount, Role: model.RoleAdmin, Scopes: []string{"*"}}
}

func draftCheck(input DraftInput) (PackageCandidate, error) {
	manifest, err := ParseManifest(input.Manifest)
	return PackageCandidate{Manifest: manifest, Source: input.Source, SHA256: DraftVersion(input.Manifest, input.Source)}, err
}

func TestDelegatedAdminGrantsWithinManifestAndIsAudited(t *testing.T) {
	h := newHarness(t)
	h.host.servers[2] = lanReadyServer()
	result := h.installManifest(lanManifest, Grant{Capabilities: map[string]CapabilityGrant{CapLANRequest: {Servers: []int64{2}, Targets: []string{"router_status"}}}})

	flat := GrantInput{Capabilities: json.RawMessage(`["lan.request"]`), Servers: []int64{2}, LANTargets: []string{"router_status", "router_change"}}
	grant, err := flat.Grant()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.SetGrant(h.ctx, serviceAccountAdminPrincipal(), result.InstanceID, AnyGrantRevision, grant); CodeOf(err) != CodePermissionDenied {
		t.Fatalf("service accounts must not grant plugins, got %v", err)
	}
	if _, err := h.service.SetGrant(h.ctx, delegatedAdminPrincipal(), result.InstanceID, AnyGrantRevision, grant); err != nil {
		t.Fatalf("the administrator's OAuth grant must be able to grant: %v", err)
	}
	events, _ := h.store.ListPluginAuditEvents(h.ctx, result.InstanceID, 0, 10)
	if len(events) == 0 || events[0].Capability != AuditManageGrant {
		t.Fatalf("grant change not audited: %+v", events)
	}
	var detail struct {
		Added   []string       `json:"added"`
		Removed []string       `json:"removed"`
		Actor   map[string]any `json:"actor"`
	}
	if err := json.Unmarshal(events[0].DetailJSON, &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Added) != 1 || detail.Added[0] != "lan.request:lan_target:router_change" || len(detail.Removed) != 0 || detail.Actor["oauth_grant_id"] != "grt_1" {
		t.Fatalf("audit must name the actor and the permission diff: %+v", detail)
	}

	beyond, err := GrantInput{Capabilities: json.RawMessage(`["lan.request"]`), Servers: []int64{2}, LANTargets: []string{"undeclared"}}.Grant()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.SetGrant(h.ctx, delegatedAdminPrincipal(), result.InstanceID, AnyGrantRevision, beyond); CodeOf(err) != CodeInvalidArgument {
		t.Fatalf("an undeclared target must be refused, got %v", err)
	}
	undeclared, _ := GrantInput{Capabilities: json.RawMessage(`["http.request"]`)}.Grant()
	if _, err := h.service.SetGrant(h.ctx, delegatedAdminPrincipal(), result.InstanceID, AnyGrantRevision, undeclared); CodeOf(err) != CodeInvalidArgument {
		t.Fatalf("an undeclared capability must be refused, got %v", err)
	}

	disabled := false
	if _, err := h.service.UpdateInstance(h.ctx, delegatedAdminPrincipal(), result.InstanceID, InstanceUpdate{Enabled: &disabled}); err != nil {
		t.Fatalf("delegated disable failed: %v", err)
	}
	if err := h.service.RevokeGrant(h.ctx, delegatedAdminPrincipal(), result.InstanceID); err != nil {
		t.Fatalf("delegated revoke failed: %v", err)
	}
	events, _ = h.store.ListPluginAuditEvents(h.ctx, result.InstanceID, 0, 10)
	if events[0].Capability != AuditManageRevoke || events[1].Capability != AuditManageDisable {
		t.Fatalf("revoke and disable must be audited: %s %s", events[0].Capability, events[1].Capability)
	}
}

func TestDelegatedPublishBindsDraftVersion(t *testing.T) {
	h := newHarness(t)
	result := h.installManifest(lanManifest, Grant{Capabilities: map[string]CapabilityGrant{}})
	draft := strings.Replace(lanManifest, `"version": "1.0.0"`, `"version": "1.1.0"`, 1)
	source := "function main(run) { return 'v2' }"
	if err := h.service.SaveDraft(h.ctx, adminPrincipal(), result.InstallationID, DraftInput{Manifest: json.RawMessage(draft), Source: source}); err != nil {
		t.Fatal(err)
	}
	installation, _ := h.store.GetPluginInstallation(h.ctx, result.InstallationID)
	version := DraftVersion(installation.DraftManifestJSON, installation.DraftSource)
	if _, err := h.service.Publish(h.ctx, serviceAccountAdminPrincipal(), result.InstallationID, version, draftCheck); CodeOf(err) != CodePermissionDenied {
		t.Fatalf("service accounts must not publish, got %v", err)
	}
	if _, err := h.service.Publish(h.ctx, delegatedAdminPrincipal(), result.InstallationID, strings.Repeat("0", 64), draftCheck); CodeOf(err) != CodeConflict {
		t.Fatalf("a stale draft_version must be refused, got %v", err)
	}
	published, err := h.service.Publish(h.ctx, delegatedAdminPrincipal(), result.InstallationID, version, draftCheck)
	if err != nil || published.PackageID == 0 {
		t.Fatalf("delegated publish failed: %+v %v", published, err)
	}
	events, _ := h.store.ListPluginAuditEvents(h.ctx, result.InstanceID, 0, 10)
	if len(events) == 0 || events[0].Capability != AuditManagePublish || !strings.Contains(string(events[0].DetailJSON), version) {
		t.Fatalf("publish not audited with its draft version: %+v", events)
	}
}

func TestDraftTrialRunBlocksSideEffectTargets(t *testing.T) {
	h := newHarness(t)
	h.host.servers[2] = lanReadyServer()
	result := h.installManifest(lanManifest, Grant{Capabilities: map[string]CapabilityGrant{CapLANRequest: {Servers: []int64{2}, Targets: []string{"router_change", "router_status"}}}})
	draft := strings.Replace(lanManifest, `"version": "1.0.0"`, `"version": "1.1.0"`, 1)
	if err := h.service.SaveDraft(h.ctx, adminPrincipal(), result.InstallationID, DraftInput{Manifest: json.RawMessage(draft), Source: "function main(run) {}"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.RunDraft(h.ctx, delegatedAdminPrincipal(), result.InstallationID, result.InstanceID, "t-1", draftCheck); err != nil {
		t.Fatalf("delegated test run failed: %v", err)
	}
	lease, err := h.service.Lease(h.ctx, "worker-1")
	if err != nil || lease == nil {
		t.Fatalf("no lease: %v", err)
	}
	expectCode(t, h.call(lease, "lan.request", map[string]any{"server_id": "2", "target": "router_change", "method": "POST"}), CodeDraftSideEffectBlocked)
	if response := h.call(lease, "lan.request", map[string]any{"server_id": "2", "target": "router_status"}); !response.OK {
		t.Fatalf("read-only targets must still work in a test run: %+v", response)
	}
	if len(h.host.diagnostics) != 1 {
		t.Fatalf("a blocked side-effect call must never reach the Agent: %d", len(h.host.diagnostics))
	}
}

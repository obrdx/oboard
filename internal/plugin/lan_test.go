package plugin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/pluginrpc"
	"github.com/OboardProject/oboard/internal/store"
)

const lanManifest = `{
  "id": "acme.gl-switch",
  "name": "GL switch",
  "version": "1.0.0",
  "description": "",
  "runtime": "oboard-js",
  "entry": "main.js",
  "capabilities": ["lan.request"],
  "lan": {"targets": [
    {"id": "router_status", "scheme": "http", "address": "10.10.18.1", "port": 5000, "methods": ["GET"], "path_prefix": "/", "reason": "读取当前出口"},
    {"id": "router_change", "scheme": "http", "address": "10.10.18.1", "port": 5000, "methods": ["POST"], "path_prefix": "/change", "side_effect": true, "reason": "切换出口"}
  ]},
  "triggers": {"schedule": true}
}`

func TestLANManifestAcceptsOnlyExactPrivateTargets(t *testing.T) {
	manifest := mustManifest(t, lanManifest)
	if targets := manifest.SideEffectTargets(); len(targets) != 1 || targets[0] != "router_change" {
		t.Fatalf("side effect targets: %v", targets)
	}
	bad := map[string]string{
		"public":         `"address": "1.1.1.1"`,
		"loopback":       `"address": "127.0.0.1"`,
		"metadata":       `"address": "169.254.169.254"`,
		"hostname":       `"address": "router.lan"`,
		"cidr":           `"address": "10.10.18.0/24"`,
		"no side effect": `"side_effect": false`,
		"query prefix":   `"path_prefix": "/change?x=1"`,
		"dot prefix":     `"path_prefix": "/change/.."`,
		"insecure http":  `"insecure_tls": true, "side_effect": true`,
	}
	for name, replacement := range bad {
		var mutated string
		switch {
		case strings.HasPrefix(replacement, `"address"`):
			mutated = strings.Replace(lanManifest, `"address": "10.10.18.1", "port": 5000, "methods": ["POST"]`, replacement+`, "port": 5000, "methods": ["POST"]`, 1)
		case strings.HasPrefix(replacement, `"path_prefix"`):
			mutated = strings.Replace(lanManifest, `"path_prefix": "/change"`, replacement, 1)
		default:
			mutated = strings.Replace(lanManifest, `"side_effect": true`, replacement, 1)
		}
		if mutated == lanManifest {
			t.Fatalf("%s: fixture replacement did not apply", name)
		}
		if _, err := ParseManifest([]byte(mutated)); err == nil {
			t.Fatalf("%s: manifest must be rejected", name)
		}
	}
	if _, err := ParseManifest([]byte(strings.Replace(lanManifest, `"capabilities": ["lan.request"]`, `"capabilities": []`, 1))); err == nil {
		t.Fatal("lan without lan.request must be rejected")
	}
}

func TestLANGrantDiffAndCarryOver(t *testing.T) {
	manifest := mustManifest(t, lanManifest)
	grant := Grant{Capabilities: map[string]CapabilityGrant{CapLANRequest: {Servers: []int64{2}, Targets: []string{"router_status", "nope"}}}}
	if err := ValidateGrant(manifest, &grant, nil, nil, nil, nil); err == nil {
		t.Fatal("undeclared target must not be grantable")
	}
	grant.Capabilities[CapLANRequest] = CapabilityGrant{Servers: []int64{2}}
	if err := ValidateGrant(manifest, &grant, nil, nil, nil, nil); err == nil {
		t.Fatal("lan.request needs at least one target")
	}
	moved := mustManifest(t, strings.Replace(lanManifest, `"address": "10.10.18.1", "port": 5000, "methods": ["POST"]`, `"address": "10.10.18.2", "port": 5000, "methods": ["POST"]`, 1))
	diff := DiffPermissions(&manifest, moved)
	if !diff.Expanded || len(diff.AddedLANTargets) != 1 || diff.AddedLANTargets[0] != "router_change" {
		t.Fatalf("moving a target to another address must require review: %+v", diff)
	}
	if same := DiffPermissions(&manifest, manifest); same.Expanded {
		t.Fatalf("an identical manifest must not expand: %+v", same)
	}
}

func (h *harness) installManifest(manifestJSON string, grant Grant) InstallResult {
	h.t.Helper()
	candidate := h.candidate(manifestJSON, model.PluginPublisherLocal)
	result, err := h.service.InstallPackage(h.ctx, adminPrincipal(), candidate, candidate.SHA256)
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := h.service.SetGrant(h.ctx, adminPrincipal(), result.InstanceID, 0, grant); err != nil {
		h.t.Fatal(err)
	}
	if err := h.service.SetInstallationEnabled(h.ctx, adminPrincipal(), result.InstallationID, true); err != nil {
		h.t.Fatal(err)
	}
	enabled := true
	if detail, err := h.service.UpdateInstance(h.ctx, adminPrincipal(), result.InstanceID, InstanceUpdate{Enabled: &enabled}); err != nil || detail.Status != StatusReady {
		h.t.Fatalf("instance not ready: %+v %v", detail, err)
	}
	return result
}

func lanReadyServer() ServerInfo {
	return ServerInfo{ID: 2, Name: "Tokyo-01", Online: true, Enrolled: true, PluginsGate: true, LANGate: true, Capabilities: []string{model.AgentCapabilityLANRequest}}
}

func TestLANRequestGatewayEnforcesTargetGrantAndLocalGate(t *testing.T) {
	h := newHarness(t)
	h.host.servers[2] = lanReadyServer()
	result := h.installManifest(lanManifest, Grant{Capabilities: map[string]CapabilityGrant{CapLANRequest: {Servers: []int64{2}, Targets: []string{"router_status"}}}})
	lease := h.startRun(result.InstanceID)
	call := func(args map[string]any) pluginrpc.CallResponse { return h.call(lease, "lan.request", args) }
	expectCode(t, call(map[string]any{"server_id": "2", "target": "router_change", "method": "POST", "path": "/change"}), CodeTargetNotAllowed)
	expectCode(t, call(map[string]any{"server_id": "2", "target": "router_status", "method": "POST"}), CodeTargetNotAllowed)
	expectCode(t, call(map[string]any{"server_id": "2", "target": "router_status", "address": "10.0.0.1"}), CodeInvalidArgument)
	expectCode(t, call(map[string]any{"server_id": "2", "target": "router_status", "headers": map[string]string{"Host": "evil"}}), CodeInvalidArgument)
	expectCode(t, call(map[string]any{"server_id": "3", "target": "router_status"}), CodeResourceDenied)
	gateClosed := lanReadyServer()
	gateClosed.LANGate = false
	h.host.servers[2] = gateClosed
	refused := call(map[string]any{"server_id": "2", "target": "router_status"})
	expectCode(t, refused, CodeAgentPolicyDenied)
	if !strings.Contains(refused.Message, "obag remote-access allow lan") {
		t.Fatalf("refusal must name the local rule: %s", refused.Message)
	}
	if len(h.host.diagnostics) != 0 {
		t.Fatal("refused LAN requests must never reach the Agent")
	}
	h.host.servers[2] = lanReadyServer()
	response := call(map[string]any{"server_id": "2", "target": "router_status", "path": "/?full=1"})
	if !response.OK || !strings.Contains(string(response.Result), `"outbound":"b"`) {
		t.Fatalf("LAN request failed: %+v", response)
	}
	var payload model.LANRequestTaskPayload
	if err := json.Unmarshal(h.host.diagnostics[0].Payload, &payload); err != nil || payload.Address != "10.10.18.1" || payload.Port != 5000 || payload.Path != "/?full=1" || payload.MaxResponseBytes != model.LANRequestDefaultResponseBytes || payload.SideEffect {
		t.Fatalf("unexpected payload: %+v %v", payload, err)
	}
	events, _ := h.store.ListPluginAuditEvents(h.ctx, result.InstanceID, 0, 50)
	if len(events) == 0 || events[0].Capability != CapLANRequest || events[0].Result != "succeeded" || !strings.Contains(string(events[0].DetailJSON), `"endpoint":"http://10.10.18.1:5000/"`) {
		t.Fatalf("LAN request not audited with its endpoint: %+v", events)
	}
	if events[1].Result != "denied" {
		t.Fatalf("policy refusals must be audited as denied: %+v", events[1])
	}
}

func TestDraftTrialRunUsesDraftSourceAndNarrowedGrant(t *testing.T) {
	h := newHarness(t)
	h.host.servers[2] = lanReadyServer()
	result := h.installManifest(lanManifest, Grant{Capabilities: map[string]CapabilityGrant{CapLANRequest: {Servers: []int64{2}, Targets: []string{"router_change", "router_status"}}}})
	draftManifest := strings.Replace(strings.Replace(lanManifest, `"version": "1.0.0"`, `"version": "1.1.0"`, 1), `"address": "10.10.18.1", "port": 5000, "methods": ["POST"]`, `"address": "10.10.18.9", "port": 5000, "methods": ["POST"]`, 1)
	if err := h.service.SaveDraft(h.ctx, adminPrincipal(), result.InstallationID, DraftInput{Manifest: json.RawMessage(draftManifest), Source: "function main(run) { return 'draft' }"}); err != nil {
		t.Fatal(err)
	}
	check := func(input DraftInput) (PackageCandidate, error) {
		manifest, err := ParseManifest(input.Manifest)
		return PackageCandidate{Manifest: manifest, Source: input.Source}, err
	}
	if _, err := h.service.RunDraft(h.ctx, operatorPrincipal(), result.InstallationID, result.InstanceID, "d-1", check); CodeOf(err) != CodePermissionDenied {
		t.Fatalf("draft runs must be interactive-admin only, got %v", err)
	}
	run, err := h.service.RunDraft(h.ctx, adminPrincipal(), result.InstallationID, result.InstanceID, "d-1", check)
	if err != nil || !run.IsDraft || run.PluginVersion != "1.1.0" {
		t.Fatalf("draft run not queued: %+v %v", run, err)
	}
	lease, err := h.service.Lease(h.ctx, "worker-1")
	if err != nil || lease == nil || lease.Source != "function main(run) { return 'draft' }" {
		t.Fatalf("lease must carry the draft source: %+v %v", lease, err)
	}
	expectCode(t, h.call(lease, "lan.request", map[string]any{"server_id": "2", "target": "router_change", "method": "POST"}), CodeTargetNotAllowed)
	if response := h.call(lease, "lan.request", map[string]any{"server_id": "2", "target": "router_status"}); !response.OK {
		t.Fatalf("unchanged granted target must work in a draft run: %+v", response)
	}
	if err := h.service.Complete(h.ctx, pluginrpc.CompleteRequest{WorkerID: "worker-1", RunUUID: lease.RunUUID, LeaseGeneration: lease.LeaseGeneration, Status: model.PluginRunFailed, ErrorCode: "SCRIPT_ERROR"}); err != nil {
		t.Fatal(err)
	}
	instance, _ := h.store.GetPluginInstance(h.ctx, result.InstanceID)
	if instance.FailureStreak != 0 {
		t.Fatalf("draft failures must not count toward the instance streak: %+v", instance)
	}
	runs, _ := h.service.ListRuns(h.ctx, adminPrincipal(), store.PluginRunFilter{InstanceID: result.InstanceID, Limit: 10})
	if len(runs) != 1 || !runs[0].IsDraft || runs[0].DraftSource != "" {
		t.Fatalf("listed run must be marked draft without its source: %+v", runs)
	}
}

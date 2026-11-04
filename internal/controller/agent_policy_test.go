package controller

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/application"
	"github.com/OboardProject/oboard/internal/mcpauth"
	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/security"
	"github.com/OboardProject/oboard/internal/store"
)

func TestAgentPolicySetDescriptorsAreDelegatedAdminCapabilities(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "agent-policy.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := newTestServer(db, "test-secret", "")
	for _, name := range []string{"servers.agent_policy.set", "plugins.publish", "plugins.drafts.test_run", "plugin_instances.enable", "plugin_instances.disable", "plugin_instances.grant", "plugin_instances.grant.revoke",
		"plugins.packages.preview", "plugins.packages.install", "plugins.source.check", "plugins.source.update", "plugins.versions.activate", "plugins.uninstall", "plugin_instances.secrets.update"} {
		descriptor, ok := srv.capabilities.Get(name)
		if !ok || !descriptor.MCPEnabled || !descriptor.Executable || !descriptor.AdminOnly || descriptor.ApprovalPolicy != "" || descriptor.Privileged() {
			t.Fatalf("%s must be an immediately applied, MCP-enabled admin capability: %+v", name, descriptor)
		}
		admin := application.Principal{ID: "oauth_admin", Type: model.APIPrincipalOAuth, Role: model.RoleAdmin, AccessLevel: mcpauth.AccessOperate}
		if _, ok := srv.capabilities.Authorize(admin, name); !ok {
			t.Fatalf("an administrator's OAuth grant must reach %s", name)
		}
		operator := application.Principal{ID: "oauth_op", Type: model.APIPrincipalOAuth, Role: model.RoleOperator, AccessLevel: mcpauth.AccessOperate}
		if _, ok := srv.capabilities.Authorize(operator, name); ok {
			t.Fatalf("an operator must not reach %s", name)
		}
	}
	for _, name := range []string{"plugins.set_enabled", "plugin_instances.delete", "plugins.runtime.settings.update"} {
		if descriptor, ok := srv.capabilities.Get(name); !ok || descriptor.MCPEnabled {
			t.Fatalf("%s must stay interactive-admin only", name)
		}
	}
	if descriptor, _ := srv.capabilities.Get("plugin_instances.secrets.update"); len(descriptor.SealedInput) != 1 || descriptor.SealedInput[0] != "value" {
		t.Fatalf("the plugin secret value must be sealed before a changeset is stored: %+v", descriptor.SealedInput)
	}
}

func TestAgentPolicySetHonoursHardenedAndCapability(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "agent-policy-check.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := newTestServer(db, "test-secret", "")
	ctx := context.Background()
	server := &model.Server{Name: "Tokyo", AgentID: "a1", AgentTokenHash: security.HashSecret("tok"), ChainSecret: "chain", ListenIP: "0.0.0.0", Status: model.ServerOnline}
	if err := db.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	admin := application.Principal{ID: "oauth_admin", Type: model.APIPrincipalOAuth, Role: model.RoleAdmin, AccessLevel: mcpauth.AccessOperate}
	allow, deny := true, false
	request := agentPolicySetOperation{ServerID: server.ID, Feature: model.LocalPolicyFeatureLAN, Allow: &allow}
	if _, err := srv.checkAgentPolicySet(ctx, admin, request); err == nil || !strings.Contains(err.Error(), "agent_upgrade_required") {
		t.Fatalf("an Agent without the capability must be refused: %v", err)
	}
	server.KernelCapabilities = []string{model.AgentCapabilityLocalPolicyRemote}
	if err := db.UpdateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertServerRemoteAccessStatus(ctx, server.ID, model.RemoteAccessReport{LocalMode: model.RemoteAccessModeHardened}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.checkAgentPolicySet(ctx, admin, request); err == nil || !strings.Contains(err.Error(), model.LocalPolicyErrorHardened) {
		t.Fatalf("opening a gate on a Hardened node must be refused: %v", err)
	}
	request.Allow = &deny
	if _, err := srv.checkAgentPolicySet(ctx, admin, request); err == nil || !strings.Contains(err.Error(), "server_offline") {
		t.Fatalf("closing a Hardened gate must pass the policy check: %v", err)
	}
	operator := admin
	operator.Role = model.RoleOperator
	if _, err := srv.checkAgentPolicySet(ctx, operator, request); err == nil {
		t.Fatal("operators must not change node gates")
	}

	result, _ := json.Marshal(model.AgentLocalPolicyResult{Feature: "lan", Allow: true, Changed: true, LocalMode: model.RemoteAccessModeStandard, LocalAllow: model.RemoteAccessLocalAllow{PluginsEnabled: true, LANRequests: true}})
	stored, _ := db.GetServer(ctx, server.ID)
	srv.applyAgentLocalPolicyTaskResult(ctx, stored, string(result))
	status, err := db.GetServerRemoteAccessStatus(ctx, server.ID)
	if err != nil || !status.LocalAllow.LANRequests || status.LocalMode != model.RemoteAccessModeStandard {
		t.Fatalf("the enforced gates from the task result must be stored: %+v %v", status, err)
	}
}

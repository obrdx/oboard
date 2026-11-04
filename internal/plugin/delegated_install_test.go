package plugin

import (
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
)

func TestDelegatedAdminInstallsActivatesAndUninstallsWithAudit(t *testing.T) {
	h := newHarness(t)
	first := h.candidate(gatewayManifest, model.PluginPublisherLocal)
	if _, err := h.service.InstallPackage(h.ctx, serviceAccountAdminPrincipal(), first, first.SHA256); CodeOf(err) != CodePermissionDenied {
		t.Fatalf("service accounts must not install plugins, got %v", err)
	}
	if _, err := h.service.PreviewPackage(h.ctx, serviceAccountAdminPrincipal(), first); CodeOf(err) != CodePermissionDenied {
		t.Fatalf("service accounts must not preview packages, got %v", err)
	}
	if !h.service.CanDelegate(delegatedAdminPrincipal(), PermInstall) || h.service.CanDelegate(serviceAccountAdminPrincipal(), PermInstall) {
		t.Fatal("CanDelegate must follow requireDelegable")
	}
	installed, err := h.service.InstallPackage(h.ctx, delegatedAdminPrincipal(), first, first.SHA256)
	if err != nil || installed.Action != "install" {
		t.Fatalf("delegated install failed: %+v %v", installed, err)
	}
	events, _ := h.store.ListPluginAuditEvents(h.ctx, installed.InstanceID, 0, 10)
	if len(events) == 0 || events[0].Capability != AuditManageInstall || !strings.Contains(string(events[0].DetailJSON), "grt_1") {
		t.Fatalf("install must be audited with the OAuth grant: %+v", events)
	}

	second := h.candidate(strings.Replace(gatewayManifest, `"version": "1.0.0"`, `"version": "1.1.0"`, 1), model.PluginPublisherLocal)
	updated, err := h.service.InstallPackage(h.ctx, delegatedAdminPrincipal(), second, second.SHA256)
	if err != nil || updated.Action != "update" {
		t.Fatalf("delegated update failed: %+v %v", updated, err)
	}
	activated, err := h.service.ActivateVersion(h.ctx, delegatedAdminPrincipal(), installed.InstallationID, installed.PackageID)
	if err != nil || activated.Action != "activate" {
		t.Fatalf("delegated rollback failed: %+v %v", activated, err)
	}
	events, _ = h.store.ListPluginAuditEvents(h.ctx, installed.InstanceID, 0, 10)
	if events[0].Capability != AuditManageActivate || events[1].Capability != AuditManageUpdate {
		t.Fatalf("update and rollback must be audited: %s %s", events[0].Capability, events[1].Capability)
	}

	if err := h.service.SetSecret(h.ctx, serviceAccountAdminPrincipal(), installed.InstanceID, "API_KEY", "s3cret-value"); CodeOf(err) != CodePermissionDenied {
		t.Fatalf("service accounts must not write secrets, got %v", err)
	}
	if err := h.service.SetSecret(h.ctx, delegatedAdminPrincipal(), installed.InstanceID, "API_KEY", "s3cret-value"); err != nil {
		t.Fatalf("delegated secret write failed: %v", err)
	}
	events, _ = h.store.ListPluginAuditEvents(h.ctx, installed.InstanceID, 0, 10)
	if events[0].Capability != AuditManageSecret || strings.Contains(string(events[0].DetailJSON), "s3cret-value") || !strings.Contains(string(events[0].DetailJSON), "API_KEY") {
		t.Fatalf("a secret write must be audited by name only: %s", events[0].DetailJSON)
	}

	if err := h.service.Uninstall(h.ctx, serviceAccountAdminPrincipal(), installed.InstallationID); CodeOf(err) != CodePermissionDenied {
		t.Fatalf("service accounts must not uninstall, got %v", err)
	}
	if err := h.service.Uninstall(h.ctx, delegatedAdminPrincipal(), installed.InstallationID); err != nil {
		t.Fatalf("delegated uninstall failed: %v", err)
	}
	events, _ = h.store.ListPluginAuditEvents(h.ctx, installed.InstanceID, 0, 10)
	if len(events) == 0 || events[0].Capability != AuditManageUninstall {
		t.Fatalf("uninstall must leave an audit entry behind: %+v", events)
	}
}

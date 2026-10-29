package controller

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/automation"
	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
)

func TestControllerExternalProbeProcess(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OBOARD_DOWNLOADS", dir)
	binary := filepath.Join(dir, "oboard-sb-"+runtime.GOOS+"-"+runtime.GOARCH)
	script := "#!/bin/sh\n[ \"$#\" -eq 1 ] && [ \"$1\" = '-check-outbound' ] || exit 1\ncat >/dev/null\nprintf '%s' '{\"exit_ip\":\"8.8.8.8\"}'\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	ip, err := s.runControllerOutboundProbe(context.Background(), json.RawMessage(`{"password":"test-only"}`))
	if err != nil || ip != "8.8.8.8" {
		t.Fatalf("%q %v", ip, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.runControllerOutboundProbe(ctx, json.RawMessage(`{}`)); err == nil {
		t.Fatal("canceled process started")
	}
	var output boundedProbeOutput
	if _, err := output.Write([]byte(strings.Repeat("x", 1025))); err == nil {
		t.Fatal("unbounded output")
	}
}

func TestControllerExternalProbeFailureClearsSharedRegion(t *testing.T) {
	f := newExternalEgressControllerFixture(t)
	ctx := context.Background()
	f.srv.geoIP = &fakeConnectionAuditGeoResolver{geo: model.IPGeography{CountryCode: "JP"}}
	f.srv.externalCheckRunner = func(context.Context, json.RawMessage) (string, error) { return "8.8.8.8", nil }
	if _, _, err := f.srv.checkExternalOutbound(ctx, *f.external); err != nil {
		t.Fatal(err)
	}
	f.srv.externalCheckRunner = func(context.Context, json.RawMessage) (string, error) {
		return "", errors.New("credential-must-not-leak")
	}
	result, _, err := f.srv.checkExternalOutbound(ctx, *f.external)
	if err != nil || strings.Contains(result.Error, "credential-must-not-leak") {
		t.Fatalf("%+v %v", result, err)
	}
	saved, err := f.db.GetExternalOutbound(ctx, f.external.ID)
	if err != nil || saved.ProbeRegionCode != "" || saved.ProbeExitIP != "" || saved.ProbeStatus != "failed" || !core.ExternalOutboundProbeCurrent(*saved) {
		t.Fatalf("%+v %v", saved, err)
	}
}

func TestControllerExternalProbeRejectsConcurrentAndChangedConfiguration(t *testing.T) {
	f := newExternalEgressControllerFixture(t)
	ctx := context.Background()
	f.srv.externalCheckMu.Lock()
	if _, status, err := f.srv.checkExternalOutbound(ctx, *f.external); err == nil || status != 409 {
		t.Fatal("concurrent probe accepted")
	}
	f.srv.externalCheckMu.Unlock()
	f.srv.externalCheckRunner = func(context.Context, json.RawMessage) (string, error) {
		changed := *f.external
		changed.TargetPort++
		return "8.8.8.8", f.db.UpdateExternalOutbound(ctx, &changed)
	}
	if _, status, err := f.srv.checkExternalOutbound(ctx, *f.external); err == nil || status != 409 {
		t.Fatal("changed configuration accepted")
	}
	saved, err := f.db.GetExternalOutbound(ctx, f.external.ID)
	if err != nil || core.ExternalOutboundProbeCurrent(*saved) {
		t.Fatal("stale result reused")
	}
}

func TestControllerExternalProbePreservesMCPResourceBoundary(t *testing.T) {
	f := newExternalEgressControllerFixture(t)
	ctx := context.Background()
	operator := &model.User{Username: "probe-operator", PasswordHash: "unused", Role: model.RoleOperator, Status: "active", ProxyUUID: "11111111-1111-4111-8111-111111111111", ProxyPassword: "unused"}
	if err := f.db.CreateUser(ctx, operator); err != nil {
		t.Fatal(err)
	}
	f.external.Scope, f.external.ServerID = model.ExternalOutboundScopeServer, &f.server.ID
	if err := f.db.UpdateExternalOutbound(ctx, f.external); err != nil {
		t.Fatal(err)
	}
	principal := trafficAutomationPrincipal(t, f.srv, operator.Username)
	input, _ := json.Marshal(map[string]any{"external_outbound_id": f.external.ID})
	draft := automation.DraftValidationRequest{Operations: []automation.OperationRequest{{Capability: "external_outbounds.check", Input: input}}}
	if _, err := f.srv.automation.ValidateDraft(ctx, principal, draft); err != nil {
		t.Fatal(err)
	}
	principal.ResourceFilter = json.RawMessage(`{"servers":{"mode":"none"}}`)
	if _, err := f.srv.automation.ValidateDraft(ctx, principal, draft); err == nil {
		t.Fatal("unauthorized node check accepted")
	}
}

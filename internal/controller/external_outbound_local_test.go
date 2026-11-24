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
	"time"

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
	if _, _, err := f.srv.checkExternalOutbound(ctx, *f.external, nil); err != nil {
		t.Fatal(err)
	}
	f.srv.externalCheckRunner = func(context.Context, json.RawMessage) (string, error) {
		return "", errors.New("credential-must-not-leak")
	}
	result, _, err := f.srv.checkExternalOutbound(ctx, *f.external, nil)
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
	if _, status, err := f.srv.checkExternalOutbound(ctx, *f.external, nil); err == nil || status != 409 {
		t.Fatal("concurrent probe accepted")
	}
	f.srv.externalCheckMu.Unlock()
	f.srv.externalCheckRunner = func(context.Context, json.RawMessage) (string, error) {
		changed := *f.external
		changed.TargetPort++
		return "8.8.8.8", f.db.UpdateExternalOutbound(ctx, &changed)
	}
	if _, status, err := f.srv.checkExternalOutbound(ctx, *f.external, nil); err == nil || status != 409 {
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

// A loopback client proxy lives on each server; the Controller must not probe
// its own loopback, and an earlier Controller failure must not keep shadowing
// the deployed path probe.
func TestControllerExternalProbeSkipsServerLocalAddresses(t *testing.T) {
	f := newExternalEgressControllerFixture(t)
	ctx := context.Background()
	f.srv.externalCheckRunner = func(context.Context, json.RawMessage) (string, error) {
		return "", errors.New("probe failed")
	}
	if _, _, err := f.srv.checkExternalOutbound(ctx, *f.external, nil); err != nil {
		t.Fatal(err)
	}
	f.external.TargetAddress = "127.0.0.1"
	if err := f.db.UpdateExternalOutbound(ctx, f.external); err != nil {
		t.Fatal(err)
	}
	ran := false
	f.srv.externalCheckRunner = func(context.Context, json.RawMessage) (string, error) {
		ran = true
		return "8.8.8.8", nil
	}
	result, status, err := f.srv.checkExternalOutbound(ctx, *f.external, nil)
	if err != nil || status != 200 || ran || result.Status != "skipped" || result.ExitIP != "" {
		t.Fatalf("result=%+v status=%d ran=%t err=%v", result, status, ran, err)
	}
	saved, err := f.db.GetExternalOutbound(ctx, f.external.ID)
	if err != nil || saved.ProbeFingerprint != "" || saved.ProbeStatus != "" {
		t.Fatalf("stored controller result must be cleared: %+v %v", saved, err)
	}
}

func TestExternalOutboundServerLocal(t *testing.T) {
	for address, want := range map[string]bool{
		"127.0.0.1": true, "::1": true, "[::1]": true, "localhost": true, "LOCALHOST.": true, "proxy.localhost": true,
		"10.0.0.5": true, "192.168.1.1": true, "172.16.0.1": true, "100.64.1.1": true, "fe80::1%eth0": true, "fd00::1": true, "0.0.0.0": true,
		"85.121.122.169": false, "210.198.124.223": false, "2001:4860::8888": false, "proxy.example.com": false, "": false,
	} {
		if got := externalOutboundServerLocal(address); got != want {
			t.Errorf("externalOutboundServerLocal(%q) = %t, want %t", address, got, want)
		}
	}
}

func enableStandaloneProbe(t *testing.T, f *externalEgressControllerFixture) {
	t.Helper()
	f.server.KernelCapabilities = []string{"external_outbound_probe_v1", "external_outbound_check_v1"}
	if err := f.db.UpdateServer(context.Background(), f.server); err != nil {
		t.Fatal(err)
	}
}

// answerStandaloneProbe plays the Agent: it waits for the queued standalone
// task and delivers a result through the real callback handler.
func answerStandaloneProbe(t *testing.T, f *externalEgressControllerFixture, status, exitIP string) <-chan model.AgentTask {
	t.Helper()
	done := make(chan model.AgentTask, 1)
	go func() {
		ctx := context.Background()
		for i := 0; i < 200; i++ {
			tasks, err := f.db.ListPendingTasksByServerAndType(ctx, f.server.ID, model.AgentTaskTypeProbeExternalEgress)
			if err == nil && len(tasks) == 1 {
				task := tasks[0]
				var plan model.ExternalEgressProbePlan
				_ = json.Unmarshal([]byte(task.PayloadJSON), &plan)
				item := model.ExternalEgressProbeItem{ProbeID: plan.Standalone.Fingerprint, Status: status, ExitIP: exitIP}
				raw, _ := json.Marshal(model.ExternalEgressProbeResult{Items: []model.ExternalEgressProbeItem{item}})
				_ = f.srv.applyExternalEgressTaskResults(ctx, f.server.ID, task, status, string(raw))
				_ = f.db.CompleteTask(ctx, task.ID, status, string(raw))
				done <- task
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		close(done)
	}()
	return done
}

func TestControllerExternalProbeFallsBackToServerAgent(t *testing.T) {
	f := newExternalEgressControllerFixture(t)
	ctx := context.Background()
	enableStandaloneProbe(t, &f)
	f.srv.geoIP = &fakeConnectionAuditGeoResolver{geo: model.IPGeography{CountryCode: "JP"}}
	f.srv.externalCheckRunner = func(context.Context, json.RawMessage) (string, error) {
		return "", errors.New("controller address not allowed by proxy")
	}
	answered := answerStandaloneProbe(t, &f, "succeeded", "8.8.8.8")
	result, status, err := f.srv.checkExternalOutbound(ctx, *f.external, nil)
	if err != nil || status != 200 {
		t.Fatalf("status=%d err=%v", status, err)
	}
	if task, ok := <-answered; !ok || task.ServerID != f.server.ID {
		t.Fatal("no standalone probe was queued on the server")
	}
	if result.Status != "reachable" || result.ExitIP != "8.8.8.8" || result.RegionCode != "JP" || result.ServerID != f.server.ID || result.ServerName != f.server.Name {
		t.Fatalf("fallback result = %+v", result)
	}
	saved, err := f.db.GetExternalOutbound(ctx, f.external.ID)
	if err != nil || saved.ProbeRegionCode != "JP" || saved.ProbeTaskID == 0 || !core.ExternalOutboundProbeCurrent(*saved) {
		t.Fatalf("saved = %+v %v", saved, err)
	}
}

func TestControllerExternalProbeFallbackReportsBothFailures(t *testing.T) {
	f := newExternalEgressControllerFixture(t)
	ctx := context.Background()
	enableStandaloneProbe(t, &f)
	f.srv.externalCheckRunner = func(context.Context, json.RawMessage) (string, error) { return "", errors.New("down") }
	answered := answerStandaloneProbe(t, &f, "failed", "")
	result, _, err := f.srv.checkExternalOutbound(ctx, *f.external, nil)
	<-answered
	if err != nil || result.Status != "unreachable" || result.ServerID != f.server.ID || !strings.Contains(result.Error, f.server.Name) {
		t.Fatalf("result = %+v %v", result, err)
	}
}

func TestControllerExternalProbeFallbackRespectsServerBoundary(t *testing.T) {
	f := newExternalEgressControllerFixture(t)
	ctx := context.Background()
	enableStandaloneProbe(t, &f)
	f.srv.externalCheckRunner = func(context.Context, json.RawMessage) (string, error) { return "", errors.New("down") }
	result, _, err := f.srv.checkExternalOutbound(ctx, *f.external, func(int64) bool { return false })
	if err != nil || result.Status != "unreachable" || result.ServerID != 0 || !strings.Contains(result.Error, "授权服务器") {
		t.Fatalf("result = %+v %v", result, err)
	}
	tasks, _ := f.db.ListPendingTasksByServerAndType(ctx, f.server.ID, model.AgentTaskTypeProbeExternalEgress)
	if len(tasks) != 0 {
		t.Fatalf("task queued outside the boundary: %+v", tasks)
	}
}

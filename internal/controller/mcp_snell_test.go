package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/automation"
	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
)

func TestSnellProfileAutomationLifecycle(t *testing.T) {
	db := openControllerAutomationTestStore(t)
	server := newTestServer(db, "test-secret", "")
	ctx := context.Background()
	admin := &model.User{Username: "root", PasswordHash: "unused", Role: model.RoleAdmin, Status: "active", ProxyUUID: "22222222-2222-4222-8222-222222222222", ProxyPassword: "unused"}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}
	adminPrincipal := userAutomationPrincipal(t, db, admin.ID)
	createInput := json.RawMessage(`{"snell_profile":{"name":"机房A v4","version":4,"psk":"secret-psk-1234","obfs_mode":"http","obfs_host":"bing.com","mode":"default"}}`)
	applyAutomationChangeset(t, server, adminPrincipal, "snell-profile-create", automation.OperationRequest{Capability: "snell_profiles.create", Input: createInput})
	profiles, err := db.ListSnellProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var profile *model.SnellProfile
	for index := range profiles {
		if profiles[index].Name == "机房A v4" {
			profile = &profiles[index]
			break
		}
	}
	if profile == nil || profile.Version != 4 || profile.PSK != "secret-psk-1234" || profile.ObfsMode != "http" || profile.ObfsHost != "bing.com" {
		t.Fatalf("created profile missing: %#v", profiles)
	}
	updateInput, _ := json.Marshal(map[string]any{"snell_profile_id": profile.ID, "changes": map[string]any{"psk": "new-psk-5678", "obfs_mode": "http"}})
	applyAutomationChangeset(t, server, adminPrincipal, "snell-profile-update", automation.OperationRequest{Capability: "snell_profiles.update", Input: updateInput})
	after, err := db.GetSnellProfile(ctx, profile.ID)
	if err != nil || after.PSK != "new-psk-5678" || after.ObfsMode != "http" {
		t.Fatalf("profile after update: %#v err=%v", after, err)
	}
	// Deleting an unreferenced custom profile succeeds.
	deleteInput, _ := json.Marshal(map[string]any{"snell_profile_id": profile.ID, "confirm": true})
	applyAutomationChangeset(t, server, adminPrincipal, "snell-profile-delete", automation.OperationRequest{Capability: "snell_profiles.delete", Input: deleteInput})
	if _, err := db.GetSnellProfile(ctx, profile.ID); err == nil {
		t.Fatal("profile not deleted")
	}
}

func TestSnellProfileShapingLifecycle(t *testing.T) {
	db := openControllerAutomationTestStore(t)
	server := newTestServer(db, "test-secret", "")
	ctx := context.Background()
	admin := &model.User{Username: "root", PasswordHash: "unused", Role: model.RoleAdmin, Status: "active", ProxyUUID: "22222222-2222-4222-8222-222222222222", ProxyPassword: "unused"}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}
	adminPrincipal := userAutomationPrincipal(t, db, admin.ID)
	createInput := json.RawMessage(`{"snell_profile":{"name":"v6 避让","version":6,"psk":"","mode":"default","enabled":true,"shaping":{"first_record_density":{"mode":"outside","min":0.36,"max":0.65}}}}`)
	applyAutomationChangeset(t, server, adminPrincipal, "snell-profile-shaping", automation.OperationRequest{Capability: "snell_profiles.create", Input: createInput})
	profiles, err := db.ListSnellProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var profile *model.SnellProfile
	for index := range profiles {
		if profiles[index].Name == "v6 避让" {
			profile = &profiles[index]
		}
	}
	want := model.SnellShapingPolicy{FirstRecordDensity: model.SnellDensityBand{Mode: "outside", Min: 0.36, Max: 0.65}}
	if profile == nil || profile.Shaping != want {
		t.Fatalf("shaping not stored: %#v", profile)
	}

	inbound := model.Inbound{Protocol: model.ProtocolSnell, ConfigJSON: fmt.Sprintf(`{"version":6,"snell_profile_id":%d}`, profile.ID)}
	if err := server.resolveSnellProfileIntoInbound(ctx, &inbound); err != nil {
		t.Fatal(err)
	}
	if got := core.InboundSnellShaping(inbound); got != want {
		t.Fatalf("inbound did not inherit shaping: %s", inbound.ConfigJSON)
	}
	inbound.ConfigJSON = fmt.Sprintf(`{"version":6,"snell_profile_id":%d,"shaping":{"padding_style":"bit_biased"}}`, profile.ID)
	if err := server.resolveSnellProfileIntoInbound(ctx, &inbound); err != nil {
		t.Fatal(err)
	}
	if got := core.InboundSnellShaping(inbound); got != (model.SnellShapingPolicy{PaddingStyle: "bit_biased"}) {
		t.Fatalf("inbound shaping was overridden by the profile: %s", inbound.ConfigJSON)
	}

	for name, candidate := range map[string]model.SnellProfile{
		"unsafe-raw": {Name: "raw", Version: 6, Mode: "unsafe-raw"},
		"v4":         {Name: "v4", Version: 4, Shaping: want},
		"unshaped":   {Name: "unshaped", Version: 6, Mode: "unshaped", Shaping: want},
		"infeasible": {Name: "strict", Version: 6, Shaping: model.SnellShapingPolicy{PaddingStyle: "utf8_like", FirstRecordDensity: model.SnellDensityBand{Mode: "outside", Min: 0.425, Max: 0.573}}},
	} {
		if err := validateSnellProfile(candidate); err == nil {
			t.Fatalf("%s profile accepted", name)
		}
	}
}

func TestSnellShapingCheckRecommendsOverRESTAndMCP(t *testing.T) {
	db := openControllerAutomationTestStore(t)
	server := newTestServer(db, "test-secret", "")
	ctx := context.Background()
	operator := &model.User{Username: "operator", PasswordHash: "unused", Role: model.RoleOperator, Status: "active", ProxyUUID: "33333333-3333-4333-8333-333333333333", ProxyPassword: "unused"}
	if err := db.CreateUser(ctx, operator); err != nil {
		t.Fatal(err)
	}
	conflicting := `{"shaping":{"padding_style":"utf8_like","first_record_density":{"mode":"outside","min":0.36,"max":0.65}}}`
	want := model.SnellShapingPolicy{PaddingStyle: "bit_biased", FirstRecordDensity: model.SnellDensityBand{Mode: "outside", Min: 0.36, Max: 0.65}}

	recorder := httptest.NewRecorder()
	server.checkSnellShaping(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/snell-shaping/check", strings.NewReader(conflicting)))
	var rest struct {
		Check core.SnellShapingCheck `json:"check"`
	}
	if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &rest) != nil || rest.Check.Feasible || rest.Check.Recommended == nil || *rest.Check.Recommended != want || len(rest.Check.Changes) == 0 {
		t.Fatalf("REST check: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	server.checkSnellShaping(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/snell-shaping/check", strings.NewReader(`{"shaping":{"max_padding":10}}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid policy accepted: %d", recorder.Code)
	}

	result, err := server.application.Query(ctx, userAutomationPrincipal(t, db, operator.ID), "snell_profiles.shaping.check", json.RawMessage(conflicting))
	if err != nil {
		t.Fatal(err)
	}
	check, ok := result.(core.SnellShapingCheck)
	if !ok || check.Feasible || check.Recommended == nil || *check.Recommended != want {
		t.Fatalf("MCP check: %#v", result)
	}
}

package controller

import (
	"context"
	"net/http"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
)

func onboardingItemStatus(t *testing.T, view map[string]any, key string) string {
	t.Helper()
	for _, raw := range view["items"].([]any) {
		item := raw.(map[string]any)
		if item["key"] == key {
			return item["status"].(string)
		}
	}
	t.Fatalf("onboarding item %s missing: %#v", key, view["items"])
	return ""
}

func TestOnboardingGuidesFreshPanelAndGatesCompletion(t *testing.T) {
	db := openControllerAutomationTestStore(t)
	srv := newTestServer(db, "test-secret", "")
	h := srv.Handler()
	ctx := context.Background()
	request(t, h, http.MethodPost, "/api/v1/ui/auth/bootstrap", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusCreated)
	token := request(t, h, http.MethodPost, "/api/v1/ui/auth/login", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusOK)["token"].(string)
	revisionBefore, err := db.ConfigurationRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}

	fresh := request(t, h, http.MethodGet, "/api/v1/ui/onboarding", token, nil, http.StatusOK)
	if fresh["mode"] != "fresh" || fresh["show_welcome"] != true || fresh["can_complete"] != false {
		t.Fatalf("fresh panel view = %#v", fresh)
	}
	if onboardingItemStatus(t, fresh, "password") != "pending" || onboardingItemStatus(t, fresh, "server") != "pending" {
		t.Fatalf("fresh checklist = %#v", fresh["items"])
	}

	started := request(t, h, http.MethodPost, "/api/v1/ui/onboarding", token, map[string]any{"action": "start"}, http.StatusOK)
	if started["state"].(map[string]any)["status"] != "in_progress" || started["show_welcome"] != false || started["mode"] != "tracked" {
		t.Fatalf("started view = %#v", started)
	}
	request(t, h, http.MethodPost, "/api/v1/ui/onboarding", token, map[string]any{"action": "skip", "item": "password"}, http.StatusBadRequest)
	request(t, h, http.MethodPost, "/api/v1/ui/onboarding", token, map[string]any{"action": "confirm", "item": "server"}, http.StatusBadRequest)
	request(t, h, http.MethodPost, "/api/v1/ui/onboarding", token, map[string]any{"action": "skip", "item": "unknown"}, http.StatusBadRequest)
	confirmed := request(t, h, http.MethodPost, "/api/v1/ui/onboarding", token, map[string]any{"action": "confirm", "item": "node_defaults"}, http.StatusOK)
	if onboardingItemStatus(t, confirmed, "node_defaults") != "done" {
		t.Fatalf("confirmed node defaults = %#v", confirmed["items"])
	}
	for _, item := range []string{"server", "entry", "access", "subscription"} {
		request(t, h, http.MethodPost, "/api/v1/ui/onboarding", token, map[string]any{"action": "skip", "item": item}, http.StatusOK)
	}
	request(t, h, http.MethodPost, "/api/v1/ui/onboarding", token, map[string]any{"action": "complete"}, http.StatusConflict)

	// Rotating the generated bootstrap password is recorded by the password
	// endpoint itself; it revokes the session, so sign in again.
	request(t, h, http.MethodPost, "/api/v1/ui/auth/password", token, map[string]any{"current_password": "very-secure-password", "new_password": "another-secure-password"}, http.StatusOK)
	token = request(t, h, http.MethodPost, "/api/v1/ui/auth/login", "", map[string]any{"username": "admin", "password": "another-secure-password"}, http.StatusOK)["token"].(string)
	rotated := request(t, h, http.MethodGet, "/api/v1/ui/onboarding", token, nil, http.StatusOK)
	if onboardingItemStatus(t, rotated, "password") != "done" {
		t.Fatalf("password rotation not recorded: %#v", rotated["items"])
	}

	if err := db.SetSetting(ctx, "controller_url", "https://panel.example.com"); err != nil {
		t.Fatal(err)
	}
	completed := request(t, h, http.MethodPost, "/api/v1/ui/onboarding", token, map[string]any{"action": "complete"}, http.StatusOK)
	if completed["state"].(map[string]any)["status"] != "completed" || completed["can_complete"] != true {
		t.Fatalf("completed view = %#v", completed)
	}
	if onboardingItemStatus(t, completed, "server") != "skipped" {
		t.Fatalf("skipped item lost: %#v", completed["items"])
	}

	tours := request(t, h, http.MethodPost, "/api/v1/ui/onboarding/tours", token, map[string]any{"key": "proxy_path", "status": "completed"}, http.StatusOK)
	if tours["tours"].(map[string]any)["proxy_path"] != "completed" {
		t.Fatalf("tour progress = %#v", tours)
	}
	request(t, h, http.MethodPost, "/api/v1/ui/onboarding/tours", token, map[string]any{"key": "unknown", "status": "completed"}, http.StatusBadRequest)

	// Guide progress is presentation state and must never look like a
	// configuration change to the deployment coordinator.
	revisionAfter, err := db.ConfigurationRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if revisionAfter != revisionBefore {
		t.Fatalf("onboarding writes moved the configuration revision from %d to %d", revisionBefore, revisionAfter)
	}
}

func TestOnboardingLeavesEstablishedPanelsQuiet(t *testing.T) {
	db := openControllerAutomationTestStore(t)
	srv := newTestServer(db, "test-secret", "")
	h := srv.Handler()
	ctx := context.Background()
	request(t, h, http.MethodPost, "/api/v1/ui/auth/bootstrap", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusCreated)
	token := request(t, h, http.MethodPost, "/api/v1/ui/auth/login", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusOK)["token"].(string)
	if err := db.CreateServer(ctx, &model.Server{Name: "existing", ListenIP: "0.0.0.0", PortRangeStart: 10000, PortRangeEnd: 20000, Status: model.ServerOnline}); err != nil {
		t.Fatal(err)
	}

	view := request(t, h, http.MethodGet, "/api/v1/ui/onboarding", token, nil, http.StatusOK)
	if view["mode"] != "established" || view["show_welcome"] != false {
		t.Fatalf("established panel view = %#v", view)
	}
	if onboardingItemStatus(t, view, "password") != "done" || onboardingItemStatus(t, view, "server") != "done" {
		t.Fatalf("established checklist = %#v", view["items"])
	}

	// A routine password change on an established panel must not create
	// guide state and make the guide appear.
	request(t, h, http.MethodPost, "/api/v1/ui/auth/password", token, map[string]any{"current_password": "very-secure-password", "new_password": "another-secure-password"}, http.StatusOK)
	if raw, err := db.GetSetting(ctx, onboardingSetupSetting); err != nil || raw != "" {
		t.Fatalf("established password change stored guide state %q err=%v", raw, err)
	}
}

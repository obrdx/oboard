package controller

import (
	"net/http"
	"testing"
)

func TestServerCreateRequiresRenewalCycleOnlyWithExpiry(t *testing.T) {
	db := openControllerAutomationTestStore(t)
	srv := newTestServer(db, "test-secret", "")
	h := srv.Handler()
	request(t, h, http.MethodPost, "/api/v1/ui/auth/bootstrap", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusCreated)
	login := request(t, h, http.MethodPost, "/api/v1/ui/auth/login", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusOK)
	token := login["token"].(string)
	request(t, h, http.MethodPost, "/api/v1/ui/servers", token, map[string]any{"name": "missing-cycle", "expires_at": "2027-01-02T03:04:05Z"}, http.StatusBadRequest)
	undated := request(t, h, http.MethodPost, "/api/v1/ui/servers", token, map[string]any{"name": "undated"}, http.StatusCreated)["server"].(map[string]any)
	if undated["auto_renew_enabled"] != true || undated["renewal_cycle"] != "monthly" {
		t.Fatalf("renewal without expiry should keep the monthly default: %#v", undated)
	}
	blank := request(t, h, http.MethodPost, "/api/v1/ui/servers", token, map[string]any{"name": "blank-cycle", "renewal_cycle": ""}, http.StatusCreated)["server"].(map[string]any)
	if blank["renewal_cycle"] != "monthly" {
		t.Fatalf("blank renewal cycle should fall back to monthly: %#v", blank)
	}
	request(t, h, http.MethodPost, "/api/v1/ui/servers", token, map[string]any{"name": "blank-dated", "renewal_cycle": "", "expires_at": "2027-01-02T03:04:05Z"}, http.StatusBadRequest)
	dated := request(t, h, http.MethodPost, "/api/v1/ui/servers", token, map[string]any{"name": "dated", "expires_at": "2027-01-02T03:04:05Z", "renewal_cycle": "quarterly"}, http.StatusCreated)["server"].(map[string]any)
	if dated["renewal_cycle"] != "quarterly" {
		t.Fatalf("dated renewal cycle = %#v", dated)
	}
	created := request(t, h, http.MethodPost, "/api/v1/ui/servers", token, map[string]any{"name": "monthly", "renewal_cycle": "monthly"}, http.StatusCreated)["server"].(map[string]any)
	if created["auto_renew_enabled"] != true || created["renewal_cycle"] != "monthly" {
		t.Fatalf("default renewal = %#v", created)
	}
	for _, cycle := range []string{"semiannual", "annual"} {
		created := request(t, h, http.MethodPost, "/api/v1/ui/servers", token, map[string]any{"name": cycle, "renewal_cycle": cycle}, http.StatusCreated)["server"].(map[string]any)
		if created["renewal_cycle"] != cycle {
			t.Fatalf("renewal cycle = %v, want %s", created["renewal_cycle"], cycle)
		}
	}
	disabled := request(t, h, http.MethodPost, "/api/v1/ui/servers", token, map[string]any{"name": "disabled", "auto_renew_enabled": false}, http.StatusCreated)["server"].(map[string]any)
	if disabled["auto_renew_enabled"] != false {
		t.Fatalf("explicitly disabled renewal = %#v", disabled)
	}
}

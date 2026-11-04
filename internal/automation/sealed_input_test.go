package automation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/application"
	"github.com/OboardProject/oboard/internal/capability"
	"github.com/OboardProject/oboard/internal/mcpauth"
	"github.com/OboardProject/oboard/internal/model"
)

func TestSealedInputNeverPersistsThePluginSecret(t *testing.T) {
	ctx := context.Background()
	db := openAutomationTestStore(t)
	stored := &model.APIPrincipal{ID: "oauth_admin", Name: "OAuth", Type: model.APIPrincipalOAuth, Enabled: true, Scopes: []string{"plugins:authorize"}, ResourceFilter: json.RawMessage(`{}`), RateLimitPerMinute: 60, MaxConcurrency: 2}
	if err := db.CreateAPIPrincipal(ctx, stored); err != nil {
		t.Fatal(err)
	}
	principal := application.Principal{ID: stored.ID, Type: stored.Type, Role: model.RoleAdmin, AccessLevel: mcpauth.AccessOperate, Scopes: stored.Scopes, ResourceFilter: stored.ResourceFilter}
	const capabilityName, secret = "plugin_instances.secrets.update", "top-secret-value"
	input := json.RawMessage(`{"instance_id":"7","name":"API_KEY","value":"` + secret + `"}`)

	unsealed := NewService(db, capability.NewCatalog())
	register := func(service *Service, seen *[]string) {
		service.RegisterValidator(capabilityName, func(_ context.Context, _ application.Principal, raw json.RawMessage) (any, error) {
			*seen = append(*seen, string(raw))
			return map[string]bool{"valid": true}, nil
		})
		service.Register(capabilityName, func(_ context.Context, _ application.Principal, raw json.RawMessage) (any, error) {
			*seen = append(*seen, string(raw))
			return map[string]bool{"configured": true}, nil
		})
	}
	var ignored []string
	register(unsealed, &ignored)
	if _, err := unsealed.Create(ctx, principal, CreateRequest{IdempotencyKey: "no-key", Operations: []OperationRequest{{Capability: capabilityName, Input: input}}}); err == nil {
		t.Fatal("a sealed field must be refused when no seal key is configured")
	}

	service := NewService(db, capability.NewCatalog())
	service.SetInputSealKey("test-session-secret")
	allow := func(context.Context, application.Principal, model.AutomationOperation) error { return nil }
	service.SetReplayAuthorizer(allow)
	service.SetResultAuthorizer(allow)
	var seen []string
	register(service, &seen)
	item, err := service.Create(ctx, principal, CreateRequest{IdempotencyKey: "secret-1", Operations: []OperationRequest{{Capability: capabilityName, Input: input}}})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := db.GetAutomationChangeset(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(persisted.Operations[0].Input), secret) || !strings.Contains(string(persisted.Operations[0].Input), sealedInputPrefix) {
		t.Fatalf("the stored operation must carry only the sealed value: %s", persisted.Operations[0].Input)
	}
	result, err := service.Validate(ctx, principal, item.ID)
	if err != nil || result.Status != model.ChangesetSucceeded {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(seen) < 2 {
		t.Fatalf("validator and handler must both run: %v", seen)
	}
	for _, raw := range seen {
		if !strings.Contains(raw, `"value":"`+secret+`"`) {
			t.Fatalf("the validator and handler must receive the plaintext: %s", raw)
		}
	}
	persisted, _ = db.GetAutomationChangeset(ctx, item.ID)
	encoded, _ := json.Marshal(persisted)
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("the applied changeset must not contain the secret: %s", encoded)
	}
	replay, err := service.Create(ctx, principal, CreateRequest{IdempotencyKey: "secret-1", Operations: []OperationRequest{{Capability: capabilityName, Input: input}}})
	if err != nil || replay.ID != item.ID {
		t.Fatalf("an identical retry must replay the sealed changeset: %+v %v", replay, err)
	}
	changed := json.RawMessage(`{"instance_id":"7","name":"API_KEY","value":"another"}`)
	if _, err := service.Create(ctx, principal, CreateRequest{IdempotencyKey: "secret-1", Operations: []OperationRequest{{Capability: capabilityName, Input: changed}}}); err != ErrIdempotencyConflict {
		t.Fatalf("a different secret under the same key must conflict, got %v", err)
	}
}

package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OboardProject/oboard/internal/store"
)

func newMCPProtocolRoutingServer(t *testing.T) (endpoint, token string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "oboard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	server := newAgentLinkTestServer(db, "test-secret", "")
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	if err := db.SetSetting(context.Background(), "controller_url", httpServer.URL); err != nil {
		t.Fatal(err)
	}
	request(t, server.Handler(), http.MethodPost, "/api/v1/ui/auth/bootstrap", "", map[string]any{"username": "admin", "password": "very-secure-password"}, http.StatusCreated)
	user, err := db.GetUserByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	client := testOAuthClient(t, db, "oc_"+randomTestID(), "MCP routing client", []string{"http://127.0.0.1/callback"})
	grant, principal := createTestGrant(t, server, *user, client, nil)
	token = "oba_test-token-" + randomTestID()
	endpoint = httpServer.URL + "/api/v1/mcp"
	issueTestMCPToken(t, db, grant, principal, client, user.ID, endpoint, token)
	return endpoint, token
}

func postMCP(t *testing.T, endpoint, token, protocolVersion, sessionID, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if protocolVersion != "" {
		req.Header.Set("Mcp-Protocol-Version", protocolVersion)
	}
	var envelope struct {
		Method string `json:"method"`
	}
	if protocolVersion >= mcpModernProtocolVersion && json.Unmarshal([]byte(body), &envelope) == nil {
		req.Header.Set("Mcp-Method", envelope.Method)
	}
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeMCPResult(t *testing.T, resp *http.Response, into any) {
	t.Helper()
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode MCP response (status %d): %v", resp.StatusCode, err)
	}
	if envelope.Error != nil {
		t.Fatalf("MCP error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if err := json.Unmarshal(envelope.Result, into); err != nil {
		t.Fatal(err)
	}
}

func modernMCPBody(id int, method string) string {
	return `{"jsonrpc":"2.0","id":` + mcpRoutingID(id) + `,"method":"` + method + `","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"chatgpt-like","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
}

func mcpRoutingID(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestMCPModernProtocolDiscoverAndListTools(t *testing.T) {
	endpoint, token := newMCPProtocolRoutingServer(t)

	discover := postMCP(t, endpoint, token, "2026-07-28", "", modernMCPBody(1, "server/discover"))
	if discover.StatusCode != http.StatusOK {
		t.Fatalf("server/discover status = %d", discover.StatusCode)
	}
	var discovered struct {
		SupportedVersions []string `json:"supportedVersions"`
	}
	decodeMCPResult(t, discover, &discovered)
	found := false
	for _, v := range discovered.SupportedVersions {
		found = found || v == "2026-07-28"
	}
	if !found {
		t.Fatalf("server/discover supportedVersions = %v; want 2026-07-28", discovered.SupportedVersions)
	}

	list := postMCP(t, endpoint, token, "2026-07-28", "", modernMCPBody(2, "tools/list"))
	if list.StatusCode != http.StatusOK {
		t.Fatalf("tools/list status = %d", list.StatusCode)
	}
	if list.Header.Get("Mcp-Session-Id") != "" {
		t.Fatalf("modern protocol response carries a session id")
	}
	var tools struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	decodeMCPResult(t, list, &tools)
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	if !names["oboard_task"] || !names["system_get_capabilities"] {
		t.Fatalf("modern tools/list is missing stable tools: %v", names)
	}
}

func TestMCPModernProtocolRequiresBearerToken(t *testing.T) {
	endpoint, _ := newMCPProtocolRoutingServer(t)
	resp := postMCP(t, endpoint, "oba_invalid", "2026-07-28", "", modernMCPBody(1, "server/discover"))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated server/discover status = %d; want 401", resp.StatusCode)
	}
}

func TestMCPLegacyInitializeKeepsStatefulSession(t *testing.T) {
	endpoint, token := newMCPProtocolRoutingServer(t)
	initialize := postMCP(t, endpoint, token, "", "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"legacy","version":"1"}}}`)
	if initialize.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d", initialize.StatusCode)
	}
	sessionID := initialize.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("legacy initialize did not open a stateful session")
	}
	var initialized mcp.InitializeResult
	decodeMCPResult(t, initialize, &initialized)
	if initialized.ProtocolVersion != "2025-11-25" {
		t.Fatalf("negotiated %q; want 2025-11-25", initialized.ProtocolVersion)
	}
	postMCP(t, endpoint, token, "2025-11-25", sessionID, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	list := postMCP(t, endpoint, token, "2025-11-25", sessionID, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	var tools struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	decodeMCPResult(t, list, &tools)
	if len(tools.Tools) == 0 {
		t.Fatal("legacy tools/list returned no tools")
	}
}

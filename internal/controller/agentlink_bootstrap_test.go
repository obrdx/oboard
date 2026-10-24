package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/agentlink"
	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/security"
)

func TestAgentLinkTransitionDiscoveryAndRPCIsolation(t *testing.T) {
	t.Setenv("OBOARD_STEALTH_ADDR", "")
	db := openControllerAutomationTestStore(t)
	srv := newTestServer(db, "test-secret", "")
	defer srv.Close()
	srv.ConfigureStealthTransport(filepath.Join(t.TempDir(), "controller.sqlite"))
	if err := srv.StartStealthTransport(context.Background()); err != nil {
		t.Fatal(err)
	}
	addr := unusedStealthAddress(t)
	cfg := stealthTransportConfig{Enabled: true, ListenAddress: addr, PublicAddress: addr}
	if err := srv.updateStealthConfig(context.Background(), cfg, true); err != nil {
		t.Fatal(err)
	}
	node := &model.Server{Name: "standard-upgrade", AgentID: "old-agent", AgentTokenHash: security.HashSecret("old-token"), Status: model.ServerOffline}
	if err := db.CreateServer(context.Background(), node); err != nil {
		t.Fatal(err)
	}
	nonce := strings.Repeat("b", 64)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/transport", nil)
	req.Header.Set("X-Agent-ID", node.AgentID)
	req.Header.Set("Authorization", "Bearer old-token")
	req.Header.Set("X-Oboard-Transport-Nonce", nonce)
	out := httptest.NewRecorder()
	srv.Handler().ServeHTTP(out, req)
	var bootstrap agentlink.Bootstrap
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &bootstrap) != nil || bootstrap.ServerID != node.ID || bootstrap.Nonce != nonce || !bootstrap.Verify(node.AgentTokenHash) {
		t.Fatalf("invalid transition metadata: %d %s", out.Code, out.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	control, err := agentlink.Dial(ctx, agentlink.ClientConfig{Address: addr, CertSHA256: bootstrap.CertSHA256, AgentID: node.AgentID, Token: "old-token"})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	messages := make(chan string, 16)
	controlDone := make(chan error, 1)
	go func() {
		controlDone <- control.RunClient(func(msg map[string]json.RawMessage) {
			var kind string
			_ = json.Unmarshal(msg["type"], &kind)
			select {
			case messages <- kind:
			default:
			}
		})
	}()
	defer func() { control.Close(); <-controlDone }()
	select {
	case kind := <-messages:
		if kind != "hello" {
			t.Fatal(kind)
		}
	case <-ctx.Done():
		t.Fatal("control hello missing")
	}
	rpc, err := agentlink.Dial(ctx, agentlink.ClientConfig{Address: addr, CertSHA256: bootstrap.CertSHA256, AgentID: node.AgentID, Token: "old-token", Purpose: "rpc"})
	if err != nil {
		t.Fatal(err)
	}
	rpcDone := make(chan error, 1)
	go func() { rpcDone <- rpc.RunClient(nil) }()
	response, err := rpc.Request(ctx, "/download", map[string]any{"stream": "unknown", "offset": 0, "length": 16}, nil)
	rpc.Close()
	<-rpcDone
	if err != nil || response.Status != 404 {
		t.Fatalf("download RPC: %v %+v", err, response)
	}
	if !srv.agentControlOnline(node.ID) || !srv.sendAgentControl(node.ID, map[string]any{"type": "transition_test"}) {
		t.Fatal("RPC replaced control session")
	}
	for {
		select {
		case kind := <-messages:
			if kind == "transition_test" {
				return
			}
		case <-ctx.Done():
			t.Fatal("control stopped after RPC")
		}
	}
}

func TestAgentLinkRequiresDedicatedAddressForAllModes(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		if _, err := normalizeStealthConfig(stealthTransportConfig{Enabled: enabled, ListenAddress: "0.0.0.0:24443"}); err == nil {
			t.Fatal("empty dedicated address accepted")
		}
	}
	db := openControllerAutomationTestStore(t)
	srv := newTestServer(db, "test-secret", "")
	defer srv.Close()
	_ = db.SetSetting(context.Background(), "controller_url", "https://panel.example.com")
	for _, stealth := range []bool{false, true} {
		if _, _, err := srv.agentEnrollmentCommand(context.Background(), stealth, 1); err == nil {
			t.Fatal("install command omitted AgentLink")
		}
	}
	node := &model.Server{Name: "old", AgentID: "old-agent", Status: model.ServerOnline}
	if _, _, err := srv.enqueueAgentUpdate(context.Background(), node, model.AgentUpdateRequest{}); err == nil {
		t.Fatal("update allowed before AgentLink configured")
	}
}

func TestAgentLinkTerminalUsesProofAndBoundedStream(t *testing.T) {
	t.Setenv("OBOARD_STEALTH_ADDR", "")
	db := openControllerAutomationTestStore(t)
	srv := newTestServer(db, "test-secret", "")
	defer srv.Close()
	srv.ConfigureStealthTransport(filepath.Join(t.TempDir(), "controller.sqlite"))
	if err := srv.StartStealthTransport(context.Background()); err != nil {
		t.Fatal(err)
	}
	addr := unusedStealthAddress(t)
	if err := srv.updateStealthConfig(context.Background(), stealthTransportConfig{Enabled: true, ListenAddress: addr, PublicAddress: addr}, true); err != nil {
		t.Fatal(err)
	}
	node := &model.Server{Name: "terminal-node", AgentID: "terminal-agent", AgentTokenHash: security.HashSecret("terminal-token"), Status: model.ServerOffline}
	if err := db.CreateServer(context.Background(), node); err != nil {
		t.Fatal(err)
	}
	_, pin, _ := srv.StealthTransportInfo()
	expires := time.Now().Add(time.Minute)
	terminal := &terminalSession{ID: "terminal-1", ServerID: node.ID, Nonce: "nonce-1", PrepareExp: expires.Format(time.RFC3339Nano), ExpiresAt: expires, OwnerType: InteractiveOwnerMCP, outputBuffer: newTerminalOutputBuffer(terminalMCPBufferMax), readyCh: make(chan struct{})}
	srv.terminalHub.mu.Lock()
	srv.terminalHub.sessions[terminal.ID] = terminal
	srv.terminalHub.mu.Unlock()
	defer srv.releaseTerminalSession(terminal.ID, "test_cleanup")
	cfg := agentlink.ClientConfig{Address: addr, CertSHA256: pin, AgentID: node.AgentID, Token: "terminal-token", Purpose: "interactive", SessionID: terminal.ID, Proof: "invalid"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := agentlink.Dial(ctx, cfg); err == nil {
		t.Fatal("invalid terminal proof accepted")
	}
	cfg.Proof = security.InteractiveProof(node.AgentTokenHash, terminal.ID, node.ID, terminal.Nonce, terminal.PrepareExp)
	link, err := agentlink.Dial(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer link.Close()
	conn := &agentlink.MessageConn{Session: link}
	if err := conn.WriteMessage(2, []byte("terminal-output")); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for terminal.outputBuffer.TotalBytes() == 0 {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("AgentLink terminal output was not relayed")
		}
	}
	output, _, _, _, _ := terminal.outputBuffer.ReadAfter(0, 1024)
	if string(output) != "terminal-output" {
		t.Fatalf("terminal output changed: %q", output)
	}
	if _, err := agentlink.Dial(ctx, cfg); err == nil {
		t.Fatal("terminal proof replay attached a second connection")
	}
	if err := srv.mcpTerminalWrite(terminal, "terminal-input"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	kind, input, err := conn.ReadMessage()
	if err != nil || kind != 2 || string(input) != "terminal-input" {
		t.Fatalf("terminal input was not relayed: %d %q %v", kind, input, err)
	}
}

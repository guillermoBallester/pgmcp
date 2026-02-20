package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/guillermoBallester/isthmus/internal/auth"
	"github.com/guillermoBallester/isthmus/pkg/tunnel"
	"github.com/guillermoBallester/isthmus/pkg/tunnel/agent"
	"github.com/hashicorp/yamux"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testDatabaseID is a well-known UUID used in tests for the single tunnel database.
var testDatabaseID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// newStaticAuth creates a simple in-memory authenticator for tests.
func newStaticAuth(keys ...string) auth.Authenticator {
	return &staticTestAuth{keys: keys}
}

type staticTestAuth struct {
	keys []string
}

func (a *staticTestAuth) Authenticate(_ context.Context, token string) (*auth.AuthResult, error) {
	for _, k := range a.keys {
		if k == token {
			return &auth.AuthResult{
				DatabaseIDs: []uuid.UUID{testDatabaseID},
			}, nil
		}
	}
	return nil, nil
}

// newTestLogger returns a logger that writes to testing.T.
func newTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	w := &testWriter{t: t}
	t.Cleanup(func() { w.done.Store(true) })
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

type testWriter struct {
	t    *testing.T
	done atomic.Bool
}

func (w *testWriter) Write(p []byte) (n int, err error) {
	if w.done.Load() {
		return len(p), nil
	}
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// newAgentMCPServer creates an MCPServer with a simple "echo" tool for testing.
func newAgentMCPServer() *server.MCPServer {
	s := server.NewMCPServer("test-agent", "0.1.0")
	s.AddTool(
		mcp.NewTool("echo",
			mcp.WithDescription("Echoes back the input message"),
			mcp.WithString("message",
				mcp.Required(),
				mcp.Description("Message to echo"),
			),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			msg := request.GetArguments()["message"]
			return mcp.NewToolResultText(fmt.Sprintf("echo: %v", msg)), nil
		},
	)
	return s
}

// newSlowEchoMCPServer creates an MCPServer with a "slow-echo" tool that sleeps.
func newSlowEchoMCPServer(delay time.Duration) *server.MCPServer {
	s := server.NewMCPServer("test-agent", "0.1.0")
	s.AddTool(
		mcp.NewTool("slow-echo",
			mcp.WithDescription("Echoes back the input message after a delay"),
			mcp.WithString("message",
				mcp.Required(),
				mcp.Description("Message to echo"),
			),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			time.Sleep(delay)
			msg := request.GetArguments()["message"]
			return mcp.NewToolResultText(fmt.Sprintf("echo: %v", msg)), nil
		},
	)
	return s
}

func testYamuxConfig() tunnel.YamuxConfig {
	return tunnel.YamuxConfig{
		KeepAliveInterval:      15 * time.Second,
		ConnectionWriteTimeout: 10 * time.Second,
	}
}

func testHeartbeatConfig() tunnel.HeartbeatConfig {
	return tunnel.HeartbeatConfig{
		Interval:      100 * time.Millisecond,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	}
}

func testAgentTunnelConfig() tunnel.AgentTunnelConfig {
	return tunnel.AgentTunnelConfig{
		SessionTTL:             10 * time.Minute,
		SessionCleanupInterval: 1 * time.Minute,
		InitialBackoff:         1 * time.Second,
		MaxBackoff:             30 * time.Second,
		ForceCloseTimeout:      30 * time.Second,
		Yamux:                  testYamuxConfig(),
	}
}

func testServerTunnelConfig(hbCfg tunnel.HeartbeatConfig) tunnel.ServerTunnelConfig {
	return tunnel.ServerTunnelConfig{
		Heartbeat:        hbCfg,
		HandshakeTimeout: 10 * time.Second,
		Yamux:            testYamuxConfig(),
	}
}

// setupRegistry creates a TunnelRegistry + agent for testing with multi-tenant flow.
func setupRegistry(t *testing.T, agentMCP *server.MCPServer, hbCfg tunnel.HeartbeatConfig) (*agent.Agent, *TunnelRegistry, *httptest.Server) {
	t.Helper()
	logger := newTestLogger(t)
	apiKey := "test-secret"

	srvCfg := testServerTunnelConfig(hbCfg)
	registry := NewTunnelRegistry(newStaticAuth(apiKey), srvCfg, "0.1.0", logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", registry.HandleTunnel)
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(httpSrv.Close)

	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"
	ag := agent.NewAgent(tunnelURL, apiKey, "0.1.0", agentMCP, testAgentTunnelConfig(), logger)

	return ag, registry, httpSrv
}

// setupRegistryWithAgentConfig is like setupRegistry but accepts a custom AgentTunnelConfig.
func setupRegistryWithAgentConfig(t *testing.T, agentMCP *server.MCPServer, hbCfg tunnel.HeartbeatConfig, agentCfg tunnel.AgentTunnelConfig) (*agent.Agent, *TunnelRegistry, *httptest.Server) {
	t.Helper()
	logger := newTestLogger(t)
	apiKey := "test-secret"

	srvCfg := testServerTunnelConfig(hbCfg)
	registry := NewTunnelRegistry(newStaticAuth(apiKey), srvCfg, "0.1.0", logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", registry.HandleTunnel)
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(httpSrv.Close)

	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"
	ag := agent.NewAgent(tunnelURL, apiKey, "0.1.0", agentMCP, agentCfg, logger)

	return ag, registry, httpSrv
}

// forwardCallViaRegistry sends a JSON-RPC request through the registry's MCPServer
// for the static database ID.
func forwardCallViaRegistry(t *testing.T, ctx context.Context, registry *TunnelRegistry, sessionID string, payload json.RawMessage) (json.RawMessage, error) {
	t.Helper()
	// Get the per-database MCPServer and use its tool handler.
	entry := registry.tunnels[testDatabaseID]
	if entry == nil {
		return nil, ErrNoTunnel
	}
	return entry.proxy.forwardCall(ctx, sessionID, payload)
}

// TestTunnelEndToEnd tests the full flow using TunnelRegistry.
func TestTunnelEndToEnd(t *testing.T) {
	agentMCP := newAgentMCPServer()
	ag, registry, _ := setupRegistry(t, agentMCP, testHeartbeatConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agentErr := make(chan error, 1)
	go func() { agentErr <- ag.Run(ctx) }()

	// Wait for agent to connect and registry to have the tunnel.
	require.Eventually(t, func() bool {
		return registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond, "agent should connect")

	// Verify the per-database MCPServer has the "echo" tool.
	mcpSrv := registry.GetMCPServer(testDatabaseID)
	require.NotNil(t, mcpSrv)
	tool := mcpSrv.GetTool("echo")
	require.NotNil(t, tool, "echo tool should be registered")
	assert.Equal(t, "echo", tool.Tool.Name)

	// Forward a tool call through the per-database MCPServer.
	session := server.NewInProcessSession("test-session", nil)
	require.NoError(t, mcpSrv.RegisterSession(ctx, session))
	sessionCtx := mcpSrv.WithContext(ctx, session)

	// Initialize session.
	initBytes, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "init-1", "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test-client", "version": "1.0"},
		},
	})
	initResult := mcpSrv.HandleMessage(sessionCtx, initBytes)
	initJSON, _ := json.Marshal(initResult)
	assert.Contains(t, string(initJSON), "serverInfo")

	// Call the echo tool.
	reqBytes, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "test-1", "method": "tools/call",
		"params": map[string]any{
			"name":      "echo",
			"arguments": map[string]any{"message": "hello tunnel"},
		},
	})
	result := mcpSrv.HandleMessage(sessionCtx, reqBytes)
	resultJSON, _ := json.Marshal(result)

	var rpcResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct{ Message string } `json:"error,omitempty"`
	}
	require.NoError(t, json.Unmarshal(resultJSON, &rpcResp))
	require.Nil(t, rpcResp.Error)
	require.Len(t, rpcResp.Result.Content, 1)
	assert.Equal(t, "echo: hello tunnel", rpcResp.Result.Content[0].Text)

	// Disconnect agent.
	cancel()
	select {
	case <-agentErr:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not stop in time")
	}

	require.Eventually(t, func() bool {
		return !registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond, "registry should be empty after disconnect")
}

// TestTunnelAuthRejected verifies that an agent with a bad API key is rejected.
func TestTunnelAuthRejected(t *testing.T) {
	logger := newTestLogger(t)

	registry := NewTunnelRegistry(newStaticAuth("correct-key"), testServerTunnelConfig(testHeartbeatConfig()), "0.1.0", logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", registry.HandleTunnel)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"
	agentMCP := newAgentMCPServer()
	ag := agent.NewAgent(tunnelURL, "wrong-key", "0.1.0", agentMCP, testAgentTunnelConfig(), logger)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := ag.ConnectAndServe(ctx)
	require.Error(t, err)
	assert.False(t, registry.AnyConnected())
}

// TestTunnelMultipleConcurrentCalls verifies concurrent tool calls through TunnelRegistry.
func TestTunnelMultipleConcurrentCalls(t *testing.T) {
	agentMCP := newAgentMCPServer()
	ag, registry, _ := setupRegistry(t, agentMCP, testHeartbeatConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond)

	mcpSrv := registry.GetMCPServer(testDatabaseID)
	require.NotNil(t, mcpSrv)

	session := server.NewInProcessSession("concurrent-session", nil)
	require.NoError(t, mcpSrv.RegisterSession(ctx, session))
	sessionCtx := mcpSrv.WithContext(ctx, session)

	initBytes, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "init", "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})
	mcpSrv.HandleMessage(sessionCtx, initBytes)

	const n = 10
	type result struct {
		idx  int
		text string
		err  error
	}
	results := make(chan result, n)

	for i := range n {
		go func(idx int) {
			msg := fmt.Sprintf("msg-%d", idx)
			req := map[string]any{
				"jsonrpc": "2.0", "id": fmt.Sprintf("call-%d", idx), "method": "tools/call",
				"params": map[string]any{
					"name":      "echo",
					"arguments": map[string]any{"message": msg},
				},
			}
			reqBytes, _ := json.Marshal(req)
			resp := mcpSrv.HandleMessage(sessionCtx, reqBytes)
			respBytes, _ := json.Marshal(resp)

			var rpcResp struct {
				Result struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"result"`
			}
			if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
				results <- result{idx: idx, err: err}
				return
			}
			if len(rpcResp.Result.Content) == 0 {
				results <- result{idx: idx, err: fmt.Errorf("no content in response: %s", string(respBytes))}
				return
			}
			results <- result{idx: idx, text: rpcResp.Result.Content[0].Text}
		}(i)
	}

	seen := make(map[string]bool)
	for range n {
		select {
		case r := <-results:
			require.NoError(t, r.err, "call %d failed", r.idx)
			expected := fmt.Sprintf("echo: msg-%d", r.idx)
			assert.Equal(t, expected, r.text)
			seen[r.text] = true
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for concurrent results")
		}
	}
	assert.Len(t, seen, n)
	cancel()
}

func TestNewYamuxConfig(t *testing.T) {
	cfg := tunnel.NewYamuxConfig(tunnel.YamuxConfig{
		KeepAliveInterval:      15 * time.Second,
		ConnectionWriteTimeout: 10 * time.Second,
	})
	assert.True(t, cfg.EnableKeepAlive)
	assert.Equal(t, 15*time.Second, cfg.KeepAliveInterval)
	assert.Equal(t, 10*time.Second, cfg.ConnectionWriteTimeout)
}

// TestSessionTTLEviction verifies that idle sessions are evicted after the TTL expires.
func TestSessionTTLEviction(t *testing.T) {
	agentMCP := newAgentMCPServer()
	agentCfg := testAgentTunnelConfig()
	agentCfg.SessionTTL = 50 * time.Millisecond

	ag, registry, _ := setupRegistryWithAgentConfig(t, agentMCP, tunnel.HeartbeatConfig{
		Interval:      10 * time.Second,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	}, agentCfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond)

	// Forward a call to create a session on the agent.
	registry.mu.RLock()
	entry := registry.tunnels[testDatabaseID]
	registry.mu.RUnlock()
	require.NotNil(t, entry)

	_, err := entry.proxy.forwardCall(ctx, "ttl-sess", json.RawMessage(
		`{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"echo","arguments":{"message":"hi"}}}`,
	))
	require.NoError(t, err)

	assert.True(t, ag.HasSession("ttl-sess"))

	time.Sleep(100 * time.Millisecond)
	ag.CleanStaleSessions(ctx)
	assert.Equal(t, 0, ag.SessionCount())
	cancel()
}

// TestSessionClearedOnReconnect verifies sessions are cleared on agent reconnect.
func TestSessionClearedOnReconnect(t *testing.T) {
	agentMCP := newAgentMCPServer()
	ag, registry, _ := setupRegistry(t, agentMCP, tunnel.HeartbeatConfig{
		Interval:      10 * time.Second,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond)

	// Forward a call to create a session.
	registry.mu.RLock()
	entry := registry.tunnels[testDatabaseID]
	registry.mu.RUnlock()
	_, err := entry.proxy.forwardCall(ctx, "reconnect-sess", json.RawMessage(
		`{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"echo","arguments":{"message":"hi"}}}`,
	))
	require.NoError(t, err)
	assert.True(t, ag.HasSession("reconnect-sess"))

	cancel()

	require.Eventually(t, func() bool {
		return !registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	ag.SetDraining(false)
	go func() { _ = ag.Run(ctx2) }()

	require.Eventually(t, func() bool {
		return registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond)

	assert.False(t, ag.HasSession("reconnect-sess"))
	cancel2()
}

// TestHeartbeatPingPong verifies heartbeats work via TunnelRegistry.
func TestHeartbeatPingPong(t *testing.T) {
	agentMCP := newAgentMCPServer()
	ag, registry, _ := setupRegistry(t, agentMCP, tunnel.HeartbeatConfig{
		Interval:      100 * time.Millisecond,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	})
	_ = ag

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond)

	time.Sleep(500 * time.Millisecond)
	assert.True(t, registry.AnyConnected())
	cancel()
}

// TestHeartbeatDetectsDeadAgent verifies heartbeat detection via TunnelRegistry.
func TestHeartbeatDetectsDeadAgent(t *testing.T) {
	logger := newTestLogger(t)
	apiKey := "test-secret"

	registry := NewTunnelRegistry(newStaticAuth(apiKey), testServerTunnelConfig(tunnel.HeartbeatConfig{
		Interval:      50 * time.Millisecond,
		Timeout:       50 * time.Millisecond,
		MissThreshold: 2,
	}), "0.1.0", logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", registry.HandleTunnel)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"
	agentMCP := newAgentMCPServer()
	ag := agent.NewAgent(tunnelURL, apiKey, "0.1.0", agentMCP, testAgentTunnelConfig(), logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agentErr := make(chan error, 1)
	go func() { agentErr <- ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond)

	cancel()
	select {
	case <-agentErr:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not stop")
	}

	require.Eventually(t, func() bool {
		return !registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond)
}

// TestGracefulShutdownDrainsHandlers verifies in-flight calls complete during shutdown.
func TestGracefulShutdownDrainsHandlers(t *testing.T) {
	agentMCP := newSlowEchoMCPServer(200 * time.Millisecond)
	ag, registry, _ := setupRegistry(t, agentMCP, tunnel.HeartbeatConfig{
		Interval:      10 * time.Second,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond)

	registry.mu.RLock()
	entry := registry.tunnels[testDatabaseID]
	registry.mu.RUnlock()

	callDone := make(chan error, 1)
	go func() {
		_, err := entry.proxy.forwardCall(context.Background(), "drain-sess", json.RawMessage(
			`{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"slow-echo","arguments":{"message":"hello"}}}`,
		))
		callDone <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	assert.NoError(t, ag.Shutdown(shutdownCtx))

	select {
	case err := <-callDone:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight call did not complete")
	}
}

// TestGracefulShutdownTimeout verifies Shutdown returns context.DeadlineExceeded when drain takes too long.
func TestGracefulShutdownTimeout(t *testing.T) {
	agentMCP := newSlowEchoMCPServer(500 * time.Millisecond)
	ag, registry, _ := setupRegistry(t, agentMCP, tunnel.HeartbeatConfig{
		Interval:      10 * time.Second,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond)

	registry.mu.RLock()
	entry := registry.tunnels[testDatabaseID]
	registry.mu.RUnlock()

	go func() {
		_, _ = entry.proxy.forwardCall(context.Background(), "timeout-sess", json.RawMessage(
			`{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"slow-echo","arguments":{"message":"hello"}}}`,
		))
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer shutdownCancel()
	assert.ErrorIs(t, ag.Shutdown(shutdownCtx), context.DeadlineExceeded)
}

// TestPongReportsDraining verifies pong responses include Draining=true after agent starts draining.
func TestPongReportsDraining(t *testing.T) {
	agentMCP := newAgentMCPServer()
	ag, registry, _ := setupRegistry(t, agentMCP, tunnel.HeartbeatConfig{
		Interval:      10 * time.Second,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond)

	registry.mu.RLock()
	entry := registry.tunnels[testDatabaseID]
	registry.mu.RUnlock()

	_, draining, err := registry.sendPing(entry.yamuxSession)
	require.NoError(t, err)
	assert.False(t, draining)

	ag.SetDraining(true)

	_, draining, err = registry.sendPing(entry.yamuxSession)
	require.NoError(t, err)
	assert.True(t, draining)

	cancel()
}

// TestHeartbeatConcurrentWithCalls verifies heartbeats and tool calls run concurrently.
func TestHeartbeatConcurrentWithCalls(t *testing.T) {
	agentMCP := newAgentMCPServer()
	ag, registry, _ := setupRegistry(t, agentMCP, tunnel.HeartbeatConfig{
		Interval:      100 * time.Millisecond,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	})
	_ = ag

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond)

	registry.mu.RLock()
	entry := registry.tunnels[testDatabaseID]
	registry.mu.RUnlock()

	const n = 5
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			payload := json.RawMessage(fmt.Sprintf(
				`{"jsonrpc":"2.0","id":"c-%d","method":"tools/call","params":{"name":"echo","arguments":{"message":"msg-%d"}}}`,
				idx, idx,
			))
			_, err := entry.proxy.forwardCall(ctx, fmt.Sprintf("sess-%d", idx), payload)
			if err != nil {
				errs <- fmt.Errorf("call %d: %w", idx, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent call failed: %v", err)
	}

	assert.True(t, registry.AnyConnected())
	cancel()
}

// TestHandshakeSuccess verifies handshake + tool discovery via TunnelRegistry.
func TestHandshakeSuccess(t *testing.T) {
	agentMCP := newAgentMCPServer()
	ag, registry, _ := setupRegistry(t, agentMCP, testHeartbeatConfig())
	_ = ag

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return registry.AnyConnected()
	}, 5*time.Second, 50*time.Millisecond)

	mcpSrv := registry.GetMCPServer(testDatabaseID)
	require.NotNil(t, mcpSrv)
	tool := mcpSrv.GetTool("echo")
	require.NotNil(t, tool)

	cancel()
}

// TestHandshakeVersionMismatch verifies protocol version mismatch.
func TestHandshakeVersionMismatch(t *testing.T) {
	logger := newTestLogger(t)
	apiKey := "test-secret"

	mux := http.NewServeMux()
	disconnected := make(chan struct{})
	mux.HandleFunc("/tunnel", func(w http.ResponseWriter, r *http.Request) {
		defer close(disconnected)

		wsConn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("websocket accept: %v", err)
			return
		}
		defer wsConn.CloseNow() //nolint:errcheck

		netConn := websocket.NetConn(r.Context(), wsConn, websocket.MessageBinary)

		yamuxCfg := yamux.DefaultConfig()
		yamuxCfg.LogOutput = io.Discard
		session, err := yamux.Client(netConn, yamuxCfg)
		if err != nil {
			t.Errorf("yamux client: %v", err)
			return
		}
		defer session.Close() //nolint:errcheck

		stream, err := session.Open()
		if err != nil {
			t.Errorf("open stream: %v", err)
			return
		}
		defer stream.Close() //nolint:errcheck

		h := &tunnel.Handshake{
			ProtocolVersion: 99,
			ServerVersion:   "0.1.0",
		}
		if err := tunnel.WriteHandshake(stream, h); err != nil {
			t.Errorf("write handshake: %v", err)
			return
		}

		ack, err := tunnel.ReadHandshakeAck(stream)
		if err != nil {
			t.Errorf("read handshake ack: %v", err)
			return
		}

		assert.NotEmpty(t, ack.Error)
		assert.Contains(t, ack.Error, "incompatible protocol version")
	})

	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"
	agentMCP := newAgentMCPServer()
	ag := agent.NewAgent(tunnelURL, apiKey, "0.1.0", agentMCP, testAgentTunnelConfig(), logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() { _ = ag.ConnectAndServe(ctx) }()

	select {
	case <-disconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for version mismatch test")
	}
}

// TestHandshakeTimeout verifies handshake timeout.
func TestHandshakeTimeout(t *testing.T) {
	logger := newTestLogger(t)
	apiKey := "test-secret"

	serverCfg := testServerTunnelConfig(testHeartbeatConfig())
	handshakeTimeout := serverCfg.HandshakeTimeout

	registry := NewTunnelRegistry(newStaticAuth(apiKey), serverCfg, "0.1.0", logger)

	handshakeFailed := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", registry.HandleTunnel)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"

	go func() {
		defer close(handshakeFailed)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		wsConn, _, err := websocket.Dial(ctx, tunnelURL, &websocket.DialOptions{
			HTTPHeader: http.Header{
				"Authorization": []string{"Bearer " + apiKey},
			},
		})
		if err != nil {
			t.Errorf("websocket dial: %v", err)
			return
		}
		defer wsConn.CloseNow() //nolint:errcheck

		netConn := websocket.NetConn(ctx, wsConn, websocket.MessageBinary)
		yamuxCfg := yamux.DefaultConfig()
		yamuxCfg.LogOutput = io.Discard
		session, err := yamux.Server(netConn, yamuxCfg)
		if err != nil {
			t.Errorf("yamux server: %v", err)
			return
		}
		defer session.Close() //nolint:errcheck

		stream, err := session.Accept()
		if err != nil {
			return
		}
		defer stream.Close() //nolint:errcheck

		<-ctx.Done()
	}()

	time.Sleep(handshakeTimeout + 2*time.Second)
	assert.False(t, registry.AnyConnected())

	select {
	case <-handshakeFailed:
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for handshake timeout test")
	}
}

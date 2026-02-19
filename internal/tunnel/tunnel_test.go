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
	"github.com/guillermoBallester/isthmus/pkg/tunnel"
	"github.com/guillermoBallester/isthmus/pkg/tunnel/agent"
	"github.com/hashicorp/yamux"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newStaticAuth creates a simple in-memory authenticator for tests.
func newStaticAuth(keys ...string) Authenticator {
	return &staticTestAuth{keys: keys}
}

type staticTestAuth struct {
	keys []string
}

func (a *staticTestAuth) Authenticate(_ context.Context, token string) (bool, error) {
	for _, k := range a.keys {
		if k == token {
			return true, nil
		}
	}
	return false, nil
}

// newTestLogger returns a logger that writes to testing.T.
// Logging is silently dropped after the test finishes to avoid data races.
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
	// Skip logging after the test has finished to avoid a data race
	// (e.g., HandleTunnel logs "agent disconnected" in its HTTP goroutine).
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

// testYamuxConfig returns a YamuxConfig for tests.
func testYamuxConfig() tunnel.YamuxConfig {
	return tunnel.YamuxConfig{
		KeepAliveInterval:      15 * time.Second,
		ConnectionWriteTimeout: 10 * time.Second,
	}
}

// testHeartbeatConfig returns a fast heartbeat config for tests.
func testHeartbeatConfig() tunnel.HeartbeatConfig {
	return tunnel.HeartbeatConfig{
		Interval:      100 * time.Millisecond,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	}
}

// testAgentTunnelConfig returns an AgentTunnelConfig with sensible test defaults.
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

// testServerTunnelConfig returns a ServerTunnelConfig with the given heartbeat config.
func testServerTunnelConfig(hbCfg tunnel.HeartbeatConfig) tunnel.ServerTunnelConfig {
	return tunnel.ServerTunnelConfig{
		Heartbeat:        hbCfg,
		HandshakeTimeout: 10 * time.Second,
		Yamux:            testYamuxConfig(),
	}
}

// setupTunnel creates an agent+server tunnel pair for testing.
// Returns the agent, tunnelSrv, proxy, httpSrv, and the tunnel URL.
func setupTunnel(t *testing.T, agentMCP *server.MCPServer, hbCfg tunnel.HeartbeatConfig) (*agent.Agent, *TunnelServer, *Proxy, *httptest.Server) {
	t.Helper()
	logger := newTestLogger(t)
	apiKey := "test-secret"

	cloudMCP := server.NewMCPServer("test-cloud", "0.1.0",
		server.WithToolCapabilities(true),
	)
	tunnelSrv := NewTunnelServer(newStaticAuth(apiKey), testServerTunnelConfig(hbCfg), "0.1.0", logger)
	proxy := NewProxy(tunnelSrv, cloudMCP, testServerTunnelConfig(hbCfg), logger)
	proxy.Setup()

	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", tunnelSrv.HandleTunnel)
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(httpSrv.Close)

	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"
	ag := agent.NewAgent(tunnelURL, apiKey, "0.1.0", agentMCP, testAgentTunnelConfig(), logger)

	return ag, tunnelSrv, proxy, httpSrv
}

// TestTunnelEndToEnd tests the full flow:
// 1. Start HTTP server with tunnel endpoint
// 2. Agent connects via WebSocket
// 3. Proxy discovers the "echo" tool
// 4. Forward a tools/call through the tunnel
// 5. Verify the response
// 6. Agent disconnects, tools are removed
func TestTunnelEndToEnd(t *testing.T) {
	logger := newTestLogger(t)
	apiKey := "test-secret"

	// --- Cloud side ---
	cloudMCP := server.NewMCPServer("test-cloud", "0.1.0",
		server.WithToolCapabilities(true),
	)
	srvCfg := testServerTunnelConfig(testHeartbeatConfig())
	tunnelSrv := NewTunnelServer(newStaticAuth(apiKey), srvCfg, "0.1.0", logger)
	proxy := NewProxy(tunnelSrv, cloudMCP, srvCfg, logger)
	proxy.Setup()

	// HTTP test server with the /tunnel endpoint.
	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", tunnelSrv.HandleTunnel)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	// Convert http:// to ws:// for the agent.
	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"

	// --- Agent side ---
	agentMCP := newAgentMCPServer()
	ag := agent.NewAgent(tunnelURL, apiKey, "0.1.0", agentMCP, testAgentTunnelConfig(), logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run agent in background.
	agentErr := make(chan error, 1)
	go func() {
		agentErr <- ag.Run(ctx)
	}()

	// Wait for the agent to connect and proxy to discover tools.
	require.Eventually(t, func() bool {
		return tunnelSrv.Connected()
	}, 5*time.Second, 50*time.Millisecond, "agent should connect")

	// Give the proxy time to run tool discovery (onConnect callback).
	require.Eventually(t, func() bool {
		return len(proxy.ToolNames()) > 0
	}, 5*time.Second, 50*time.Millisecond, "proxy should discover tools")

	// Verify the "echo" tool was registered on the cloud MCPServer.
	tool := cloudMCP.GetTool("echo")
	require.NotNil(t, tool, "echo tool should be registered on cloud MCPServer")
	assert.Equal(t, "echo", tool.Tool.Name)

	// --- Forward a tool call through the tunnel ---
	toolCallReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      "test-1",
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "echo",
			"arguments": map[string]any{"message": "hello tunnel"},
		},
	}
	reqBytes, err := json.Marshal(toolCallReq)
	require.NoError(t, err)

	// We need a session to call HandleMessage on the cloud MCPServer.
	// First, send initialize to set up the session.
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      "init-1",
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test-client", "version": "1.0"},
		},
	}
	initBytes, err := json.Marshal(initReq)
	require.NoError(t, err)

	session := server.NewInProcessSession("test-session", nil)
	err = cloudMCP.RegisterSession(ctx, session)
	require.NoError(t, err)

	sessionCtx := cloudMCP.WithContext(ctx, session)

	// Initialize the session.
	initResult := cloudMCP.HandleMessage(sessionCtx, initBytes)
	initJSON, err := json.Marshal(initResult)
	require.NoError(t, err)
	assert.Contains(t, string(initJSON), "serverInfo")

	// Now call the echo tool.
	result := cloudMCP.HandleMessage(sessionCtx, reqBytes)

	resultJSON, err := json.Marshal(result)
	require.NoError(t, err)

	// Parse the JSON-RPC response.
	var rpcResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	err = json.Unmarshal(resultJSON, &rpcResp)
	require.NoError(t, err)
	require.Nil(t, rpcResp.Error, "expected no JSON-RPC error, got: %s", string(resultJSON))
	require.Len(t, rpcResp.Result.Content, 1)
	assert.Equal(t, "text", rpcResp.Result.Content[0].Type)
	assert.Equal(t, "echo: hello tunnel", rpcResp.Result.Content[0].Text)

	// --- Disconnect: cancel agent, verify tools are removed ---
	cancel()

	// Wait for agent to stop.
	select {
	case <-agentErr:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not stop in time")
	}

	// The agent disconnecting should trigger onDisconnect which removes tools.
	require.Eventually(t, func() bool {
		return cloudMCP.GetTool("echo") == nil
	}, 5*time.Second, 50*time.Millisecond, "echo tool should be removed after disconnect")
}

// TestTunnelAuthRejected verifies that an agent with a bad API key is rejected.
func TestTunnelAuthRejected(t *testing.T) {
	logger := newTestLogger(t)

	tunnelSrv := NewTunnelServer(newStaticAuth("correct-key"), testServerTunnelConfig(testHeartbeatConfig()), "0.1.0", logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", tunnelSrv.HandleTunnel)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"

	agentMCP := newAgentMCPServer()
	ag := agent.NewAgent(tunnelURL, "wrong-key", "0.1.0", agentMCP, testAgentTunnelConfig(), logger)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// ConnectAndServe should fail because auth is rejected.
	err := ag.ConnectAndServe(ctx)
	require.Error(t, err)
	assert.False(t, tunnelSrv.Connected())
}

// TestForwardCallNoAgent verifies that ForwardCall returns ErrNoAgent
// when no agent is connected.
func TestForwardCallNoAgent(t *testing.T) {
	logger := newTestLogger(t)
	tunnelSrv := NewTunnelServer(newStaticAuth("key"), testServerTunnelConfig(testHeartbeatConfig()), "0.1.0", logger)

	_, err := tunnelSrv.ForwardCall(context.Background(), "sess", json.RawMessage(`{}`))
	require.ErrorIs(t, err, ErrNoAgent)
}

// TestTunnelMultipleConcurrentCalls verifies that multiple tool calls
// can be forwarded concurrently through the tunnel.
func TestTunnelMultipleConcurrentCalls(t *testing.T) {
	logger := newTestLogger(t)
	apiKey := "test-key"

	cloudMCP := server.NewMCPServer("test-cloud", "0.1.0",
		server.WithToolCapabilities(true),
	)
	srvCfg := testServerTunnelConfig(testHeartbeatConfig())
	tunnelSrv := NewTunnelServer(newStaticAuth(apiKey), srvCfg, "0.1.0", logger)
	proxy := NewProxy(tunnelSrv, cloudMCP, srvCfg, logger)
	proxy.Setup()

	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", tunnelSrv.HandleTunnel)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"

	agentMCP := newAgentMCPServer()
	ag := agent.NewAgent(tunnelURL, apiKey, "0.1.0", agentMCP, testAgentTunnelConfig(), logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = ag.Run(ctx) }()

	// Wait for tool discovery.
	require.Eventually(t, func() bool {
		return len(proxy.ToolNames()) > 0
	}, 5*time.Second, 50*time.Millisecond)

	// Set up a session for the cloud MCPServer.
	session := server.NewInProcessSession("concurrent-session", nil)
	require.NoError(t, cloudMCP.RegisterSession(ctx, session))
	sessionCtx := cloudMCP.WithContext(ctx, session)

	// Initialize session.
	initBytes, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "init", "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})
	cloudMCP.HandleMessage(sessionCtx, initBytes)

	// Fire 10 concurrent tool calls.
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
				"jsonrpc": "2.0",
				"id":      fmt.Sprintf("call-%d", idx),
				"method":  "tools/call",
				"params": map[string]any{
					"name":      "echo",
					"arguments": map[string]any{"message": msg},
				},
			}
			reqBytes, _ := json.Marshal(req)
			resp := cloudMCP.HandleMessage(sessionCtx, reqBytes)

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
			assert.Equal(t, expected, r.text, "call %d returned wrong text", r.idx)
			seen[r.text] = true
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for concurrent results")
		}
	}
	assert.Len(t, seen, n, "all calls should return unique results")

	cancel()
}

// --- New heartbeat and graceful shutdown tests ---

// TestNewYamuxConfig verifies that the shared yamux config helper returns expected values.
func TestNewYamuxConfig(t *testing.T) {
	cfg := tunnel.NewYamuxConfig(tunnel.YamuxConfig{
		KeepAliveInterval:      15 * time.Second,
		ConnectionWriteTimeout: 10 * time.Second,
	})
	assert.True(t, cfg.EnableKeepAlive, "EnableKeepAlive should be true")
	assert.Equal(t, 15*time.Second, cfg.KeepAliveInterval, "KeepAliveInterval should be 15s")
	assert.Equal(t, 10*time.Second, cfg.ConnectionWriteTimeout, "ConnectionWriteTimeout should be 10s")
}

// TestSessionTTLEviction verifies that idle sessions are evicted after the TTL expires.
func TestSessionTTLEviction(t *testing.T) {
	agentMCP := newAgentMCPServer()
	agentCfg := testAgentTunnelConfig()
	agentCfg.SessionTTL = 50 * time.Millisecond

	ag, tunnelSrv, _, _ := setupTunnelWithAgentConfig(t, agentMCP, tunnel.HeartbeatConfig{
		Interval:      10 * time.Second,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	}, agentCfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return tunnelSrv.Connected()
	}, 5*time.Second, 50*time.Millisecond)

	// Forward a call to create a session on the agent.
	_, err := tunnelSrv.ForwardCall(ctx, "ttl-sess", json.RawMessage(
		`{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"echo","arguments":{"message":"hi"}}}`,
	))
	require.NoError(t, err)

	// Session should exist (along with the "discovery" session from proxy).
	assert.True(t, ag.HasSession("ttl-sess"), "ttl-sess session should exist after call")

	// Wait for TTL to expire.
	time.Sleep(100 * time.Millisecond)

	// Manually trigger cleanup.
	ag.CleanStaleSessions(ctx)

	// All sessions should be evicted (both ttl-sess and discovery).
	assert.Equal(t, 0, ag.SessionCount(), "all sessions should be evicted after TTL")

	cancel()
}

// setupTunnelWithAgentConfig is like setupTunnel but accepts a custom AgentTunnelConfig.
func setupTunnelWithAgentConfig(t *testing.T, agentMCP *server.MCPServer, hbCfg tunnel.HeartbeatConfig, agentCfg tunnel.AgentTunnelConfig) (*agent.Agent, *TunnelServer, *Proxy, *httptest.Server) {
	t.Helper()
	logger := newTestLogger(t)
	apiKey := "test-secret"

	cloudMCP := server.NewMCPServer("test-cloud", "0.1.0",
		server.WithToolCapabilities(true),
	)
	srvCfg := testServerTunnelConfig(hbCfg)
	tunnelSrv := NewTunnelServer(newStaticAuth(apiKey), srvCfg, "0.1.0", logger)
	proxy := NewProxy(tunnelSrv, cloudMCP, srvCfg, logger)
	proxy.Setup()

	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", tunnelSrv.HandleTunnel)
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(httpSrv.Close)

	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"
	ag := agent.NewAgent(tunnelURL, apiKey, "0.1.0", agentMCP, agentCfg, logger)

	return ag, tunnelSrv, proxy, httpSrv
}

// TestSessionClearedOnReconnect verifies that sessions are cleared when the agent reconnects.
func TestSessionClearedOnReconnect(t *testing.T) {
	agentMCP := newAgentMCPServer()
	ag, tunnelSrv, _, _ := setupTunnel(t, agentMCP, tunnel.HeartbeatConfig{
		Interval:      10 * time.Second,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())

	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return tunnelSrv.Connected()
	}, 5*time.Second, 50*time.Millisecond)

	// Forward a call to create a session.
	_, err := tunnelSrv.ForwardCall(ctx, "reconnect-sess", json.RawMessage(
		`{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"echo","arguments":{"message":"hi"}}}`,
	))
	require.NoError(t, err)

	assert.True(t, ag.HasSession("reconnect-sess"), "reconnect-sess session should exist after call")

	// Disconnect by cancelling context.
	cancel()

	// Wait for disconnection.
	require.Eventually(t, func() bool {
		return !tunnelSrv.Connected()
	}, 5*time.Second, 50*time.Millisecond, "agent should disconnect")

	// Reconnect with a new context.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	// Reset draining so agent can reconnect.
	ag.SetDraining(false)
	go func() { _ = ag.Run(ctx2) }()

	require.Eventually(t, func() bool {
		return tunnelSrv.Connected()
	}, 5*time.Second, 50*time.Millisecond, "agent should reconnect")

	// The old "reconnect-sess" session should be gone (cleared on reconnect).
	// A new "discovery" session may exist from the proxy's tool discovery callback.
	assert.False(t, ag.HasSession("reconnect-sess"), "old session should be cleared after reconnect")

	cancel2()
}

// TestHeartbeatPingPong verifies the server sends heartbeat pings and receives pongs.
func TestHeartbeatPingPong(t *testing.T) {
	agentMCP := newAgentMCPServer()
	ag, tunnelSrv, _, _ := setupTunnel(t, agentMCP, tunnel.HeartbeatConfig{
		Interval:      100 * time.Millisecond,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return tunnelSrv.Connected()
	}, 5*time.Second, 50*time.Millisecond, "agent should connect")

	// Wait for several heartbeat intervals to verify pings succeed.
	time.Sleep(500 * time.Millisecond)

	// Agent should still be connected (heartbeats are succeeding).
	assert.True(t, tunnelSrv.Connected(), "agent should remain connected during heartbeat")

	cancel()
}

// TestHeartbeatDetectsDeadAgent verifies the server detects a dead agent via heartbeat.
func TestHeartbeatDetectsDeadAgent(t *testing.T) {
	logger := newTestLogger(t)
	apiKey := "test-secret"

	tunnelSrv := NewTunnelServer(newStaticAuth(apiKey), testServerTunnelConfig(tunnel.HeartbeatConfig{
		Interval:      50 * time.Millisecond,
		Timeout:       50 * time.Millisecond,
		MissThreshold: 2,
	}), "0.1.0", logger)

	disconnected := make(chan struct{})
	tunnelSrv.SetOnDisconnect(func() {
		close(disconnected)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", tunnelSrv.HandleTunnel)
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
		return tunnelSrv.Connected()
	}, 5*time.Second, 50*time.Millisecond)

	// Forcibly cancel the agent context to simulate death.
	cancel()

	// Wait for agent goroutine to exit.
	select {
	case <-agentErr:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not stop")
	}

	// Server should detect the dead agent via heartbeat or yamux close.
	select {
	case <-disconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not detect dead agent")
	}

	assert.False(t, tunnelSrv.Connected())
}

// TestGracefulShutdownDrainsHandlers verifies that in-flight calls complete during shutdown.
func TestGracefulShutdownDrainsHandlers(t *testing.T) {
	agentMCP := newSlowEchoMCPServer(200 * time.Millisecond)
	ag, tunnelSrv, _, _ := setupTunnel(t, agentMCP, tunnel.HeartbeatConfig{
		Interval:      10 * time.Second, // Effectively disable heartbeat for this test.
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return tunnelSrv.Connected()
	}, 5*time.Second, 50*time.Millisecond)

	// Send a slow call.
	callDone := make(chan error, 1)
	go func() {
		_, err := tunnelSrv.ForwardCall(context.Background(), "drain-sess", json.RawMessage(
			`{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"slow-echo","arguments":{"message":"hello"}}}`,
		))
		callDone <- err
	}()

	// Give the call time to start processing.
	time.Sleep(50 * time.Millisecond)

	// Cancel the agent context and initiate graceful shutdown.
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	err := ag.Shutdown(shutdownCtx)
	assert.NoError(t, err, "Shutdown should complete without error")

	// The in-flight call should have completed.
	select {
	case err := <-callDone:
		assert.NoError(t, err, "in-flight call should complete successfully")
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight call did not complete")
	}
}

// TestGracefulShutdownTimeout verifies that Shutdown returns context.DeadlineExceeded
// when drain takes too long.
func TestGracefulShutdownTimeout(t *testing.T) {
	agentMCP := newSlowEchoMCPServer(500 * time.Millisecond)
	ag, tunnelSrv, _, _ := setupTunnel(t, agentMCP, tunnel.HeartbeatConfig{
		Interval:      10 * time.Second,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return tunnelSrv.Connected()
	}, 5*time.Second, 50*time.Millisecond)

	// Send a slow call.
	go func() {
		_, _ = tunnelSrv.ForwardCall(context.Background(), "timeout-sess", json.RawMessage(
			`{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"slow-echo","arguments":{"message":"hello"}}}`,
		))
	}()

	// Give the call time to start.
	time.Sleep(50 * time.Millisecond)

	cancel()

	// Very short drain timeout — should exceed.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer shutdownCancel()

	err := ag.Shutdown(shutdownCtx)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "Shutdown should time out")
}

// TestPongReportsDraining verifies that pong responses include Draining=true
// after the agent starts draining.
func TestPongReportsDraining(t *testing.T) {
	agentMCP := newAgentMCPServer()
	ag, tunnelSrv, _, _ := setupTunnel(t, agentMCP, tunnel.HeartbeatConfig{
		Interval:      10 * time.Second, // We'll send pings manually.
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return tunnelSrv.Connected()
	}, 5*time.Second, 50*time.Millisecond)

	// Before draining, pong should not report draining.
	tunnelSrv.mu.RLock()
	session := tunnelSrv.yamuxSession
	tunnelSrv.mu.RUnlock()

	_, draining, err := tunnelSrv.sendPing(session)
	require.NoError(t, err)
	assert.False(t, draining, "pong should not report draining before shutdown")

	// Set agent to draining.
	ag.SetDraining(true)

	_, draining, err = tunnelSrv.sendPing(session)
	require.NoError(t, err)
	assert.True(t, draining, "pong should report draining after shutdown initiated")

	cancel()
}

// TestHeartbeatConcurrentWithCalls verifies heartbeats and tool calls can run concurrently.
func TestHeartbeatConcurrentWithCalls(t *testing.T) {
	agentMCP := newAgentMCPServer()
	ag, tunnelSrv, _, _ := setupTunnel(t, agentMCP, tunnel.HeartbeatConfig{
		Interval:      100 * time.Millisecond,
		Timeout:       5 * time.Second,
		MissThreshold: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = ag.Run(ctx) }()

	require.Eventually(t, func() bool {
		return tunnelSrv.Connected()
	}, 5*time.Second, 50*time.Millisecond)

	// Fire 5 tool calls concurrently while heartbeat is running.
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
			_, err := tunnelSrv.ForwardCall(ctx, fmt.Sprintf("sess-%d", idx), payload)
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

	// Heartbeats should still be succeeding.
	assert.True(t, tunnelSrv.Connected(), "agent should remain connected")

	cancel()
}

// --- Handshake tests ---

// TestHandshakeSuccess verifies that the handshake completes successfully
// and tool discovery works after handshake.
func TestHandshakeSuccess(t *testing.T) {
	agentMCP := newAgentMCPServer()
	ag, tunnelSrv, proxy, _ := setupTunnel(t, agentMCP, testHeartbeatConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = ag.Run(ctx) }()

	// Agent should connect and handshake should complete.
	require.Eventually(t, func() bool {
		return tunnelSrv.Connected()
	}, 5*time.Second, 50*time.Millisecond, "agent should connect after handshake")

	// Proxy should discover tools (only happens after successful handshake).
	require.Eventually(t, func() bool {
		return len(proxy.ToolNames()) > 0
	}, 5*time.Second, 50*time.Millisecond, "proxy should discover tools after handshake")

	cancel()
}

// TestHandshakeVersionMismatch verifies that a protocol version mismatch
// results in a rejected connection.
func TestHandshakeVersionMismatch(t *testing.T) {
	logger := newTestLogger(t)
	apiKey := "test-secret"

	// Create a server that will send protocol version 99 (incompatible).
	cloudMCP := server.NewMCPServer("test-cloud", "0.1.0",
		server.WithToolCapabilities(true),
	)

	// We need a custom tunnel server that overrides the protocol version in the handshake.
	// Instead, we'll create a raw HTTP server that does the yamux + handshake manually
	// with a wrong version.
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

		// Send handshake with incompatible protocol version 99.
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

		assert.NotEmpty(t, ack.Error, "handshake ack should contain error for version mismatch")
		assert.Contains(t, ack.Error, "incompatible protocol version")
	})

	_ = cloudMCP // not used in this test, just verifying handshake rejection

	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"
	agentMCP := newAgentMCPServer()
	ag := agent.NewAgent(tunnelURL, apiKey, "0.1.0", agentMCP, testAgentTunnelConfig(), logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run one connection attempt — the agent will connect, handshake will fail
	// on the server side (version mismatch error in ack), and agent continues.
	go func() { _ = ag.ConnectAndServe(ctx) }()

	select {
	case <-disconnected:
		// Server handler completed — version mismatch was detected.
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for version mismatch test to complete")
	}
}

// TestHandshakeTimeout verifies that a handshake times out if the agent
// doesn't respond.
func TestHandshakeTimeout(t *testing.T) {
	logger := newTestLogger(t)
	apiKey := "test-secret"

	// Create server with very short handshake timeout.
	serverCfg := testServerTunnelConfig(testHeartbeatConfig())
	handshakeTimeout := serverCfg.HandshakeTimeout

	tunnelSrv := NewTunnelServer(newStaticAuth(apiKey), serverCfg, "0.1.0", logger)

	handshakeFailed := make(chan struct{})
	originalOnConnect := tunnelSrv.onConnect
	tunnelSrv.SetOnConnect(func() {
		if originalOnConnect != nil {
			originalOnConnect()
		}
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", tunnelSrv.HandleTunnel)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"

	// Create a raw WebSocket client that establishes yamux but never responds to handshake.
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

		// Accept the stream but never read/write — this should cause the server's
		// handshake to time out.
		stream, err := session.Accept()
		if err != nil {
			// Session may be closed by the server after timeout, which is expected.
			return
		}
		defer stream.Close() //nolint:errcheck

		// Just hold the stream open without responding.
		// The server's handshake timeout should fire.
		<-ctx.Done()
	}()

	// The server should NOT become Connected() since handshake times out.
	// Wait a bit longer than the handshake timeout to be sure.
	time.Sleep(handshakeTimeout + 2*time.Second)
	assert.False(t, tunnelSrv.Connected(), "server should not be connected after handshake timeout")

	select {
	case <-handshakeFailed:
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for handshake timeout test")
	}
}

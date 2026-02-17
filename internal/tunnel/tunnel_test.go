package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestLogger returns a logger that writes to testing.T.
func newTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(&testWriter{t}, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

type testWriter struct{ t *testing.T }

func (w *testWriter) Write(p []byte) (n int, err error) {
	// Recover from panic if t.Log is called after test completes
	// (e.g., HandleTunnel logs "agent disconnected" in its HTTP goroutine).
	defer func() {
		if r := recover(); r != nil {
			n = len(p)
		}
	}()
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
	tunnelSrv := NewTunnelServer([]string{apiKey}, logger)
	proxy := NewProxy(tunnelSrv, cloudMCP, logger)
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
	agent := NewAgent(tunnelURL, apiKey, agentMCP, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run agent in background.
	agentErr := make(chan error, 1)
	go func() {
		agentErr <- agent.Run(ctx)
	}()

	// Wait for the agent to connect and proxy to discover tools.
	require.Eventually(t, func() bool {
		return tunnelSrv.Connected()
	}, 5*time.Second, 50*time.Millisecond, "agent should connect")

	// Give the proxy time to run tool discovery (onConnect callback).
	require.Eventually(t, func() bool {
		names, _ := proxy.toolNames.Load().([]string)
		return len(names) > 0
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

	tunnelSrv := NewTunnelServer([]string{"correct-key"}, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", tunnelSrv.HandleTunnel)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"

	agentMCP := newAgentMCPServer()
	agent := NewAgent(tunnelURL, "wrong-key", agentMCP, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// connectAndServe should fail because auth is rejected.
	err := agent.connectAndServe(ctx)
	require.Error(t, err)
	assert.False(t, tunnelSrv.Connected())
}

// TestForwardCallNoAgent verifies that ForwardCall returns ErrNoAgent
// when no agent is connected.
func TestForwardCallNoAgent(t *testing.T) {
	logger := newTestLogger(t)
	tunnelSrv := NewTunnelServer([]string{"key"}, logger)

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
	tunnelSrv := NewTunnelServer([]string{apiKey}, logger)
	proxy := NewProxy(tunnelSrv, cloudMCP, logger)
	proxy.Setup()

	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", tunnelSrv.HandleTunnel)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	tunnelURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/tunnel"

	agentMCP := newAgentMCPServer()
	agent := NewAgent(tunnelURL, apiKey, agentMCP, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = agent.Run(ctx) }()

	// Wait for tool discovery.
	require.Eventually(t, func() bool {
		names, _ := proxy.toolNames.Load().([]string)
		return len(names) > 0
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

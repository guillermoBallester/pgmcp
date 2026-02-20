package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guillermoBallester/isthmus/pkg/tunnel"
	"github.com/hashicorp/yamux"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// jsonRPCRequest is a minimal JSON-RPC 2.0 request envelope for outgoing calls.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCError is the error object in a JSON-RPC 2.0 error response.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// toolsListResponse is the JSON-RPC response for tools/list.
type toolsListResponse struct {
	Result mcp.ListToolsResult `json:"result"`
	Error  *jsonRPCError       `json:"error,omitempty"`
}

// toolCallResponse is the JSON-RPC response for tools/call.
type toolCallResponse struct {
	Result *mcp.CallToolResult `json:"result"`
	Error  *jsonRPCError       `json:"error,omitempty"`
}

const defaultDiscoveryTimeout = 30 * time.Second

// Proxy discovers tools from an agent and registers proxy handlers on a
// per-database MCPServer that forward calls through a yamux session.
type Proxy struct {
	session   *yamux.Session
	mcpServer *server.MCPServer
	cfg       tunnel.ServerTunnelConfig
	logger    *slog.Logger

	mu         sync.Mutex
	toolNames  []string
	reqCounter atomic.Uint64
}

// NewProxy creates a new proxy that bridges a yamux session and an MCPServer.
func NewProxy(session *yamux.Session, mcpServer *server.MCPServer, cfg tunnel.ServerTunnelConfig, logger *slog.Logger) *Proxy {
	return &Proxy{
		session:   session,
		mcpServer: mcpServer,
		cfg:       cfg,
		logger:    logger,
	}
}

// ToolNames returns a copy of the currently registered proxy tool names.
func (p *Proxy) ToolNames() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.toolNames == nil {
		return nil
	}
	out := make([]string, len(p.toolNames))
	copy(out, p.toolNames)
	return out
}

func (p *Proxy) nextRequestID() string {
	return fmt.Sprintf("proxy-%d", p.reqCounter.Add(1))
}

func (p *Proxy) discoveryTimeout() time.Duration {
	if p.cfg.DiscoveryTimeout > 0 {
		return p.cfg.DiscoveryTimeout
	}
	return defaultDiscoveryTimeout
}

// DiscoverAndRegister sends a tools/list request through the tunnel,
// discovers the agent's tools, and registers proxy handlers on the MCPServer.
func (p *Proxy) DiscoverAndRegister() error {
	p.logger.Info("discovering tools from agent")

	// Send a tools/list request through the tunnel.
	listReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      p.nextRequestID(),
		Method:  "tools/list",
	}
	payload, err := json.Marshal(listReq)
	if err != nil {
		return fmt.Errorf("marshal tools/list request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.discoveryTimeout())
	defer cancel()

	respBytes, err := p.forwardCall(ctx, "discovery", payload)
	if err != nil {
		return fmt.Errorf("discover tools: %w", err)
	}

	// Parse the JSON-RPC response to extract tools.
	var rpcResp toolsListResponse
	if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
		return fmt.Errorf("parse tools/list response: %w", err)
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("tools/list error: %s", rpcResp.Error.Message)
	}

	// Register proxy handlers for each discovered tool.
	names := make([]string, 0, len(rpcResp.Result.Tools))
	for _, tool := range rpcResp.Result.Tools {
		p.mcpServer.AddTool(tool, p.makeProxyHandler(tool.Name))
		names = append(names, tool.Name)
	}

	p.mu.Lock()
	p.toolNames = names
	p.mu.Unlock()

	p.logger.Info("registered tools from agent",
		slog.Int("count", len(names)),
		slog.Any("tools", names),
	)

	return nil
}

// Cleanup removes all registered tools from the MCPServer.
func (p *Proxy) Cleanup() {
	p.mu.Lock()
	names := p.toolNames
	p.toolNames = nil
	p.mu.Unlock()

	if len(names) > 0 {
		p.mcpServer.DeleteTools(names...)
		p.logger.Info("removed tools on agent disconnect",
			slog.Int("count", len(names)),
		)
	}
}

// forwardCall sends a JSON-RPC payload through a new yamux stream and returns the response.
func (p *Proxy) forwardCall(ctx context.Context, sessionID string, payload json.RawMessage) (json.RawMessage, error) {
	stream, err := p.session.Open()
	if err != nil {
		return nil, fmt.Errorf("open yamux stream: %w", err)
	}
	defer stream.Close() //nolint:errcheck // best-effort cleanup

	req := tunnel.Request{
		SessionID: sessionID,
		Payload:   payload,
	}
	if err := tunnel.WriteRequest(stream, &req); err != nil {
		return nil, fmt.Errorf("write tunnel request: %w", err)
	}

	resp, err := tunnel.ReadResponse(stream)
	if err != nil {
		return nil, fmt.Errorf("read tunnel response: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("agent error: %s", resp.Error)
	}

	return resp.Payload, nil
}

func (p *Proxy) makeProxyHandler(toolName string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Derive session ID from the client session if available.
		sessionID := "default"
		if cs := server.ClientSessionFromContext(ctx); cs != nil {
			sessionID = cs.SessionID()
		}

		log := p.logger.With(
			slog.String("tool", toolName),
			slog.String("session_id", sessionID),
		)
		log.Debug("forwarding tool call")

		// Build the JSON-RPC envelope for tools/call.
		rpcReq := jsonRPCRequest{
			JSONRPC: "2.0",
			ID:      p.nextRequestID(),
			Method:  "tools/call",
			Params: map[string]any{
				"name":      request.Params.Name,
				"arguments": request.GetArguments(),
			},
		}

		payload, err := json.Marshal(rpcReq)
		if err != nil {
			log.Error("failed to marshal request", slog.String("error", err.Error()))
			return mcp.NewToolResultErrorFromErr("marshal request", err), nil
		}

		respBytes, err := p.forwardCall(ctx, sessionID, payload)
		if err != nil {
			log.Error("tunnel forward failed", slog.String("error", err.Error()))
			return mcp.NewToolResultErrorFromErr("tunnel error", err), nil
		}

		// Parse the JSON-RPC response.
		var rpcResp toolCallResponse
		if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
			log.Error("failed to parse response", slog.String("error", err.Error()))
			return mcp.NewToolResultErrorFromErr("parse response", err), nil
		}
		if rpcResp.Error != nil {
			log.Error("agent returned error",
				slog.Int("code", rpcResp.Error.Code),
				slog.String("message", rpcResp.Error.Message),
			)
			return mcp.NewToolResultError(rpcResp.Error.Message), nil
		}
		if rpcResp.Result == nil {
			log.Error("empty result from agent")
			return mcp.NewToolResultError("empty result from agent"), nil
		}

		return rpcResp.Result, nil
	}
}

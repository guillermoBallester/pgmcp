package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guillermoballestersasso/pgmcp/pkg/tunnel"
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

// Proxy discovers tools from the agent when it connects and registers
// proxy handlers on the cloud server's MCPServer that forward calls
// through the tunnel.
type Proxy struct {
	tunnel    *TunnelServer
	mcpServer *server.MCPServer
	cfg       tunnel.ServerTunnelConfig
	logger    *slog.Logger

	mu         sync.Mutex
	toolNames  []string
	reqCounter atomic.Uint64
}

// NewProxy creates a new proxy that bridges the tunnel and cloud MCPServer.
func NewProxy(tunnel *TunnelServer, mcpServer *server.MCPServer, cfg tunnel.ServerTunnelConfig, logger *slog.Logger) *Proxy {
	return &Proxy{
		tunnel:    tunnel,
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

// Setup registers the onConnect/onDisconnect callbacks on the TunnelServer.
func (p *Proxy) Setup() {
	p.tunnel.SetOnConnect(p.onAgentConnect)
	p.tunnel.SetOnDisconnect(p.onAgentDisconnect)
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

func (p *Proxy) onAgentConnect() {
	p.logger.Info("discovering tools from agent")

	// Send a tools/list request through the tunnel.
	listReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      p.nextRequestID(),
		Method:  "tools/list",
	}
	payload, err := json.Marshal(listReq)
	if err != nil {
		p.logger.Error("failed to marshal tools/list request",
			slog.String("error", err.Error()),
		)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.discoveryTimeout())
	defer cancel()

	respBytes, err := p.tunnel.ForwardCall(ctx, "discovery", payload)
	if err != nil {
		p.logger.Error("failed to discover tools",
			slog.String("error", err.Error()),
		)
		return
	}

	// Parse the JSON-RPC response to extract tools.
	var rpcResp toolsListResponse
	if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
		p.logger.Error("failed to parse tools/list response",
			slog.String("error", err.Error()),
		)
		return
	}
	if rpcResp.Error != nil {
		p.logger.Error("tools/list returned error",
			slog.String("error", rpcResp.Error.Message),
		)
		return
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
}

func (p *Proxy) onAgentDisconnect() {
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

		respBytes, err := p.tunnel.ForwardCall(ctx, sessionID, payload)
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

package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Proxy discovers tools from the agent when it connects and registers
// proxy handlers on the cloud server's MCPServer that forward calls
// through the tunnel.
type Proxy struct {
	tunnel    *TunnelServer
	mcpServer *server.MCPServer
	logger    *slog.Logger

	toolNames atomic.Value // []string — names of currently registered proxy tools
}

// NewProxy creates a new proxy that bridges the tunnel and cloud MCPServer.
func NewProxy(tunnel *TunnelServer, mcpServer *server.MCPServer, logger *slog.Logger) *Proxy {
	return &Proxy{
		tunnel:    tunnel,
		mcpServer: mcpServer,
		logger:    logger,
	}
}

// Setup registers the onConnect/onDisconnect callbacks on the TunnelServer.
func (p *Proxy) Setup() {
	p.tunnel.SetOnConnect(p.onAgentConnect)
	p.tunnel.SetOnDisconnect(p.onAgentDisconnect)
}

func (p *Proxy) onAgentConnect() {
	p.logger.Info("discovering tools from agent")

	// Send a tools/list request through the tunnel.
	listReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      "discovery",
		"method":  "tools/list",
	}
	payload, err := json.Marshal(listReq)
	if err != nil {
		p.logger.Error("failed to marshal tools/list request",
			slog.String("error", err.Error()),
		)
		return
	}

	respBytes, err := p.tunnel.ForwardCall(context.Background(), "discovery", payload)
	if err != nil {
		p.logger.Error("failed to discover tools",
			slog.String("error", err.Error()),
		)
		return
	}

	// Parse the JSON-RPC response to extract tools.
	var rpcResp struct {
		Result struct {
			Tools []mcp.Tool `json:"tools"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
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
	p.toolNames.Store(names)

	p.logger.Info("registered tools from agent",
		slog.Int("count", len(names)),
		slog.Any("tools", names),
	)
}

func (p *Proxy) onAgentDisconnect() {
	names, _ := p.toolNames.Load().([]string)
	if len(names) > 0 {
		p.mcpServer.DeleteTools(names...)
		p.logger.Info("removed tools on agent disconnect",
			slog.Int("count", len(names)),
		)
	}
	p.toolNames.Store([]string(nil))
}

func (p *Proxy) makeProxyHandler(toolName string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Build the JSON-RPC envelope for tools/call.
		rpcReq := map[string]any{
			"jsonrpc": "2.0",
			"id":      fmt.Sprintf("proxy-%s", toolName),
			"method":  "tools/call",
			"params": map[string]any{
				"name":      request.Params.Name,
				"arguments": request.GetArguments(),
			},
		}

		payload, err := json.Marshal(rpcReq)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("marshal request: %v", err)), nil
		}

		// Derive session ID from the client session if available.
		sessionID := "default"
		if cs := server.ClientSessionFromContext(ctx); cs != nil {
			sessionID = cs.SessionID()
		}

		respBytes, err := p.tunnel.ForwardCall(ctx, sessionID, payload)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("tunnel error: %v", err)), nil
		}

		// Parse the JSON-RPC response.
		var rpcResp struct {
			Result *mcp.CallToolResult `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("parse response: %v", err)), nil
		}
		if rpcResp.Error != nil {
			return mcp.NewToolResultError(rpcResp.Error.Message), nil
		}
		if rpcResp.Result == nil {
			return mcp.NewToolResultError("empty result from agent"), nil
		}

		return rpcResp.Result, nil
	}
}

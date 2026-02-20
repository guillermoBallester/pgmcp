package mcp

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func toolCallHooks(logger *slog.Logger) *server.Hooks {
	hooks := &server.Hooks{}
	var starts sync.Map

	hooks.AddBeforeCallTool(func(ctx context.Context, id any, req *mcp.CallToolRequest) {
		starts.Store(id, time.Now())
	})

	hooks.AddAfterCallTool(func(ctx context.Context, id any, req *mcp.CallToolRequest, result any) {
		duration := sinceStart(&starts, id)
		level := slog.LevelInfo
		isErr := false

		if r, ok := result.(*mcp.CallToolResult); ok && r.IsError {
			level = slog.LevelError
			isErr = true
		}

		logger.LogAttrs(ctx, level, "tool call",
			slog.String("rpc.method", "tools/call"),
			slog.String("mcp.tool", req.Params.Name),
			slog.Duration("duration", duration),
			slog.Bool("error", isErr),
		)
	})

	hooks.AddOnError(func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
		duration := sinceStart(&starts, id)
		toolName := ""
		if req, ok := message.(*mcp.CallToolRequest); ok {
			toolName = req.Params.Name
		}
		if toolName != "" {
			logger.LogAttrs(ctx, slog.LevelError, "tool call",
				slog.String("rpc.method", "tools/call"),
				slog.String("mcp.tool", toolName),
				slog.Duration("duration", duration),
				slog.Bool("error", true),
				slog.String("error.message", err.Error()),
			)
		}
	})

	return hooks
}

func sinceStart(starts *sync.Map, id any) time.Duration {
	if v, ok := starts.LoadAndDelete(id); ok {
		return time.Since(v.(time.Time))
	}
	return 0
}

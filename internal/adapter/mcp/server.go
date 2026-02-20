package mcp

import (
	"log/slog"

	"github.com/guillermoBallester/isthmus/internal/core/service"
	"github.com/mark3labs/mcp-go/server"
)

func NewServer(version string, explorer *service.ExplorerService, query *service.QueryService, logger *slog.Logger) *server.MCPServer {
	s := server.NewMCPServer(
		serverName,
		version,
		server.WithHooks(toolCallHooks(logger)),
	)

	RegisterTools(s, explorer, query)

	return s
}

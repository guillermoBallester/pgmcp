package app

import (
	"log/slog"

	"github.com/guillermoballestersasso/pgmcp/pkg/core/service"
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

package app

import (
	"log/slog"

	"github.com/guillermoballestersasso/pgmcp/pkg/core/service"
	"github.com/mark3labs/mcp-go/server"
)

func NewServer(explorer *service.ExplorerService, query *service.QueryService, logger *slog.Logger) *server.MCPServer {
	s := server.NewMCPServer(
		serverName,
		serverVersion,
	)

	RegisterTools(s, explorer, query, logger)

	return s
}

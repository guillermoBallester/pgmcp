package app

import (
	"github.com/guillermoballestersasso/pgmcp/internal/core/service"
	"github.com/mark3labs/mcp-go/server"
)

func NewServer(explorer *service.ExplorerService, query *service.QueryService) *server.MCPServer {
	s := server.NewMCPServer(
		serverName,
		serverVersion,
	)

	RegisterTools(s, explorer, query)

	return s
}

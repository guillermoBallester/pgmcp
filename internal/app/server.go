package app

import (
	"github.com/guillermoballestersasso/pgmcp/internal/core/ports"
	"github.com/mark3labs/mcp-go/server"
)

func NewServer(explorer ports.SchemaExplorer, executor ports.QueryExecutor) *server.MCPServer {
	s := server.NewMCPServer(
		serverName,
		serverVersion,
	)

	RegisterTools(s, explorer, executor)

	return s
}

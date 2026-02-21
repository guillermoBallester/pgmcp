package mcp

import (
	"log/slog"

	"github.com/guillermoBallester/isthmus/internal/core/port"
	"github.com/guillermoBallester/isthmus/internal/core/service"
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates an MCPServer with tools, hooks, and optional audit logging.
// auditLogger may be nil (e.g. on the agent side where there is no audit DB).
func NewServer(version string, explorer *service.ExplorerService, query *service.QueryService, logger *slog.Logger, auditLogger port.AuditLogger) *server.MCPServer {
	s := server.NewMCPServer(
		serverName,
		version,
		server.WithHooks(toolCallHooks(logger, auditLogger)),
	)

	RegisterTools(s, explorer, query)

	return s
}

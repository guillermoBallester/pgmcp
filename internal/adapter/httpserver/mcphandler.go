package httpserver

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/guillermoBallester/isthmus/internal/core/port"
	"github.com/guillermoBallester/isthmus/internal/core/service"
	itunnel "github.com/guillermoBallester/isthmus/internal/tunnel"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// handleMCP returns an HTTP handler that authenticates MCP clients using API keys
// and routes requests to the correct per-database MCPServer. Checks the tunnel
// registry first (agent-backed), then falls back to direct connections.
func (s *Server) handleMCP(registry *itunnel.TunnelRegistry, directSvc *service.DirectConnectionService, authenticator port.Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract Bearer token.
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")

		// Authenticate.
		result, err := authenticator.Authenticate(r.Context(), token)
		if err != nil {
			s.logger.Error("mcp auth error", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		if result == nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		// Determine which database to route to.
		if len(result.DatabaseIDs) == 0 {
			http.Error(w, `{"error":"api key has no database assigned"}`, http.StatusForbidden)
			return
		}
		if len(result.DatabaseIDs) > 1 {
			http.Error(w, `{"error":"api key has multiple databases, specify X-Database-ID header"}`, http.StatusBadRequest)
			return
		}

		databaseID := result.DatabaseIDs[0]

		// Look up per-database MCPServer: tunnel first, then direct.
		mcpSrv := registry.GetMCPServer(databaseID)
		if mcpSrv == nil && directSvc != nil {
			mcpSrv, err = directSvc.GetMCPServer(r.Context(), databaseID)
			if err != nil {
				s.logger.Error("direct connection failed",
					slog.String("database_id", databaseID.String()),
					slog.String("error", err.Error()),
				)
			}
		}
		if mcpSrv == nil {
			http.Error(w, `{"error":"database not available"}`, http.StatusServiceUnavailable)
			return
		}

		// Delegate to the per-database MCPServer's HTTP handler.
		mcpserver.NewStreamableHTTPServer(mcpSrv).ServeHTTP(w, r)
	}
}

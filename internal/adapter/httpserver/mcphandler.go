package httpserver

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
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

		// Rate limit by API key.
		if !s.mcpLimiter.Allow(result.KeyID.String()) {
			retryAfter := s.mcpLimiter.RetryAfter(result.KeyID.String())
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}

		// Attach auth info to context so MCP hooks can access it for audit logging.
		r = r.WithContext(port.ContextWithAuth(r.Context(), result))

		// Determine which database to route to.
		if result.DatabaseID == uuid.Nil {
			http.Error(w, `{"error":"api key has no database assigned"}`, http.StatusForbidden)
			return
		}

		databaseID := result.DatabaseID

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

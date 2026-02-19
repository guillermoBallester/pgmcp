package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/guillermoBallester/isthmus/internal/config"
	"github.com/guillermoBallester/isthmus/internal/store"
	itunnel "github.com/guillermoBallester/isthmus/internal/tunnel"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps the HTTP server with chi routing, middleware, and graceful shutdown.
type Server struct {
	httpServer  *http.Server
	router      chi.Router
	logger      *slog.Logger
	adminSecret string
	corsOrigin  string
}

// New creates a new Server wired with the given tunnel and MCP servers.
// queries and adminSecret may be nil/"" when running in static-key mode (no Supabase).
func New(listenAddr string, tunnelSrv *itunnel.TunnelServer, mcpSrv *server.MCPServer,
	httpCfg config.HTTPConfig, queries *store.Queries, adminSecret, corsOrigin string, logger *slog.Logger) *Server {
	s := &Server{
		logger:      logger,
		adminSecret: adminSecret,
		corsOrigin:  corsOrigin,
	}

	s.setupRoutes(tunnelSrv, mcpSrv, queries)

	s.httpServer = &http.Server{
		Addr:              listenAddr,
		Handler:           s.router,
		ReadHeaderTimeout: httpCfg.ReadHeaderTimeout,
		IdleTimeout:       httpCfg.IdleTimeout,
	}

	return s
}

// ListenAndServe starts the HTTP server and blocks until it stops.
// Returns nil if the server was shut down gracefully via Shutdown.
func (s *Server) ListenAndServe() error {
	s.logger.Info("HTTP server listening",
		slog.String("addr", s.httpServer.Addr),
	)
	if err := s.httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

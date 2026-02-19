package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/guillermoBallester/isthmus/internal/config"
	itunnel "github.com/guillermoBallester/isthmus/internal/tunnel"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps the HTTP server with chi routing, middleware, and graceful shutdown.
type Server struct {
	httpServer *http.Server
	router     chi.Router
	logger     *slog.Logger
}

// New creates a new Server wired with the given tunnel and MCP servers.
func New(listenAddr string, tunnelSrv *itunnel.TunnelServer, mcpSrv *server.MCPServer,
	httpCfg config.HTTPConfig, logger *slog.Logger) *Server {
	s := &Server{
		logger: logger,
	}

	s.setupRoutes(tunnelSrv, mcpSrv)

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

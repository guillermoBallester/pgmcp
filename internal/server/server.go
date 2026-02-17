package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	itunnel "github.com/guillermoballestersasso/pgmcp/internal/tunnel"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps the HTTP server with chi routing, middleware, and graceful shutdown.
type Server struct {
	httpServer *http.Server
	router     chi.Router
	logger     *slog.Logger
}

// New creates a new Server wired with the given tunnel and MCP servers.
func New(listenAddr string, tunnelSrv *itunnel.TunnelServer, mcpSrv *server.MCPServer, logger *slog.Logger) *Server {
	s := &Server{
		logger: logger,
	}

	s.setupRoutes(tunnelSrv, mcpSrv)

	s.httpServer = &http.Server{
		Addr:              listenAddr,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return s
}

// Start begins listening in the background and returns an error channel.
// A non-nil error is sent if the server fails to start or encounters a fatal error.
// The channel is closed when the server stops.
func (s *Server) Start() <-chan error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("HTTP server listening",
			slog.String("addr", s.httpServer.Addr),
		)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	return errCh
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

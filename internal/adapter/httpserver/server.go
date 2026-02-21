package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/guillermoBallester/isthmus/internal/core/port"
	"github.com/guillermoBallester/isthmus/internal/core/service"
	itunnel "github.com/guillermoBallester/isthmus/internal/tunnel"
)

// Config holds HTTP server configuration.
type Config struct {
	ListenAddr        string
	AdminSecret       string
	CORSOrigin        string
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
}

// Server wraps the HTTP server with chi routing, middleware, and graceful shutdown.
type Server struct {
	httpServer     *http.Server
	router         chi.Router
	logger         *slog.Logger
	cfg            Config
	webhookHandler *WebhookHandler
}

// New creates a new Server wired with the given dependencies.
func New(cfg Config, registry *itunnel.TunnelRegistry,
	directSvc *service.DirectConnectionService,
	authenticator port.Authenticator,
	adminSvc *service.AdminService,
	webhookHandler *WebhookHandler, logger *slog.Logger) *Server {

	s := &Server{
		logger:         logger,
		cfg:            cfg,
		webhookHandler: webhookHandler,
	}

	s.setupRoutes(registry, directSvc, authenticator, adminSvc)

	s.httpServer = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           s.router,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
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

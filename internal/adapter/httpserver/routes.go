package httpserver

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/guillermoBallester/isthmus/internal/core/port"
	"github.com/guillermoBallester/isthmus/internal/core/service"
	itunnel "github.com/guillermoBallester/isthmus/internal/tunnel"
)

func (s *Server) setupRoutes(registry *itunnel.TunnelRegistry, directSvc *service.DirectConnectionService, authenticator port.Authenticator, adminSvc *service.AdminService) {
	r := chi.NewRouter()

	// Global middleware stack
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(s.requestLogger)
	r.Use(chimw.Recoverer)

	// MCP endpoint — authenticated, routes to per-database MCPServer.
	r.HandleFunc("/mcp", s.handleMCP(registry, directSvc, authenticator))

	// Agent tunnel WebSocket endpoint
	r.HandleFunc("/tunnel", registry.HandleTunnel)

	// Health probes
	r.Get("/health", s.handleHealth())
	r.Get("/ready", s.handleRegistryReady(registry))

	// Clerk webhook — Svix-verified, no admin auth needed.
	if s.webhookHandler != nil {
		r.Post("/api/webhooks/clerk", s.webhookHandler.HandleClerkWebhook())
	}

	// Admin API
	r.Route("/api", func(api chi.Router) {
		if s.cfg.CORSOrigin != "" {
			api.Use(cors.Handler(cors.Options{
				AllowedOrigins:   []string{s.cfg.CORSOrigin},
				AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
				AllowedHeaders:   []string{"Authorization", "Content-Type"},
				AllowCredentials: false,
				MaxAge:           300,
			}))
		}
		api.Use(s.adminLimiter.Middleware)
		api.Use(s.adminAuth)
		api.Post("/keys", s.handleCreateKey(adminSvc))
		api.Get("/keys", s.handleListKeys(adminSvc))
		api.Delete("/keys/{id}", s.handleDeleteKey(adminSvc))
		api.Post("/databases", s.handleCreateDatabase(adminSvc))
		api.Get("/databases", s.handleListDatabases(adminSvc))
		api.Delete("/databases/{id}", s.handleDeleteDatabase(adminSvc))
		api.Get("/query-logs", s.handleListQueryLogs(adminSvc))
	})

	s.router = r
}

// handleRegistryReady returns 200 if at least one tunnel is connected, 503 otherwise.
func (s *Server) handleRegistryReady(registry *itunnel.TunnelRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !registry.AnyConnected() {
			http.Error(w, "no agents connected", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

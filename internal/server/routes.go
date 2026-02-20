package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/guillermoBallester/isthmus/internal/adapter/store"
	"github.com/guillermoBallester/isthmus/internal/auth"
	"github.com/guillermoBallester/isthmus/internal/direct"
	itunnel "github.com/guillermoBallester/isthmus/internal/tunnel"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func (s *Server) setupRoutes(registry *itunnel.TunnelRegistry, directMgr *direct.Manager, mcpSrv *mcpserver.MCPServer, authenticator auth.Authenticator, queries *store.Queries, encryptionKey string) {
	r := chi.NewRouter()

	// Global middleware stack
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(s.requestLogger)
	r.Use(chimw.Recoverer)

	// MCP endpoint — authenticated, routes to per-database MCPServer via registry.
	// In multi-tenant mode (queries != nil), clients must provide an API key.
	// In static-key mode, fall back to the single global MCPServer.
	if queries != nil {
		r.HandleFunc("/mcp", s.handleMCP(registry, directMgr, authenticator))
	} else {
		r.Handle("/mcp", mcpserver.NewStreamableHTTPServer(mcpSrv))
	}

	// Agent tunnel WebSocket endpoint
	r.HandleFunc("/tunnel", registry.HandleTunnel)

	// Health probes
	r.Get("/health", s.handleHealth())
	r.Get("/ready", s.handleRegistryReady(registry))

	// Clerk webhook — Svix-verified, no admin auth needed.
	if s.webhookHandler != nil {
		r.Post("/api/webhooks/clerk", s.webhookHandler.HandleClerkWebhook())
	}

	// Admin API — only available when Supabase is configured.
	if queries != nil && s.adminSecret != "" {
		r.Route("/api", func(api chi.Router) {
			if s.corsOrigin != "" {
				api.Use(cors.Handler(cors.Options{
					AllowedOrigins:   []string{s.corsOrigin},
					AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
					AllowedHeaders:   []string{"Authorization", "Content-Type"},
					AllowCredentials: false,
					MaxAge:           300,
				}))
			}
			api.Use(s.adminAuth)
			api.Post("/keys", s.handleCreateKey(queries))
			api.Get("/keys", s.handleListKeys(queries))
			api.Delete("/keys/{id}", s.handleDeleteKey(queries))
			api.Post("/keys/{id}/databases", s.handleGrantKeyDatabase(queries))
			api.Delete("/keys/{id}/databases/{db_id}", s.handleRevokeKeyDatabase(queries))
			api.Post("/databases", s.handleCreateDatabase(queries, encryptionKey))
			api.Get("/databases", s.handleListDatabases(queries))
			api.Delete("/databases/{id}", s.handleDeleteDatabase(queries, directMgr))
		})
	}

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

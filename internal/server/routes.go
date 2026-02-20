package server

import (
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/guillermoBallester/isthmus/internal/store"
	itunnel "github.com/guillermoBallester/isthmus/internal/tunnel"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func (s *Server) setupRoutes(tunnelSrv *itunnel.TunnelServer, mcpSrv *mcpserver.MCPServer, queries *store.Queries) {
	r := chi.NewRouter()

	// Global middleware stack
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(s.requestLogger)
	r.Use(chimw.Recoverer)

	// MCP streamable HTTP endpoint
	r.Handle("/mcp", mcpserver.NewStreamableHTTPServer(mcpSrv))

	// Agent tunnel WebSocket endpoint
	r.HandleFunc("/tunnel", tunnelSrv.HandleTunnel)

	// Health probes
	r.Get("/health", s.handleHealth())
	r.Get("/ready", s.handleReady(tunnelSrv))

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
		})
	}

	// Control Plane API — Clerk JWT authenticated, for the dashboard.
	if queries != nil && s.clerkEnabled {
		r.Route("/api/v1", func(api chi.Router) {
			if s.corsOrigin != "" {
				api.Use(cors.Handler(cors.Options{
					AllowedOrigins:   []string{s.corsOrigin},
					AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
					AllowedHeaders:   []string{"Authorization", "Content-Type"},
					AllowCredentials: true,
					MaxAge:           300,
				}))
			}

			api.Use(s.clerkJWTAuth(queries))

			// Workspace listing (no workspace_id needed).
			api.Get("/workspaces", s.handleListWorkspaces(queries))

			api.Route("/workspaces/{workspace_id}", func(ws chi.Router) {
				ws.Use(s.workspaceMemberAuth(queries))

				// Database management
				ws.Get("/databases", s.handleListDatabases(queries))
				ws.Post("/databases", s.handleCreateDatabase(queries, s.encryptor))
				ws.Delete("/databases/{db_id}", s.handleDeleteDatabase(queries))

				// API key management
				ws.Get("/api-keys", s.handleCPListKeys(queries))
				ws.Post("/api-keys", s.handleCPCreateKey(queries))
				ws.Delete("/api-keys/{key_id}", s.handleCPDeleteKey(queries))
			})
		})
	}

	s.router = r
}

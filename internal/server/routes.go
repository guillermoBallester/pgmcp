package server

import (
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
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

	// Admin API — only available when Supabase is configured.
	if queries != nil && s.adminSecret != "" {
		r.Route("/api", func(api chi.Router) {
			api.Use(s.adminAuth)
			api.Post("/keys", s.handleCreateKey(queries))
			api.Get("/keys", s.handleListKeys(queries))
			api.Delete("/keys/{id}", s.handleRevokeKey(queries))
		})
	}

	s.router = r
}

package server

import (
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	itunnel "github.com/guillermoballestersasso/pgmcp/internal/tunnel"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func (s *Server) setupRoutes(tunnelSrv *itunnel.TunnelServer, mcpSrv *mcpserver.MCPServer) {
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

	s.router = r
}

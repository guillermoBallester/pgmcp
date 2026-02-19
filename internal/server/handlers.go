package server

import (
	"encoding/json"
	"net/http"

	itunnel "github.com/guillermoBallester/isthmus/internal/tunnel"
)

// handleHealth returns a liveness probe handler. Always responds 200 if the
// server process is running.
func (s *Server) handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// handleReady returns a readiness probe handler. Responds 200 when at least
// one agent is connected, 503 otherwise.
func (s *Server) handleReady(tunnelSrv *itunnel.TunnelServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if tunnelSrv.Connected() {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "no agent connected"})
		}
	}
}

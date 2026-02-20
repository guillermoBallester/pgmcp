package httpserver

import (
	"encoding/json"
	"net/http"
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

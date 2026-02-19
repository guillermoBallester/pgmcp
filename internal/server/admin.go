package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/guillermoBallester/isthmus/internal/auth"
	"github.com/guillermoBallester/isthmus/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// adminAuth is middleware that checks the Authorization header for the admin secret.
func (s *Server) adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		if token != s.adminSecret {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// createKeyRequest is the JSON body for POST /api/keys.
type createKeyRequest struct {
	Name string `json:"name"`
}

// createKeyResponse is the JSON response for POST /api/keys.
type createKeyResponse struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	KeyPrefix string `json:"key_prefix"`
	Name      string `json:"name"`
}

// handleCreateKey creates a new API key and returns the full key (shown once).
func (s *Server) handleCreateKey(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		fullKey, hash, displayPrefix, err := auth.GenerateKey()
		if err != nil {
			s.logger.Error("failed to generate api key", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		row, err := queries.CreateAPIKey(r.Context(), store.CreateAPIKeyParams{
			KeyHash:   hash,
			KeyPrefix: displayPrefix,
			Name:      req.Name,
		})
		if err != nil {
			s.logger.Error("failed to insert api key", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		resp := createKeyResponse{
			ID:        uuidToString(row.ID),
			Key:       fullKey,
			KeyPrefix: row.KeyPrefix,
			Name:      row.Name,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// keyResponse is one item in the GET /api/keys list.
type keyResponse struct {
	ID         string  `json:"id"`
	KeyPrefix  string  `json:"key_prefix"`
	Name       string  `json:"name"`
	IsActive   bool    `json:"is_active"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	RevokedAt  *string `json:"revoked_at"`
}

// handleListKeys lists all API keys (without the full key or hash).
func (s *Server) handleListKeys(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := queries.ListAPIKeys(r.Context())
		if err != nil {
			s.logger.Error("failed to list api keys", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		resp := make([]keyResponse, 0, len(rows))
		for _, row := range rows {
			kr := keyResponse{
				ID:        uuidToString(row.ID),
				KeyPrefix: row.KeyPrefix,
				Name:      row.Name,
				IsActive:  row.IsActive,
				CreatedAt: formatTimestamptz(row.CreatedAt),
			}
			if row.LastUsedAt.Valid {
				s := formatTimestamptz(row.LastUsedAt)
				kr.LastUsedAt = &s
			}
			if row.RevokedAt.Valid {
				s := formatTimestamptz(row.RevokedAt)
				kr.RevokedAt = &s
			}
			resp = append(resp, kr)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// handleRevokeKey revokes an API key by ID.
func (s *Server) handleRevokeKey(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")

		var id pgtype.UUID
		if err := id.Scan(idStr); err != nil {
			http.Error(w, `{"error":"invalid key id"}`, http.StatusBadRequest)
			return
		}

		if err := queries.RevokeAPIKey(r.Context(), id); err != nil {
			s.logger.Error("failed to revoke api key", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func uuidToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return strings.Join([]string{
		hex(b[0:4]), hex(b[4:6]), hex(b[6:8]), hex(b[8:10]), hex(b[10:16]),
	}, "-")
}

func hex(b []byte) string {
	const hextable = "0123456789abcdef"
	dst := make([]byte, len(b)*2)
	for i, v := range b {
		dst[i*2] = hextable[v>>4]
		dst[i*2+1] = hextable[v&0x0f]
	}
	return string(dst)
}

func formatTimestamptz(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.Format(time.RFC3339)
}

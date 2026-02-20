package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/guillermoBallester/isthmus/internal/adapter/store"
	"github.com/guillermoBallester/isthmus/internal/auth"
	"github.com/guillermoBallester/isthmus/internal/crypto"
	"github.com/guillermoBallester/isthmus/internal/direct"
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
	Name        string `json:"name"`
	WorkspaceID string `json:"workspace_id"`
}

// createKeyResponse is the JSON response for POST /api/keys.
type createKeyResponse struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	KeyPrefix   string `json:"key_prefix"`
	Name        string `json:"name"`
	WorkspaceID string `json:"workspace_id"`
}

// handleCreateKey creates a new API key and returns the full key (shown once).
func (s *Server) handleCreateKey(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		var wsID pgtype.UUID
		if err := wsID.Scan(req.WorkspaceID); err != nil {
			http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
			return
		}

		fullKey, hash, displayPrefix, err := auth.GenerateKey()
		if err != nil {
			s.logger.Error("failed to generate api key", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		row, err := queries.CreateAPIKey(r.Context(), store.CreateAPIKeyParams{
			WorkspaceID: wsID,
			KeyHash:     hash,
			KeyPrefix:   displayPrefix,
			Name:        req.Name,
		})
		if err != nil {
			s.logger.Error("failed to insert api key", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		resp := createKeyResponse{
			ID:          uuidToString(row.ID),
			Key:         fullKey,
			KeyPrefix:   row.KeyPrefix,
			Name:        row.Name,
			WorkspaceID: uuidToString(row.WorkspaceID),
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
	CreatedAt  string  `json:"created_at"`
	ExpiresAt  *string `json:"expires_at,omitempty"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
}

// handleListKeys lists API keys for a workspace (without the full key or hash).
func (s *Server) handleListKeys(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wsIDStr := r.URL.Query().Get("workspace_id")
		if wsIDStr == "" {
			http.Error(w, `{"error":"workspace_id query param required"}`, http.StatusBadRequest)
			return
		}

		var wsID pgtype.UUID
		if err := wsID.Scan(wsIDStr); err != nil {
			http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
			return
		}

		rows, err := queries.ListAPIKeysByWorkspace(r.Context(), wsID)
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
				CreatedAt: formatTimestamptz(row.CreatedAt),
			}
			if row.ExpiresAt.Valid {
				s := formatTimestamptz(row.ExpiresAt)
				kr.ExpiresAt = &s
			}
			if row.LastUsedAt.Valid {
				s := formatTimestamptz(row.LastUsedAt)
				kr.LastUsedAt = &s
			}
			resp = append(resp, kr)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// handleDeleteKey deletes an API key by ID (scoped to workspace).
func (s *Server) handleDeleteKey(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		wsIDStr := r.URL.Query().Get("workspace_id")

		var id pgtype.UUID
		if err := id.Scan(idStr); err != nil {
			http.Error(w, `{"error":"invalid key id"}`, http.StatusBadRequest)
			return
		}

		var wsID pgtype.UUID
		if err := wsID.Scan(wsIDStr); err != nil {
			http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
			return
		}

		if err := queries.DeleteAPIKey(r.Context(), store.DeleteAPIKeyParams{
			ID:          id,
			WorkspaceID: wsID,
		}); err != nil {
			s.logger.Error("failed to delete api key", slog.String("error", err.Error()))
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

// --- Database management endpoints ---

type createDatabaseRequest struct {
	WorkspaceID    string `json:"workspace_id"`
	Name           string `json:"name"`
	ConnectionType string `json:"connection_type"` // "tunnel" or "direct"
	ConnectionURL  string `json:"connection_url"`  // required when connection_type="direct"
}

type databaseResponse struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspace_id"`
	Name           string `json:"name"`
	ConnectionType string `json:"connection_type"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
}

// handleCreateDatabase creates a new database record.
// For direct connections, encrypts and stores the connection URL.
func (s *Server) handleCreateDatabase(queries *store.Queries, encryptionKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createDatabaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
			return
		}
		if req.ConnectionType == "" {
			req.ConnectionType = "tunnel"
		}

		var wsID pgtype.UUID
		if err := wsID.Scan(req.WorkspaceID); err != nil {
			http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
			return
		}

		var resp databaseResponse

		if req.ConnectionType == "direct" {
			if req.ConnectionURL == "" {
				http.Error(w, `{"error":"connection_url is required for direct connections"}`, http.StatusBadRequest)
				return
			}
			if encryptionKey == "" {
				http.Error(w, `{"error":"direct connections not enabled (ENCRYPTION_KEY not set)"}`, http.StatusBadRequest)
				return
			}

			encrypted, err := crypto.Encrypt([]byte(req.ConnectionURL), encryptionKey)
			if err != nil {
				s.logger.Error("failed to encrypt connection URL", slog.String("error", err.Error()))
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}

			row, err := queries.CreateDatabaseWithURL(r.Context(), store.CreateDatabaseWithURLParams{
				WorkspaceID:            wsID,
				Name:                   req.Name,
				ConnectionType:         "direct",
				EncryptedConnectionUrl: encrypted,
				Status:                 "ready",
			})
			if err != nil {
				s.logger.Error("failed to create database", slog.String("error", err.Error()))
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}

			resp = databaseResponse{
				ID:             uuidToString(row.ID),
				WorkspaceID:    uuidToString(row.WorkspaceID),
				Name:           row.Name,
				ConnectionType: row.ConnectionType,
				Status:         row.Status,
				CreatedAt:      formatTimestamptz(row.CreatedAt),
			}
		} else {
			row, err := queries.CreateDatabase(r.Context(), store.CreateDatabaseParams{
				WorkspaceID:    wsID,
				Name:           req.Name,
				ConnectionType: req.ConnectionType,
				Status:         "pending",
			})
			if err != nil {
				s.logger.Error("failed to create database", slog.String("error", err.Error()))
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}

			resp = databaseResponse{
				ID:             uuidToString(row.ID),
				WorkspaceID:    uuidToString(row.WorkspaceID),
				Name:           row.Name,
				ConnectionType: row.ConnectionType,
				Status:         row.Status,
				CreatedAt:      formatTimestamptz(row.CreatedAt),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// handleListDatabases lists databases for a workspace.
func (s *Server) handleListDatabases(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wsIDStr := r.URL.Query().Get("workspace_id")
		if wsIDStr == "" {
			http.Error(w, `{"error":"workspace_id query param required"}`, http.StatusBadRequest)
			return
		}

		var wsID pgtype.UUID
		if err := wsID.Scan(wsIDStr); err != nil {
			http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
			return
		}

		rows, err := queries.ListDatabasesByWorkspace(r.Context(), wsID)
		if err != nil {
			s.logger.Error("failed to list databases", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		resp := make([]databaseResponse, 0, len(rows))
		for _, row := range rows {
			resp = append(resp, databaseResponse{
				ID:             uuidToString(row.ID),
				WorkspaceID:    uuidToString(row.WorkspaceID),
				Name:           row.Name,
				ConnectionType: row.ConnectionType,
				Status:         row.Status,
				CreatedAt:      formatTimestamptz(row.CreatedAt),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// handleDeleteDatabase deletes a database record and cleans up any cached direct connection.
func (s *Server) handleDeleteDatabase(queries *store.Queries, directMgr *direct.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		wsIDStr := r.URL.Query().Get("workspace_id")

		var id pgtype.UUID
		if err := id.Scan(idStr); err != nil {
			http.Error(w, `{"error":"invalid database id"}`, http.StatusBadRequest)
			return
		}

		var wsID pgtype.UUID
		if err := wsID.Scan(wsIDStr); err != nil {
			http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
			return
		}

		if err := queries.DeleteDatabase(r.Context(), store.DeleteDatabaseParams{
			ID:          id,
			WorkspaceID: wsID,
		}); err != nil {
			s.logger.Error("failed to delete database", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		// Clean up cached direct connection if any.
		if directMgr != nil {
			if dbUUID, err := uuid.Parse(idStr); err == nil {
				directMgr.Remove(dbUUID)
			}
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// --- API key → database linking endpoints ---

type grantKeyDatabaseRequest struct {
	DatabaseID string `json:"database_id"`
}

// handleGrantKeyDatabase links an API key to a database.
func (s *Server) handleGrantKeyDatabase(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyIDStr := chi.URLParam(r, "id")
		var keyID pgtype.UUID
		if err := keyID.Scan(keyIDStr); err != nil {
			http.Error(w, `{"error":"invalid key id"}`, http.StatusBadRequest)
			return
		}

		var req grantKeyDatabaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		var dbID pgtype.UUID
		if err := dbID.Scan(req.DatabaseID); err != nil {
			http.Error(w, `{"error":"invalid database_id"}`, http.StatusBadRequest)
			return
		}

		if err := queries.GrantAPIKeyDatabase(r.Context(), store.GrantAPIKeyDatabaseParams{
			ApiKeyID:   keyID,
			DatabaseID: dbID,
		}); err != nil {
			s.logger.Error("failed to grant key database", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// handleRevokeKeyDatabase unlinks an API key from a database.
func (s *Server) handleRevokeKeyDatabase(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyIDStr := chi.URLParam(r, "id")
		dbIDStr := chi.URLParam(r, "db_id")

		var keyID pgtype.UUID
		if err := keyID.Scan(keyIDStr); err != nil {
			http.Error(w, `{"error":"invalid key id"}`, http.StatusBadRequest)
			return
		}

		var dbID pgtype.UUID
		if err := dbID.Scan(dbIDStr); err != nil {
			http.Error(w, `{"error":"invalid database id"}`, http.StatusBadRequest)
			return
		}

		if err := queries.RevokeAPIKeyDatabase(r.Context(), store.RevokeAPIKeyDatabaseParams{
			ApiKeyID:   keyID,
			DatabaseID: dbID,
		}); err != nil {
			s.logger.Error("failed to revoke key database", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/guillermoBallester/isthmus/internal/auth"
	"github.com/guillermoBallester/isthmus/internal/crypto"
	"github.com/guillermoBallester/isthmus/internal/store"
)

// ---- Workspace endpoints ----

type workspaceResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsPersonal bool   `json:"is_personal"`
	Role       string `json:"role"`
	CreatedAt  string `json:"created_at"`
}

// handleListWorkspaces — GET /api/v1/workspaces
func (s *Server) handleListWorkspaces(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		rows, err := queries.ListWorkspacesForUser(r.Context(), user.ID)
		if err != nil {
			s.logger.Error("failed to list workspaces", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		resp := make([]workspaceResponse, 0, len(rows))
		for _, row := range rows {
			resp = append(resp, workspaceResponse{
				ID:         uuidToString(row.ID),
				Name:       row.Name,
				IsPersonal: row.IsPersonal,
				Role:       row.Role,
				CreatedAt:  formatTimestamptz(row.CreatedAt),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// ---- Database endpoints ----

type databaseResponse struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspace_id"`
	Name           string `json:"name"`
	ConnectionType string `json:"connection_type"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type createDatabaseRequest struct {
	Name           string `json:"name"`
	ConnectionType string `json:"connection_type"`
	ConnectionURL  string `json:"connection_url"`
}

// handleListDatabases — GET /api/v1/workspaces/{workspace_id}/databases
func (s *Server) handleListDatabases(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wsIDStr := chi.URLParam(r, "workspace_id")
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
				UpdatedAt:      formatTimestamptz(row.UpdatedAt),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// handleCreateDatabase — POST /api/v1/workspaces/{workspace_id}/databases
func (s *Server) handleCreateDatabase(queries *store.Queries, enc *crypto.Encryptor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wsIDStr := chi.URLParam(r, "workspace_id")
		var wsID pgtype.UUID
		if err := wsID.Scan(wsIDStr); err != nil {
			http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
			return
		}

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

		status := "pending"
		if req.ConnectionType == "direct" && req.ConnectionURL != "" {
			status = "connected"
		}

		var row databaseResponse

		if req.ConnectionURL != "" && enc != nil {
			encryptedURL, err := enc.Encrypt([]byte(req.ConnectionURL))
			if err != nil {
				s.logger.Error("failed to encrypt connection url", slog.String("error", err.Error()))
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}

			dbRow, err := queries.CreateDatabaseWithURL(r.Context(), store.CreateDatabaseWithURLParams{
				WorkspaceID:            wsID,
				Name:                   req.Name,
				ConnectionType:         req.ConnectionType,
				EncryptedConnectionUrl: encryptedURL,
				Status:                 status,
			})
			if err != nil {
				s.logger.Error("failed to create database", slog.String("error", err.Error()))
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}
			row = databaseResponse{
				ID:             uuidToString(dbRow.ID),
				WorkspaceID:    uuidToString(dbRow.WorkspaceID),
				Name:           dbRow.Name,
				ConnectionType: dbRow.ConnectionType,
				Status:         dbRow.Status,
				CreatedAt:      formatTimestamptz(dbRow.CreatedAt),
				UpdatedAt:      formatTimestamptz(dbRow.UpdatedAt),
			}
		} else {
			dbRow, err := queries.CreateDatabase(r.Context(), store.CreateDatabaseParams{
				WorkspaceID:    wsID,
				Name:           req.Name,
				ConnectionType: req.ConnectionType,
				Status:         status,
			})
			if err != nil {
				s.logger.Error("failed to create database", slog.String("error", err.Error()))
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}
			row = databaseResponse{
				ID:             uuidToString(dbRow.ID),
				WorkspaceID:    uuidToString(dbRow.WorkspaceID),
				Name:           dbRow.Name,
				ConnectionType: dbRow.ConnectionType,
				Status:         dbRow.Status,
				CreatedAt:      formatTimestamptz(dbRow.CreatedAt),
				UpdatedAt:      formatTimestamptz(dbRow.UpdatedAt),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(row)
	}
}

// handleDeleteDatabase — DELETE /api/v1/workspaces/{workspace_id}/databases/{db_id}
func (s *Server) handleDeleteDatabase(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wsIDStr := chi.URLParam(r, "workspace_id")
		dbIDStr := chi.URLParam(r, "db_id")

		var wsID, dbID pgtype.UUID
		if err := wsID.Scan(wsIDStr); err != nil {
			http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
			return
		}
		if err := dbID.Scan(dbIDStr); err != nil {
			http.Error(w, `{"error":"invalid database id"}`, http.StatusBadRequest)
			return
		}

		if err := queries.DeleteDatabase(r.Context(), store.DeleteDatabaseParams{
			ID:          dbID,
			WorkspaceID: wsID,
		}); err != nil {
			s.logger.Error("failed to delete database", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// ---- API Key endpoints ----

// handleCPListKeys — GET /api/v1/workspaces/{workspace_id}/api-keys
func (s *Server) handleCPListKeys(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wsIDStr := chi.URLParam(r, "workspace_id")
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

// handleCPCreateKey — POST /api/v1/workspaces/{workspace_id}/api-keys
func (s *Server) handleCPCreateKey(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		wsIDStr := chi.URLParam(r, "workspace_id")
		var wsID pgtype.UUID
		if err := wsID.Scan(wsIDStr); err != nil {
			http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
			return
		}

		var req struct {
			Name string `json:"name"`
		}
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
			WorkspaceID: wsID,
			KeyHash:     hash,
			KeyPrefix:   displayPrefix,
			Name:        req.Name,
			CreatedBy:   user.ID,
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

// handleCPDeleteKey — DELETE /api/v1/workspaces/{workspace_id}/api-keys/{key_id}
func (s *Server) handleCPDeleteKey(queries *store.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wsIDStr := chi.URLParam(r, "workspace_id")
		keyIDStr := chi.URLParam(r, "key_id")

		var wsID, keyID pgtype.UUID
		if err := wsID.Scan(wsIDStr); err != nil {
			http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
			return
		}
		if err := keyID.Scan(keyIDStr); err != nil {
			http.Error(w, `{"error":"invalid key id"}`, http.StatusBadRequest)
			return
		}

		if err := queries.DeleteAPIKey(r.Context(), store.DeleteAPIKeyParams{
			ID:          keyID,
			WorkspaceID: wsID,
		}); err != nil {
			s.logger.Error("failed to delete api key", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

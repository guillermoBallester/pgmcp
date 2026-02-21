package httpserver

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/guillermoBallester/isthmus/internal/core/service"
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
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.AdminSecret)) != 1 {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- API key endpoints ---

type createKeyRequest struct {
	Name        string `json:"name"`
	WorkspaceID string `json:"workspace_id"`
	DatabaseID  string `json:"database_id"`
}

type createKeyResponse struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	KeyPrefix   string `json:"key_prefix"`
	Name        string `json:"name"`
	WorkspaceID string `json:"workspace_id"`
	DatabaseID  string `json:"database_id"`
}

func (s *Server) handleCreateKey(adminSvc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		wsID, err := uuid.Parse(req.WorkspaceID)
		if err != nil {
			http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
			return
		}

		dbID, err := uuid.Parse(req.DatabaseID)
		if err != nil {
			http.Error(w, `{"error":"invalid database_id"}`, http.StatusBadRequest)
			return
		}

		fullKey, record, err := adminSvc.CreateAPIKey(r.Context(), wsID, dbID, req.Name)
		if err != nil {
			s.logger.Error("failed to create api key", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		resp := createKeyResponse{
			ID:          record.ID.String(),
			Key:         fullKey,
			KeyPrefix:   record.KeyPrefix,
			Name:        record.Name,
			WorkspaceID: record.WorkspaceID.String(),
			DatabaseID:  record.DatabaseID.String(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

type keyResponse struct {
	ID         string  `json:"id"`
	KeyPrefix  string  `json:"key_prefix"`
	Name       string  `json:"name"`
	DatabaseID string  `json:"database_id"`
	CreatedAt  string  `json:"created_at"`
	ExpiresAt  *string `json:"expires_at,omitempty"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
}

func (s *Server) handleListKeys(adminSvc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wsID, err := uuid.Parse(r.URL.Query().Get("workspace_id"))
		if err != nil {
			http.Error(w, `{"error":"invalid or missing workspace_id"}`, http.StatusBadRequest)
			return
		}

		records, err := adminSvc.ListAPIKeys(r.Context(), wsID)
		if err != nil {
			s.logger.Error("failed to list api keys", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		resp := make([]keyResponse, 0, len(records))
		for _, rec := range records {
			kr := keyResponse{
				ID:         rec.ID.String(),
				KeyPrefix:  rec.KeyPrefix,
				Name:       rec.Name,
				DatabaseID: rec.DatabaseID.String(),
				CreatedAt:  rec.CreatedAt.Format(time.RFC3339),
			}
			if rec.ExpiresAt != nil {
				s := rec.ExpiresAt.Format(time.RFC3339)
				kr.ExpiresAt = &s
			}
			if rec.LastUsedAt != nil {
				s := rec.LastUsedAt.Format(time.RFC3339)
				kr.LastUsedAt = &s
			}
			resp = append(resp, kr)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (s *Server) handleDeleteKey(adminSvc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, `{"error":"invalid key id"}`, http.StatusBadRequest)
			return
		}

		wsID, err := uuid.Parse(r.URL.Query().Get("workspace_id"))
		if err != nil {
			http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
			return
		}

		if err := adminSvc.DeleteAPIKey(r.Context(), id, wsID); err != nil {
			s.logger.Error("failed to delete api key", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// --- Database endpoints ---

type createDatabaseRequest struct {
	WorkspaceID    string `json:"workspace_id"`
	Name           string `json:"name"`
	ConnectionType string `json:"connection_type"`
	ConnectionURL  string `json:"connection_url"`
}

type databaseResponse struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspace_id"`
	Name           string `json:"name"`
	ConnectionType string `json:"connection_type"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
}

func (s *Server) handleCreateDatabase(adminSvc *service.AdminService) http.HandlerFunc {
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

		wsID, err := uuid.Parse(req.WorkspaceID)
		if err != nil {
			http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
			return
		}

		info, err := adminSvc.CreateDatabase(r.Context(), wsID, req.Name, req.ConnectionType, req.ConnectionURL)
		if err != nil {
			s.logger.Error("failed to create database", slog.String("error", err.Error()))
			http.Error(w, `{"error":"failed to create database"}`, http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(databaseResponse{
			ID:             info.ID.String(),
			WorkspaceID:    info.WorkspaceID.String(),
			Name:           info.Name,
			ConnectionType: info.ConnectionType,
			Status:         info.Status,
			CreatedAt:      info.CreatedAt.Format(time.RFC3339),
		})
	}
}

func (s *Server) handleListDatabases(adminSvc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wsID, err := uuid.Parse(r.URL.Query().Get("workspace_id"))
		if err != nil {
			http.Error(w, `{"error":"invalid or missing workspace_id"}`, http.StatusBadRequest)
			return
		}

		infos, err := adminSvc.ListDatabases(r.Context(), wsID)
		if err != nil {
			s.logger.Error("failed to list databases", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		resp := make([]databaseResponse, 0, len(infos))
		for _, info := range infos {
			resp = append(resp, databaseResponse{
				ID:             info.ID.String(),
				WorkspaceID:    info.WorkspaceID.String(),
				Name:           info.Name,
				ConnectionType: info.ConnectionType,
				Status:         info.Status,
				CreatedAt:      info.CreatedAt.Format(time.RFC3339),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (s *Server) handleDeleteDatabase(adminSvc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, `{"error":"invalid database id"}`, http.StatusBadRequest)
			return
		}

		wsID, err := uuid.Parse(r.URL.Query().Get("workspace_id"))
		if err != nil {
			http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
			return
		}

		if err := adminSvc.DeleteDatabase(r.Context(), id, wsID); err != nil {
			s.logger.Error("failed to delete database", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// --- Query log endpoints ---

type queryLogResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	DatabaseID  string `json:"database_id"`
	KeyID       string `json:"key_id,omitempty"`
	ToolName    string `json:"tool_name"`
	ToolInput   string `json:"tool_input,omitempty"`
	DurationMs  int    `json:"duration_ms"`
	IsError     bool   `json:"is_error"`
	CreatedAt   string `json:"created_at"`
}

func (s *Server) handleListQueryLogs(adminSvc *service.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wsID, err := uuid.Parse(r.URL.Query().Get("workspace_id"))
		if err != nil {
			http.Error(w, `{"error":"invalid or missing workspace_id"}`, http.StatusBadRequest)
			return
		}

		var dbID *uuid.UUID
		if v := r.URL.Query().Get("database_id"); v != "" {
			parsed, parseErr := uuid.Parse(v)
			if parseErr != nil {
				http.Error(w, `{"error":"invalid database_id"}`, http.StatusBadRequest)
				return
			}
			dbID = &parsed
		}

		limit := 100
		if v := r.URL.Query().Get("limit"); v != "" {
			n, parseErr := strconv.Atoi(v)
			if parseErr != nil || n <= 0 {
				http.Error(w, `{"error":"invalid limit"}`, http.StatusBadRequest)
				return
			}
			limit = n
		}

		logs, err := adminSvc.ListQueryLogs(r.Context(), wsID, dbID, limit)
		if err != nil {
			s.logger.Error("failed to list query logs", slog.String("error", err.Error()))
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		resp := make([]queryLogResponse, 0, len(logs))
		for _, l := range logs {
			entry := queryLogResponse{
				ID:          l.ID.String(),
				WorkspaceID: l.WorkspaceID.String(),
				DatabaseID:  l.DatabaseID.String(),
				ToolName:    l.ToolName,
				ToolInput:   l.ToolInput,
				DurationMs:  l.DurationMs,
				IsError:     l.IsError,
				CreatedAt:   l.CreatedAt.Format(time.RFC3339),
			}
			if l.KeyID != uuid.Nil {
				entry.KeyID = l.KeyID.String()
			}
			resp = append(resp, entry)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

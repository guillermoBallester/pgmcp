package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/guillermoBallester/isthmus/internal/adapter/auth"
	"github.com/guillermoBallester/isthmus/internal/core/port"
)

// AdminService provides admin operations for API keys and databases.
type AdminService struct {
	repo      port.AdminRepository
	auditRepo port.AuditRepository     // nil when audit logging disabled
	encryptor port.Encryptor           // nil when direct connections disabled
	directSvc *DirectConnectionService // nil when direct connections disabled
	logger    *slog.Logger
}

// NewAdminService creates a new AdminService.
func NewAdminService(repo port.AdminRepository, auditRepo port.AuditRepository, encryptor port.Encryptor, directSvc *DirectConnectionService, logger *slog.Logger) *AdminService {
	return &AdminService{
		repo:      repo,
		auditRepo: auditRepo,
		encryptor: encryptor,
		directSvc: directSvc,
		logger:    logger,
	}
}

// CreateAPIKey generates a new API key, stores its hash, and returns the full key (shown once).
func (s *AdminService) CreateAPIKey(ctx context.Context, workspaceID, databaseID uuid.UUID, name string) (string, *port.APIKeyRecord, error) {
	fullKey, hash, displayPrefix, err := auth.GenerateKey()
	if err != nil {
		return "", nil, fmt.Errorf("generating api key: %w", err)
	}

	record, err := s.repo.CreateAPIKey(ctx, workspaceID, databaseID, hash, displayPrefix, name)
	if err != nil {
		return "", nil, fmt.Errorf("storing api key: %w", err)
	}

	return fullKey, record, nil
}

// ListAPIKeys returns all API keys for a workspace.
func (s *AdminService) ListAPIKeys(ctx context.Context, workspaceID uuid.UUID) ([]port.APIKeyRecord, error) {
	return s.repo.ListAPIKeys(ctx, workspaceID)
}

// DeleteAPIKey removes an API key scoped to a workspace.
func (s *AdminService) DeleteAPIKey(ctx context.Context, id, workspaceID uuid.UUID) error {
	return s.repo.DeleteAPIKey(ctx, id, workspaceID)
}

// CreateDatabase creates a new database record. For direct connections, encrypts the URL.
func (s *AdminService) CreateDatabase(ctx context.Context, workspaceID uuid.UUID, name, connType, connURL string) (*port.DatabaseInfo, error) {
	if connType == "" {
		connType = "tunnel"
	}

	if connType == "direct" {
		if connURL == "" {
			return nil, fmt.Errorf("connection_url is required for direct connections")
		}
		if s.encryptor == nil {
			return nil, fmt.Errorf("direct connections not enabled (ENCRYPTION_KEY not set)")
		}

		encrypted, err := s.encryptor.Encrypt([]byte(connURL))
		if err != nil {
			return nil, fmt.Errorf("encrypting connection URL: %w", err)
		}

		return s.repo.CreateDatabase(ctx, workspaceID, name, "direct", "ready", encrypted)
	}

	return s.repo.CreateDatabase(ctx, workspaceID, name, connType, "pending", nil)
}

// ListDatabases returns all databases for a workspace.
func (s *AdminService) ListDatabases(ctx context.Context, workspaceID uuid.UUID) ([]port.DatabaseInfo, error) {
	return s.repo.ListDatabases(ctx, workspaceID)
}

// ListQueryLogs retrieves audit log entries for a workspace.
func (s *AdminService) ListQueryLogs(ctx context.Context, workspaceID uuid.UUID, databaseID *uuid.UUID, limit int) ([]port.QueryLogRecord, error) {
	if s.auditRepo == nil {
		return nil, fmt.Errorf("audit logging not configured")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	return s.auditRepo.ListQueryLogs(ctx, workspaceID, databaseID, limit)
}

// DeleteDatabase removes a database record and cleans up any cached direct connection.
func (s *AdminService) DeleteDatabase(ctx context.Context, id, workspaceID uuid.UUID) error {
	if err := s.repo.DeleteDatabase(ctx, id, workspaceID); err != nil {
		return err
	}

	if s.directSvc != nil {
		s.directSvc.Remove(id)
	}

	return nil
}

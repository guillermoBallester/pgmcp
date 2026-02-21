package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/guillermoBallester/isthmus/internal/core/port"
	"github.com/jackc/pgx/v5/pgtype"
)

// AdminRepositoryAdapter implements port.AdminRepository using sqlc-generated queries.
type AdminRepositoryAdapter struct {
	queries *Queries
}

// NewAdminRepository creates a new AdminRepositoryAdapter.
func NewAdminRepository(queries *Queries) *AdminRepositoryAdapter {
	return &AdminRepositoryAdapter{queries: queries}
}

func (a *AdminRepositoryAdapter) CreateAPIKey(ctx context.Context, workspaceID, databaseID uuid.UUID, keyHash, keyPrefix, name string) (*port.APIKeyRecord, error) {
	wsID, err := toPgUUID(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace_id: %w", err)
	}

	dbID, err := toPgUUID(databaseID)
	if err != nil {
		return nil, fmt.Errorf("invalid database_id: %w", err)
	}

	row, err := a.queries.CreateAPIKey(ctx, CreateAPIKeyParams{
		WorkspaceID: wsID,
		DatabaseID:  dbID,
		KeyHash:     keyHash,
		KeyPrefix:   keyPrefix,
		Name:        name,
	})
	if err != nil {
		return nil, fmt.Errorf("inserting api key: %w", err)
	}

	return &port.APIKeyRecord{
		ID:          fromPgUUID(row.ID),
		KeyPrefix:   row.KeyPrefix,
		Name:        row.Name,
		WorkspaceID: fromPgUUID(row.WorkspaceID),
		DatabaseID:  fromPgUUID(row.DatabaseID),
		CreatedAt:   row.CreatedAt.Time,
	}, nil
}

func (a *AdminRepositoryAdapter) ListAPIKeys(ctx context.Context, workspaceID uuid.UUID) ([]port.APIKeyRecord, error) {
	wsID, err := toPgUUID(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace_id: %w", err)
	}

	rows, err := a.queries.ListAPIKeysByWorkspace(ctx, wsID)
	if err != nil {
		return nil, fmt.Errorf("listing api keys: %w", err)
	}

	result := make([]port.APIKeyRecord, 0, len(rows))
	for _, row := range rows {
		rec := port.APIKeyRecord{
			ID:         fromPgUUID(row.ID),
			KeyPrefix:  row.KeyPrefix,
			Name:       row.Name,
			DatabaseID: fromPgUUID(row.DatabaseID),
			CreatedAt:  row.CreatedAt.Time,
		}
		if row.ExpiresAt.Valid {
			t := row.ExpiresAt.Time
			rec.ExpiresAt = &t
		}
		if row.LastUsedAt.Valid {
			t := row.LastUsedAt.Time
			rec.LastUsedAt = &t
		}
		result = append(result, rec)
	}

	return result, nil
}

func (a *AdminRepositoryAdapter) DeleteAPIKey(ctx context.Context, id, workspaceID uuid.UUID) error {
	pgID, err := toPgUUID(id)
	if err != nil {
		return fmt.Errorf("invalid key id: %w", err)
	}
	wsID, err := toPgUUID(workspaceID)
	if err != nil {
		return fmt.Errorf("invalid workspace_id: %w", err)
	}

	return a.queries.DeleteAPIKey(ctx, DeleteAPIKeyParams{
		ID:          pgID,
		WorkspaceID: wsID,
	})
}

func (a *AdminRepositoryAdapter) CreateDatabase(ctx context.Context, workspaceID uuid.UUID, name, connType, status string, encryptedURL []byte) (*port.DatabaseInfo, error) {
	wsID, err := toPgUUID(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace_id: %w", err)
	}

	if len(encryptedURL) > 0 {
		row, err := a.queries.CreateDatabaseWithURL(ctx, CreateDatabaseWithURLParams{
			WorkspaceID:            wsID,
			Name:                   name,
			ConnectionType:         connType,
			EncryptedConnectionUrl: encryptedURL,
			Status:                 status,
		})
		if err != nil {
			return nil, fmt.Errorf("creating database: %w", err)
		}
		return &port.DatabaseInfo{
			ID:             fromPgUUID(row.ID),
			WorkspaceID:    fromPgUUID(row.WorkspaceID),
			Name:           row.Name,
			ConnectionType: row.ConnectionType,
			Status:         row.Status,
			CreatedAt:      row.CreatedAt.Time,
		}, nil
	}

	row, err := a.queries.CreateDatabase(ctx, CreateDatabaseParams{
		WorkspaceID:    wsID,
		Name:           name,
		ConnectionType: connType,
		Status:         status,
	})
	if err != nil {
		return nil, fmt.Errorf("creating database: %w", err)
	}
	return &port.DatabaseInfo{
		ID:             fromPgUUID(row.ID),
		WorkspaceID:    fromPgUUID(row.WorkspaceID),
		Name:           row.Name,
		ConnectionType: row.ConnectionType,
		Status:         row.Status,
		CreatedAt:      row.CreatedAt.Time,
	}, nil
}

func (a *AdminRepositoryAdapter) ListDatabases(ctx context.Context, workspaceID uuid.UUID) ([]port.DatabaseInfo, error) {
	wsID, err := toPgUUID(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace_id: %w", err)
	}

	rows, err := a.queries.ListDatabasesByWorkspace(ctx, wsID)
	if err != nil {
		return nil, fmt.Errorf("listing databases: %w", err)
	}

	result := make([]port.DatabaseInfo, 0, len(rows))
	for _, row := range rows {
		result = append(result, port.DatabaseInfo{
			ID:             fromPgUUID(row.ID),
			WorkspaceID:    fromPgUUID(row.WorkspaceID),
			Name:           row.Name,
			ConnectionType: row.ConnectionType,
			Status:         row.Status,
			CreatedAt:      row.CreatedAt.Time,
		})
	}

	return result, nil
}

func (a *AdminRepositoryAdapter) DeleteDatabase(ctx context.Context, id, workspaceID uuid.UUID) error {
	pgID, err := toPgUUID(id)
	if err != nil {
		return fmt.Errorf("invalid database id: %w", err)
	}
	wsID, err := toPgUUID(workspaceID)
	if err != nil {
		return fmt.Errorf("invalid workspace_id: %w", err)
	}

	return a.queries.DeleteDatabase(ctx, DeleteDatabaseParams{
		ID:          pgID,
		WorkspaceID: wsID,
	})
}

// toPgUUID converts a uuid.UUID to pgtype.UUID.
func toPgUUID(id uuid.UUID) (pgtype.UUID, error) {
	var pg pgtype.UUID
	if err := pg.Scan(id.String()); err != nil {
		return pg, err
	}
	return pg, nil
}

// fromPgUUID converts a pgtype.UUID to uuid.UUID, returning uuid.Nil on failure.
func fromPgUUID(pg pgtype.UUID) uuid.UUID {
	if !pg.Valid {
		return uuid.Nil
	}
	id, err := uuid.FromBytes(pg.Bytes[:])
	if err != nil {
		return uuid.Nil
	}
	return id
}

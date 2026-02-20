package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/guillermoBallester/isthmus/internal/core/port"
	"github.com/jackc/pgx/v5/pgtype"
)

// DatabaseRepositoryAdapter implements port.DatabaseRepository using sqlc-generated queries.
type DatabaseRepositoryAdapter struct {
	queries *Queries
}

// NewDatabaseRepository creates a new DatabaseRepositoryAdapter.
func NewDatabaseRepository(queries *Queries) *DatabaseRepositoryAdapter {
	return &DatabaseRepositoryAdapter{queries: queries}
}

// GetDatabaseByID loads a database record by ID, converting from pgtype to domain types.
func (a *DatabaseRepositoryAdapter) GetDatabaseByID(ctx context.Context, id uuid.UUID) (*port.DatabaseRecord, error) {
	var pgID pgtype.UUID
	if err := pgID.Scan(id.String()); err != nil {
		return nil, fmt.Errorf("invalid database id: %w", err)
	}

	db, err := a.queries.GetDatabaseByID(ctx, pgID)
	if err != nil {
		return nil, fmt.Errorf("querying database: %w", err)
	}

	return &port.DatabaseRecord{
		ID:                     id,
		Name:                   db.Name,
		ConnectionType:         db.ConnectionType,
		EncryptedConnectionURL: db.EncryptedConnectionUrl,
	}, nil
}

// UpdateDatabaseStatus updates the status field of a database record.
func (a *DatabaseRepositoryAdapter) UpdateDatabaseStatus(ctx context.Context, id uuid.UUID, status string) error {
	var pgID pgtype.UUID
	if err := pgID.Scan(id.String()); err != nil {
		return fmt.Errorf("invalid database id: %w", err)
	}

	return a.queries.UpdateDatabaseStatus(ctx, UpdateDatabaseStatusParams{
		Status: status,
		ID:     pgID,
	})
}

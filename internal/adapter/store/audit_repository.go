package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/guillermoBallester/isthmus/internal/core/port"
	"github.com/jackc/pgx/v5/pgtype"
)

// AuditRepositoryAdapter implements port.AuditRepository using sqlc-generated queries.
type AuditRepositoryAdapter struct {
	queries *Queries
}

// NewAuditRepository creates a new AuditRepositoryAdapter.
func NewAuditRepository(queries *Queries) *AuditRepositoryAdapter {
	return &AuditRepositoryAdapter{queries: queries}
}

func (a *AuditRepositoryAdapter) InsertBatch(ctx context.Context, entries []port.AuditEntry) error {
	for _, entry := range entries {
		wsID, err := toPgUUID(entry.WorkspaceID)
		if err != nil {
			return fmt.Errorf("invalid workspace_id: %w", err)
		}
		dbID, err := toPgUUID(entry.DatabaseID)
		if err != nil {
			return fmt.Errorf("invalid database_id: %w", err)
		}
		keyID, err := toPgUUID(entry.KeyID)
		if err != nil {
			return fmt.Errorf("invalid key_id: %w", err)
		}

		err = a.queries.InsertQueryLog(ctx, InsertQueryLogParams{
			WorkspaceID: wsID,
			DatabaseID:  dbID,
			KeyID:       keyID,
			ToolName:    entry.ToolName,
			ToolInput:   pgtype.Text{String: entry.ToolInput, Valid: entry.ToolInput != ""},
			DurationMs:  int32(entry.DurationMs),
			IsError:     entry.IsError,
		})
		if err != nil {
			return fmt.Errorf("inserting query log: %w", err)
		}
	}
	return nil
}

func (a *AuditRepositoryAdapter) ListQueryLogs(ctx context.Context, workspaceID uuid.UUID, databaseID *uuid.UUID, limit int) ([]port.QueryLogRecord, error) {
	wsID, err := toPgUUID(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace_id: %w", err)
	}

	var dbID pgtype.UUID
	if databaseID != nil {
		dbID, err = toPgUUID(*databaseID)
		if err != nil {
			return nil, fmt.Errorf("invalid database_id: %w", err)
		}
	}

	rows, err := a.queries.ListQueryLogs(ctx, ListQueryLogsParams{
		WorkspaceID: wsID,
		DatabaseID:  dbID,
		ResultLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("listing query logs: %w", err)
	}

	result := make([]port.QueryLogRecord, 0, len(rows))
	for _, row := range rows {
		rec := port.QueryLogRecord{
			ID:          fromPgUUID(row.ID),
			WorkspaceID: fromPgUUID(row.WorkspaceID),
			DatabaseID:  fromPgUUID(row.DatabaseID),
			KeyID:       fromPgUUID(row.KeyID),
			ToolName:    row.ToolName,
			DurationMs:  int(row.DurationMs),
			IsError:     row.IsError,
			CreatedAt:   row.CreatedAt.Time,
		}
		if row.ToolInput.Valid {
			rec.ToolInput = row.ToolInput.String
		}
		result = append(result, rec)
	}

	return result, nil
}

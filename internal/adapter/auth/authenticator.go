package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/guillermoBallester/isthmus/internal/adapter/store"
	"github.com/guillermoBallester/isthmus/internal/core/port"
	"github.com/jackc/pgx/v5"
)

// Authenticator validates tokens by hashing them and looking up the
// hash in the api_keys table via sqlc-generated queries.
type Authenticator struct {
	queries *store.Queries
	logger  *slog.Logger
}

// NewAuthenticator creates an authenticator backed by the api_keys table.
func NewAuthenticator(queries *store.Queries, logger *slog.Logger) *Authenticator {
	return &Authenticator{
		queries: queries,
		logger:  logger,
	}
}

// Authenticate hashes the token, looks it up in the database, and returns
// the key metadata including the single linked database.
func (a *Authenticator) Authenticate(ctx context.Context, token string) (*port.AuthResult, error) {
	hash := HashKey(token)

	row, err := a.queries.ValidateAPIKey(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // key not found
		}
		return nil, fmt.Errorf("validating api key: %w", err)
	}

	keyID, _ := uuid.FromBytes(row.ID.Bytes[:])
	wsID, _ := uuid.FromBytes(row.WorkspaceID.Bytes[:])

	var dbID uuid.UUID
	if row.DatabaseID.Valid {
		dbID, _ = uuid.FromBytes(row.DatabaseID.Bytes[:])
	}

	// Fire-and-forget: update last_used_at asynchronously to avoid
	// adding latency to the auth path.
	go func() {
		if err := a.queries.TouchAPIKeyLastUsed(context.Background(), row.ID); err != nil {
			a.logger.Warn("failed to update api key last_used_at",
				slog.String("error", err.Error()),
			)
		}
	}()

	return &port.AuthResult{
		KeyID:       keyID,
		WorkspaceID: wsID,
		DatabaseID:  dbID,
	}, nil
}

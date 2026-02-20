package auth

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/guillermoBallester/isthmus/internal/adapter/store"
)

// AuthResult contains metadata from a successful authentication.
// A nil result with nil error means the token was not found (invalid key).
type AuthResult struct {
	KeyID       uuid.UUID
	WorkspaceID uuid.UUID
	DatabaseIDs []uuid.UUID // databases this key has access to
}

// Authenticator validates a Bearer token from an incoming request.
type Authenticator interface {
	// Authenticate validates the token and returns metadata about the key.
	// Returns (nil, nil) when the token is not found.
	Authenticate(ctx context.Context, token string) (*AuthResult, error)
}

// SupabaseAuthenticator validates tokens by hashing them and looking up the
// hash in the api_keys table via sqlc-generated queries.
type SupabaseAuthenticator struct {
	queries *store.Queries
	logger  *slog.Logger
}

// NewSupabaseAuthenticator creates an authenticator backed by the api_keys table.
func NewSupabaseAuthenticator(queries *store.Queries, logger *slog.Logger) *SupabaseAuthenticator {
	return &SupabaseAuthenticator{
		queries: queries,
		logger:  logger,
	}
}

// Authenticate hashes the token, looks it up in the database, fetches the
// associated databases, and updates last_used_at on success.
func (a *SupabaseAuthenticator) Authenticate(ctx context.Context, token string) (*AuthResult, error) {
	hash := HashKey(token)

	row, err := a.queries.ValidateAPIKey(ctx, hash)
	if err != nil {
		// pgx returns no rows as an error — treat as "not found".
		return nil, nil
	}

	// Look up which databases this key has access to.
	dbRows, err := a.queries.GetAPIKeyDatabases(ctx, row.ID)
	if err != nil {
		return nil, err
	}

	dbIDs := make([]uuid.UUID, 0, len(dbRows))
	for _, dbRow := range dbRows {
		if dbRow.ID.Valid {
			id, err := uuid.FromBytes(dbRow.ID.Bytes[:])
			if err != nil {
				continue
			}
			dbIDs = append(dbIDs, id)
		}
	}

	keyID, _ := uuid.FromBytes(row.ID.Bytes[:])
	wsID, _ := uuid.FromBytes(row.WorkspaceID.Bytes[:])

	// Fire-and-forget: update last_used_at asynchronously to avoid
	// adding latency to the auth path.
	go func() {
		if err := a.queries.TouchAPIKeyLastUsed(context.Background(), row.ID); err != nil {
			a.logger.Warn("failed to update api key last_used_at",
				slog.String("error", err.Error()),
			)
		}
	}()

	return &AuthResult{
		KeyID:       keyID,
		WorkspaceID: wsID,
		DatabaseIDs: dbIDs,
	}, nil
}

package auth

import (
	"context"
	"log/slog"

	"github.com/guillermoBallester/isthmus/internal/store"
)

// Authenticator validates a Bearer token from an incoming request.
type Authenticator interface {
	// Authenticate returns true if the token is valid.
	Authenticate(ctx context.Context, token string) (bool, error)
}

// StaticAuthenticator validates tokens against an in-memory set of keys.
// Used for local development when no Supabase connection is configured.
type StaticAuthenticator struct {
	keys map[string]bool
}

// NewStaticAuthenticator creates an authenticator from a list of plaintext keys.
func NewStaticAuthenticator(keys []string) *StaticAuthenticator {
	keySet := make(map[string]bool, len(keys))
	for _, k := range keys {
		keySet[k] = true
	}
	return &StaticAuthenticator{keys: keySet}
}

// Authenticate checks if the token is in the static key set.
func (a *StaticAuthenticator) Authenticate(_ context.Context, token string) (bool, error) {
	return a.keys[token], nil
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

// Authenticate hashes the token, looks it up in the database, and updates
// last_used_at on success.
func (a *SupabaseAuthenticator) Authenticate(ctx context.Context, token string) (bool, error) {
	hash := HashKey(token)

	row, err := a.queries.ValidateAPIKey(ctx, hash)
	if err != nil {
		// pgx returns no rows as an error — treat as "not found".
		return false, nil
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

	return true, nil
}

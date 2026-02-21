package port

import (
	"context"

	"github.com/google/uuid"
)

// AuthResult contains metadata from a successful authentication.
// A nil result with nil error means the token was not found (invalid key).
type AuthResult struct {
	KeyID       uuid.UUID
	WorkspaceID uuid.UUID
	DatabaseID  uuid.UUID // the single database this key is linked to
}

// Authenticator validates a Bearer token from an incoming request.
type Authenticator interface {
	// Authenticate validates the token and returns metadata about the key.
	// Returns (nil, nil) when the token is not found.
	Authenticate(ctx context.Context, token string) (*AuthResult, error)
}

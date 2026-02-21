package port

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// APIKeyRecord is the domain representation of an API key.
type APIKeyRecord struct {
	ID          uuid.UUID
	KeyPrefix   string
	Name        string
	WorkspaceID uuid.UUID
	CreatedAt   time.Time
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
}

// DatabaseInfo is the domain representation of a database entry for admin operations.
type DatabaseInfo struct {
	ID             uuid.UUID
	WorkspaceID    uuid.UUID
	Name           string
	ConnectionType string
	Status         string
	CreatedAt      time.Time
}

// AdminRepository provides access to admin-managed resources (API keys, databases, linking).
type AdminRepository interface {
	// API keys
	CreateAPIKey(ctx context.Context, workspaceID uuid.UUID, keyHash, keyPrefix, name string) (*APIKeyRecord, error)
	ListAPIKeys(ctx context.Context, workspaceID uuid.UUID) ([]APIKeyRecord, error)
	DeleteAPIKey(ctx context.Context, id, workspaceID uuid.UUID) error

	// Databases
	CreateDatabase(ctx context.Context, workspaceID uuid.UUID, name, connType, status string, encryptedURL []byte) (*DatabaseInfo, error)
	ListDatabases(ctx context.Context, workspaceID uuid.UUID) ([]DatabaseInfo, error)
	DeleteDatabase(ctx context.Context, id, workspaceID uuid.UUID) error

	// Key-Database linking
	GrantKeyDatabase(ctx context.Context, keyID, databaseID uuid.UUID) error
	RevokeKeyDatabase(ctx context.Context, keyID, databaseID uuid.UUID) error
}

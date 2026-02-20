package port

import (
	"context"

	"github.com/google/uuid"
)

// DatabaseRecord is the domain representation of a database entry.
type DatabaseRecord struct {
	ID                     uuid.UUID
	Name                   string
	ConnectionType         string // "tunnel" or "direct"
	EncryptedConnectionURL []byte
}

// DatabaseRepository provides access to database metadata records.
type DatabaseRepository interface {
	GetDatabaseByID(ctx context.Context, id uuid.UUID) (*DatabaseRecord, error)
	UpdateDatabaseStatus(ctx context.Context, id uuid.UUID, status string) error
}

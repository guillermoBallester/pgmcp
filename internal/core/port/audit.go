package port

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AuditEntry represents a single MCP tool call audit record.
type AuditEntry struct {
	WorkspaceID uuid.UUID
	DatabaseID  uuid.UUID
	KeyID       uuid.UUID
	ToolName    string
	ToolInput   string
	DurationMs  int
	IsError     bool
}

// QueryLogRecord is the domain representation of a stored query log entry.
type QueryLogRecord struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	DatabaseID  uuid.UUID
	KeyID       uuid.UUID
	ToolName    string
	ToolInput   string
	DurationMs  int
	IsError     bool
	CreatedAt   time.Time
}

// AuditLogger accepts audit entries for asynchronous persistence.
type AuditLogger interface {
	// Log enqueues an audit entry for writing. Non-blocking.
	Log(entry AuditEntry)

	// Close flushes remaining entries and stops the background writer.
	Close()
}

// AuditRepository provides storage operations for audit log entries.
type AuditRepository interface {
	// InsertBatch writes multiple audit entries in a single operation.
	InsertBatch(ctx context.Context, entries []AuditEntry) error

	// ListQueryLogs retrieves audit logs with optional database filter.
	ListQueryLogs(ctx context.Context, workspaceID uuid.UUID, databaseID *uuid.UUID, limit int) ([]QueryLogRecord, error)
}

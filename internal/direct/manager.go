package direct

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/guillermoBallester/isthmus/internal/adapter/postgres"
	"github.com/guillermoBallester/isthmus/internal/crypto"
	"github.com/guillermoBallester/isthmus/internal/store"
	"github.com/guillermoBallester/isthmus/pkg/app"
	"github.com/guillermoBallester/isthmus/pkg/core/domain"
	"github.com/guillermoBallester/isthmus/pkg/core/service"
)

// Default safe limits for direct connections.
const (
	defaultMaxRows      = 1000
	defaultQueryTimeout = 30 * time.Second
)

type directEntry struct {
	pool      *pgxpool.Pool
	mcpServer *mcpserver.MCPServer
}

// Manager manages direct database connections, lazily created on first MCP request.
type Manager struct {
	queries       *store.Queries
	encryptionKey string
	version       string
	logger        *slog.Logger

	mu      sync.RWMutex
	entries map[uuid.UUID]*directEntry
}

// NewManager creates a new direct connection manager.
func NewManager(queries *store.Queries, encryptionKey, version string, logger *slog.Logger) *Manager {
	return &Manager{
		queries:       queries,
		encryptionKey: encryptionKey,
		version:       version,
		logger:        logger,
		entries:       make(map[uuid.UUID]*directEntry),
	}
}

// GetMCPServer returns the MCPServer for a direct database. On the first call
// for a given databaseID it loads the record, decrypts the URL, connects, and
// builds the full MCP stack (pool → adapters → services → app.NewServer).
func (m *Manager) GetMCPServer(ctx context.Context, databaseID uuid.UUID) (*mcpserver.MCPServer, error) {
	// Fast path: read lock.
	m.mu.RLock()
	if entry, ok := m.entries[databaseID]; ok {
		m.mu.RUnlock()
		return entry.mcpServer, nil
	}
	m.mu.RUnlock()

	// Slow path: write lock with double-check.
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.entries[databaseID]; ok {
		return entry.mcpServer, nil
	}

	// Load database record.
	var pgID pgtype.UUID
	_ = pgID.Scan(databaseID.String())

	db, err := m.queries.GetDatabaseByID(ctx, pgID)
	if err != nil {
		return nil, fmt.Errorf("loading database record: %w", err)
	}
	if db.ConnectionType != "direct" {
		return nil, fmt.Errorf("database %s is not a direct connection", databaseID)
	}
	if len(db.EncryptedConnectionUrl) == 0 {
		return nil, fmt.Errorf("database %s has no connection URL", databaseID)
	}

	// Decrypt connection URL.
	urlBytes, err := crypto.Decrypt(db.EncryptedConnectionUrl, m.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decrypting connection URL: %w", err)
	}

	// Create connection pool.
	pool, err := postgres.NewPool(ctx, string(urlBytes))
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	// Build MCP stack — same as the agent, reusing all existing components.
	explorer := postgres.NewExplorer(pool, nil)
	executor := postgres.NewExecutor(pool, true, defaultMaxRows, defaultQueryTimeout)
	explorerSvc := service.NewExplorerService(explorer)
	querySvc := service.NewQueryService(domain.NewQueryValidator(), executor, m.logger)
	mcpSrv := app.NewServer(m.version, explorerSvc, querySvc, m.logger)

	m.entries[databaseID] = &directEntry{
		pool:      pool,
		mcpServer: mcpSrv,
	}

	// Best-effort status update.
	_ = m.queries.UpdateDatabaseStatus(ctx, store.UpdateDatabaseStatusParams{
		Status: "connected",
		ID:     pgID,
	})

	m.logger.Info("direct connection established",
		slog.String("database_id", databaseID.String()),
		slog.String("database_name", db.Name),
	)

	return mcpSrv, nil
}

// Remove closes the pool and removes the cached entry for a database.
func (m *Manager) Remove(databaseID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.entries[databaseID]; ok {
		entry.pool.Close()
		delete(m.entries, databaseID)
		m.logger.Info("direct connection closed",
			slog.String("database_id", databaseID.String()),
		)
	}
}

// Close closes all cached connection pools.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, entry := range m.entries {
		entry.pool.Close()
		m.logger.Info("direct connection closed",
			slog.String("database_id", id.String()),
		)
	}
	m.entries = make(map[uuid.UUID]*directEntry)
}

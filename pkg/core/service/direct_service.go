package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"golang.org/x/sync/singleflight"

	"github.com/guillermoBallester/isthmus/internal/adapter/postgres"
	"github.com/guillermoBallester/isthmus/pkg/core/domain"
	"github.com/guillermoBallester/isthmus/pkg/core/ports"
)

// Default safe limits for direct connections.
const (
	directMaxRows      = 1000
	directQueryTimeout = 30 * time.Second
)

// MCPServerFactory creates an MCPServer from explorer and query services.
// Injected to avoid a circular dependency between service and app packages.
type MCPServerFactory func(explorer *ExplorerService, query *QueryService) *mcpserver.MCPServer

type directEntry struct {
	pool      *pgxpool.Pool
	mcpServer *mcpserver.MCPServer
}

// DirectConnectionService manages direct database connections, lazily created
// on the first MCP request. It depends on ports for database record lookup and
// decryption — no direct dependency on store or crypto packages.
type DirectConnectionService struct {
	repo       ports.DatabaseRepository
	encryptor  ports.Encryptor
	mcpFactory MCPServerFactory
	logger     *slog.Logger

	mu       sync.RWMutex
	entries  map[uuid.UUID]*directEntry
	inflight singleflight.Group
}

// NewDirectConnectionService creates a new service.
func NewDirectConnectionService(
	repo ports.DatabaseRepository,
	encryptor ports.Encryptor,
	mcpFactory MCPServerFactory,
	logger *slog.Logger,
) *DirectConnectionService {
	return &DirectConnectionService{
		repo:       repo,
		encryptor:  encryptor,
		mcpFactory: mcpFactory,
		logger:     logger,
		entries:    make(map[uuid.UUID]*directEntry),
	}
}

// GetMCPServer returns the MCPServer for a direct database. On the first call
// for a given databaseID it loads the record, decrypts the URL, connects, and
// builds the full MCP stack. Concurrent requests for the same database are
// deduplicated via singleflight — the lock is never held during network I/O.
func (s *DirectConnectionService) GetMCPServer(ctx context.Context, databaseID uuid.UUID) (*mcpserver.MCPServer, error) {
	// Fast path: check cache under read lock.
	s.mu.RLock()
	if entry, ok := s.entries[databaseID]; ok {
		s.mu.RUnlock()
		return entry.mcpServer, nil
	}
	s.mu.RUnlock()

	// Slow path: use singleflight to deduplicate concurrent connection attempts.
	// This runs WITHOUT holding the lock — no blocking other databases.
	key := databaseID.String()
	result, err, _ := s.inflight.Do(key, func() (any, error) {
		// Double-check cache (another goroutine may have completed while we waited).
		s.mu.RLock()
		if entry, ok := s.entries[databaseID]; ok {
			s.mu.RUnlock()
			return entry.mcpServer, nil
		}
		s.mu.RUnlock()

		return s.connect(ctx, databaseID)
	})
	if err != nil {
		return nil, err
	}

	return result.(*mcpserver.MCPServer), nil
}

// connect loads the database record, decrypts the URL, and builds the MCP stack.
// Called within singleflight — never under lock.
func (s *DirectConnectionService) connect(ctx context.Context, databaseID uuid.UUID) (*mcpserver.MCPServer, error) {
	db, err := s.repo.GetDatabaseByID(ctx, databaseID)
	if err != nil {
		return nil, fmt.Errorf("loading database record: %w", err)
	}
	if db.ConnectionType != "direct" {
		return nil, fmt.Errorf("database %s is not a direct connection", databaseID)
	}
	if len(db.EncryptedConnectionURL) == 0 {
		return nil, fmt.Errorf("database %s has no connection URL", databaseID)
	}

	urlBytes, err := s.encryptor.Decrypt(db.EncryptedConnectionURL)
	if err != nil {
		return nil, fmt.Errorf("decrypting connection URL: %w", err)
	}

	pool, err := postgres.NewPool(ctx, string(urlBytes))
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	// Build MCP stack — reuses the same components as the agent.
	explorer := postgres.NewExplorer(pool, nil)
	executor := postgres.NewExecutor(pool, true, directMaxRows, directQueryTimeout)
	explorerSvc := NewExplorerService(explorer)
	querySvc := NewQueryService(domain.NewQueryValidator(), executor, s.logger)
	mcpSrv := s.mcpFactory(explorerSvc, querySvc)

	// Cache under write lock.
	s.mu.Lock()
	s.entries[databaseID] = &directEntry{pool: pool, mcpServer: mcpSrv}
	s.mu.Unlock()

	// Best-effort status update.
	_ = s.repo.UpdateDatabaseStatus(ctx, databaseID, "connected")

	s.logger.Info("direct connection established",
		slog.String("database_id", databaseID.String()),
		slog.String("database_name", db.Name),
	)

	return mcpSrv, nil
}

// Remove closes the pool and removes the cached entry for a database.
func (s *DirectConnectionService) Remove(databaseID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.entries[databaseID]; ok {
		entry.pool.Close()
		delete(s.entries, databaseID)
		s.logger.Info("direct connection closed",
			slog.String("database_id", databaseID.String()),
		)
	}
}

// Close closes all cached connection pools.
func (s *DirectConnectionService) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, entry := range s.entries {
		entry.pool.Close()
		s.logger.Info("direct connection closed",
			slog.String("database_id", id.String()),
		)
	}
	s.entries = make(map[uuid.UUID]*directEntry)
}

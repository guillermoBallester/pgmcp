package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guillermoBallester/isthmus/internal/core/port"
)

// --- mock DatabaseRepository ---

type mockDBRepo struct {
	record *port.DatabaseRecord
	err    error
}

func (m *mockDBRepo) GetDatabaseByID(_ context.Context, _ uuid.UUID) (*port.DatabaseRecord, error) {
	return m.record, m.err
}

func (m *mockDBRepo) UpdateDatabaseStatus(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}

// --- mock MCPServerFactory ---

func testMCPFactory(explorer *ExplorerService, query *QueryService) *mcpserver.MCPServer {
	return mcpserver.NewMCPServer("test", "0.1.0")
}

// --- helper to build a pool that never actually connects ---

func newDummyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig("host=localhost dbname=dummy_test_never_connect")
	require.NoError(t, err)
	// MinConns=0 ensures no connections are eagerly created.
	config.MinConns = 0
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	require.NoError(t, err)
	return pool
}

// --- helper to build the service with pre-populated entries ---

func newTestDirectService(t *testing.T) *DirectConnectionService {
	t.Helper()
	return NewDirectConnectionService(
		&mockDBRepo{},
		&mockEncryptor{},
		testMCPFactory,
		10*time.Minute,
		testLogger(),
	)
}

// --- tests ---

func TestDirectService_GetMCPServer_CachedEntry(t *testing.T) {
	svc := newTestDirectService(t)
	defer svc.Close()

	dbID := uuid.New()
	pool := newDummyPool(t)
	mcpSrv := mcpserver.NewMCPServer("cached", "0.1.0")

	// Pre-populate cache.
	entry := &directEntry{pool: pool, mcpServer: mcpSrv}
	entry.lastAccess.Store(time.Now().UnixNano())
	svc.mu.Lock()
	svc.entries[dbID] = entry
	svc.mu.Unlock()

	// Should return the cached server without hitting repo.
	got, err := svc.GetMCPServer(context.Background(), dbID)
	require.NoError(t, err)
	assert.Equal(t, mcpSrv, got)

	// lastAccess should have been updated.
	assert.Greater(t, entry.lastAccess.Load(), int64(0))
}

func TestDirectService_GetMCPServer_RepoError(t *testing.T) {
	svc := NewDirectConnectionService(
		&mockDBRepo{err: fmt.Errorf("not found")},
		&mockEncryptor{},
		testMCPFactory,
		10*time.Minute,
		testLogger(),
	)
	defer svc.Close()

	_, err := svc.GetMCPServer(context.Background(), uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDirectService_GetMCPServer_NotDirect(t *testing.T) {
	dbID := uuid.New()
	svc := NewDirectConnectionService(
		&mockDBRepo{record: &port.DatabaseRecord{
			ID:             dbID,
			ConnectionType: "tunnel",
		}},
		&mockEncryptor{},
		testMCPFactory,
		10*time.Minute,
		testLogger(),
	)
	defer svc.Close()

	_, err := svc.GetMCPServer(context.Background(), dbID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a direct connection")
}

func TestDirectService_GetMCPServer_NoURL(t *testing.T) {
	dbID := uuid.New()
	svc := NewDirectConnectionService(
		&mockDBRepo{record: &port.DatabaseRecord{
			ID:                     dbID,
			ConnectionType:         "direct",
			EncryptedConnectionURL: nil,
		}},
		&mockEncryptor{},
		testMCPFactory,
		10*time.Minute,
		testLogger(),
	)
	defer svc.Close()

	_, err := svc.GetMCPServer(context.Background(), dbID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no connection URL")
}

func TestDirectService_GetMCPServer_DecryptError(t *testing.T) {
	dbID := uuid.New()
	svc := NewDirectConnectionService(
		&mockDBRepo{record: &port.DatabaseRecord{
			ID:                     dbID,
			ConnectionType:         "direct",
			EncryptedConnectionURL: []byte("encrypted"),
		}},
		&mockEncryptor{decryptErr: fmt.Errorf("bad key")},
		testMCPFactory,
		10*time.Minute,
		testLogger(),
	)
	defer svc.Close()

	_, err := svc.GetMCPServer(context.Background(), dbID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decrypting connection URL")
}

func TestDirectService_Remove(t *testing.T) {
	svc := newTestDirectService(t)
	defer svc.Close()

	dbID := uuid.New()
	pool := newDummyPool(t)
	entry := &directEntry{pool: pool, mcpServer: mcpserver.NewMCPServer("x", "0.1.0")}
	entry.lastAccess.Store(time.Now().UnixNano())

	svc.mu.Lock()
	svc.entries[dbID] = entry
	svc.mu.Unlock()

	svc.Remove(dbID)

	svc.mu.RLock()
	_, exists := svc.entries[dbID]
	svc.mu.RUnlock()
	assert.False(t, exists, "entry should be removed after Remove()")
}

func TestDirectService_Remove_Nonexistent(t *testing.T) {
	svc := newTestDirectService(t)
	defer svc.Close()

	// Should not panic when removing a non-existent entry.
	svc.Remove(uuid.New())
}

func TestDirectService_EvictIdle(t *testing.T) {
	svc := NewDirectConnectionService(
		&mockDBRepo{},
		&mockEncryptor{},
		testMCPFactory,
		100*time.Millisecond, // very short TTL for testing
		testLogger(),
	)
	defer svc.Close()

	dbActive := uuid.New()
	dbIdle := uuid.New()

	poolActive := newDummyPool(t)
	poolIdle := newDummyPool(t)

	entryActive := &directEntry{pool: poolActive, mcpServer: mcpserver.NewMCPServer("a", "0.1.0")}
	entryActive.lastAccess.Store(time.Now().UnixNano()) // fresh

	entryIdle := &directEntry{pool: poolIdle, mcpServer: mcpserver.NewMCPServer("b", "0.1.0")}
	entryIdle.lastAccess.Store(time.Now().Add(-time.Hour).UnixNano()) // old

	svc.mu.Lock()
	svc.entries[dbActive] = entryActive
	svc.entries[dbIdle] = entryIdle
	svc.mu.Unlock()

	svc.evictIdle()

	svc.mu.RLock()
	_, activeExists := svc.entries[dbActive]
	_, idleExists := svc.entries[dbIdle]
	svc.mu.RUnlock()

	assert.True(t, activeExists, "active entry should remain")
	assert.False(t, idleExists, "idle entry should be evicted")
}

func TestDirectService_Close(t *testing.T) {
	svc := newTestDirectService(t)

	db1 := uuid.New()
	db2 := uuid.New()
	pool1 := newDummyPool(t)
	pool2 := newDummyPool(t)

	entry1 := &directEntry{pool: pool1, mcpServer: mcpserver.NewMCPServer("1", "0.1.0")}
	entry1.lastAccess.Store(time.Now().UnixNano())
	entry2 := &directEntry{pool: pool2, mcpServer: mcpserver.NewMCPServer("2", "0.1.0")}
	entry2.lastAccess.Store(time.Now().UnixNano())

	svc.mu.Lock()
	svc.entries[db1] = entry1
	svc.entries[db2] = entry2
	svc.mu.Unlock()

	svc.Close()

	svc.mu.RLock()
	count := len(svc.entries)
	svc.mu.RUnlock()
	assert.Equal(t, 0, count, "all entries should be cleared after Close()")
}

func TestDirectService_CleanupLoop_EvictsIdle(t *testing.T) {
	ttl := 50 * time.Millisecond
	svc := NewDirectConnectionService(
		&mockDBRepo{},
		&mockEncryptor{},
		testMCPFactory,
		ttl,
		testLogger(),
	)
	defer svc.Close()

	dbID := uuid.New()
	pool := newDummyPool(t)

	entry := &directEntry{pool: pool, mcpServer: mcpserver.NewMCPServer("c", "0.1.0")}
	// Set lastAccess to the past so it's idle.
	entry.lastAccess.Store(time.Now().Add(-time.Hour).UnixNano())

	svc.mu.Lock()
	svc.entries[dbID] = entry
	svc.mu.Unlock()

	// Wait for the cleanup loop to fire (ticks at ttl/2 = 25ms).
	time.Sleep(ttl)

	svc.mu.RLock()
	_, exists := svc.entries[dbID]
	svc.mu.RUnlock()
	assert.False(t, exists, "idle entry should be evicted by cleanup loop")
}

func TestDirectService_ConcurrentGetMCPServer_Cached(t *testing.T) {
	svc := newTestDirectService(t)
	defer svc.Close()

	dbID := uuid.New()
	pool := newDummyPool(t)
	mcpSrv := mcpserver.NewMCPServer("shared", "0.1.0")

	entry := &directEntry{pool: pool, mcpServer: mcpSrv}
	entry.lastAccess.Store(time.Now().UnixNano())
	svc.mu.Lock()
	svc.entries[dbID] = entry
	svc.mu.Unlock()

	// Concurrent reads should all succeed without races.
	var ready, done atomic.Int32
	n := 10
	ready.Store(0)
	done.Store(0)

	for i := 0; i < n; i++ {
		go func() {
			ready.Add(1)
			// Spin until all goroutines are ready.
			for ready.Load() < int32(n) {
				// busy-wait
			}
			got, err := svc.GetMCPServer(context.Background(), dbID)
			assert.NoError(t, err)
			assert.Equal(t, mcpSrv, got)
			done.Add(1)
		}()
	}

	// Wait for all goroutines to finish.
	require.Eventually(t, func() bool { return done.Load() == int32(n) }, 2*time.Second, 10*time.Millisecond)
}

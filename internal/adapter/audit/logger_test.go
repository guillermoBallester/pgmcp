package audit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guillermoBallester/isthmus/internal/core/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"log/slog"
)

// --- mock AuditRepository ---

type mockAuditRepo struct {
	mu      sync.Mutex
	batches [][]port.AuditEntry
	err     error
}

func (m *mockAuditRepo) InsertBatch(_ context.Context, entries []port.AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	cp := make([]port.AuditEntry, len(entries))
	copy(cp, entries)
	m.batches = append(m.batches, cp)
	return nil
}

func (m *mockAuditRepo) ListQueryLogs(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ int) ([]port.QueryLogRecord, error) {
	return nil, nil
}

func (m *mockAuditRepo) totalEntries() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, b := range m.batches {
		n += len(b)
	}
	return n
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testEntry(tool string) port.AuditEntry {
	return port.AuditEntry{
		WorkspaceID: uuid.New(),
		DatabaseID:  uuid.New(),
		KeyID:       uuid.New(),
		ToolName:    tool,
		DurationMs:  10,
	}
}

// --- tests ---

func TestBatchLogger_FlushOnClose(t *testing.T) {
	repo := &mockAuditRepo{}
	l := NewBatchLogger(repo, testLogger())

	l.Log(testEntry("list_schemas"))
	l.Log(testEntry("query"))
	l.Close()

	assert.Equal(t, 2, repo.totalEntries())
}

func TestBatchLogger_FlushOnBatchSize(t *testing.T) {
	repo := &mockAuditRepo{}
	l := NewBatchLogger(repo, testLogger())
	defer l.Close()

	// Send exactly defaultBatchSize entries.
	for i := 0; i < defaultBatchSize; i++ {
		l.Log(testEntry("query"))
	}

	// Wait briefly for the flush to complete.
	require.Eventually(t, func() bool {
		return repo.totalEntries() >= defaultBatchSize
	}, 2*time.Second, 10*time.Millisecond)
}

func TestBatchLogger_FlushOnTicker(t *testing.T) {
	repo := &mockAuditRepo{}
	l := NewBatchLogger(repo, testLogger())
	defer l.Close()

	l.Log(testEntry("describe_table"))

	// Wait for the timer flush (defaultFlushTimeout = 5s, but should flush within that).
	require.Eventually(t, func() bool {
		return repo.totalEntries() > 0
	}, defaultFlushTimeout+time.Second, 100*time.Millisecond)
}

func TestBatchLogger_DropOnFullChannel(t *testing.T) {
	repo := &mockAuditRepo{}
	l := NewBatchLogger(repo, testLogger())
	// Don't defer Close — we want to test the non-blocking behavior.

	// Fill the channel.
	for i := 0; i < defaultChanBuffer+100; i++ {
		l.Log(testEntry("query"))
	}

	// No panic, no blocking — just some entries dropped.
	l.Close()

	// Should have flushed most entries (at least channel capacity).
	assert.GreaterOrEqual(t, repo.totalEntries(), defaultBatchSize)
}

package audit

import (
	"context"
	"log/slog"
	"time"

	"github.com/guillermoBallester/isthmus/internal/core/port"
)

const (
	defaultBatchSize    = 50
	defaultFlushTimeout = 5 * time.Second
	defaultChanBuffer   = 1000
)

// BatchLogger implements port.AuditLogger using a buffered channel and
// a background goroutine that batch-inserts entries into the database.
type BatchLogger struct {
	repo   port.AuditRepository
	ch     chan port.AuditEntry
	done   chan struct{}
	logger *slog.Logger
}

// NewBatchLogger creates a BatchLogger that writes audit entries to the
// given repository in batches. The background goroutine flushes when the
// batch is full or the flush interval elapses, whichever comes first.
func NewBatchLogger(repo port.AuditRepository, logger *slog.Logger) *BatchLogger {
	l := &BatchLogger{
		repo:   repo,
		ch:     make(chan port.AuditEntry, defaultChanBuffer),
		done:   make(chan struct{}),
		logger: logger,
	}
	go l.run()
	return l
}

// Log enqueues an audit entry. Non-blocking; drops the entry if the
// channel is full (backpressure safety).
func (l *BatchLogger) Log(entry port.AuditEntry) {
	select {
	case l.ch <- entry:
	default:
		l.logger.Warn("audit log channel full, dropping entry",
			slog.String("tool", entry.ToolName),
		)
	}
}

// Close signals the background goroutine to flush and exit.
// Blocks until all remaining entries are flushed.
func (l *BatchLogger) Close() {
	close(l.ch)
	<-l.done
}

func (l *BatchLogger) run() {
	defer close(l.done)

	batch := make([]port.AuditEntry, 0, defaultBatchSize)
	ticker := time.NewTicker(defaultFlushTimeout)
	defer ticker.Stop()

	for {
		select {
		case entry, ok := <-l.ch:
			if !ok {
				// Channel closed — flush remaining and exit.
				if len(batch) > 0 {
					l.flush(batch)
				}
				return
			}
			batch = append(batch, entry)
			if len(batch) >= defaultBatchSize {
				l.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				l.flush(batch)
				batch = batch[:0]
			}
		}
	}
}

func (l *BatchLogger) flush(batch []port.AuditEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := l.repo.InsertBatch(ctx, batch); err != nil {
		l.logger.Error("failed to flush audit log batch",
			slog.Int("count", len(batch)),
			slog.String("error", err.Error()),
		)
	}
}

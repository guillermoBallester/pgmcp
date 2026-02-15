package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/guillermoballestersasso/pgmcp/internal/adapter/postgres"
	"github.com/guillermoballestersasso/pgmcp/internal/config"
	"github.com/guillermoballestersasso/pgmcp/pkg/app"
	"github.com/guillermoballestersasso/pgmcp/pkg/core/domain"
	"github.com/guillermoballestersasso/pgmcp/pkg/core/service"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Structured logging on stderr (stdout is reserved for MCP JSON-RPC).
	// Uses slog with OTEL semantic conventions so a future otelslog bridge
	// is a one-line handler swap.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	logger.Info("starting pgmcp",
		slog.String("log_level", cfg.LogLevel.String()),
		slog.Bool("read_only", cfg.ReadOnly),
		slog.Int("max_rows", cfg.MaxRows),
		slog.String("query_timeout", cfg.QueryTimeout.String()),
	)

	// Signal-aware context: first SIGTERM/SIGINT triggers graceful shutdown,
	// second signal force-kills. signal.NotifyContext handles the first;
	// we re-register below for the hard kill.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}

	logger.Info("database pool connected",
		slog.String("db.system", "postgresql"),
	)

	// Adapters (driven/outbound)
	explorer := postgres.NewExplorer(pool, cfg.Schemas)
	executor := postgres.NewExecutor(pool, cfg.ReadOnly, cfg.MaxRows, cfg.QueryTimeout)

	// Domain
	validator := domain.NewQueryValidator()

	// Services (application layer)
	explorerSvc := service.NewExplorerService(explorer, logger)
	querySvc := service.NewQueryService(validator, executor, logger)

	mcpServer := app.NewServer(explorerSvc, querySvc, logger)

	// Use StdioServer.Listen directly instead of ServeStdio so we control
	// the context lifecycle. When ctx is cancelled (signal received),
	// Listen returns and we run cleanup below.
	stdioServer := server.NewStdioServer(mcpServer)
	listenErr := stdioServer.Listen(ctx, os.Stdin, os.Stdout)

	// --- Shutdown sequence ---
	logger.Info("shutting down")

	// Give in-flight operations a bounded window to finish.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	// Second signal during shutdown = hard exit.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		select {
		case sig := <-sigCh:
			logger.Warn("forced shutdown",
				slog.String("signal", sig.String()),
			)
			os.Exit(1)
		case <-shutdownCtx.Done():
		}
	}()

	// Close the connection pool. This waits for acquired connections
	// to be released or the pool's close timeout to expire.
	pool.Close()
	logger.Info("database pool closed",
		slog.String("db.system", "postgresql"),
	)

	logger.Info("shutdown complete")
	return listenErr
}

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

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

	ctx := context.Background()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

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

	return server.ServeStdio(mcpServer)
}

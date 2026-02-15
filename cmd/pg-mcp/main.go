package main

import (
	"context"
	"fmt"
	"os"

	"github.com/guillermoballestersasso/pgmcp/internal/adapter/postgres"
	"github.com/guillermoballestersasso/pgmcp/internal/app"
	"github.com/guillermoballestersasso/pgmcp/internal/config"
	"github.com/guillermoballestersasso/pgmcp/internal/core/domain"
	"github.com/guillermoballestersasso/pgmcp/internal/core/service"
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

	ctx := context.Background()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	// Adapters (driven/outbound)
	explorer := postgres.NewExplorer(pool, cfg.Schemas)
	executor := postgres.NewExecutor(pool, cfg.ReadOnly, cfg.MaxRows, cfg.QueryTimeout)

	// Domain
	validator := domain.NewQueryValidator()

	// Services (application layer)
	explorerSvc := service.NewExplorerService(explorer)
	querySvc := service.NewQueryService(validator, executor)

	mcpServer := app.NewServer(explorerSvc, querySvc)

	return server.ServeStdio(mcpServer)
}

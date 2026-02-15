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
	itunnel "github.com/guillermoballestersasso/pgmcp/internal/tunnel"
	"github.com/guillermoballestersasso/pgmcp/pkg/app"
	"github.com/guillermoballestersasso/pgmcp/pkg/core/domain"
	"github.com/guillermoballestersasso/pgmcp/pkg/core/service"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadAgent()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Agent logs to stdout — no stdio MCP protocol to protect (uses tunnel instead).
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	logger.Info("starting pgmcp-agent",
		slog.String("log_level", cfg.LogLevel.String()),
		slog.Bool("read_only", cfg.ReadOnly),
		slog.Int("max_rows", cfg.MaxRows),
		slog.String("query_timeout", cfg.QueryTimeout.String()),
		slog.String("tunnel_url", cfg.TunnelURL),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}

	logger.Info("database pool connected",
		slog.String("db.system", "postgresql"),
	)

	// Adapters
	explorer := postgres.NewExplorer(pool, cfg.Schemas)
	executor := postgres.NewExecutor(pool, cfg.ReadOnly, cfg.MaxRows, cfg.QueryTimeout)

	// Domain
	validator := domain.NewQueryValidator()

	// Services
	explorerSvc := service.NewExplorerService(explorer, logger)
	querySvc := service.NewQueryService(validator, executor, logger)

	// MCP server with real tool handlers (same as standalone binary).
	mcpServer := app.NewServer(explorerSvc, querySvc, logger)

	// Tunnel agent — connects outbound to cloud server.
	agent := itunnel.NewAgent(cfg.TunnelURL, cfg.APIKey, mcpServer, logger)

	// Run blocks until ctx is cancelled.
	runErr := agent.Run(ctx)

	// --- Shutdown sequence ---
	logger.Info("shutting down")

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

	pool.Close()
	logger.Info("database pool closed",
		slog.String("db.system", "postgresql"),
	)

	logger.Info("shutdown complete")
	return runErr
}

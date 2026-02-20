package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/guillermoBallester/isthmus/internal/adapter/mcp"
	"github.com/guillermoBallester/isthmus/internal/adapter/postgres"
	"github.com/guillermoBallester/isthmus/internal/agent"
	"github.com/guillermoBallester/isthmus/internal/config"
	"github.com/guillermoBallester/isthmus/internal/core/domain"
	"github.com/guillermoBallester/isthmus/internal/core/service"
	"github.com/guillermoBallester/isthmus/internal/protocol"
)

var version = "dev"

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

	logger.Info("starting isthmus-agent",
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
	explorerSvc := service.NewExplorerService(explorer)
	querySvc := service.NewQueryService(validator, executor, logger)

	// MCP server with real tool handlers (same as standalone binary).
	mcpServer := mcp.NewServer(version, explorerSvc, querySvc, logger)

	// Tunnel agent — connects outbound to cloud server.
	tunnelCfg := protocol.AgentTunnelConfig{
		SessionTTL:             cfg.SessionTTL,
		SessionCleanupInterval: cfg.SessionCleanupInterval,
		InitialBackoff:         cfg.ReconnectInitialBackoff,
		MaxBackoff:             cfg.ReconnectMaxBackoff,
		ForceCloseTimeout:      cfg.ForceCloseTimeout,
		Yamux: protocol.YamuxConfig{
			KeepAliveInterval:      cfg.YamuxKeepAliveInterval,
			ConnectionWriteTimeout: cfg.YamuxWriteTimeout,
		},
	}
	agent := agent.NewAgent(cfg.TunnelURL, cfg.APIKey, version, mcpServer, tunnelCfg, logger)

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
		case <-ctx.Done():
		}
	}()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return agent.Run(ctx)
	})

	// Shutdown trigger: drain in-flight handlers when ctx is cancelled.
	g.Go(func() error {
		<-ctx.Done()
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.DrainTimeout)
		defer cancel()
		if err := agent.Shutdown(shutdownCtx); err != nil {
			logger.Warn("drain did not complete", slog.String("error", err.Error()))
		}
		pool.Close()
		logger.Info("database pool closed", slog.String("db.system", "postgresql"))
		return nil
	})

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	logger.Info("shutdown complete")
	return nil
}

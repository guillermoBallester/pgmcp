package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"golang.org/x/sync/errgroup"

	"github.com/guillermoBallester/isthmus/internal/adapter/store"
	"github.com/guillermoBallester/isthmus/internal/adapter/store/migrations"
	"github.com/guillermoBallester/isthmus/internal/auth"
	"github.com/guillermoBallester/isthmus/internal/config"
	"github.com/guillermoBallester/isthmus/internal/direct"
	"github.com/guillermoBallester/isthmus/internal/server"
	itunnel "github.com/guillermoBallester/isthmus/internal/tunnel"
	"github.com/guillermoBallester/isthmus/pkg/tunnel"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadServer()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	logger.Info("starting isthmus-server",
		slog.String("log_level", cfg.LogLevel.String()),
		slog.String("listen_addr", cfg.ListenAddr),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Build authenticator based on configuration.
	var (
		authenticator auth.Authenticator
		queries       *store.Queries
		pool          *pgxpool.Pool
	)

	if cfg.SupabaseDBURL != "" {
		pool, err = pgxpool.New(ctx, cfg.SupabaseDBURL)
		if err != nil {
			return fmt.Errorf("connecting to supabase: %w", err)
		}
		defer pool.Close()

		logger.Info("supabase database connected")

		// Run goose migrations.
		if err := runMigrations(cfg.SupabaseDBURL); err != nil {
			return fmt.Errorf("running migrations: %w", err)
		}
		logger.Info("database migrations applied")

		queries = store.New(pool)
		supaAuth := auth.NewSupabaseAuthenticator(queries, logger)
		authenticator = supaAuth

		logger.Info("using supabase authenticator (multi-tenant)")

		if cfg.ClerkWebhookSecret != "" {
			logger.Info("clerk webhook handler enabled")
		}
	} else {
		authenticator = auth.NewStaticAuthenticator(cfg.APIKeys)
		logger.Info("using static api key authenticator",
			slog.Int("key_count", len(cfg.APIKeys)),
		)
	}

	// Tunnel config.
	tunnelCfg := tunnel.ServerTunnelConfig{
		Heartbeat: tunnel.HeartbeatConfig{
			Interval:      cfg.HeartbeatInterval,
			Timeout:       cfg.HeartbeatTimeout,
			MissThreshold: cfg.HeartbeatMissThreshold,
		},
		HandshakeTimeout: cfg.HandshakeTimeout,
		Yamux: tunnel.YamuxConfig{
			KeepAliveInterval:      cfg.YamuxKeepAliveInterval,
			ConnectionWriteTimeout: cfg.YamuxWriteTimeout,
		},
	}

	// TunnelRegistry — manages multiple simultaneous agent tunnels.
	// Each agent connects for a specific database and gets its own
	// MCPServer + Proxy. In static-key mode, uses StaticDatabaseID.
	registry := itunnel.NewTunnelRegistry(authenticator, tunnelCfg, version, logger)

	// DirectManager — manages direct database connections (no agent needed).
	// Only enabled when Supabase + encryption key are configured.
	var directMgr *direct.Manager
	if queries != nil && cfg.EncryptionKey != "" {
		directMgr = direct.NewManager(queries, cfg.EncryptionKey, version, logger)
		defer directMgr.Close()
		logger.Info("direct connection manager enabled")
	}

	// Cloud MCPServer — used as fallback for static-key mode only.
	// In multi-tenant mode, per-database MCPServers are created by the registry.
	mcpSrv := mcpserver.NewMCPServer("isthmus-cloud", version,
		mcpserver.WithToolCapabilities(true),
	)

	// Clerk webhook handler (optional).
	var webhookHandler *server.WebhookHandler
	if pool != nil && cfg.ClerkWebhookSecret != "" {
		webhookHandler = server.NewWebhookHandler(pool, queries, cfg.ClerkWebhookSecret, logger)
	}

	// HTTP server with chi routing and middleware.
	srv := server.New(cfg.ListenAddr, registry, directMgr, mcpSrv, authenticator, config.HTTPConfig{
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}, queries, cfg.AdminSecret, cfg.CORSOrigin, cfg.EncryptionKey, webhookHandler, logger)

	// Second signal during shutdown = hard exit.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		select {
		case sig := <-sigCh:
			logger.Warn("forced shutdown", slog.String("signal", sig.String()))
			os.Exit(1)
		case <-ctx.Done():
		}
	}()

	g, ctx := errgroup.WithContext(ctx)

	// Component: HTTP server.
	g.Go(func() error {
		return srv.ListenAndServe()
	})

	// Shutdown trigger: when ctx is cancelled (signal or component failure),
	// gracefully stop the HTTP server.
	g.Go(func() error {
		<-ctx.Done()
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	})

	// Wait for all goroutines. The first non-nil error cancels ctx,
	// which triggers the shutdown goroutine.
	if err := g.Wait(); err != nil {
		return err
	}

	logger.Info("shutdown complete")
	return nil
}

// runMigrations applies goose migrations from the embedded migration files.
func runMigrations(dbURL string) error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("opening db for migrations: %w", err)
	}
	defer func() { _ = db.Close() }()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("setting goose dialect: %w", err)
	}

	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}

	return nil
}

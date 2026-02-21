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

	"github.com/guillermoBallester/isthmus/internal/adapter/auth"
	"github.com/guillermoBallester/isthmus/internal/adapter/crypto"
	"github.com/guillermoBallester/isthmus/internal/adapter/httpserver"
	"github.com/guillermoBallester/isthmus/internal/adapter/mcp"
	"github.com/guillermoBallester/isthmus/internal/adapter/store"
	"github.com/guillermoBallester/isthmus/internal/adapter/store/migrations"
	"github.com/guillermoBallester/isthmus/internal/config"
	"github.com/guillermoBallester/isthmus/internal/core/port"
	"github.com/guillermoBallester/isthmus/internal/core/service"
	itunnel "github.com/guillermoBallester/isthmus/internal/tunnel"
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

	// Database.
	pool, err := pgxpool.New(ctx, cfg.SupabaseDBURL)
	if err != nil {
		return fmt.Errorf("connecting to supabase: %w", err)
	}
	defer pool.Close()
	logger.Info("supabase database connected")

	if err := runMigrations(cfg.SupabaseDBURL); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}
	logger.Info("database migrations applied")

	queries := store.New(pool)

	// Auth adapter.
	authenticator := auth.NewAuthenticator(queries, logger)

	// Tunnel registry.
	registry := itunnel.NewTunnelRegistry(authenticator, cfg.TunnelConfig(), version, logger)

	// Direct connection service (optional).
	var (
		enc       port.Encryptor
		directSvc *service.DirectConnectionService
	)
	if cfg.EncryptionKey != "" {
		aesEnc, encErr := crypto.NewAESEncryptor(cfg.EncryptionKey)
		if encErr != nil {
			return fmt.Errorf("creating encryptor: %w", encErr)
		}
		enc = aesEnc

		repo := store.NewDatabaseRepository(queries)
		mcpFactory := func(explorer *service.ExplorerService, query *service.QueryService) *mcpserver.MCPServer {
			return mcp.NewServer(version, explorer, query, logger)
		}
		directSvc = service.NewDirectConnectionService(repo, enc, mcpFactory, cfg.DirectPoolIdleTTL, logger)
		defer directSvc.Close()
		logger.Info("direct connection service enabled")
	}

	// Admin service.
	adminRepo := store.NewAdminRepository(queries)
	adminSvc := service.NewAdminService(adminRepo, enc, directSvc, logger)

	// Webhook handler (optional).
	var webhookHandler *httpserver.WebhookHandler
	if cfg.ClerkWebhookSecret != "" {
		webhookHandler = httpserver.NewWebhookHandler(pool, queries, cfg.ClerkWebhookSecret, logger)
	}

	// HTTP server.
	srv := httpserver.New(httpserver.Config{
		ListenAddr:        cfg.ListenAddr,
		AdminSecret:       cfg.AdminSecret,
		CORSOrigin:        cfg.CORSOrigin,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}, registry, directSvc, authenticator, adminSvc, webhookHandler, logger)

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

	g.Go(func() error {
		return srv.ListenAndServe()
	})

	g.Go(func() error {
		<-ctx.Done()
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	})

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

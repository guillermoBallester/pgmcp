package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/guillermoballestersasso/pgmcp/internal/config"
	"github.com/guillermoballestersasso/pgmcp/internal/server"
	itunnel "github.com/guillermoballestersasso/pgmcp/internal/tunnel"
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

	logger.Info("starting pgmcp-server",
		slog.String("log_level", cfg.LogLevel.String()),
		slog.String("listen_addr", cfg.ListenAddr),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Cloud MCPServer — starts with zero tools. Tools are added dynamically
	// when the agent connects and removed when it disconnects.
	mcpSrv := mcpserver.NewMCPServer("pgmcp-cloud", version,
		mcpserver.WithToolCapabilities(true),
	)

	// Tunnel server — manages WebSocket connection to the agent.
	hbCfg := itunnel.HeartbeatConfig{
		Interval:      cfg.HeartbeatInterval,
		Timeout:       cfg.HeartbeatTimeout,
		MissThreshold: cfg.HeartbeatMissThreshold,
	}
	tunnelSrv := itunnel.NewTunnelServer(cfg.APIKeys, hbCfg, version, logger)

	// Proxy — discovers agent tools and registers proxy handlers on the cloud MCPServer.
	proxy := itunnel.NewProxy(tunnelSrv, mcpSrv, logger)
	proxy.Setup()

	// HTTP server with chi routing and middleware.
	srv := server.New(cfg.ListenAddr, tunnelSrv, mcpSrv, logger)
	errCh := srv.Start()

	// Wait for shutdown signal or server error.
	select {
	case <-ctx.Done():
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	}

	// --- Shutdown sequence ---
	logger.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Second signal during shutdown = hard exit.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		select {
		case sig := <-sigCh:
			logger.Warn("forced shutdown", slog.String("signal", sig.String()))
			os.Exit(1)
		case <-shutdownCtx.Done():
		}
	}()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", slog.String("error", err.Error()))
	}

	logger.Info("shutdown complete")
	return nil
}

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/guillermoballestersasso/pgmcp/internal/config"
	"github.com/guillermoballestersasso/pgmcp/internal/server"
	itunnel "github.com/guillermoballestersasso/pgmcp/internal/tunnel"
	"github.com/guillermoballestersasso/pgmcp/pkg/tunnel"
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
	tunnelSrv := itunnel.NewTunnelServer(cfg.APIKeys, tunnelCfg, version, logger)

	// Proxy — discovers agent tools and registers proxy handlers on the cloud MCPServer.
	proxy := itunnel.NewProxy(tunnelSrv, mcpSrv, tunnelCfg, logger)
	proxy.Setup()

	// HTTP server with chi routing and middleware.
	srv := server.New(cfg.ListenAddr, tunnelSrv, mcpSrv, config.HTTPConfig{
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}, logger)

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

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/guillermoballestersasso/pgmcp/internal/config"
	itunnel "github.com/guillermoballestersasso/pgmcp/internal/tunnel"
	"github.com/mark3labs/mcp-go/server"
)

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
	mcpServer := server.NewMCPServer("pgmcp-cloud", "0.1.0",
		server.WithToolCapabilities(true), // enable tools/changed notifications
	)

	// Tunnel server — manages WebSocket connection to the agent.
	tunnelServer := itunnel.NewTunnelServer(cfg.APIKeys, logger)

	// Proxy — discovers agent tools and registers proxy handlers on the cloud MCPServer.
	proxy := itunnel.NewProxy(tunnelServer, mcpServer, logger)
	proxy.Setup()

	// Streamable HTTP server — mcp-go handles sessions, SSE, protocol.
	streamableHTTP := server.NewStreamableHTTPServer(mcpServer)

	mux := http.NewServeMux()
	mux.Handle("/mcp", streamableHTTP)
	mux.HandleFunc("/tunnel", tunnelServer.HandleTunnel)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if tunnelServer.Connected() {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ok")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "no agent connected")
		}
	})

	httpServer := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: mux,
	}

	// Start HTTP server in background.
	errCh := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening",
			slog.String("addr", cfg.ListenAddr),
		)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

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
			logger.Warn("forced shutdown",
				slog.String("signal", sig.String()),
			)
			os.Exit(1)
		case <-shutdownCtx.Done():
		}
	}()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error",
			slog.String("error", err.Error()),
		)
	}

	logger.Info("shutdown complete")
	return nil
}

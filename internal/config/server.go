package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// ServerConfig holds configuration for the cloud server.
type ServerConfig struct {
	ListenAddr             string
	APIKeys                []string
	SupabaseDBURL          string
	AdminSecret            string
	CORSOrigin             string
	ClerkWebhookSecret     string
	LogLevel               slog.Level
	HeartbeatInterval      time.Duration
	HeartbeatTimeout       time.Duration
	HeartbeatMissThreshold int

	// Tunnel operational params.
	HandshakeTimeout  time.Duration
	ShutdownTimeout   time.Duration
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration

	// Yamux settings.
	YamuxKeepAliveInterval time.Duration
	YamuxWriteTimeout      time.Duration
}

// LoadServer loads server configuration from environment variables.
func LoadServer() (*ServerConfig, error) {
	cfg := &ServerConfig{
		ListenAddr:             ":8080",
		HeartbeatInterval:      10 * time.Second,
		HeartbeatTimeout:       5 * time.Second,
		HeartbeatMissThreshold: 3,
		HandshakeTimeout:       10 * time.Second,
		ShutdownTimeout:        10 * time.Second,
		ReadHeaderTimeout:      10 * time.Second,
		IdleTimeout:            120 * time.Second,
		YamuxKeepAliveInterval: 15 * time.Second,
		YamuxWriteTimeout:      10 * time.Second,
	}

	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}

	cfg.SupabaseDBURL = os.Getenv("SUPABASE_DB_URL")
	cfg.AdminSecret = os.Getenv("ADMIN_SECRET")
	cfg.CORSOrigin = os.Getenv("CORS_ORIGIN")
	cfg.ClerkWebhookSecret = os.Getenv("CLERK_WEBHOOK_SECRET")
	keysRaw := os.Getenv("API_KEYS")
	if keysRaw != "" {
		for _, k := range strings.Split(keysRaw, ",") {
			k = strings.TrimSpace(k)
			if k != "" {
				cfg.APIKeys = append(cfg.APIKeys, k)
			}
		}
	}

	// At least one auth method must be configured.
	if cfg.SupabaseDBURL == "" && len(cfg.APIKeys) == 0 {
		return nil, fmt.Errorf("either SUPABASE_DB_URL or API_KEYS must be set")
	}

	if cfg.SupabaseDBURL != "" && cfg.AdminSecret == "" {
		return nil, fmt.Errorf("ADMIN_SECRET is required when SUPABASE_DB_URL is set")
	}

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		level, err := parseLogLevel(v)
		if err != nil {
			return nil, err
		}
		cfg.LogLevel = level
	}

	if v := os.Getenv("HEARTBEAT_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid HEARTBEAT_INTERVAL: %w", err)
		}
		cfg.HeartbeatInterval = d
	}

	if v := os.Getenv("HEARTBEAT_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid HEARTBEAT_TIMEOUT: %w", err)
		}
		cfg.HeartbeatTimeout = d
	}

	if v := os.Getenv("HEARTBEAT_MISS_THRESHOLD"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid HEARTBEAT_MISS_THRESHOLD: %w", err)
		}
		cfg.HeartbeatMissThreshold = n
	}

	if v := os.Getenv("HANDSHAKE_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid HANDSHAKE_TIMEOUT: %w", err)
		}
		cfg.HandshakeTimeout = d
	}

	if v := os.Getenv("SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid SHUTDOWN_TIMEOUT: %w", err)
		}
		cfg.ShutdownTimeout = d
	}

	if v := os.Getenv("READ_HEADER_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid READ_HEADER_TIMEOUT: %w", err)
		}
		cfg.ReadHeaderTimeout = d
	}

	if v := os.Getenv("IDLE_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid IDLE_TIMEOUT: %w", err)
		}
		cfg.IdleTimeout = d
	}

	if v := os.Getenv("YAMUX_KEEPALIVE_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid YAMUX_KEEPALIVE_INTERVAL: %w", err)
		}
		cfg.YamuxKeepAliveInterval = d
	}

	if v := os.Getenv("YAMUX_WRITE_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid YAMUX_WRITE_TIMEOUT: %w", err)
		}
		cfg.YamuxWriteTimeout = d
	}

	return cfg, nil
}

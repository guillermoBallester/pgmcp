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

	keysRaw := os.Getenv("API_KEYS")
	if keysRaw == "" {
		return nil, fmt.Errorf("API_KEYS environment variable is required")
	}
	for _, k := range strings.Split(keysRaw, ",") {
		k = strings.TrimSpace(k)
		if k != "" {
			cfg.APIKeys = append(cfg.APIKeys, k)
		}
	}
	if len(cfg.APIKeys) == 0 {
		return nil, fmt.Errorf("API_KEYS must contain at least one key")
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

package config

import (
	"fmt"
	"os"
	"time"
)

// AgentConfig extends the base Config with tunnel-specific settings.
type AgentConfig struct {
	*Config
	TunnelURL    string
	APIKey       string
	DrainTimeout time.Duration

	// Tunnel operational params.
	SessionTTL              time.Duration
	SessionCleanupInterval  time.Duration
	ReconnectInitialBackoff time.Duration
	ReconnectMaxBackoff     time.Duration
	ForceCloseTimeout       time.Duration

	// Yamux settings.
	YamuxKeepAliveInterval time.Duration
	YamuxWriteTimeout      time.Duration
}

// LoadAgent loads agent configuration from environment variables.
// It reuses the base Config (DATABASE_URL, READ_ONLY, MAX_ROWS, etc.)
// and adds TUNNEL_URL and API_KEY.
func LoadAgent() (*AgentConfig, error) {
	base, err := Load()
	if err != nil {
		return nil, err
	}

	cfg := &AgentConfig{
		Config:                  base,
		TunnelURL:               os.Getenv("TUNNEL_URL"),
		APIKey:                  os.Getenv("API_KEY"),
		DrainTimeout:            5 * time.Second,
		SessionTTL:              10 * time.Minute,
		SessionCleanupInterval:  1 * time.Minute,
		ReconnectInitialBackoff: 1 * time.Second,
		ReconnectMaxBackoff:     30 * time.Second,
		ForceCloseTimeout:       30 * time.Second,
		YamuxKeepAliveInterval:  15 * time.Second,
		YamuxWriteTimeout:       10 * time.Second,
	}

	if v := os.Getenv("DRAIN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid DRAIN_TIMEOUT: %w", err)
		}
		cfg.DrainTimeout = d
	}

	if v := os.Getenv("SESSION_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid SESSION_TTL: %w", err)
		}
		cfg.SessionTTL = d
	}

	if v := os.Getenv("SESSION_CLEANUP_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid SESSION_CLEANUP_INTERVAL: %w", err)
		}
		cfg.SessionCleanupInterval = d
	}

	if v := os.Getenv("RECONNECT_INITIAL_BACKOFF"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid RECONNECT_INITIAL_BACKOFF: %w", err)
		}
		cfg.ReconnectInitialBackoff = d
	}

	if v := os.Getenv("RECONNECT_MAX_BACKOFF"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid RECONNECT_MAX_BACKOFF: %w", err)
		}
		cfg.ReconnectMaxBackoff = d
	}

	if v := os.Getenv("FORCE_CLOSE_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid FORCE_CLOSE_TIMEOUT: %w", err)
		}
		cfg.ForceCloseTimeout = d
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

	if cfg.TunnelURL == "" {
		return nil, fmt.Errorf("TUNNEL_URL environment variable is required")
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API_KEY environment variable is required")
	}

	return cfg, nil
}

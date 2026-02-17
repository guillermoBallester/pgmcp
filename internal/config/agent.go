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
}

// LoadAgent loads agent configuration from environment variables.
// It reuses the base Config (DATABASE_URL, READ_ONLY, MAX_ROWS, etc.)
// and adds TUNNEL_URL and API_KEY.
func LoadAgent() (*AgentConfig, error) {
	base, err := Load()
	if err != nil {
		return nil, err
	}

	drainTimeout := 5 * time.Second
	if v := os.Getenv("DRAIN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid DRAIN_TIMEOUT: %w", err)
		}
		drainTimeout = d
	}

	cfg := &AgentConfig{
		Config:       base,
		TunnelURL:    os.Getenv("TUNNEL_URL"),
		APIKey:       os.Getenv("API_KEY"),
		DrainTimeout: drainTimeout,
	}

	if cfg.TunnelURL == "" {
		return nil, fmt.Errorf("TUNNEL_URL environment variable is required")
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API_KEY environment variable is required")
	}

	return cfg, nil
}

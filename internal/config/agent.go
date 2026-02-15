package config

import (
	"fmt"
	"os"
)

// AgentConfig extends the base Config with tunnel-specific settings.
type AgentConfig struct {
	*Config
	TunnelURL string
	APIKey    string
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
		Config:    base,
		TunnelURL: os.Getenv("TUNNEL_URL"),
		APIKey:    os.Getenv("API_KEY"),
	}

	if cfg.TunnelURL == "" {
		return nil, fmt.Errorf("TUNNEL_URL environment variable is required")
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API_KEY environment variable is required")
	}

	return cfg, nil
}

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setAgentEnv sets the minimum env vars for a valid AgentConfig and returns
// a cleanup function that restores the original environment.
func setAgentEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("TUNNEL_URL", "ws://localhost:8080/tunnel")
	t.Setenv("API_KEY", "test-key")
}

func TestLoadAgent_Valid(t *testing.T) {
	setAgentEnv(t)

	cfg, err := LoadAgent()
	require.NoError(t, err)

	assert.Equal(t, "postgres://localhost/test", cfg.DatabaseURL)
	assert.Equal(t, "ws://localhost:8080/tunnel", cfg.TunnelURL)
	assert.Equal(t, "test-key", cfg.APIKey)
	// Verify base config defaults are inherited.
	assert.True(t, cfg.ReadOnly)
	assert.Equal(t, 100, cfg.MaxRows)
}

func TestLoadAgent_MissingDatabaseURL(t *testing.T) {
	// DATABASE_URL not set — base Load() should fail.
	t.Setenv("TUNNEL_URL", "ws://localhost:8080/tunnel")
	t.Setenv("API_KEY", "test-key")

	_, err := LoadAgent()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL")
}

func TestLoadAgent_MissingTunnelURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("API_KEY", "test-key")

	_, err := LoadAgent()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TUNNEL_URL")
}

func TestLoadAgent_MissingAPIKey(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("TUNNEL_URL", "ws://localhost:8080/tunnel")

	_, err := LoadAgent()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API_KEY")
}

func TestLoadAgent_InheritsBaseConfig(t *testing.T) {
	setAgentEnv(t)
	t.Setenv("READ_ONLY", "false")
	t.Setenv("MAX_ROWS", "500")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := LoadAgent()
	require.NoError(t, err)

	assert.False(t, cfg.ReadOnly)
	assert.Equal(t, 500, cfg.MaxRows)
}

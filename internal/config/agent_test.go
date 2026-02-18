package config

import (
	"testing"
	"time"

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

func TestLoadAgent_TunnelDefaults(t *testing.T) {
	setAgentEnv(t)

	cfg, err := LoadAgent()
	require.NoError(t, err)

	assert.Equal(t, 10*time.Minute, cfg.SessionTTL)
	assert.Equal(t, 1*time.Minute, cfg.SessionCleanupInterval)
	assert.Equal(t, 1*time.Second, cfg.ReconnectInitialBackoff)
	assert.Equal(t, 30*time.Second, cfg.ReconnectMaxBackoff)
	assert.Equal(t, 30*time.Second, cfg.ForceCloseTimeout)
	assert.Equal(t, 15*time.Second, cfg.YamuxKeepAliveInterval)
	assert.Equal(t, 10*time.Second, cfg.YamuxWriteTimeout)
}

func TestLoadAgent_TunnelOverrides(t *testing.T) {
	setAgentEnv(t)
	t.Setenv("SESSION_TTL", "5m")
	t.Setenv("SESSION_CLEANUP_INTERVAL", "30s")
	t.Setenv("RECONNECT_INITIAL_BACKOFF", "2s")
	t.Setenv("RECONNECT_MAX_BACKOFF", "1m")
	t.Setenv("FORCE_CLOSE_TIMEOUT", "45s")
	t.Setenv("YAMUX_KEEPALIVE_INTERVAL", "20s")
	t.Setenv("YAMUX_WRITE_TIMEOUT", "15s")

	cfg, err := LoadAgent()
	require.NoError(t, err)

	assert.Equal(t, 5*time.Minute, cfg.SessionTTL)
	assert.Equal(t, 30*time.Second, cfg.SessionCleanupInterval)
	assert.Equal(t, 2*time.Second, cfg.ReconnectInitialBackoff)
	assert.Equal(t, 1*time.Minute, cfg.ReconnectMaxBackoff)
	assert.Equal(t, 45*time.Second, cfg.ForceCloseTimeout)
	assert.Equal(t, 20*time.Second, cfg.YamuxKeepAliveInterval)
	assert.Equal(t, 15*time.Second, cfg.YamuxWriteTimeout)
}

func TestLoadAgent_InvalidDurations(t *testing.T) {
	envVars := []string{
		"SESSION_TTL",
		"SESSION_CLEANUP_INTERVAL",
		"RECONNECT_INITIAL_BACKOFF",
		"RECONNECT_MAX_BACKOFF",
		"FORCE_CLOSE_TIMEOUT",
		"YAMUX_KEEPALIVE_INTERVAL",
		"YAMUX_WRITE_TIMEOUT",
	}

	for _, env := range envVars {
		t.Run(env, func(t *testing.T) {
			setAgentEnv(t)
			t.Setenv(env, "not-a-duration")

			_, err := LoadAgent()
			require.Error(t, err)
			assert.Contains(t, err.Error(), env)
		})
	}
}

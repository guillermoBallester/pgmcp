package config

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadServer_Valid(t *testing.T) {
	t.Setenv("API_KEYS", "key1,key2")

	cfg, err := LoadServer()
	require.NoError(t, err)

	assert.Equal(t, ":8080", cfg.ListenAddr)
	assert.Equal(t, []string{"key1", "key2"}, cfg.APIKeys)
	assert.Equal(t, slog.LevelInfo, cfg.LogLevel)
}

func TestLoadServer_MissingAPIKeys(t *testing.T) {
	_, err := LoadServer()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API_KEYS")
}

func TestLoadServer_EmptyAPIKeys(t *testing.T) {
	t.Setenv("API_KEYS", "  ,  ,  ")

	_, err := LoadServer()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one key")
}

func TestLoadServer_CustomListenAddr(t *testing.T) {
	t.Setenv("API_KEYS", "k1")
	t.Setenv("LISTEN_ADDR", ":9090")

	cfg, err := LoadServer()
	require.NoError(t, err)

	assert.Equal(t, ":9090", cfg.ListenAddr)
}

func TestLoadServer_LogLevel(t *testing.T) {
	t.Setenv("API_KEYS", "k1")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := LoadServer()
	require.NoError(t, err)

	assert.Equal(t, slog.LevelDebug, cfg.LogLevel)
}

func TestLoadServer_InvalidLogLevel(t *testing.T) {
	t.Setenv("API_KEYS", "k1")
	t.Setenv("LOG_LEVEL", "bogus")

	_, err := LoadServer()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOG_LEVEL")
}

func TestLoadServer_TrimKeys(t *testing.T) {
	t.Setenv("API_KEYS", " key1 , key2 , key3 ")

	cfg, err := LoadServer()
	require.NoError(t, err)

	assert.Equal(t, []string{"key1", "key2", "key3"}, cfg.APIKeys)
}

func TestLoadServer_SingleKey(t *testing.T) {
	t.Setenv("API_KEYS", "only-key")

	cfg, err := LoadServer()
	require.NoError(t, err)

	assert.Equal(t, []string{"only-key"}, cfg.APIKeys)
}

func TestLoadServer_TunnelDefaults(t *testing.T) {
	t.Setenv("API_KEYS", "k1")

	cfg, err := LoadServer()
	require.NoError(t, err)

	assert.Equal(t, 10*time.Second, cfg.HandshakeTimeout)
	assert.Equal(t, 10*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, 10*time.Second, cfg.ReadHeaderTimeout)
	assert.Equal(t, 120*time.Second, cfg.IdleTimeout)
	assert.Equal(t, 15*time.Second, cfg.YamuxKeepAliveInterval)
	assert.Equal(t, 10*time.Second, cfg.YamuxWriteTimeout)
}

func TestLoadServer_TunnelOverrides(t *testing.T) {
	t.Setenv("API_KEYS", "k1")
	t.Setenv("HANDSHAKE_TIMEOUT", "15s")
	t.Setenv("SHUTDOWN_TIMEOUT", "20s")
	t.Setenv("READ_HEADER_TIMEOUT", "5s")
	t.Setenv("IDLE_TIMEOUT", "60s")
	t.Setenv("YAMUX_KEEPALIVE_INTERVAL", "30s")
	t.Setenv("YAMUX_WRITE_TIMEOUT", "20s")

	cfg, err := LoadServer()
	require.NoError(t, err)

	assert.Equal(t, 15*time.Second, cfg.HandshakeTimeout)
	assert.Equal(t, 20*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, 5*time.Second, cfg.ReadHeaderTimeout)
	assert.Equal(t, 60*time.Second, cfg.IdleTimeout)
	assert.Equal(t, 30*time.Second, cfg.YamuxKeepAliveInterval)
	assert.Equal(t, 20*time.Second, cfg.YamuxWriteTimeout)
}

func TestLoadServer_InvalidDurations(t *testing.T) {
	envVars := []string{
		"HANDSHAKE_TIMEOUT",
		"SHUTDOWN_TIMEOUT",
		"READ_HEADER_TIMEOUT",
		"IDLE_TIMEOUT",
		"YAMUX_KEEPALIVE_INTERVAL",
		"YAMUX_WRITE_TIMEOUT",
	}

	for _, env := range envVars {
		t.Run(env, func(t *testing.T) {
			t.Setenv("API_KEYS", "k1")
			t.Setenv(env, "not-a-duration")

			_, err := LoadServer()
			require.Error(t, err)
			assert.Contains(t, err.Error(), env)
		})
	}
}

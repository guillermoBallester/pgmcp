package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadServer_Valid(t *testing.T) {
	t.Setenv("SUPABASE_DB_URL", "postgresql://localhost/test")
	t.Setenv("ADMIN_SECRET", "my-secret")

	cfg, err := LoadServer()
	require.NoError(t, err)

	assert.Equal(t, ":8080", cfg.ListenAddr)
	assert.Equal(t, "postgresql://localhost/test", cfg.SupabaseDBURL)
	assert.Equal(t, "my-secret", cfg.AdminSecret)
}

func TestLoadServer_MissingSupabaseDBURL(t *testing.T) {
	t.Setenv("ADMIN_SECRET", "my-secret")

	_, err := LoadServer()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SUPABASE_DB_URL")
}

func TestLoadServer_MissingAdminSecret(t *testing.T) {
	t.Setenv("SUPABASE_DB_URL", "postgresql://localhost/test")

	_, err := LoadServer()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ADMIN_SECRET")
}

func TestLoadServer_CustomListenAddr(t *testing.T) {
	t.Setenv("SUPABASE_DB_URL", "postgresql://localhost/test")
	t.Setenv("ADMIN_SECRET", "my-secret")
	t.Setenv("LISTEN_ADDR", ":9090")

	cfg, err := LoadServer()
	require.NoError(t, err)

	assert.Equal(t, ":9090", cfg.ListenAddr)
}

func TestLoadServer_LogLevel(t *testing.T) {
	t.Setenv("SUPABASE_DB_URL", "postgresql://localhost/test")
	t.Setenv("ADMIN_SECRET", "my-secret")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := LoadServer()
	require.NoError(t, err)

	assert.Equal(t, cfg.LogLevel.String(), "DEBUG")
}

func TestLoadServer_InvalidLogLevel(t *testing.T) {
	t.Setenv("SUPABASE_DB_URL", "postgresql://localhost/test")
	t.Setenv("ADMIN_SECRET", "my-secret")
	t.Setenv("LOG_LEVEL", "bogus")

	_, err := LoadServer()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOG_LEVEL")
}

func TestLoadServer_TunnelDefaults(t *testing.T) {
	t.Setenv("SUPABASE_DB_URL", "postgresql://localhost/test")
	t.Setenv("ADMIN_SECRET", "my-secret")

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
	t.Setenv("SUPABASE_DB_URL", "postgresql://localhost/test")
	t.Setenv("ADMIN_SECRET", "my-secret")
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
			t.Setenv("SUPABASE_DB_URL", "postgresql://localhost/test")
			t.Setenv("ADMIN_SECRET", "my-secret")
			t.Setenv(env, "not-a-duration")

			_, err := LoadServer()
			require.Error(t, err)
			assert.Contains(t, err.Error(), env)
		})
	}
}

package config

import (
	"log/slog"
	"testing"

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

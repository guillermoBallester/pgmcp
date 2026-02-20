package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateKey(t *testing.T) {
	fullKey, hash, prefix, err := GenerateKey()
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(fullKey, KeyPrefix))
	// ism_ (4) + 64 hex chars = 68
	assert.Len(t, fullKey, 68)

	// Hash should be a 64-char hex string (SHA-256).
	assert.Len(t, hash, 64)

	// Hashing the same key again should produce the same hash.
	assert.Equal(t, hash, HashKey(fullKey))

	// Display prefix should be truncated.
	assert.Contains(t, prefix, "...")
	assert.True(t, strings.HasPrefix(prefix, "ism_"))
}

func TestGenerateKeyUniqueness(t *testing.T) {
	key1, _, _, err := GenerateKey()
	require.NoError(t, err)
	key2, _, _, err := GenerateKey()
	require.NoError(t, err)

	assert.NotEqual(t, key1, key2)
}

func TestHashKeyDeterministic(t *testing.T) {
	key := "ism_abc123"
	h1 := HashKey(key)
	h2 := HashKey(key)
	assert.Equal(t, h1, h2)
}

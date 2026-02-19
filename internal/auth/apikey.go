package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const (
	// KeyPrefix is prepended to every generated API key so users and
	// secret-scanning tools can identify Isthmus keys.
	KeyPrefix = "ism_"

	// keyBytes is the number of random bytes used to generate a key.
	keyBytes = 32
)

// GenerateKey creates a new API key and returns the full plaintext key,
// its SHA-256 hash (for storage), and a display prefix (for UI).
//
// The full key is shown to the user exactly once. Only the hash is stored.
func GenerateKey() (fullKey, hash, displayPrefix string, err error) {
	buf := make([]byte, keyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf("generating random bytes: %w", err)
	}

	fullKey = KeyPrefix + hex.EncodeToString(buf)
	hash = HashKey(fullKey)
	displayPrefix = buildDisplayPrefix(fullKey)
	return fullKey, hash, displayPrefix, nil
}

// HashKey returns the hex-encoded SHA-256 digest of a plaintext API key.
func HashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// buildDisplayPrefix returns a safe-to-display version of the key:
// first 12 chars + "..." + last 4 chars.
func buildDisplayPrefix(key string) string {
	if len(key) <= 16 {
		return key
	}
	return key[:12] + "..." + key[len(key)-4:]
}

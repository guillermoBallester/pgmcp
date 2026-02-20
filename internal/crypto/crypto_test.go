package crypto

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validHexKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return hex.EncodeToString(key)
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	enc, err := NewEncryptor(validHexKey(t))
	require.NoError(t, err)

	plaintext := []byte("postgresql://user:pass@host:5432/db")

	ciphertext, err := enc.Encrypt(plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)

	decrypted, err := enc.Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	enc, err := NewEncryptor(validHexKey(t))
	require.NoError(t, err)

	plaintext := []byte("same input")

	ct1, err := enc.Encrypt(plaintext)
	require.NoError(t, err)

	ct2, err := enc.Encrypt(plaintext)
	require.NoError(t, err)

	assert.NotEqual(t, ct1, ct2, "different nonces should produce different ciphertexts")
}

func TestNewEncryptorBadKeyLength(t *testing.T) {
	_, err := NewEncryptor("aabbcc") // too short
	require.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func TestNewEncryptorInvalidHex(t *testing.T) {
	_, err := NewEncryptor("not-valid-hex")
	require.Error(t, err)
}

func TestDecryptTooShort(t *testing.T) {
	enc, err := NewEncryptor(validHexKey(t))
	require.NoError(t, err)

	_, err = enc.Decrypt([]byte{1, 2, 3})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too short")
}

func TestDecryptTampered(t *testing.T) {
	enc, err := NewEncryptor(validHexKey(t))
	require.NoError(t, err)

	ct, err := enc.Encrypt([]byte("secret"))
	require.NoError(t, err)

	ct[len(ct)-1] ^= 0xff // flip last byte
	_, err = enc.Decrypt(ct)
	require.Error(t, err)
}

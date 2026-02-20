package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// AESEncryptor provides AES-256-GCM encryption and decryption.
// It implements ports.Encryptor for the Decrypt method.
type AESEncryptor struct {
	gcm cipher.AEAD
}

// NewAESEncryptor creates a new AESEncryptor from a hex-encoded 256-bit key.
func NewAESEncryptor(hexKey string) (*AESEncryptor, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("decoding encryption key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}
	return &AESEncryptor{gcm: gcm}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM. Returns nonce || ciphertext.
func (e *AESEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	return e.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts ciphertext (nonce || ciphertext) using AES-256-GCM.
// Implements ports.Encryptor.
func (e *AESEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := e.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return e.gcm.Open(nil, nonce, ct, nil)
}

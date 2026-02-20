package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func testEncryptor(t *testing.T) *AESEncryptor {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	enc, err := NewAESEncryptor(hex.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

func TestRoundTrip(t *testing.T) {
	enc := testEncryptor(t)
	plaintext := []byte("postgresql://user:pass@host:5432/db")

	ct, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	got, err := enc.Decrypt(ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(got) != string(plaintext) {
		t.Fatalf("got %q, want %q", got, plaintext)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	enc1 := testEncryptor(t)
	enc2 := testEncryptor(t)

	ct, err := enc1.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = enc2.Decrypt(ct)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestDecryptTooShort(t *testing.T) {
	enc := testEncryptor(t)
	_, err := enc.Decrypt([]byte("short"))
	if err == nil {
		t.Fatal("expected error for short ciphertext")
	}
}

func TestInvalidHexKey(t *testing.T) {
	_, err := NewAESEncryptor("not-hex")
	if err == nil {
		t.Fatal("expected error for invalid hex key")
	}
}

func TestDifferentCiphertextsPerCall(t *testing.T) {
	enc := testEncryptor(t)
	plaintext := []byte("same input")

	ct1, _ := enc.Encrypt(plaintext)
	ct2, _ := enc.Encrypt(plaintext)

	if string(ct1) == string(ct2) {
		t.Fatal("two encryptions of same plaintext should produce different ciphertext")
	}
}

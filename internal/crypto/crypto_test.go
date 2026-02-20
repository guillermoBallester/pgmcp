package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func testKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(key)
}

func TestRoundTrip(t *testing.T) {
	key := testKey(t)
	plaintext := []byte("postgresql://user:pass@host:5432/db")

	ct, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	got, err := Decrypt(ct, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(got) != string(plaintext) {
		t.Fatalf("got %q, want %q", got, plaintext)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := testKey(t)
	key2 := testKey(t)

	ct, err := Encrypt([]byte("secret"), key1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Decrypt(ct, key2)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestDecryptTooShort(t *testing.T) {
	key := testKey(t)
	_, err := Decrypt([]byte("short"), key)
	if err == nil {
		t.Fatal("expected error for short ciphertext")
	}
}

func TestInvalidHexKey(t *testing.T) {
	_, err := Encrypt([]byte("test"), "not-hex")
	if err == nil {
		t.Fatal("expected error for invalid hex key")
	}
}

func TestDifferentCiphertextsPerCall(t *testing.T) {
	key := testKey(t)
	plaintext := []byte("same input")

	ct1, _ := Encrypt(plaintext, key)
	ct2, _ := Encrypt(plaintext, key)

	if string(ct1) == string(ct2) {
		t.Fatal("two encryptions of same plaintext should produce different ciphertext")
	}
}

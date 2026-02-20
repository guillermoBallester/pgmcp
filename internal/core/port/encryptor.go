package port

// Encryptor encrypts and decrypts data. Used by services and adapters
// without depending on a specific encryption implementation.
type Encryptor interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

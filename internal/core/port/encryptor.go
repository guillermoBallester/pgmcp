package port

// Encryptor decrypts data. Services use this port to decrypt stored secrets
// without depending on a specific encryption implementation.
type Encryptor interface {
	Decrypt(ciphertext []byte) ([]byte, error)
}

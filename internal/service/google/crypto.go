package google

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// googleTokenEncryptionKey is the AES-256 key used to encrypt refresh tokens at rest.
// It must be 32 bytes, provided as base64 in the GOOGLE_TOKEN_ENCRYPTION_KEY env var.
// shortcut: key is read once from env on first use. No key rotation support yet.
// Upgrade path: integrate with a KMS or secrets manager.
var googleTokenEncryptionKey []byte
var googleTokenEncryptionKeyOnce sync.Once

func getGoogleTokenEncryptionKey() ([]byte, error) {
	var err error
	googleTokenEncryptionKeyOnce.Do(func() {
		encoded := os.Getenv("GOOGLE_TOKEN_ENCRYPTION_KEY")
		if encoded == "" {
			err = errors.New("GOOGLE_TOKEN_ENCRYPTION_KEY env var is required")
			return
		}
		googleTokenEncryptionKey, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			err = fmt.Errorf("invalid GOOGLE_TOKEN_ENCRYPTION_KEY: %w", err)
			return
		}
		if len(googleTokenEncryptionKey) != 32 {
			err = errors.New("GOOGLE_TOKEN_ENCRYPTION_KEY must decode to 32 bytes")
			return
		}
	})
	return googleTokenEncryptionKey, err
}

// EncryptToken encrypts a plaintext token with AES-GCM and returns base64(nonce+ciphertext).
func EncryptToken(plaintext string) (string, error) {
	key, err := getGoogleTokenEncryptionKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptToken decrypts a base64(nonce+ciphertext) token back to plaintext.
func DecryptToken(ciphertext string) (string, error) {
	key, err := getGoogleTokenEncryptionKey()
	if err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

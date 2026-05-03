package kms

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

// KeyFetcher retrieves a data encryption key (DEK) from a key management service.
type KeyFetcher interface {
	// Fetch returns the plaintext DEK (32 bytes for AES-256) and a key ID
	// that is embedded in ciphertext for key rotation.
	Fetch(ctx context.Context) (dek []byte, keyID string, err error)
}

// StaticKeyFetcher reads the DEK from a base64-encoded string (typically an env var).
type StaticKeyFetcher struct {
	dek   []byte
	keyID string
}

// NewStaticKeyFetcher creates a StaticKeyFetcher from a base64-encoded 32-byte key.
func NewStaticKeyFetcher(encodedKey, keyID string) (*StaticKeyFetcher, error) {
	if encodedKey == "" {
		return nil, errors.New("kms: static key is empty")
	}
	dek, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, errors.New("kms: static key is not valid base64")
	}
	if len(dek) != 32 {
		return nil, errors.New("kms: static key must be 32 bytes (base64-encoded)")
	}
	return &StaticKeyFetcher{dek: dek, keyID: keyID}, nil
}

// Fetch returns the static DEK and key ID.
func (f *StaticKeyFetcher) Fetch(_ context.Context) ([]byte, string, error) {
	return f.dek, f.keyID, nil
}

// NonceSize is the standard nonce size for AES-GCM.
const nonceSize = 12

// Encrypt encrypts plaintext using AES-256-GCM with the given DEK.
// Output format: "dek-v1:" + base64(keyID) + ":" + base64(nonce + ciphertext)
func Encrypt(plaintext string, dek []byte, keyID string) (string, error) {
	aead, err := newGCM(dek)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	plainBytes := []byte(plaintext)
	ciphertext := aead.Seal(nil, nonce, plainBytes, nil)

	// nonce || ciphertext
	combined := append(nonce, ciphertext...)

	return "dek-v1:" + base64.StdEncoding.EncodeToString([]byte(keyID)) + ":" + base64.StdEncoding.EncodeToString(combined), nil
}

// Decrypt decrypts a ciphertext produced by Encrypt.
// dekByKID is called with the key ID from the ciphertext to retrieve the appropriate DEK.
func Decrypt(ciphertext string, dekByKID func(keyID string) ([]byte, error)) (string, error) {
	if ciphertext == "" {
		return "", errors.New("kms: empty ciphertext")
	}

	// Parse "dek-v1:base64(keyID):base64(nonce+ciphertext)"
	if len(ciphertext) < 7 || ciphertext[:7] != "dek-v1:" {
		return "", errors.New("kms: invalid ciphertext format (missing prefix)")
	}
	rest := ciphertext[7:]

	// Split on second colon
	idx := indexOfColon(rest)
	if idx < 0 {
		return "", errors.New("kms: invalid ciphertext format (missing key ID separator)")
	}

	keyIDB64 := rest[:idx]
	combinedB64 := rest[idx+1:]

	keyIDBytes, err := base64.StdEncoding.DecodeString(keyIDB64)
	if err != nil {
		return "", errors.New("kms: invalid key ID encoding")
	}
	keyID := string(keyIDBytes)

	combined, err := base64.StdEncoding.DecodeString(combinedB64)
	if err != nil {
		return "", errors.New("kms: invalid ciphertext encoding")
	}

	if len(combined) < nonceSize+1 {
		return "", errors.New("kms: ciphertext too short")
	}

	nonce := combined[:nonceSize]
	ct := combined[nonceSize:]

	dek, err := dekByKID(keyID)
	if err != nil {
		return "", err
	}

	aead, err := newGCM(dek)
	if err != nil {
		return "", err
	}

	plainBytes, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", errors.New("kms: decryption failed (wrong key or corrupted data)")
	}

	return string(plainBytes), nil
}

// indexOfColon returns the index of the first colon in s, or -1 if none.
func indexOfColon(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return i
		}
	}
	return -1
}

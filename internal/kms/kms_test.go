package kms

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
)

func TestStaticKeyFetcherValid(t *testing.T) {
	// Generate a valid 32-byte key
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	encoded := base64.StdEncoding.EncodeToString(key)

	fetcher, err := NewStaticKeyFetcher(encoded, "test-key-1")
	if err != nil {
		t.Fatalf("NewStaticKeyFetcher: %v", err)
	}

	dek, keyID, err := fetcher.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if keyID != "test-key-1" {
		t.Errorf("keyID = %q, want %q", keyID, "test-key-1")
	}
	if len(dek) != 32 {
		t.Errorf("len(dek) = %d, want 32", len(dek))
	}
}

func TestStaticKeyFetcherEmpty(t *testing.T) {
	_, err := NewStaticKeyFetcher("", "test")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestStaticKeyFetcherWrongLength(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("too-short"))
	_, err := NewStaticKeyFetcher(short, "test")
	if err == nil {
		t.Fatal("expected error for wrong-length key")
	}
}

func TestStaticKeyFetcherInvalidBase64(t *testing.T) {
	_, err := NewStaticKeyFetcher("not-base64!!!", "test")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	ciphertext, err := Encrypt("hello world", key, "key-v1")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	plaintext, err := Decrypt(ciphertext, func(keyID string) ([]byte, error) {
		return key, nil
	})
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plaintext != "hello world" {
		t.Errorf("plaintext = %q, want %q", plaintext, "hello world")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	for i := range key2 {
		key2[i] = byte(i + 1)
	}

	ciphertext, err := Encrypt("secret", key1, "key-v1")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	_, err = Decrypt(ciphertext, func(keyID string) ([]byte, error) {
		return key2, nil
	})
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestCiphertextFormat(t *testing.T) {
	key := make([]byte, 32)
	ciphertext, err := Encrypt("test", key, "my-key-id")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Must start with "dek-v1:"
	if len(ciphertext) < 7 || ciphertext[:7] != "dek-v1:" {
		t.Errorf("ciphertext doesn't start with dek-v1: %q", ciphertext)
	}

	// Must contain the key ID (base64-encoded)
	b64KeyID := base64.StdEncoding.EncodeToString([]byte("my-key-id"))
	rest := ciphertext[7:]
	found := false
	for i := 0; i < len(rest); i++ {
		if rest[i] == ':' {
			if rest[:i] == b64KeyID {
				found = true
			}
			break
		}
	}
	if !found {
		t.Errorf("ciphertext doesn't contain expected key ID: %q", ciphertext)
	}
}

func TestEmptyPlaintext(t *testing.T) {
	key := make([]byte, 32)
	ciphertext, err := Encrypt("", key, "key-v1")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	plaintext, err := Decrypt(ciphertext, func(keyID string) ([]byte, error) {
		return key, nil
	})
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plaintext != "" {
		t.Errorf("plaintext = %q, want empty", plaintext)
	}
}

func TestLargePlaintext(t *testing.T) {
	key := make([]byte, 32)
	large := make([]byte, 4096)
	for i := range large {
		large[i] = byte(i % 256)
	}
	plain := string(large)

	ciphertext, err := Encrypt(plain, key, "key-v1")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	decrypted, err := Decrypt(ciphertext, func(keyID string) ([]byte, error) {
		return key, nil
	})
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if decrypted != plain {
		t.Errorf("large plaintext mismatch: got %d bytes, want %d", len(decrypted), len(plain))
	}
}

func TestDecryptInvalidFormat(t *testing.T) {
	key := make([]byte, 32)
	resolver := func(keyID string) ([]byte, error) { return key, nil }

	cases := []struct {
		name string
		ct   string
	}{
		{"empty", ""},
		{"no prefix", "abc:def"},
		{"missing separator", "dek-v1:onlyonepart"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decrypt(tc.ct, resolver)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestKeyRotation(t *testing.T) {
	oldKey := make([]byte, 32)
	newKey := make([]byte, 32)
	for i := range newKey {
		newKey[i] = byte(i + 10)
	}

	// Encrypt with old key
	ct1, err := Encrypt("old-value", oldKey, "key-v1")
	if err != nil {
		t.Fatalf("Encrypt old: %v", err)
	}

	// Encrypt with new key
	ct2, err := Encrypt("new-value", newKey, "key-v2")
	if err != nil {
		t.Fatalf("Encrypt new: %v", err)
	}

	// Decryptor knows both keys
	keys := map[string][]byte{"key-v1": oldKey, "key-v2": newKey}
	resolver := func(keyID string) ([]byte, error) {
		k, ok := keys[keyID]
		if !ok {
			return nil, errUnknownKey
		}
		return k, nil
	}

	p1, err := Decrypt(ct1, resolver)
	if err != nil {
		t.Fatalf("Decrypt old: %v", err)
	}
	if p1 != "old-value" {
		t.Errorf("old decrypt = %q", p1)
	}

	p2, err := Decrypt(ct2, resolver)
	if err != nil {
		t.Fatalf("Decrypt new: %v", err)
	}
	if p2 != "new-value" {
		t.Errorf("new decrypt = %q", p2)
	}
}

var errUnknownKey = errors.New("unknown key ID")

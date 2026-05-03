package kms

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVaultKeyFetcher_HappyPath(t *testing.T) {
	// Create a valid 32-byte DEK
	dek := make([]byte, 32)
	for i := range dek {
		dek[i] = byte(i)
	}
	dekB64 := base64.StdEncoding.EncodeToString(dek)

	// Mock Vault server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "test-token" {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, `{"errors":["permission denied"]}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"data": {
				"data": {
					"key": "%s",
					"key_id": "v1"
				}
			}
		}`, dekB64)
	}))
	defer server.Close()

	fetcher, err := NewVaultKeyFetcher(server.URL, "test-token", "", "secret/data/openlimit-dek", false)
	if err != nil {
		t.Fatalf("NewVaultKeyFetcher: %v", err)
	}

	gotDEK, gotKeyID, err := fetcher.Fetch(nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if gotKeyID != "v1" {
		t.Errorf("keyID = %q, want %q", gotKeyID, "v1")
	}
	if len(gotDEK) != 32 {
		t.Fatalf("DEK length = %d, want 32", len(gotDEK))
	}
	for i, b := range gotDEK {
		if b != byte(i) {
			t.Errorf("DEK[%d] = %d, want %d", i, b, byte(i))
		}
	}
}

func TestVaultKeyFetcher_WrongKeyLength(t *testing.T) {
	// Return a 16-byte key instead of 32
	shortDEK := base64.StdEncoding.EncodeToString(make([]byte, 16))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"data": {
				"data": {
					"key": "%s",
					"key_id": "v1"
				}
			}
		}`, shortDEK)
	}))
	defer server.Close()

	fetcher, err := NewVaultKeyFetcher(server.URL, "test-token", "", "secret/data/test", false)
	if err != nil {
		t.Fatalf("NewVaultKeyFetcher: %v", err)
	}

	_, _, err = fetcher.Fetch(nil)
	if err == nil {
		t.Fatal("expected error for wrong key length")
	}
}

func TestVaultKeyFetcher_EmptyAddr(t *testing.T) {
	_, err := NewVaultKeyFetcher("", "test-token", "", "secret/data/test", false)
	if err == nil {
		t.Fatal("expected error for empty addr")
	}
}

func TestVaultKeyFetcher_403Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, `{"errors":["permission denied"]}`)
	}))
	defer server.Close()

	fetcher, err := NewVaultKeyFetcher(server.URL, "wrong-token", "", "secret/data/test", false)
	if err != nil {
		t.Fatalf("NewVaultKeyFetcher: %v", err)
	}

	_, _, err = fetcher.Fetch(nil)
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	// Verify no panic — graceful error handling
}

func TestVaultKeyFetcher_SecretNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	fetcher, err := NewVaultKeyFetcher(server.URL, "test-token", "", "secret/data/nonexistent", false)
	if err != nil {
		t.Fatalf("NewVaultKeyFetcher: %v", err)
	}

	_, _, err = fetcher.Fetch(nil)
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

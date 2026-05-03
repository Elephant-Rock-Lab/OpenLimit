package kms

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/hashicorp/vault/api"
)

// VaultKeyFetcher fetches a DEK from HashiCorp Vault KV v2.
type VaultKeyFetcher struct {
	client *api.Client
	path   string // full KV v2 logical path, e.g., "secret/data/openlimit-dek"
}

// NewVaultKeyFetcher creates a new Vault-based key fetcher.
// Token resolution: cfg.Token → VAULT_TOKEN env var → error if both empty.
func NewVaultKeyFetcher(addr, token, namespace, secretPath string, tlsSkipVerify bool) (*VaultKeyFetcher, error) {
	if addr == "" {
		return nil, fmt.Errorf("kms/vault: addr is required")
	}

	// Resolve token: config → env var
	if token == "" {
		token = os.Getenv("VAULT_TOKEN")
	}
	if token == "" {
		return nil, fmt.Errorf("kms/vault: token is required (set config or VAULT_TOKEN env var)")
	}

	config := api.DefaultConfig()
	config.Address = addr

	if tlsSkipVerify {
		//nolint:staticcheck // SA1019 — ConfigureTLS is the supported way
		if err := config.ConfigureTLS(&api.TLSConfig{Insecure: true}); err != nil {
			return nil, fmt.Errorf("kms/vault: TLS config: %w", err)
		}
	}

	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("kms/vault: create client: %w", err)
	}

	client.SetToken(token)

	if namespace != "" {
		client.SetNamespace(namespace)
	}

	return &VaultKeyFetcher{
		client: client,
		path:   secretPath,
	}, nil
}

// Fetch retrieves the DEK from Vault KV v2.
// The secret must contain "key" (base64-encoded 32-byte DEK) and "key_id" fields.
func (f *VaultKeyFetcher) Fetch(ctx context.Context) (dek []byte, keyID string, err error) {
	secret, err := f.client.Logical().Read(f.path)
	if err != nil {
		return nil, "", fmt.Errorf("kms/vault: read %s: %w", f.path, err)
	}
	if secret == nil {
		return nil, "", fmt.Errorf("kms/vault: secret not found at %s", f.path)
	}

	// KV v2 stores data under .Data["data"]
	data, ok := secret.Data["data"].(map[string]any)
	if !ok {
		return nil, "", fmt.Errorf("kms/vault: unexpected secret format at %s", f.path)
	}

	keyB64, _ := data["key"].(string)
	if keyB64 == "" {
		return nil, "", fmt.Errorf("kms/vault: missing 'key' field in secret at %s", f.path)
	}

	dek, err = base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, "", fmt.Errorf("kms/vault: invalid base64 key: %w", err)
	}

	if len(dek) != 32 {
		return nil, "", fmt.Errorf("kms/vault: DEK must be 32 bytes, got %d", len(dek))
	}

	keyID, _ = data["key_id"].(string)
	if keyID == "" {
		keyID = "default"
	}

	return dek, keyID, nil
}

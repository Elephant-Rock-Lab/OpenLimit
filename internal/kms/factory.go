package kms

import (
	"context"
	"fmt"
	"os"
)

// VaultFetcherConfig holds Vault-specific configuration for the factory.
type VaultFetcherConfig struct {
	Addr          string
	Token         string
	Namespace     string
	TLSSkipVerify bool
}

// NewKeyFetcher creates the appropriate KeyFetcher based on KMS config.
func NewKeyFetcher(kmsType, keyID string, vaultCfg VaultFetcherConfig) (KeyFetcher, error) {
	switch kmsType {
	case "static":
		staticKey := os.Getenv("KMS_STATIC_KEY")
		if staticKey == "" {
			return nil, fmt.Errorf("kms: KMS_STATIC_KEY environment variable is required for static KMS")
		}
		return NewStaticKeyFetcher(staticKey, keyID)
	case "aws-kms":
		return NewAWSKMSFetcher(keyID)
	case "vault":
		return NewVaultKeyFetcher(vaultCfg.Addr, vaultCfg.Token, vaultCfg.Namespace, keyID, vaultCfg.TLSSkipVerify)
	default:
		return nil, fmt.Errorf("kms: unsupported type %q", kmsType)
	}
}

// DecryptProviderKey decrypts an encrypted provider key value using the given fetcher.
// Returns the plaintext key or an error.
func DecryptProviderKey(encryptedValue string, fetcher KeyFetcher) (string, error) {
	dek, keyID, err := fetcher.Fetch(context.Background())
	if err != nil {
		return "", fmt.Errorf("kms: fetch DEK: %w", err)
	}

	plaintext, err := Decrypt(encryptedValue, func(kid string) ([]byte, error) {
		if kid == keyID {
			return dek, nil
		}
		// Re-fetch if key ID doesn't match (rotation scenario)
		dek2, _, err := fetcher.Fetch(context.Background())
		return dek2, err
	})
	if err != nil {
		return "", fmt.Errorf("kms: decrypt: %w", err)
	}

	return plaintext, nil
}

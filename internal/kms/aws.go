//go:build !noaws

package kms

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
)

// AWSKMSFetcher retrieves a DEK from AWS Key Management Service.
type AWSKMSFetcher struct {
	client *kms.Client
	keyID  string
}

// NewAWSKMSFetcher creates a new KMS fetcher using the standard AWS credential chain.
func NewAWSKMSFetcher(keyID string) (*AWSKMSFetcher, error) {
	if keyID == "" {
		return nil, errors.New("kms: aws-kms key_id is required")
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("kms: load AWS config: %w", err)
	}

	return &AWSKMSFetcher{
		client: kms.NewFromConfig(cfg),
		keyID:  keyID,
	}, nil
}

// Fetch calls AWS KMS GenerateDataKey to get a new 32-byte DEK.
func (f *AWSKMSFetcher) Fetch(ctx context.Context) ([]byte, string, error) {
	resp, err := f.client.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
		KeyId:   aws.String(f.keyID),
		KeySpec: typesKeySpecAES256,
	})
	if err != nil {
		return nil, "", fmt.Errorf("kms: GenerateDataKey: %w", err)
	}
	if len(resp.Plaintext) != 32 {
		return nil, "", errors.New("kms: unexpected DEK length from AWS KMS")
	}
	return resp.Plaintext, f.keyID, nil
}

// typesKeySpecAES256 is the KeySpec value for AES-256 data keys.
const typesKeySpecAES256 = "AES_256"

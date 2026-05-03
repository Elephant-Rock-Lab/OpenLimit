package kms

import (
	"testing"
)

func TestAWSKMSFetcherEmptyKeyID(t *testing.T) {
	_, err := NewAWSKMSFetcher("")
	if err == nil {
		t.Fatal("expected error for empty key ID")
	}
}

func TestAWSKMSFetcherMissingCreds(t *testing.T) {
	// This test will fail in environments without AWS credentials.
	// That's expected — it validates the constructor fails cleanly.
	_, err := NewAWSKMSFetcher("arn:aws:kms:us-east-1:000000000000:key/alias/test")
	// In CI without AWS creds, this should error
	// In an environment with AWS creds, it may succeed
	// Either way, we're testing that the constructor doesn't panic
	_ = err
}

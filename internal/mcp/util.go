package mcp

import (
	"crypto/rand"
	"encoding/hex"
)

// randomHex generates n random bytes and returns them as a hex string.
func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// NewInstanceID generates a unique identifier for this gateway instance.
// Used by the Redis bridge to distinguish messages from different instances.
func NewInstanceID() string {
	return "inst_" + randomHex(8)
}

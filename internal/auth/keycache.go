package auth

import (
	"sync"
	"time"
)

// KeyCacheEntry holds a cached auth context and its expiration.
type KeyCacheEntry struct {
	authCtx   *Context
	expiresAt time.Time
}

// KeyCache is a simple LRU-like cache for virtual key lookups.
// Keys are indexed by their bcrypt hash to avoid re-hashing on every lookup.
type KeyCache struct {
	mu      sync.RWMutex
	entries map[string]*KeyCacheEntry
	maxSize int
	ttl     time.Duration
}

// NewKeyCache creates a new key cache with the given max size and TTL.
func NewKeyCache(maxSize int, ttl time.Duration) *KeyCache {
	return &KeyCache{
		entries: make(map[string]*KeyCacheEntry, maxSize),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Get retrieves a cached auth context by key hash.
func (c *KeyCache) Get(keyHash string) (*Context, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[keyHash]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.authCtx, true
}

// Set stores an auth context by key hash.
func (c *KeyCache) Set(keyHash string, authCtx *Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict expired entries if at capacity
	if len(c.entries) >= c.maxSize {
		now := time.Now()
		for k, v := range c.entries {
			if now.After(v.expiresAt) {
				delete(c.entries, k)
			}
		}
		// If still at capacity, remove oldest (simple random eviction for now)
		if len(c.entries) >= c.maxSize {
			for k := range c.entries {
				delete(c.entries, k)
				break
			}
		}
	}

	c.entries[keyHash] = &KeyCacheEntry{
		authCtx:   authCtx,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// Invalidate removes a cached entry by key hash.
func (c *KeyCache) Invalidate(keyHash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, keyHash)
}

// Clear removes all cached entries.
func (c *KeyCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*KeyCacheEntry, c.maxSize)
}

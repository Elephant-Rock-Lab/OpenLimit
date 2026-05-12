package auth

import (
	"sync"
	"time"
)

// KeyCacheEntry holds a cached auth context and its expiration.
type KeyCacheEntry struct {
	authCtx    *Context
	expiresAt  time.Time
	lastAccess time.Time
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
// Uses write lock to safely update lastAccess for LRU tracking.
func (c *KeyCache) Get(keyHash string) (*Context, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[keyHash]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		return nil, false
	}
	entry.lastAccess = time.Now()
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
		// If still at capacity, remove the entry with the oldest lastAccess (LRU eviction)
		if len(c.entries) >= c.maxSize {
			var oldest string
			var oldestTime time.Time
			first := true
			for k, v := range c.entries {
				if first || v.lastAccess.Before(oldestTime) {
					oldest = k
					oldestTime = v.lastAccess
					first = false
				}
			}
			if oldest != "" {
				delete(c.entries, oldest)
			}
		}
	}

	now := time.Now()
	c.entries[keyHash] = &KeyCacheEntry{
		authCtx:    authCtx,
		expiresAt:  now.Add(c.ttl),
		lastAccess: now,
	}
}

// Invalidate removes a cached entry by key hash.
func (c *KeyCache) Invalidate(keyHash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, keyHash)
}

// GetWithGrace retrieves a cached auth context allowing entries that expired
// within the given grace duration. This is used as a fallback when the database
// is unavailable.
func (c *KeyCache) GetWithGrace(keyHash string, grace time.Duration) (*Context, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[keyHash]
	if !ok {
		return nil, false
	}
	// Allow entries expired within the grace period
	if time.Now().After(entry.expiresAt.Add(grace)) {
		return nil, false
	}
	return entry.authCtx, true
}

// Clear removes all cached entries.
func (c *KeyCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*KeyCacheEntry, c.maxSize)
}

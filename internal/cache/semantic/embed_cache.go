package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// EmbeddingCache stores text embeddings in memory to avoid repeated API calls.
type EmbeddingCache struct {
	mu    sync.Mutex
	items map[string]*lruNode // key → LRU node
	lru   *lruList
	max   int
	ttl   time.Duration
}

type cacheEntry struct {
	key       string
	embedding []float32
	createdAt time.Time
}

type lruNode struct {
	entry *cacheEntry
	prev  *lruNode
	next  *lruNode
}

type lruList struct {
	head *lruNode
	tail *lruNode
}

// NewEmbeddingCache creates a new embedding LRU cache.
func NewEmbeddingCache(maxEntries int, ttl time.Duration) *EmbeddingCache {
	if maxEntries <= 0 {
		maxEntries = 5000
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &EmbeddingCache{
		items: make(map[string]*lruNode),
		lru:   &lruList{},
		max:   maxEntries,
		ttl:   ttl,
	}
}

// Get retrieves a cached embedding by text. Returns nil if not found or expired.
func (c *EmbeddingCache) Get(text string) []float32 {
	key := hashText(text)
	c.mu.Lock()
	defer c.mu.Unlock()

	node, ok := c.items[key]
	if !ok {
		return nil
	}

	if time.Since(node.entry.createdAt) > c.ttl {
		// Expired — remove
		c.removeNode(node)
		delete(c.items, key)
		return nil
	}

	c.lru.moveToFront(node)
	result := make([]float32, len(node.entry.embedding))
	copy(result, node.entry.embedding)
	return result
}

// Set stores an embedding for the given text.
func (c *EmbeddingCache) Set(text string, embedding []float32) {
	key := hashText(text)
	c.mu.Lock()
	defer c.mu.Unlock()

	// If already exists, update
	if node, ok := c.items[key]; ok {
		node.entry.embedding = make([]float32, len(embedding))
		copy(node.entry.embedding, embedding)
		node.entry.createdAt = time.Now()
		c.lru.moveToFront(node)
		return
	}

	// Evict if needed
	for len(c.items) >= c.max {
		tail := c.lru.removeTail()
		if tail != nil {
			delete(c.items, tail.key)
		}
	}

	entry := &cacheEntry{
		key:       key,
		embedding: make([]float32, len(embedding)),
		createdAt: time.Now(),
	}
	copy(entry.embedding, embedding)
	node := c.lru.pushFront(entry)
	c.items[key] = node
}

func (c *EmbeddingCache) removeNode(node *lruNode) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		c.lru.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		c.lru.tail = node.prev
	}
}

func (l *lruList) pushFront(entry *cacheEntry) *lruNode {
	node := &lruNode{entry: entry}
	if l.head == nil {
		l.head = node
		l.tail = node
		return node
	}
	node.next = l.head
	l.head.prev = node
	l.head = node
	return node
}

func (l *lruList) moveToFront(node *lruNode) {
	if node == l.head {
		return
	}
	if node.prev != nil {
		node.prev.next = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	}
	if node == l.tail {
		l.tail = node.prev
	}
	node.prev = nil
	node.next = l.head
	l.head.prev = node
	l.head = node
}

func (l *lruList) removeTail() *cacheEntry {
	if l.tail == nil {
		return nil
	}
	entry := l.tail.entry
	if l.tail.prev != nil {
		l.tail.prev.next = nil
	} else {
		l.head = nil
	}
	l.tail = l.tail.prev
	return entry
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

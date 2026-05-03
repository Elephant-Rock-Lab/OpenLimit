package cache

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"openlimit/internal/schema/openai"
)

type Cache interface {
	Get(ctx context.Context, key string) (*openai.ChatCompletionResponse, bool, error)
	Set(ctx context.Context, key string, value *openai.ChatCompletionResponse, ttl time.Duration) error
}

type entry struct {
	key       string
	value     *openai.ChatCompletionResponse
	expiresAt time.Time
}

type ExactLRU struct {
	mu         sync.Mutex
	maxEntries int
	items      map[string]*list.Element
	order      *list.List
}

func NewExactLRU(maxEntries int) *ExactLRU {
	if maxEntries <= 0 {
		maxEntries = 10000
	}
	return &ExactLRU{
		maxEntries: maxEntries,
		items:      map[string]*list.Element{},
		order:      list.New(),
	}
}

func (c *ExactLRU) Get(_ context.Context, key string) (*openai.ChatCompletionResponse, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, ok := c.items[key]
	if !ok {
		return nil, false, nil
	}

	item := element.Value.(*entry)
	if !item.expiresAt.IsZero() && time.Now().After(item.expiresAt) {
		c.order.Remove(element)
		delete(c.items, key)
		return nil, false, nil
	}

	c.order.MoveToFront(element)
	return cloneResponse(item.value), true, nil
}

func (c *ExactLRU) Set(_ context.Context, key string, value *openai.ChatCompletionResponse, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	expiresAt := time.Time{}
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	if element, ok := c.items[key]; ok {
		item := element.Value.(*entry)
		item.value = cloneResponse(value)
		item.expiresAt = expiresAt
		c.order.MoveToFront(element)
		return nil
	}

	element := c.order.PushFront(&entry{key: key, value: cloneResponse(value), expiresAt: expiresAt})
	c.items[key] = element

	for len(c.items) > c.maxEntries {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		item := oldest.Value.(*entry)
		delete(c.items, item.key)
		c.order.Remove(oldest)
	}

	return nil
}

func HashRequest(req openai.ChatCompletionRequest) (string, error) {
	canonical := req
	canonical.Stream = false
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func cloneResponse(resp *openai.ChatCompletionResponse) *openai.ChatCompletionResponse {
	if resp == nil {
		return nil
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return resp
	}
	var cloned openai.ChatCompletionResponse
	if err := json.Unmarshal(data, &cloned); err != nil {
		return resp
	}
	return &cloned
}

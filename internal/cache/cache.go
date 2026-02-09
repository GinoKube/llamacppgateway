package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

type entry struct {
	data      []byte
	createdAt time.Time
}

// ResponseCache caches responses for deterministic requests (temperature=0).
type ResponseCache struct {
	mu         sync.RWMutex
	entries    map[string]*entry
	maxEntries int
	ttl        time.Duration
}

func New(maxEntries int, ttlSec int) *ResponseCache {
	c := &ResponseCache{
		entries:    make(map[string]*entry),
		maxEntries: maxEntries,
		ttl:        time.Duration(ttlSec) * time.Second,
	}
	// Periodic cleanup
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			c.cleanup()
		}
	}()
	return c
}

// CacheKey generates a deterministic key from the request body.
// Only caches if temperature == 0 (or not set and we default to 0).
func CacheKey(body []byte) (string, bool) {
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", false
	}

	// Only cache deterministic requests
	temp, ok := req["temperature"]
	if !ok {
		return "", false // don't cache if temperature not explicitly 0
	}
	tempFloat, ok := temp.(float64)
	if !ok || tempFloat != 0 {
		return "", false
	}

	// Don't cache streaming requests
	if stream, ok := req["stream"].(bool); ok && stream {
		return "", false
	}

	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:]), true
}

// Get returns cached response data and true if found and not expired.
func (c *ResponseCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Since(e.createdAt) > c.ttl {
		return nil, false
	}
	return e.data, true
}

// Set stores a response in the cache.
func (c *ResponseCache) Set(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity
	if len(c.entries) >= c.maxEntries {
		var oldestKey string
		var oldestTime time.Time
		for k, e := range c.entries {
			if oldestKey == "" || e.createdAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = e.createdAt
			}
		}
		if oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}

	c.entries[key] = &entry{
		data:      data,
		createdAt: time.Now(),
	}
}

func (c *ResponseCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, e := range c.entries {
		if now.Sub(e.createdAt) > c.ttl {
			delete(c.entries, k)
		}
	}
}

// Stats returns cache statistics.
func (c *ResponseCache) Stats() (size int, maxSize int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries), c.maxEntries
}

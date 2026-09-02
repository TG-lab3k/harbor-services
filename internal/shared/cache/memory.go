package cache

import (
	"sync"
	"time"
)

type entry struct {
	value     interface{}
	expiresAt time.Time
}

// InMemoryCache is a simple TTL cache.
type InMemoryCache struct {
	mu   sync.RWMutex
	data map[string]entry
}

func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{data: make(map[string]entry)}
}

func (c *InMemoryCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	e, ok := c.data[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if !e.expiresAt.IsZero() && time.Now().After(e.expiresAt) {
		c.Delete(key)
		return nil, false
	}
	return e.value, true
}

func (c *InMemoryCache) Set(key string, value interface{}, ttl time.Duration) {
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}
	c.mu.Lock()
	c.data[key] = entry{value: value, expiresAt: expiresAt}
	c.mu.Unlock()
}

func (c *InMemoryCache) Delete(key string) {
	c.mu.Lock()
	delete(c.data, key)
	c.mu.Unlock()
}

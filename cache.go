package main

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
)

// Cache is a simple in-memory cache with expiration.
type Cache struct {
	items map[string]CacheItem
	mu    sync.RWMutex
}

// CacheItem represents an item in the cache.
type CacheItem struct {
	Value      any
	Expiration int64
}

// NewCache creates a new Cache.
func NewCache() *Cache {
	cache := &Cache{
		items: make(map[string]CacheItem),
	}
	// Add a background goroutine to clean up expired items.
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			cache.mu.Lock()
			for key, item := range cache.items {
				if time.Now().UnixNano() > item.Expiration {
					delete(cache.items, key)
				}
			}
			cache.mu.Unlock()
		}
	}()
	return cache
}

// Set adds an item to the cache, replacing any existing item.
func (c *Cache) Set(key string, value any, duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = CacheItem{
		Value:      value,
		Expiration: time.Now().Add(duration).UnixNano(),
	}
}

// Get gets an item from the cache. It returns the item or nil, and a bool indicating whether the key was found.
func (c *Cache) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	item, found := c.items[key]
	if !found {
		return nil, false
	}
	if time.Now().UnixNano() > item.Expiration {
		return nil, false
	}
	return item.Value, true
}

// Global cache instances
var (
	messageConversionCache = NewCache()
	toolsValidationCache   = NewCache()
)

// generateMessagesCacheKey creates a cache key from chat messages.
func generateMessagesCacheKey(messages []ChatMessage) string {
	var b strings.Builder
	for _, msg := range messages {
		b.WriteString(msg.Role)
		if content, ok := msg.Content.(string); ok {
			b.WriteString(content)
		}
	}
	hash := sha1.Sum([]byte(b.String()))
	return hex.EncodeToString(hash[:])
}

// generateToolsCacheKey creates a cache key from a slice of tools.
func generateToolsCacheKey(tools []Tool) string {
	var b strings.Builder
	for _, t := range tools {
		b.WriteString(t.Type)
		b.WriteString(t.Function.Name)
	}
	hash := sha1.Sum([]byte(b.String()))
	return hex.EncodeToString(hash[:])
}

// Helper function to marshal JSON, using Sonic for performance
func marshalJSON(v any) ([]byte, error) {
	return sonic.Marshal(v)
}

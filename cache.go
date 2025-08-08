package main

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
)

// LRUCache is a thread-safe LRU cache with expiration
type LRUCache struct {
	capacity int
	items    map[string]*CacheItem
	mu       sync.RWMutex
	head     *CacheItem
	tail     *CacheItem
}

// CacheItem represents an item in the cache with LRU links
type CacheItem struct {
	Value      any
	Expiration int64
	key        string
	prev       *CacheItem
	next       *CacheItem
}

// NewCache creates a new LRU Cache with optimized capacity.
func NewCache() *LRUCache {
	cache := &LRUCache{
		capacity: 1000, // 优化缓存容量
		items:    make(map[string]*CacheItem),
	}
	
	// Initialize sentinel nodes
	cache.head = &CacheItem{}
	cache.tail = &CacheItem{}
	cache.head.next = cache.tail
	cache.tail.prev = cache.head
	
	// Add a background goroutine to clean up expired items.
	go func() {
		for {
			time.Sleep(5 * time.Minute) // 减少清理频率到5分钟
			cache.cleanupExpired()
		}
	}()
	return cache
}

// Set adds an item to the cache, replacing any existing item.
func (c *LRUCache) Set(key string, value any, duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// If item exists, update it and move to front
	if item, exists := c.items[key]; exists {
		item.Value = value
		item.Expiration = time.Now().Add(duration).UnixNano()
		c.moveToFront(item)
		return
	}
	
	// Create new item
	item := &CacheItem{
		Value:      value,
		Expiration: time.Now().Add(duration).UnixNano(),
		key:        key,
	}
	
	// Add to front
	c.addToFront(item)
	c.items[key] = item
	
	// Evict if over capacity
	if len(c.items) > c.capacity {
		c.evict()
	}
}

// Get gets an item from the cache. It returns the item or nil, and a bool indicating whether the key was found.
func (c *LRUCache) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	item, found := c.items[key]
	if !found {
		return nil, false
	}
	
	if time.Now().UnixNano() > item.Expiration {
		return nil, false
	}
	
	// Move to front for LRU
	c.moveToFront(item)
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

// generateParamsCacheKey creates a cache key from parameter schemas
func generateParamsCacheKey(params map[string]any) string {
	// 使用 Sonic 快速序列化
	data, _ := sonic.Marshal(params)
	hash := sha1.Sum(data)
	return hex.EncodeToString(hash[:])
}

// Helper function to marshal JSON, using Sonic for performance
func marshalJSON(v any) ([]byte, error) {
	return sonic.Marshal(v)
}

// LRU cache helper methods
func (c *LRUCache) addToFront(item *CacheItem) {
	item.next = c.head.next
	item.prev = c.head
	c.head.next.prev = item
	c.head.next = item
}

func (c *LRUCache) moveToFront(item *CacheItem) {
	c.remove(item)
	c.addToFront(item)
}

func (c *LRUCache) remove(item *CacheItem) {
	item.prev.next = item.next
	item.next.prev = item.prev
}

func (c *LRUCache) evict() {
	if c.tail.prev == c.head {
		return
	}
	
	item := c.tail.prev
	c.remove(item)
	delete(c.items, item.key)
}

func (c *LRUCache) cleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	now := time.Now().UnixNano()
	for key, item := range c.items {
		if now > item.Expiration {
			c.remove(item)
			delete(c.items, key)
		}
	}
}

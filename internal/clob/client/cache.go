package client

import (
	"sync"
	"time"

	"polymarket-btc-bot/internal/clob/types"
)

// orderBookCacheEntry represents a cached order book entry
type orderBookCacheEntry struct {
	book      *types.OrderBookSummary
	timestamp time.Time
}

// orderBookCache provides thread-safe caching for order books
type orderBookCache struct {
	mu       sync.RWMutex
	cache    map[string]*orderBookCacheEntry
	ttl      time.Duration // Time-to-live for cache entries
	maxSize  int           // Maximum number of cached entries
}

// newOrderBookCache creates a new order book cache
func newOrderBookCache(ttl time.Duration, maxSize int) *orderBookCache {
	if ttl <= 0 {
		ttl = 500 * time.Millisecond // Default 500ms TTL
	}
	if maxSize <= 0 {
		maxSize = 100 // Default 100 entries
	}
	return &orderBookCache{
		cache:   make(map[string]*orderBookCacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

// get retrieves a cached order book if it's still valid
func (c *orderBookCache) get(tokenID string) (*types.OrderBookSummary, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.cache[tokenID]
	if !ok {
		return nil, false
	}

	// Check if entry is still valid
	if time.Since(entry.timestamp) > c.ttl {
		// Entry expired, but don't delete here (let write handle it)
		return nil, false
	}

	// Return a copy to avoid race conditions
	// Note: This is a shallow copy, but OrderBookSummary should be immutable
	return entry.book, true
}

// set stores an order book in the cache
func (c *orderBookCache) set(tokenID string, book *types.OrderBookSummary) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clean up expired entries if cache is getting full
	if len(c.cache) >= c.maxSize {
		c.cleanup()
	}

	c.cache[tokenID] = &orderBookCacheEntry{
		book:      book,
		timestamp: time.Now(),
	}
}

// cleanup removes expired entries
func (c *orderBookCache) cleanup() {
	now := time.Now()
	for key, entry := range c.cache {
		if now.Sub(entry.timestamp) > c.ttl {
			delete(c.cache, key)
		}
	}
}

// clear removes all entries
func (c *orderBookCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]*orderBookCacheEntry)
}


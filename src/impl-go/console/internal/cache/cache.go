// Package cache provides an in-process TTL cache with
// stale-while-revalidate support. Designed for the dashw BFF to
// reduce dashd load from O(users) to O(1).
//
// Design decisions (see dashw-web-scale-design-req-analysis.md §3):
//   - Zero external dependencies (stdlib only)
//   - ~500KB memory (50 entries × 10KB)
//   - Per-endpoint configurable TTL (fast 5s, slow 30s)
//   - Stale window: serve expired data while refreshing in background
//   - Mutation-aware invalidation via pattern matching
//   - Thread-safe via sync.RWMutex
package cache

import (
	"strings"
	"sync"
	"time"
)

// Status indicates the result of a cache lookup.
type Status int

const (
	Miss  Status = iota // Key not found or fully expired
	Hit                 // Key found and fresh
	Stale               // Key found but TTL expired; still within stale window
)

// String returns a human-readable cache status for response headers.
func (s Status) String() string {
	switch s {
	case Hit:
		return "hit"
	case Stale:
		return "stale"
	default:
		return "miss"
	}
}

// Entry is a single cached value with TTL and stale window.
type Entry struct {
	Data      []byte
	CreatedAt time.Time
	ExpiresAt time.Time // after this: entry is stale
	StaleAt   time.Time // after this: entry is evicted
}

// Cache is a thread-safe in-process TTL cache.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*Entry
	stopCh  chan struct{}
}

// New creates a cache and starts a background cleanup goroutine
// that evicts fully expired entries every cleanupInterval.
func New(cleanupInterval time.Duration) *Cache {
	c := &Cache{
		entries: make(map[string]*Entry),
		stopCh:  make(chan struct{}),
	}
	go c.cleanup(cleanupInterval)
	return c
}

// Get retrieves a cached entry. Returns the data, cache status,
// and the age of the entry (time since creation).
//
// Returns:
//   - (data, Hit, age)   if the entry exists and is fresh
//   - (data, Stale, age) if the entry exists but TTL expired (within stale window)
//   - (nil, Miss, 0)     if the entry doesn't exist or is fully expired
func (c *Cache) Get(key string) ([]byte, Status, time.Duration) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil, Miss, 0
	}

	now := time.Now()
	age := now.Sub(entry.CreatedAt)

	// Fully expired (beyond stale window) — treat as miss
	if now.After(entry.StaleAt) {
		return nil, Miss, 0
	}

	// Fresh (within TTL)
	if now.Before(entry.ExpiresAt) {
		return entry.Data, Hit, age
	}

	// Stale (TTL expired but within stale window)
	return entry.Data, Stale, age
}

// Set stores a value with the given TTL and stale window.
// The entry is fresh for `ttl` duration, then stale for
// `staleWindow` duration, then evicted.
func (c *Cache) Set(key string, data []byte, ttl, staleWindow time.Duration) {
	now := time.Now()
	entry := &Entry{
		Data:      data,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
		StaleAt:   now.Add(ttl + staleWindow),
	}

	c.mu.Lock()
	c.entries[key] = entry
	c.mu.Unlock()
}

// Invalidate removes a single key from the cache.
func (c *Cache) Invalidate(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// InvalidatePattern removes all keys that start with the given prefix.
// A prefix of "*" is equivalent to Flush (removes everything).
// A prefix of "vnet/" removes "vnet/detail:x", "vnet/canvas:x", etc.
func (c *Cache) InvalidatePattern(prefix string) {
	if prefix == "*" {
		c.Flush()
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for key := range c.entries {
		if strings.HasPrefix(key, prefix) {
			delete(c.entries, key)
		}
	}
}

// InvalidateKeys removes multiple specific keys from the cache.
func (c *Cache) InvalidateKeys(keys []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, key := range keys {
		delete(c.entries, key)
	}
}

// Flush removes all entries from the cache.
func (c *Cache) Flush() {
	c.mu.Lock()
	c.entries = make(map[string]*Entry)
	c.mu.Unlock()
}

// Len returns the number of entries in the cache (including stale).
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Keys returns all current cache keys (for debugging/testing).
func (c *Cache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.entries))
	for k := range c.entries {
		keys = append(keys, k)
	}
	return keys
}

// Stop terminates the background cleanup goroutine.
// Call this when shutting down the server.
func (c *Cache) Stop() {
	close(c.stopCh)
}

// cleanup periodically removes fully expired entries (past stale window).
func (c *Cache) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.evictExpired()
		case <-c.stopCh:
			return
		}
	}
}

// evictExpired removes entries that are past their stale window.
func (c *Cache) evictExpired() {
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	for key, entry := range c.entries {
		if now.After(entry.StaleAt) {
			delete(c.entries, key)
		}
	}
}
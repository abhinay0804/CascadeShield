package resolver

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
)

// cacheEntry wraps a key-value pair stored in the LRU list along with its expiration timestamp.
type cacheEntry[K comparable, V any] struct {
	key       K
	val       V
	expiresAt time.Time
}

// Cache is a thread-safe, generic Least-Recently-Used (LRU) cache with Time-To-Live (TTL) eviction.
//
// Go Pattern Note for Beginners (Generics & Data Structures):
// - `[K comparable, V any]` defines Go Generics. K must be comparable (supports == and !=),
//   allowing it to be used as a map key. V can be any type.
// - `container/list` provides a doubly-linked list. Moving accessed elements to the front
//   gives O(1) LRU management.
// - `sync.Mutex` ensures safe concurrent access when multiple goroutines read/write the cache.
type Cache[K comparable, V any] struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	items    map[K]*list.Element
	evictList *list.List

	// Stats for monitoring cache effectiveness
	hits   uint64
	misses uint64
}

// NewCache constructs a new generic LRU cache with the specified capacity and TTL.
func NewCache[K comparable, V any](capacity int, ttl time.Duration) *Cache[K, V] {
	if capacity <= 0 {
		capacity = 1024
	}
	return &Cache[K, V]{
		capacity:  capacity,
		ttl:       ttl,
		items:     make(map[K]*list.Element),
		evictList: list.New(),
	}
}

// Get retrieves a value from the cache if present and not expired.
// Returns (value, true) if found & valid, or (zero-value, false) if missing/expired.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, exists := c.items[key]
	if !exists {
		atomic.AddUint64(&c.misses, 1)
		var zero V
		return zero, false
	}

	entry := elem.Value.(*cacheEntry[K, V])

	// Check if item has expired based on TTL
	if c.ttl > 0 && time.Now().After(entry.expiresAt) {
		c.removeElement(elem)
		atomic.AddUint64(&c.misses, 1)
		var zero V
		return zero, false
	}

	// Move accessed element to front of LRU list (most recently used)
	c.evictList.MoveToFront(elem)
	atomic.AddUint64(&c.hits, 1)
	return entry.val, true
}

// Set inserts or updates a key-value pair in the cache.
// If capacity is reached, the least recently used (LRU) element is evicted.
func (c *Cache[K, V]) Set(key K, val V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	expiresAt := time.Time{}
	if c.ttl > 0 {
		expiresAt = time.Now().Add(c.ttl)
	}

	// If key already exists, update value and move to front
	if elem, exists := c.items[key]; exists {
		c.evictList.MoveToFront(elem)
		entry := elem.Value.(*cacheEntry[K, V])
		entry.val = val
		entry.expiresAt = expiresAt
		return
	}

	// Evict oldest item if capacity is exceeded
	if c.evictList.Len() >= c.capacity {
		c.evictOldest()
	}

	// Add new element to front
	entry := &cacheEntry[K, V]{
		key:       key,
		val:       val,
		expiresAt: expiresAt,
	}
	elem := c.evictList.PushFront(entry)
	c.items[key] = elem
}

// Delete removes a specific key from the cache.
func (c *Cache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, exists := c.items[key]; exists {
		c.removeElement(elem)
	}
}

// Len returns the current number of non-expired items in the cache.
func (c *Cache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evictList.Len()
}

// Stats returns the cumulative cache hits and misses.
func (c *Cache[K, V]) Stats() (hits, misses uint64) {
	return atomic.LoadUint64(&c.hits), atomic.LoadUint64(&c.misses)
}

// evictOldest removes the least recently used element (back of list).
// Assumes mutex lock is already held.
func (c *Cache[K, V]) evictOldest() {
	elem := c.evictList.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

// removeElement unlinks element from list and map.
// Assumes mutex lock is already held.
func (c *Cache[K, V]) removeElement(elem *list.Element) {
	c.evictList.Remove(elem)
	entry := elem.Value.(*cacheEntry[K, V])
	delete(c.items, entry.key)
}

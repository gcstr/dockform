package dockercli

import (
	"container/list"
	"sync"
)

// LRUCache is a simple thread-safe LRU cache with a maximum size.
// When the cache exceeds maxSize, the least recently used entries are evicted.
type LRUCache[K comparable, V any] struct {
	maxSize int
	mu      sync.RWMutex
	items   map[K]*list.Element
	order   *list.List // front = most recent, back = least recent
}

type lruEntry[K comparable, V any] struct {
	key   K
	value V
}

// NewLRUCache creates a new LRU cache with the specified maximum size.
// If maxSize <= 0, it defaults to 100.
func NewLRUCache[K comparable, V any](maxSize int) *LRUCache[K, V] {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &LRUCache[K, V]{
		maxSize: maxSize,
		items:   make(map[K]*list.Element),
		order:   list.New(),
	}
}

// Get retrieves a value from the cache. Returns the value and true if found,
// or the zero value and false if not found. Accessing an item moves it to
// the front (most recently used).
func (c *LRUCache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		return elem.Value.(*lruEntry[K, V]).value, true
	}
	var zero V
	return zero, false
}

// Set adds or updates a value in the cache. If the cache is at capacity,
// the least recently used entry is evicted.
func (c *LRUCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update existing entry
	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		elem.Value.(*lruEntry[K, V]).value = value
		return
	}

	// Evict if at capacity
	for c.order.Len() >= c.maxSize {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*lruEntry[K, V]).key)
	}

	// Add new entry
	entry := &lruEntry[K, V]{key: key, value: value}
	elem := c.order.PushFront(entry)
	c.items[key] = elem
}

// Len returns the number of items in the cache.
func (c *LRUCache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.order.Len()
}

// Clear removes all items from the cache.
func (c *LRUCache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[K]*list.Element)
	c.order.Init()
}

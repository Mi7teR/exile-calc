// Package boundcache is a bounded, concurrent string-keyed cache for a
// long-running consumer that sees an endless stream of new keys (a price
// checker parsing every item it is shown).
//
// It keeps two generations of at most half the capacity each. A store
// into a full current generation drops the old one and starts a new
// current one; a hit in the old generation copies the entry forward. So
// entries in use (the lines of the build being compared) survive, and
// one-off keys age out instead of filling the cache for good.
package boundcache

import "sync"

// generations is how many generations share the capacity.
const generations = 2

// Cache maps strings to V. The zero value is not usable: use New.
type Cache[V any] struct {
	mu       sync.RWMutex
	cur, old map[string]V
	gen      int // the most entries a generation holds
}

// New returns an empty cache holding at most capacity entries (at least 2).
func New[V any](capacity int) *Cache[V] {
	gen := max(capacity/generations, 1)
	return &Cache[V]{cur: make(map[string]V), gen: gen}
}

// Load returns the value stored for key.
func (c *Cache[V]) Load(key string) (V, bool) {
	c.mu.RLock()
	v, ok := c.cur[key]
	if ok {
		c.mu.RUnlock()
		return v, true
	}
	v, ok = c.old[key]
	c.mu.RUnlock()
	if ok {
		c.Store(key, v) // still in use: keep it past the next rotation
	}
	return v, ok
}

// Store stores v for key unless the current generation already has key
// (the first store wins, as [sync.Map.LoadOrStore]).
func (c *Cache[V]) Store(key string, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.cur[key]; ok {
		return
	}
	if len(c.cur) >= c.gen {
		c.old, c.cur = c.cur, make(map[string]V, c.gen)
	}
	c.cur[key] = v
}

// Len is the number of entries held, both generations (a key in both
// counts twice).
func (c *Cache[V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cur) + len(c.old)
}

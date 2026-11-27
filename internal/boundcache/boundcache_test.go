package boundcache

import (
	"strconv"
	"sync"
	"testing"
)

func TestBoundedAndKeepsUsedEntries(t *testing.T) {
	c := New[int](10) // 5 per generation
	c.Store("hot", 1)
	for i := range 100 {
		c.Store(strconv.Itoa(i), i)
		if _, ok := c.Load("hot"); !ok {
			t.Fatalf("hot entry dropped after %d one-off keys", i+1)
		}
		if n := c.Len(); n > 10 {
			t.Fatalf("Len %d > capacity 10", n)
		}
	}
	if _, ok := c.Load("0"); ok {
		t.Error("an old one-off key is still cached")
	}
	if v, ok := c.Load("99"); !ok || v != 99 {
		t.Errorf("newest key = %d, %v", v, ok)
	}
}

func TestFirstStoreWins(t *testing.T) {
	c := New[int](10)
	c.Store("k", 1)
	c.Store("k", 2)
	if v, _ := c.Load("k"); v != 1 {
		t.Errorf("k = %d, want 1", v)
	}
}

func TestConcurrent(t *testing.T) {
	c := New[int](64)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 2000 {
				k := strconv.Itoa((g*7 + i) % 300)
				if v, ok := c.Load(k); ok && strconv.Itoa(v) != k {
					t.Errorf("%s = %d", k, v)
					return
				}
				n, _ := strconv.Atoi(k)
				c.Store(k, n)
			}
		})
	}
	wg.Wait()
	if n := c.Len(); n > 64 {
		t.Errorf("Len %d > 64", n)
	}
}

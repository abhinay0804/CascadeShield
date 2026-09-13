package resolver

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestCache_SetAndGet(t *testing.T) {
	cache := NewCache[string, string](10, 1*time.Minute)

	cache.Set("key1", "val1")
	val, ok := cache.Get("key1")
	if !ok || val != "val1" {
		t.Fatalf("expected val1, got %v (ok=%v)", val, ok)
	}

	_, ok = cache.Get("missing")
	if ok {
		t.Fatalf("expected missing key to return false")
	}
}

func TestCache_LRUEviction(t *testing.T) {
	// Capacity of 3
	cache := NewCache[int, string](3, 1*time.Minute)

	cache.Set(1, "one")
	cache.Set(2, "two")
	cache.Set(3, "three")

	// Access key 1 to make it recently used (so 2 becomes oldest)
	_, _ = cache.Get(1)

	// Add key 4 -> should evict key 2
	cache.Set(4, "four")

	if _, ok := cache.Get(2); ok {
		t.Errorf("expected key 2 to be evicted as LRU")
	}
	if _, ok := cache.Get(1); !ok {
		t.Errorf("expected key 1 to still exist")
	}
	if cache.Len() != 3 {
		t.Errorf("expected len 3, got %d", cache.Len())
	}
}

func TestCache_TTLExpiration(t *testing.T) {
	// Short TTL of 50ms
	cache := NewCache[string, string](10, 50*time.Millisecond)

	cache.Set("temp", "value")

	val, ok := cache.Get("temp")
	if !ok || val != "value" {
		t.Fatalf("expected value before expiration")
	}

	time.Sleep(70 * time.Millisecond)

	_, ok = cache.Get("temp")
	if ok {
		t.Fatalf("expected item to be expired after TTL")
	}
}

func TestCache_ConcurrentRace(t *testing.T) {
	cache := NewCache[int, int](100, 1*time.Minute)
	var wg sync.WaitGroup

	// Launch 20 concurrent readers and writers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := (workerID * 10) + (j % 10)
				cache.Set(key, j)
				_, _ = cache.Get(key)
			}
		}(i)
	}

	wg.Wait()
	hits, misses := cache.Stats()
	if hits+misses == 0 {
		t.Errorf("expected non-zero cache access stats")
	}
	fmt.Printf("Cache stats under concurrent access: hits=%d misses=%d\n", hits, misses)
}

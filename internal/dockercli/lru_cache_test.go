package dockercli

import (
	"testing"
)

func TestLRUCache_BasicOperations(t *testing.T) {
	cache := NewLRUCache[string, int](3)

	// Test Set and Get
	cache.Set("a", 1)
	cache.Set("b", 2)
	cache.Set("c", 3)

	if v, ok := cache.Get("a"); !ok || v != 1 {
		t.Errorf("expected a=1, got %v, ok=%v", v, ok)
	}
	if v, ok := cache.Get("b"); !ok || v != 2 {
		t.Errorf("expected b=2, got %v, ok=%v", v, ok)
	}
	if v, ok := cache.Get("c"); !ok || v != 3 {
		t.Errorf("expected c=3, got %v, ok=%v", v, ok)
	}
}

func TestLRUCache_Eviction(t *testing.T) {
	cache := NewLRUCache[string, int](3)

	cache.Set("a", 1)
	cache.Set("b", 2)
	cache.Set("c", 3)

	// Adding a 4th item should evict "a" (least recently used)
	cache.Set("d", 4)

	if _, ok := cache.Get("a"); ok {
		t.Error("expected 'a' to be evicted")
	}
	if v, ok := cache.Get("b"); !ok || v != 2 {
		t.Errorf("expected b=2, got %v, ok=%v", v, ok)
	}
	if v, ok := cache.Get("c"); !ok || v != 3 {
		t.Errorf("expected c=3, got %v, ok=%v", v, ok)
	}
	if v, ok := cache.Get("d"); !ok || v != 4 {
		t.Errorf("expected d=4, got %v, ok=%v", v, ok)
	}
}

func TestLRUCache_AccessUpdatesOrder(t *testing.T) {
	cache := NewLRUCache[string, int](3)

	cache.Set("a", 1)
	cache.Set("b", 2)
	cache.Set("c", 3)

	// Access "a" to make it most recently used
	cache.Get("a")

	// Adding "d" should now evict "b" (least recently used)
	cache.Set("d", 4)

	if _, ok := cache.Get("a"); !ok {
		t.Error("expected 'a' to still exist after access")
	}
	if _, ok := cache.Get("b"); ok {
		t.Error("expected 'b' to be evicted")
	}
}

func TestLRUCache_UpdateExisting(t *testing.T) {
	cache := NewLRUCache[string, int](3)

	cache.Set("a", 1)
	cache.Set("b", 2)
	cache.Set("c", 3)

	// Update "a"
	cache.Set("a", 100)

	if v, ok := cache.Get("a"); !ok || v != 100 {
		t.Errorf("expected a=100 after update, got %v, ok=%v", v, ok)
	}

	// Cache should still have 3 items
	if cache.Len() != 3 {
		t.Errorf("expected len=3, got %d", cache.Len())
	}

	// Adding "d" should evict "b" (since "a" was updated, making it most recent)
	cache.Set("d", 4)

	if _, ok := cache.Get("b"); ok {
		t.Error("expected 'b' to be evicted after 'a' was updated")
	}
}

func TestLRUCache_Clear(t *testing.T) {
	cache := NewLRUCache[string, int](3)

	cache.Set("a", 1)
	cache.Set("b", 2)

	cache.Clear()

	if cache.Len() != 0 {
		t.Errorf("expected len=0 after clear, got %d", cache.Len())
	}
	if _, ok := cache.Get("a"); ok {
		t.Error("expected 'a' to be gone after clear")
	}
}

func TestLRUCache_DefaultSize(t *testing.T) {
	cache := NewLRUCache[string, int](0) // Should default to 100
	if cache.maxSize != 100 {
		t.Errorf("expected default maxSize=100, got %d", cache.maxSize)
	}

	cache2 := NewLRUCache[string, int](-5) // Negative should also default
	if cache2.maxSize != 100 {
		t.Errorf("expected default maxSize=100 for negative input, got %d", cache2.maxSize)
	}
}

func TestLRUCache_GetMissing(t *testing.T) {
	cache := NewLRUCache[string, int](3)

	v, ok := cache.Get("nonexistent")
	if ok {
		t.Error("expected ok=false for missing key")
	}
	if v != 0 {
		t.Errorf("expected zero value for missing key, got %v", v)
	}
}

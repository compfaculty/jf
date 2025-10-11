package utils

import (
	"sync"
	"testing"
	"time"
)

func TestCache(t *testing.T) {
	cache := NewCache(time.Minute)

	t.Run("Basic Operations", func(t *testing.T) {
		// Test Set and Get
		cache.Set("key1", "value1")

		value, found := cache.Get("key1")
		if !found {
			t.Error("Expected to find key1")
		}
		if value != "value1" {
			t.Errorf("Expected value1, got %v", value)
		}

		// Test non-existent key
		_, found = cache.Get("nonexistent")
		if found {
			t.Error("Expected not to find nonexistent key")
		}
	})

	t.Run("Expiration", func(t *testing.T) {
		// Set with short expiration
		cache.SetWithTTL("expired", "value", 10*time.Millisecond)

		// Should be found immediately
		_, found := cache.Get("expired")
		if !found {
			t.Error("Expected to find key before expiration")
		}

		// Wait for expiration
		time.Sleep(20 * time.Millisecond)

		// Should not be found after expiration
		_, found = cache.Get("expired")
		if found {
			t.Error("Expected key to be expired")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		cache.Set("delete_test", "value")

		// Should be found
		_, found := cache.Get("delete_test")
		if !found {
			t.Error("Expected to find key before deletion")
		}

		// Delete the key
		cache.Delete("delete_test")

		// Should not be found after deletion
		_, found = cache.Get("delete_test")
		if found {
			t.Error("Expected key to be deleted")
		}
	})

	t.Run("Clear", func(t *testing.T) {
		// Add multiple keys
		cache.Set("clear1", "value1")
		cache.Set("clear2", "value2")
		cache.Set("clear3", "value3")

		// Clear all
		cache.Clear()

		// None should be found
		keys := []string{"clear1", "clear2", "clear3"}
		for _, key := range keys {
			_, found := cache.Get(key)
			if found {
				t.Errorf("Expected key %s to be cleared", key)
			}
		}
	})
}

func TestCacheConcurrency(t *testing.T) {
	cache := NewCache(time.Minute)
	const numGoroutines = 50
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Test concurrent writes and reads
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := string(rune('a' + (j % 26)))
				value := string(rune('A' + (j % 26)))

				// Set value
				cache.Set(key, value)

				// Get value
				retrievedValue, found := cache.Get(key)
				if found && retrievedValue != value {
					t.Errorf("Concurrent access failed: expected %s, got %v", value, retrievedValue)
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestCacheExpirationCleanup(t *testing.T) {
	cache := NewCache(time.Minute)

	// Add multiple keys with different expiration times
	cache.SetWithTTL("short", "value1", 10*time.Millisecond)
	cache.Set("long", "value2")
	cache.SetWithTTL("medium", "value3", 50*time.Millisecond)

	// Wait for short expiration
	time.Sleep(20 * time.Millisecond)

	// Try to get all keys
	_, found1 := cache.Get("short")
	_, found2 := cache.Get("long")
	_, found3 := cache.Get("medium")

	if found1 {
		t.Error("Expected short-lived key to be expired")
	}
	if !found2 {
		t.Error("Expected long-lived key to still exist")
	}
	if !found3 {
		t.Error("Expected medium-lived key to still exist")
	}

	// Wait for medium expiration
	time.Sleep(40 * time.Millisecond)

	_, found3 = cache.Get("medium")
	if found3 {
		t.Error("Expected medium-lived key to be expired")
	}

	_, found2 = cache.Get("long")
	if !found2 {
		t.Error("Expected long-lived key to still exist")
	}
}

func TestCacheStats(t *testing.T) {
	cache := NewCache(time.Minute)

	// Test stats tracking
	cache.Set("key1", "value1")
	cache.Set("key2", "value2")

	// Get existing key
	_, found := cache.Get("key1")
	if !found {
		t.Error("Expected to find key1")
	}

	// Get non-existent key
	_, found = cache.Get("nonexistent")
	if found {
		t.Error("Expected not to find nonexistent key")
	}

	// Test size
	size := cache.Size()
	if size != 2 {
		t.Errorf("Expected size 2, got %d", size)
	}
}

func TestCacheWithDifferentTypes(t *testing.T) {
	cache := NewCache(time.Minute)

	// Test with different value types
	testCases := []struct {
		key   string
		value interface{}
	}{
		{"string", "hello world"},
		{"int", 42},
		{"float", 3.14},
		{"bool", true},
		{"slice", []string{"a", "b", "c"}},
		{"map", map[string]int{"a": 1, "b": 2}},
	}

	for _, tc := range testCases {
		cache.Set(tc.key, tc.value)

		retrieved, found := cache.Get(tc.key)
		if !found {
			t.Errorf("Expected to find key %s", tc.key)
		}
		// For slices and maps, we just check that we got something back
		// The cache stores the same pointer, so the comparison would work for pointer equality
		// but not for value equality without reflection
		if retrieved == nil {
			t.Errorf("Expected non-nil value for key %s", tc.key)
		}
	}
}

func BenchmarkCache(b *testing.B) {
	cache := NewCache(time.Minute)

	b.Run("Set", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := string(rune('a' + (i % 26)))
			cache.Set(key, "value")
		}
	})

	b.Run("Get", func(b *testing.B) {
		// Pre-populate cache
		for i := 0; i < 1000; i++ {
			key := string(rune('a' + (i % 26)))
			cache.Set(key, "value")
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := string(rune('a' + (i % 26)))
			_, _ = cache.Get(key)
		}
	})

	b.Run("SetAndGet", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := string(rune('a' + (i % 26)))
			cache.Set(key, "value")
			_, _ = cache.Get(key)
		}
	})
}

func BenchmarkCacheConcurrency(b *testing.B) {
	cache := NewCache(time.Minute)

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := string(rune('a' + (i % 26)))
			cache.Set(key, "value")
			_, _ = cache.Get(key)
			i++
		}
	})
}

package utils

import (
	"sync"
	"time"
)

// CacheItem represents a cached item with expiration
type CacheItem struct {
	Value     interface{}
	ExpiresAt time.Time
	CreatedAt time.Time
}

// IsExpired checks if the cache item has expired
func (ci *CacheItem) IsExpired() bool {
	return time.Now().After(ci.ExpiresAt)
}

// Cache provides a thread-safe cache with TTL support
type Cache struct {
	mu    sync.RWMutex
	items map[string]*CacheItem
	ttl   time.Duration
}

// NewCache creates a new cache with the specified TTL
func NewCache(ttl time.Duration) *Cache {
	c := &Cache{
		items: make(map[string]*CacheItem),
		ttl:   ttl,
	}

	// Start cleanup goroutine
	go c.cleanup()

	return c
}

// Get retrieves a value from the cache
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, exists := c.items[key]
	if !exists || item.IsExpired() {
		return nil, false
	}

	return item.Value, true
}

// Set stores a value in the cache with the default TTL
func (c *Cache) Set(key string, value interface{}) {
	c.SetWithTTL(key, value, c.ttl)
}

// SetWithTTL stores a value in the cache with a custom TTL
func (c *Cache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = &CacheItem{
		Value:     value,
		ExpiresAt: time.Now().Add(ttl),
		CreatedAt: time.Now(),
	}
}

// Delete removes a key from the cache
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// Clear removes all items from the cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*CacheItem)
}

// Size returns the number of items in the cache
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// cleanup removes expired items periodically
func (c *Cache) cleanup() {
	ticker := time.NewTicker(c.ttl / 2) // Cleanup twice per TTL period
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		for key, item := range c.items {
			if item.IsExpired() {
				delete(c.items, key)
			}
		}
		c.mu.Unlock()
	}
}

// GetOrSet retrieves a value from cache or sets it using the provided function
func (c *Cache) GetOrSet(key string, setFunc func() (interface{}, error)) (interface{}, error) {
	if value, found := c.Get(key); found {
		return value, nil
	}

	value, err := setFunc()
	if err != nil {
		return nil, err
	}

	c.Set(key, value)
	return value, nil
}

// Global caches for common operations
var (
	// CompanyCache caches company data
	CompanyCache = NewCache(30 * time.Minute)

	// JobCache caches job listings
	JobCache = NewCache(10 * time.Minute)

	// HTMLCache caches HTML content
	HTMLCache = NewCache(5 * time.Minute)

	// URLCache caches resolved URLs
	URLCache = NewCache(1 * time.Hour)
)

// GetCachedCompany retrieves a company from cache
func GetCachedCompany(key string) (interface{}, bool) {
	return CompanyCache.Get(key)
}

// SetCachedCompany stores a company in cache
func SetCachedCompany(key string, company interface{}) {
	CompanyCache.Set(key, company)
}

// GetCachedJobs retrieves jobs from cache
func GetCachedJobs(key string) (interface{}, bool) {
	return JobCache.Get(key)
}

// SetCachedJobs stores jobs in cache
func SetCachedJobs(key string, jobs interface{}) {
	JobCache.Set(key, jobs)
}

// GetCachedHTML retrieves HTML content from cache
func GetCachedHTML(key string) (interface{}, bool) {
	return HTMLCache.Get(key)
}

// SetCachedHTML stores HTML content in cache
func SetCachedHTML(key string, html interface{}) {
	HTMLCache.Set(key, html)
}

// GetCachedURL retrieves a resolved URL from cache
func GetCachedURL(key string) (interface{}, bool) {
	return URLCache.Get(key)
}

// SetCachedURL stores a resolved URL in cache
func SetCachedURL(key string, url interface{}) {
	URLCache.Set(key, url)
}

// CacheStats provides statistics about cache usage
type CacheStats struct {
	CompanySize int
	JobSize     int
	HTMLSize    int
	URLSize     int
}

// GetCacheStats returns statistics for all global caches
func GetCacheStats() CacheStats {
	return CacheStats{
		CompanySize: CompanyCache.Size(),
		JobSize:     JobCache.Size(),
		HTMLSize:    HTMLCache.Size(),
		URLSize:     URLCache.Size(),
	}
}

// ClearAllCaches clears all global caches
func ClearAllCaches() {
	CompanyCache.Clear()
	JobCache.Clear()
	HTMLCache.Clear()
	URLCache.Clear()
}

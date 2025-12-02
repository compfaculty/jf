package utils

import (
	"strings"
	"sync"
	"sync/atomic"
)

// StringPool provides string interning to reduce memory usage for repeated strings
type StringPool struct {
	mu    sync.RWMutex
	pool  map[string]string
	stats struct {
		hits   int64
		misses int64
	}
}

// NewStringPool creates a new StringPool
func NewStringPool() *StringPool {
	return &StringPool{
		pool: make(map[string]string),
	}
}

// Global string pool instance
var globalStringPool = NewStringPool()

// Intern returns an interned version of the string, reducing memory usage for repeated strings
func (sp *StringPool) Intern(s string) string {
	if s == "" {
		return s
	}

	sp.mu.RLock()
	if interned, ok := sp.pool[s]; ok {
		sp.mu.RUnlock()
		atomic.AddInt64(&sp.stats.hits, 1)
		return interned
	}
	sp.mu.RUnlock()

	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Double-check after acquiring write lock
	if interned, ok := sp.pool[s]; ok {
		atomic.AddInt64(&sp.stats.hits, 1)
		return interned
	}

	sp.pool[s] = s
	atomic.AddInt64(&sp.stats.misses, 1)
	return s
}

// InternString is a convenience function using the global string pool
func InternString(s string) string {
	return globalStringPool.Intern(s)
}

// GetStats returns pool statistics for monitoring
func (sp *StringPool) GetStats() (hits, misses int64) {
	// Use atomic loads for thread-safe reads
	return atomic.LoadInt64(&sp.stats.hits), atomic.LoadInt64(&sp.stats.misses)
}

// OptimizedStringBuilder provides efficient string building with pre-allocation
type OptimizedStringBuilder struct {
	builder strings.Builder
}

// NewOptimizedStringBuilder creates a new builder with estimated capacity
func NewOptimizedStringBuilder(capacity int) *OptimizedStringBuilder {
	sb := &OptimizedStringBuilder{}
	sb.builder.Grow(capacity)
	return sb
}

// WriteString efficiently writes a string
func (sb *OptimizedStringBuilder) WriteString(s string) {
	sb.builder.WriteString(s)
}

// WriteRune efficiently writes a rune
func (sb *OptimizedStringBuilder) WriteRune(r rune) {
	sb.builder.WriteRune(r)
}

// WriteByte efficiently writes a byte
func (sb *OptimizedStringBuilder) WriteByte(b byte) error {
	return sb.builder.WriteByte(b)
}

// String returns the built string
func (sb *OptimizedStringBuilder) String() string {
	return sb.builder.String()
}

// Len returns the current length
func (sb *OptimizedStringBuilder) Len() int {
	return sb.builder.Len()
}

// Cap returns the current capacity
func (sb *OptimizedStringBuilder) Cap() int {
	return sb.builder.Cap()
}

// Reset resets the builder for reuse
func (sb *OptimizedStringBuilder) Reset() {
	sb.builder.Reset()
}

// OptimizedJoin efficiently joins strings with a separator
func OptimizedJoin(sep string, elems []string) string {
	if len(elems) == 0 {
		return ""
	}
	if len(elems) == 1 {
		return elems[0]
	}

	// Calculate total length needed
	n := len(sep) * (len(elems) - 1)
	for i := 0; i < len(elems); i++ {
		n += len(elems[i])
	}

	sb := NewOptimizedStringBuilder(n)
	sb.WriteString(elems[0])
	for _, s := range elems[1:] {
		sb.WriteString(sep)
		sb.WriteString(s)
	}
	return sb.String()
}

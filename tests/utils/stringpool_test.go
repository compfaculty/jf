package utils_test

import (
	"sync"
	"testing"

	"jf/internal/utils"
)

func TestStringPool(t *testing.T) {
	pool := utils.NewStringPool()

	tests := []string{
		"hello world",
		"test string",
		"",
		"repeated string",
		"repeated string", // Should return same instance
		"UPPERCASE",
		"uppercase",
	}

	// Test basic interning
	for i, test := range tests {
		result := pool.Intern(test)
		if result != test {
			t.Errorf("Intern(%q) = %q, want %q", test, result, test)
		}

		// Test that repeated strings return the same instance
		if test == "repeated string" && i > 3 {
			result2 := pool.Intern(test)
			if result != result2 {
				t.Errorf("Repeated intern returned different instances")
			}
		}
	}

	// Test stats
	hits, misses := pool.GetStats()
	if hits < 1 {
		t.Errorf("Expected at least 1 hit for repeated string, got %d", hits)
	}
	if misses < 1 {
		t.Errorf("Expected at least 1 miss, got %d", misses)
	}
}

func TestStringPoolConcurrency(t *testing.T) {
	pool := utils.NewStringPool()

	const numGoroutines = 100
	const numStrings = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Test concurrent access
	for i := 0; i < numGoroutines; i++ {
		go func(goroutine int) {
			defer wg.Done()
			for j := 0; j < numStrings; j++ {
				str := string(rune('a' + (j % 26)))
				result := pool.Intern(str)
				if result != str {
					t.Errorf("Concurrent intern failed: got %q, want %q", result, str)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify final state is consistent
	for j := 0; j < numStrings; j++ {
		str := string(rune('a' + (j % 26)))
		result := pool.Intern(str)
		if result != str {
			t.Errorf("Post-concurrent intern failed: got %q, want %q", result, str)
		}
	}
}

func TestOptimizedStringBuilder(t *testing.T) {
	tests := []struct {
		name       string
		capacity   int
		operations []func(*utils.OptimizedStringBuilder)
		expected   string
	}{
		{
			name:       "empty",
			capacity:   0,
			operations: []func(*utils.OptimizedStringBuilder){},
			expected:   "",
		},
		{
			name:     "simple string",
			capacity: 10,
			operations: []func(*utils.OptimizedStringBuilder){
				func(sb *utils.OptimizedStringBuilder) { sb.WriteString("hello") },
			},
			expected: "hello",
		},
		{
			name:     "multiple operations",
			capacity: 20,
			operations: []func(*utils.OptimizedStringBuilder){
				func(sb *utils.OptimizedStringBuilder) { sb.WriteString("hello") },
				func(sb *utils.OptimizedStringBuilder) { sb.WriteRune(' ') },
				func(sb *utils.OptimizedStringBuilder) { sb.WriteString("world") },
				func(sb *utils.OptimizedStringBuilder) { sb.WriteByte('!') },
			},
			expected: "hello world!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := utils.NewOptimizedStringBuilder(tt.capacity)

			for _, op := range tt.operations {
				op(sb)
			}

			result := sb.String()
			if result != tt.expected {
				t.Errorf("OptimizedStringBuilder = %q, want %q", result, tt.expected)
			}

			if sb.Len() != len(tt.expected) {
				t.Errorf("Len() = %d, want %d", sb.Len(), len(tt.expected))
			}
		})
	}
}

func TestOptimizedStringBuilderReset(t *testing.T) {
	sb := utils.NewOptimizedStringBuilder(10)
	sb.WriteString("test")

	if sb.String() != "test" {
		t.Errorf("Initial string = %q, want %q", sb.String(), "test")
	}

	sb.Reset()

	if sb.String() != "" {
		t.Errorf("After reset = %q, want %q", sb.String(), "")
	}

	if sb.Len() != 0 {
		t.Errorf("After reset Len() = %d, want 0", sb.Len())
	}
}

func TestOptimizedJoin(t *testing.T) {
	tests := []struct {
		name     string
		sep      string
		elems    []string
		expected string
	}{
		{
			name:     "empty slice",
			sep:      ",",
			elems:    []string{},
			expected: "",
		},
		{
			name:     "single element",
			sep:      ",",
			elems:    []string{"hello"},
			expected: "hello",
		},
		{
			name:     "multiple elements",
			sep:      ",",
			elems:    []string{"hello", "world", "test"},
			expected: "hello,world,test",
		},
		{
			name:     "space separator",
			sep:      " ",
			elems:    []string{"hello", "world"},
			expected: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utils.OptimizedJoin(tt.sep, tt.elems)
			if result != tt.expected {
				t.Errorf("OptimizedJoin(%q, %v) = %q, want %q", tt.sep, tt.elems, result, tt.expected)
			}
		})
	}
}

func BenchmarkStringPool(b *testing.B) {
	pool := utils.NewStringPool()

	testStrings := []string{
		"hello world",
		"test string",
		"another test",
		"repeated string",
		"unique string",
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		str := testStrings[i%len(testStrings)]
		pool.Intern(str)
	}
}

func BenchmarkOptimizedStringBuilder(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sb := utils.NewOptimizedStringBuilder(100)
		sb.WriteString("hello")
		sb.WriteRune(' ')
		sb.WriteString("world")
		sb.WriteByte('!')
		_ = sb.String()
	}
}

func BenchmarkOptimizedJoin(b *testing.B) {
	elems := []string{"hello", "world", "test", "string", "optimization"}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = utils.OptimizedJoin(" ", elems)
	}
}

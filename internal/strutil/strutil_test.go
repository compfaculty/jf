package strutil

import (
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"  hello  world  ", "hello world"},
		{"HELLO WORLD", "hello world"},
		{"Hello\t\nWorld", "hello world"},
		{"   ", ""},
		{"multiple    spaces", "multiple spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := Normalize(tt.input)
			if result != tt.expected {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestContainsFold(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"Hello World", "hello", true},
		{"Hello World", "WORLD", true},
		{"Hello World", "foo", false},
		{"", "test", false},
		{"test", "", true},
		{"Test", "test", true},
	}

	for _, tt := range tests {
		t.Run(tt.s+"/"+tt.substr, func(t *testing.T) {
			result := ContainsFold(tt.s, tt.substr)
			if result != tt.expected {
				t.Errorf("ContainsFold(%q, %q) = %v, want %v", tt.s, tt.substr, result, tt.expected)
			}
		})
	}
}

func TestTokens(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", []string{}},
		{"hello world", []string{"hello", "world"}},
		{"Hello, World!", []string{"hello", "world"}},
		{"123abc456def", []string{"123abc456def"}},
		{"test@email.com", []string{"test", "email", "com"}},
		{"multiple   spaces", []string{"multiple", "spaces"}},
		{"UPPERCASE lowercase", []string{"uppercase", "lowercase"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := Tokens(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("Tokens(%q) length = %d, want %d", tt.input, len(result), len(tt.expected))
				return
			}
			for i, token := range result {
				if token != tt.expected[i] {
					t.Errorf("Tokens(%q)[%d] = %q, want %q", tt.input, i, token, tt.expected[i])
				}
			}
		})
	}
}

func TestCanonURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"https://example.com/", "https://example.com/"},
		{"https://EXAMPLE.COM/path", "https://example.com/path"},
		{"https://example.com/path?utm_source=test&id=123", "https://example.com/path?id=123"},
		{"https://example.com/path?gclid=abc123&other=value", "https://example.com/path?other=value"},
		{"https://example.com/path#fragment", "https://example.com/path"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := CanonURL(tt.input)
			if result != tt.expected {
				t.Errorf("CanonURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSHA16(t *testing.T) {
	tests := []struct {
		input string
	}{
		{""},
		{"hello world"},
		{"test string"},
		{"another test"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SHA16(tt.input)
			if len(result) != 16 {
				t.Errorf("SHA16(%q) length = %d, want 16", tt.input, len(result))
			}
			// Test consistency
			result2 := SHA16(tt.input)
			if result != result2 {
				t.Errorf("SHA16(%q) inconsistent results: %q vs %q", tt.input, result, result2)
			}
		})
	}
}

func TestMin(t *testing.T) {
	tests := []struct {
		a        float64
		b        float64
		expected float64
	}{
		{1.0, 2.0, 1.0},
		{2.0, 1.0, 1.0},
		{1.5, 1.5, 1.5},
		{-1.0, 1.0, -1.0},
		{0.0, 0.0, 0.0},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := Min(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Min(%f, %f) = %f, want %f", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestMax(t *testing.T) {
	tests := []struct {
		a        float64
		b        float64
		expected float64
	}{
		{1.0, 2.0, 2.0},
		{2.0, 1.0, 2.0},
		{1.5, 1.5, 1.5},
		{-1.0, 1.0, 1.0},
		{0.0, 0.0, 0.0},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := Max(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Max(%f, %f) = %f, want %f", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestHasPathPrefixSafe(t *testing.T) {
	tests := []struct {
		path     string
		prefix   string
		expected bool
	}{
		{"/path/to/file", "/path", true},
		{"/path/to/file", "/path/", true},
		{"/path/to/file", "/other", false},
		{"", "/path", false},
		{"/path/to/file", "", true},
		{"/path/", "/path", true},
		{"/path", "/path", true},
	}

	for _, tt := range tests {
		t.Run(tt.path+"/"+tt.prefix, func(t *testing.T) {
			result := HasPathPrefixSafe(tt.path, tt.prefix)
			if result != tt.expected {
				t.Errorf("HasPathPrefixSafe(%q, %q) = %v, want %v", tt.path, tt.prefix, result, tt.expected)
			}
		})
	}
}

package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jf/internal/httpx"
)

func TestNew(t *testing.T) {
	t.Run("Default Configuration", func(t *testing.T) {
		cfg := httpx.HttpClientConfig{
			Timeout:  30 * time.Second,
			RPS:      10,
			Burst:    5,
			RetryMax: 3,
		}
		client := httpx.New(cfg)

		if client == nil {
			t.Fatal("Expected HTTP client to be created")
		}
	})

	t.Run("Custom Configuration", func(t *testing.T) {
		cfg := httpx.HttpClientConfig{
			Timeout:      15 * time.Second,
			RPS:          5,
			Burst:        3,
			RetryMax:     2,
			RetryWaitMin: 100 * time.Millisecond,
			RetryWaitMax: 1 * time.Second,
			UserAgent:    "Custom User Agent",
		}

		client := httpx.New(cfg)

		if client == nil {
			t.Fatal("Expected HTTP client to be created")
		}
	})
}

func TestClient_Do(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check User-Agent
		userAgent := r.Header.Get("User-Agent")
		if userAgent == "" {
			t.Error("Expected User-Agent header to be set")
		}

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test response"))
	}))
	defer server.Close()

	cfg := httpx.HttpClientConfig{
		Timeout:  5 * time.Second,
		RetryMax: 3,
	}
	client := httpx.New(cfg)

	t.Run("Successful Request", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Read response body
		buf := make([]byte, 100)
		n, _ := resp.Body.Read(buf)
		responseBody := string(buf[:n])
		if responseBody != "test response" {
			t.Errorf("Expected 'test response', got %q", responseBody)
		}
	})

	t.Run("Request with Context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to make request with context: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})
}

func TestClient_RetryMechanism(t *testing.T) {
	retryCount := 0
	maxRetries := 3

	// Create a test server that fails first few times
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retryCount++
		if retryCount < maxRetries {
			// Return server error for first few requests
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("server error"))
		} else {
			// Return success on final request
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		}
	}))
	defer server.Close()

	cfg := httpx.HttpClientConfig{
		Timeout:      5 * time.Second,
		RetryMax:     maxRetries,
		RetryWaitMin: 10 * time.Millisecond, // Short delay for testing
		RetryWaitMax: 50 * time.Millisecond,
	}

	client := httpx.New(cfg)

	t.Run("Retry on Server Error", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to make request after retries: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 after retries, got %d", resp.StatusCode)
		}

		if retryCount < maxRetries {
			t.Errorf("Expected at least %d attempts, got %d", maxRetries, retryCount)
		}
	})
}

func TestClient_UserAgent(t *testing.T) {
	var receivedUserAgent string

	// Create a test server that captures User-Agent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	t.Run("Default User Agent", func(t *testing.T) {
		cfg := httpx.HttpClientConfig{
			Timeout: 5 * time.Second,
		}
		client := httpx.New(cfg)

		req, err := http.NewRequest("GET", server.URL, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		_, err = client.Do(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}

		if receivedUserAgent == "" {
			t.Error("Expected User-Agent to be set")
		}

		// Check that it contains expected components
		if !strings.Contains(receivedUserAgent, "Mozilla") {
			t.Errorf("Expected User-Agent to contain 'Mozilla', got %q", receivedUserAgent)
		}
	})

	t.Run("Custom User Agent", func(t *testing.T) {
		cfg := httpx.HttpClientConfig{
			Timeout:   5 * time.Second,
			UserAgent: "Custom-Test-Agent/1.0",
		}

		client := httpx.New(cfg)

		req, err := http.NewRequest("GET", server.URL, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		_, err = client.Do(req)
		if err != nil {
			t.Fatalf("Failed to make request with custom client: %v", err)
		}

		if receivedUserAgent != "Custom-Test-Agent/1.0" {
			t.Errorf("Expected custom User-Agent, got %q", receivedUserAgent)
		}
	})
}

func TestClient_Timeout(t *testing.T) {
	// Create a test server with delay
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // Delay longer than timeout
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("delayed response"))
	}))
	defer server.Close()

	cfg := httpx.HttpClientConfig{
		Timeout:  50 * time.Millisecond, // Short timeout
		RetryMax: 1,                     // No retries for this test
	}

	client := httpx.New(cfg)

	t.Run("Request Timeout", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		_, err = client.Do(req)
		if err == nil {
			t.Error("Expected request to timeout")
		}

		if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "timeout") {
			t.Errorf("Expected timeout error, got %v", err)
		}
	})
}

func TestClient_ContextCancellation(t *testing.T) {
	// Create a test server with delay
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("delayed response"))
	}))
	defer server.Close()

	cfg := httpx.HttpClientConfig{
		Timeout:  5 * time.Second,
		RetryMax: 1,
	}
	client := httpx.New(cfg)

	t.Run("Context Cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		_, err = client.Do(req)
		if err == nil {
			t.Error("Expected request to be cancelled")
		}

		if !strings.Contains(err.Error(), "context deadline exceeded") {
			t.Errorf("Expected context cancellation error, got %v", err)
		}
	})
}

func TestClient_RateLimiting(t *testing.T) {
	requestCount := 0

	// Create a test server that counts requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	// Test with rate limiting enabled
	cfg := httpx.HttpClientConfig{
		Timeout:  5 * time.Second,
		RPS:      10, // 10 requests per second
		Burst:    5,
		RetryMax: 1,
	}
	client := httpx.New(cfg)

	t.Run("Rate Limited Requests", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			req, err := http.NewRequest("GET", server.URL, nil)
			if err != nil {
				t.Fatalf("Failed to create request %d: %v", i, err)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Failed to make request %d: %v", i, err)
			}
			resp.Body.Close()
		}

		if requestCount != 5 {
			t.Errorf("Expected 5 requests, got %d", requestCount)
		}
	})
}

func BenchmarkClient(b *testing.B) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("benchmark response"))
	}))
	defer server.Close()

	cfg := httpx.HttpClientConfig{
		Timeout:  5 * time.Second,
		RetryMax: 3,
	}
	client := httpx.New(cfg)

	b.Run("Do", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			req, _ := http.NewRequest("GET", server.URL, nil)
			resp, err := client.Do(req)
			if err != nil {
				b.Fatalf("Failed to make request: %v", err)
			}
			resp.Body.Close()
		}
	})

	b.Run("DoWithContext", func(b *testing.B) {
		ctx := context.Background()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
			resp, err := client.Do(req)
			if err != nil {
				b.Fatalf("Failed to make request: %v", err)
			}
			resp.Body.Close()
		}
	})
}

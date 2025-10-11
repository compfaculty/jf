package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	t.Run("Default Configuration", func(t *testing.T) {
		client := New(HttpClientConfig{})

		if client == nil {
			t.Fatal("Expected HTTP client to be created")
		}

		// Test that client has proper timeout
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", "http://example.com", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// This should not panic and should respect the context timeout
		_, err = client.Do(req)
		// We expect an error since example.com might not be reachable, but the client should handle it gracefully
		if err != nil && !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "no such host") {
			t.Errorf("Unexpected error: %v", err)
		}
	})

	t.Run("Custom Configuration", func(t *testing.T) {
		config := HttpClientConfig{
			Timeout:   30 * time.Second,
			RetryMax:  3,
			UserAgent: "Custom User Agent",
		}

		client := New(config)

		if client == nil {
			t.Fatal("Expected HTTP client to be created")
		}
	})
}

func TestHTTPClient_Get(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected GET request, got %s", r.Method)
		}

		// Check User-Agent
		userAgent := r.Header.Get("User-Agent")
		if userAgent == "" {
			t.Error("Expected User-Agent header to be set")
		}

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	}))
	defer server.Close()

	client := New(HttpClientConfig{})

	t.Run("Successful GET Request", func(t *testing.T) {
		req, _ := http.NewRequest("GET", server.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to make GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Read response body
		buf := make([]byte, 100)
		n, err := resp.Body.Read(buf)
		if err != nil && err.Error() != "EOF" {
			t.Errorf("Failed to read response body: %v", err)
		}

		responseBody := string(buf[:n])
		if responseBody != "test response" {
			t.Errorf("Expected 'test response', got %q", responseBody)
		}
	})

	t.Run("Request with Context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to make GET request with context: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})
}

func TestHTTPClient_Post(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		// Check Content-Type
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got %s", contentType)
		}

		// Read request body
		buf := make([]byte, 100)
		n, err := r.Body.Read(buf)
		if err != nil && err.Error() != "EOF" {
			t.Errorf("Failed to read request body: %v", err)
		}

		requestBody := string(buf[:n])
		if requestBody != `{"test":"data"}` {
			t.Errorf("Expected '{\"test\":\"data\"}', got %q", requestBody)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}))
	defer server.Close()

	client := New(HttpClientConfig{})

	t.Run("Successful POST Request", func(t *testing.T) {
		payload := `{"test":"data"}`
		req, _ := http.NewRequest("POST", server.URL, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to make POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})
}

func TestHTTPClient_RetryMechanism(t *testing.T) {
	retryCount := 0
	maxRetries := 3

	// Create a test server that fails first few times
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retryCount++
		if retryCount <= maxRetries-1 {
			// Return server error for first few requests
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("server error"))
		} else {
			// Return success on final request
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		}
	}))
	defer server.Close()

	config := HttpClientConfig{
		Timeout:      5 * time.Second,
		RetryMax:     maxRetries,
		RetryWaitMin: 10 * time.Millisecond, // Short delay for testing
	}

	client := New(config)

	t.Run("Retry on Server Error", func(t *testing.T) {
		req, _ := http.NewRequest("GET", server.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to make GET request after retries: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 after retries, got %d", resp.StatusCode)
		}

		if retryCount != maxRetries {
			t.Errorf("Expected %d retries, got %d", maxRetries, retryCount)
		}
	})
}

func TestHTTPClient_RateLimiting(t *testing.T) {
	requestCount := 0

	// Create a test server that counts requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	// Test that multiple requests are made (rate limiting is internal)
	client := New(HttpClientConfig{})

	t.Run("Multiple Requests", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			req, _ := http.NewRequest("GET", server.URL, nil)
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

func TestHTTPClient_UserAgent(t *testing.T) {
	var receivedUserAgent string

	// Create a test server that captures User-Agent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := New(HttpClientConfig{})

	t.Run("Default User Agent", func(t *testing.T) {
		req, _ := http.NewRequest("GET", server.URL, nil)
		_, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}

		if receivedUserAgent == "" {
			t.Error("Expected User-Agent to be set")
		}

		// Check that it contains expected components (should be DefaultUserAgent)
		if !strings.Contains(receivedUserAgent, "Mozilla") {
			t.Errorf("Expected User-Agent to contain 'Mozilla', got %q", receivedUserAgent)
		}
	})

	t.Run("Custom User Agent", func(t *testing.T) {
		config := HttpClientConfig{
			UserAgent: "Custom-Test-Agent/1.0",
		}

		customClient := New(config)

		req, _ := http.NewRequest("GET", server.URL, nil)
		_, err := customClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to make request with custom client: %v", err)
		}

		if receivedUserAgent != "Custom-Test-Agent/1.0" {
			t.Errorf("Expected custom User-Agent, got %q", receivedUserAgent)
		}
	})
}

func TestHTTPClient_Timeout(t *testing.T) {
	// Create a test server with delay
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Delay longer than timeout
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("delayed response"))
	}))
	defer server.Close()

	config := HttpClientConfig{
		Timeout: 100 * time.Millisecond, // Short timeout
	}

	client := New(config)

	t.Run("Request Timeout", func(t *testing.T) {
		req, _ := http.NewRequest("GET", server.URL, nil)
		_, err := client.Do(req)
		if err == nil {
			t.Error("Expected request to timeout")
		}

		// Accept either "timeout" or "context deadline exceeded" as valid timeout errors
		if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "context deadline exceeded") {
			t.Errorf("Expected timeout error, got %v", err)
		}
	})
}

func TestHTTPClient_ContextCancellation(t *testing.T) {
	// Create a test server with delay
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("delayed response"))
	}))
	defer server.Close()

	client := New(HttpClientConfig{})

	t.Run("Context Cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
		_, err := client.Do(req)
		if err == nil {
			t.Error("Expected request to be cancelled")
		}

		if !strings.Contains(err.Error(), "context deadline exceeded") {
			t.Errorf("Expected context cancellation error, got %v", err)
		}
	})
}

func BenchmarkHTTPClient(b *testing.B) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("benchmark response"))
	}))
	defer server.Close()

	client := New(HttpClientConfig{})

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

	b.Run("Post", func(b *testing.B) {
		payload := `{"test":"data"}`
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			req, _ := http.NewRequest("POST", server.URL, strings.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				b.Fatalf("Failed to make request: %v", err)
			}
			resp.Body.Close()
		}
	})
}

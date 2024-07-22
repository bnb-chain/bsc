package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// Mock mockfaucet struct
type mockfaucet struct {
	limiter *MockIPRateLimiter
}

// Mock MockIPRateLimiter struct and methods
type MockIPRateLimiter struct {
	ips map[string]*rate.Limiter
	mu  *sync.RWMutex
	r   rate.Limit
	b   int
}

func MockNewIPRateLimiter(r rate.Limit, b int) *MockIPRateLimiter {
	i := &MockIPRateLimiter{
		ips: make(map[string]*rate.Limiter),
		mu:  &sync.RWMutex{},
		r:   r,
		b:   b,
	}
	return i
}

func (i *MockIPRateLimiter) AddIP(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()
	limiter := rate.NewLimiter(i.r, i.b)
	i.ips[ip] = limiter
	return limiter
}

func (i *MockIPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	i.mu.Lock()
	limiter, exists := i.ips[ip]
	if !exists {
		i.mu.Unlock()
		return i.AddIP(ip)
	}
	i.mu.Unlock()
	return limiter
}

// Mock apiHandler
func (f *mockfaucet) apiHandler(w http.ResponseWriter, r *http.Request) {
	//ip := r.RemoteAddr
	ip := "test-ip" // Use a constant IP for testing
	limiter := f.limiter.GetLimiter(ip)
	if !limiter.Allow() {
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Mock mocknewFaucet function
func mocknewFaucet() *mockfaucet {
	return &mockfaucet{
		limiter: MockNewIPRateLimiter(rate.Limit(1), 2), // 1 request per second, burst of 2
	}
}

func TestMockFaucetRateLimiting(t *testing.T) {
	// Create a mockfaucet with rate limiting
	f := mocknewFaucet()

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(f.apiHandler))
	defer server.Close()

	// Helper function to make a request
	makeRequest := func() int {
		resp, err := http.Get(server.URL)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	// Test rapid requests

	results := make([]int, 5)

	for i := 0; i < 5; i++ {
		results[i] = makeRequest()
		time.Sleep(10 * time.Millisecond) // Small delay to ensure order
	}

	// Check results
	successCount := 0
	rateLimitCount := 0
	for _, status := range results {
		if status == http.StatusOK {
			successCount++
		} else if status == http.StatusTooManyRequests {
			rateLimitCount++
		}
	}

	// We expect 2 successful requests (due to burst) and 3 rate-limited requests
	if successCount != 2 || rateLimitCount != 3 {
		t.Errorf("Expected 2 successful and 3 rate-limited requests, got %d successful and %d rate-limited", successCount, rateLimitCount)
	}

	// Wait for rate limit to reset
	time.Sleep(2 * time.Second)

	// Make another request, it should succeed
	status := makeRequest()
	if status != http.StatusOK {
		t.Errorf("Expected success after rate limit reset, got status %d", status)
	}
}

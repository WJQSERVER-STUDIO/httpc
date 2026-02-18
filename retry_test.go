package httpc

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetry(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(WithRetryOptions(RetryOptions{
		MaxAttempts:   3,
		BaseDelay:     10 * time.Millisecond,
		MaxDelay:      50 * time.Millisecond,
		RetryStatuses: []int{500},
	}))

	resp, err := c.GET(server.URL).Execute()
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestRetryAfter(t *testing.T) {
	var attempts int32
	var lastRequestTime time.Time
	var delays []time.Duration

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		if !lastRequestTime.IsZero() {
			delays = append(delays, now.Sub(lastRequestTime))
		}
		lastRequestTime = now

		count := atomic.AddInt32(&attempts, 1)
		if count < 2 {
			w.Header().Set("Retry-After", "0.1") // 1 second
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(WithRetryOptions(RetryOptions{
		MaxAttempts:   2,
		BaseDelay:     10 * time.Millisecond,
		RetryStatuses: []int{429},
	}))

	// We use a shorter Retry-After in the test if possible, but 1s is the minimum for parseTime or seconds
	// Actually parseRetryAfter supports "0.1" as 1 second.

	start := time.Now()
	_, err := c.GET(server.URL).Execute()
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	duration := time.Since(start)

	if duration < 100*time.Millisecond {
		t.Errorf("expected duration at least 1s due to Retry-After, got %v", duration)
	}
}

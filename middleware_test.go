package httpc

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddleware(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Middleware") != "true" {
			t.Errorf("middleware did not set header")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	middleware := func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			req.Header.Set("X-Middleware", "true")
			return next.RoundTrip(req)
		})
	}

	c := New(WithMiddleware(middleware))
	_, err := c.GET(server.URL).Execute()
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
}

func TestMiddlewareOrder(t *testing.T) {
	var order []string
	m1 := func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			order = append(order, "m1-in")
			resp, err := next.RoundTrip(req)
			order = append(order, "m1-out")
			return resp, err
		})
	}
	m2 := func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			order = append(order, "m2-in")
			resp, err := next.RoundTrip(req)
			order = append(order, "m2-out")
			return resp, err
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(WithMiddleware(m1, m2))
	_, err := c.GET(server.URL).Execute()
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	expected := []string{"m1-in", "m2-in", "handler", "m2-out", "m1-out"}
	if len(order) != len(expected) {
		t.Fatalf("expected order length %d, got %d", len(expected), len(order))
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("at index %d: expected %s, got %s", i, v, order[i])
		}
	}
}

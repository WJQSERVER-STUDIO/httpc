package httpc

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("custom error message"))
	}))
	defer server.Close()

	c := New()
	_, err := c.GET(server.URL).Bytes()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T", err)
	}

	if httpErr.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", httpErr.StatusCode)
	}
	if string(httpErr.Body) != "custom error message" {
		t.Errorf("expected custom error message, got %s", string(httpErr.Body))
	}
}

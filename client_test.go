package httpc

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_New(t *testing.T) {
	c := New(
		WithUserAgent("TestAgent"),
		WithTimeout(5 * time.Second),
	)

	if c.userAgent != "TestAgent" {
		t.Errorf("expected userAgent TestAgent, got %s", c.userAgent)
	}
	if c.client.Timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", c.client.Timeout)
	}
}

func TestClient_Get(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected method GET, got %s", r.Method)
		}
		if r.Header.Get("User-Agent") != "Touka HTTP Client/v0" {
			t.Errorf("expected default User-Agent, got %s", r.Header.Get("User-Agent"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer server.Close()

	c := New()
	resp, err := c.Get(server.URL)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestClient_PostJSON(t *testing.T) {
	type TestData struct {
		Name string `json:"name"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected method POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{\"status\":\"ok\"}"))
	}))
	defer server.Close()

	c := New()
	data := TestData{Name: "test"}
	resp, err := c.PostJSON(nil, server.URL, data)
	if err != nil {
		t.Fatalf("PostJSON failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

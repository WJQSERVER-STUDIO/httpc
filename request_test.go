package httpc

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestBuilder_DecodeJSON(t *testing.T) {
	type Response struct {
		Message string `json:"message"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{\"message\": \"hello\"}"))
	}))
	defer server.Close()

	c := New()
	var res Response
	err := c.GET(server.URL).DecodeJSON(&res)
	if err != nil {
		t.Fatalf("DecodeJSON failed: %v", err)
	}

	if res.Message != "hello" {
		t.Errorf("expected hello, got %s", res.Message)
	}
}

func TestRequestBuilder_QueryParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("foo") != "bar" {
			t.Errorf("expected foo=bar, got %s", q.Get("foo"))
		}
		if q.Get("baz") != "qux" {
			t.Errorf("expected baz=qux, got %s", q.Get("baz"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New()
	resp, err := c.GET(server.URL).
		SetQueryParam("foo", "bar").
		AddQueryParam("baz", "qux").
		Execute()
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	resp.Body.Close()
}

func TestRequestBuilder_Headers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "value" {
			t.Errorf("expected X-Test: value, got %s", r.Header.Get("X-Test"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New()
	resp, err := c.GET(server.URL).
		SetHeader("X-Test", "value").
		Execute()
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	resp.Body.Close()
}

func TestRequestBuilder_TextAndBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("raw content"))
	}))
	defer server.Close()

	c := New()
	text, err := c.GET(server.URL).Text()
	if err != nil {
		t.Fatalf("Text failed: %v", err)
	}
	if text != "raw content" {
		t.Errorf("expected raw content, got %s", text)
	}

	bytes, err := c.GET(server.URL).Bytes()
	if err != nil {
		t.Fatalf("Bytes failed: %v", err)
	}
	if string(bytes) != "raw content" {
		t.Errorf("expected raw content, got %s", string(bytes))
	}
}

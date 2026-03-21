package httpc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestBuilderBuildMergesQueryAndDefaultHeaders(t *testing.T) {
	client := New(WithUserAgent("test-agent/1.0"))

	req, err := client.GET("https://example.com/search?q=golang").
		SetHeader("X-Test", "value").
		AddQueryParam("page", "1").
		AddQueryParam("page", "2").
		SetQueryParam("lang", "zh").
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if got := req.Header.Get("User-Agent"); got != "test-agent/1.0" {
		t.Fatalf("User-Agent = %q, want %q", got, "test-agent/1.0")
	}
	if got := req.Header.Get("X-Test"); got != "value" {
		t.Fatalf("X-Test = %q, want %q", got, "value")
	}

	query := req.URL.Query()
	if got := query.Get("q"); got != "golang" {
		t.Fatalf("q = %q, want %q", got, "golang")
	}
	if got := query["page"]; len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Fatalf("page = %#v, want [1 2]", got)
	}
	if got := query.Get("lang"); got != "zh" {
		t.Fatalf("lang = %q, want %q", got, "zh")
	}
}

func TestRequestBuilderNoDefaultHeaders(t *testing.T) {
	client := New(WithUserAgent("test-agent/1.0"))

	req, err := client.GET("https://example.com").NoDefaultHeaders().Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if got := req.Header.Get("User-Agent"); got != "" {
		t.Fatalf("User-Agent = %q, want empty", got)
	}
}

func TestOptionsStackTransportMergeAndProtocols(t *testing.T) {
	client := New(
		WithTransport(&http.Transport{
			DisableCompression: true,
			IdleConnTimeout:    42 * time.Second,
		}),
		WithProtocols(ProtocolsConfig{
			Http1:           true,
			Http2:           false,
			Http2_Cleartext: true,
		}),
	)

	if !client.transport.DisableCompression {
		t.Fatal("DisableCompression = false, want true")
	}
	if got := client.transport.IdleConnTimeout; got != 42*time.Second {
		t.Fatalf("IdleConnTimeout = %v, want %v", got, 42*time.Second)
	}
	if !client.transport.Protocols.HTTP1() {
		t.Fatal("HTTP1 disabled, want enabled")
	}
	if client.transport.Protocols.HTTP2() {
		t.Fatal("HTTP2 enabled, want disabled")
	}
	if !client.transport.Protocols.UnencryptedHTTP2() {
		t.Fatal("UnencryptedHTTP2 disabled, want enabled")
	}
	if !client.transport.ForceAttemptHTTP2 {
		t.Fatal("ForceAttemptHTTP2 = false, want true when h2c is enabled")
	}
}

func TestWithProtocolsForceH2COverridesOtherProtocols(t *testing.T) {
	client := New(WithProtocols(ProtocolsConfig{
		Http1:           true,
		Http2:           true,
		Http2_Cleartext: false,
		ForceH2C:        true,
	}))

	if client.transport.Protocols.HTTP1() {
		t.Fatal("HTTP1 enabled, want disabled")
	}
	if client.transport.Protocols.HTTP2() {
		t.Fatal("HTTP2 enabled, want disabled")
	}
	if !client.transport.Protocols.UnencryptedHTTP2() {
		t.Fatal("UnencryptedHTTP2 disabled, want enabled")
	}
	if client.transport.ForceAttemptHTTP2 {
		t.Fatal("ForceAttemptHTTP2 = true, want false when ForceH2C is used")
	}
}

func TestRetryAndDecodeJSONWithReplayableBody(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := attempts.Add(1)
		if current < 3 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"retry"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := New(WithRetryOptions(RetryOptions{
		MaxAttempts:   3,
		BaseDelay:     time.Millisecond,
		MaxDelay:      5 * time.Millisecond,
		RetryStatuses: []int{http.StatusBadGateway},
	}))

	var resp struct {
		OK bool `json:"ok"`
	}
	if err := client.POST(server.URL).SetRawBody([]byte(`{"ping":true}`)).DecodeJSON(&resp); err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}
	if !resp.OK {
		t.Fatal("decoded response OK = false, want true")
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestHTTPErrorIncludesStatusHeadersAndBodyPreview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Trace", "trace-123")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(strings.Repeat("a", 2048)))
	}))
	defer server.Close()

	_, err := New().GET(server.URL).Text()
	if err == nil {
		t.Fatal("Text() error = nil, want HTTPError")
	}

	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("error type = %T, want *HTTPError", err)
	}
	if httpErr.StatusCode != http.StatusTeapot {
		t.Fatalf("StatusCode = %d, want %d", httpErr.StatusCode, http.StatusTeapot)
	}
	if got := httpErr.Header.Get("X-Trace"); got != "trace-123" {
		t.Fatalf("X-Trace = %q, want %q", got, "trace-123")
	}
	if len(httpErr.Body) != 1024 {
		t.Fatalf("body preview length = %d, want 1024", len(httpErr.Body))
	}
}

func TestPostJSONSetsContentTypeAndHonorsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if !strings.Contains(string(body), `"name":"touka"`) {
			t.Fatalf("body = %s, want JSON payload", string(body))
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := New().PostJSON(ctx, server.URL, map[string]string{"name": "touka"})
	if err != nil {
		t.Fatalf("PostJSON() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.Request == nil || resp.Request.Context() != ctx {
		t.Fatal("request context was not propagated")
	}
}

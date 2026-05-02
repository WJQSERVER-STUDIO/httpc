package httpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestCalculateExponentialBackoffWithoutJitter(t *testing.T) {
	client := New(WithRetryOptions(RetryOptions{
		BaseDelay: 100 * time.Millisecond,
		MaxDelay:  700 * time.Millisecond,
	}))

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 100 * time.Millisecond},
		{attempt: 1, want: 200 * time.Millisecond},
		{attempt: 2, want: 400 * time.Millisecond},
		{attempt: 3, want: 700 * time.Millisecond},
	}

	for _, tt := range tests {
		if got := client.calculateExponentialBackoff(tt.attempt, false); got != tt.want {
			t.Fatalf("attempt %d: backoff = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestCalculateExponentialBackoffWithJitterUsesRandomFactor(t *testing.T) {
	client := New(WithRetryOptions(RetryOptions{
		BaseDelay: 200 * time.Millisecond,
		MaxDelay:  time.Second,
		Jitter:    true,
	}))
	client.randomFloat64 = func() float64 { return 0.25 }

	got := client.calculateExponentialBackoff(1, true)
	want := 300 * time.Millisecond
	if got != want {
		t.Fatalf("backoff with jitter = %v, want %v", got, want)
	}
}

func TestCalculateExponentialBackoffWithJitterStillHonorsMaxDelay(t *testing.T) {
	client := New(WithRetryOptions(RetryOptions{
		BaseDelay: 500 * time.Millisecond,
		MaxDelay:  800 * time.Millisecond,
		Jitter:    true,
	}))
	client.randomFloat64 = func() float64 { return 0.99 }

	got := client.calculateExponentialBackoff(3, true)
	if got != 800*time.Millisecond {
		t.Fatalf("backoff with jitter cap = %v, want %v", got, 800*time.Millisecond)
	}
}

func TestRedirect301PostSwitchesToGet(t *testing.T) {
	var gotMethod atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod.Store(r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusMovedPermanently)
	}))
	defer redirector.Close()

	resp, err := New().POST(redirector.URL).SetRawBody([]byte(`{"a":1}`)).Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer resp.Body.Close()

	if m, _ := gotMethod.Load().(string); m != http.MethodGet {
		t.Fatalf("redirected method = %q, want GET", m)
	}
}

func TestRedirect307PreservesMethodAndBody(t *testing.T) {
	var gotMethod atomic.Value
	var gotBody atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod.Store(r.Method)
		b, _ := io.ReadAll(r.Body)
		gotBody.Store(string(b))
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	resp, err := New().POST(redirector.URL).SetRawBody([]byte(`payload`)).Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer resp.Body.Close()

	if m, _ := gotMethod.Load().(string); m != http.MethodPost {
		t.Fatalf("redirected method = %q, want POST", m)
	}
	if b, _ := gotBody.Load().(string); b != "payload" {
		t.Fatalf("redirected body = %q, want payload", b)
	}
}

func TestRedirectFollowDisabledReturns3xx(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer redirector.Close()

	resp, err := New(WithFollowRedirects(false)).GET(redirector.URL).Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
}

func TestRedirectMaxRedirectsExceeded(t *testing.T) {
	redirector := httptest.NewServer(nil)
	redirector.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", redirector.URL)
		w.WriteHeader(http.StatusFound)
	})
	defer redirector.Close()

	_, err := New(WithMaxRedirects(2)).GET(redirector.URL).Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want redirect limit error")
	}
	if !strings.Contains(err.Error(), "stopped after 2 redirects") {
		t.Fatalf("error = %v, want stopped after 2 redirects", err)
	}
}

func TestRedirectStripsAuthorizationCrossDomain(t *testing.T) {
	var authHeader atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	u, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://"+u.Host+"/")
		w.WriteHeader(http.StatusFound)
	}))
	defer redirector.Close()

	startURL := strings.Replace(redirector.URL, "127.0.0.1", "localhost", 1)

	resp, err := New().GET(startURL).SetHeader("Authorization", "Bearer token").Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer resp.Body.Close()

	if got, _ := authHeader.Load().(string); got != "" {
		t.Fatalf("Authorization = %q, want empty", got)
	}
}

func TestRedirect303ForcesGet(t *testing.T) {
	var gotMethod atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod.Store(r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusSeeOther)
	}))
	defer redirector.Close()

	resp, err := New().PUT(redirector.URL).SetRawBody([]byte("abc")).Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer resp.Body.Close()

	if m, _ := gotMethod.Load().(string); m != http.MethodGet {
		t.Fatalf("redirected method = %q, want GET", m)
	}
}

func TestRedirect308WithNonReplayableBodyReturns308(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusPermanentRedirect)
	}))
	defer redirector.Close()

	body := io.NopCloser(strings.NewReader("stream-body"))
	req, err := http.NewRequest(http.MethodPost, redirector.URL, body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.GetBody = nil
	req.ContentLength = int64(len("stream-body"))

	resp, err := New().Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("status = %d, want 308", resp.StatusCode)
	}
}

func TestRedirectWithoutLocationReturnsOriginal3xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	resp, err := New().GET(server.URL).Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
}

func TestRedirect302PostSwitchesToGet(t *testing.T) {
	var gotMethod atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod.Store(r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	resp, err := New().POST(redirector.URL).SetRawBody([]byte(`{"a":1}`)).Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer resp.Body.Close()

	if m, _ := gotMethod.Load().(string); m != http.MethodGet {
		t.Fatalf("redirected method = %q, want GET", m)
	}
}

func TestRedirect308PreservesMethodAndBodyWhenReplayable(t *testing.T) {
	var gotMethod atomic.Value
	var gotBody atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod.Store(r.Method)
		b, _ := io.ReadAll(r.Body)
		gotBody.Store(string(b))
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusPermanentRedirect)
	}))
	defer redirector.Close()

	resp, err := New().POST(redirector.URL).SetRawBody([]byte("payload-308")).Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer resp.Body.Close()

	if m, _ := gotMethod.Load().(string); m != http.MethodPost {
		t.Fatalf("redirected method = %q, want POST", m)
	}
	if b, _ := gotBody.Load().(string); b != "payload-308" {
		t.Fatalf("redirected body = %q, want payload-308", b)
	}
}

func TestRedirectRelativeLocationIsResolved(t *testing.T) {
	var finalPath atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			w.Header().Set("Location", "/final")
			w.WriteHeader(http.StatusFound)
		case "/final":
			finalPath.Store(r.URL.Path)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	resp, err := New().GET(server.URL + "/start").Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer resp.Body.Close()

	if p, _ := finalPath.Load().(string); p != "/final" {
		t.Fatalf("final path = %q, want /final", p)
	}
}

func TestRedirect307StripsAuthorizationCrossDomain(t *testing.T) {
	var authHeader atomic.Value
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	u, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://"+u.Host+"/")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	startURL := strings.Replace(redirector.URL, "127.0.0.1", "localhost", 1)
	resp, err := New().POST(startURL).SetRawBody([]byte("abc")).SetHeader("Authorization", "Bearer token").Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer resp.Body.Close()

	if got, _ := authHeader.Load().(string); got != "" {
		t.Fatalf("Authorization = %q, want empty", got)
	}
}

func TestRedirectMaxRedirectsZeroReturns3xx(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer redirector.Close()

	resp, err := New(WithMaxRedirects(0)).GET(redirector.URL).Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
}

func TestRedirectMaxRedirectsNegativeReturns3xx(t *testing.T) {
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://example.com")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer redirector.Close()

	resp, err := New(WithMaxRedirects(-5)).GET(redirector.URL).Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", resp.StatusCode)
	}
}

func TestRedirectCheckRedirectUseLastResponse(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer redirector.Close()

	client := New(WithCheckRedirect(func(req *http.Request, via []*http.Request) error {
		return ErrUseLastResponse
	}))

	resp, err := client.GET(redirector.URL).Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
}

func TestRedirectCheckRedirectCustomError(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer redirector.Close()

	customErr := errors.New("custom redirect blocked")
	client := New(WithCheckRedirect(func(req *http.Request, via []*http.Request) error {
		return customErr
	}))

	_, err := client.GET(redirector.URL).Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want custom error")
	}
	if !strings.Contains(err.Error(), "custom redirect blocked") {
		t.Fatalf("error = %v, want custom redirect blocked", err)
	}
}

func TestRedirectCheckRedirectOverridesMaxRedirects(t *testing.T) {
	var redirectCount atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if redirectCount.Add(1) <= 5 {
			w.Header().Set("Location", srv.URL)
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := New(
		WithMaxRedirects(1),
		WithCheckRedirect(func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("custom limit reached")
			}
			return nil
		}),
	)

	_, err := client.GET(srv.URL).Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want custom limit error")
	}
	if !strings.Contains(err.Error(), "custom limit reached") {
		t.Fatalf("error = %v, want custom limit reached", err)
	}
}

func TestBodySlurpCopyNDoesNotBlockOnSmallChunkedBody(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	chunkedRedirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
		_, _ = w.Write([]byte("small"))
	}))
	defer chunkedRedirector.Close()

	done := make(chan struct{})
	var resp *http.Response
	var execErr error

	go func() {
		defer close(done)
		resp, execErr = New(WithMaxRedirects(10)).GET(chunkedRedirector.URL).Execute()
	}()

	select {
	case <-done:
		if execErr != nil {
			t.Fatalf("Execute() error = %v", execErr)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("body slurp blocked: io.CopyN did not return within timeout")
	}
}

func TestBodySlurpCopyNDoesNotBlockOnEmptyChunkedBody(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	chunkedRedirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer chunkedRedirector.Close()

	done := make(chan struct{})
	var resp *http.Response
	var execErr error

	go func() {
		defer close(done)
		resp, execErr = New(WithMaxRedirects(10)).GET(chunkedRedirector.URL).Execute()
	}()

	select {
	case <-done:
		if execErr != nil {
			t.Fatalf("Execute() error = %v", execErr)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("body slurp blocked: io.CopyN did not return within timeout")
	}
}

func TestBodySlurpCopyNReadsAtMostMaxBodySlurpSize(t *testing.T) {
	const maxBodySlurpSize = 2 << 10

	largeBody := strings.Repeat("x", maxBodySlurpSize*4)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
		_, _ = w.Write([]byte(largeBody))
	}))
	defer redirector.Close()

	done := make(chan struct{})
	var resp *http.Response
	var execErr error

	go func() {
		defer close(done)
		resp, execErr = New(WithMaxRedirects(10)).GET(redirector.URL).Execute()
	}()

	select {
	case <-done:
		if execErr != nil {
			t.Fatalf("Execute() error = %v", execErr)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("body slurp blocked on large body: io.CopyN did not return within timeout")
	}
}

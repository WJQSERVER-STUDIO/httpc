package httpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSSEEventRenderMatchesToukaWireFormat(t *testing.T) {
	var buf bytes.Buffer
	event := SSEEvent{
		Id:    "evt-1",
		Event: "tick",
		Data:  "hello\nworld",
		Retry: "3000",
	}

	if err := event.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	want := "id: evt-1\nevent: tick\ndata: hello\ndata: world\nretry: 3000\n\n"
	if got := buf.String(); got != want {
		t.Fatalf("rendered event = %q, want %q", got, want)
	}
}

func TestRequestBuilderSSEParsesToukaStyleEventStream(t *testing.T) {
	handlerErrCh := make(chan error, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			handlerErrCh <- fmt.Errorf("Accept = %q, want text/event-stream", got)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			handlerErrCh <- errors.New("ResponseWriter does not implement http.Flusher")
			return
		}

		first := SSEEvent{
			Id:    "evt-1",
			Event: "tick",
			Data:  "hello\nworld",
			Retry: "3000",
		}
		if err := first.Render(w); err != nil {
			handlerErrCh <- fmt.Errorf("Render() first event error = %v", err)
			return
		}
		flusher.Flush()

		second := SSEEvent{Data: "plain"}
		if err := second.Render(w); err != nil {
			handlerErrCh <- fmt.Errorf("Render() second event error = %v", err)
			return
		}
		flusher.Flush()
	}))
	defer server.Close()

	stream, err := New().GET(server.URL).SSE()
	if err != nil {
		t.Fatalf("SSE() error = %v", err)
	}
	defer stream.Close()

	first, err := stream.Next()
	if err != nil {
		t.Fatalf("first Next() error = %v", err)
	}
	if first.Id != "evt-1" {
		t.Fatalf("first.Id = %q, want evt-1", first.Id)
	}
	if first.Event != "tick" {
		t.Fatalf("first.Event = %q, want tick", first.Event)
	}
	if first.Data != "hello\nworld" {
		t.Fatalf("first.Data = %q, want hello\\nworld", first.Data)
	}
	if first.Retry != "3000" {
		t.Fatalf("first.Retry = %q, want 3000", first.Retry)
	}

	second, err := stream.Next()
	if err != nil {
		t.Fatalf("second Next() error = %v", err)
	}
	if second.Id != "" {
		t.Fatalf("second.Id = %q, want empty", second.Id)
	}
	if second.Event != "" {
		t.Fatalf("second.Event = %q, want empty", second.Event)
	}
	if second.Data != "plain" {
		t.Fatalf("second.Data = %q, want plain", second.Data)
	}
	if second.Retry != "" {
		t.Fatalf("second.Retry = %q, want empty", second.Retry)
	}

	if _, err := stream.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("third Next() error = %v, want io.EOF", err)
	}

	select {
	case err := <-handlerErrCh:
		t.Fatalf("handler error: %v", err)
	default:
	}
}

func TestRequestBuilderSSERejectsUnexpectedContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-sse"))
	}))
	defer server.Close()

	_, err := New().GET(server.URL).SSE()
	if !errors.Is(err, ErrInvalidSSEStream) {
		t.Fatalf("SSE() error = %v, want ErrInvalidSSEStream", err)
	}
}

func TestClientGetSSEReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("fail"))
	}))
	defer server.Close()

	_, err := New().GetSSE(context.Background(), server.URL)
	if err == nil {
		t.Fatal("GetSSE() error = nil, want HTTPError")
	}

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("GetSSE() error type = %T, want *HTTPError", err)
	}
	if httpErr.StatusCode != http.StatusTeapot {
		t.Fatalf("StatusCode = %d, want %d", httpErr.StatusCode, http.StatusTeapot)
	}
}

func TestSSEStreamParsesUnterminatedStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: hello\n")
	}))
	defer server.Close()

	stream, err := New().GET(server.URL).SSE()
	if err != nil {
		t.Fatalf("SSE() error = %v", err)
	}
	defer stream.Close()

	event, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if event.Data != "hello" {
		t.Fatalf("event.Data = %q, want %q", event.Data, "hello")
	}

	_, err = stream.Next()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("second Next() error = %v, want io.EOF", err)
	}
}

func TestSSEStreamIgnoresInvalidRetryField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "retry: 3s\ndata: hello\n\n")
	}))
	defer server.Close()

	stream, err := New().GET(server.URL).SSE()
	if err != nil {
		t.Fatalf("SSE() error = %v", err)
	}
	defer stream.Close()

	event, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if event.Retry != "" {
		t.Fatalf("event.Retry = %q, want empty", event.Retry)
	}
	if event.Data != "hello" {
		t.Fatalf("event.Data = %q, want %q", event.Data, "hello")
	}
}

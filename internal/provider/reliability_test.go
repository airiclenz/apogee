package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const okJSON = `{"choices":[{"message":{"content":"recovered"},"finish_reason":"stop"}]}`

func TestRespond_RetriesThenSucceeds(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, okJSON)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "m", WithMaxRetries(2), WithRetryBaseDelay(time.Millisecond))
	got, err := client.Respond(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if got.Content != "recovered" {
		t.Errorf("Content = %q, want recovered", got.Content)
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("server calls = %d, want 3 (two 503s then success)", n)
	}
}

func TestRespond_RetriesExhausted(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "m", WithMaxRetries(2), WithRetryBaseDelay(time.Millisecond))
	_, err := client.Respond(context.Background(), Request{})
	if err == nil {
		t.Fatal("Respond succeeded, want error after exhausting retries")
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("server calls = %d, want 3 attempts", n)
	}
	// After exhausting retries on a persistent 5xx, the final HTTP status error is
	// surfaced (more informative than a generic retry-count message).
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error = %q, want it to mention HTTP 502", err)
	}
}

func TestRespond_PerAttemptTimeout(t *testing.T) {
	t.Parallel()

	// The handler stalls until released; the client's per-attempt timeout must fire first.
	// release is closed before srv.Close() so the handler always returns (not relying on
	// in-process server-side context propagation).
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	client := NewClient(srv.URL, "m", WithMaxRetries(0), WithRequestTimeout(30*time.Millisecond))
	_, err := client.Respond(context.Background(), Request{})
	if err == nil {
		t.Fatal("Respond succeeded, want a timeout error")
	}
}

func TestRespond_CallerCancellationDoesNotRetry(t *testing.T) {
	t.Parallel()

	var started sync.Once
	startedCh := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		started.Do(func() { close(startedCh) })
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-startedCh
		cancel()
	}()

	client := NewClient(srv.URL, "m", WithMaxRetries(3), WithRetryBaseDelay(time.Millisecond))
	_, err := client.Respond(ctx, Request{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("server calls = %d, want 1 (caller cancellation must not retry)", n)
	}
}

func TestRespond_ContextOverflow(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"the prompt exceeds the maximum context length of this model"}`)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "m").Respond(context.Background(), Request{})
	if !errors.Is(err, ErrContextOverflow) {
		t.Fatalf("error = %v, want ErrContextOverflow", err)
	}
}

func TestRespond_GenericBadRequestNotRetried(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"bad field 'foo'"}`)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "m", WithMaxRetries(2)).Respond(context.Background(), Request{})
	if err == nil || errors.Is(err, ErrContextOverflow) {
		t.Fatalf("error = %v, want a generic HTTP 400 error", err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("server calls = %d, want 1 (a 400 is not retryable)", n)
	}
}

func TestRespond_SanitizesAPIKey(t *testing.T) {
	t.Parallel()

	const secret = "sk-super-secret-key"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "invalid api key: "+secret)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "m", WithAPIKey(secret)).Respond(context.Background(), Request{})
	if err == nil {
		t.Fatal("Respond succeeded, want an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaked the API key: %q", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("error did not redact the API key: %q", err)
	}
}

func TestRespond_SendsAuthHeader(t *testing.T) {
	t.Parallel()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, okJSON)
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL, "m", WithAPIKey("tok")).Respond(context.Background(), Request{}); err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer tok")
	}
}

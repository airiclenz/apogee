package provider

import (
	"context"
	"errors"
	"fmt"
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

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	// The HTTP-date cases are relative to now, so this table's subtests stay sequential —
	// a parallel subtest would not run until the clock had moved on.
	soon := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)

	for _, tc := range []struct {
		name    string
		header  string
		wantOK  bool
		wantMin time.Duration
		wantMax time.Duration
	}{
		{name: "delta seconds", header: "2", wantOK: true, wantMin: 2 * time.Second, wantMax: 2 * time.Second},
		{name: "zero seconds", header: "0", wantOK: true},
		{name: "negative seconds", header: "-5", wantOK: true},
		// HTTP-dates carry second granularity, so the remaining wait is somewhere below 2s.
		{name: "http date ahead", header: soon, wantOK: true, wantMin: 500 * time.Millisecond, wantMax: 2 * time.Second},
		{name: "http date in the past", header: past, wantOK: true},
		{name: "garbage", header: "soonish"},
		{name: "empty", header: ""},
		{name: "whitespace only", header: "  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tc.header)
			if ok != tc.wantOK {
				t.Fatalf("parseRetryAfter(%q) ok = %v, want %v", tc.header, ok, tc.wantOK)
			}
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("parseRetryAfter(%q) = %v, want within [%v, %v]", tc.header, got, tc.wantMin, tc.wantMax)
			}
		})
	}
}

// A Retry-After the client is willing to honour replaces the exponential backoff entirely —
// proven here by a wait that finishes far inside the 1s a header-less 429 would have cost.
func TestRespond_HonorsRetryAfterHeader(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, okJSON)
	}))
	defer srv.Close()

	start := time.Now()
	got, err := NewClient(srv.URL, "m", WithMaxRetries(2)).Respond(context.Background(), Request{})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if got.Content != "recovered" {
		t.Errorf("Content = %q, want recovered", got.Content)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("server calls = %d, want 2 (429 then success)", n)
	}
	if elapsed >= retry429BaseDelay {
		t.Errorf("elapsed = %v, want well under the %v header-less 429 base", elapsed, retry429BaseDelay)
	}
}

// A ban longer than maxRetryAfter is not waited out: the 429 becomes the answer at once, with
// the retry budget untouched.
func TestRespond_LongRetryAfterGivesUpImmediately(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"temporarily rate-limited upstream"}}`)
	}))
	defer srv.Close()

	start := time.Now()
	_, err := NewClient(srv.URL, "m", WithMaxRetries(2)).Respond(context.Background(), Request{})
	elapsed := time.Since(start)

	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v (%T), want *StatusError", err, err)
	}
	if statusErr.Code != http.StatusTooManyRequests {
		t.Errorf("Code = %d, want 429", statusErr.Code)
	}
	if !strings.Contains(statusErr.Body, "rate-limited upstream") {
		t.Errorf("Body = %q, want the server's message", statusErr.Body)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("server calls = %d, want 1 (a long ban consumes no further attempts)", n)
	}
	if elapsed > 5*time.Second {
		t.Errorf("elapsed = %v, want an immediate give-up", elapsed)
	}
}

func TestRespond_RetryAfterWaitIsCancellable(t *testing.T) {
	t.Parallel()

	var served sync.Once
	servedCh := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30") // at the cap: honoured, so the client settles in to wait
		w.WriteHeader(http.StatusTooManyRequests)
		served.Do(func() { close(servedCh) })
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-servedCh
		cancel()
	}()

	start := time.Now()
	_, err := NewClient(srv.URL, "m", WithMaxRetries(2)).Respond(ctx, Request{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("elapsed = %v, want the wait to abort promptly on cancellation", elapsed)
	}
}

// Sleep-free proof of the delay selection: a header-less 429 backs off from the slow base
// while transport faults and 5xx keep the configured one.
func TestClient_RetryDelayBaseBySpec(t *testing.T) {
	t.Parallel()

	client := NewClient("http://example.invalid", "m")
	for _, tc := range []struct {
		name    string
		status  int
		attempt int
		want    time.Duration
	}{
		{name: "429 first retry", status: http.StatusTooManyRequests, attempt: 1, want: time.Second},
		{name: "429 second retry", status: http.StatusTooManyRequests, attempt: 2, want: 2 * time.Second},
		{name: "500 first retry", status: http.StatusInternalServerError, attempt: 1, want: defaultRetryBaseDelay},
		{name: "500 second retry", status: http.StatusInternalServerError, attempt: 2, want: 2 * defaultRetryBaseDelay},
		{name: "transport fault", status: 0, attempt: 1, want: defaultRetryBaseDelay},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := client.retryDelay(tc.status, tc.attempt); got != tc.want {
				t.Errorf("retryDelay(%d, %d) = %v, want %v", tc.status, tc.attempt, got, tc.want)
			}
		})
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
	// The overflow branch stays a sentinel, not a StatusError: a caller that retries a 4xx
	// with a smaller request must not mistake "the prompt is too long" for one.
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		t.Errorf("overflow surfaced as *StatusError (%v); it must stay the ErrContextOverflow sentinel", statusErr)
	}
}

// A non-2xx that is not an overflow reaches the caller as a typed *StatusError, so an HTTP
// class can be branched on with errors.As instead of matched in the message text.
func TestRespond_StatusErrorCarriesCode(t *testing.T) {
	t.Parallel()

	for _, code := range []int{http.StatusBadRequest, http.StatusNotFound} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				_, _ = io.WriteString(w, `{"error":"unknown field 'chat_template_kwargs'"}`)
			}))
			defer srv.Close()

			_, err := NewClient(srv.URL, "m").Respond(context.Background(), Request{})
			var statusErr *StatusError
			if !errors.As(err, &statusErr) {
				t.Fatalf("error = %v (%T), want *StatusError", err, err)
			}
			if statusErr.Code != code {
				t.Errorf("Code = %d, want %d", statusErr.Code, code)
			}
			if !strings.Contains(statusErr.Body, "chat_template_kwargs") {
				t.Errorf("Body = %q, want the server's message", statusErr.Body)
			}
			if want := fmt.Sprintf("apogee: upstream HTTP %d: ", code); !strings.HasPrefix(err.Error(), want) {
				t.Errorf("Error() = %q, want the prefix %q it carried before the type existed", err.Error(), want)
			}
		})
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

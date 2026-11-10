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

	"github.com/airiclenz/apogee/internal/testtime"
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
// proven here by the GAP the server itself observes between the two requests. Timing the whole
// call would have measured the backoff plus two real round trips, dialling and JSON, so on a
// loaded box a passing client could still blow a bound set at the backoff constant; the gap
// between the two arrivals is the wait and nothing else.
func TestRespond_HonorsRetryAfterHeader(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	var mu sync.Mutex
	var seen []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, time.Now())
		mu.Unlock()
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, okJSON)
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, "m", WithMaxRetries(2)).Respond(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if got.Content != "recovered" {
		t.Errorf("Content = %q, want recovered", got.Content)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("server calls = %d, want 2 (429 then success)", n)
	}

	mu.Lock()
	arrivals := append([]time.Time(nil), seen...)
	mu.Unlock()
	if len(arrivals) != 2 {
		t.Fatalf("server saw %d requests, want 2 (429 then success)", len(arrivals))
	}
	if gap := arrivals[1].Sub(arrivals[0]); gap >= retry429BaseDelay {
		t.Errorf("the server saw %v between the two requests, want well under the %v a header-less 429 would have waited", gap, retry429BaseDelay)
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

// An empty non-2xx body names the status instead of ending in a bare colon, and a 3xx —
// which the client refuses to follow by design (see NewClient) — additionally names the
// redirect target so the user can re-point endpoint: without guessing. Both surfaces
// (Respond and Stream) must render the identical text, since one function owns it.
func TestStatusError_EmptyBodyNamesTheStatus(t *testing.T) {
	t.Parallel()

	t.Run("redirect names the Location", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "http://elsewhere.invalid/v1")
			w.WriteHeader(http.StatusPermanentRedirect)
		}))
		defer srv.Close()

		const want = "apogee: upstream HTTP 308 Permanent Redirect — redirects are not followed; point endpoint: at the URL the server redirects to (Location: http://elsewhere.invalid/v1)"
		client := NewClient(srv.URL, "m", WithMaxRetries(0))

		_, err := client.Respond(context.Background(), Request{})
		var statusErr *StatusError
		if !errors.As(err, &statusErr) {
			t.Fatalf("error = %v (%T), want *StatusError", err, err)
		}
		if statusErr.Code != http.StatusPermanentRedirect {
			t.Errorf("Code = %d, want %d", statusErr.Code, http.StatusPermanentRedirect)
		}
		if statusErr.Location != "http://elsewhere.invalid/v1" {
			t.Errorf("Location = %q, want the server's Location header", statusErr.Location)
		}
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}

		deltas := collectStream(client, Request{})
		if len(deltas) != 1 || deltas[0].Kind != DeltaError {
			t.Fatalf("deltas = %+v, want exactly one DeltaError", deltas)
		}
		if deltas[0].Err != want {
			t.Errorf("stream Err = %q, want %q (identical to Respond)", deltas[0].Err, want)
		}
	})

	t.Run("no Location names the status alone", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		_, err := NewClient(srv.URL, "m", WithMaxRetries(0)).Respond(context.Background(), Request{})
		var statusErr *StatusError
		if !errors.As(err, &statusErr) {
			t.Fatalf("error = %v (%T), want *StatusError", err, err)
		}
		if want := "apogee: upstream HTTP 503 Service Unavailable"; err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	})
}

// TestStatusError_Loading503WithRetriesOffPostsOnce pins WithMaxRetries(0) as one POST whatever
// the reply: a loading server's 503 is not waited out with retries off — the naming call relies on
// it — and surfaces at once, its body quoted, on both surfaces.
func TestStatusError_Loading503WithRetriesOffPostsOnce(t *testing.T) {
	t.Parallel()

	srv, calls := scriptedServer(t, writeLoadingReply)
	client := NewClient(srv.URL, "m", WithMaxRetries(0))

	_, err := client.Respond(context.Background(), Request{})
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Code != http.StatusServiceUnavailable || !strings.Contains(statusErr.Body, "Loading model") {
		t.Errorf("Respond error = %v, want a 503 StatusError quoting the Loading model body", err)
	}
	deltas := collectStream(client, Request{})
	if len(deltas) != 1 || deltas[0].Kind != DeltaError || !strings.Contains(deltas[0].Err, "Loading model") {
		t.Errorf("deltas = %+v, want exactly one DeltaError quoting the body", deltas)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("server calls = %d, want 2 — one POST per call", n)
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

// loadingReplyBody is what llama-server answers a request made while its model is still loading,
// with HTTP 503.
const loadingReplyBody = `{"error":{"code":503,"message":"Loading model","type":"unavailable_error"}}`

// loadedSSE is the streamed reply a server gives once its model is loaded.
const loadedSSE = "data: {\"choices\":[{\"delta\":{\"content\":\"loaded\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"

// writeLoadingReply answers w with llama-server's loading reply.
func writeLoadingReply(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, loadingReplyBody)
}

// writeLoadedSSE answers w with loadedSSE.
func writeLoadedSSE(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, loadedSSE)
}

// scriptedServer answers its n-th POST (1-based) with the n-th reply of script, and every POST
// past the script with its last reply, counting them all.
func scriptedServer(t *testing.T, script ...func(http.ResponseWriter)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := int(calls.Add(1))
		script[min(n, len(script))-1](w)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// repeated is reply n times, for a scriptedServer script.
func repeated(n int, reply func(http.ResponseWriter)) []func(http.ResponseWriter) {
	out := make([]func(http.ResponseWriter), n)
	for i := range out {
		out[i] = reply
	}
	return out
}

// loadingPacing scales the wait on a loading server for a test: a hold-off of base doubling to
// ceiling, given up once backstop has passed.
func loadingPacing(base, ceiling, backstop time.Duration) Option {
	return func(c *Client) {
		c.loadingBase, c.loadingCap, c.loadingBackstop = base, ceiling, backstop
	}
}

// TestRespond_Loading503IsWaitedOutPastTheRetryLadder pins the patient wait on a server still
// loading its model: eight loading replies — past the 0.6 s the 200ms·2ⁿ ladder gives up on — and
// then the answer, which Respond returns.
func TestRespond_Loading503IsWaitedOutPastTheRetryLadder(t *testing.T) {
	t.Parallel()

	okReply := func(w http.ResponseWriter) { _, _ = io.WriteString(w, okJSON) }
	srv, calls := scriptedServer(t, append(repeated(8, writeLoadingReply), okReply)...)
	client := NewClient(srv.URL, "m",
		loadingPacing(testtime.Fixed(20*time.Millisecond), testtime.Fixed(100*time.Millisecond), testtime.Budget(time.Minute)))

	began := time.Now()
	got, err := client.Respond(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Respond: %v — the loading server was given up on", err)
	}
	if got.Content != "recovered" {
		t.Errorf("Content = %q, want recovered", got.Content)
	}
	if n := calls.Load(); n != 9 {
		t.Errorf("server calls = %d, want 9 (eight loading replies, then the answer)", n)
	}
	if waited := time.Since(began); waited < testtime.Fixed(600*time.Millisecond) {
		t.Errorf("the wait took %v, want past the 0.6 s the retry ladder spans", waited)
	}
}

// TestStream_Loading503IsSaidOnceAndRecordedOncePerWait pins the stream's view of the wait: one
// DeltaLoadingWait per call, ahead of every attempt, even when the server flaps out of loading
// (a 500 the short ladder retries) and back into it; and one DeltaAttempt per wait, not per
// re-send, with the re-send that ended each wait opening an attempt of its own.
func TestStream_Loading503IsSaidOnceAndRecordedOncePerWait(t *testing.T) {
	t.Parallel()

	serverError := func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) }
	script := append(repeated(3, writeLoadingReply), serverError)
	script = append(script, repeated(2, writeLoadingReply)...)
	script = append(script, writeLoadedSSE)
	srv, calls := scriptedServer(t, script...)
	client := identified(srv, WithRetryBaseDelay(time.Millisecond),
		loadingPacing(testtime.Fixed(5*time.Millisecond), testtime.Fixed(10*time.Millisecond), testtime.Budget(time.Minute)))

	deltas := collectStream(client, Request{})

	if len(deltas) == 0 || deltas[0].Kind != DeltaLoadingWait {
		t.Fatalf("deltas = %+v, want the DeltaLoadingWait first", deltas)
	}
	attempts, kinds := attemptsOf(deltas)
	waits := 0
	var content string
	for _, d := range deltas {
		if d.Kind == DeltaLoadingWait {
			waits++
		}
		content += d.Content
	}
	if waits != 1 {
		t.Errorf("DeltaLoadingWait yielded %d times, want once per call", waits)
	}
	if content != "loaded" || kinds[len(kinds)-1] != DeltaDone {
		t.Errorf("content %q, kinds %v; want the loaded reply ending on Done", content, kinds)
	}
	var outcomes []string
	for i, a := range attempts {
		outcomes = append(outcomes, a.Outcome)
		if a.Index != i {
			t.Errorf("attempt %d carries Index %d", i, a.Index)
		}
	}
	want := []string{"http_503", "http_500", "http_503", AttemptOK}
	if strings.Join(outcomes, ",") != strings.Join(want, ",") {
		t.Errorf("attempt outcomes = %v, want %v — one per wait, one per other reply", outcomes, want)
	}
	if n := calls.Load(); n != 7 {
		t.Errorf("server calls = %d, want 7", n)
	}
}

// TestStream_Loading503PastTheBackstopQuotesTheBody pins the backstop: a server that never
// finishes loading is given up on once the backstop has passed, and the last loading reply is the
// terminal fault, its "Loading model" body quoted as statusDelta quotes any status's — on Respond
// too — and recorded as the one attempt the wait was.
func TestStream_Loading503PastTheBackstopQuotesTheBody(t *testing.T) {
	t.Parallel()

	srv, calls := scriptedServer(t, writeLoadingReply)
	pacing := loadingPacing(testtime.Fixed(10*time.Millisecond), testtime.Fixed(20*time.Millisecond), testtime.Fixed(150*time.Millisecond))

	deltas := collectStream(identified(srv, pacing), Request{})
	last := deltas[len(deltas)-1]
	if last.Kind != DeltaError || !strings.Contains(last.Err, "Loading model") || !strings.Contains(last.Err, "503") {
		t.Fatalf("terminal delta = %+v, want a DeltaError quoting the 503's Loading model body", last)
	}
	if last.Fault.Class != FaultStatus || last.Fault.Code != http.StatusServiceUnavailable {
		t.Errorf("Fault = %+v, want the 503 status", last.Fault)
	}
	if attempts, _ := attemptsOf(deltas); len(attempts) != 1 || attempts[0].Outcome != "http_503" {
		t.Errorf("attempts = %+v, want the one http_503 attempt the wait was", attempts)
	}
	if n := calls.Load(); n < 4 {
		t.Errorf("server calls = %d, want the wait to have re-sent until the backstop", n)
	}

	_, err := NewClient(srv.URL, "m", pacing).Respond(context.Background(), Request{})
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Code != http.StatusServiceUnavailable || !strings.Contains(statusErr.Body, "Loading model") {
		t.Errorf("Respond error = %v, want a 503 StatusError quoting the Loading model body", err)
	}
}

// TestStream_Loading503CancelledMidWaitReturnsAtOnce pins that the wait is the caller's to end: a
// cancel during a hold-off far longer than any test returns the call at once, as a fault.
func TestStream_Loading503CancelledMidWaitReturnsAtOnce(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, calls := scriptedServer(t, func(w http.ResponseWriter) {
		writeLoadingReply(w)
		cancel()
	})
	client := identified(srv, loadingPacing(time.Hour, time.Hour, 24*time.Hour))

	done := make(chan []Delta, 1)
	go func() {
		var deltas []Delta
		for d := range client.Stream(ctx, Request{}) {
			deltas = append(deltas, d)
		}
		done <- deltas
	}()
	select {
	case deltas := <-done:
		last := deltas[len(deltas)-1]
		if last.Kind != DeltaError {
			t.Errorf("terminal delta = %+v, want a DeltaError for the cancel", last)
		}
		if attempts, _ := attemptsOf(deltas); len(attempts) != 1 || attempts[0].Outcome != AttemptCancelled {
			t.Errorf("attempts = %+v, want the one wait, cancelled", attempts)
		}
	case <-time.After(testtime.Budget(10 * time.Second)):
		t.Fatal("the cancelled wait did not return")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("server calls = %d, want 1", n)
	}
}

// TestStream_Loading503ConsumerBreakStopsTheCall pins that a consumer breaking on the wait's
// Delta ends the call there: no further POST, nothing more yielded.
func TestStream_Loading503ConsumerBreakStopsTheCall(t *testing.T) {
	t.Parallel()

	srv, calls := scriptedServer(t, writeLoadingReply)
	client := identified(srv, loadingPacing(time.Millisecond, time.Millisecond, testtime.Budget(time.Minute)))

	var seen []DeltaKind
	for d := range client.Stream(context.Background(), Request{}) {
		seen = append(seen, d.Kind)
		break
	}
	if len(seen) != 1 || seen[0] != DeltaLoadingWait {
		t.Errorf("deltas before the break = %v, want the DeltaLoadingWait", seen)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("server calls = %d, want 1 — the call stops where its consumer did", n)
	}
}

// TestRespond_RetryLadderHoldsForEveryOther5xx pins that the wait fires on the loading reply
// alone: a 500 carrying the same words and a 503 with any other body keep the short ladder —
// three POSTs and the fault.
func TestRespond_RetryLadderHoldsForEveryOther5xx(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "500 saying loading", status: http.StatusInternalServerError, body: loadingReplyBody},
		{name: "503 overloaded", status: http.StatusServiceUnavailable, body: "server overloaded"},
		{name: "503 empty", status: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, calls := scriptedServer(t, func(w http.ResponseWriter) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			client := NewClient(srv.URL, "m", WithRetryBaseDelay(time.Millisecond),
				loadingPacing(time.Hour, time.Hour, 24*time.Hour))

			if _, err := client.Respond(context.Background(), Request{}); err == nil {
				t.Fatal("Respond succeeded against a failing server")
			}
			if n := calls.Load(); n != 3 {
				t.Errorf("server calls = %d, want 3 — the ladder, not the wait", n)
			}
		})
	}
}

// TestStream_Loading503YieldedToAFallbackKeepsTheLadder pins Request.SkipLoadingWait: a seat whose
// bound entry names a fallback list gets the short ladder on the loading reply, so the fault
// reaches its resolver and the `fallback-after:` grace governs.
func TestStream_Loading503YieldedToAFallbackKeepsTheLadder(t *testing.T) {
	t.Parallel()

	srv, calls := scriptedServer(t, writeLoadingReply)
	client := NewClient(srv.URL, "m", WithRetryBaseDelay(time.Millisecond),
		loadingPacing(time.Hour, time.Hour, 24*time.Hour))

	deltas := collectStream(client, Request{SkipLoadingWait: true})
	for _, d := range deltas {
		if d.Kind == DeltaLoadingWait {
			t.Errorf("a call that yields the wait announced one: %+v", deltas)
		}
	}
	if last := deltas[len(deltas)-1]; last.Kind != DeltaError || !strings.Contains(last.Err, "Loading model") {
		t.Errorf("terminal delta = %+v, want the loading reply surfaced", last)
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("server calls = %d, want 3 — the ladder, not the wait", n)
	}
}

// TestStream_Loading503ReArmsTheHeaderIdleWindow pins that a loading reply is not silence: a wait
// that runs well past the pre-header idle window is not cut by it, because each loading reply
// re-arms the window.
func TestStream_Loading503ReArmsTheHeaderIdleWindow(t *testing.T) {
	t.Parallel()

	const polls = 12
	srv, calls := scriptedServer(t, append(repeated(polls, writeLoadingReply), writeLoadedSSE)...)
	window := testtime.Fixed(300 * time.Millisecond)
	client := NewClient(srv.URL, "m", WithStreamIdleTimeout(window),
		loadingPacing(testtime.Fixed(50*time.Millisecond), testtime.Fixed(50*time.Millisecond), testtime.Budget(time.Minute)))

	began := time.Now()
	deltas := collectStream(client, Request{})
	if waited := time.Since(began); waited < window {
		t.Fatalf("the wait took %v, want past the %v idle window for the pin to bite", waited, window)
	}
	if last := deltas[len(deltas)-1]; last.Kind != DeltaDone {
		t.Errorf("terminal delta = %+v, want Done — the idle window cut the wait", last)
	}
	if n := calls.Load(); n != polls+1 {
		t.Errorf("server calls = %d, want %d", n, polls+1)
	}
}

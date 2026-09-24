package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// attemptsOf splits a delta sequence into its DeltaAttempt payloads and the kinds of the rest.
func attemptsOf(deltas []Delta) ([]Attempt, []DeltaKind) {
	var attempts []Attempt
	var kinds []DeltaKind
	for _, d := range deltas {
		if d.Kind == DeltaAttempt {
			attempts = append(attempts, *d.Attempt)
			continue
		}
		kinds = append(kinds, d.Kind)
	}
	return attempts, kinds
}

// identified builds a Client stamped with a server identity against srv.
func identified(srv *httptest.Server, opts ...Option) *Client {
	opts = append([]Option{WithServerIdentity("local", srv.URL)}, opts...)
	return NewClient(srv.URL, "m", opts...)
}

func TestAttemptRetryThenSuccessSharesRequestID(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, roundTripSSE)
	}))
	defer srv.Close()

	attempts, kinds := attemptsOf(collectStream(identified(srv), Request{}))
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, want 2: %+v", len(attempts), attempts)
	}
	first, second := attempts[0], attempts[1]
	if first.Index != 0 || second.Index != 1 {
		t.Errorf("indices = %d/%d, want 0/1", first.Index, second.Index)
	}
	if first.RequestID == "" || first.RequestID != second.RequestID {
		t.Errorf("request ids %q / %q, want one shared non-empty id", first.RequestID, second.RequestID)
	}
	if first.Outcome != "http_429" || second.Outcome != AttemptOK {
		t.Errorf("outcomes = %q/%q, want http_429/ok", first.Outcome, second.Outcome)
	}
	if first.Server != "local" || first.Endpoint != srv.URL {
		t.Errorf("identity = %q %q, want local %q", first.Server, first.Endpoint, srv.URL)
	}
	if kinds[len(kinds)-1] != DeltaDone {
		t.Errorf("kinds = %v, want the stream to end on Done", kinds)
	}
}

func TestAttemptPrecedesTerminalDelta(t *testing.T) {
	t.Parallel()
	srv := sseServer(roundTripSSE)
	defer srv.Close()

	deltas := collectStream(identified(srv), Request{})
	n := len(deltas)
	if n < 2 || deltas[n-2].Kind != DeltaAttempt || deltas[n-1].Kind != DeltaDone {
		t.Fatalf("tail = %v, want …attempt, done", deltaKinds(deltas))
	}
}

func TestAttemptKeepalivesAdvanceTTFBNotTTFT(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for range 4 {
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
			time.Sleep(500 * time.Millisecond)
		}
		_, _ = io.WriteString(w, roundTripSSE)
	}))
	defer srv.Close()

	attempts, _ := attemptsOf(collectStream(identified(srv), Request{}))
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(attempts))
	}
	a := attempts[0]
	if a.TTFB == 0 || a.TTFT < a.TTFB+time.Second {
		t.Errorf("ttfb %v, ttft %v: want ttfb well before ttft", a.TTFB, a.TTFT)
	}
}

func TestAttemptLastFollowsTTFTAndCarriesUsage(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"model\":\"served\",\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		flusher.Flush()
		time.Sleep(50 * time.Millisecond)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"b\"},\"finish_reason\":\"stop\"}],"+
			"\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":7,\"total_tokens\":10}}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	attempts, _ := attemptsOf(collectStream(identified(srv), Request{}))
	a := attempts[0]
	if a.TTFT == 0 || a.Last <= a.TTFT || a.Duration < a.Last {
		t.Errorf("ttft %v, last %v, duration %v: want ttft < last ≤ duration", a.TTFT, a.Last, a.Duration)
	}
	if a.OutputTokens != 7 {
		t.Errorf("output tokens = %d, want 7", a.OutputTokens)
	}
	if a.Model != "served" {
		t.Errorf("model = %q, want the served id", a.Model)
	}
}

func TestAttemptWithoutUsageReportsZeroTokens(t *testing.T) {
	t.Parallel()
	srv := sseServer("data: {\"choices\":[{\"delta\":{\"content\":\"a\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	defer srv.Close()

	attempts, _ := attemptsOf(collectStream(identified(srv), Request{}))
	if attempts[0].OutputTokens != 0 {
		t.Errorf("output tokens = %d, want 0 with no usage block", attempts[0].OutputTokens)
	}
	if attempts[0].Model != "m" {
		t.Errorf("model = %q, want the requested id when the server names none", attempts[0].Model)
	}
}

func TestAttemptCancelledMidStream(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	streamed := streamAsync(ctx, identified(srv))
	first := <-streamed
	if first.Kind != DeltaContent {
		t.Fatalf("first delta = %v, want content", first.Kind)
	}
	cancel()
	attempts, _ := attemptsOf(drainWithin(t, streamed, 5*time.Second))
	if len(attempts) != 1 || attempts[0].Outcome != AttemptCancelled {
		t.Fatalf("attempts = %+v, want one cancelled", attempts)
	}
}

func TestAttemptTTFBWithoutIdleCut(t *testing.T) {
	t.Parallel()
	srv := sseServer(roundTripSSE)
	defer srv.Close()

	attempts, _ := attemptsOf(collectStream(identified(srv, WithStreamIdleTimeout(0)), Request{}))
	if len(attempts) != 1 || attempts[0].TTFB == 0 {
		t.Fatalf("attempts = %+v, want one with ttfb recorded", attempts)
	}
}

func TestAttemptUnstampedClientYieldsNone(t *testing.T) {
	t.Parallel()
	srv := sseServer(roundTripSSE)
	defer srv.Close()

	for _, d := range collectStream(NewClient(srv.URL, "m"), Request{}) {
		if d.Kind == DeltaAttempt {
			t.Fatal("an unstamped Client yielded a DeltaAttempt")
		}
	}
}

func TestRedactEndpoint(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://u:p@h/api?key=x":   "https://h/api",
		"http://localhost:8080/v1":  "http://localhost:8080/v1",
		"https://h/api#frag":        "https://h/api",
		"https://h:443/path?a=b&c=": "https://h:443/path",
	}
	for in, want := range cases {
		if got := redactEndpoint(in); got != want {
			t.Errorf("redactEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
	c := NewClient("http://h", "m", WithServerIdentity("n", "https://u:p@h/api?key=x"))
	if name, endpoint, ok := c.ServerIdentity(); !ok || name != "n" || endpoint != "https://h/api" {
		t.Errorf("ServerIdentity() = %q %q %v, want n https://h/api true", name, endpoint, ok)
	}
	if _, _, ok := NewClient("http://h", "m").ServerIdentity(); ok {
		t.Error("an unstamped Client reported an identity")
	}
}

// TestAttemptOutcomes pins the closed outcome vocabulary: every member, reached through the
// path that produces it.
func TestAttemptOutcomes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		handler http.HandlerFunc
		opts    []Option
		ctx     func() context.Context
		want    string
	}{
		{
			name:    "ok",
			handler: sseHandler(roundTripSSE),
			want:    AttemptOK,
		},
		{
			name: "http status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "nope", http.StatusForbidden)
			},
			want: "http_403",
		},
		{
			name: "overflow",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "maximum context length exceeded", http.StatusBadRequest)
			},
			want: AttemptOverflow,
		},
		{
			name:    "in band",
			handler: sseHandler("data: {\"error\":{\"code\":502,\"message\":\"upstream gone\"}}\n\n"),
			want:    AttemptInBand,
		},
		{
			name: "transport",
			handler: func(w http.ResponseWriter, r *http.Request) {
				hj, _ := w.(http.Hijacker)
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
			},
			opts: []Option{WithMaxRetries(0)},
			want: AttemptTransport,
		},
		{
			name: "idle",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			},
			opts: []Option{WithStreamIdleTimeout(100 * time.Millisecond)},
			want: AttemptIdle,
		},
		{
			name:    "stream fault",
			handler: sseHandler("data: {not json\n\n"),
			want:    AttemptStreamFault,
		},
		{
			name:    "cancelled",
			handler: sseHandler(roundTripSSE),
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			want: AttemptCancelled,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			ctx := context.Background()
			if tc.ctx != nil {
				ctx = tc.ctx()
			}
			var deltas []Delta
			for d := range identified(srv, tc.opts...).Stream(ctx, Request{}) {
				deltas = append(deltas, d)
			}
			attempts, _ := attemptsOf(deltas)
			if len(attempts) == 0 {
				t.Fatalf("no attempt in %v", deltaKinds(deltas))
			}
			if got := attempts[len(attempts)-1].Outcome; got != tc.want {
				t.Errorf("outcome = %q, want %q", got, tc.want)
			}
		})
	}
}

// sseHandler writes body verbatim as an event-stream.
func sseHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}
}

// deltaKinds lists a sequence's kinds, for failure messages.
func deltaKinds(deltas []Delta) []DeltaKind {
	kinds := make([]DeltaKind, 0, len(deltas))
	for _, d := range deltas {
		kinds = append(kinds, d.Kind)
	}
	return kinds
}

// A reply of tool calls alone is clocked by its fragments as they arrive, not by the assembled
// DeltaToolCall the codec flushes at the stream's end — otherwise TTFT ≈ Last and the attempt's
// generation rate collapses. The server pauses between the first fragment and the second, and
// again before the terminator: TTFT must sit a pause before Last, and Last a pause before the end.
func TestAttemptToolCallOnlyReplyClockedByFragments(t *testing.T) {
	t.Parallel()
	const pause = 200 * time.Millisecond
	cases := []struct {
		name  string
		wire  Wire
		parts [3]string // first fragment, second fragment, terminator
	}{
		{
			name: "openai",
			wire: WireOpenAI,
			parts: [3]string{
				"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\"," +
					"\"function\":{\"name\":\"grep\",\"arguments\":\"{\\\"q\\\":\"}}]}}]}\n\n",
				"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0," +
					"\"function\":{\"arguments\":\"\\\"x\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n",
				"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":9,\"total_tokens\":12}}\n\n" +
					"data: [DONE]\n\n",
			},
		},
		{
			name: "anthropic",
			wire: WireAnthropic,
			parts: [3]string{
				"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-x\"," +
					"\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n" +
					"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0," +
					"\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"grep\",\"input\":{}}}\n\n",
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0," +
					"\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"q\\\":\\\"x\\\"}\"}}\n\n",
				"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
					"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}," +
					"\"usage\":{\"output_tokens\":9}}\n\n" +
					"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				flusher, _ := w.(http.Flusher)
				for i, part := range tc.parts {
					if i > 0 {
						time.Sleep(pause)
					}
					_, _ = io.WriteString(w, part)
					flusher.Flush()
				}
			}))
			defer srv.Close()

			attempts, kinds := attemptsOf(collectStream(identified(srv, WithWire(tc.wire)), Request{}))
			if len(attempts) != 1 || attempts[0].Outcome != AttemptOK {
				t.Fatalf("attempts = %+v, want one ok", attempts)
			}
			if want := []DeltaKind{DeltaToolCall, DeltaDone}; !slices.Equal(kinds, want) {
				t.Fatalf("kinds = %v, want %v", kinds, want)
			}
			a := attempts[0]
			if a.TTFT == 0 || a.Last-a.TTFT < pause*3/4 {
				t.Errorf("ttft %v, last %v: want ttft at the first fragment, a pause before last", a.TTFT, a.Last)
			}
			if a.Duration-a.Last < pause*3/4 {
				t.Errorf("last %v, duration %v: want last at the final fragment, not the flush", a.Last, a.Duration)
			}
			if a.OutputTokens != 9 {
				t.Errorf("output tokens = %d, want 9", a.OutputTokens)
			}
		})
	}
}

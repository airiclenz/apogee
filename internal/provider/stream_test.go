package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// sseServer returns a server that writes body verbatim as an event-stream, flushing so a
// consumer sees chunks as they arrive.
func sseServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
}

// collectStream drains a Stream into a slice for assertion.
func collectStream(client *Client, req Request) []Delta {
	var deltas []Delta
	for d := range client.Stream(context.Background(), req) {
		deltas = append(deltas, d)
	}
	return deltas
}

const roundTripSSE = `data: {"choices":[{"delta":{"content":"Hel"}}]}

data: {"choices":[{"delta":{"content":"lo"}}]}

data: {"choices":[{"delta":{"reasoning_content":"hmm"}}]}

data: {"choices":[{"delta":{"tool_calls":[{"id":"tc_1","function":{"name":"grep","arguments":"{\"q\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"\"x\"}"}}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: {"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}

data: [DONE]

`

func TestStream_RoundTrip(t *testing.T) {
	t.Parallel()

	srv := sseServer(roundTripSSE)
	defer srv.Close()

	deltas := collectStream(NewClient(srv.URL, "m"), Request{Messages: []Message{{Role: "user", Content: "hi"}}})

	var content, thinking string
	var toolCalls []ToolCall
	var done *Delta
	for i := range deltas {
		switch deltas[i].Kind {
		case DeltaContent:
			content += deltas[i].Content
		case DeltaThinking:
			thinking += deltas[i].Thinking
		case DeltaToolCall:
			toolCalls = append(toolCalls, *deltas[i].ToolCall)
		case DeltaDone:
			done = &deltas[i]
		case DeltaError, DeltaContextOverflow:
			t.Fatalf("unexpected terminal delta: %+v", deltas[i])
		}
	}

	if content != "Hello" {
		t.Errorf("assembled content = %q, want Hello", content)
	}
	if thinking != "hmm" {
		t.Errorf("thinking = %q, want hmm", thinking)
	}
	if len(toolCalls) != 1 || toolCalls[0].Function.Name != "grep" || toolCalls[0].Function.Arguments != `{"q":"x"}` {
		t.Errorf("tool calls = %+v, want one grep call with assembled args", toolCalls)
	}
	if done == nil {
		t.Fatal("no terminal Done delta")
	}
	if done.FinishReason != "tool_calls" {
		t.Errorf("finish reason = %q, want tool_calls", done.FinishReason)
	}
	// No prompt_tokens_details member on this stream — the shape most Upstreams send — so the
	// cached share reads 0 rather than the parse failing or inventing a hit.
	if done.Usage == nil || *done.Usage != (Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7}) {
		t.Errorf("usage = %+v, want {3 4 7} with no cached share", done.Usage)
	}
}

// TestStream_UsageCarriesCachedPromptTokens pins the OpenAI-shaped prompt-token breakdown: when
// the terminal usage chunk reports how much of the prompt came from the server's prefix cache,
// that number reaches the seam beside the counters it qualifies. It is a subset of PromptTokens,
// never a replacement for it, which is why the prompt count is asserted unchanged alongside.
func TestStream_UsageCarriesCachedPromptTokens(t *testing.T) {
	t.Parallel()

	const body = `data: {"choices":[{"delta":{"content":"hi"},"finish_reason":"stop"}]}

data: {"usage":{"prompt_tokens":5000,"completion_tokens":40,"total_tokens":5040,"prompt_tokens_details":{"cached_tokens":1200}}}

data: [DONE]

`

	srv := sseServer(body)
	defer srv.Close()

	deltas := collectStream(NewClient(srv.URL, "m"), Request{Messages: []Message{{Role: "user", Content: "hi"}}})

	var done *Delta
	for i := range deltas {
		if deltas[i].Kind == DeltaDone {
			done = &deltas[i]
		}
	}
	if done == nil {
		t.Fatal("no terminal Done delta")
	}
	want := Usage{PromptTokens: 5000, CompletionTokens: 40, TotalTokens: 5040, CachedPromptTokens: 1200}
	if done.Usage == nil || *done.Usage != want {
		t.Errorf("usage = %+v, want %+v", done.Usage, want)
	}
}

func TestStream_DropsMalformedEvent(t *testing.T) {
	t.Parallel()

	const body = `data: {"choices":[{"delta":{"content":"a"}}]}

data: {not valid json

data: {"choices":[{"delta":{"content":"b"}}]}

data: [DONE]

`
	srv := sseServer(body)
	defer srv.Close()

	deltas := collectStream(NewClient(srv.URL, "m"), Request{})

	var content string
	for _, d := range deltas {
		if d.Kind == DeltaContent {
			content += d.Content
		}
		if d.Kind == DeltaError {
			t.Fatalf("malformed event surfaced as an error: %+v", d)
		}
	}
	if content != "ab" {
		t.Errorf("content = %q, want ab (malformed event dropped)", content)
	}
}

func TestStream_TerminatesWithoutDone(t *testing.T) {
	t.Parallel()

	// Server closes the stream after one content delta, never sending [DONE].
	const body = `data: {"choices":[{"delta":{"content":"x"}}]}

`
	srv := sseServer(body)
	defer srv.Close()

	deltas := collectStream(NewClient(srv.URL, "m"), Request{})
	last := deltas[len(deltas)-1]
	if last.Kind != DeltaDone || last.FinishReason != "stop" {
		t.Errorf("last delta = %+v, want a synthesised Done(stop)", last)
	}
}

func TestStream_ContextOverflow(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "context length exceeded")
	}))
	defer srv.Close()

	deltas := collectStream(NewClient(srv.URL, "m"), Request{})
	if len(deltas) != 1 || deltas[0].Kind != DeltaContextOverflow {
		t.Fatalf("deltas = %+v, want a single context_overflow", deltas)
	}
}

func TestStream_ErrorStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	defer srv.Close()

	deltas := collectStream(NewClient(srv.URL, "m", WithMaxRetries(0)), Request{})
	if len(deltas) != 1 || deltas[0].Kind != DeltaError {
		t.Fatalf("deltas = %+v, want a single error", deltas)
	}
	if !strings.Contains(deltas[0].Err, "500") {
		t.Errorf("error = %q, want it to mention HTTP 500", deltas[0].Err)
	}
}

// TestStream_ThinkingEffortHint is the streaming counterpart of TestRespond_ThinkingEffortHint,
// and the one that matters in practice: the loop streams, so a server that rejects an effort
// value fails a turn here. An effort on the wire in ANY dialect ⇒ the hint rides the terminal
// error delta, whether the failure arrives as a 4xx status or in-band under a 200; the `off`
// dialect and an effortless request ⇒ today's text unchanged.
func TestStream_ThinkingEffortHint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dialect  EffortDialect
		effort   Effort
		wantHint bool
	}{
		{name: "kwargs dialect", dialect: EffortDialectKwargs, effort: EffortMedium, wantHint: true},
		{name: "reasoning dialect", dialect: EffortDialectReasoning, effort: EffortMedium, wantHint: true},
		{name: "reasoning dialect switching thinking off", dialect: EffortDialectReasoning, effort: EffortOff, wantHint: true},
		{name: "openai dialect", dialect: EffortDialectOpenAI, effort: EffortMedium, wantHint: true},
		{name: "the zero dialect still speaks kwargs", effort: EffortMedium, wantHint: true},
		{name: "the off dialect emits nothing, so it explains nothing", dialect: EffortDialectOff, effort: EffortMedium},
		{name: "no effort, nothing on the wire, no hint", dialect: EffortDialectOpenAI},
	}

	framings := []struct {
		name   string
		server func() *httptest.Server
	}{
		{
			name: "4xx status",
			server: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, "jinja2.exceptions.TemplateError")
				}))
			},
		},
		{
			name: "in-band error under a 200",
			server: func() *httptest.Server {
				return sseServer("data: {\"error\":{\"message\":\"jinja2.exceptions.TemplateError\",\"code\":400}}\n\n")
			},
		},
	}

	for _, tc := range tests {
		for _, framing := range framings {
			t.Run(tc.name+"/"+framing.name, func(t *testing.T) {
				t.Parallel()

				srv := framing.server()
				defer srv.Close()

				deltas := collectStream(NewClient(srv.URL, "m", WithMaxRetries(0)), Request{
					ThinkingEffort: tc.effort,
					EffortDialect:  tc.dialect,
				})
				if len(deltas) != 1 || deltas[0].Kind != DeltaError {
					t.Fatalf("deltas = %+v, want a single error", deltas)
				}
				if got := strings.Contains(deltas[0].Err, thinkingEffortHint); got != tc.wantHint {
					t.Errorf("error %q carries the hint = %t, want %t", deltas[0].Err, got, tc.wantHint)
				}
			})
		}
	}
}

// TestStream_ErrorBodyIsCapped covers the hostile-upstream case: an error body far larger
// than maxErrorBodyBytes must not be buffered whole. The proof is positional — an overflow
// marker sitting past the cap never reaches the sniff (so the delta stays a plain error),
// while the same marker inside the cap still classifies as it always did.
func TestStream_ErrorBodyIsCapped(t *testing.T) {
	t.Parallel()

	const marker = "context length exceeded"
	filler := strings.Repeat("A", maxErrorBodyBytes)

	tests := []struct {
		name string
		body string
		want DeltaKind
	}{
		{name: "marker past the cap is never read", body: filler + marker, want: DeltaError},
		{name: "marker within the cap still fires", body: marker + filler, want: DeltaContextOverflow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			deltas := collectStream(NewClient(srv.URL, "m"), Request{})
			if len(deltas) != 1 {
				t.Fatalf("deltas = %+v, want a single terminal delta", deltas)
			}
			if deltas[0].Kind != tt.want {
				t.Errorf("kind = %q, want %q", deltas[0].Kind, tt.want)
			}
			if len(deltas[0].Err) > maxErrorLength+100 {
				t.Errorf("error text is %d bytes, want the sanitised bound", len(deltas[0].Err))
			}
		})
	}
}

// TestStream_InBandError covers the aggregator failure mode where an HTTP 200 stream
// carries the provider's error as a data event: it must end in a terminal fault, never in
// the Done that would commit a silent empty reply. The hint cases pin that this framing
// explains a template failure exactly as the non-2xx one does — kwargs on the wire ⇒ the
// hint rides the terminal delta, no kwargs (and any overflow) ⇒ today's text unchanged.
func TestStream_InBandError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		apiKey        string
		effort        Effort
		body          string
		wantKind      DeltaKind
		wantContent   string
		wantContains  []string
		wantAbsent    string
		wantRetryable bool
		wantHint      bool
	}{
		{
			name: "error only stream",
			body: `data: {"error":{"message":"Provider returned error","code":429,"metadata":{"raw":"temporarily rate-limited upstream"}}}

`,
			wantKind:      DeltaError,
			wantContains:  []string{"429", "Provider returned error", "rate-limited upstream"},
			wantRetryable: true,
		},
		{
			name: "error after a content delta",
			body: `data: {"choices":[{"delta":{"content":"partial"}}]}

data: {"error":{"message":"upstream died","code":502}}

data: [DONE]

`,
			wantKind:      DeltaError,
			wantContent:   "partial",
			wantContains:  []string{"502", "upstream died"},
			wantRetryable: true,
		},
		{
			name: "context overflow",
			body: `data: {"error":{"message":"This model's maximum context length is 8192 tokens","code":400}}

`,
			wantKind:     DeltaContextOverflow,
			wantContains: []string{"maximum context length"},
		},
		{
			name: "non-numeric code still surfaces",
			body: `data: {"error":{"message":"rate limited","code":"rate_limit_exceeded"}}

`,
			wantKind:     DeltaError,
			wantContains: []string{"rate limited"},
		},
		{
			name: "in-band 400 is terminal",
			body: `data: {"error":{"message":"invalid request payload","code":400}}

`,
			wantKind:     DeltaError,
			wantContains: []string{"400", "invalid request payload"},
		},
		{
			// The observed OpenRouter shape (session 20260813T100440Z-104eaf7a): the class
			// slug is the retry signal when the code alone would read as terminal.
			name: "provider unavailable retries on its error_type alone",
			body: `data: {"error":{"message":"Upstream error from provider","code":404,"error_type":"provider_unavailable","metadata":{"raw":"no instances available"}}}

`,
			wantKind:      DeltaError,
			wantContains:  []string{"Upstream error from provider", "no instances available"},
			wantRetryable: true,
		},
		{
			name:   "api key redacted",
			apiKey: "sk-secret-123",
			body: `data: {"error":{"message":"bad key sk-secret-123","code":401}}

`,
			wantKind:     DeltaError,
			wantContains: []string{"[REDACTED]"},
			wantAbsent:   "sk-secret-123",
		},
		{
			name:   "template failure wrapped in a 200 gets the hint",
			effort: EffortHigh,
			body: `data: {"error":{"message":"jinja2.exceptions.TemplateError","code":500}}

`,
			wantKind:      DeltaError,
			wantContains:  []string{"TemplateError"},
			wantRetryable: true,
			wantHint:      true,
		},
		{
			name:   "overflow stays unhinted even with kwargs on the wire",
			effort: EffortHigh,
			body: `data: {"error":{"message":"This model's maximum context length is 8192 tokens","code":400}}

`,
			wantKind:     DeltaContextOverflow,
			wantContains: []string{"maximum context length"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := sseServer(tc.body)
			defer srv.Close()

			var opts []Option
			if tc.apiKey != "" {
				opts = append(opts, WithAPIKey(tc.apiKey))
			}
			deltas := collectStream(NewClient(srv.URL, "m", opts...), Request{ThinkingEffort: tc.effort})
			if len(deltas) == 0 {
				t.Fatal("no deltas, want a terminal fault")
			}

			var content string
			for _, d := range deltas {
				switch d.Kind {
				case DeltaContent:
					content += d.Content
				case DeltaDone:
					t.Errorf("got a Done delta after an in-band error: %+v", deltas)
				case DeltaToolCall:
					t.Errorf("got a partial tool call after an in-band error: %+v", deltas)
				}
			}
			if content != tc.wantContent {
				t.Errorf("content = %q, want %q", content, tc.wantContent)
			}

			last := deltas[len(deltas)-1]
			if last.Kind != tc.wantKind {
				t.Fatalf("terminal delta = %+v, want kind %s", last, tc.wantKind)
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(last.Err, want) {
					t.Errorf("error = %q, want it to contain %q", last.Err, want)
				}
			}
			if tc.wantAbsent != "" && strings.Contains(last.Err, tc.wantAbsent) {
				t.Errorf("error = %q leaks %q", last.Err, tc.wantAbsent)
			}
			if last.Retryable != tc.wantRetryable {
				t.Errorf("retryable = %v, want %v — the loop re-streams exactly the classes the client retries at the HTTP layer", last.Retryable, tc.wantRetryable)
			}
			if got := strings.Contains(last.Err, thinkingEffortHint); got != tc.wantHint {
				t.Errorf("error %q carries the template hint = %t, want %t", last.Err, got, tc.wantHint)
			}
		})
	}
}

func TestStream_RequestShapeIncludesStreamOptions(t *testing.T) {
	t.Parallel()

	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	for range NewClient(srv.URL, "m").Stream(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}}) {
	}

	var body map[string]any
	if err := json.Unmarshal(captured, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["stream"] != true {
		t.Errorf("stream = %v, want true", body["stream"])
	}
	opts, ok := body["stream_options"].(map[string]any)
	if !ok || opts["include_usage"] != true {
		t.Errorf("stream_options = %v, want {include_usage:true}", body["stream_options"])
	}
}

func TestStream_EarlyBreakIsClean(t *testing.T) {
	t.Parallel()

	srv := sseServer(roundTripSSE)
	defer srv.Close()

	// Break after the first delta — the iterator must release the body without hanging.
	for range NewClient(srv.URL, "m").Stream(context.Background(), Request{}) {
		break
	}
}

// chunkedTextServer returns a server that writes 64 KiB SSE text deltas into the named delta
// field ("content" or "reasoning_content") and keeps writing until the client disconnects, so
// nothing but the client's own byte cap can end the stream. The chunk count is bounded well
// past maxReplyTextBytes so a missed disconnect fails the assertion instead of hanging.
func chunkedTextServer(field string) *httptest.Server {
	const chunkBytes = 64 << 10
	maxChunks := maxReplyTextBytes/chunkBytes + 32
	payload := `data: {"choices":[{"delta":{"` + field + `":"` + strings.Repeat("a", chunkBytes) + `"}}]}` + "\n\n"

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for range maxChunks {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			if _, err := io.WriteString(w, payload); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
}

// assertReplyTextCapFires drains a stream from srv and asserts the reply-text cap is what
// ended it: exactly one terminal DeltaError naming the limit, not retryable, no Done at all,
// and no more text handed to the consumer than the cap allows.
func assertReplyTextCapFires(t *testing.T, srv *httptest.Server) {
	t.Helper()

	deltas := collectStream(NewClient(srv.URL, "m"), Request{})

	var errorCount, doneCount, delivered int
	var terminal Delta
	for _, delta := range deltas {
		switch delta.Kind {
		case DeltaError:
			errorCount++
			terminal = delta
		case DeltaDone:
			doneCount++
		}
		delivered += len(delta.Content) + len(delta.Thinking)
	}

	if errorCount != 1 {
		t.Fatalf("terminal DeltaError count = %d, want exactly 1 (deltas = %d)", errorCount, len(deltas))
	}
	if want := fmt.Sprintf("%d MiB", maxReplyTextBytes>>20); !strings.Contains(terminal.Err, want) {
		t.Errorf("error %q does not name the %s limit", terminal.Err, want)
	}
	if terminal.Retryable {
		t.Error("the cap error is Retryable; a re-stream would overflow again")
	}
	if doneCount != 0 {
		t.Errorf("DeltaDone count = %d, want 0 — the reply was faulted, not finished", doneCount)
	}
	if delivered > maxReplyTextBytes {
		t.Errorf("delivered %d text bytes, want at most maxReplyTextBytes (%d)", delivered, maxReplyTextBytes)
	}
}

// TestStream_ReplyTextIsCapped proves an endless content stream ends at the byte cap rather
// than exhausting the agent.
func TestStream_ReplyTextIsCapped(t *testing.T) {
	t.Parallel()

	srv := chunkedTextServer("content")
	defer srv.Close()

	assertReplyTextCapFires(t, srv)
}

// TestStream_ThinkingCountsTowardTheCap proves the reasoning channel is summed into the same
// budget — a model that never leaves its thinking channel cannot stream without bound either.
func TestStream_ThinkingCountsTowardTheCap(t *testing.T) {
	t.Parallel()

	srv := chunkedTextServer("reasoning_content")
	defer srv.Close()

	assertReplyTextCapFires(t, srv)
}

// TestStream_CtxCancelEndsTheBody pins the contract Stream documents: the caller's ctx is the
// stream's only deadline. There is no idle timeout, so a server that goes silent mid-reply
// hangs until the caller cancels — and that cancel must end the body read, surface as a
// terminal DeltaError, and close the connection the handler is sitting on.
func TestStream_CtxCancelEndsTheBody(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	observed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"hi"}}]}`+"\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-r.Context().Done():
			close(observed)
		case <-release:
		}
	}))
	// LIFO: release the handler before Close waits on it, so a failed assertion cannot deadlock.
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamed := make(chan Delta, 16)
	go func() {
		defer close(streamed)
		for delta := range NewClient(srv.URL, "m").Stream(ctx, Request{}) {
			streamed <- delta
		}
	}()

	var got []Delta
	select {
	case delta := <-streamed:
		if delta.Kind != DeltaContent {
			t.Fatalf("first delta = %+v, want DeltaContent", delta)
		}
		got = append(got, delta)
	case <-time.After(2 * time.Second):
		t.Fatal("no content delta within 2s of the handler's flush")
	}

	cancel()

	deadline := time.After(2 * time.Second)
	for drained := false; !drained; {
		select {
		case delta, open := <-streamed:
			if !open {
				drained = true
				break
			}
			got = append(got, delta)
		case <-deadline:
			t.Fatal("the stream did not end within 2s of the ctx cancel")
		}
	}

	terminal := got[len(got)-1]
	if terminal.Kind != DeltaError {
		t.Errorf("terminal delta = %+v, want DeltaError from the cancelled body read", terminal)
	}

	select {
	case <-observed:
	case <-time.After(2 * time.Second):
		t.Fatal("the handler never saw its request context end — the body was not closed")
	}
}

// roundTripWirePayload is every data: payload of roundTripSSE, in arrival order and
// newline-joined — what one WireResponse record must hold for that stream.
const roundTripWirePayload = `{"choices":[{"delta":{"content":"Hel"}}]}
{"choices":[{"delta":{"content":"lo"}}]}
{"choices":[{"delta":{"reasoning_content":"hmm"}}]}
{"choices":[{"delta":{"tool_calls":[{"id":"tc_1","function":{"name":"grep","arguments":"{\"q\":"}}]}}]}
{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"\"x\"}"}}]}}]}
{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}
{"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}
[DONE]`

func TestWireObserver_StreamRecordsEveryDataPayload(t *testing.T) {
	t.Parallel()

	srv := sseServer(roundTripSSE)
	defer srv.Close()

	var records []WireRecord
	client := NewClient(srv.URL, "m", collectWire(&records))
	collectStream(client, Request{Messages: []Message{{Role: "user", Content: "hi"}}})

	if len(wireOf(t, records, WireRequest)) != 1 {
		t.Errorf("request records = %d, want exactly 1", len(wireOf(t, records, WireRequest)))
	}
	responses := wireOf(t, records, WireResponse)
	if len(responses) != 1 {
		t.Fatalf("response records = %d, want exactly 1 at stream end", len(responses))
	}
	if responses[0] != roundTripWirePayload {
		t.Errorf("response record =\n%s\nwant\n%s", responses[0], roundTripWirePayload)
	}
	// The record must arrive after the stream is drained, never interleaved per chunk.
	if records[len(records)-1].Direction != WireResponse {
		t.Errorf("last record = %q, want the response delivered once at stream end", records[len(records)-1].Direction)
	}
}

func TestWireObserver_AbsentObserverLeavesStreamUnchanged(t *testing.T) {
	t.Parallel()

	srv := sseServer(roundTripSSE)
	defer srv.Close()

	var records []WireRecord
	observed := collectStream(NewClient(srv.URL, "m", collectWire(&records)), Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	plain := NewClient(srv.URL, "m")
	unobserved := collectStream(plain, Request{Messages: []Message{{Role: "user", Content: "hi"}}})

	if plain.wireObserver != nil {
		t.Fatal("a Client built without WithWireObserver must hold no observer")
	}
	if !reflect.DeepEqual(observed, unobserved) {
		t.Errorf("deltas differ with an observer installed:\n%+v\nvs\n%+v", observed, unobserved)
	}
}

func TestWireObserver_StreamRecordsPayloadOnEarlyBreak(t *testing.T) {
	t.Parallel()

	srv := sseServer(roundTripSSE)
	defer srv.Close()

	var records []WireRecord
	client := NewClient(srv.URL, "m", collectWire(&records))
	for range client.Stream(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}}) {
		break // consumer walks away after the first delta
	}

	responses := wireOf(t, records, WireResponse)
	if len(responses) != 1 {
		t.Fatalf("response records = %d, want exactly 1 even on an early break", len(responses))
	}
	if !strings.HasPrefix(responses[0], `{"choices":[{"delta":{"content":"Hel"}}]}`) {
		t.Errorf("response record = %q, want the payloads read before the break", responses[0])
	}
}

func TestWireObserver_StreamRecordsSanitisedErrorBody(t *testing.T) {
	t.Parallel()

	const apiKey = "sk-super-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom "+apiKey)
	}))
	defer srv.Close()

	var records []WireRecord
	client := NewClient(srv.URL, "m", WithAPIKey(apiKey), WithMaxRetries(0), collectWire(&records))
	collectStream(client, Request{Messages: []Message{{Role: "user", Content: "hi"}}})

	responses := wireOf(t, records, WireResponse)
	if len(responses) != 1 {
		t.Fatalf("response records = %d, want exactly 1", len(responses))
	}
	if strings.Contains(responses[0], apiKey) || !strings.Contains(responses[0], "[REDACTED]") {
		t.Errorf("error record = %q, want the sanitised body", responses[0])
	}
}

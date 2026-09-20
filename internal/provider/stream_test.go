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
	"sync"
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

// TestStream_DoneCarriesTheServedModel pins the served-model read: the id the server repeats on
// every chunk of a reply reaches the terminal Done, taken off the first chunk that names one, and
// a stream whose chunks name none leaves it empty rather than filling in the requested model.
func TestStream_DoneCarriesTheServedModel(t *testing.T) {
	t.Parallel()

	t.Run("the first chunk's id rides the Done", func(t *testing.T) {
		t.Parallel()
		srv := sseServer(`data: {"model":"x","choices":[{"delta":{"content":"Hel"}}]}

data: {"model":"x","choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}]}

data: [DONE]

`)
		defer srv.Close()

		deltas := collectStream(NewClient(srv.URL, "m"), Request{Messages: []Message{{Role: "user", Content: "hi"}}})

		done := deltas[len(deltas)-1]
		if done.Kind != DeltaDone {
			t.Fatalf("last delta = %+v, want the terminal Done", done)
		}
		if done.Model != "x" {
			t.Errorf("Done.Model = %q, want x — the id the server put on the reply, not the requested m", done.Model)
		}
	})

	t.Run("a stream that names no model leaves it empty", func(t *testing.T) {
		t.Parallel()
		srv := sseServer(roundTripSSE)
		defer srv.Close()

		deltas := collectStream(NewClient(srv.URL, "m"), Request{Messages: []Message{{Role: "user", Content: "hi"}}})

		done := deltas[len(deltas)-1]
		if done.Kind != DeltaDone || done.Model != "" {
			t.Errorf("Done = %+v, want Model empty — an absent id is unknown, never the bound model", done)
		}
	})
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

// TestStream_DropsMalformedEvent pins that a chunk which fails to decode is skipped rather than
// failing a stream that is otherwise fine — and that the skip is on the record: the terminal
// Done carries how many were skipped, so a stream capture can be bisected to the bad chunk.
func TestStream_DropsMalformedEvent(t *testing.T) {
	t.Parallel()

	const body = `data: {"choices":[{"delta":{"content":"a"}}]}

data: {not valid json

data: {"choices":[{"delta":{"content":"b"}}]}

data: ["also not a chunk"

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
		t.Errorf("content = %q, want ab (malformed events skipped)", content)
	}
	last := deltas[len(deltas)-1]
	if last.Kind != DeltaDone {
		t.Fatalf("last delta = %+v, want the terminal Done", last)
	}
	if last.MalformedChunks != 2 {
		t.Errorf("Done.MalformedChunks = %d, want 2 — every skipped chunk is counted", last.MalformedChunks)
	}
}

// TestStream_OnlyMalformedChunksIsAFault pins the other half of the count: a stream whose every
// chunk failed to decode has yielded nothing the consumer could commit, and it ends as a fault
// naming the count rather than as an empty Done the loop would report as a bare empty reply.
// Both ends of a stream are held to it — the explicit [DONE] and the server closing.
func TestStream_OnlyMalformedChunksIsAFault(t *testing.T) {
	t.Parallel()

	const chunks = `data: {not valid json

data: {"choices": [

data: {"choices":[{"delta":{"content":

`
	for name, body := range map[string]string{
		"explicit [DONE]": chunks + "data: [DONE]\n\n",
		"server closes":   chunks,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := sseServer(body)
			defer srv.Close()

			deltas := collectStream(NewClient(srv.URL, "m"), Request{})

			if len(deltas) != 1 {
				t.Fatalf("deltas = %+v, want exactly the one terminal fault", deltas)
			}
			fault := deltas[0]
			if fault.Kind != DeltaError {
				t.Fatalf("terminal delta = %+v, want a DeltaError", fault)
			}
			if !strings.Contains(fault.Err, "(3 malformed chunks skipped)") {
				t.Errorf("fault %q does not name the three skipped chunks", fault.Err)
			}
			if fault.MalformedChunks != 3 {
				t.Errorf("MalformedChunks = %d, want 3", fault.MalformedChunks)
			}
			if fault.Retryable {
				t.Error("the malformed-only fault is Retryable; the same request would decode no better")
			}
		})
	}
}

// cutServer returns a server that streams body as an event-stream and then kills the TCP
// connection without ending the chunked body — the shape a server or proxy dropping a stream
// mid-reply leaves behind, which the client's chunked reader reports as io.ErrUnexpectedEOF.
// Returning from the handler would end the body cleanly instead, which the provider commits as a
// finished reply — the very case this server exists to tell apart.
func cutServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			panic(http.ErrAbortHandler)
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			panic(http.ErrAbortHandler)
		}
		_ = conn.Close()
	}))
}

// TestStream_MidStreamEOFIsRetryable pins the class of a body cut before its terminator: the
// tokens streamed so far are yielded, then a terminal DeltaError marked Retryable, so the loop
// re-streams it exactly like an in-band 502 (the text-cap overflow stays non-retryable —
// assertReplyTextCapFires). The read fault's own text is kept: it is what the give-up path
// surfaces when the re-stream fails too.
func TestStream_MidStreamEOFIsRetryable(t *testing.T) {
	t.Parallel()

	const body = `data: {"choices":[{"delta":{"content":"Hel"}}]}

data: {"choices":[{"delta":{"content":"lo"}}]}

`
	srv := cutServer(body)
	defer srv.Close()

	deltas := collectStream(NewClient(srv.URL, "m"), Request{})

	var content string
	for _, d := range deltas {
		if d.Kind == DeltaContent {
			content += d.Content
		}
		if d.Kind == DeltaDone {
			t.Fatalf("a cut stream ended in a Done: %+v", d)
		}
	}
	if content != "Hello" {
		t.Errorf("content before the cut = %q, want Hello", content)
	}
	fault := deltas[len(deltas)-1]
	if fault.Kind != DeltaError {
		t.Fatalf("last delta = %+v, want the terminal read fault", fault)
	}
	if !fault.Retryable {
		t.Errorf("read fault %q is not Retryable; a mid-stream EOF is the class the loop re-streams", fault.Err)
	}
	if !strings.Contains(fault.Err, "apogee: read stream: ") || !strings.Contains(fault.Err, "unexpected EOF") {
		t.Errorf("read fault = %q, want the read-stream wording naming the unexpected EOF", fault.Err)
	}
	if fault.MalformedChunks != 0 {
		t.Errorf("MalformedChunks = %d on a stream that decoded cleanly, want 0", fault.MalformedChunks)
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

// streamKindsAndCalls splits a drained stream into the delta kinds in arrival order and the
// tool calls those deltas carried, so a test can assert both the content and the ordering.
func streamKindsAndCalls(t *testing.T, deltas []Delta) ([]DeltaKind, []ToolCall) {
	t.Helper()

	kinds := make([]DeltaKind, 0, len(deltas))
	var calls []ToolCall
	for i := range deltas {
		kinds = append(kinds, deltas[i].Kind)
		if deltas[i].Kind == DeltaToolCall {
			calls = append(calls, *deltas[i].ToolCall)
		}
	}
	return kinds, calls
}

// assertCall fails unless a tool call carries exactly this id, name and joined arguments.
func assertCall(t *testing.T, got ToolCall, id, name, args string) {
	t.Helper()

	if got.ID != id || got.Function.Name != name || got.Function.Arguments != args {
		t.Errorf("call = %+v, want id %q name %q args %q", got, id, name, args)
	}
}

// TestStream_InterleavedIndexedToolCallsStayApart pins the shape the pre-index accumulator
// mis-joined: two parallel calls whose argument fragments arrive interleaved. Keyed by wire
// index each keeps its own arguments; keyed by arrival order the second call's id would have
// flushed the first mid-arguments and the tail of call 0 would have landed on call 1.
func TestStream_InterleavedIndexedToolCallsStayApart(t *testing.T) {
	t.Parallel()

	const body = `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"tc_a","function":{"name":"grep","arguments":"{\"q\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"tc_b","function":{"name":"read","arguments":"{\"p\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"\"y\"}"}}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	srv := sseServer(body)
	defer srv.Close()

	kinds, calls := streamKindsAndCalls(t, collectStream(NewClient(srv.URL, "m"), Request{}))
	if len(calls) != 2 {
		t.Fatalf("tool calls = %+v, want two", calls)
	}
	assertCall(t, calls[0], "tc_a", "grep", `{"q":"x"}`)
	assertCall(t, calls[1], "tc_b", "read", `{"p":"y"}`)

	// Every call reaches the consumer before the terminal Done, exactly as it did when calls
	// were flushed mid-stream.
	want := []DeltaKind{DeltaToolCall, DeltaToolCall, DeltaDone}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("delta kinds = %v, want %v", kinds, want)
	}
}

// TestStream_ToolCallsEmitInIndexOrder pins the emission order: the wire index orders the
// calls, not the order the server happened to open them in.
func TestStream_ToolCallsEmitInIndexOrder(t *testing.T) {
	t.Parallel()

	const body = `data: {"choices":[{"delta":{"tool_calls":[{"index":2,"id":"tc_late","function":{"name":"read","arguments":"{}"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"tc_early","function":{"name":"grep","arguments":"{}"}}]}}]}

data: [DONE]

`
	srv := sseServer(body)
	defer srv.Close()

	_, calls := streamKindsAndCalls(t, collectStream(NewClient(srv.URL, "m"), Request{}))
	if len(calls) != 2 {
		t.Fatalf("tool calls = %+v, want two", calls)
	}
	if calls[0].ID != "tc_early" || calls[1].ID != "tc_late" {
		t.Errorf("call ids = %q, %q, want tc_early, tc_late (ascending index)", calls[0].ID, calls[1].ID)
	}
}

// TestStream_RepeatedIDContinuesOneCall covers the server that repeats the call's id on every
// fragment and sends no index: that is one call, not one per fragment.
func TestStream_RepeatedIDContinuesOneCall(t *testing.T) {
	t.Parallel()

	const body = `data: {"choices":[{"delta":{"tool_calls":[{"id":"tc_1","function":{"name":"grep","arguments":"{\"q\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"id":"tc_1","function":{"arguments":"\"x\""}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"id":"tc_1","function":{"arguments":"}"}}]}}]}

data: [DONE]

`
	srv := sseServer(body)
	defer srv.Close()

	_, calls := streamKindsAndCalls(t, collectStream(NewClient(srv.URL, "m"), Request{}))
	if len(calls) != 1 {
		t.Fatalf("tool calls = %+v, want one joined call", calls)
	}
	assertCall(t, calls[0], "tc_1", "grep", `{"q":"x"}`)
}

// TestStream_FlushesOpenCallsWithoutDone pins the second terminal path: a server that closes
// the connection without [DONE] still hands over the calls it opened, before the synthesised
// Done.
func TestStream_FlushesOpenCallsWithoutDone(t *testing.T) {
	t.Parallel()

	const body = `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"tc_1","function":{"name":"grep","arguments":"{\"q\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]}}]}

`
	srv := sseServer(body)
	defer srv.Close()

	kinds, calls := streamKindsAndCalls(t, collectStream(NewClient(srv.URL, "m"), Request{}))
	if len(calls) != 1 {
		t.Fatalf("tool calls = %+v, want one flushed call", calls)
	}
	assertCall(t, calls[0], "tc_1", "grep", `{"q":"x"}`)
	want := []DeltaKind{DeltaToolCall, DeltaDone}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("delta kinds = %v, want %v", kinds, want)
	}
}

// TestStream_IndexOnlyFragmentOpensNothing pins the guard against manufacturing a nameless,
// id-less call: an index-bearing fragment carrying neither, arriving when no call is open, is
// dropped in silence — no delta of any kind, in particular no DeltaError. (internal/provider
// imports no internal/domain, so the loop's ErrorEvent for such a call is not observable here;
// the point is that no call reaches the loop to be reported at all.)
func TestStream_IndexOnlyFragmentOpensNothing(t *testing.T) {
	t.Parallel()

	const body = `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":1}"}}]}}]}

data: {"choices":[{"delta":{"content":"hi"}}]}

data: [DONE]

`
	srv := sseServer(body)
	defer srv.Close()

	kinds, calls := streamKindsAndCalls(t, collectStream(NewClient(srv.URL, "m"), Request{}))
	if len(calls) != 0 {
		t.Errorf("tool calls = %+v, want none — the fragment names no call to open", calls)
	}
	want := []DeltaKind{DeltaContent, DeltaDone}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("delta kinds = %v, want %v", kinds, want)
	}
}

// TestStream_InBandErrorDropsEveryAccumulatedCall pins what buffering costs: an in-band fault
// now discards every call of the reply, not only the one in progress. The second id-bearing
// fragment is what makes the assertion bite — before this item it flushed the first call, so
// a DeltaToolCall reached the consumer ahead of the fault.
func TestStream_InBandErrorDropsEveryAccumulatedCall(t *testing.T) {
	t.Parallel()

	const body = `data: {"choices":[{"delta":{"tool_calls":[{"id":"tc_1","function":{"name":"grep","arguments":"{}"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"id":"tc_2","function":{"name":"read","arguments":"{}"}}]}}]}

data: {"error":{"message":"upstream died","code":502}}

data: [DONE]

`
	srv := sseServer(body)
	defer srv.Close()

	kinds, calls := streamKindsAndCalls(t, collectStream(NewClient(srv.URL, "m"), Request{}))
	if len(calls) != 0 {
		t.Errorf("tool calls = %+v, want none — the reply is faulted, not partly usable", calls)
	}
	want := []DeltaKind{DeltaError}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("delta kinds = %v, want %v", kinds, want)
	}
}

// TestStream_ToolCallSizeCapSumsAcrossCalls pins the cap on the SUM: two calls each just over
// half the limit trip it together, so a server cannot multiply maxToolCallBytes by the number
// of calls it opens.
func TestStream_ToolCallSizeCapSumsAcrossCalls(t *testing.T) {
	t.Parallel()

	half := strings.Repeat("a", maxToolCallBytes/2+1)
	body := fmt.Sprintf(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"tc_a","function":{"name":"grep","arguments":%q}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"tc_b","function":{"name":"read","arguments":%q}}]}}]}

data: [DONE]

`, half, half)
	srv := sseServer(body)
	defer srv.Close()

	kinds, calls := streamKindsAndCalls(t, collectStream(NewClient(srv.URL, "m"), Request{}))
	if len(calls) != 0 {
		t.Errorf("tool calls = %d, want none past the cap", len(calls))
	}
	if len(kinds) != 1 || kinds[0] != DeltaError {
		t.Fatalf("delta kinds = %v, want a single error", kinds)
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
// ended it (assertReplyTextCapDeltas).
func assertReplyTextCapFires(t *testing.T, srv *httptest.Server) {
	t.Helper()

	deltas := collectStream(NewClient(srv.URL, "m"), Request{})

	assertReplyTextCapDeltas(t, deltas)
}

// assertReplyTextCapDeltas asserts the reply-text cap is what ended the stream that yielded
// deltas, whichever wire parsed it: exactly one terminal DeltaError naming the limit, not
// retryable, no Done at all, and no more text handed to the consumer than the cap allows.
func assertReplyTextCapDeltas(t *testing.T, deltas []Delta) {
	t.Helper()

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
// Both wire spellings of the channel are held to it: an Ollama/OpenRouter stream cannot buy
// itself an unbounded reply by spelling the field the other way.
func TestStream_ThinkingCountsTowardTheCap(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"reasoning_content", "reasoning"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			srv := chunkedTextServer(field)
			defer srv.Close()

			assertReplyTextCapFires(t, srv)
		})
	}
}

// TestStream_CtxCancelEndsTheBody pins the first of the stream's two deadlines: the caller's
// ctx. A server that goes silent mid-reply is held by the idle timeout (the 10m default here,
// far beyond this test) until the caller cancels — and that cancel must end the body read,
// surface as a terminal DeltaError, and close the connection the handler is sitting on. The
// second deadline, the idle cut itself, is TestStream_IdleTimeoutCutsASilentBody's.
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

// collectThinkingAndContent drains a stream and returns the joined thinking and content the
// consumer saw, plus how many thinking deltas arrived — the count is what proves a chunk
// carrying BOTH spellings yields its reasoning once and only once.
func collectThinkingAndContent(t *testing.T, body string) (thinking, content string, thinkingDeltas int) {
	t.Helper()

	srv := sseServer(body)
	defer srv.Close()

	for _, delta := range collectStream(NewClient(srv.URL, "m"), Request{}) {
		switch delta.Kind {
		case DeltaThinking:
			thinking += delta.Thinking
			thinkingDeltas++
		case DeltaContent:
			content += delta.Content
		case DeltaError, DeltaContextOverflow:
			t.Fatalf("unexpected terminal delta: %+v", delta)
		}
	}
	return thinking, content, thinkingDeltas
}

// TestStream_ThinkingChannelSpellings covers the server matrix the thinking channel actually
// arrives in: llama.cpp and vLLM spell it reasoning_content, Ollama and OpenRouter spell it
// reasoning, and a duplicating proxy sends both. All three must reach the consumer as the same
// joined thinking text — and the both-case must yield it ONCE, never twice.
func TestStream_ThinkingChannelSpellings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		delta        string
		wantThinking string
		wantDeltas   int
	}{
		{
			name:         "reasoning_content only (llama.cpp, vLLM)",
			delta:        `{"reasoning_content":"hmm"}`,
			wantThinking: "hmm",
			wantDeltas:   1,
		},
		{
			name:         "reasoning only (Ollama, OpenRouter)",
			delta:        `{"reasoning":"hmm"}`,
			wantThinking: "hmm",
			wantDeltas:   1,
		},
		{
			name:         "both spellings on one chunk",
			delta:        `{"reasoning_content":"hmm","reasoning":"hmm"}`,
			wantThinking: "hmm",
			wantDeltas:   1,
		},
		{
			name:         "empty reasoning_content beside a populated reasoning (LM Studio shape)",
			delta:        `{"reasoning_content":"","reasoning":"hmm"}`,
			wantThinking: "hmm",
			wantDeltas:   1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := "data: {\"choices\":[{\"delta\":" + tc.delta + "}]}\n\n" +
				"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n" +
				"data: [DONE]\n\n"

			thinking, content, deltas := collectThinkingAndContent(t, body)
			if thinking != tc.wantThinking {
				t.Errorf("thinking = %q, want %q", thinking, tc.wantThinking)
			}
			if deltas != tc.wantDeltas {
				t.Errorf("thinking delta count = %d, want %d — the channel must not be yielded twice", deltas, tc.wantDeltas)
			}
			if content != "hi" {
				t.Errorf("content = %q, want hi", content)
			}
		})
	}
}

// TestStream_ThinkingSpellingIsPerChunk pins that nothing latches onto the first spelling it
// sees: a stream that opens in reasoning_content and continues in reasoning yields both, in
// order. Real mixed rosters and duplicating proxies produce exactly this.
func TestStream_ThinkingSpellingIsPerChunk(t *testing.T) {
	t.Parallel()

	const body = `data: {"choices":[{"delta":{"reasoning_content":"first "}}]}

data: {"choices":[{"delta":{"reasoning":"second "}}]}

data: {"choices":[{"delta":{"reasoning":"third"}}]}

data: {"choices":[{"delta":{"content":"answer"},"finish_reason":"stop"}]}

data: [DONE]

`

	thinking, content, _ := collectThinkingAndContent(t, body)
	if thinking != "first second third" {
		t.Errorf("thinking = %q, want %q", thinking, "first second third")
	}
	if content != "answer" {
		t.Errorf("content = %q, want answer", content)
	}
}

// TestStream_NullReasoningYieldsNoThinking pins OpenRouter's terminal chunk shape: it spells
// the channel as JSON null, which decodes to the four bytes "null" rather than to "". Nothing
// but the empty string may reach the consumer as thinking.
func TestStream_NullReasoningYieldsNoThinking(t *testing.T) {
	t.Parallel()

	const body = `data: {"choices":[{"delta":{"content":"hi","reasoning":null}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

	thinking, content, deltas := collectThinkingAndContent(t, body)
	if deltas != 0 || thinking != "" {
		t.Errorf("thinking = %q over %d deltas, want none — a null reasoning is no reasoning", thinking, deltas)
	}
	if content != "hi" {
		t.Errorf("content = %q, want hi", content)
	}
}

// TestStream_NonStringReasoningKeepsTheChunk is why the field is decoded as json.RawMessage: a
// server that spells reasoning as an OBJECT must not cost the chunk its content. A string-typed
// field would fail the whole Unmarshal and the chunk would be dropped as malformed.
func TestStream_NonStringReasoningKeepsTheChunk(t *testing.T) {
	t.Parallel()

	const body = `data: {"choices":[{"delta":{"content":"kept","reasoning":{"effort":"high"}}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

	thinking, content, deltas := collectThinkingAndContent(t, body)
	if content != "kept" {
		t.Errorf("content = %q, want kept — the chunk was dropped, not decoded", content)
	}
	if deltas != 0 || thinking != "" {
		t.Errorf("thinking = %q over %d deltas, want none — a non-string reasoning is no reasoning", thinking, deltas)
	}
}

// holdServer returns a server whose handler writes body — when there is one — and flushes it,
// then holds the connection open, silent, until the client goes away or the returned stop func
// releases it (and closes the server). An empty body holds BEFORE the headers: nothing is written,
// so the client sits waiting for the response line. Stop is safe to call from a defer after a
// failed assertion — the release runs first, so Close cannot deadlock on the handler.
func holdServer(body string) (*httptest.Server, func()) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body != "" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, body)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	var once sync.Once
	return srv, func() {
		once.Do(func() { close(release) })
		srv.Close()
	}
}

// dripServer returns a server that writes line every interval for the whole of span, flushing
// each one, then ends the stream with [DONE] — activity that must hold an idle window shorter
// than span open.
func dripServer(line string, interval, span time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for end := time.Now().Add(span); time.Now().Before(end); time.Sleep(interval) {
			_, _ = io.WriteString(w, line)
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
}

// streamAsync drives a Stream on its own goroutine and hands back the channel its deltas arrive
// on, closed when the range ends — for a test that must act (cancel, wait) mid-stream.
func streamAsync(ctx context.Context, client *Client) <-chan Delta {
	streamed := make(chan Delta, 16)
	go func() {
		defer close(streamed)
		for delta := range client.Stream(ctx, Request{}) {
			streamed <- delta
		}
	}()
	return streamed
}

// drainWithin collects every delta until the channel closes, failing the test when that takes
// longer than limit.
func drainWithin(t *testing.T, streamed <-chan Delta, limit time.Duration) []Delta {
	t.Helper()
	var got []Delta
	deadline := time.After(limit)
	for {
		select {
		case delta, open := <-streamed:
			if !open {
				return got
			}
			got = append(got, delta)
		case <-deadline:
			t.Fatalf("the stream did not end within %s; deltas so far: %+v", limit, got)
		}
	}
}

// idleCutBody is a content chunk followed by an open tool-call fragment — the shape whose
// content must reach the consumer live while the unfinished call must not be flushed by the
// cut.
const idleCutBody = `data: {"choices":[{"delta":{"content":"Hel"}}]}

data: {"choices":[{"delta":{"tool_calls":[{"id":"tc_1","function":{"name":"grep","arguments":"{\"q\":"}}]}}]}

`

// assertIdleCut checks the terminal delta of a stream the idle timeout cut: a Retryable
// DeltaError whose text is exactly the read-stream rendering of the idle sentinel, with no
// `apogee:` doubled and no Done anywhere.
func assertIdleCut(t *testing.T, deltas []Delta, window time.Duration) {
	t.Helper()
	if len(deltas) == 0 {
		t.Fatal("the cut stream yielded nothing")
	}
	for _, d := range deltas {
		if d.Kind == DeltaDone {
			t.Fatalf("an idle-cut stream ended in a Done: %+v", d)
		}
	}
	fault := deltas[len(deltas)-1]
	if fault.Kind != DeltaError {
		t.Fatalf("last delta = %+v, want the terminal idle fault", fault)
	}
	if !fault.Retryable {
		t.Errorf("idle fault %q is not Retryable; the idle cut is the class the loop re-streams", fault.Err)
	}
	want := fmt.Sprintf("apogee: read stream: upstream stream idle for %s", window)
	if fault.Err != want {
		t.Errorf("idle fault = %q, want exactly %q", fault.Err, want)
	}
}

// TestStream_IdleTimeoutCutsASilentBody pins the idle cut at the body layer: the content
// streamed before the upstream fell silent arrives live, the tool-call fragment still open at the
// cut is NOT flushed, and the sequence ends with a Retryable DeltaError rendered exactly as the
// codec's read fault over the idle sentinel.
func TestStream_IdleTimeoutCutsASilentBody(t *testing.T) {
	t.Parallel()

	const window = 50 * time.Millisecond
	srv, stop := holdServer(idleCutBody)
	defer stop()

	deltas := collectStream(NewClient(srv.URL, "m", WithStreamIdleTimeout(window)), Request{})

	var content string
	for _, d := range deltas {
		switch d.Kind {
		case DeltaContent:
			content += d.Content
		case DeltaToolCall:
			t.Errorf("the open tool-call fragment was flushed by the cut: %+v", d.ToolCall)
		}
	}
	if content != "Hel" {
		t.Errorf("content before the cut = %q, want Hel", content)
	}
	assertIdleCut(t, deltas, window)
}

// TestStream_IdleTimeoutCutsTheAnthropicWire proves the cut sits beneath the codec: the same
// silent body on the anthropic wire is cut and rendered identically.
func TestStream_IdleTimeoutCutsTheAnthropicWire(t *testing.T) {
	t.Parallel()

	const window = 50 * time.Millisecond
	const body = "event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}` + "\n\n"
	srv, stop := holdServer(body)
	defer stop()

	client := NewClient(srv.URL, "m", WithWire(WireAnthropic), WithStreamIdleTimeout(window))
	deltas := collectStream(client, Request{Messages: []Message{{Role: "user", Content: "hi"}}})

	if len(deltas) == 0 || deltas[0].Kind != DeltaContent || deltas[0].Content != "Hel" {
		t.Errorf("first delta = %+v, want the content streamed before the cut", deltas)
	}
	assertIdleCut(t, deltas, window)
}

// TestClientStreamIdleTimeoutDefaultsAndOverrides pins the option's contract: 10m unless set,
// and 0 when disabled.
func TestClientStreamIdleTimeoutDefaultsAndOverrides(t *testing.T) {
	t.Parallel()

	if got := NewClient("http://unused.invalid", "m").StreamIdleTimeout(); got != 10*time.Minute {
		t.Errorf("default StreamIdleTimeout() = %s, want 10m", got)
	}
	if got := NewClient("http://unused.invalid", "m", WithStreamIdleTimeout(0)).StreamIdleTimeout(); got != 0 {
		t.Errorf("WithStreamIdleTimeout(0) reports %s, want 0", got)
	}
	if got := NewClient("http://unused.invalid", "m", WithStreamIdleTimeout(time.Second)).StreamIdleTimeout(); got != time.Second {
		t.Errorf("WithStreamIdleTimeout(1s) reports %s, want 1s", got)
	}
}

// assertDripHoldsWindow pins that bytes of the given kind reset the window: a server dripping
// line at an interval well inside a window shorter than the span it drips over is never cut,
// and ends in a Done. The drip is far inside the window so a scheduling hiccup on a loaded box
// cannot fail the check; wantKind, when set, is a delta kind the drip must have yielded.
func assertDripHoldsWindow(t *testing.T, line string, wantKind DeltaKind) {
	t.Helper()
	const (
		window   = 100 * time.Millisecond
		interval = 10 * time.Millisecond
		span     = 300 * time.Millisecond
	)
	srv := dripServer(line, interval, span)
	defer srv.Close()

	deltas := collectStream(NewClient(srv.URL, "m", WithStreamIdleTimeout(window)), Request{})

	var seen int
	for _, d := range deltas {
		if d.Kind == DeltaError {
			t.Fatalf("the dripping stream was cut: %+v", d)
		}
		if wantKind != "" && d.Kind == wantKind {
			seen++
		}
	}
	if last := deltas[len(deltas)-1]; last.Kind != DeltaDone {
		t.Errorf("last delta = %+v, want Done", last)
	}
	if wantKind != "" && seen == 0 {
		t.Errorf("no %s delta arrived from the drip", wantKind)
	}
}

// TestStream_IdleTimeoutCountsReasoningAsActivity: reasoning deltas hold the window open, and
// arrive as thinking deltas.
func TestStream_IdleTimeoutCountsReasoningAsActivity(t *testing.T) {
	t.Parallel()

	assertDripHoldsWindow(t, `data: {"choices":[{"delta":{"reasoning_content":"."}}]}`+"\n\n", DeltaThinking)
}

// TestStream_IdleTimeoutCountsSSECommentsAsActivity: `: keep-alive` comment lines — bytes the
// codec never yields — hold the window open all the same.
func TestStream_IdleTimeoutCountsSSECommentsAsActivity(t *testing.T) {
	t.Parallel()

	assertDripHoldsWindow(t, ": keep-alive\n\n", "")
}

// TestStream_IdleTimeoutCutsAPreHeaderStall pins the same window over the wait for headers: a
// server that accepts the connection and never answers is cut, and the fault is the idle one —
// Retryable, naming the window — never a caller cancel, which is what the child ctx that cut it
// would otherwise read as.
func TestStream_IdleTimeoutCutsAPreHeaderStall(t *testing.T) {
	t.Parallel()

	const window = 50 * time.Millisecond
	srv, stop := holdServer("")
	defer stop()

	deltas := collectStream(NewClient(srv.URL, "m", WithStreamIdleTimeout(window)), Request{})

	if len(deltas) != 1 {
		t.Fatalf("deltas = %+v, want exactly the terminal fault", deltas)
	}
	fault := deltas[0]
	if fault.Kind != DeltaError || !fault.Retryable {
		t.Fatalf("delta = %+v, want a Retryable DeltaError", fault)
	}
	if !strings.Contains(fault.Err, "upstream stream idle for 50ms") || strings.Contains(fault.Err, "context canceled") {
		t.Errorf("pre-header fault = %q, want the idle wording, not a caller cancel", fault.Err)
	}
}

// TestStream_IdleTimeoutOffWaitsForCtx pins WithStreamIdleTimeout(0): with the cut disabled, a
// silent upstream — mid-body or before its headers — ends only when the caller cancels.
func TestStream_IdleTimeoutOffWaitsForCtx(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name string
		body string
	}{
		{"silent body", `data: {"choices":[{"delta":{"content":"hi"}}]}` + "\n\n"},
		{"before headers", ""},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			srv, stop := holdServer(row.body)
			defer stop()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			streamed := streamAsync(ctx, NewClient(srv.URL, "m", WithStreamIdleTimeout(0)))
			var got []Delta
			settle := time.After(200 * time.Millisecond)
			for waiting := true; waiting; {
				select {
				case delta, open := <-streamed:
					if !open || delta.Kind == DeltaError {
						t.Fatalf("the stream ended before the ctx was cancelled: open=%v %+v", open, delta)
					}
					got = append(got, delta)
				case <-settle:
					waiting = false
				}
			}
			cancel()
			got = append(got, drainWithin(t, streamed, 2*time.Second)...)

			if row.body != "" && (len(got) < 2 || got[0].Kind != DeltaContent) {
				t.Errorf("deltas = %+v, want the content then the cancel fault", got)
			}
			if terminal := got[len(got)-1]; terminal.Kind != DeltaError || terminal.Retryable {
				t.Errorf("terminal delta = %+v, want the non-retryable ctx-cancel fault", terminal)
			}
		})
	}
}

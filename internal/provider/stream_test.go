package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	if done.Usage == nil || *done.Usage != (Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7}) {
		t.Errorf("usage = %+v, want {3 4 7}", done.Usage)
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

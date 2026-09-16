package stubllm

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/provider"
)

// TestInProcessPlaysTheSameBytesAsAListener pins the parity the in-process transport exists
// for: one Script, served once through a loopback listener and once through Handler over the
// pipe, puts the same SSE payloads in front of the client. If this holds, every engine test
// that plays a Script in process is testing the wire shape a listening stub would have sent.
func TestInProcessPlaysTheSameBytesAsAListener(t *testing.T) {
	t.Parallel()

	script := Script{Model: "stub-model", Turns: []Turn{{
		Reasoning:  "thinking it over",
		Text:       "Hello, world",
		ChunkRunes: 5,
		ToolCalls:  []ToolCall{{Name: "list_dir", Arguments: `{"path":"."}`}},
		Usage:      &Usage{Prompt: 3, Completion: 4, Cached: 1},
	}}}
	body := `{"model":"stub-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`

	listening, err := readStreamRaw(t, New(t, script), body)
	if err != nil {
		t.Fatalf("listening stub: %v", err)
	}
	inProcess, err := readInProcessRaw(t, InProcess(t, script), body)
	if err != nil {
		t.Fatalf("in-process stub: %v", err)
	}

	if inProcess != listening {
		t.Errorf("in-process bytes differ from the listener's:\n got %q\nwant %q", inProcess, listening)
	}
}

// TestNarratingToolCallTurnFramesTheCallAfterTheText pins the shape a narrating model sends
// on the wire: the content deltas first, then the tool call in its head/tail split, ending on
// tool_calls — and the provider client reads back one text and one whole call.
func TestNarratingToolCallTurnFramesTheCallAfterTheText(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{
		Text:      "Let me look.",
		ToolCalls: []ToolCall{{Name: "list_dir", Arguments: `{"path":"."}`}},
		Repeat:    true,
	}}})

	t.Run("wire framing", func(t *testing.T) {
		events := postStream(t, server, "hi")

		want := []string{"Let ", "me l", "ook."}
		if got := contentDeltas(t, events); !reflect.DeepEqual(got, want) {
			t.Errorf("content deltas = %q, want %q before the call", got, want)
		}
		order := []int{
			indexOf(events, `"content":"ook."`),
			indexOf(events, `"id":"call_1","type":"function","function":{"name":"list_dir"`),
			indexOf(events, `"tool_calls":[{"index":0,"function":{"arguments"`),
			indexOf(events, `"finish_reason":"tool_calls"`),
		}
		for i := 1; i < len(order); i++ {
			if order[i-1] < 0 || order[i] <= order[i-1] {
				t.Fatalf("events out of order (positions %v): want the last text delta, then the call's head, its tail, then tool_calls; got %q", order, events)
			}
		}
	})

	t.Run("through the provider client", func(t *testing.T) {
		deltas := streamThroughProvider(t, server, "hi")

		var text string
		var calls []provider.ToolCall
		for _, d := range deltas {
			switch d.Kind {
			case provider.DeltaContent:
				text += d.Content
			case provider.DeltaToolCall:
				calls = append(calls, *d.ToolCall)
			}
		}
		if text != "Let me look." {
			t.Errorf("text = %q, want the narration whole", text)
		}
		if len(calls) != 1 || calls[0].Function.Name != "list_dir" || calls[0].Function.Arguments != `{"path":"."}` {
			t.Errorf("calls = %+v, want one whole list_dir call", calls)
		}
		if got := terminal(t, deltas).FinishReason; got != "tool_calls" {
			t.Errorf("finish reason = %q, want tool_calls", got)
		}
	})
}

// TestChunksReachTheClientAtTheirBoundaries pins that a hand-placed chunks list arrives one
// delta per element through the provider client — the `<think>` tag in a delta of its own is
// exactly what a suppression test has to see — for the content and reasoning channels alike.
func TestChunksReachTheClientAtTheirBoundaries(t *testing.T) {
	t.Parallel()

	server := InProcess(t, Script{Model: "stub-model", Turns: []Turn{{
		ReasoningChunks: []string{"Weighing ", "the greeting."},
		Chunks:          []string{"Let me check. ", "<think>", "hidden", "</think>", "Hello!"},
	}}})

	deltas := streamInProcess(t, server, "hi")

	var content, reasoning []string
	for _, d := range deltas {
		switch d.Kind {
		case provider.DeltaContent:
			content = append(content, d.Content)
		case provider.DeltaThinking:
			reasoning = append(reasoning, d.Thinking)
		}
	}
	wantContent := []string{"Let me check. ", "<think>", "hidden", "</think>", "Hello!"}
	if !reflect.DeepEqual(content, wantContent) {
		t.Errorf("content deltas = %q, want the hand-placed %q", content, wantContent)
	}
	wantReasoning := []string{"Weighing ", "the greeting."}
	if !reflect.DeepEqual(reasoning, wantReasoning) {
		t.Errorf("reasoning deltas = %q, want the hand-placed %q", reasoning, wantReasoning)
	}
}

// TestInProcessCutSurfacesAsAnUnexpectedEOF pins the transport's one fault translation: a
// `cut` turn's kill, which has no socket to drop, ends the pipe with io.ErrUnexpectedEOF — the
// read error the provider client classes retryable, exactly as it does for a dropped
// connection — after the deltas before the cut have arrived.
func TestInProcessCutSurfacesAsAnUnexpectedEOF(t *testing.T) {
	t.Parallel()

	t.Run("raw read", func(t *testing.T) {
		t.Parallel()

		server := InProcess(t, Script{Model: "stub-model", Turns: []Turn{{
			Text:       "Hello, world",
			ChunkRunes: 5,
			Cut:        &Cut{AfterRunes: 7},
		}}})

		raw, err := readInProcessRaw(t, server, `{"model":"stub-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("read error = %v, want io.ErrUnexpectedEOF from the killed pipe", err)
		}
		if !strings.Contains(raw, `"content":"Hello"`) || strings.Contains(raw, "[DONE]") {
			t.Errorf("stream %q, want the deltas before the cut and no terminator", raw)
		}
	})

	t.Run("through the provider client", func(t *testing.T) {
		t.Parallel()

		server := InProcess(t, Script{Model: "stub-model", Turns: []Turn{{Text: "Hello", Cut: &Cut{AfterRunes: 2}}}})

		var fault provider.Delta
		client := provider.NewClient("http://stubllm", server.Model, provider.WithHTTPClient(&http.Client{Transport: server.Transport()}))
		for delta := range client.Stream(t.Context(), provider.Request{Messages: []provider.Message{{Role: "user", Content: "hi"}}}) {
			if delta.Kind == provider.DeltaError {
				fault = delta
			}
		}

		if fault.Kind != provider.DeltaError || !fault.Retryable {
			t.Errorf("terminal delta = %+v, want a retryable DeltaError — the dropped-stream class", fault)
		}
	})

	t.Run("killed before any status is a transport error", func(t *testing.T) {
		t.Parallel()

		transport := pipeTransport{handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic(http.ErrAbortHandler)
		})}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://stubllm/", nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}

		_, err = transport.RoundTrip(request)

		if !errors.Is(err, errKilledBeforeReply) {
			t.Errorf("RoundTrip error = %v, want errKilledBeforeReply", err)
		}
	})
}

// TestInProcessHTTPTurnKeepsItsStatus pins that a raw HTTP reply crosses the pipe with its
// status, headers and body intact — an in-process 400 is a 400 to the client.
func TestInProcessHTTPTurnKeepsItsStatus(t *testing.T) {
	t.Parallel()

	server := InProcess(t, Script{Model: "stub-model", Turns: []Turn{{
		HTTP: &HTTPReply{Status: 400, Body: `{"error":{"message":"too many tokens"}}`, ContentType: "application/json"},
	}}})

	resp, err := inProcessClient(server).Post("http://stubllm/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"stub-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := readAll(resp)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}

	if resp.StatusCode != 400 || resp.Header.Get("Content-Type") != "application/json" || body != `{"error":{"message":"too many tokens"}}` {
		t.Errorf("reply = %d %q %q, want the scripted 400 with its content type and body", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
}

// inProcessClient is an http.Client that reaches server through its Transport; the host in a
// URL is immaterial, since nothing is dialled.
func inProcessClient(server *Server) *http.Client {
	return &http.Client{Transport: server.Transport()}
}

// readInProcessRaw is readStreamRaw over the in-process transport: one request, the raw body
// drained, and the read error returned for the caller to judge.
func readInProcessRaw(t *testing.T, server *Server, body string) (string, error) {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://stubllm/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	resp, err := inProcessClient(server).Do(request)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	return string(raw), err
}

// streamInProcess is streamThroughProvider over the in-process transport.
func streamInProcess(t *testing.T, server *Server, prompt string) []provider.Delta {
	t.Helper()

	var deltas []provider.Delta
	client := provider.NewClient("http://stubllm", server.Model, provider.WithHTTPClient(inProcessClient(server)))
	for delta := range client.Stream(t.Context(), provider.Request{
		Messages: []provider.Message{{Role: "user", Content: prompt}},
	}) {
		if delta.Kind == provider.DeltaError {
			t.Fatalf("stream error: %s", delta.Err)
		}
		deltas = append(deltas, delta)
	}
	return deltas
}

// indexOf is the position of the first event containing marker, or -1.
func indexOf(events []string, marker string) int {
	for i, event := range events {
		if strings.Contains(event, marker) {
			return i
		}
	}
	return -1
}

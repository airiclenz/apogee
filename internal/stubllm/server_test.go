package stubllm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/provider"
)

// TestServerStreamsTextInConfiguredChunks pins the two halves of the text turn: the exact rune
// chunking the script asked for on the wire, and the same text reassembled by the real
// provider client. The stub is only useful if both hold — a chunking bug that the client
// happens to smooth over is still a wire shape no real server produces.
func TestServerStreamsTextInConfiguredChunks(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{
		Text:       "Hello, world",
		ChunkRunes: 5,
		Repeat:     true,
	}}})

	t.Run("wire chunking", func(t *testing.T) {
		events := postStream(t, server, "hi")

		want := []string{"Hello", ", wor", "ld"}
		if got := contentDeltas(t, events); !reflect.DeepEqual(got, want) {
			t.Errorf("content deltas = %q, want %q", got, want)
		}
	})

	t.Run("through the provider client", func(t *testing.T) {
		deltas := streamThroughProvider(t, server, "hi")

		var content string
		for _, d := range deltas {
			if d.Kind == provider.DeltaContent {
				content += d.Content
			}
		}
		if content != "Hello, world" {
			t.Errorf("assembled content = %q, want %q", content, "Hello, world")
		}
	})
}

// TestServerStreamsToolCallThroughProvider pins that a scripted call survives the two-fragment
// split real servers use: the client must see one named, id'd call with whole arguments.
func TestServerStreamsToolCallThroughProvider(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{
		ToolCalls: []ToolCall{{Name: "list_dir", Arguments: `{"path":"."}`}},
	}}})

	deltas := streamThroughProvider(t, server, "look around")

	var calls []provider.ToolCall
	var finish string
	for _, d := range deltas {
		switch d.Kind {
		case provider.DeltaToolCall:
			calls = append(calls, *d.ToolCall)
		case provider.DeltaDone:
			finish = d.FinishReason
		}
	}
	if len(calls) != 1 {
		t.Fatalf("tool calls = %+v, want exactly one", calls)
	}
	if calls[0].ID != "call_1" || calls[0].Function.Name != "list_dir" || calls[0].Function.Arguments != `{"path":"."}` {
		t.Errorf("tool call = %+v, want call_1 list_dir with whole arguments", calls[0])
	}
	if finish != "tool_calls" {
		t.Errorf("finish reason = %q, want tool_calls", finish)
	}
}

// TestServerUsageReportsCachedShareOnlyWhenSet pins the distinction the provider seam depends
// on: an absent prompt_tokens_details member means "this server does not report caching",
// while a present one reporting zero would mean "nothing was cached". Both must be scriptable,
// so the member appears only above zero.
func TestServerUsageReportsCachedShareOnlyWhenSet(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		cached      int
		wantDetails bool
	}{
		{name: "no cached share", cached: 0, wantDetails: false},
		{name: "cached share", cached: 3, wantDetails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := New(t, Script{Model: "stub-model", Turns: []Turn{{
				Text:  "hi",
				Usage: &Usage{Prompt: 10, Completion: 2, Cached: tc.cached},
			}}})

			events := postStream(t, server, "hi")

			usage := findEvent(t, events, "prompt_tokens")
			if strings.Contains(usage, "prompt_tokens_details") != tc.wantDetails {
				t.Errorf("usage event = %s, want prompt_tokens_details present=%v", usage, tc.wantDetails)
			}
		})
	}
}

// TestServerCachedShareReachesTheProviderSeam pins that the scripted cached share arrives as
// the seam's own counter, not merely as bytes on the wire.
func TestServerCachedShareReachesTheProviderSeam(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{
		Text:  "hi",
		Usage: &Usage{Prompt: 5000, Completion: 40, Cached: 1200},
	}}})

	deltas := streamThroughProvider(t, server, "hi")

	done := terminal(t, deltas)
	want := provider.Usage{PromptTokens: 5000, CompletionTokens: 40, TotalTokens: 5040, CachedPromptTokens: 1200}
	if done.Usage == nil || *done.Usage != want {
		t.Errorf("usage = %+v, want %+v", done.Usage, want)
	}
}

// TestServerMatchedTurnBeatsOrderAndRepeatIsNeverConsumed pins the matching rule: a `when:`
// turn jumps the queue for the requests it recognises, the ordered turns keep their own order
// around it, and a repeating turn stays available for every later request.
func TestServerMatchedTurnBeatsOrderAndRepeatIsNeverConsumed(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{
		{Text: "first"},
		{When: &Match{LastMessage: "^weather"}, Text: "sunny", Repeat: true},
		{Text: "second"},
	}})

	prompts := []string{"weather today?", "hello", "weather tomorrow?", "bye"}
	want := []string{"sunny", "first", "sunny", "second"}
	for i, prompt := range prompts {
		if got := wholeText(t, server, prompt); got != want[i] {
			t.Errorf("reply to %q = %q, want %q", prompt, got, want[i])
		}
	}

	wantTurns := []int{1, 0, 1, 2}
	var gotTurns []int
	for _, r := range server.Requests() {
		gotTurns = append(gotTurns, r.TurnIndex)
	}
	if !reflect.DeepEqual(gotTurns, wantTurns) {
		t.Errorf("turns served = %v, want %v", gotTurns, wantTurns)
	}
	server.AssertConsumed(t)
}

// TestServerMatchesOnAToolResultByName pins the tool_result matcher, which has to follow the
// tool_call_id back to the assistant turn that issued the call because the wire shape of a
// tool result carries no name.
func TestServerMatchesOnAToolResultByName(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{
		{When: &Match{ToolResult: "list_dir"}, Text: "I looked around"},
		{Text: "unreachable ordered turn"},
	}})

	body := `{"model":"stub-model","messages":[
		{"role":"user","content":"look"},
		{"role":"assistant","content":null,"tool_calls":[{"id":"tc_9","type":"function","function":{"name":"list_dir","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"tc_9","content":"a.txt"}
	]}`
	if got := decodeWhole(t, post(t, server, body)).Choices[0].Message.Content; got != "I looked around" {
		t.Errorf("reply = %q, want the tool_result turn", got)
	}
}

// TestServerUnmatchedRequestFailsLoudly pins the strictness rule: a request the script did not
// anticipate is a 500 and a logged entry, never an improvised reply that would turn the most
// interesting failure a driver test can surface into a green run.
func TestServerUnmatchedRequestFailsLoudly(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{Text: "only"}}})

	_ = wholeText(t, server, "first")
	resp := post(t, server, `{"model":"stub-model","messages":[{"role":"user","content":"second"}]}`)

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.status)
	}
	if !strings.Contains(resp.body, "stubllm: no turn for request 2") {
		t.Errorf("body = %q, want it to name the unanswered request", resp.body)
	}
	unmatched := server.Unmatched()
	if len(unmatched) != 1 || unmatched[0].N != 2 || unmatched[0].TurnIndex != -1 {
		t.Errorf("unmatched log = %+v, want one entry for request 2 with no turn", unmatched)
	}
}

// TestServerHTTPTurnWritesNoStream pins that an http turn replaces the completion entirely: a
// redirect keeps its status and Location and carries not one SSE event.
func TestServerHTTPTurnWritesNoStream(t *testing.T) {
	t.Parallel()

	const location = "https://elsewhere.example/v1/chat/completions"
	server := New(t, Script{Model: "stub-model", Turns: []Turn{{
		HTTP: &HTTPReply{Status: http.StatusPermanentRedirect, Location: location, Body: "moved"},
	}}})

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp := postWith(t, client, server, `{"model":"stub-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	if resp.status != http.StatusPermanentRedirect {
		t.Errorf("status = %d, want 308", resp.status)
	}
	if resp.location != location {
		t.Errorf("Location = %q, want %q", resp.location, location)
	}
	if strings.Contains(resp.contentType, "event-stream") || strings.Contains(resp.body, "data: ") {
		t.Errorf("reply = %q (content-type %q), want no SSE", resp.body, resp.contentType)
	}
}

// TestServerHangEndsWithTheRequestContext pins both halves of a hang turn: a cancelled request
// releases it at once (the shape a cancel test needs), and an elapsed one answers as the
// empty-reply turn does.
func TestServerHangEndsWithTheRequestContext(t *testing.T) {
	t.Parallel()

	t.Run("cancelled", func(t *testing.T) {
		t.Parallel()

		server := New(t, Script{Model: "stub-model", Turns: []Turn{{Hang: time.Minute}}})
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		started := time.Now()
		request := newRequest(t, ctx, server, `{"model":"stub-model","messages":[{"role":"user","content":"hi"}]}`)
		if _, err := http.DefaultClient.Do(request); err == nil {
			t.Fatal("request succeeded, want the cancelled context to end it")
		}
		if elapsed := time.Since(started); elapsed > 10*time.Second {
			t.Errorf("cancel took %s, want the hang to release with the context", elapsed)
		}
	})

	t.Run("elapsed", func(t *testing.T) {
		t.Parallel()

		server := New(t, Script{Model: "stub-model", Turns: []Turn{{Hang: 10 * time.Millisecond}}})

		deltas := streamThroughProvider(t, server, "hi")

		if done := terminal(t, deltas); done.FinishReason != "stop" {
			t.Errorf("finish reason = %q, want stop", done.FinishReason)
		}
		for _, d := range deltas {
			if d.Kind == provider.DeltaContent {
				t.Errorf("content delta %q, want an empty reply", d.Content)
			}
		}
	})
}

// TestServerCutTurnKillsTheConnection pins the `cut` terminator to the connection KILL: the
// client reads the deltas before the cut point and then io.ErrUnexpectedEOF, never a clean
// end. A handler that merely returned would close the chunked body cleanly — a clean EOF the
// provider commits as Done(stop) — and io.ReadAll would succeed, so the errors.Is here is what
// tells the two apart.
func TestServerCutTurnKillsTheConnection(t *testing.T) {
	t.Parallel()

	t.Run("inside the text", func(t *testing.T) {
		t.Parallel()

		server := New(t, Script{Model: "stub-model", Turns: []Turn{{
			Text:       "Hello, world",
			ChunkRunes: 5,
			Cut:        &Cut{AfterRunes: 7},
		}}})

		raw, err := readStreamRaw(t, server, `{"model":"stub-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("read error = %v, want io.ErrUnexpectedEOF from the killed connection", err)
		}
		for _, want := range []string{`"content":"Hello"`, `"content":", "`} {
			if !strings.Contains(raw, want) {
				t.Errorf("stream %q lacks %s, want the deltas before the cut", raw, want)
			}
		}
		for _, unwanted := range []string{"ld", "finish_reason", "[DONE]"} {
			if strings.Contains(raw, unwanted) {
				t.Errorf("stream %q carries %q, want nothing past the cut point", raw, unwanted)
			}
		}
	})

	t.Run("past the text streams the tool calls then dies", func(t *testing.T) {
		t.Parallel()

		server := New(t, Script{Model: "stub-model", Turns: []Turn{{
			ToolCalls: []ToolCall{{Name: "list_dir", Arguments: `{"path":"."}`}},
			Cut:       &Cut{AfterRunes: 0},
		}}})

		raw, err := readStreamRaw(t, server, `{"model":"stub-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("read error = %v, want io.ErrUnexpectedEOF from the killed connection", err)
		}
		if !strings.Contains(raw, `"name":"list_dir"`) {
			t.Errorf("stream %q lacks the tool-call head, want every delta before the terminator", raw)
		}
		if strings.Contains(raw, "finish_reason") || strings.Contains(raw, "[DONE]") {
			t.Errorf("stream %q carries a terminator, want the kill in its place", raw)
		}
	})

	t.Run("non-streamed", func(t *testing.T) {
		t.Parallel()

		server := New(t, Script{Model: "stub-model", Turns: []Turn{{Text: "hi", Cut: &Cut{AfterRunes: 1}}}})

		raw, err := readStreamRaw(t, server, `{"model":"stub-model","messages":[{"role":"user","content":"hi"}]}`)

		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("read error = %v, want io.ErrUnexpectedEOF from the killed connection", err)
		}
		if raw != "" {
			t.Errorf("body = %q, want nothing before the kill", raw)
		}
	})
}

// TestServerErrorTurnEndsWithTheInBandObject pins the `error` terminator: the leading deltas
// stream as usual, then the in-band object takes the terminator's place — and the real
// provider client faults on it as a retryable 502 rather than reading on to the [DONE].
func TestServerErrorTurnEndsWithTheInBandObject(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{
		Text:   "Hello",
		Error:  &InBandError{Message: "upstream gone"},
		Repeat: true,
	}}})

	t.Run("wire", func(t *testing.T) {
		events := postStream(t, server, "hi")

		if got := contentDeltas(t, events); !reflect.DeepEqual(got, []string{"Hell", "o"}) {
			t.Errorf("content deltas = %q, want the leading deltas before the error", got)
		}
		errorEvent := findEvent(t, events, `"error"`)
		if want := `"error":{"code":502,"message":"upstream gone"}`; !strings.Contains(errorEvent, want) {
			t.Errorf("error event = %s, want it to carry %s", errorEvent, want)
		}
		for _, event := range events {
			if strings.Contains(event, "finish_reason") {
				t.Errorf("events = %q, want no finish_reason after an in-band error", events)
			}
		}
	})

	t.Run("through the provider client", func(t *testing.T) {
		var fault provider.Delta
		client := provider.NewClient(server.URL, server.Model)
		for delta := range client.Stream(t.Context(), provider.Request{
			Messages: []provider.Message{{Role: "user", Content: "hi"}},
		}) {
			if delta.Kind == provider.DeltaError {
				fault = delta
			}
		}
		if fault.Kind != provider.DeltaError {
			t.Fatal("no error delta, want the in-band object faulted")
		}
		if !fault.Retryable || !strings.Contains(fault.Err, "502") {
			t.Errorf("fault = %+v, want a retryable in-band 502", fault)
		}
	})

	t.Run("non-streamed", func(t *testing.T) {
		got := post(t, server, `{"model":"stub-model","messages":[{"role":"user","content":"hi"}]}`)

		if reply := decodeWhole(t, got); reply.Error == nil || reply.Error.Code != 502 || reply.Error.Message != "upstream gone" {
			t.Errorf("whole reply = %s, want the in-band error member", got.body)
		}
	})
}

// TestServerWholeReplyCarriesEverythingTheStreamDoes pins the non-streamed path, which the
// title and probe calls use.
func TestServerWholeReplyCarriesEverythingTheStreamDoes(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{
		Text:      "done",
		Reasoning: "thinking",
		Usage:     &Usage{Prompt: 7, Completion: 1},
	}}})

	got, err := provider.NewClient(server.URL, server.Model).Respond(t.Context(), provider.Request{
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if got.Content != "done" || got.Thinking != "thinking" || got.FinishReason != "stop" {
		t.Errorf("response = %+v, want the scripted content, reasoning and stop", got)
	}
	if got.Usage != (provider.Usage{PromptTokens: 7, CompletionTokens: 1, TotalTokens: 8}) {
		t.Errorf("usage = %+v, want {7 1 8}", got.Usage)
	}
}

// TestServerLogsWhatWasAsked pins the request log — the stub's half of an assertion about what
// the agent actually sent.
func TestServerLogsWhatWasAsked(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{Text: "ok"}}})

	body := `{"model":"stub-model","stream":true,"messages":[{"role":"user","content":"what is up"}],` +
		`"tools":[{"type":"function","function":{"name":"read_file"}}]}`
	post(t, server, body)

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests = %+v, want one", requests)
	}
	got := requests[0]
	if got.N != 1 || got.Model != "stub-model" || !got.Stream || got.TurnIndex != 0 {
		t.Errorf("request = %+v, want request 1 answered by turn 0 on the streamed path", got)
	}
	if !reflect.DeepEqual(got.Tools, []string{"read_file"}) {
		t.Errorf("tools = %v, want [read_file]", got.Tools)
	}
	if server.LastMessage(1) != "what is up" {
		t.Errorf("LastMessage(1) = %q, want %q", server.LastMessage(1), "what is up")
	}
}

// TestServerAPIKeyGateRefusesAnUnauthenticatedRequest pins WithAPIKey, the option that lets a
// test prove apogee sends the key it was configured with.
func TestServerAPIKeyGateRefusesAnUnauthenticatedRequest(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{Text: "ok", Repeat: true}}}, WithAPIKey("s3cret"))
	body := `{"model":"stub-model","messages":[{"role":"user","content":"hi"}]}`

	if got := post(t, server, body).status; got != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d, want 401", got)
	}

	request := newRequest(t, t.Context(), server, body)
	request.Header.Set("Authorization", "Bearer s3cret")
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("authenticated request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("authenticated status = %d, want 200", resp.StatusCode)
	}
}

// TestServerAdvertisesTheScriptedModel pins the discovery endpoint apogee probes at startup.
func TestServerAdvertisesTheScriptedModel(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{Text: "ok"}}})

	resp, err := http.Get(server.URL + "/v1/models")
	if err != nil {
		t.Fatalf("get models: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var models modelsReply
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	if len(models.Data) != 1 || models.Data[0].ID != "stub-model" {
		t.Errorf("models = %+v, want the one scripted model", models.Data)
	}
}

// TestMatcherReportsUnservedTurns pins what AssertConsumed reports on: a script whose later
// turns never played means the run stopped early.
func TestMatcherReportsUnservedTurns(t *testing.T) {
	t.Parallel()

	m, err := newMatcher(Script{Turns: []Turn{{Text: "a"}, {Text: "b", Repeat: true}, {Text: "c"}}})
	if err != nil {
		t.Fatalf("new matcher: %v", err)
	}

	m.next(Request{})

	if got := m.unserved(); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("unserved = %v, want [2] (turn 1 repeats, so it is never spent)", got)
	}
}

// TestServerCaptureSubstitutesTheAnnouncedPath pins the point of captures: the path in the tool
// call is the one THIS request announced, so a fixture drives the agent to what apogee told it
// rather than to a path the fixture guessed and would keep passing after the announcement moved.
func TestServerCaptureSubstitutesTheAnnouncedPath(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{
		Captures:  []Capture{{Name: "scratch", From: captureFromSystem, Pattern: `scratch directory[^\n]*?(/\S+)`}},
		ToolCalls: []ToolCall{{Name: "terminal", Arguments: `{"command":"mkdir -p {{scratch}}/tmp && echo ok"}`}},
	}}})

	got := scratchCall(t, server, "/tmp/x/scratch/abc")

	want := `{"command":"mkdir -p /tmp/x/scratch/abc/tmp && echo ok"}`
	if got != want {
		t.Errorf("arguments = %q, want %q", got, want)
	}
}

// TestServerRepeatingCaptureTurnExpandsPerRequest pins that expansion never writes back into the
// Script: a repeating turn answers many requests, and each must be answered with its own
// request's path.
func TestServerRepeatingCaptureTurnExpandsPerRequest(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{
		Repeat:    true,
		Captures:  []Capture{{Name: "scratch", From: captureFromSystem, Pattern: `scratch directory: (\S+)`}},
		ToolCalls: []ToolCall{{Name: "terminal", Arguments: `{"command":"ls {{scratch}}"}`}},
	}}})

	first := scratchCall(t, server, "/tmp/one")
	second := scratchCall(t, server, "/tmp/two")

	if first != `{"command":"ls /tmp/one"}` || second != `{"command":"ls /tmp/two"}` {
		t.Errorf("arguments = %q then %q, want each request's own scratch dir", first, second)
	}
}

// TestServerUnmatchedCaptureFailsWithoutSpendingTheTurn pins the strict half of captures. A
// request that never carried the announcement is a 500 naming the capture — never a reply with
// the placeholder silently rendered as nothing — and the turn survives it, so the run that
// arrives with the announcement still finds the script where it was.
func TestServerUnmatchedCaptureFailsWithoutSpendingTheTurn(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{
		Captures: []Capture{{Name: "scratch", From: captureFromSystem, Pattern: `scratch directory: (\S+)`}},
		Text:     "using {{scratch}}",
	}}})

	missing := post(t, server, `{"model":"stub-model","messages":[{"role":"user","content":"no orientation here"}]}`)

	if missing.status != http.StatusInternalServerError {
		t.Errorf("status = %d body = %q, want 500", missing.status, missing.body)
	}
	if !strings.Contains(missing.body, "stubllm: capture scratch unmatched for request 1") {
		t.Errorf("body = %q, want it to name the capture and the request", missing.body)
	}
	unmatched := server.Unmatched()
	if len(unmatched) != 1 || unmatched[0].N != 1 || unmatched[0].TurnIndex != -1 {
		t.Errorf("unmatched log = %+v, want one entry for request 1 with no turn", unmatched)
	}

	announced := `{"model":"stub-model","messages":[{"role":"system","content":"scratch directory: /tmp/s"}]}`
	if got := decodeWhole(t, post(t, server, announced)).Choices[0].Message.Content; got != "using /tmp/s" {
		t.Errorf("text = %q, want the unspent turn expanded against the second request", got)
	}
	server.AssertConsumed(t)
}

// TestTurnExpandReadsWhatTheRequestCarried is the table over the one function captures are
// evaluated by: which text each `from:` reads, where the values land, and how a miss reads.
func TestTurnExpandReadsWhatTheRequestCarried(t *testing.T) {
	t.Parallel()

	orientation := Message{Role: "system", Content: "workspace: /ws\nscratch directory: /home/.apogee/scratch/s1"}
	header := Message{Role: "user", Content: "files: /home/.apogee/skills/announced — this skill's bundled files"}
	skilldir := Capture{Name: "skilldir", From: captureFromLastMessage, Pattern: `files: (\S+) — this skill`}
	scratch := Capture{Name: "scratch", From: captureFromSystem, Pattern: `scratch directory: (\S+)`}

	for _, tc := range []struct {
		name          string
		turn          Turn
		messages      []Message
		wantText      string
		wantArguments string
		wantErr       string
	}{
		{
			name:     "a turn without captures is its own answer",
			turn:     Turn{Text: "plain {reply}"},
			messages: []Message{header},
			wantText: "plain {reply}",
		},
		{
			name:          "the system prompt reaches a tool call's arguments",
			turn:          Turn{Captures: []Capture{scratch}, ToolCalls: []ToolCall{{Name: "terminal", Arguments: `{"command":"ls {{scratch}}/tmp"}`}}},
			messages:      []Message{orientation, header},
			wantArguments: `{"command":"ls /home/.apogee/scratch/s1/tmp"}`,
		},
		{
			name:     "the last message reaches the text",
			turn:     Turn{Captures: []Capture{skilldir}, Text: "reading {{skilldir}}/prompts/a.md"},
			messages: []Message{orientation, header},
			wantText: "reading /home/.apogee/skills/announced/prompts/a.md",
		},
		{
			name:     "two captures, one of them used twice",
			turn:     Turn{Captures: []Capture{scratch, skilldir}, Text: "{{skilldir}} -> {{scratch}}; then {{scratch}}"},
			messages: []Message{orientation, header},
			wantText: "/home/.apogee/skills/announced -> /home/.apogee/scratch/s1; then /home/.apogee/scratch/s1",
		},
		{
			name: "a system capture does not read the user's message",
			turn: Turn{
				Captures: []Capture{{Name: "skilldir", From: captureFromSystem, Pattern: skilldir.Pattern}},
				Text:     "{{skilldir}}",
			},
			messages: []Message{orientation, header},
			wantErr:  "capture skilldir unmatched",
		},
		{
			name:     "nothing announced at all",
			turn:     Turn{Captures: []Capture{scratch}, Text: "{{scratch}}"},
			messages: []Message{header},
			wantErr:  "capture scratch unmatched",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := tc.turn.expand(Request{Messages: tc.messages})

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("expand: %v", err)
			}
			if got.Text != tc.wantText {
				t.Errorf("text = %q, want %q", got.Text, tc.wantText)
			}
			if tc.wantArguments != "" && got.ToolCalls[0].Arguments != tc.wantArguments {
				t.Errorf("arguments = %q, want %q", got.ToolCalls[0].Arguments, tc.wantArguments)
			}
		})
	}
}

// --- helpers ---

// reply is the part of an HTTP answer the assertions here read.
type reply struct {
	status      int
	body        string
	contentType string
	location    string
}

// newRequest builds a chat-completions POST against server.
func newRequest(t *testing.T, ctx context.Context, server *Server, body string) *http.Request {
	t.Helper()
	return newRequestAt(t, ctx, server, "/v1/chat/completions", body)
}

// newRequestAt builds a JSON POST against one of server's routes.
func newRequestAt(t *testing.T, ctx context.Context, server *Server, path, body string) *http.Request {
	t.Helper()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	return request
}

// post sends one chat-completions request with the default client and reads the whole reply.
func post(t *testing.T, server *Server, body string) reply {
	t.Helper()
	return postWith(t, http.DefaultClient, server, body)
}

// postWith is post with a caller-supplied client, for the redirect case.
func postWith(t *testing.T, client *http.Client, server *Server, body string) reply {
	t.Helper()

	resp, err := client.Do(newRequest(t, t.Context(), server, body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := readAll(resp)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	return reply{
		status:      resp.StatusCode,
		body:        raw,
		contentType: resp.Header.Get("Content-Type"),
		location:    resp.Header.Get("Location"),
	}
}

// readStreamRaw sends one request and drains the raw body with a fresh client, returning what
// arrived and the read error — the shape a killed connection is judged on. The client is fresh
// so a dropped connection can never be one the default client's pool hands to another test.
func readStreamRaw(t *testing.T, server *Server, body string) (string, error) {
	t.Helper()

	client := &http.Client{Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	resp, err := client.Do(newRequest(t, t.Context(), server, body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 before the kill", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	return string(raw), err
}

// readAll drains a response body into a string.
func readAll(resp *http.Response) (string, error) {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// postStream sends one streaming request and returns the raw `data:` payloads, in order.
func postStream(t *testing.T, server *Server, prompt string) []string {
	t.Helper()

	body := fmt.Sprintf(`{"model":%q,"stream":true,"messages":[{"role":"user","content":%q}]}`, server.Model, prompt)
	resp, err := http.DefaultClient.Do(newRequest(t, t.Context(), server, body))
	if err != nil {
		t.Fatalf("post stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if contentType := resp.Header.Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("content type = %q, want text/event-stream", contentType)
	}

	var events []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if payload, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
			events = append(events, payload)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if len(events) == 0 || events[len(events)-1] != "[DONE]" {
		t.Fatalf("stream = %q, want it to end with [DONE]", events)
	}
	return events
}

// contentDeltas is the content text of every streamed event, in order.
func contentDeltas(t *testing.T, events []string) []string {
	t.Helper()

	var out []string
	for _, event := range events {
		if event == "[DONE]" {
			continue
		}
		var envelope sseEnvelope
		if err := json.Unmarshal([]byte(event), &envelope); err != nil {
			t.Fatalf("decode event %q: %v", event, err)
		}
		if len(envelope.Choices) > 0 && envelope.Choices[0].Delta.Content != "" {
			out = append(out, envelope.Choices[0].Delta.Content)
		}
	}
	return out
}

// findEvent returns the first raw event containing marker.
func findEvent(t *testing.T, events []string, marker string) string {
	t.Helper()

	for _, event := range events {
		if strings.Contains(event, marker) {
			return event
		}
	}
	t.Fatalf("no event containing %q in %q", marker, events)
	return ""
}

// streamThroughProvider drives the stub with the real provider client and collects the deltas.
func streamThroughProvider(t *testing.T, server *Server, prompt string) []provider.Delta {
	t.Helper()

	var deltas []provider.Delta
	client := provider.NewClient(server.URL, server.Model)
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

// terminal returns the stream's Done delta.
func terminal(t *testing.T, deltas []provider.Delta) provider.Delta {
	t.Helper()

	for _, delta := range deltas {
		if delta.Kind == provider.DeltaDone {
			return delta
		}
	}
	t.Fatalf("no terminal delta in %+v", deltas)
	return provider.Delta{}
}

// wholeText sends a non-streamed request and returns the reply's content.
func wholeText(t *testing.T, server *Server, prompt string) string {
	t.Helper()

	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":%q}]}`, server.Model, prompt)
	return decodeWhole(t, post(t, server, body)).Choices[0].Message.Content
}

// decodeWhole decodes a non-streamed completion.
func decodeWhole(t *testing.T, got reply) wholeReply {
	t.Helper()

	if got.status != http.StatusOK {
		t.Fatalf("status = %d body = %q, want 200", got.status, got.body)
	}
	var out wholeReply
	if err := json.Unmarshal([]byte(got.body), &out); err != nil {
		t.Fatalf("decode reply %q: %v", got.body, err)
	}
	if len(out.Choices) == 0 {
		t.Fatalf("reply %q carries no choices", got.body)
	}
	return out
}

// scratchCall sends a request whose system prompt announces scratch the way apogee's
// orientation does, and returns the arguments of the tool call the stub answered with.
func scratchCall(t *testing.T, server *Server, scratch string) string {
	t.Helper()

	body := fmt.Sprintf(
		`{"model":%q,"messages":[{"role":"system","content":"scratch directory: %s"}]}`,
		server.Model, scratch,
	)
	return decodeWhole(t, post(t, server, body)).Choices[0].Message.ToolCalls[0].Function.Arguments
}

// TestServerWithoutRequestLogStillNumbersRequests pins that switching the log off drops only
// the stored entries: request numbering and turn consumption are the stub's own state and must
// keep running, or the 500 body would name the wrong request.
func TestServerWithoutRequestLogStillNumbersRequests(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{Text: "only"}}}, WithRequestLog(false))
	body := `{"model":"stub-model","messages":[{"role":"user","content":"hi"}]}`

	post(t, server, body)
	second := post(t, server, body)

	if len(server.Requests()) != 0 {
		t.Errorf("requests = %+v, want none logged", server.Requests())
	}
	if !strings.Contains(second.body, "no turn for request 2") {
		t.Errorf("second reply = %q, want it to name request 2", second.body)
	}
}

// TestServerWithLatencyDelaysTheFirstByte pins the time-to-first-token knob a spinner or a
// cancel path needs to be observable at all.
func TestServerWithLatencyDelaysTheFirstByte(t *testing.T) {
	t.Parallel()

	const latency = 60 * time.Millisecond
	server := New(t, Script{Model: "stub-model", Turns: []Turn{{Text: "ok"}}}, WithLatency(latency))

	started := time.Now()
	_ = wholeText(t, server, "hi")

	if elapsed := time.Since(started); elapsed < latency {
		t.Errorf("reply arrived after %s, want at least %s", elapsed, latency)
	}
}

// TestServeListensAndStopsWithItsContext pins the binary's entry point: the same handler on a
// real listener, whose lifetime is the context's.
func TestServeListensAndStopsWithItsContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	server, err := Serve(ctx, "127.0.0.1:0", Script{Model: "stub-model", Turns: []Turn{{Text: "ok"}}})
	if err != nil {
		t.Fatalf("serve: %v", err)
	}

	if got := wholeText(t, server, "hi"); got != "ok" {
		t.Errorf("reply = %q, want ok", got)
	}

	cancel()
	waitUntil(t, func() bool {
		_, err := http.Get(server.URL + "/v1/models")
		return err != nil
	})
}

// waitUntil polls done for up to two seconds, failing t when it never becomes true.
func waitUntil(t *testing.T, done func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition never held within two seconds")
}

// TestServerAwaitHoldsTheReplyUntilReleased pins the whole of the `await:` gate: the request is
// matched and logged the moment it arrives, its ANSWER waits for [Server.Release], a Release that
// ran before the request did is already open when it gets there, and a gate nobody opens refuses
// the request rather than hanging.
//
// The subtests are deliberately NOT parallel with each other: the refusal case moves awaitLimit,
// which the other two read.
func TestServerAwaitHoldsTheReplyUntilReleased(t *testing.T) {
	t.Parallel()

	body := `{"model":"stub-model","messages":[{"role":"user","content":"hi"}]}`

	t.Run("held until released", func(t *testing.T) {
		server := New(t, Script{Model: "stub-model", Turns: []Turn{{Await: "go", Text: "done"}}})
		request := newRequest(t, t.Context(), server, body)

		answered := make(chan string, 1)
		failed := make(chan error, 1)
		go func() {
			resp, err := http.DefaultClient.Do(request)
			if err != nil {
				failed <- err
				return
			}
			defer func() { _ = resp.Body.Close() }()
			raw, err := readAll(resp)
			if err != nil {
				failed <- err
				return
			}
			answered <- raw
		}()

		// The request reaches the log while its answer is still waiting — the gate holds the
		// reply, not the request, which is what lets a test assert on what was ASKED mid-hold.
		waitUntil(t, func() bool { return len(server.Requests()) == 1 })
		select {
		case raw := <-answered:
			t.Fatalf("the reply arrived before the gate opened: %s", raw)
		case err := <-failed:
			t.Fatalf("post: %v", err)
		case <-time.After(50 * time.Millisecond):
		}

		server.Release("go")
		select {
		case raw := <-answered:
			if !strings.Contains(raw, "done") {
				t.Errorf("reply = %s, want the scripted text once the gate opened", raw)
			}
		case err := <-failed:
			t.Fatalf("post: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("the reply never came after Release")
		}
	})

	t.Run("released before the request arrives", func(t *testing.T) {
		server := New(t, Script{Model: "stub-model", Turns: []Turn{{Await: "go", Text: "done"}}})
		server.Release("go")
		server.Release("go")

		if got := post(t, server, body); !strings.Contains(got.body, "done") {
			t.Errorf("reply = %s, want an already-open gate to answer at once", got.body)
		}
	})

	t.Run("nothing releases it", func(t *testing.T) {
		previous := awaitLimit
		awaitLimit = 20 * time.Millisecond
		t.Cleanup(func() { awaitLimit = previous })

		server := New(t, Script{Model: "stub-model", Turns: []Turn{{Await: "nobody opens this", Text: "done"}}})

		got := post(t, server, body)
		if got.status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 for a gate nothing releases", got.status)
		}
		if !strings.Contains(got.body, "nobody opens this") {
			t.Errorf("body = %q, want it to name the gate that was never released", got.body)
		}
	})
}

// TestServerWritesTheScriptedSpellingOfTheThinkingChannel pins `reasoning_field` at the BYTES on
// both paths. Bytes rather than a decoded struct is the whole point: what this knob exists for is
// reproducing the wire an Ollama or OpenRouter server writes, so a test that decoded the reply
// through a client that already reads both spellings would pass whichever key had been emitted.
//
// The delta is matched WHOLE — `"delta":{"reasoning_content":"thinking"}` — because that also pins
// the exclusivity the emitters promise: the other spelling is `omitempty` and absent, not present
// and empty, so an unset key streams the bytes it streamed before this knob existed.
func TestServerWritesTheScriptedSpellingOfTheThinkingChannel(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		field  string
		delta  string
		absent string
	}{
		{
			name:   "the default spelling, as llama.cpp and vLLM write it",
			field:  "",
			delta:  `"delta":{"reasoning_content":"thinking"}`,
			absent: `"reasoning":`,
		},
		{
			name:   "the bare spelling, as Ollama and OpenRouter write it",
			field:  "reasoning",
			delta:  `"delta":{"reasoning":"thinking"}`,
			absent: `"reasoning_content":`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// One chunk, so the whole channel is in one delta and the assertion is over the
			// key rather than over the chunking.
			server := New(t, Script{Model: "stub-model", Turns: []Turn{{
				Text:           "done",
				Reasoning:      "thinking",
				ReasoningField: tc.field,
				ChunkRunes:     32,
				Repeat:         true,
			}}})

			stream := strings.Join(postStream(t, server, "hi"), "\n")
			if !strings.Contains(stream, tc.delta) {
				t.Errorf("the stream carries no %s:\n%s", tc.delta, stream)
			}
			if strings.Contains(stream, tc.absent) {
				t.Errorf("the stream carries %s, and a server writes one spelling:\n%s", tc.absent, stream)
			}

			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}]}`, server.Model)
			whole := post(t, server, body).body
			if want := strings.TrimPrefix(tc.delta, `"delta":{`); !strings.Contains(whole, strings.TrimSuffix(want, "}")) {
				t.Errorf("the whole reply carries no %s:\n%s", want, whole)
			}
			if strings.Contains(whole, tc.absent) {
				t.Errorf("the whole reply carries %s, and a server writes one spelling:\n%s", tc.absent, whole)
			}
		})
	}
}

// --- the Messages route ---

// TestMessagesRouteRunsAScriptedToolLoop pins that the same Script answers on /v1/messages: a
// hand-built Messages request is reduced to the neutral log — the top-level system visible as
// a role-system message, the effort dial on Effort.OutputEffort — the tool_use id the stub
// issued rounds back in as the tool_result that a `when.tool_result` turn matches on, and every
// finish reason reaches the wire as its Messages stop_reason.
func TestMessagesRouteRunsAScriptedToolLoop(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{
		{ToolCalls: []ToolCall{{Name: "list_dir", Arguments: `{"path":"."}`}}},
		{When: &Match{ToolResult: "list_dir"}, Text: "There are three files."},
		{Text: "cut short", FinishReason: "length"},
	}})
	tools := `"tools":[{"name":"list_dir","description":"list","input_schema":{"type":"object"}}]`

	first := postMessages(t, server, `{"model":"stub-model","max_tokens":64,"system":"You are apogee.",`+
		`"messages":[{"role":"user","content":"look around"}],`+tools+`,"output_config":{"effort":"high"}}`)
	if first.status != http.StatusOK || first.contentType != "application/json" {
		t.Fatalf("first reply = %d %s %s, want a 200 JSON message", first.status, first.contentType, first.body)
	}
	call := decodeMessage(t, first.body)
	if len(call.Content) != 1 || call.Content[0].Type != "tool_use" || call.Content[0].ID != "call_1" ||
		call.Content[0].Name != "list_dir" || string(call.Content[0].Input) != `{"path":"."}` {
		t.Errorf("content = %s, want one tool_use block call_1 list_dir {\"path\":\".\"}", first.body)
	}
	if call.StopReason == nil || *call.StopReason != "tool_use" {
		t.Errorf("stop_reason = %v, want tool_use", call.StopReason)
	}

	second := postMessages(t, server, `{"model":"stub-model","max_tokens":64,"system":"You are apogee.","messages":[`+
		`{"role":"user","content":[{"type":"text","text":"look around"}]},`+
		`{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"list_dir","input":{"path":"."}}]},`+
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"a\nb\nc"}]}],`+tools+`}`)
	text := decodeMessage(t, second.body)
	if len(text.Content) != 1 || text.Content[0].Type != "text" || text.Content[0].text() != "There are three files." {
		t.Errorf("content = %s, want one text block with the matched turn's text", second.body)
	}
	if text.StopReason == nil || *text.StopReason != "end_turn" {
		t.Errorf("stop_reason = %v, want end_turn", text.StopReason)
	}

	third := decodeMessage(t, postMessages(t, server, `{"model":"stub-model","max_tokens":1,"messages":[{"role":"user","content":"go on"}]}`).body)
	if third.StopReason == nil || *third.StopReason != "max_tokens" {
		t.Errorf("stop_reason = %v, want max_tokens for a length finish", third.StopReason)
	}

	requests := server.Requests()
	if len(requests) != 3 {
		t.Fatalf("requests = %+v, want three", requests)
	}
	got := requests[0]
	if got.Wire != WireAnthropic || got.Model != "stub-model" || got.Stream || got.TurnIndex != 0 {
		t.Errorf("request 1 = %+v, want the anthropic wire, whole path, answered by turn 0", got)
	}
	wantMessages := []Message{{Role: "system", Content: "You are apogee."}, {Role: "user", Content: "look around"}}
	if !reflect.DeepEqual(got.Messages, wantMessages) {
		t.Errorf("request 1 messages = %+v, want %+v", got.Messages, wantMessages)
	}
	if !reflect.DeepEqual(got.Tools, []string{"list_dir"}) || got.Effort.OutputEffort != "high" {
		t.Errorf("request 1 tools = %v effort = %+v, want [list_dir] and output effort high", got.Tools, got.Effort)
	}
	if got.Sampling.MaxTokens == nil || *got.Sampling.MaxTokens != 64 {
		t.Errorf("request 1 max_tokens = %v, want 64", got.Sampling.MaxTokens)
	}
	wantLoop := []Message{
		{Role: "system", Content: "You are apogee."},
		{Role: "user", Content: "look around"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "list_dir", Arguments: `{"path":"."}`}}},
		{Role: "tool", ToolCallID: "call_1", Content: "a\nb\nc"},
	}
	if !reflect.DeepEqual(requests[1].Messages, wantLoop) || requests[1].TurnIndex != 1 {
		t.Errorf("request 2 = %+v, want the folded tool loop answered by turn 1", requests[1])
	}
	server.AssertConsumed(t)
}

// TestMessagesRouteStreamsBlockEvents pins the streamed shape of the Messages wire, first on
// the raw events — the block runs in order, each payload typed on its `event:` line, the text
// deltas at the scripted boundaries, the tool input in two fragments, the usage split across
// message_start and message_delta with the cached share outside input_tokens — and then as the
// real provider client on the anthropic wire reassembles it.
func TestMessagesRouteStreamsBlockEvents(t *testing.T) {
	t.Parallel()

	script := Script{Model: "stub-model", Turns: []Turn{{
		Reasoning: "hmm",
		Chunks:    []string{"Hel", "lo"},
		ToolCalls: []ToolCall{{Name: "read_file", Arguments: `{"path":"a.go"}`}},
		Usage:     &Usage{Prompt: 10, Completion: 4, Cached: 3},
		Repeat:    true,
	}}}
	server := New(t, script)

	events := postMessagesStream(t, server, `{"model":"stub-model","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"read it"}]}`)

	var types []string
	var texts, inputs []string
	for _, event := range events {
		types = append(types, event.Type)
		if event.Delta != nil {
			texts = append(texts, event.Delta.Text)
			inputs = append(inputs, event.Delta.PartialJSON)
		}
	}
	wantTypes := []string{
		"message_start",
		"content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	}
	if !reflect.DeepEqual(types, wantTypes) {
		t.Fatalf("event types = %v, want %v", types, wantTypes)
	}
	if got := strings.Join(texts, "|"); got != "|Hel|lo|||" {
		t.Errorf("text deltas = %q, want the scripted chunks Hel then lo", got)
	}
	if got := strings.Join(inputs, ""); got != `{"path":"a.go"}` {
		t.Errorf("input_json_delta fragments = %q, want them to concatenate to the arguments", got)
	}
	start, closing := events[0], events[len(events)-2]
	if start.Message == nil || start.Message.Usage == nil || start.Message.Usage.InputTokens == nil ||
		*start.Message.Usage.InputTokens != 7 || *start.Message.Usage.CacheReadTokens != 3 {
		t.Errorf("message_start = %+v, want input_tokens 7 beside cache_read_input_tokens 3", start.Message)
	}
	if closing.Delta == nil || closing.Delta.StopReason != "tool_use" || closing.Usage == nil ||
		closing.Usage.OutputTokens == nil || *closing.Usage.OutputTokens != 4 {
		t.Errorf("message_delta = %+v, want stop_reason tool_use and output_tokens 4", closing)
	}
	if events[1].ContentBlock == nil || events[1].ContentBlock.Type != "thinking" ||
		events[8].ContentBlock == nil || events[8].ContentBlock.ID != "call_1" || events[8].ContentBlock.Name != "read_file" {
		t.Errorf("block starts = %+v / %+v, want a thinking block first and the id'd tool_use block last", events[1].ContentBlock, events[8].ContentBlock)
	}

	var content, thinking string
	var calls []provider.ToolCall
	client := provider.NewClient(server.URL, server.Model, provider.WithWire(provider.WireAnthropic))
	var done provider.Delta
	for delta := range client.Stream(t.Context(), provider.Request{Messages: []provider.Message{{Role: "user", Content: "read it"}}}) {
		switch delta.Kind {
		case provider.DeltaError:
			t.Fatalf("stream error: %s", delta.Err)
		case provider.DeltaContent:
			content += delta.Content
		case provider.DeltaThinking:
			thinking += delta.Thinking
		case provider.DeltaToolCall:
			calls = append(calls, *delta.ToolCall)
		case provider.DeltaDone:
			done = delta
		}
	}
	if content != "Hello" || thinking != "hmm" {
		t.Errorf("content = %q thinking = %q, want Hello and hmm", content, thinking)
	}
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Function.Name != "read_file" || calls[0].Function.Arguments != `{"path":"a.go"}` {
		t.Errorf("tool calls = %+v, want call_1 read_file with whole arguments", calls)
	}
	if done.FinishReason != "tool_calls" || done.Usage == nil || done.Usage.PromptTokens != 10 || done.Usage.CachedPromptTokens != 3 || done.Usage.CompletionTokens != 4 {
		t.Errorf("done = %+v, want tool_calls with the scripted usage (prompt 10, cached 3, completion 4)", done)
	}
}

// TestMessagesRouteRendersTheErrorBodyAndReadsXAPIKey pins the two remaining wire facts: an
// `error` turn is the API's error body — on the streamed path an `error` event with no
// message_stop after it, on the whole path the body itself — with the default 502 rendered as
// the overloaded class, and the x-api-key header opens the api-key gate.
func TestMessagesRouteRendersTheErrorBodyAndReadsXAPIKey(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{
		{Text: "partial", Error: &InBandError{Message: "upstream fell over"}, Repeat: true},
	}}, WithAPIKey("s3cret"))
	body := `{"model":"stub-model","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`

	if got := postMessages(t, server, body).status; got != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d, want 401", got)
	}

	request := newRequestAt(t, t.Context(), server, "/v1/messages", body)
	request.Header.Set("x-api-key", "s3cret")
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("authenticated request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := readAll(resp)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	var whole anthropicErrorBody
	if err := json.Unmarshal([]byte(raw), &whole); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	if resp.StatusCode != http.StatusOK || whole.Type != "error" || whole.Error.Type != "overloaded_error" || whole.Error.Message != "upstream fell over" {
		t.Errorf("whole reply = %d %s, want a 200 error body of the overloaded class", resp.StatusCode, raw)
	}

	stream := newRequestAt(t, t.Context(), server, "/v1/messages", `{"model":"stub-model","max_tokens":8,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	stream.Header.Set("x-api-key", "s3cret")
	events := readMessagesEvents(t, stream)
	last := events[len(events)-1]
	if last.Type != "error" || last.Error == nil || last.Error.Type != "overloaded_error" {
		t.Errorf("last event = %+v, want the error event with no message_stop after it", last)
	}
	if got := events[len(events)-2].Type; got != "content_block_stop" {
		t.Errorf("event before the error = %q, want the text block to have streamed first", got)
	}
}

// postMessages sends one non-streamed Messages request and reads the whole reply.
func postMessages(t *testing.T, server *Server, body string) reply {
	t.Helper()

	resp, err := http.DefaultClient.Do(newRequestAt(t, t.Context(), server, "/v1/messages", body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := readAll(resp)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	return reply{status: resp.StatusCode, body: raw, contentType: resp.Header.Get("Content-Type")}
}

// decodeMessage decodes a whole Messages reply.
func decodeMessage(t *testing.T, body string) anthropicReply {
	t.Helper()

	var message anthropicReply
	if err := json.Unmarshal([]byte(body), &message); err != nil {
		t.Fatalf("decode message %q: %v", body, err)
	}
	return message
}

// postMessagesStream sends one streaming Messages request and returns its decoded events.
func postMessagesStream(t *testing.T, server *Server, body string) []anthropicEvent {
	t.Helper()
	return readMessagesEvents(t, newRequestAt(t, t.Context(), server, "/v1/messages", body))
}

// readMessagesEvents sends a streaming request and decodes every event, checking that each
// `event:` line names the type its `data:` payload carries.
func readMessagesEvents(t *testing.T, request *http.Request) []anthropicEvent {
	t.Helper()

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if contentType := resp.Header.Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("content type = %q, want text/event-stream", contentType)
	}

	var events []anthropicEvent
	var named string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if name, ok := strings.CutPrefix(line, "event: "); ok {
			named = name
			continue
		}
		payload, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var event anthropicEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatalf("decode event %q: %v", payload, err)
		}
		if event.Type != named {
			t.Errorf("event line %q does not name the payload type %q", named, event.Type)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("stream carried no events")
	}
	return events
}

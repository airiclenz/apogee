package agent

// Coverage for UpstreamAttemptEvent (ADR 0085): every HTTP attempt a model call makes over a Client
// stamped with its server's identity reaches the EventSink as an UpstreamAttemptEvent — from a
// Turn's reply, from a delegated child's at its own depth, and from compaction's summary call —
// while the attempt measurement itself touches nothing the collector folds: the conversation, the
// transcript events and the usage accounting are exactly what an unstamped run produces.

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// attemptServer and attemptEndpoint are the identity the stamped responders below carry; the
// endpoint holds userinfo and a query so the event is seen carrying the REDACTED form.
const (
	attemptServer   = "bench-box"
	attemptEndpoint = "http://user:secret@stubllm/v1?key=x"
	redactedAttempt = "http://stubllm/v1"
)

// stampedResponder is scriptResponder's real provider client, stamped with a server identity so
// its streams yield one DeltaAttempt per HTTP attempt — the shape every Client dialOptions builds.
func stampedResponder(t testing.TB, turns ...stubllm.Turn) *scriptedUpstream {
	t.Helper()
	server := stubllm.InProcess(t, stubllm.Script{Model: testModel, Turns: turns})
	client := provider.NewClient("http://stubllm", testModel,
		provider.WithHTTPClient(&http.Client{Transport: server.Transport()}),
		provider.WithMaxRetries(0),
		provider.WithServerIdentity(attemptServer, attemptEndpoint),
	)
	return &scriptedUpstream{Client: client, server: server}
}

// attemptEvents returns every UpstreamAttemptEvent in events, in emission order.
func attemptEvents(events []domain.Event) []domain.UpstreamAttemptEvent {
	var out []domain.UpstreamAttemptEvent
	for _, e := range events {
		if at, ok := e.(domain.UpstreamAttemptEvent); ok {
			out = append(out, at)
		}
	}
	return out
}

// withoutAttempts returns events with every UpstreamAttemptEvent removed.
func withoutAttempts(events []domain.Event) []domain.Event {
	var out []domain.Event
	for _, e := range events {
		if _, ok := e.(domain.UpstreamAttemptEvent); !ok {
			out = append(out, e)
		}
	}
	return out
}

// indexOf returns the position of the first event in events that match accepts, or -1.
func indexOf(events []domain.Event, match func(domain.Event) bool) int {
	for i, e := range events {
		if match(e) {
			return i
		}
	}
	return -1
}

// TestUpstreamAttemptEventIsEmittedForATurnsCall: a Turn's reply over a stamped client emits one
// UpstreamAttemptEvent, carrying the Turn and depth of the Agent that made the call, the stamped
// identity with its endpoint redacted, and the attempt's measurement — after the streamed content
// and before the MessageEvent that ends the Turn.
func TestUpstreamAttemptEventIsEmittedForATurnsCall(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	up := stampedResponder(t, stubllm.Turn{Text: "hello there", Usage: &stubllm.Usage{Prompt: 12, Completion: 7}})
	a, err := newAgent(baseConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "hi")

	attempts := attemptEvents(sink.events)
	if len(attempts) != 1 {
		t.Fatalf("attempt events = %d, want exactly one for the Turn's one call: %+v", len(attempts), attempts)
	}
	at := attempts[0]
	msgAt := indexOf(sink.events, func(e domain.Event) bool { _, ok := e.(domain.MessageEvent); return ok })
	if msgAt < 0 {
		t.Fatal("no MessageEvent ended the Turn")
	}
	msg := sink.events[msgAt].(domain.MessageEvent)
	if at.Depth != 0 || at.Turn != msg.Turn || at.EventBase.CallID != "" {
		t.Errorf("attempt base = %+v, want the top-level Agent's (Depth 0, Turn %d, no CallID)", at.EventBase, msg.Turn)
	}
	if at.Server != attemptServer || at.Endpoint != redactedAttempt {
		t.Errorf("attempt identity = %q @ %q, want %q @ %q (redacted)", at.Server, at.Endpoint, attemptServer, redactedAttempt)
	}
	if at.Model != testModel || at.Index != 0 || at.RequestID == "" || at.Outcome != provider.AttemptOK {
		t.Errorf("attempt = %+v, want model %q, index 0, a request id and outcome %q", at, testModel, provider.AttemptOK)
	}
	if at.OutputTokens != 7 {
		t.Errorf("attempt output tokens = %d, want the server's reported 7", at.OutputTokens)
	}
	if at.Duration <= 0 || at.TTFT > at.Duration || at.Last < at.TTFT {
		t.Errorf("attempt timeline ttft=%v last=%v duration=%v, want 0 < ttft ≤ last ≤ duration", at.TTFT, at.Last, at.Duration)
	}

	attemptAt := indexOf(sink.events, func(e domain.Event) bool { _, ok := e.(domain.UpstreamAttemptEvent); return ok })
	tokenAt := indexOf(sink.events, func(e domain.Event) bool { _, ok := e.(domain.TokenEvent); return ok })
	if tokenAt < 0 || !(tokenAt < attemptAt && attemptAt < msgAt) {
		t.Errorf("event order token@%d attempt@%d message@%d, want the attempt after the content and before the message", tokenAt, attemptAt, msgAt)
	}
}

// TestUpstreamAttemptDeltaChangesNothingItFolds: the same script over a stamped and an unstamped
// client leaves the same conversation and the same event stream once the attempt events are set
// aside — content, tool calls and usage accounting never see a DeltaAttempt.
func TestUpstreamAttemptDeltaChangesNothingItFolds(t *testing.T) {
	t.Parallel()

	turns := []stubllm.Turn{
		toolCallTurn("t1", "w", `{}`),
		{Text: "all done", Usage: &stubllm.Usage{Prompt: 30, Completion: 5}},
	}
	run := func(up provider.Responder) (*Agent, []domain.Event) {
		sink := &recordingSink{}
		a, err := newAgent(configWithTools(sink, fakeTool{name: "w", readOnly: true}), up)
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		runExchange(t, a, "use the tool")
		return a, sink.events
	}
	plain, plainEvents := run(scriptedResponder(t, turns...))
	stamped, stampedEvents := run(stampedResponder(t, turns...))

	if got := len(attemptEvents(stampedEvents)); got != 2 {
		t.Fatalf("stamped run emitted %d attempt events, want one per call (2)", got)
	}
	if got := len(attemptEvents(plainEvents)); got != 0 {
		t.Errorf("unstamped run emitted %d attempt events, want none", got)
	}
	if !reflect.DeepEqual(plain.conv.Messages(), stamped.conv.Messages()) {
		t.Errorf("conversation differs with attempt deltas:\n plain   %+v\n stamped %+v", plain.conv.Messages(), stamped.conv.Messages())
	}
	if got, want := withoutAttempts(stampedEvents), plainEvents; !reflect.DeepEqual(got, want) {
		t.Errorf("events differ once attempts are set aside:\n got  %+v\n want %+v", got, want)
	}
}

// TestUpstreamAttemptEventCarriesTheDelegationsDepth: a delegated child speaking over the parent's
// stamped client reports its own attempt at its own identity — Depth 1 and the spawning call id —
// while the parent's two calls report at depth 0.
func TestUpstreamAttemptEventCarriesTheDelegationsDepth(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	up := stampedResponder(t,
		subAgentCallTurn("c1", "summarise the repo"),
		contentTurn("the repo is a Go TUI agent"),
		contentTurn("done — delegated and summarised"),
	)
	a, err := newAgent(subAgentConfig(sink, domain.ModeAskBefore), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "please research")

	attempts := attemptEvents(sink.events)
	if len(attempts) != 3 {
		t.Fatalf("attempt events = %d, want one per call (parent, child, parent): %+v", len(attempts), attempts)
	}
	wantDepth := []int{0, 1, 0}
	wantCall := []string{"", "c1", ""}
	for i, at := range attempts {
		if at.Depth != wantDepth[i] || at.EventBase.CallID != wantCall[i] {
			t.Errorf("attempt %d base = %+v, want Depth %d CallID %q", i, at.EventBase, wantDepth[i], wantCall[i])
		}
		if at.Server != attemptServer || at.Outcome != provider.AttemptOK {
			t.Errorf("attempt %d = %+v, want server %q outcome %q", i, at, attemptServer, provider.AttemptOK)
		}
	}
}

// TestCompactionSummaryEmitsItsAttempt: compaction's summary call is silent in the transcript but
// not in the measurement — its attempt-only observer passes the DeltaAttempt on as an
// UpstreamAttemptEvent and nothing else, so no Token or Reasoning event leaks from the summary.
func TestCompactionSummaryEmitsItsAttempt(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	up := stampedResponder(t, stubllm.Turn{Text: "a summary", Usage: &stubllm.Usage{Prompt: 40, Completion: 3}})
	a, err := newAgent(baseConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "the history to fold"},
		{Role: domain.RoleAssistant, Content: "a reply in it"},
	}
	summary, err := compactCompleter{a: a}.Complete(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if summary != "a summary" {
		t.Errorf("summary = %q, want the scripted text", summary)
	}

	attempts := attemptEvents(sink.events)
	if len(attempts) != 1 {
		t.Fatalf("attempt events = %d, want the summary call's one: %+v", len(attempts), sink.events)
	}
	if at := attempts[0]; at.Server != attemptServer || at.Outcome != provider.AttemptOK || at.OutputTokens != 3 {
		t.Errorf("summary attempt = %+v, want server %q, outcome %q, 3 output tokens", at, attemptServer, provider.AttemptOK)
	}
	for _, e := range sink.events {
		switch e.(type) {
		case domain.TokenEvent, domain.ReasoningEvent, domain.MessageEvent:
			t.Errorf("the summary leaked %T into the transcript stream", e)
		}
	}
}

package agent

// The overflow seam: a context-window rejection is classified apart from a generic Upstream
// fault, because only the former says something the loop can act on (the PROMPT did not fit, so a
// shorter history is a real remedy). These tests pin the two halves of that split — the outcome
// respondAndReview reports and, crucially, the ErrorEvent it does NOT emit — plus the observable
// behaviour a caller sees, which must stay exactly what a plain fault produces until recovery is
// wired in.
//
// The transient seam (at the foot of this file) is the second fault the respond phase can act on:
// an in-band error whose CLASS the provider marked retryable, which the Turn re-streams once.

import (
	"context"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// faultResponder answers every stream with one terminal fault Delta of the given kind — the fake
// for both halves of the split (DeltaContextOverflow vs DeltaError) with everything else held
// equal, so a difference in the assertion is a difference in classification and nothing else.
type faultResponder struct {
	kind provider.DeltaKind
	msg  string
}

func (r faultResponder) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	return func(yield func(provider.Delta) bool) {
		yield(provider.Delta{Kind: r.kind, Err: r.msg})
	}
}

// overflowFaultMsg is the sanitized message the provider builds for a llama.cpp 400 whose body
// matches an overflow marker (statusDelta, internal/provider/stream.go) — the real shape of the
// text the loop must carry through the seam unchanged.
const overflowFaultMsg = "apogee: context window exceeded: " +
	`{"error":{"message":"request (57546 tokens) exceeds the available context size (32768 tokens)"}}`

// errorEvents returns every ErrorEvent among events, in order.
func errorEvents(events []domain.Event) []domain.ErrorEvent {
	var out []domain.ErrorEvent
	for _, e := range events {
		if ee, ok := e.(domain.ErrorEvent); ok {
			out = append(out, ee)
		}
	}
	return out
}

// TestRespondAndReviewSplitsOverflowFromPlainFault proves the seam: an overflowed request ends the
// respond phase as turnOverflowed and stays SILENT, carrying its message out to the caller (which
// owns the give-up event, so a recovered Turn can be quiet), while a generic fault keeps today's
// behaviour verbatim — turnFailed, one ErrorEvent from source "loop", nothing carried.
func TestRespondAndReviewSplitsOverflowFromPlainFault(t *testing.T) {
	tests := []struct {
		name        string
		kind        provider.DeltaKind
		msg         string
		wantOutcome turnOutcome
		wantCarried string
		wantEvents  int
	}{
		{
			name:        "overflow is its own outcome and surfaces nothing",
			kind:        provider.DeltaContextOverflow,
			msg:         overflowFaultMsg,
			wantOutcome: turnOverflowed,
			wantCarried: overflowFaultMsg,
			wantEvents:  0,
		},
		{
			name:        "a plain fault still fails loudly",
			kind:        provider.DeltaError,
			msg:         "apogee: upstream HTTP 500: boom",
			wantOutcome: turnFailed,
			wantCarried: "",
			wantEvents:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			a, err := newAgent(baseConfig(sink), faultResponder{kind: tc.kind, msg: tc.msg})
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			req, _ := a.buildRequest(0)
			run := &turnRun{turn: 0, req: req}

			resp, outcome, carried := a.respondAndReview(context.Background(), run)

			if outcome != tc.wantOutcome {
				t.Errorf("outcome = %v, want %v", outcome, tc.wantOutcome)
			}
			if resp != nil {
				t.Errorf("resp = %+v, want nil on a terminal fault", resp)
			}
			if carried != tc.wantCarried {
				t.Errorf("carried message = %q, want %q", carried, tc.wantCarried)
			}
			errs := errorEvents(sink.events)
			if len(errs) != tc.wantEvents {
				t.Fatalf("ErrorEvents = %d (%v), want %d", len(errs), errs, tc.wantEvents)
			}
			if tc.wantEvents == 1 {
				if errs[0].Source != "loop" || errs[0].Err != tc.msg {
					t.Errorf("ErrorEvent = {Source:%q Err:%q}, want {Source:%q Err:%q}",
						errs[0].Source, errs[0].Err, "loop", tc.msg)
				}
			}
		})
	}
}

// TestStepOverflowStillAbandonsTheTurnUnchanged pins the observable contract the seam must not
// move: where recovery cannot run — baseConfig leaves `auto-compact` off, the decision-4 opt-out —
// an overflowed request degrades the Turn exactly as before: one ErrorEvent from source "loop"
// LEADING with the provider's message, a clean Exchange-complete boundary, and no assistant
// message committed. Recovery is quiet on SUCCESS (overflowrecovery_test.go); it may never change
// what the GIVE-UP looks like, and this is the anchor for that.
//
// The one amendment (2026-08-02): baseConfig knows no window, and a session with no window wedges
// — every later message overflows identically — so the give-up appends the remedy that ends it.
// The provider's message still leads unchanged; the suffix and its window-gating are pinned by
// TestOverflowGiveUpNamesTheWindowRemedy below.
func TestStepOverflowStillAbandonsTheTurnUnchanged(t *testing.T) {
	sink := &recordingSink{}
	a, err := newAgent(baseConfig(sink), faultResponder{kind: provider.DeltaContextOverflow, msg: overflowFaultMsg})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "summarize the repository"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q — an overflow ends the Exchange at a clean boundary",
			res.Status, domain.StatusExchangeComplete)
	}
	errs := errorEvents(sink.events)
	if len(errs) != 1 {
		t.Fatalf("ErrorEvents = %d (%v), want exactly 1", len(errs), errs)
	}
	if errs[0].Source != "loop" {
		t.Errorf("ErrorEvent.Source = %q, want %q", errs[0].Source, "loop")
	}
	if !strings.HasPrefix(errs[0].Err, overflowFaultMsg) {
		t.Errorf("ErrorEvent.Err = %q, want it to lead with the provider's message %q", errs[0].Err, overflowFaultMsg)
	}
	if hasEvent[domain.MessageEvent](sink.events) {
		t.Error("a MessageEvent was emitted for a Turn that produced no assistant message")
	}
	if got := a.conv.Len(); got != 1 {
		msgs := a.conv.Messages()
		var roles []string
		for _, m := range msgs {
			roles = append(roles, string(m.Role))
		}
		t.Errorf("conv.Len() = %d (roles %s), want 1 — only the user message survives an abandoned Turn",
			got, strings.Join(roles, ","))
	}
}

// TestOverflowGiveUpNamesTheWindowRemedy pins the give-up message's one window-dependent half: with
// NO window known every growth bound is inert, so the session repeats this failure on every message
// until /clear — the event therefore names the config key that ends it. With a window known the
// user has nothing to set, so the provider's message surfaces byte-identically, exactly as ADR 0018
// decision 2 requires. Both rows drive the full Step give-up path, not the helper alone.
func TestOverflowGiveUpNamesTheWindowRemedy(t *testing.T) {
	tests := []struct {
		name       string
		window     int
		wantRemedy bool
	}{
		{name: "an unknown window names `context-window:`", window: 0, wantRemedy: true},
		{name: "a known window keeps the provider's message verbatim", window: 8192, wantRemedy: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := baseConfig(sink)
			cfg.Context.MaxContextTokens = tc.window // 0 ⇒ neither discovery nor `context-window:` reported one
			a, err := newAgent(cfg, faultResponder{kind: provider.DeltaContextOverflow, msg: overflowFaultMsg})
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			if err := a.Submit(domain.UserInput{Text: "summarize the repository"}); err != nil {
				t.Fatalf("Submit: %v", err)
			}

			if _, err := a.Step(context.Background()); err != nil {
				t.Fatalf("Step: %v", err)
			}

			errs := errorEvents(sink.events)
			if len(errs) != 1 || errs[0].Source != "loop" {
				t.Fatalf("ErrorEvents = %v, want exactly one from source %q", errs, "loop")
			}
			if !strings.HasPrefix(errs[0].Err, overflowFaultMsg) {
				t.Errorf("ErrorEvent.Err = %q, want it to lead with the provider's message %q", errs[0].Err, overflowFaultMsg)
			}
			if got := strings.Contains(errs[0].Err, "`context-window:`"); got != tc.wantRemedy {
				t.Errorf("ErrorEvent names `context-window:` = %v, want %v; message: %q", got, tc.wantRemedy, errs[0].Err)
			}
			if !tc.wantRemedy && errs[0].Err != overflowFaultMsg {
				t.Errorf("ErrorEvent.Err = %q, want the provider's message verbatim %q with a window known",
					errs[0].Err, overflowFaultMsg)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The transient seam: one re-stream per Turn
// ---------------------------------------------------------------------------

// transientFaultMsg is the sanitized text the provider builds for the in-band 502 an aggregator
// wrapped in an HTTP 200 mid-stream — the observed shape (session 20260813T100440Z-104eaf7a) that
// used to kill the whole exchange with no retry.
const transientFaultMsg = `apogee: upstream in-band error 502: ` +
	`{"error":{"code":502,"message":"Provider returned error","metadata":{"raw":"upstream timed out"}},` +
	`"error_type":"provider_unavailable"}`

// retryableErrorScript is a stream that faults with a TRANSIENT in-band error — the classification
// the provider attaches (Delta.Retryable) when the fault's class is one it would have retried at
// the HTTP layer. errorScript is its non-retryable twin, everything else held equal.
func retryableErrorScript(msg string) []provider.Delta {
	return []provider.Delta{{Kind: provider.DeltaError, Err: msg, Retryable: true}}
}

// shortRestreamHoldoff shrinks the loop's re-stream hold-off for the duration of one test, so a
// test driving the recovery does not sit through the production second, and restores it after.
// Safe because the tests that call it are serial: none of them calls t.Parallel.
func shortRestreamHoldoff(t *testing.T) {
	t.Helper()
	previous := restreamHoldoff
	restreamHoldoff = time.Millisecond
	t.Cleanup(func() { restreamHoldoff = previous })
}

// countEvents reports how many of events are of type T.
func countEvents[T domain.Event](events []domain.Event) int {
	n := 0
	for _, e := range events {
		if _, ok := e.(T); ok {
			n++
		}
	}
	return n
}

// lastFaultMsg returns the Err of the last fault Delta in scripts — the message the give-up path
// must surface, which for a re-streamed Turn is the SECOND attempt's fault, not the first's.
func lastFaultMsg(scripts [][]provider.Delta) string {
	msg := ""
	for _, script := range scripts {
		for _, d := range script {
			if d.Kind == provider.DeltaError {
				msg = d.Err
			}
		}
	}
	return msg
}

// TestRespondAndReviewReStreamsATransientFaultOnce pins the recovery and both its edges. A fault
// the provider classed transient re-sends the SAME request once, and a second attempt that lands
// completes the Turn SILENTLY — one StreamResetEvent, so a streaming Driver discards the partial
// reply, and no ErrorEvent, because nothing reached the user that they must act on (the
// overflow-recovery precedent). The two edges keep today's behaviour byte-for-byte: a second
// transient fault gives up, and a fault that was never transient never re-streams at all.
func TestRespondAndReviewReStreamsATransientFaultOnce(t *testing.T) {
	shortRestreamHoldoff(t)

	tests := []struct {
		name        string
		scripts     [][]provider.Delta
		wantOutcome turnOutcome
		wantText    string
		wantCalls   int
		wantResets  int
		wantErrors  int
	}{
		{
			name:        "a transient fault re-streams and the recovered Turn stays quiet",
			scripts:     [][]provider.Delta{retryableErrorScript(transientFaultMsg), contentScript("recovered")},
			wantOutcome: turnOK,
			wantText:    "recovered",
			wantCalls:   2,
			wantResets:  1,
			wantErrors:  0,
		},
		{
			name:        "a second transient fault gives up exactly as today",
			scripts:     [][]provider.Delta{retryableErrorScript(transientFaultMsg), retryableErrorScript(transientFaultMsg)},
			wantOutcome: turnFailed,
			wantCalls:   2,
			wantResets:  1,
			wantErrors:  1,
		},
		{
			name:        "a fault that is not transient fails on the spot",
			scripts:     [][]provider.Delta{errorScript("apogee: upstream in-band error 400: bad request"), contentScript("unreached")},
			wantOutcome: turnFailed,
			wantCalls:   1,
			wantResets:  0,
			wantErrors:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			responder := &scriptedResponder{scripts: tc.scripts}
			a, err := newAgent(baseConfig(sink), responder)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			req, _ := a.buildRequest(0)
			run := &turnRun{turn: 0, req: req}

			resp, outcome, carried := a.respondAndReview(context.Background(), run)

			if outcome != tc.wantOutcome {
				t.Errorf("outcome = %v, want %v", outcome, tc.wantOutcome)
			}
			if carried != "" {
				t.Errorf("carried message = %q, want empty — only an overflow carries its fault out", carried)
			}
			if tc.wantText == "" {
				if resp != nil {
					t.Errorf("resp = %+v, want nil on a terminal fault", resp)
				}
			} else if resp == nil || resp.Text() != tc.wantText {
				t.Errorf("resp = %+v, want the re-streamed reply %q", resp, tc.wantText)
			}
			if responder.calls != tc.wantCalls {
				t.Errorf("Upstream calls = %d, want %d — the Turn re-streams at most once", responder.calls, tc.wantCalls)
			}
			if got := countEvents[domain.StreamResetEvent](sink.events); got != tc.wantResets {
				t.Errorf("StreamResetEvents = %d, want %d", got, tc.wantResets)
			}
			errs := errorEvents(sink.events)
			if len(errs) != tc.wantErrors {
				t.Fatalf("ErrorEvents = %d (%v), want %d", len(errs), errs, tc.wantErrors)
			}
			if tc.wantErrors == 1 && (errs[0].Source != "loop" || errs[0].Err != lastFaultMsg(tc.scripts)) {
				t.Errorf("ErrorEvent = {Source:%q Err:%q}, want {Source:%q Err:%q}",
					errs[0].Source, errs[0].Err, "loop", lastFaultMsg(tc.scripts))
			}
			if wantSpent := tc.wantResets == 1; run.restreamSpent != wantSpent {
				t.Errorf("restreamSpent = %v, want %v", run.restreamSpent, wantSpent)
			}
		})
	}
}

// TestReStreamLatchIsPerTurn proves the latch is scoped to the Turn rather than the session: a
// later Turn that hits its own blip re-streams again instead of inheriting a spent latch. Session-
// scoping it would leave every Turn after the first recovered stutter with no recovery at all.
func TestReStreamLatchIsPerTurn(t *testing.T) {
	shortRestreamHoldoff(t)

	sink := &recordingSink{}
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		retryableErrorScript(transientFaultMsg), contentScript("first"), // Turn 0: a blip, then the answer
		retryableErrorScript(transientFaultMsg), contentScript("second"), // Turn 1: its own blip, its own recovery
	}}
	a, err := newAgent(baseConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	for _, text := range []string{"first question", "second question"} {
		if err := a.Submit(domain.UserInput{Text: text}); err != nil {
			t.Fatalf("Submit(%q): %v", text, err)
		}
		res, err := a.Step(context.Background())
		if err != nil {
			t.Fatalf("Step(%q): %v", text, err)
		}
		if res.Status != domain.StatusExchangeComplete || res.Faulted {
			t.Fatalf("Step(%q) result = %+v, want a clean exchange-complete", text, res)
		}
	}

	if got := countEvents[domain.StreamResetEvent](sink.events); got != 2 {
		t.Errorf("StreamResetEvents = %d, want 2 — each Turn spends its own latch", got)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("ErrorEvents = %v, want none — both Turns recovered", errs)
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "second" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want %q", me, ok, "second")
	}
}

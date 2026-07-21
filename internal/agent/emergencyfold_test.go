package agent

// The emergency fold: the overflow-driven Compaction trigger. Unlike the estimate-driven
// autoCompact it may run MID-EXCHANGE (a Turn whose request the server just rejected cannot wait
// for the next Exchange opening), and it leaves the conversation in a shape a strict chat template
// accepts on the retry — prefix, one assistant summary, one user bridge. These tests pin the fold's
// contract in isolation (the retry orchestration that calls it is the loop's business): what it
// produces on success, what it re-anchors, and the four ways it declines without touching history.

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// assertTemplateLegal fails when the conversation would be rejected by a strict chat template —
// a surviving tool result or unanswered tool call (whose partner the fold replaced), or two
// consecutive messages in the same role once past the leading system messages.
func assertTemplateLegal(t *testing.T, a *Agent) {
	t.Helper()
	prev := domain.Role("")
	for i := 0; i < a.conv.Len(); i++ {
		m := a.conv.At(i)
		if m.Role == domain.RoleTool {
			t.Errorf("message %d is a dangling tool result: %+v", i, m)
		}
		if len(m.ToolCalls) > 0 {
			t.Errorf("message %d carries an unanswered tool call: %+v", i, m)
		}
		if m.Role != domain.RoleSystem && m.Role == prev {
			t.Errorf("messages %d and %d are both %q; a strict template requires alternation", i-1, i, m.Role)
		}
		prev = m.Role
	}
}

// TestEmergencyFoldRunsMidExchangeAndBridges is the happy path: a tool-heavy Exchange in flight
// (exactly where autoCompact stands down) folds to prefix + summary + user bridge, quietly, with
// exchangeStart re-anchored to the bridge so a later AbortExchange rolls back to the clean folded
// boundary instead of into the protected prefix.
func TestEmergencyFoldRunsMidExchangeAndBridges(t *testing.T) {
	sink := &recordingSink{}
	up := &compactSpyResponder{reply: "EMERGENCY-SUMMARY"}
	a, err := newAgent(autoCompactConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedToolCallConv(a) // 8 messages of paired tool calls/results past a 1-message protected prefix
	a.inExchange = true
	a.exchangeStart = 4 // "now add tests" opened the Exchange still in flight

	if !a.emergencyFold(context.Background(), 0) {
		t.Fatal("emergencyFold = false mid-Exchange; the overflow path must fold there (S2 is amended for it)")
	}

	if up.summaryCalls != 1 {
		t.Fatalf("summarizer calls = %d, want exactly 1", up.summaryCalls)
	}
	if a.conv.Len() != 3 {
		t.Fatalf("conv.Len() = %d after the fold, want 3 (prefix + summary + bridge)", a.conv.Len())
	}
	if got := a.conv.At(0); got.Role != domain.RoleUser || got.Content != "implement feature X" {
		t.Errorf("protected prefix not preserved: %+v", got)
	}
	if got := a.conv.At(1); got.Role != domain.RoleAssistant || !strings.Contains(got.Content, "EMERGENCY-SUMMARY") {
		t.Errorf("message 1 is not the assistant summary: %+v", got)
	}
	if got := a.conv.At(2); got.Role != domain.RoleUser || got.Content != overflowBridge {
		t.Errorf("message 2 is not the user bridge: %+v", got)
	}
	assertTemplateLegal(t, a)
	if hasEvent[domain.ErrorEvent](sink.events) {
		t.Errorf("a successful emergency fold emitted an ErrorEvent: %v", errorEvents(sink.events))
	}

	// The re-anchor: exchangeStart points at the bridge, so the Exchange's rollback boundary is the
	// folded prefix + summary — not a stale index into (or past) the protected prefix.
	if a.exchangeStart != a.conv.Len()-1 {
		t.Errorf("exchangeStart = %d after the fold, want %d (the bridge's index)", a.exchangeStart, a.conv.Len()-1)
	}
	a.AbortExchange()
	if a.conv.Len() != 2 {
		t.Errorf("after AbortExchange conv.Len() = %d, want 2 (prefix + summary)", a.conv.Len())
	}
	assertTemplateLegal(t, a)
}

// TestEmergencyFoldSkipsWhenNothingToFold pins the "recovery is impossible" answer: with only one
// message past the protected prefix the reducer skips (minCompactTail), so there is nothing to shed
// and a retry would overflow identically — false, no upstream call, conversation untouched.
func TestEmergencyFoldSkipsWhenNothingToFold(t *testing.T) {
	sink := &recordingSink{}
	up := &compactSpyResponder{reply: "UNREACHED"}
	a, err := newAgent(autoCompactConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "the overarching goal"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, Content: strings.Repeat("x", 25000)})
	a.inExchange = true
	a.exchangeStart = 1

	if a.emergencyFold(context.Background(), 0) {
		t.Fatal("emergencyFold = true on a skipped fold; nothing was folded, so a retry cannot help")
	}

	if up.summaryCalls != 0 {
		t.Errorf("summarizer calls = %d on a skipped fold, want 0", up.summaryCalls)
	}
	if a.conv.Len() != 2 {
		t.Errorf("conv.Len() = %d, want 2 — a skipped fold must not touch the conversation", a.conv.Len())
	}
	if a.exchangeStart != 1 {
		t.Errorf("exchangeStart = %d, want 1 — a skipped fold re-anchors nothing", a.exchangeStart)
	}
	if hasEvent[domain.ErrorEvent](sink.events) {
		t.Errorf("a skipped fold emitted an ErrorEvent: %v", errorEvents(sink.events))
	}
}

// TestEmergencyFoldRespectsCompactionOptOut pins decision 4: the emergency fold IS an automatic
// fold, so `auto-compact: false` opts out of overflow recovery too — false, and crucially NO
// upstream call, leaving the Turn to abandon exactly as it does today.
func TestEmergencyFoldRespectsCompactionOptOut(t *testing.T) {
	sink := &recordingSink{}
	up := &compactSpyResponder{reply: "UNREACHED"}
	cfg := autoCompactConfig(sink)
	cfg.Context.CompactionEnabled = false
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedFoldable(a)
	a.inExchange = true

	if a.emergencyFold(context.Background(), 0) {
		t.Fatal("emergencyFold = true with auto-compact off; the opt-out covers recovery too")
	}

	if up.summaryCalls != 0 {
		t.Errorf("summarizer calls = %d with auto-compact off, want 0 (the gate precedes the upstream call)", up.summaryCalls)
	}
	if a.conv.Len() != 4 {
		t.Errorf("conv.Len() = %d, want 4 — the opted-out fold must not touch the conversation", a.conv.Len())
	}
	if hasEvent[domain.ErrorEvent](sink.events) {
		t.Errorf("the opt-out emitted an ErrorEvent: %v", errorEvents(sink.events))
	}
}

// TestEmergencyFoldFaultSurfacesOnceAndKeepsHistory drives the summarizer itself failing (its own
// call overflows — a summary call has no recovery of its own): one ErrorEvent from source
// "compaction", false, and the conversation untouched, so a failed emergency fold never corrupts
// history and the Turn still gives up cleanly.
func TestEmergencyFoldFaultSurfacesOnceAndKeepsHistory(t *testing.T) {
	sink := &recordingSink{}
	a, err := newAgent(autoCompactConfig(sink), overflowResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedFoldable(a)
	a.inExchange = true
	a.exchangeStart = 2

	if a.emergencyFold(context.Background(), 0) {
		t.Fatal("emergencyFold = true despite the summary call failing")
	}

	errs := errorEvents(sink.events)
	if len(errs) != 1 {
		t.Fatalf("ErrorEvents = %d (%v), want exactly 1", len(errs), errs)
	}
	if errs[0].Source != "compaction" {
		t.Errorf("ErrorEvent.Source = %q, want %q (the fold faulted, not the Turn's request)", errs[0].Source, "compaction")
	}
	if !strings.Contains(errs[0].Err, "context window exceeded") {
		t.Errorf("ErrorEvent.Err = %q, want the summarizer's fault surfaced", errs[0].Err)
	}
	if a.conv.Len() != 4 {
		t.Errorf("conv.Len() = %d, want 4 — a faulted fold must leave history untouched", a.conv.Len())
	}
	if a.exchangeStart != 2 {
		t.Errorf("exchangeStart = %d, want 2 — a faulted fold re-anchors nothing", a.exchangeStart)
	}
}

// TestEmergencyFoldCancelIsQuiet pins the cancel path: the cancellation masquerades as a stream
// error, but ctx wins, so the fold declines SILENTLY — no ErrorEvent to compete with the Turn's own
// cancellation, and the conversation untouched.
func TestEmergencyFoldCancelIsQuiet(t *testing.T) {
	sink := &recordingSink{}
	up := blockingResponder{started: make(chan struct{})}
	a, err := newAgent(autoCompactConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedFoldable(a)
	a.inExchange = true

	ctx, cancel := context.WithCancel(context.Background())
	folded := make(chan bool, 1)
	go func() { folded <- a.emergencyFold(ctx, 0) }()

	<-up.started // the summary call is in flight; cancel deterministically (no sleep)
	cancel()

	if <-folded {
		t.Fatal("emergencyFold = true on a cancelled fold")
	}
	if hasEvent[domain.ErrorEvent](sink.events) {
		t.Errorf("a cancelled fold emitted an ErrorEvent: %v", errorEvents(sink.events))
	}
	if a.conv.Len() != 4 {
		t.Errorf("conv.Len() = %d, want 4 — a cancelled fold must leave history untouched", a.conv.Len())
	}
}

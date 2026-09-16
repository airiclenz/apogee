package agent

import (
	"testing"
	"time"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
)

// The exit table (item 2) is unit-testable in isolation: end() drives a turnLifecycle over a
// scripted Conversation with no fake responder and no scripted step() run — the deepening's
// testability payoff. Each row below asserts the three dimensions end() owns (whether the
// Exchange closes, whether the counter advances, the conversation rewrite) exactly as the three
// deleted helpers did — behavior is locked to today's.

func TestTurnEnd_Table(t *testing.T) {
	past := time.Now().Add(-time.Millisecond) // guarantees Elapsed > 0

	t.Run("endTurnDone advances, leaves the Exchange open and the queue untouched", func(t *testing.T) {
		conv := domain.NewConversation(nil)
		conv.Append(domain.Message{Role: domain.RoleUser, Content: "u"})
		conv.Defer("pending-correction")
		l := &turnLifecycle{conv: conv, index: 5, inExchange: true}

		res := l.end(&turnRun{turn: 5, start: past}, endTurnDone)

		if res.Status != domain.StatusTurnComplete {
			t.Errorf("Status = %q, want %q", res.Status, domain.StatusTurnComplete)
		}
		if res.TurnIndex != 5 {
			t.Errorf("TurnIndex = %d, want 5", res.TurnIndex)
		}
		if res.Elapsed <= 0 {
			t.Errorf("Elapsed = %v, want > 0", res.Elapsed)
		}
		if res.Faulted {
			t.Error("Faulted set on a completed tool-call Turn")
		}
		if l.index != 6 {
			t.Errorf("index = %d, want 6 (advanced)", l.index)
		}
		if !l.inExchange {
			t.Error("inExchange flipped off; a tool-call Turn leaves the Exchange open")
		}
		if l.conv.DeferredLen() != 1 {
			t.Errorf("deferred queue len = %d, want 1 (untouched on endTurnDone)", l.conv.DeferredLen())
		}
	})

	t.Run("endExchangeDone advances, closes the Exchange and clears the queue", func(t *testing.T) {
		conv := domain.NewConversation(nil)
		conv.Append(domain.Message{Role: domain.RoleUser, Content: "u"})
		conv.Defer("pending-correction")
		l := &turnLifecycle{conv: conv, index: 5, inExchange: true}

		res := l.end(&turnRun{turn: 5, start: past}, endExchangeDone)

		if res.Status != domain.StatusExchangeComplete {
			t.Errorf("Status = %q, want %q", res.Status, domain.StatusExchangeComplete)
		}
		if res.Faulted {
			t.Error("Faulted set on a real final answer; only an abandoned Turn is faulted")
		}
		if l.index != 6 {
			t.Errorf("index = %d, want 6 (advanced)", l.index)
		}
		if l.inExchange {
			t.Error("inExchange still set; a final no-tool reply closes the Exchange")
		}
		if l.conv.DeferredLen() != 0 {
			t.Errorf("deferred queue len = %d, want 0 (closeExchange clears it — F6)", l.conv.DeferredLen())
		}
	})

	t.Run("endStepCapped closes the Exchange, marks the boundary partial and leaves the counter alone", func(t *testing.T) {
		conv := domain.NewConversation(nil)
		conv.Append(domain.Message{Role: domain.RoleUser, Content: "u"})
		conv.Defer("pending-correction")
		l := &turnLifecycle{conv: conv, index: 5, inExchange: true}

		res := l.end(&turnRun{turn: 5, start: past}, endStepCapped)

		if res.Status != domain.StatusExchangeComplete {
			t.Errorf("Status = %q, want %q", res.Status, domain.StatusExchangeComplete)
		}
		if !res.StepCapped {
			t.Error("StepCapped not set; it is the only thing that tells a capped Exchange from a finished one")
		}
		if res.Faulted {
			t.Error("Faulted set on a step-capped Exchange; nothing failed — the work up to the cap stands")
		}
		// Run reaches this row only after endTurnDone advanced past the completed Turn, and no new
		// Turn runs here — so advancing again would put the counter one ahead of the Turns taken.
		if l.index != 5 {
			t.Errorf("index = %d, want 5 (unchanged — endTurnDone already advanced)", l.index)
		}
		if l.inExchange {
			t.Error("inExchange still set; the step cap ends the Exchange")
		}
		if l.conv.DeferredLen() != 0 {
			t.Errorf("deferred queue len = %d, want 0 (closeExchange clears it — F6)", l.conv.DeferredLen())
		}
	})

	t.Run("endAbandoned advances, closes the Exchange and empties the queue", func(t *testing.T) {
		conv := domain.NewConversation(nil)
		conv.Append(domain.Message{Role: domain.RoleUser, Content: "u"})
		// Two corrections sit on the queue — as if a deleted restoreDeferred had re-queued them.
		// The dead-restore pin: end(endAbandoned) empties the queue regardless, so re-queuing before
		// abandon was dead motion (agenda #4 / F6).
		conv.Defer("drained-1")
		conv.Defer("drained-2")
		l := &turnLifecycle{conv: conv, index: 5, inExchange: true}

		res := l.end(&turnRun{turn: 5, start: past}, endAbandoned)

		if res.Status != domain.StatusExchangeComplete {
			t.Errorf("Status = %q, want %q", res.Status, domain.StatusExchangeComplete)
		}
		// The marker that distinguishes this row from endExchangeDone, which returns the SAME
		// status: without it a reader reporting the Exchange's outcome onward (the sub-agent
		// orchestrator) cannot tell a fault from an answer.
		if !res.Faulted {
			t.Error("Faulted not set; an abandoned Turn closes on StatusExchangeComplete and is otherwise indistinguishable from a completion")
		}
		if l.index != 6 {
			t.Errorf("index = %d, want 6 (advanced)", l.index)
		}
		if l.inExchange {
			t.Error("inExchange still set; an abandoned Turn ends the Exchange")
		}
		if l.conv.DeferredLen() != 0 {
			t.Errorf("deferred queue len = %d, want 0 (abandon clears it; the pre-abandon restore was dead motion)", l.conv.DeferredLen())
		}
	})

	t.Run("endCancelled rolls back, holds the counter and restores the queue exactly once", func(t *testing.T) {
		conv := domain.NewConversation(nil)
		conv.Append(domain.Message{Role: domain.RoleSystem, Content: "sys"})  // idx 0
		conv.Append(domain.Message{Role: domain.RoleUser, Content: "u"})      // idx 1
		rollback := conv.Len()                                                // 2 — the Turn's pre-request boundary
		conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "a"}) // idx 2 — this Turn's work
		conv.Append(domain.Message{Role: domain.RoleTool, Content: "result"}) // idx 3 — dropped by the cancel
		deferredFloor := conv.DeferredLen()                                   // 0 — after the request drained the queue
		conv.Defer("own-directive")                                           // the cancelled Turn's own post-response deferral (past the floor)

		l := &turnLifecycle{conv: conv, index: 5, inExchange: true}
		t0 := &turnRun{turn: 7, start: past, rollback: rollback, deferred: []string{"drained-correction"}, deferredFloor: deferredFloor}

		res := l.end(t0, endCancelled)

		if res.Status != domain.StatusCancelled {
			t.Errorf("Status = %q, want %q", res.Status, domain.StatusCancelled)
		}
		if res.Faulted {
			t.Error("Faulted set on a cancelled Turn; a cancel is a re-attemptable rollback, not a fault")
		}
		if res.TurnIndex != 7 {
			t.Errorf("TurnIndex = %d, want 7", res.TurnIndex)
		}
		if l.index != 5 {
			t.Errorf("index = %d, want 5 (held — a cancelled Turn is re-attempted)", l.index)
		}
		if !l.inExchange {
			t.Error("inExchange cleared; a cancelled Turn deliberately leaves the Exchange open")
		}
		if l.conv.Len() != rollback {
			t.Errorf("conversation len = %d, want %d (rolled back to the Turn's boundary)", l.conv.Len(), rollback)
		}
		// The queue holds exactly the drained correction: the Turn's own "own-directive" was
		// truncated to the floor before the restore, so the re-attempt carries one directive, not two (F6).
		got, ok := l.conv.TakeDeferred()
		if !ok || len(got) != 1 || got[0] != "drained-correction" {
			t.Errorf("deferred queue = %v (ok=%v), want exactly [drained-correction]", got, ok)
		}
	})
}

// The Exchange-boundary mutations (item 4) are unit-testable in isolation: each drives a
// turnLifecycle over a scripted Conversation with no fake responder and no scripted step() run.
// reanchorAfterShrink's clamp — pinned end-to-end only through the
// TestExchangeStartRepairedAfterMidExchangeTruncation integration test — is table-tested directly
// here; openExchange and anchorAtBridge get one direct case each.

// buildConv returns a Conversation of exactly n messages whose protected prefix is [system, user]
// (PrefixEnd() == 2 for n >= 2), the rest filled with assistant/tool turns — enough shape to
// exercise reanchorAfterShrink's [PrefixEnd()+1, Len()] clamp.
func buildConv(n int) *domain.Conversation {
	conv := domain.NewConversation(nil)
	for i := 0; i < n; i++ {
		role := domain.RoleAssistant
		switch {
		case i == 0:
			role = domain.RoleSystem
		case i == 1:
			role = domain.RoleUser
		case i%2 == 1:
			role = domain.RoleTool
		}
		conv.Append(domain.Message{Role: role, Content: "m"})
	}
	return conv
}

func TestReanchorAfterShrink_Clamp(t *testing.T) {
	// PrefixEnd() is 2 for every conv below (system + first user), so the clamp floor is 3.
	cases := []struct {
		name       string
		convLen    int
		inExchange bool
		start      int
		dropped    int
		want       int
	}{
		{"shift within span", 10, true, 8, 2, 6},            // max(6,3)=6, min(6,10)=6
		{"floor clamp at PrefixEnd()+1", 10, true, 4, 5, 3}, // max(-1,3)=3, min(3,10)=3
		{"ceiling clamp at Len()", 5, true, 10, 2, 5},       // max(8,3)=8, min(8,5)=5
		{"zero dropped is a no-op", 10, true, 8, 0, 8},
		{"negative dropped is a no-op", 10, true, 8, -3, 8},
		{"not in Exchange is a no-op", 10, false, 8, 4, 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conv := buildConv(tc.convLen)
			if conv.PrefixEnd() != 2 {
				t.Fatalf("test setup: PrefixEnd() = %d, want 2", conv.PrefixEnd())
			}
			l := &turnLifecycle{conv: conv, inExchange: tc.inExchange, exchangeStart: tc.start}
			l.reanchorAfterShrink(tc.dropped)
			if l.exchangeStart != tc.want {
				t.Errorf("exchangeStart = %d, want %d", l.exchangeStart, tc.want)
			}
		})
	}
}

func TestOpenExchange(t *testing.T) {
	conv := buildConv(3) // system, user, assistant — length 3
	l := &turnLifecycle{conv: conv}

	l.openExchange()

	if l.exchangeStart != 3 {
		t.Errorf("exchangeStart = %d, want 3 (conv.Len() before the user Append)", l.exchangeStart)
	}
	if !l.inExchange {
		t.Error("inExchange not set; openExchange must flip it on")
	}
}

// countingObserver is a bare exchangeObserver for the lifecycle's notification contract: it
// counts each moment and does nothing else, standing where the Agent stands in construct.go.
type countingObserver struct{ closed, rolledBack int }

func (o *countingObserver) exchangeClosed() { o.closed++ }
func (o *countingObserver) turnRolledBack() { o.rolledBack++ }

// The lifecycle owns two moments that reach past it and tells its observer about each exactly
// once: an Exchange END (closeExchange — every row that ends an Exchange, none that leaves one
// open) and a cancelled Turn's ROLLBACK (end()'s endCancelled row). The rows are mutually
// exclusive on purpose: a cancel leaves the Exchange open for the re-attempt, so it must never
// read as a close, and a close is not a rollback.
func TestTurnLifecycleNotifiesItsObserver(t *testing.T) {
	past := time.Now().Add(-time.Millisecond)

	t.Run("closeExchange tells the observer once and nothing else", func(t *testing.T) {
		conv := domain.NewConversation(nil)
		conv.Append(domain.Message{Role: domain.RoleUser, Content: "u"})
		conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "done"})
		obs := &countingObserver{}
		l := &turnLifecycle{conv: conv, index: 5, inExchange: true, observer: obs}

		l.end(&turnRun{turn: 5, start: past}, endExchangeDone)

		if obs.closed != 1 {
			t.Errorf("exchangeClosed fired %d times, want 1 (once per Exchange END)", obs.closed)
		}
		if obs.rolledBack != 0 {
			t.Errorf("turnRolledBack fired %d times on an Exchange end, want 0", obs.rolledBack)
		}
	})

	t.Run("endCancelled tells the observer of the rollback once and never of a close", func(t *testing.T) {
		conv := domain.NewConversation(nil)
		conv.Append(domain.Message{Role: domain.RoleUser, Content: "u"})
		rollback := conv.Len()
		conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "a"})
		conv.Append(domain.Message{Role: domain.RoleTool, Content: "result"})
		obs := &countingObserver{}
		l := &turnLifecycle{conv: conv, index: 5, inExchange: true, observer: obs}

		l.end(&turnRun{turn: 5, start: past, rollback: rollback}, endCancelled)

		if obs.rolledBack != 1 {
			t.Errorf("turnRolledBack fired %d times, want 1 (once per Turn ROLLBACK)", obs.rolledBack)
		}
		if obs.closed != 0 {
			t.Errorf("exchangeClosed fired %d times on a cancel, want 0 (the Exchange stays open for the re-attempt)", obs.closed)
		}
		if !l.inExchange {
			t.Error("inExchange cleared by a cancel; the rollback must leave the Exchange open")
		}
	})

	t.Run("a nil observer is inert on both rows", func(t *testing.T) {
		conv := domain.NewConversation(nil)
		conv.Append(domain.Message{Role: domain.RoleUser, Content: "u"})
		rollback := conv.Len()
		conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "a"})
		l := &turnLifecycle{conv: conv, index: 5, inExchange: true}

		l.end(&turnRun{turn: 5, start: past, rollback: rollback}, endCancelled) // must not panic
		l.end(&turnRun{turn: 5, start: past}, endExchangeDone)                  // must not panic

		if l.inExchange {
			t.Error("inExchange still set after endExchangeDone with no observer")
		}
	})
}

func TestAnchorAtBridge(t *testing.T) {
	t.Run("mid-Exchange re-anchors to the just-appended bridge", func(t *testing.T) {
		conv := buildConv(4) // the bridge is the last message
		l := &turnLifecycle{conv: conv, inExchange: true, exchangeStart: 1}

		l.anchorAtBridge()

		if l.exchangeStart != conv.Len()-1 {
			t.Errorf("exchangeStart = %d, want %d (the bridge's index)", l.exchangeStart, conv.Len()-1)
		}
	})

	t.Run("outside an Exchange it is a no-op", func(t *testing.T) {
		conv := buildConv(4)
		l := &turnLifecycle{conv: conv, inExchange: false, exchangeStart: 1}

		l.anchorAtBridge()

		if l.exchangeStart != 1 {
			t.Errorf("exchangeStart = %d, want 1 (no-op outside an Exchange)", l.exchangeStart)
		}
	})
}

// TestGrowthBounds_Table pins deriveGrowthBounds as the ONE site the unknown-window fallback is
// applied at, and pins which window each bound derives from: room and transcriptBudget off the
// ADVERTISED window, historyFloor off the WORKING-room History allocation — never one number for
// all three.
func TestGrowthBounds_Table(t *testing.T) {
	const window = 32768
	tests := []struct {
		name   string
		budget domain.Budget
		want   growthBounds
	}{
		{
			name:   "a known window derives every bound from the Budget",
			budget: domain.Budget{Window: window, ResponseReserve: 4096, History: 12000},
			want: growthBounds{
				windowKnown:      true,
				room:             window - 4096,
				historyFloor:     12000,
				transcriptBudget: window - compactMaxTokens - compactPromptOverheadTokens,
			},
		},
		{
			name:   "an unknown window falls back to the one conservative ceiling on every bound",
			budget: domain.Budget{},
			want: growthBounds{
				windowKnown:      false,
				room:             compactUnknownWindowTranscriptTokens,
				historyFloor:     compactUnknownWindowTranscriptTokens,
				transcriptBudget: compactUnknownWindowTranscriptTokens,
			},
		},
		{
			name:   "a zero reserve leaves the whole advertised window as room",
			budget: domain.Budget{Window: window, ResponseReserve: 0, History: 20000},
			want: growthBounds{
				windowKnown:      true,
				room:             window,
				historyFloor:     20000,
				transcriptBudget: window - compactMaxTokens - compactPromptOverheadTokens,
			},
		},
		{
			name:   "a window smaller than the summary reserves floors the transcript budget",
			budget: domain.Budget{Window: 2048, ResponseReserve: 256, History: 1000},
			want: growthBounds{
				windowKnown:      true,
				room:             2048 - 256,
				historyFloor:     1000,
				transcriptBudget: compactMinTranscriptTokens,
			},
		},
		{
			name: "a working window smaller than the advertised one lowers only the History floor",
			// Budget.History is the allocation off the WORKING room; Window stays the advertised wall.
			budget: domain.Budget{Window: window, ResponseReserve: 2048, History: 6000},
			want: growthBounds{
				windowKnown:      true,
				room:             window - 2048,
				historyFloor:     6000,
				transcriptBudget: window - compactMaxTokens - compactPromptOverheadTokens,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveGrowthBounds(tc.budget)

			if got != tc.want {
				t.Errorf("deriveGrowthBounds(%+v) = %+v, want %+v", tc.budget, got, tc.want)
			}
		})
	}
}

// TestGrowthBounds_WorkingWindowKeepsReaderNumbers pins the two readers a `working-window:` key
// splits: compactTranscriptChars keeps deriving off the ADVERTISED window while structuralFloor
// follows the WORKING-room History allocation — today's numbers, through the shared derivation.
func TestGrowthBounds_WorkingWindowKeepsReaderNumbers(t *testing.T) {
	const advertised, working = 32768, 16384
	cfg := baseConfig(&recordingSink{})
	cfg.Context.MaxContextTokens = advertised
	cfg.Context.WorkingWindow = working
	a, err := newAgent(cfg, echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	b := a.budget()

	gotChars, gotFloor := a.compactTranscriptChars(), a.structuralFloor()

	wantChars := int(float64(advertised-compactMaxTokens-compactPromptOverheadTokens) * b.CharsPerToken)
	if gotChars != wantChars {
		t.Errorf("compactTranscriptChars() = %d, want %d (derived off the ADVERTISED window)", gotChars, wantChars)
	}
	if gotFloor != b.History {
		t.Errorf("structuralFloor() = %d, want the working-room History allocation %d", gotFloor, b.History)
	}
	advertisedHistory := apogeectx.Allocate(advertised, cfg.Context.ResponseReserve, cfg.Context.ResponseReserveFraction).History
	if gotFloor >= advertisedHistory {
		t.Errorf("structuralFloor() = %d, not below the advertised window's allocation %d: the working window did not lower it", gotFloor, advertisedHistory)
	}
}

// TestGrowthBounds_SwitchUpstreamIsReadLive pins that the bounds are derived at read time, never
// cached at Turn open: a SwitchUpstream between Turns re-binds the window, and the next /compact
// (Agent.Compact hands compactTranscriptChars to the reducer) renders against the NEW one.
func TestGrowthBounds_SwitchUpstreamIsReadLive(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Context.MaxContextTokens = 32768
	a, err := newAgent(cfg, echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	before := a.compactTranscriptChars()

	if err := a.SwitchUpstream(UpstreamSpec{Endpoint: "http://elsewhere.invalid:9999", MaxContextTokens: 8192}); err != nil {
		t.Fatalf("SwitchUpstream: %v", err)
	}
	after := a.compactTranscriptChars()

	want := int(float64(8192-compactMaxTokens-compactPromptOverheadTokens) * a.budget().CharsPerToken)
	if after != want {
		t.Errorf("compactTranscriptChars() after the switch = %d, want %d (the NEW window's budget)", after, want)
	}
	if after == before {
		t.Errorf("compactTranscriptChars() = %d before and after the switch: the bound was not read live", before)
	}
}

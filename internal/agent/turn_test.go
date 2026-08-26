package agent

import (
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// The exit table (item 2) is unit-testable in isolation: end() drives a turnLifecycle over a
// scripted Conversation + selfRegulator with no fake responder and no scripted step() run — the
// deepening's testability payoff. Each row below asserts the four dimensions end() owns (judge vs
// discard, whether the Exchange closes, whether the counter advances, the conversation rewrite)
// exactly as the three deleted helpers did — behavior is locked to today's.

// seededTracker returns a selfRegulator whose pending set holds one fire ("prev") and whose
// per-Turn scratch holds this Turn's fire ("cur") plus a novel read, so endTurn's rotation and
// discardTurn's scratch-clear + read-rollback are both observable directly (same package).
func seededTracker() *selfRegulator {
	r := newSelfRegulator()
	r.pendingJudgment[domain.MechanismID("prev")] = true
	r.firedThisTurn[domain.MechanismID("cur")] = true
	r.turnReads["read-key"] = true
	r.seenReads["read-key"] = true
	return r
}

// assertJudged checks endTurn ran: the pending set rotated to this Turn's fires and the scratch cleared.
func assertJudged(t *testing.T, r *selfRegulator) {
	t.Helper()
	if !r.pendingJudgment[domain.MechanismID("cur")] {
		t.Error("judged Turn: pending set did not rotate to this Turn's fire")
	}
	if r.pendingJudgment[domain.MechanismID("prev")] {
		t.Error("judged Turn: previous fire lingered in the pending set after rotation")
	}
	if len(r.firedThisTurn) != 0 {
		t.Error("judged Turn: per-Turn fired set was not cleared")
	}
}

// assertDiscarded checks discardTurn ran: the pending set is intact, the scratch cleared, and the
// novel read rolled back out of the Session ledger.
func assertDiscarded(t *testing.T, r *selfRegulator) {
	t.Helper()
	if !r.pendingJudgment[domain.MechanismID("prev")] {
		t.Error("discarded Turn: pending set was not left in place for the re-attempt to judge")
	}
	if len(r.firedThisTurn) != 0 {
		t.Error("discarded Turn: per-Turn fired set was not cleared")
	}
	if r.seenReads["read-key"] {
		t.Error("discarded Turn: this Turn's novel read was not rolled back out of seenReads")
	}
}

func TestTurnEnd_Table(t *testing.T) {
	past := time.Now().Add(-time.Millisecond) // guarantees Elapsed > 0

	t.Run("endTurnDone judges, advances, leaves the Exchange open and the queue untouched", func(t *testing.T) {
		conv := domain.NewConversation(nil)
		conv.Append(domain.Message{Role: domain.RoleUser, Content: "u"})
		conv.Defer("pending-correction")
		tracker := seededTracker()
		l := &turnLifecycle{conv: conv, tracker: tracker, index: 5, inExchange: true}

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
		assertJudged(t, tracker)
	})

	t.Run("endExchangeDone judges, advances, closes the Exchange and clears the queue", func(t *testing.T) {
		conv := domain.NewConversation(nil)
		conv.Append(domain.Message{Role: domain.RoleUser, Content: "u"})
		conv.Defer("pending-correction")
		tracker := seededTracker()
		l := &turnLifecycle{conv: conv, tracker: tracker, index: 5, inExchange: true}

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
		assertJudged(t, tracker)
	})

	t.Run("endStepCapped advances, closes the Exchange and marks the boundary partial", func(t *testing.T) {
		conv := domain.NewConversation(nil)
		conv.Append(domain.Message{Role: domain.RoleUser, Content: "u"})
		conv.Defer("pending-correction")
		tracker := seededTracker()
		l := &turnLifecycle{conv: conv, tracker: tracker, index: 5, inExchange: true}

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
		if l.index != 6 {
			t.Errorf("index = %d, want 6 (advanced)", l.index)
		}
		if l.inExchange {
			t.Error("inExchange still set; the step cap ends the Exchange")
		}
		if l.conv.DeferredLen() != 0 {
			t.Errorf("deferred queue len = %d, want 0 (closeExchange clears it — F6)", l.conv.DeferredLen())
		}
		// Run reaches this row only AFTER endTurnDone judged the Turn that just completed, so the
		// row must not judge it a second time: the pending set stays exactly as it was rather than
		// rotating against an emptied scratch and losing a judgment (R3).
		if !tracker.pendingJudgment[domain.MechanismID("prev")] {
			t.Error("step-capped exit re-judged an already-judged Turn: the pending set rotated")
		}
		if !tracker.firedThisTurn[domain.MechanismID("cur")] {
			t.Error("step-capped exit cleared the per-Turn scratch; the Turn was already resolved by endTurnDone")
		}
	})

	t.Run("endAbandoned discards, advances, closes the Exchange and empties the queue", func(t *testing.T) {
		conv := domain.NewConversation(nil)
		conv.Append(domain.Message{Role: domain.RoleUser, Content: "u"})
		// Two corrections sit on the queue — as if a deleted restoreDeferred had re-queued them.
		// The dead-restore pin: end(endAbandoned) empties the queue regardless, so re-queuing before
		// abandon was dead motion (agenda #4 / F6).
		conv.Defer("drained-1")
		conv.Defer("drained-2")
		tracker := seededTracker()
		l := &turnLifecycle{conv: conv, tracker: tracker, index: 5, inExchange: true}

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
		assertDiscarded(t, tracker)
	})

	t.Run("endCancelled discards, rolls back, holds the counter and restores the queue exactly once", func(t *testing.T) {
		conv := domain.NewConversation(nil)
		conv.Append(domain.Message{Role: domain.RoleSystem, Content: "sys"})  // idx 0
		conv.Append(domain.Message{Role: domain.RoleUser, Content: "u"})      // idx 1
		rollback := conv.Len()                                                // 2 — the Turn's pre-request boundary
		conv.Append(domain.Message{Role: domain.RoleAssistant, Content: "a"}) // idx 2 — this Turn's work
		conv.Append(domain.Message{Role: domain.RoleTool, Content: "result"}) // idx 3 — dropped by the cancel
		deferredFloor := conv.DeferredLen()                                   // 0 — after the request drained the queue
		conv.Defer("own-directive")                                           // the cancelled Turn's own post-response deferral (past the floor)

		tracker := seededTracker()
		l := &turnLifecycle{conv: conv, tracker: tracker, index: 5, inExchange: true}
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
		assertDiscarded(t, tracker)
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
			l := &turnLifecycle{conv: conv, tracker: newSelfRegulator(), inExchange: tc.inExchange, exchangeStart: tc.start}
			l.reanchorAfterShrink(tc.dropped)
			if l.exchangeStart != tc.want {
				t.Errorf("exchangeStart = %d, want %d", l.exchangeStart, tc.want)
			}
		})
	}
}

func TestOpenExchange(t *testing.T) {
	conv := buildConv(3) // system, user, assistant — length 3
	l := &turnLifecycle{conv: conv, tracker: newSelfRegulator()}

	l.openExchange()

	if l.exchangeStart != 3 {
		t.Errorf("exchangeStart = %d, want 3 (conv.Len() before the user Append)", l.exchangeStart)
	}
	if !l.inExchange {
		t.Error("inExchange not set; openExchange must flip it on")
	}
}

func TestAnchorAtBridge(t *testing.T) {
	t.Run("mid-Exchange re-anchors to the just-appended bridge", func(t *testing.T) {
		conv := buildConv(4) // the bridge is the last message
		l := &turnLifecycle{conv: conv, tracker: newSelfRegulator(), inExchange: true, exchangeStart: 1}

		l.anchorAtBridge()

		if l.exchangeStart != conv.Len()-1 {
			t.Errorf("exchangeStart = %d, want %d (the bridge's index)", l.exchangeStart, conv.Len()-1)
		}
	})

	t.Run("outside an Exchange it is a no-op", func(t *testing.T) {
		conv := buildConv(4)
		l := &turnLifecycle{conv: conv, tracker: newSelfRegulator(), inExchange: false, exchangeStart: 1}

		l.anchorAtBridge()

		if l.exchangeStart != 1 {
			t.Errorf("exchangeStart = %d, want 1 (no-op outside an Exchange)", l.exchangeStart)
		}
	})
}

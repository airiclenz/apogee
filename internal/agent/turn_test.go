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

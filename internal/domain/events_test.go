package domain

import "testing"

// TestAuditEventCallIDShadowsTheSpawningCall pins the one name collision in the Event set. Every
// variant promotes EventBase.CallID — the RUN identity, the sub_agent call that spawned the
// emitting agent — but AuditEvent also reports a call of its own (the audited one) under the same
// name, so its own member shadows the promoted one. Both facts travel; only the spelling of the
// reach differs. A future refactor that quietly drops one of them breaks this test, which is the
// point: an observer reading ev.CallID on an AuditEvent must keep getting the audited call, and
// the attribution path must keep getting the spawning one off ev.EventBase.
func TestAuditEventCallIDShadowsTheSpawningCall(t *testing.T) {
	t.Parallel()

	ev := AuditEvent{
		EventBase: EventBase{Depth: 1, Turn: 2, CallID: "spawn_7"},
		Tool:      "write_file",
		CallID:    "audited_9",
		Decision:  "allowed",
	}

	if ev.CallID != "audited_9" {
		t.Errorf("ev.CallID = %q, want the AUDITED call %q", ev.CallID, "audited_9")
	}
	if ev.EventBase.CallID != "spawn_7" {
		t.Errorf("ev.EventBase.CallID = %q, want the SPAWNING call %q", ev.EventBase.CallID, "spawn_7")
	}
}

// TestEventBaseCallIDSeparatesSiblings pins what the field is FOR (ADR 0039): two events from two
// children of one reply are indistinguishable by Depth — they share it — so the run identity is
// the only thing that tells them apart. EventBase stays comparable, which the TUI's token
// coalescing depends on (internal/tui/sink.go merges only tokens sharing a whole base), so the
// separation costs one == rather than a field-by-field comparison.
func TestEventBaseCallIDSeparatesSiblings(t *testing.T) {
	t.Parallel()

	first := TokenEvent{EventBase: EventBase{Depth: 1, Turn: 3, CallID: "c1"}, Text: "a"}
	second := TokenEvent{EventBase: EventBase{Depth: 1, Turn: 3, CallID: "c2"}, Text: "b"}
	top := MessageEvent{EventBase: EventBase{Turn: 3}, Text: "done"}

	if first.EventBase == second.EventBase {
		t.Error("two siblings' EventBases compare equal; the spawning call id must separate them")
	}
	if first.CallID != "c1" || second.CallID != "c2" {
		t.Errorf("promoted CallIDs = %q/%q, want c1/c2", first.CallID, second.CallID)
	}
	if top.CallID != "" {
		t.Errorf("a top-level event carries CallID %q, want empty — it was spawned by no call", top.CallID)
	}
}

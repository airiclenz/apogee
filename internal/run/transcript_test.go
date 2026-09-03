package run

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
)

// TestTranscriptFoldRecordsAPrune pins the scrollback entry a pruning pass leaves in a Firing's
// record: a NOTE, not an error — the engine dropped tool results it had already acted on, which is
// housekeeping rather than a fault — worded exactly as the TUI words it (internal/tui's
// transcript.addPrune) and rendered verbatim from the event's own two counts.
//
// The wording is asserted literally on purpose: a human comparing a headless run's stderr against
// the record it saved must not find two spellings of one event.
func TestTranscriptFoldRecordsAPrune(t *testing.T) {
	t.Parallel()

	f := newTranscriptFold("")

	f.fold(domain.PruneEvent{Results: 3, Tokens: 1200})

	entries := f.entries
	if len(entries) != 1 {
		t.Fatalf("the fold wrote %d entries, want the prune note alone: %+v", len(entries), entries)
	}
	const want = "pruned 3 tool results (~1200 tokens)"
	if entries[0].Kind != session.EntryKindNote || entries[0].Text != want {
		t.Errorf("prune entry = %s/%q, want %s reading %q",
			entries[0].Kind, entries[0].Text, session.EntryKindNote, want)
	}
}

// TestTranscriptFoldIgnoresAGuardFiring pins the other half of the prune contract: a Floor guard
// firing contributes NOTHING to a Firing's record, exactly as a Mechanism firing already does.
// A guard repairs the model's own failure or shapes the request without steering it (ADR 0071) —
// engine behaviour the reader of a session record is owed no line about — while a prune changes
// what the conversation still holds and earns its note above.
func TestTranscriptFoldIgnoresAGuardFiring(t *testing.T) {
	t.Parallel()

	f := newTranscriptFold("")

	f.fold(domain.FloorGuardEvent{Guard: "tool-call-repair", Action: "retry"})
	f.fold(domain.MechanismFiredEvent{Mechanism: "lab_row", Hook: domain.HookPostResponse, Action: "fired"})

	if len(f.entries) != 0 {
		t.Errorf("the fold wrote %d entries for firings that contribute none: %+v", len(f.entries), f.entries)
	}
}

// TestTranscriptFoldAttributesAChildsPrune pins the attribution half: a delegate prunes its OWN
// conversation, so the note is stamped with the child's depth and the call that spawned it and
// replays nested under its head rather than flattened into the Firing's own scrollback.
func TestTranscriptFoldAttributesAChildsPrune(t *testing.T) {
	t.Parallel()

	f := newTranscriptFold("")

	f.fold(domain.PruneEvent{
		EventBase: domain.EventBase{Depth: 1, CallID: "call_2"},
		Results:   2,
		Tokens:    800,
	})

	entries := f.entries
	if len(entries) != 1 {
		t.Fatalf("the fold wrote %d entries, want the prune note alone: %+v", len(entries), entries)
	}
	if entries[0].Depth != 1 || entries[0].SpawnCallID != "call_2" {
		t.Errorf("prune entry at depth %d/spawn %q, want depth 1 spawned by call_2",
			entries[0].Depth, entries[0].SpawnCallID)
	}
}

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

// TestTranscriptFoldRecordsAClippedReference pins the entry a clipped @file leaves in a record:
// a NOTE, never an error — the message went ahead with the reference's head and tail, and the
// reader is told what the model was shown — worded by the event itself (RefClippedEvent.Notice),
// so the TUI and the record spell one clip one way.
func TestTranscriptFoldRecordsAClippedReference(t *testing.T) {
	t.Parallel()

	f := newTranscriptFold("")

	f.fold(domain.RefClippedEvent{Ref: "@docs/big.md", Tokens: 32000, Absolute: true})

	entries := f.entries
	if len(entries) != 1 {
		t.Fatalf("the fold wrote %d entries, want the clip note alone: %+v", len(entries), entries)
	}
	const want = "@docs/big.md clipped to 32k tokens — read_file ranges for the rest"
	if entries[0].Kind != session.EntryKindNote || entries[0].Text != want {
		t.Errorf("clip entry = %s/%q, want %s reading %q",
			entries[0].Kind, entries[0].Text, session.EntryKindNote, want)
	}
}

// TestTranscriptFoldIgnoresAReactionFiring pins the other half of the prune contract: a Reaction
// firing contributes NOTHING to a Firing's record. A reaction repairs the model's own failure or
// shapes what it sees without steering it (ADR 0071, ADR 0076) — engine behaviour the reader of a
// session record is owed no line about — while a prune changes what the conversation still holds
// and earns its note above.
func TestTranscriptFoldIgnoresAReactionFiring(t *testing.T) {
	t.Parallel()

	f := newTranscriptFold("")

	f.fold(domain.ReactionFiredEvent{
		Reaction: "tool-call-repair",
		Origin:   domain.OriginEngine,
		Moment:   domain.MomentPostResponse,
		Action:   "retry",
	})
	f.fold(domain.ReactionFiredEvent{
		Reaction: "bench_row",
		Origin:   domain.OriginEngine,
		Moment:   domain.MomentPostResponse,
		Action:   "intercept",
	})

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

// TestTranscriptFoldRecordsDelegationRunIDs pins the run-id half of the attribution: a
// delegation's call and result carry the run it spawned, and the entries that run folded carry it
// as their own — so two siblings whose call ids collide still land as two runs in the record.
func TestTranscriptFoldRecordsDelegationRunIDs(t *testing.T) {
	t.Parallel()

	f := newTranscriptFold("")

	f.fold(domain.ToolCallEvent{Call: domain.ToolCall{ID: "call_0", Tool: "sub_agent"}, SpawnRunID: "0badc0de.1"})
	f.fold(domain.ToolCallEvent{Call: domain.ToolCall{ID: "call_0", Tool: "sub_agent"}, SpawnRunID: "0badc0de.2"})
	f.fold(domain.MessageEvent{
		EventBase: domain.EventBase{Depth: 1, CallID: "call_0", RunID: "0badc0de.2"},
		Text:      "second's report",
	})
	f.fold(domain.ToolResultEvent{
		Result:     domain.ToolResult{CallID: "call_0", Content: "second's report"},
		Tool:       "sub_agent",
		SpawnRunID: "0badc0de.2",
	})

	entries := f.entries
	if len(entries) != 4 {
		t.Fatalf("the fold wrote %d entries, want two heads, a message and a result: %+v", len(entries), entries)
	}
	want := []struct{ runID, spawnRunID string }{
		{"", "0badc0de.1"},
		{"", "0badc0de.2"},
		{"0badc0de.2", ""},
		{"", "0badc0de.2"},
	}
	for i, w := range want {
		if entries[i].RunID != w.runID || entries[i].SpawnRunID != w.spawnRunID {
			t.Errorf("entry %d (%s) runID %q / spawnRunID %q; want %q / %q", i, entries[i].Kind,
				entries[i].RunID, entries[i].SpawnRunID, w.runID, w.spawnRunID)
		}
	}
	if entries[2].SpawnCallID != "call_0" {
		t.Errorf("the child's message lost its fallback spawnCallID: %q", entries[2].SpawnCallID)
	}
}

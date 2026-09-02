package tui

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/airiclenz/apogee/internal/domain"
)

// liveThinking returns the text the board is holding in flight, concatenated across runs. Every
// row of the fold table folds ONE event from ONE agent, so the concatenation is that run's record
// and nothing else; the interleave cases below assert on the records themselves instead.
func liveThinking(b thinkingBoard) string {
	var text string
	for _, rec := range b.live {
		text += rec.text
	}
	return text
}

// reasoningAt builds a ReasoningEvent stamped by one run under one Turn — the shape the engine
// emits and the shape every rule in thinking.go keys on.
func reasoningAt(run runRef, turn int, text string) domain.ReasoningEvent {
	return domain.ReasoningEvent{EventBase: eventBaseAt(run, turn), Text: text}
}

// eventBaseAt stamps an Event with a run identity and a Turn index, the two facts a thinking
// record is filed under.
func eventBaseAt(run runRef, turn int) domain.EventBase {
	return domain.EventBase{Depth: run.depth, Turn: turn, CallID: run.spawn}
}

// TestThinkingBoardAppendsStrippedChunksInOrder pins the seam's two jobs on the way in: the chunks
// concatenate in the order the engine revealed them (events.go guarantees that order), and every
// one of them is escape-stripped HERE rather than at its producer — a ReasoningEvent's Text is raw
// model output, so an OSC 8 opener in it must be dead before it can ever reach a cell buffer
// (doc.go's escape-strip-at-the-seam invariant). The line breaks and tabs reasoning is actually
// written with survive, exactly as they do on the visible token path, because the pane paints them.
func TestThinkingBoardAppendsStrippedChunksInOrder(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		chunks []string
		want   string
	}{
		{
			name:   "chunks concatenate in the order they arrived",
			chunks: []string{"the user ", "asked for ", "a plan"},
			want:   "the user asked for a plan",
		},
		{
			name:   "an OSC 8 opener and its C0 terminator are stripped",
			chunks: []string{"\x1b]8;;http://evil\x07link"},
			want:   "]8;;http://evillink",
		},
		{
			name:   "an escape split across two chunks is stripped in both halves",
			chunks: []string{"before\x1b[", "31mafter"},
			want:   "before[31mafter",
		},
		{
			name:   "the line breaks and tabs reasoning is written with survive",
			chunks: []string{"step 1\n\tstep 2"},
			want:   "step 1\n\tstep 2",
		},
		{
			name:   "a DEL and a bidi override go the way the escape did",
			chunks: []string{"safe\x7f\u202etxet"}, // U+202E right-to-left override
			want:   "safetxet",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var board thinkingBoard
			for _, chunk := range tc.chunks {
				board.append(chunk, runRef{}, 1)
			}

			if got := liveThinking(board); got != tc.want {
				t.Errorf("retained thinking = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestThinkingBoardBoundsEachRecordToTheCapFromTheFront pins the bound the value-copied Model
// depends on: a Turn may reason for an hour, so a record keeps the NEWEST thinkingRecordCap bytes
// and drops the oldest, which is what a reader of the pane would have scrolled off anyway.
func TestThinkingBoardBoundsEachRecordToTheCapFromTheFront(t *testing.T) {
	t.Parallel()

	// Chunk by chunk past the cap, the way a real stream arrives — the bound must hold across
	// appends, not only within one oversized chunk.
	chunk := strings.Repeat("thinking ", 128)
	const chunks = 128 // comfortably past thinkingRecordCap, in few enough appends to stay cheap

	var board thinkingBoard
	for range chunks {
		board.append(chunk, runRef{}, 1)
	}

	got := liveThinking(board)
	if len(got) > thinkingRecordCap {
		t.Errorf("retained %d bytes, want at most the %d-byte cap", len(got), thinkingRecordCap)
	}
	if !strings.HasSuffix(strings.Repeat(chunk, chunks), got) {
		t.Error("the record is not a suffix of what arrived: the front was not what got dropped")
	}
}

// TestThinkingBoardCutsTheFrontOnARuneBoundary pins the half of the bound that a byte count alone
// gets wrong: a multi-byte rune the cut lands inside is dropped whole, so the retained text is
// never left starting on continuation bytes that decode to a replacement glyph nobody's model
// wrote.
func TestThinkingBoardCutsTheFrontOnARuneBoundary(t *testing.T) {
	t.Parallel()

	// A three-byte rune, in a count that puts the cut two bytes INTO the first one: the cap is not
	// a multiple of three, so no boundary can be hit by luck.
	const snowman = "☃"
	const kept = thinkingRecordCap / len(snowman)
	arrived := strings.Repeat(snowman, kept+1)

	var board thinkingBoard
	board.append(arrived, runRef{}, 1)

	got := liveThinking(board)
	if want := strings.Repeat(snowman, kept); got != want {
		t.Errorf("retained %d bytes, want the %d whole runes that fit under the cap", len(got), kept)
	}
	if !utf8.ValidString(got) {
		t.Error("the front cut left a partial rune at the head of the record")
	}
}

// TestThinkingBoardDropsTheOldestPastTheRecordCap pins the board's own bound. Full session history
// is the ratified scope and this is what makes it affordable: past maxThinkingRecords the OLDEST
// committed record goes, because the pane opens on the newest.
func TestThinkingBoardDropsTheOldestPastTheRecordCap(t *testing.T) {
	t.Parallel()

	// Each new Turn commits the one before it, so N appends leave N-1 committed records.
	const turns = maxThinkingRecords + 10

	var board thinkingBoard
	for turn := 1; turn <= turns; turn++ {
		board.append("thinking", runRef{}, turn)
	}

	if got := len(board.done); got != maxThinkingRecords {
		t.Fatalf("kept %d committed records, want the %d-record cap", got, maxThinkingRecords)
	}
	if got, want := board.done[0].turn, turns-maxThinkingRecords; got != want {
		t.Errorf("oldest kept record is Turn %d, want %d: the front was not what got dropped", got, want)
	}
	if got, want := board.done[len(board.done)-1].turn, turns-1; got != want {
		t.Errorf("newest committed record is Turn %d, want %d (newest last)", got, want)
	}
}

// TestFoldThinkingFilesEveryRunAndTurnSeparately walks the rules a fan-out breaks when they are not
// run-scoped. Delegates run concurrently and interleave their chunks in ONE Event stream (ADR
// 0039), so a single in-flight record would either splice two agents' sentences together or shred
// each agent's Turn into a record per interleaved chunk; and a boundary Event that ignored the run
// it came from would destroy a sibling's in-flight text on one delegate's retry.
func TestFoldThinkingFilesEveryRunAndTurnSeparately(t *testing.T) {
	t.Parallel()

	parent := runRef{}
	childA := runRef{depth: 1, spawn: "call-a"}
	childB := runRef{depth: 1, spawn: "call-b"}

	for _, tc := range []struct {
		name     string
		events   []domain.Event
		wantDone []thinkingRecord
		wantLive []thinkingRecord
	}{
		{
			name: "a Turn appends, its message commits it, and the next Turn opens its own record",
			events: []domain.Event{
				reasoningAt(parent, 1, "the user "),
				reasoningAt(parent, 1, "asked for a plan"),
				domain.MessageEvent{EventBase: eventBaseAt(parent, 1), Text: "here it is"},
				reasoningAt(parent, 2, "now the second turn"),
			},
			wantDone: []thinkingRecord{{run: parent, turn: 1, text: "the user asked for a plan"}},
			wantLive: []thinkingRecord{{run: parent, turn: 2, text: "now the second turn"}},
		},
		{
			name: "a chunk under a new Turn commits the one before it even without a message",
			// The belt beside the MessageEvent brace: a Turn that ended without a closing message
			// must not have its thinking swallowed by the next one's record.
			events: []domain.Event{
				reasoningAt(parent, 1, "first"),
				reasoningAt(parent, 2, "second"),
			},
			wantDone: []thinkingRecord{{run: parent, turn: 1, text: "first"}},
			wantLive: []thinkingRecord{{run: parent, turn: 2, text: "second"}},
		},
		{
			name: "interleaved delegates get one record each, holding their own text in order",
			// The case a single live record got wrong twice over: concatenated it read as a
			// sentence nobody wrote, and split per chunk it spent the record cap in seconds.
			events: []domain.Event{
				reasoningAt(childA, 1, "A reads "),
				reasoningAt(childB, 1, "B searches "),
				reasoningAt(childA, 1, "the tests"),
				reasoningAt(childB, 1, "the docs"),
			},
			wantLive: []thinkingRecord{
				{run: childA, turn: 1, text: "A reads the tests"},
				{run: childB, turn: 1, text: "B searches the docs"},
			},
		},
		{
			name: "a superseded stream drops that run's record and no other",
			events: []domain.Event{
				reasoningAt(childA, 1, "an answer that will not exist"),
				reasoningAt(childB, 1, "B is still going"),
				domain.StreamResetEvent{EventBase: eventBaseAt(childA, 1)},
			},
			wantLive: []thinkingRecord{{run: childB, turn: 1, text: "B is still going"}},
		},
		{
			name: "a committed message commits that run's record and no other",
			events: []domain.Event{
				reasoningAt(childA, 1, "A is done thinking"),
				reasoningAt(childB, 1, "B is still going"),
				domain.MessageEvent{EventBase: eventBaseAt(childA, 1), Text: "A reports"},
			},
			wantDone: []thinkingRecord{{run: childA, turn: 1, text: "A is done thinking"}},
			wantLive: []thinkingRecord{{run: childB, turn: 1, text: "B is still going"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := newTestModel(t)
			for _, e := range tc.events {
				m = m.foldEvent(e)
			}

			if got := m.thinking.done; !slices.Equal(got, tc.wantDone) {
				t.Errorf("committed records = %+v, want %+v", got, tc.wantDone)
			}
			if got := m.thinking.live; !slices.Equal(got, tc.wantLive) {
				t.Errorf("in-flight records = %+v, want %+v", got, tc.wantLive)
			}
		})
	}
}

// TestThinkingBoardEndsAtEveryBoundary walks the four ways an in-flight record ends. Two are Events
// folded through foldEvent — a superseded stream, which DROPS it, and a committed message, which
// KEEPS it — and two are worker boundaries no Event announces, which matter because a stop or a
// fault never sends the closing message the second case relies on, and what an agent thought before
// dying is the point of the pane.
func TestThinkingBoardEndsAtEveryBoundary(t *testing.T) {
	t.Parallel()

	t.Run("a superseded stream", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(t)
		m = m.foldEvent(domain.ReasoningEvent{Text: "an answer that will not exist"})
		m = m.foldEvent(domain.StreamResetEvent{})

		if got := liveThinking(m.thinking); got != "" {
			t.Errorf("in-flight thinking = %q, want it dropped with the re-streamed Turn", got)
		}
		if got := len(m.thinking.done); got != 0 {
			t.Errorf("committed %d records, want none: a superseded Turn is not history", got)
		}
	})

	t.Run("a committed message", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(t)
		m = m.foldEvent(domain.ReasoningEvent{Text: "working it out"})
		m = m.foldEvent(domain.MessageEvent{Text: "done"})

		if got := liveThinking(m.thinking); got != "" {
			t.Errorf("in-flight thinking = %q, want the Turn's record committed", got)
		}
		want := []thinkingRecord{{turn: 0, text: "working it out"}}
		if got := m.thinking.done; !slices.Equal(got, want) {
			t.Errorf("committed records = %+v, want %+v", got, want)
		}
	})

	t.Run("a worker that unwound", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(t)
		m = m.foldEvent(domain.ReasoningEvent{Text: "cut off by a stop"})
		m.finishWorker(stateIdle)

		if got := liveThinking(m.thinking); got != "" {
			t.Errorf("in-flight thinking = %q, want nothing left live with the worker", got)
		}
		want := []thinkingRecord{{text: "cut off by a stop"}}
		if got := m.thinking.done; !slices.Equal(got, want) {
			t.Errorf("committed records = %+v, want %+v: a stopped Turn's thinking is kept", got, want)
		}
	})

	t.Run("a fresh exchange", func(t *testing.T) {
		t.Parallel()

		m := newTestModelEng(t, &fakeEngine{}, testOpts)
		m.thinking.append("left over from before", runRef{}, 1)

		m.input.SetValue("what next?")
		m, _ = stepCmd(t, m, keyEnter())

		if m.state != stateRunning {
			t.Fatalf("state = %v, want running: the submit never launched a worker", m.state)
		}
		if got := liveThinking(m.thinking); got != "" {
			t.Errorf("in-flight thinking = %q, want a new Exchange to start with none", got)
		}
		want := []thinkingRecord{{turn: 1, text: "left over from before"}}
		if got := m.thinking.done; !slices.Equal(got, want) {
			t.Errorf("committed records = %+v, want %+v", got, want)
		}
	})
}

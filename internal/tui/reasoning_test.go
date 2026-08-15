package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestReasoningTailAppendsStrippedChunksInOrder pins the seam's two jobs on the way in: the chunks
// concatenate in the order the engine revealed them (events.go guarantees that order), and every
// one of them is escape-stripped HERE rather than at its producer — a ReasoningEvent's Text is raw
// model output, so an OSC 8 opener in it must be dead before it can ever reach a cell buffer
// (doc.go's escape-strip-at-the-seam invariant). The line breaks and tabs reasoning is actually
// written with survive, exactly as they do on the visible token path.
func TestReasoningTailAppendsStrippedChunksInOrder(t *testing.T) {
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
			chunks: []string{"safe\x7f‮txet"},
			want:   "safetxet",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var tail reasoningTail
			for _, chunk := range tc.chunks {
				tail.append(chunk, 0, "")
			}

			if tail.text != tc.want {
				t.Errorf("retained reasoning = %q, want %q", tail.text, tc.want)
			}
		})
	}
}

// TestReasoningTailBoundsToTheCapFromTheFront pins the bound the value-copied Model depends on: a
// Turn may reason for an hour, so the buffer keeps the NEWEST reasoningTailCap bytes and drops the
// oldest, which is what any tail display would have scrolled off anyway.
func TestReasoningTailBoundsToTheCapFromTheFront(t *testing.T) {
	t.Parallel()

	var tail reasoningTail
	// Chunk by chunk past the cap, the way a real stream arrives — the bound must hold across
	// appends, not only within one oversized chunk.
	const chunk = "reasoning "
	for range reasoningTailCap {
		tail.append(chunk, 0, "")
	}

	if len(tail.text) > reasoningTailCap {
		t.Errorf("retained %d bytes, want at most the %d-byte cap", len(tail.text), reasoningTailCap)
	}
	if !strings.HasSuffix(strings.Repeat(chunk, reasoningTailCap), tail.text) {
		t.Error("the tail is not a suffix of what arrived: the front was not what got dropped")
	}
}

// TestReasoningTailCutsTheFrontOnARuneBoundary pins the half of the bound that a byte count alone
// gets wrong: a multi-byte rune the cut lands inside is dropped whole, so the retained text is
// never left starting on continuation bytes that decode to a replacement glyph nobody's model
// wrote.
func TestReasoningTailCutsTheFrontOnARuneBoundary(t *testing.T) {
	t.Parallel()

	// A three-byte rune, in a count that puts the cut two bytes INTO the first one: the cap is not
	// a multiple of three, so no boundary can be hit by luck.
	const snowman = "☃"
	const kept = reasoningTailCap / len(snowman)
	arrived := strings.Repeat(snowman, kept+1)

	var tail reasoningTail
	tail.append(arrived, 0, "")

	if want := strings.Repeat(snowman, kept); tail.text != want {
		t.Errorf("retained %d bytes, want the %d whole runes that fit under the cap", len(tail.text), kept)
	}
	if !utf8.ValidString(tail.text) {
		t.Error("the front cut left a partial rune at the head of the tail")
	}
}

// TestReasoningTailHoldsOneAgentAtATime pins the rule the activity line already lives by: a fan-out
// runs delegates concurrently (ADR 0039) and their chunks interleave in one Event stream, so the
// tail speaks for whoever spoke last and starts over rather than splicing two agents' sentences
// together. Depth alone cannot tell siblings apart, which is why the spawning call id is half the
// identity.
func TestReasoningTailHoldsOneAgentAtATime(t *testing.T) {
	t.Parallel()

	var tail reasoningTail
	tail.append("the parent is planning", 0, "")

	tail.append("the child is ", 1, "call-1")
	tail.append("reading", 1, "call-1")
	if want := "the child is reading"; tail.text != want {
		t.Errorf("after a descent the tail = %q, want %q (the parent's reasoning is not the child's)", tail.text, want)
	}

	tail.append("the sibling is searching", 1, "call-2")
	if want := "the sibling is searching"; tail.text != want {
		t.Errorf("after a sibling spoke the tail = %q, want %q (same depth, different agent)", tail.text, want)
	}
	if tail.depth != 1 || tail.spawn != "call-2" {
		t.Errorf("the tail holds identity (%d, %q), want (1, \"call-2\")", tail.depth, tail.spawn)
	}
}

// TestReasoningTailEndsAtEveryBoundary walks the four ways a retained tail ends. Two are Events
// folded through foldEvent — a superseded stream and a committed message — and two are worker
// boundaries no Event announces, which matter because a stop or a fault never sends the closing
// message the third case relies on.
func TestReasoningTailEndsAtEveryBoundary(t *testing.T) {
	t.Parallel()

	t.Run("a superseded stream", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(t)
		m = m.foldEvent(domain.ReasoningEvent{Text: "an answer that will not exist"})
		m = m.foldEvent(domain.StreamResetEvent{})

		if m.reasoning.text != "" {
			t.Errorf("retained reasoning = %q, want it dropped with the re-streamed Turn", m.reasoning.text)
		}
	})

	t.Run("a committed message", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(t)
		m = m.foldEvent(domain.ReasoningEvent{Text: "working it out"})
		m = m.foldEvent(domain.MessageEvent{Text: "done"})

		if m.reasoning.text != "" {
			t.Errorf("retained reasoning = %q, want it dropped once reasoning_content holds the copy", m.reasoning.text)
		}
	})

	t.Run("a worker that unwound", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(t)
		m = m.foldEvent(domain.ReasoningEvent{Text: "cut off by a stop"})
		m.finishWorker(stateIdle)

		if m.reasoning.text != "" {
			t.Errorf("retained reasoning = %q, want it dropped with the worker", m.reasoning.text)
		}
	})

	t.Run("a fresh exchange", func(t *testing.T) {
		t.Parallel()

		m := newTestModelEng(t, &fakeEngine{}, testOpts)
		m.reasoning.append("left over from before", 0, "")

		m.input.SetValue("what next?")
		m, _ = stepCmd(t, m, keyEnter())

		if m.state != stateRunning {
			t.Fatalf("state = %v, want running: the submit never launched a worker", m.state)
		}
		if m.reasoning.text != "" {
			t.Errorf("retained reasoning = %q, want a new Exchange to start with none", m.reasoning.text)
		}
	})
}

// TestReasoningTailIsRenderedNowhere is this seam's whole point, pinned: the tail is retention and
// no surface at all, so a distinctive chunk that has been folded into the Model must appear nowhere
// in the painted frame. The day a reasoning display lands, this test is the one that says so out
// loud — it fails, and whoever changes it is stating the decision rather than discovering it.
func TestReasoningTailIsRenderedNowhere(t *testing.T) {
	t.Parallel()

	// A string no chrome, legend, placeholder or start-up box could produce on its own.
	const marker = "zzqqxx-reasoning-marker-xxqqzz"

	m := newTestModel(t)
	m.state = stateRunning
	m = m.foldEvent(domain.ReasoningEvent{Text: marker})
	if m.reasoning.text != marker {
		t.Fatalf("retained reasoning = %q, want %q: the fold never held it", m.reasoning.text, marker)
	}

	if strings.Contains(m.View().Content, marker) {
		t.Error("the retained reasoning reached the frame; nothing is supposed to render it (reasoning.go)")
	}
}

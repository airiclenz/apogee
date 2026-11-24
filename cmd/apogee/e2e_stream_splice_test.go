package main

// The stream-only repaint, end to end: while a reply streams into a long session, each delta
// re-renders only the preview at the transcript's tail and keeps every row above it
// (internal/tui, Model.splicedPaint). The unit test holds that splice to a cache-less render byte
// for byte; this one holds what a real terminal shows to the frame a FULL repaint draws, through
// the whole program — the composition seam, the sink's batching, the reserve and View.
//
// cmd/apogee cannot reach the unexported cache-less renderer, so the oracle is the program's own
// full repaint: a resize away and back moves the frame key's width, which no splice serves, so the
// frame drawn after it is a whole-scrollback paint of the same reply.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/testtime"
	"github.com/airiclenz/apogee/internal/tuitest"
)

const (
	// spliceLongPrompt asks for the long reply the session's scrollback is made of, and
	// spliceStreamPrompt for the reply that streams into it.
	spliceLongPrompt   = "Write the long design notes."
	spliceStreamPrompt = "Now stream the summary."
	// spliceEnd closes the visible part of the streamed reply: once it is on screen, every delta
	// still to come is blank and moves no row of the frame.
	spliceEnd = "The visible summary ends here."
	// spliceAwayWidth is the width the terminal is squeezed to and back from, forcing the repaint.
	spliceAwayWidth = 70
	// spliceBlankDeltas is how many whitespace-only deltas hold the stream open after spliceEnd:
	// each is a pure append the preview holds back (a trailing blank line), so the frame stands
	// still while the stream is still live. At the stub's 1 ms floor that is half a minute of
	// stream at the least — far more than a resize and two frame reads take on the slowest box —
	// and the test cancels the run as soon as it has its frames.
	spliceBlankDeltas = 30000
)

// spliceLongReply is the scrollback the stream lands under: headed sections of prose, a list and a
// code fence each, so the rows above the preview are the paint of real blocks.
func spliceLongReply() string {
	var b strings.Builder
	for s := 1; s <= 12; s++ {
		fmt.Fprintf(&b, "## Section %d\n\nThe paint of section %d is settled: it is served from the cache.\n\n", s, s)
		fmt.Fprintf(&b, "- point %d.a about the scrollback\n- point %d.b about the preview\n\n", s, s)
		fmt.Fprintf(&b, "```go\nfunc section%d() int { return %d }\n```\n\n", s, s)
	}
	return strings.TrimRight(b.String(), "\n")
}

// spliceStreamChunks is the streamed reply delta by delta: a few lines of visible summary cut
// mid-word, spliceEnd, a newline, and then the blank deltas that keep the stream open.
func spliceStreamChunks() []string {
	visible := "## Summary\n\nEach streamed delta repaints only the tail of the transcript.\n" +
		"- the settled rows above it stay as they were\n- the preview is re-rendered alone\n\n" + spliceEnd
	var chunks []string
	for runes := []rune(visible); len(runes) > 0; {
		n := min(5, len(runes))
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	chunks = append(chunks, "\n")
	for range spliceBlankDeltas {
		chunks = append(chunks, " ")
	}
	return chunks
}

// TestE2EStreamIntoALongSessionMatchesAColdRender streams a reply into a long session and holds the
// frame its spliced repaints drew to the frame a full repaint of the same state draws.
func TestE2EStreamIntoALongSessionMatchesAColdRender(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, stubllm.Script{Model: "splice-model", Turns: []stubllm.Turn{
		{When: &stubllm.Match{LastMessage: "The user's first request"}, Repeat: true, Text: "Splice notes"},
		{When: &stubllm.Match{LastMessage: "^" + regexp.QuoteMeta(spliceLongPrompt)}, Text: spliceLongReply(), ChunkRunes: 400},
		{When: &stubllm.Match{LastMessage: "^" + regexp.QuoteMeta(spliceStreamPrompt)}, Chunks: spliceStreamChunks(), TokenDelay: time.Millisecond},
	}})
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	submit(drv, spliceLongPrompt)
	drv.WaitText("func section12() int")
	waitIdle(drv)

	// Every frame from here on is a splice: the reply's first token opened the preview with a full
	// repaint, and every delta since is a pure append.
	submit(drv, spliceStreamPrompt)
	spliced := steadyTranscriptRows(t, drv, e2eSize.W, "the spliced frame")

	drv.Resize(spliceAwayWidth, e2eSize.H)
	steadyTranscriptRows(t, drv, spliceAwayWidth, "the narrow frame")
	drv.Resize(e2eSize.W, e2eSize.H)
	full := steadyTranscriptRows(t, drv, e2eSize.W, "the full repaint after the resize back")

	if !slices.Equal(spliced, full) {
		t.Errorf("the spliced frame differs from the full repaint of the same reply:\n--- spliced ---\n%s\n--- full ---\n%s",
			strings.Join(spliced, "\n"), strings.Join(full, "\n"))
	}

	// Both frames are of a reply still streaming: the blank deltas were still arriving, so what
	// was compared is a live preview and not the committed entry.
	if rows, ok := drv.Frame().PromptBox(); !ok || !strings.Contains(strings.Join(rows, "\n"), runningPromptHead) {
		t.Fatalf("the reply finished before the full repaint was read; the comparison is not of a live stream:\n%s", drv.Frame())
	}

	stopRun(t, drv)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// steadyTranscriptRows waits for a frame the program painted at width columns that shows the
// streamed reply's last visible line, and returns its transcript rows — every row from the top down
// to the one that line stands on — once two reads in a row agree on them.
//
// The emulator takes a new width the instant it is resized, and the repaint that answers it comes
// later, so a frame of the right width may still hold the paint drawn for the old one — and that
// stale paint stands as still as a fresh one, passing the steady check. So the frame counts only
// once every one of those rows reaches the last column, where the transcript's rules and its
// scrollbar track stand in a paint at this width: a paint drawn narrower ends short of it. A repaint
// also takes more than one write, so a single read may catch a row half-written; the blank deltas
// keep the stream live while the rows stand still, so the steady read is the frame the repaint drew.
func steadyTranscriptRows(t *testing.T, drv *tuitest.Driver, width int, what string) []string {
	t.Helper()
	expired := testtime.Expiry(tuitest.DefaultTimeout)
	var last []string
	for {
		f := drv.Frame()
		if _, y, ok := f.Find(spliceEnd); ok && f.Width() == width && paintedAtWidth(f, y) {
			rows := f.Rows()[:y+1]
			if last != nil && slices.Equal(rows, last) {
				return rows
			}
			last = rows
		} else {
			last = nil
		}
		if expired() {
			t.Fatalf("%s never settled at %d columns with the reply's last visible line on screen:\n%s", what, width, f)
		}
		time.Sleep(testtime.Fixed(20 * time.Millisecond))
	}
}

// paintedAtWidth reports whether every row from the top of f down to row last reaches f's last
// column — true of a transcript the program painted at f's width, and false of one painted narrower
// that the emulator has only widened.
func paintedAtWidth(f tuitest.Frame, last int) bool {
	for y := 0; y <= last; y++ {
		if lastVisibleColumn(f, y) != f.Width()-1 {
			return false
		}
	}
	return true
}

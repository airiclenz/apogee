package main

// A draft that wraps grows the box, and the transcript re-fits above it, through the whole program.
//
// The unit tests in internal/tui pin the cost — a keystroke repaints no scrollback, the offset is
// re-seated over the lines already stored (layout's no-repaint branch) — and this is the surface a
// human sees on a real run: the key-by-key typing of a line longer than the box, the box opening a
// second row, and the reply's last line moving up by exactly that row so the tail stays in view.

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

const (
	// typingTail is the reply's last line (testdata/stubllm/typing.yaml), the one the following view
	// keeps directly above the bottom chrome.
	typingTail = "finding 40, the last one"
	// typingHead is the reply's first line, scrolled off by the forty below it.
	typingHead = "finding 01"
	// typingDraftEnd closes the draft, so its arrival in the box's second row is a text wait.
	typingDraftEnd = "draft-end"
)

func TestE2ETypingWrapsAndRefitsTheTranscript(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "typing"))
	drv := tuitest.NewDriver(t, e2eSize)
	launchTUI(t, drv, stub)
	waitIdle(drv)

	submit(drv, "List the findings")
	drv.WaitText(typingTail)
	waitIdle(drv)

	before := drv.Frame()
	_, tailRow, _ := before.Find(typingTail)
	if _, _, found := before.Find(typingHead); found {
		t.Fatalf("setup: the reply does not overflow the transcript, so there is nothing to push up:\n%s", before)
	}
	if rows, ok := before.PromptBox(); !ok || len(rows) != 1 {
		t.Fatalf("setup: the idle box is %d rows (found %v); want one:\n%s", len(rows), ok, before)
	}

	// Typed key by key, as a human types it: every rune goes through the key path's lay-out, and the
	// one that crosses the box's width opens its second row.
	drv.Type(strings.Repeat("wrap ", 22) + typingDraftEnd)
	drv.WaitFor(func() bool {
		rows, ok := drv.Frame().PromptBox()
		return ok && len(rows) == 2 && strings.Contains(rows[1], typingDraftEnd)
	}, tuitest.Awaiting("the draft to wrap to the box's second row"))

	after := drv.Frame()
	_, grownRow, found := after.Find(typingTail)
	if !found {
		t.Fatalf("the reply's last line left the screen when the box grew; the following view keeps the tail:\n%s", after)
	}
	if grownRow != tailRow-1 {
		t.Errorf("the reply's last line moved from row %d to row %d; want %d, up by the one row the box grew:\n%s",
			tailRow, grownRow, tailRow-1, after)
	}
}

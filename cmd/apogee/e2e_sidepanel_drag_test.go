package main

// The side panel's separator drag (adr-0110-amend-1108 (b)), driven through the terminal: the
// pointer's bytes go in through the program's own parser, the panel's new width comes back as
// cells on the frame, and the release's write comes back as a line in the home's config.yaml. The
// Model-level tests in internal/tui pin each step of the gesture; this is the one journey that
// proves the three reports a terminal sends reach those steps at all.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

const (
	// sidePanelAutoConfig is the `ui:` block that shows the panel by the window's width alone —
	// spelled out rather than left to the default, so the journey does not hang on what the
	// default is.
	sidePanelAutoConfig = "ui:\n  side-panel:\n    show: auto\n"

	// sidePanelDragRule is the separator glyph the panel paints on its left edge (sidepanel.go
	// sidePanelSeparator).
	sidePanelDragRule = "│"

	// sidePanelDefaultWidth is `ui.side-panel.width`'s default, separator included
	// (domain.DefaultSidePanelWidth): where the separator stands before the drag.
	sidePanelDefaultWidth = 44

	// sidePanelDragColumns is how far left the drag moves the separator — well inside the limits a
	// 160-column window allows a dragged width (24 to 160 − 60 − 1 = 99 columns).
	sidePanelDragColumns = 10
)

// sidePanelDragSize is wide enough for `auto` to show the panel (137 columns) with room to widen it.
var sidePanelDragSize = tuitest.Size{W: 160, H: 30}

// TestE2ESidePanelDragResizesAndPersists drags the separator ten columns left on a 160-column
// window: the frame repaints the panel ten columns wider, and the release writes the new width as
// `ui.side-panel.width` in the home's config.yaml.
func TestE2ESidePanelDragResizesAndPersists(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, stubllm.Script{Model: "drag-model", Turns: []stubllm.Turn{
		{Repeat: true, Text: "Nothing to do."},
	}})
	drv := tuitest.NewDriver(t, sidePanelDragSize)
	sess := launchTUIConfigured(t, drv, stub, sidePanelAutoConfig)
	waitIdle(drv)
	drv.WaitQuiet(settled)

	from := sidePanelDragSize.W - sidePanelDefaultWidth
	to := from - sidePanelDragColumns
	const row = 0 // the panel's top row: inside the grip's rows, above the slot bottom
	if !separatorAt(drv.Frame(), from, row) {
		t.Fatalf("no separator at column %d before the drag:\n%s", from, drv.Frame())
	}

	drv.Press(tuitest.Click(from, row))
	drv.Press(tuitest.Drag(to, row))
	drv.WaitFor(func() bool { return separatorAt(drv.Frame(), to, row) },
		tuitest.Awaiting(fmt.Sprintf("the separator to repaint at column %d while the button is held", to)))
	drv.Press(tuitest.Release(to, row))

	wantWidth := sidePanelDefaultWidth + sidePanelDragColumns
	awaitConfigCarries(t, drv, sess.Home(), fmt.Sprintf("width: %d", wantWidth))
	drv.WaitQuiet(settled)

	frame := drv.Frame()
	if separatorAt(frame, from, row) {
		t.Errorf("the separator still stands at column %d after the drag:\n%s", from, frame)
	}
	if !separatorAt(frame, to, row) {
		t.Errorf("the separator does not stand at column %d after the release:\n%s", to, frame)
	}
	cfg, err := os.ReadFile(filepath.Join(sess.Home(), "config.yaml"))
	if err != nil {
		t.Fatalf("read the home's config.yaml: %v", err)
	}
	if body := string(cfg); !strings.Contains(body, "side-panel:") ||
		strings.Count(body, fmt.Sprintf("width: %d", wantWidth)) != 1 {
		t.Errorf("config.yaml does not carry `ui.side-panel.width: %d` once after the release:\n%s",
			wantWidth, cfg)
	}
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// separatorAt reports whether the cell at (x, y) holds the side panel's separator.
func separatorAt(frame tuitest.Frame, x, y int) bool {
	return frame.Cell(x, y).Rune == sidePanelDragRule
}

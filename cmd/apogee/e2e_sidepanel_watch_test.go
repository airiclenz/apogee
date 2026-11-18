package main

// The side panel's width while watching another session (ADR 0103 decision 5, ADR 0111), driven
// through the terminal: a watch is read-only toward the watched session, but `ui.side-panel.width`
// is the viewer's own layout, so the separator drag and `/panel width reset` reach the home's
// config.yaml exactly as they do outside a watch. The Model-level tests in internal/tui pin the gate
// and the grip's double-click on a stubbed clock; this journey proves a real watch, opened by
// `--resume <id>` on a session another instance holds, lets both through.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// sidePanelWatchTitle is the title the watched session's journal frame carries, which the watch's
// legend names in the prompt box.
const sidePanelWatchTitle = "a watched run"

// sidePanelWidthKey is the line `ui.side-panel.width` writes into the side-panel block of
// config.yaml; a reset removes it.
const sidePanelWidthKey = "width:"

// TestE2ESidePanelDragResizesWhileWatching opens watching a session another instance runs, drags
// the separator ten columns left — the frame repaints the panel wider and the release writes
// `ui.side-panel.width` — and then a typed `/panel width reset` returns the separator to the
// registry default's column and removes the width line from config.yaml.
func TestE2ESidePanelDragResizesWhileWatching(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, stubllm.Script{Model: "drag-model", Turns: []stubllm.Turn{
		{Repeat: true, Text: "Nothing to do."},
	}})
	home := e2eHome(t, stub)
	appendHomeConfig(t, home, sidePanelAutoConfig)
	store := session.NewStore(filepath.Join(home, sessionsDirName))
	id := session.NewID(time.Now())
	runElsewhere(t, store, id, sidePanelWatchTitle, "", "")
	drv := tuitest.NewDriver(t, sidePanelDragSize)
	sess := launchTUIOn(t, drv, stub, home, "", "--resume", id)
	drv.WaitText("watching " + sidePanelWatchTitle)
	drv.WaitQuiet(settled)

	from := sidePanelDragSize.W - domain.DefaultSidePanelWidth
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
	awaitConfigCarries(t, drv, sess.Home(),
		fmt.Sprintf("%s %d", sidePanelWidthKey, domain.DefaultSidePanelWidth+sidePanelDragColumns))

	submit(drv, "/panel width reset")
	drv.WaitFor(func() bool { return separatorAt(drv.Frame(), from, row) },
		tuitest.Awaiting(fmt.Sprintf("the reset separator to repaint at column %d", from)))
	drv.WaitFor(func() bool {
		cfg, err := os.ReadFile(filepath.Join(sess.Home(), "config.yaml"))
		return err == nil && !strings.Contains(string(cfg), sidePanelWidthKey)
	}, tuitest.Awaiting("the reset to remove ui.side-panel.width from the home's config.yaml"))

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

package main

// The Tools umbrella's fold end to end (plan "2026-09-17 - 01", part A): a large umbrella — at rest
// with more type rows than `ui.tools-fold-over` — folds to its header line under the shared
// `ui.tools-open` preference, a click on that header opens it and writes the preference back to the
// home's config.yaml, and a session resumed with `--continue` paints the replayed umbrella the way
// the file says. Every seam below this is pinned one layer down — the keys in internal/config, the
// fold and its paint in internal/tui, the write-back through the settings host — and none of them
// proves the ROPE: the config resolve, the composition root's inversion onto tui.Options, the
// transcript's seed and the write that the next launch reads all have to hold together, and only
// the binary shows that they do.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The prompt toolsfold.yaml answers, the wording the run is waited on, and the frame's own spelling
// of the umbrella in each of its shapes.
const (
	toolsFoldPrompt = "Survey the workspace."
	toolsFoldWrapUp = "The workspace is surveyed."
	// toolsFoldHeader is the umbrella's counted header for the fixture's three calls, as the frame
	// spells it without a glyph: the shape a SMALL umbrella keeps (no indicator at all), and the
	// prefix a large one wears its ▶/▼ after.
	toolsFoldHeader = "✦ Tools (3 calls)"
	// The large umbrella's two headers: ▼ open on its type rows, ▶ folded to the header alone.
	toolsFoldHeaderOpen   = toolsFoldHeader + " ▼"
	toolsFoldHeaderFolded = toolsFoldHeader + " ▶"
	// toolsFoldReadRow is the first type row's head — its branch marker and label — the row a folded
	// umbrella hides and an open one paints. It is asserted by its head rather than as a whole row,
	// because the row's right edge carries the run's aggregate and its own ▶, neither of which this
	// run is about; the marker is what tells the row from the word wherever else it may fall.
	toolsFoldReadRow = "┝ Read"
	// The three configs under test: a threshold the fixture's three type rows exceed, the same with
	// the preference opened, and a threshold of 0, under which no umbrella is ever large.
	toolsFoldOverTwoConfig  = "ui:\n  tools-fold-over: 2\n"
	toolsFoldOverZeroConfig = "ui:\n  tools-fold-over: 0\n"
)

// toolsFoldFrame drives one run to the umbrella at rest — the prompt sent, the wrap-up painted, the
// screen quiet — and returns the session so the caller can go on with it.
func toolsFoldFrame(t *testing.T, config string) (*tuitest.Driver, *e2eSession) {
	t.Helper()

	stub := stubllm.New(t, loadScript(t, "toolsfold"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, config)

	submit(drv, toolsFoldPrompt)
	drv.WaitText(toolsFoldWrapUp)
	drv.WaitQuiet(settled)
	return drv, sess
}

// TestE2EToolsUmbrellaFoldsAtRestOverThreshold drives the fold from the FILE: with
// `ui.tools-fold-over: 2` an umbrella of three type rows comes to rest folded to
// `✦ Tools (3 calls) ▶` with no type row beneath it — the default `ui.tools-open: false` — and a
// click on that header opens it onto its rows under a ▼. The folded frame is recorded as a golden,
// since the fold is a paint claim and the header's own spelling is what a reader sees.
func TestE2EToolsUmbrellaFoldsAtRestOverThreshold(t *testing.T) {
	t.Parallel()

	drv, sess := toolsFoldFrame(t, toolsFoldOverTwoConfig)

	frame := drv.Frame().String()
	if !strings.Contains(frame, toolsFoldHeaderFolded) {
		t.Errorf("the frame carries no folded %q header:\n%s", toolsFoldHeaderFolded, frame)
	}
	if strings.Contains(frame, toolsFoldReadRow) {
		t.Errorf("the folded umbrella paints its %q type row:\n%s", toolsFoldReadRow, frame)
	}
	tuitest.Golden(t, "t19-tools-umbrella-folded", drv.Frame(), goldenRedactions(sess)...)

	x, y, ok := drv.Frame().Find(toolsFoldHeaderFolded)
	if !ok {
		t.Fatalf("the frame carries no folded %q header to click:\n%s", toolsFoldHeaderFolded, drv.Frame())
	}
	click(drv, x, y)

	drv.WaitText(toolsFoldHeaderOpen)
	drv.WaitText(toolsFoldReadRow)
	drv.WaitQuiet(settled)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EToolsUmbrellaFoldIsRememberedAcrossAResume is the write-back half: the click that opens
// the folded umbrella records `tools-open: true` in the home's config.yaml with no note in the
// transcript — a landed write is silent — so the session resumed with `--continue` paints the
// replayed umbrella OPEN, per the file rather than per the record (block state is never persisted).
func TestE2EToolsUmbrellaFoldIsRememberedAcrossAResume(t *testing.T) {
	t.Parallel()

	drv, sess := toolsFoldFrame(t, toolsFoldOverTwoConfig)
	x, y, ok := drv.Frame().Find(toolsFoldHeaderFolded)
	if !ok {
		t.Fatalf("the frame carries no folded %q header to click:\n%s", toolsFoldHeaderFolded, drv.Frame())
	}

	click(drv, x, y)

	drv.WaitText(toolsFoldHeaderOpen)
	drv.WaitText(toolsFoldReadRow)
	drv.WaitQuiet(settled)
	// The only sentence the toggle can ever say names its key (`tools-open: not saved: …`), and a
	// landed write says nothing at all.
	if frame := drv.Frame().String(); strings.Contains(frame, "tools-open") {
		t.Errorf("the toggle left a note on the transcript; a landed write is silent:\n%s", frame)
	}
	cfg, err := os.ReadFile(filepath.Join(sess.Home(), "config.yaml"))
	if err != nil {
		t.Fatalf("read the home's config.yaml: %v", err)
	}
	if !strings.Contains(string(cfg), "tools-open: true") {
		t.Errorf("config.yaml does not carry `tools-open: true` after the click:\n%s", cfg)
	}
	if err := sess.Quit(); err != nil {
		t.Fatalf("the first run returned %v; want a clean quit", err)
	}

	next := sess.RelaunchWith("--continue")
	next.WaitText("Send a message")
	next.WaitText(toolsFoldHeaderOpen)
	next.WaitQuiet(settled)

	if frame := next.Frame().String(); !strings.Contains(frame, toolsFoldReadRow) {
		t.Errorf("the resumed umbrella hides its rows; the file says open:\n%s", frame)
	}
	if err := sess.Quit(); err != nil {
		t.Fatalf("the resumed run returned %v; want a clean quit", err)
	}
}

// TestE2EToolsFoldOverZeroNeverFolds pins the threshold's off switch: under `ui.tools-fold-over: 0`
// no umbrella is ever large, so the same three-row umbrella comes to rest OPEN with the header a
// small umbrella always wore — no ▶ and no ▼ after its count. The claim is made on the header LINE
// alone: every type row of an open umbrella carries a ▶/▼ of its own, so a frame-wide search for
// the glyph would trip on rows that are not the header's.
func TestE2EToolsFoldOverZeroNeverFolds(t *testing.T) {
	t.Parallel()

	drv, sess := toolsFoldFrame(t, toolsFoldOverZeroConfig)

	frame := drv.Frame()
	_, y, ok := frame.Find(toolsFoldHeader)
	if !ok {
		t.Fatalf("the frame carries no %q header:\n%s", toolsFoldHeader, frame)
	}
	line := frame.Row(y)
	if strings.Contains(line, "▶") || strings.Contains(line, "▼") {
		t.Errorf("the header line wears a fold glyph under tools-fold-over: 0: %q", line)
	}
	if !strings.Contains(frame.String(), toolsFoldReadRow) {
		t.Errorf("the umbrella hides its %q type row under tools-fold-over: 0:\n%s", toolsFoldReadRow, frame)
	}
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

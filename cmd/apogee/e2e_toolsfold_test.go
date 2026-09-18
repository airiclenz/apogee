package main

// The Tools umbrella's fold end to end (plan "2026-09-17 - 01", part A, amended by "2026-09-18 -
// 01"): an umbrella is large by its SIZE alone — more type rows than `ui.tools-fold-over`, whether
// the Turn is running or finished — and a large one folds to its header line under the shared
// `ui.tools-open` preference; a click on that header opens it and writes the preference back to
// the home's config.yaml, and a session resumed with `--continue` paints the replayed umbrella the
// way the file says. A SMALL umbrella folds too, on its own session-only flag that reaches no file.
// Every seam below this is pinned one layer down — the keys in internal/config, the fold and its
// paint in internal/tui, the write-back through the settings host — and none of them proves the
// ROPE: the config resolve, the composition root's inversion onto tui.Options, the transcript's
// seed and the write that the next launch reads all have to hold together, and only the binary
// shows that they do.

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
	// toolsFoldHeader is the umbrella's counted header for the fixture's three calls, the prefix
	// EVERY umbrella — small or large — wears its fold's ▶/▼ after.
	toolsFoldHeader = "✦ Tools (3 calls)"
	// The umbrella's two headers: ▼ open on its type rows, ▶ folded to the header alone.
	toolsFoldHeaderOpen   = toolsFoldHeader + " ▼"
	toolsFoldHeaderFolded = toolsFoldHeader + " ▶"
	// toolsFoldReadRow is the first type row's head — its branch marker and label — the row a folded
	// umbrella hides and an open one paints. It is asserted by its head rather than as a whole row,
	// because the row's right edge carries the run's aggregate and its own ▶, neither of which this
	// run is about; the marker is what tells the row from the word wherever else it may fall.
	toolsFoldReadRow = "┝ Read"
	// toolsFoldTerminalRow is the gated call's type row in toolsfold-live.yaml's umbrella: the row
	// that is still LIVE while the approval pane holds the Turn open, and that an open umbrella paints
	// beneath the three landed reads. It is the umbrella's LAST type row, so its branch marker is the
	// closing ┕ rather than the ┝ the rows above it wear.
	toolsFoldTerminalRow = "┕ Terminal"
	// The live fixture's umbrella, counted for its four calls and matched by its TAIL — `Tools (4
	// calls)` and the fold's glyph — without the star cell before it. The approval pane freezes the
	// blink (spinner.go foldSpinnerTick drops its ticks outside stateRunning), so the header's ✦ is
	// caught in whichever phase the pane found it, lit or blank, and an assertion that spelled the
	// star would be right half the time.
	toolsFoldLiveHeaderFolded = "Tools (4 calls) ▶"
	toolsFoldLiveHeaderOpen   = "Tools (4 calls) ▼"
	// The two configs under test: a threshold the fixture's three type rows exceed, and a threshold
	// of 0, under which no umbrella is ever large. The small fold runs on the DEFAULT, no `ui:` at all.
	toolsFoldOverTwoConfig  = "ui:\n  tools-fold-over: 2\n"
	toolsFoldOverZeroConfig = "ui:\n  tools-fold-over: 0\n"
)

// toolsFoldFrame drives one run to its wrap-up — the prompt sent, the three reads landed, the
// wrap-up painted, the screen quiet — and returns the session so the caller can go on with it.
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

// TestE2EToolsUmbrellaFoldsOverThreshold drives the fold from the FILE: with
// `ui.tools-fold-over: 2` an umbrella of three type rows stands folded to `✦ Tools (3 calls) ▶`
// with no type row beneath it — the default `ui.tools-open: false` — and a click on that header
// opens it onto its rows under a ▼. The folded frame is recorded as a golden, since the fold is a
// paint claim and the header's own spelling is what a reader sees.
func TestE2EToolsUmbrellaFoldsOverThreshold(t *testing.T) {
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
// no umbrella is ever large, so the same three-row umbrella comes to rest OPEN — a small umbrella
// starts every session open — wearing the ▼ every open umbrella's header wears after its count. The
// claim is made on the header LINE alone: every type row of an open umbrella carries a ▶/▼ of its
// own, so a frame-wide search for the glyph would trip on rows that are not the header's.
func TestE2EToolsFoldOverZeroNeverFolds(t *testing.T) {
	t.Parallel()

	drv, sess := toolsFoldFrame(t, toolsFoldOverZeroConfig)

	frame := drv.Frame()
	_, y, ok := frame.Find(toolsFoldHeader)
	if !ok {
		t.Fatalf("the frame carries no %q header:\n%s", toolsFoldHeader, frame)
	}
	line := frame.Row(y)
	if !strings.Contains(line, toolsFoldHeaderOpen) {
		t.Errorf("the header line under tools-fold-over: 0 = %q; want the open small umbrella's %q", line, toolsFoldHeaderOpen)
	}
	if strings.Contains(line, "▶") {
		t.Errorf("the header line stands folded under tools-fold-over: 0: %q", line)
	}
	if !strings.Contains(frame.String(), toolsFoldReadRow) {
		t.Errorf("the umbrella hides its %q type row under tools-fold-over: 0:\n%s", toolsFoldReadRow, frame)
	}
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// toolsFoldLiveSize is the window the mid-Turn run is driven at: e2eSize's width with ten rows more.
// The approval pane takes its rows FROM the transcript (model.go View: every overlay shrinks the
// viewport rather than covering it), and at e2eSize's 30 rows the thirteen the pane leaves hold the
// banner, the notice and the prompt up to the umbrella's header and not one row further — the four
// type rows the opened umbrella paints would scroll out below it, and the claim that they are there
// could not be read off the frame. Ten more rows and the whole opened umbrella stands above the pane.
var toolsFoldLiveSize = tuitest.Size{W: e2eSize.W, H: e2eSize.H + 10}

// TestE2EToolsUmbrellaFoldsWhileTheTurnRuns pins the size-only rule against the one state the
// shipped fold got wrong: a large umbrella whose Turn has NOT come to rest. toolsfold-live.yaml
// follows the three reads with a gated `terminal` call, so the approval pane holds the Turn open
// with the umbrella's fourth type row still live — and under `ui.tools-fold-over: 2` that umbrella
// stands folded to `Tools (4 calls) ▶` with no type row beneath it all the same. A click on the
// header, pane and all, opens it onto its four rows, the live Terminal row among them; the
// decision then lets the Turn wrap up. The header is matched by its tail (toolsFoldLiveHeaderFolded)
// because the pane freezes the star's blink phase.
func TestE2EToolsUmbrellaFoldsWhileTheTurnRuns(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "toolsfold-live"))
	drv := tuitest.NewDriver(t, toolsFoldLiveSize)
	sess := launchTUIConfigured(t, drv, stub, toolsFoldOverTwoConfig)

	submit(drv, toolsFoldPrompt)
	pane := awaitApprovalPane(drv)

	if frame := pane.String(); !strings.Contains(frame, toolsFoldLiveHeaderFolded) {
		t.Errorf("the frame under the pane carries no folded %q header:\n%s", toolsFoldLiveHeaderFolded, frame)
	}
	if frame := pane.String(); strings.Contains(frame, toolsFoldReadRow) {
		t.Errorf("the folded live umbrella paints its %q type row:\n%s", toolsFoldReadRow, frame)
	}
	x, y, ok := pane.Find(toolsFoldLiveHeaderFolded)
	if !ok {
		t.Fatalf("the frame carries no folded %q header to click:\n%s", toolsFoldLiveHeaderFolded, pane)
	}

	click(drv, x, y)

	drv.WaitText(toolsFoldLiveHeaderOpen)
	drv.WaitText(toolsFoldReadRow)
	drv.WaitText(toolsFoldTerminalRow)
	drv.WaitQuiet(settled)
	if frame := drv.Frame().String(); !strings.Contains(frame, approvalMarker) {
		t.Errorf("the click on the header took the pane down; the decision is still owed:\n%s", frame)
	}

	// The denial is the Turn's last result, and the wrap-up answers it.
	decide(drv, "d")
	drv.WaitText(toolsFoldWrapUp)
	drv.WaitQuiet(settled)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EToolsUmbrellaSmallFoldsAlone is the small umbrella's own fold: at the default threshold
// (no `ui:` at all) the three-row umbrella comes to rest OPEN as `✦ Tools (3 calls) ▼`, a click on
// that header folds it to `▶` with no type row beneath it, and the home's config.yaml carries no
// `tools-open` line afterwards — a small umbrella's fold is a session-only flag, never the shared
// preference and never a write.
func TestE2EToolsUmbrellaSmallFoldsAlone(t *testing.T) {
	t.Parallel()

	drv, sess := toolsFoldFrame(t, "")
	frame := drv.Frame()
	_, y, ok := frame.Find(toolsFoldHeader)
	if !ok {
		t.Fatalf("the frame carries no %q header:\n%s", toolsFoldHeader, frame)
	}
	if line := frame.Row(y); !strings.Contains(line, toolsFoldHeaderOpen) {
		t.Fatalf("the header line at the default threshold = %q; want the open small umbrella's %q", line, toolsFoldHeaderOpen)
	}
	if !strings.Contains(frame.String(), toolsFoldReadRow) {
		t.Fatalf("the open small umbrella hides its %q type row:\n%s", toolsFoldReadRow, frame)
	}
	x, y, ok := frame.Find(toolsFoldHeaderOpen)
	if !ok {
		t.Fatalf("the frame carries no open %q header to click:\n%s", toolsFoldHeaderOpen, frame)
	}

	click(drv, x, y)

	drv.WaitText(toolsFoldHeaderFolded)
	drv.WaitGone(toolsFoldReadRow)
	drv.WaitQuiet(settled)
	cfg, err := os.ReadFile(filepath.Join(sess.Home(), "config.yaml"))
	if err != nil {
		t.Fatalf("read the home's config.yaml: %v", err)
	}
	if strings.Contains(string(cfg), "tools-open") {
		t.Errorf("config.yaml carries a `tools-open` line after a small umbrella's fold:\n%s", cfg)
	}
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

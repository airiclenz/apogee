package main

// The settled Tools umbrella's auto-collapse end to end (plan "2026-11-18 - 00", item 9): under the
// DEFAULT config — `ui.tools-auto-collapse: true`, no other `ui:` key — an umbrella whose calls have
// all returned and that a later entry follows folds to its Group summary row whatever its size; one
// the reader has opened something in stays as the reader left it through the next reply; a session
// resumed with `--continue` paints its replayed, settled umbrellas folded; a failed call keeps its
// count on the folded header; and `tools-auto-collapse: false` hands the fold back to the size rule
// and `ui.tools-open`. The settledness, the touched exemption and the live apply are pinned in
// internal/tui; only the binary shows that the config default, the composition root's inversion onto
// tui.Options and the transcript's seed hold together.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The prompts toolsautocollapse.yaml answers beyond the survey (whose prompt, wrap-up, header and
// type row are e2e_toolsfold_test.go's, since the survey issues toolsfold.yaml's three calls), and
// the replies the runs are waited on.
const (
	autoCollapseMissingPrompt = "Read the missing file."
	autoCollapseMissingWrapUp = "The file is not there."
	autoCollapseNextPrompt    = "Anything else?"
	autoCollapseNextReply     = "Nothing more to report."
	// autoCollapseFailedHeader is the missing-file umbrella's Group summary: one read that failed
	// and one listing, the failure counted in the summary's outcome half.
	autoCollapseFailedHeader = "✦ Read 1 file, listed 1 dir · 1 failed"
	// autoCollapseOffConfig switches the collapse off at the default threshold, under which the
	// survey's three type rows make a SMALL umbrella.
	autoCollapseOffConfig = "ui:\n  tools-auto-collapse: false\n"
	// autoCollapseOffLargeOpenConfig switches the collapse off with a threshold the survey's three
	// type rows exceed and the shared preference set to open, so the umbrella is LARGE and its fold
	// is `ui.tools-open`'s.
	autoCollapseOffLargeOpenConfig = "ui:\n  tools-fold-over: 2\n  tools-open: true\n  tools-auto-collapse: false\n"
)

// autoCollapseFailedFolded is the missing-file umbrella folded to its Group summary.
var autoCollapseFailedFolded = umbrellaHeaderMatch{text: autoCollapseFailedHeader, glyph: "▶"}

// autoCollapseTallSize is the window the touched run is driven at: e2eSize's width with ten rows
// more, so the opened umbrella's three type rows, the wrap-up, the second prompt and its reply all
// stand on one screen without scrolling the umbrella's header off the top.
var autoCollapseTallSize = tuitest.Size{W: e2eSize.W, H: e2eSize.H + 10}

// autoCollapseSurvey launches a run of toolsautocollapse.yaml at size under config and drives the
// survey to its wrap-up, returning the session so the caller can go on with it.
func autoCollapseSurvey(t *testing.T, size tuitest.Size, config string) (*tuitest.Driver, *e2eSession) {
	t.Helper()

	stub := stubllm.New(t, loadScript(t, "toolsautocollapse"))
	drv := tuitest.NewDriver(t, size)
	sess := launchTUIConfigured(t, drv, stub, config)

	submit(drv, toolsFoldPrompt)
	drv.WaitText(toolsFoldWrapUp)
	waitIdle(drv)
	return drv, sess
}

// quitCleanly ends the run and fails the test unless it returned cleanly.
func quitCleanly(t *testing.T, sess *e2eSession) {
	t.Helper()

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EToolsAutoCollapseFoldsASettledUmbrella is the default's own claim: the survey's umbrella —
// SMALL at the default threshold, so the size rule alone would stand it open — comes to rest folded
// to `✦ Read 1 file, listed 1 dir, searched 1 pattern ⋯ ▶` once its wrap-up follows it, with no
// type row beneath it.
func TestE2EToolsAutoCollapseFoldsASettledUmbrella(t *testing.T) {
	t.Parallel()

	drv, sess := autoCollapseSurvey(t, e2eSize, "")

	toolsFoldHeaderFolded.await(drv)
	if frame := drv.Frame().String(); strings.Contains(frame, toolsFoldReadRow) {
		t.Errorf("the settled umbrella paints its %q type row:\n%s", toolsFoldReadRow, frame)
	}
	quitCleanly(t, sess)
}

// TestE2EToolsAutoCollapseSparesATouchedUmbrella is the reader's exemption: a click on the settled
// umbrella's folded header opens it onto its rows, and it stays open through the next reply — the
// collapse never folds an umbrella the reader has touched. Opening an auto-collapsed umbrella is
// view state of that umbrella alone, so the home's config.yaml carries no `tools-open` line.
func TestE2EToolsAutoCollapseSparesATouchedUmbrella(t *testing.T) {
	t.Parallel()

	drv, sess := autoCollapseSurvey(t, autoCollapseTallSize, "")
	toolsFoldHeaderFolded.await(drv)
	x, y, ok := toolsFoldHeaderFolded.find(drv.Frame())
	if !ok {
		t.Fatalf("the frame carries no folded %q header to click:\n%s", toolsFoldHeaderFolded, drv.Frame())
	}

	click(drv, x, y)
	toolsFoldHeaderOpen.await(drv)
	drv.WaitText(toolsFoldReadRow)

	submit(drv, autoCollapseNextPrompt)
	drv.WaitText(autoCollapseNextReply)
	waitIdle(drv)

	frame := drv.Frame()
	if !toolsFoldHeaderOpen.in(frame) {
		t.Errorf("the touched umbrella does not stand open as %q after the next reply:\n%s", toolsFoldHeaderOpen, frame)
	}
	if !strings.Contains(frame.String(), toolsFoldReadRow) {
		t.Errorf("the touched umbrella hides its %q type row after the next reply:\n%s", toolsFoldReadRow, frame)
	}
	cfg, err := os.ReadFile(filepath.Join(sess.Home(), "config.yaml"))
	if err != nil {
		t.Fatalf("read the home's config.yaml: %v", err)
	}
	if strings.Contains(string(cfg), "tools-open") {
		t.Errorf("config.yaml carries a `tools-open` line after opening an auto-collapsed umbrella:\n%s", cfg)
	}
	quitCleanly(t, sess)
}

// TestE2EToolsAutoCollapseFoldsAResumedUmbrella is the replay half: the session resumed with
// `--continue` paints its replayed umbrella — every call returned, the wrap-up after it — folded to
// its Group summary, with no type row beneath it.
func TestE2EToolsAutoCollapseFoldsAResumedUmbrella(t *testing.T) {
	t.Parallel()

	drv, sess := autoCollapseSurvey(t, e2eSize, "")
	toolsFoldHeaderFolded.await(drv)
	quitCleanly(t, sess)

	next := sess.RelaunchWith("--continue")
	next.WaitText(toolsFoldWrapUp)
	toolsFoldHeaderFolded.await(next)
	waitIdle(next)

	if frame := next.Frame().String(); strings.Contains(frame, toolsFoldReadRow) {
		t.Errorf("the resumed, settled umbrella paints its %q type row:\n%s", toolsFoldReadRow, frame)
	}
	quitCleanly(t, sess)
}

// TestE2EToolsAutoCollapseCountsAFailure is the folded header's outcome half: an umbrella holding a
// read of a path the workspace lacks folds, once settled, to
// `✦ Read 1 file, listed 1 dir · 1 failed ⋯ ▶` — the collapse hides the failed call's row but never
// its count.
func TestE2EToolsAutoCollapseCountsAFailure(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "toolsautocollapse"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	submit(drv, autoCollapseMissingPrompt)
	drv.WaitText(autoCollapseMissingWrapUp)
	autoCollapseFailedFolded.await(drv)
	waitIdle(drv)

	if frame := drv.Frame().String(); strings.Contains(frame, toolsFoldReadRow) {
		t.Errorf("the settled umbrella paints its %q type row:\n%s", toolsFoldReadRow, frame)
	}
	quitCleanly(t, sess)
}

// TestE2EToolsAutoCollapseOffKeepsTheSizeRule is the switch: under `tools-auto-collapse: false` the
// settled umbrella's fold is the size rule's again. At the default threshold the survey's umbrella
// is small and stands open; over `tools-fold-over: 2` it is large and follows `tools-open: true`,
// standing open too — where the default switch would have folded both.
func TestE2EToolsAutoCollapseOffKeepsTheSizeRule(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		config string
	}{
		{name: "a small umbrella stands open", config: autoCollapseOffConfig},
		{name: "a large umbrella follows tools-open", config: autoCollapseOffLargeOpenConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			drv, sess := autoCollapseSurvey(t, e2eSize, tc.config)

			toolsFoldHeaderOpen.await(drv)
			drv.WaitText(toolsFoldReadRow)
			quitCleanly(t, sess)
		})
	}
}

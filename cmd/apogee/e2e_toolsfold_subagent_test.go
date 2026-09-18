package main

// The Tools umbrella's fold INSIDE a run view (plan "2026-09-18 - 01", item 5): an umbrella is
// large by its size alone at every depth, so the one a delegated child builds — painted only when
// its run is opened as a view, rooted at the child — folds to its header under the shared
// `ui.tools-open` exactly as a top-level one does: with the run finished, with the parent's Turn
// still open on the child's approval pane, and replayed after a `--continue`. This is the shape the
// shipped fold got wrong: `umbrellaIsLarge` once asked whether the Turn was busy and every member
// done, and a >5-row umbrella ordinarily appears nowhere but inside a run opened while its parent
// works. e2e_toolsfold_test.go pins the same rule in the conversation; nothing below the binary
// proves that a view's rooted paint reaches the fold with the same answer.

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The two prompts toolsfold-subagent.yaml answers, the child they delegate to, and the wordings
// the runs are waited on. The umbrella's own spellings are e2e_toolsfold_test.go's: the child's
// three reads are toolsfold.yaml's three calls, so its header counts the same `(3 calls)`, and the
// gated task's four the same `(4 calls)` as toolsfold-live.yaml's.
const (
	// The finished shape's prompt and the gated one's — the second matched first in the fixture,
	// since the first is its prefix.
	toolsFoldDelegatePrompt = "Delegate the survey."
	toolsFoldGatedPrompt    = "Delegate the survey and the check."
	// The child, as the sub_agent call names it; the crumb the view wears is derived from it the
	// way the run view derives its own (`← main › <name>`), restated here because cmd/apogee
	// cannot import the constant it comes from.
	toolsFoldChild        = "surveyor"
	toolsFoldRunViewCrumb = "← main › " + toolsFoldChild
	// toolsFoldDelegationRow is the head of the delegation's collapsed row — the ┕ of the one-member
	// Sub-Agent list and the child's name — the row a click opens as a run view. It is matched by
	// its head rather than by the name alone because the approval pane names the child too
	// (`Sub-agent: surveyor — …`), and a click aimed at that row would land on the pane.
	toolsFoldDelegationRow = "┕ " + toolsFoldChild
	// The parent's wrap-up, once the child has reported: the run is over.
	toolsFoldDelegateWrapUp = "The surveyor has reported."
)

// toolsFoldDelegatedRun drives the finished shape to its wrap-up under `ui.tools-fold-over: 2` —
// the delegation sent, the child's three reads landed and reported, the parent's wrap-up painted,
// the screen quiet — and returns the session so the caller can go on with it.
func toolsFoldDelegatedRun(t *testing.T) (*tuitest.Driver, *e2eSession) {
	t.Helper()

	stub := stubllm.New(t, loadScript(t, "toolsfold-subagent"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, toolsFoldOverTwoConfig)

	submit(drv, toolsFoldDelegatePrompt)
	drv.WaitText(toolsFoldDelegateWrapUp)
	drv.WaitQuiet(settled)
	return drv, sess
}

// assertRunViewUmbrellaFolded asserts the one claim every run below makes on the opened view: the
// child's umbrella stands folded to its header — matched by its tail, folded, so the same assertion
// holds at either star phase — with no type row beneath it.
func assertRunViewUmbrellaFolded(t *testing.T, f tuitest.Frame, header string) {
	t.Helper()

	frame := f.String()
	if !strings.Contains(frame, toolsFoldRunViewCrumb) {
		t.Fatalf("the run view is not open; no %q crumb:\n%s", toolsFoldRunViewCrumb, frame)
	}
	if !strings.Contains(frame, header) {
		t.Errorf("the run view carries no folded %q header:\n%s", header, frame)
	}
	if strings.Contains(frame, toolsFoldReadRow) {
		t.Errorf("the folded umbrella inside the run view paints its %q type row:\n%s", toolsFoldReadRow, frame)
	}
}

// TestE2EToolsUmbrellaFoldsInsideARunView is the finished shape: with the run over, ⌥↑ ⏎ on the
// delegation opens its view, and the child's three-row umbrella stands there folded to
// `✦ Tools (3 calls) ▶` under the default `ui.tools-open: false`; a click on that header opens it
// onto its rows under a ▼, and esc walks back up to the conversation.
func TestE2EToolsUmbrellaFoldsInsideARunView(t *testing.T) {
	t.Parallel()

	drv, sess := toolsFoldDelegatedRun(t)

	openLastRun(drv)
	drv.WaitText(toolsFoldRunViewCrumb)
	drv.WaitQuiet(settled)

	assertRunViewUmbrellaFolded(t, drv.Frame(), toolsFoldHeaderFolded)
	x, y, ok := drv.Frame().Find(toolsFoldHeaderFolded)
	if !ok {
		t.Fatalf("the run view carries no folded %q header to click:\n%s", toolsFoldHeaderFolded, drv.Frame())
	}

	click(drv, x, y)

	drv.WaitText(toolsFoldHeaderOpen)
	drv.WaitText(toolsFoldReadRow)
	drv.WaitQuiet(settled)
	if frame := drv.Frame().String(); !strings.Contains(frame, toolsFoldRunViewCrumb) {
		t.Errorf("the click on the header left the run view:\n%s", frame)
	}
	drv.Press(tuitest.Esc)
	drv.WaitGone(toolsFoldRunViewCrumb)
	drv.WaitText(toolsFoldDelegateWrapUp)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EToolsUmbrellaFoldsInsideARunViewWhileTheTurnRuns is the live shape: the child's Turn ends
// in a gated `terminal` call, so the approval pane holds the parent's Turn open with the child's
// umbrella at four type rows, the last of them live. The view is opened over that pane by a CLICK
// on the delegation's row — ⌥↑ never enters the walk while a decision is pending
// (blockCursorOwnsKeys) and ⏎ belongs to the pane's rows — and the umbrella stands folded to
// `Tools (4 calls) ▶` all the same. The header is matched by its tail for
// e2e_toolsfold_test.go's reason: the pane freezes the star's blink phase.
//
// The decision is taken before esc, not after the wrap-up: inside a view esc means "one level up"
// only while no pane waits for an answer (runViewOwnsEsc), and the view is rooted at the child, so
// the parent's wrap-up never paints inside it — the walk back up has to come first.
func TestE2EToolsUmbrellaFoldsInsideARunViewWhileTheTurnRuns(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "toolsfold-subagent"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, toolsFoldOverTwoConfig)

	submit(drv, toolsFoldGatedPrompt)
	pane := awaitApprovalPane(drv)
	x, y, ok := pane.Find(toolsFoldDelegationRow)
	if !ok {
		t.Fatalf("the frame under the pane carries no %q row to click:\n%s", toolsFoldDelegationRow, pane)
	}

	click(drv, x, y)

	drv.WaitText(toolsFoldRunViewCrumb)
	drv.WaitQuiet(settled)
	opened := drv.Frame()
	assertRunViewUmbrellaFolded(t, opened, toolsFoldLiveHeaderFolded)
	if frame := opened.String(); !strings.Contains(frame, approvalMarker) {
		t.Errorf("the click on the delegation took the pane down; the decision is still owed:\n%s", frame)
	}

	// The denial is the child's last result; its report and the parent's wrap-up answer it.
	decide(drv, "d")
	drv.Press(tuitest.Esc)
	drv.WaitGone(toolsFoldRunViewCrumb)
	drv.WaitText(toolsFoldDelegateWrapUp)
	drv.WaitQuiet(settled)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EToolsUmbrellaFoldInARunViewSurvivesAResume is the replayed shape: the session resumed
// with `--continue` rebuilds the child's run from the record, and its umbrella — never opened, so
// the file still says the default `ui.tools-open: false` — stands folded inside the reopened view
// exactly as it did live. Block state is never persisted; the fold is re-derived from the size and
// the file.
func TestE2EToolsUmbrellaFoldInARunViewSurvivesAResume(t *testing.T) {
	t.Parallel()

	_, sess := toolsFoldDelegatedRun(t)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the first run returned %v; want a clean quit", err)
	}

	next := sess.RelaunchWith("--continue")
	next.WaitText("Send a message")
	next.WaitText(toolsFoldDelegateWrapUp)
	next.WaitQuiet(settled)
	openLastRun(next)
	next.WaitText(toolsFoldRunViewCrumb)
	next.WaitQuiet(settled)

	assertRunViewUmbrellaFolded(t, next.Frame(), toolsFoldHeaderFolded)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the resumed run returned %v; want a clean quit", err)
	}
}

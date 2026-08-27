package main

// T-15 of the v0.17.1 release checklist — "tool outcome slots report the tool's data, not its
// prose" — as tests.
//
// It was manual because the claim is about which CELLS carry the scheme's error tone and where a
// diff body draws its elision rule: a colour and a layout judgment nobody could make against a
// View() string. The emulator makes both ordinary assertions — a cell's foreground is the scheme's
// `error` role or it is not, and a row either is the ⋯ rule or holds text — so what is left of the
// item is nothing at all, and the PTY variant at the bottom makes the colour claim a second time
// against the SHIPPED BINARY's own SGR rather than against an in-process renderer's.
//
// The one shape the checklist describes that no driven run reaches is the CANCELLED delegation's
// outcome slot: a live Esc leaves the call open with no verdict at all (see the DEFER note on the
// cancel test below), and the verdict the checklist asks about — `interrupted — the run did not
// finish`, in red, with no ✓ — is written by the REPLAY that closes it. So that half is asserted
// where it exists: on the reopened session.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The prompts testdata/stubllm/outcome.yaml answers, and the wordings the frames are read for. The
// wordings are internal constants (toolview.go's interruptedSummary, subagent.go's
// subAgentFaultPrefix) restated here for the reason the delegation tests restate theirs: they are
// what the checklist promises a human will read, so a rename over there has to fail here.
const (
	doomedPrompt   = "Delegate the doomed survey to a sub-agent."
	cancelPrompt   = "Delegate the outcome survey to a sub-agent."
	terminalPrompt = "Run the terminal command that prints the error line."
	nearEditPrompt = "Edit the near lines of lines.txt."
	farEditPrompt  = "Edit the far lines of lines.txt."

	// doomedTask and cancelTask are the tasks the two delegations carry — the child's own words,
	// and how a child's request is told from the parent's.
	doomedTask = "Count the files and report the number"
	cancelTask = "Survey every file in the workspace"

	faultedSlot   = "sub-agent faulted"
	quotedError   = "error: 3 errors found"
	interruptedIn = "interrupted — the run did not finish"
)

// linesFixture is the twelve-line file the two edits change. Its content is the whole reason the
// elision claim can be made at all: the near edit moves lines 5 and 7, one unchanged line apart, and
// the far edit moves lines 1 and 12, ten apart — either side of the six lines two regions' context
// can tile across (internal/tools' editRegionContext).
var linesFixture = []string{
	"line one", "line two", "line three", "line four", "line five", "line six",
	"line seven", "line eight", "line nine", "line ten", "line eleven", "line twelve",
}

// TestE2EOutcomeSlotsCarryTheToolsVerdict is T-15 steps 6 to 9, plus the tone half of steps 3 and 4
// on the delegation that CAN carry a verdict: one that failed.
//
// The four claims are made in one session because they are one claim about one column — the outcome
// slot says what the tool did, in the tone the tool's own verdict earns, and the body it quotes has
// no vote in either.
func TestE2EOutcomeSlotsCarryTheToolsVerdict(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "outcome"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)
	seedLinesFile(t, sess.Workspace())

	// A FAILED delegation is painted red and wears no ✓ — the verdict rides the branch summary
	// (commit e46bef05), and design call 11 of the tool-layout work makes that red the whole of the
	// failure marking, so a row without it does not read as a failure at all.
	submit(drv, doomedPrompt)
	drv.WaitText(faultedSlot)
	drv.WaitQuiet(settled)
	failed := drv.Frame()
	assertErrorTone(t, failed, faultedSlot)
	assertNoDoneMark(t, failed, faultedSlot)

	// Step 5 — and opening it shows the delegate the prompt it was handed. This one got as far as
	// being refused by its own server, so the run has an answer behind it and the prompt stands at
	// the top of its rail, above that answer (the second half of the checklist's step 5).
	expandLastBlock(drv)
	if opened := drv.Frame(); !strings.Contains(flatten(opened.String()), doomedTask) {
		t.Errorf("the expanded delegation does not show the prompt it carried:\n%s", opened)
	}

	// Steps 6 and 7 — a command that EXITS 0 and quotes the word "error" on its way out. The slot
	// carries the tool's own verdict, and the quoted text never colours it.
	submit(drv, terminalPrompt)
	drv.WaitText(approvalMarker)
	drv.WaitQuiet(settled)
	decide(drv, "a")
	drv.WaitText("That is what the command had to say.")
	drv.WaitQuiet(settled)
	quoted := drv.Frame()
	if !strings.Contains(rowContaining(t, quoted, quotedError), quotedError) {
		t.Errorf("the terminal block does not quote the command's output:\n%s", quoted)
	}
	assertNoErrorTone(t, quoted, quotedError)

	// Step 9, the negative half first: two changed regions ONE unchanged line apart tile end to end,
	// so the body draws no rule anywhere.
	approveEdit(t, drv, nearEditPrompt)
	expandLastBlock(drv)
	if near := scrollTranscript(drv); countRuleRows(near) != 0 {
		t.Errorf("the near edit's diff elides between two regions that meet:\n%s", near)
	}

	// And the positive half: ten unchanged lines is more than the two regions' context can cover, so
	// the rule stands between them and says the lines it stands for were skipped.
	approveEdit(t, drv, farEditPrompt)
	expandLastBlock(drv)
	if far := scrollTranscript(drv); countRuleRows(far) == 0 {
		t.Errorf("the far edit's diff draws no elision rule between regions ten lines apart:\n%s", far)
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EOutcomeCancelledDelegationCarriesTheFailureTone is T-15 steps 2 to 4: a delegation stopped
// with Esc while its child was still working, and what its outcome slot says afterwards.
//
// It says it on the REOPENED session, and that is a finding rather than a convenience. A live Esc
// discards the interrupted Exchange (Model.foldCancelled) and closes nothing: the delegation's call
// is left open — its leader dots run to the row's edge with no verdict in the slot, and the block
// keeps the ✦ that marks work still standing behind it. The verdict the checklist asks to see is
// written by the replay that closes every call a record left open (closeInterruptedCalls), so it
// exists from the reopen onwards and nowhere before it.
func TestE2EOutcomeCancelledDelegationCarriesTheFailureTone(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "outcome"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	// Step 2 — the delegation opens and its child starts working. Waiting for the child's own
	// request is what makes "still running" a fact rather than a likelihood: the script never
	// answers it, so from here until the Esc the delegation cannot possibly have finished.
	submit(drv, cancelPrompt)
	drv.WaitText("survey")
	drv.WaitFor(func() bool { return childRequests(stub, cancelTask) > 0 },
		tuitest.Awaiting("the child's request, which the script never answers"))

	// Step 3 — Esc stops it. The note is the whole of what the live session says about it.
	drv.Press(tuitest.Esc)
	drv.WaitText("cancelled")
	drv.WaitQuiet(settled)
	live := drv.Frame()
	assertNoDoneMark(t, live, "survey")

	// Steps 3 and 4 on the surface that carries them: the reopened session, where the call the
	// record left open is closed as what befell it, in the scheme's error tone, with no ✓ beside it.
	drv.WaitFor(func() bool { return len(sess.sessionRecords()) > 0 },
		tuitest.Awaiting("the cancelled session to reach the store"))
	next := restoreNewestSession(t, sess)

	restored := next.Frame()
	if !strings.Contains(flatten(restored.String()), interruptedIn) {
		t.Fatalf("the reopened delegation's slot does not read %q:\n%s", interruptedIn, restored)
	}
	assertErrorTone(t, restored, interruptedIn)
	assertNoDoneMark(t, restored, interruptedIn)
	tuitest.Golden(t, "t15-cancelled-delegation", restored, goldenRedactions(sess)...)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the reopened run returned %v; want a clean quit", err)
	}
}

// TestE2EOutcomeTonePTY is step 4's colour claim through the SHIPPED BINARY: apogee's own SGR
// sequences, written into a real pseudo-terminal, reconstructed by the same emulator. The in-process
// test above proves the renderer asks for the scheme's error role; this one proves what a terminal
// is actually told, which is the half a human at a real keyboard was being asked to judge.
func TestE2EOutcomeTonePTY(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "outcome"))
	sess := launchPTY(t, stub)

	submit(sess.drv, doomedPrompt)
	sess.drv.WaitText(faultedSlot)
	sess.drv.WaitQuiet(settled)

	failed := sess.drv.Frame()
	assertErrorTone(t, failed, faultedSlot)
	assertNoDoneMark(t, failed, faultedSlot)

	if code := sess.drv.Quit(); code != 0 {
		t.Errorf("the binary exited %d; want a clean quit", code)
	}
}

// ----------------------------------------------------------------------------
// The helpers
// ----------------------------------------------------------------------------

// seedLinesFile writes the twelve-line file the two edits change into a run's workspace.
func seedLinesFile(t *testing.T, ws string) {
	t.Helper()

	body := strings.Join(linesFixture, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(ws, "lines.txt"), []byte(body), 0o600); err != nil {
		t.Fatalf("seed the edit fixture: %v", err)
	}
}

// approveEdit sends one edit prompt and allows the write it raises, returning once the model has
// spoken again — which is what says the result reached the transcript and the block is settled.
func approveEdit(t *testing.T, drv *tuitest.Driver, prompt string) {
	t.Helper()

	submit(drv, prompt)
	drv.WaitText(approvalMarker)
	drv.WaitQuiet(settled)
	decide(drv, "a")
	drv.WaitText("The edit is in.")
	drv.WaitQuiet(settled)
}

// assertErrorTone fails unless the row carrying marker holds a cell painted in the colour scheme's
// error role. It is [assertNoErrorTone]'s positive twin, and the two are the whole of what "reads as
// a failure" means since design call 11 made that red the only failure marking.
func assertErrorTone(t *testing.T, f tuitest.Frame, marker string) {
	t.Helper()

	_, y, ok := f.Find(marker)
	if !ok {
		t.Fatalf("no row of the frame holds %q:\n%s", marker, f)
	}
	want := mustColor(t, scheme.Default().Error)
	for _, run := range f.StyleRuns(y) {
		if tuitest.SameColor(run.Style.FG, want) {
			return
		}
	}
	t.Errorf("the row holding %q carries no cell in the scheme's error tone:\n%s", marker, f)
}

// assertNoDoneMark fails when the row carrying marker wears the done ✓. A failed or unfinished run
// must not: the mark says the delegate reported, and the outcome slot beside it says it did not.
func assertNoDoneMark(t *testing.T, f tuitest.Frame, marker string) {
	t.Helper()

	row := rowContaining(t, f, marker)
	if strings.Contains(row, "✓") {
		t.Errorf("the row %q wears the done ✓ beside its failure verdict:\n%s", row, f)
	}
}

// countRuleRows counts the elision rules in a walked transcript: the rows a diff body draws between
// two regions that do NOT meet in the file's numbering, and the whole of what T-15 step 9 is about.
//
// It is asked of a WALK rather than of one frame ([scrollTranscript]) because an expanded diff of a
// twelve-line file is taller than the terminal: the rule between two far-apart regions sits below
// the fold, and a claim made against the visible frame alone would report "no rule" for a body whose
// rule is simply one page further down.
//
// A rule is recognised by being nothing BUT rule: a run of ⋯ with no text anywhere on the row. The
// leader every tool row runs from its target to its outcome slot is made of the same glyph and is
// not one — it has the target on its left and the verdict on its right, which is exactly what this
// predicate refuses.
func countRuleRows(pages string) int {
	n := 0
	for _, row := range strings.Split(pages, "\n") {
		// The scroll rail the frame paints down its right-hand edge is chrome, not content.
		trimmed := strings.Trim(row, " │┃")
		if trimmed == "" {
			continue
		}
		if strings.Count(trimmed, "⋯") == len([]rune(trimmed)) && len([]rune(trimmed)) >= 8 {
			n++
		}
	}
	return n
}

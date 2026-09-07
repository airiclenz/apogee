package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/undo"
)

// ----------------------------------------------------------------------------
// /undo routing (through the Model, over a fake Engine)
// ----------------------------------------------------------------------------

// scriptedStep is the preview a fake engine answers with: one restore, one delete and one skip, so
// every classification the note renders is exercised by a single line. The paths are short on
// purpose — the assertions read them back out of the rendered View, which wraps at the frame width.
func scriptedStep(generation uint64) undo.Step {
	return undo.Step{
		Ordinal:    3,
		Generation: generation,
		Changes: []undo.Change{
			{Path: "/w/a.go", Action: undo.ActionRestore},
			{Path: "/w/new.go", Action: undo.ActionDelete},
			{Path: "/w/b.go", Action: undo.ActionSkip, Reason: "edited since"},
		},
	}
}

// unwrapped folds the frame's soft wraps away — every run of whitespace becomes one space — so a
// note longer than the transcript's width is asserted as the single line its builder wrote.
func unwrapped(view string) string { return strings.Join(strings.Fields(view), " ") }

// runUndoLine drives one /undo or /redo line through the real key path and returns the model plus
// the plain-text View, so a test asserts on what the human actually sees. It takes the model rather
// than building one, because the two-step grammar only means anything across two lines.
func runUndoLine(t *testing.T, m Model, line string) (Model, string) {
	t.Helper()
	m.input.SetValue(line)
	m, cmd := stepCmd(t, m, keyEnter())
	if cmd != nil {
		t.Errorf("%s returned a Cmd; it is synchronous and must not launch a worker", line)
	}
	if m.state != stateIdle {
		t.Errorf("state = %v, want idle (%s must not launch a worker)", m.state, line)
	}
	return m, plain(m.View())
}

// ----------------------------------------------------------------------------
// The grammar (command.go)
// ----------------------------------------------------------------------------

func TestUndoParsesItsTwoForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line    string
		want    undoAction
		wantErr bool
	}{
		{line: "/undo", want: undoPreviewOnly},
		{line: "/undo confirm", want: undoConfirm},
		{line: "/undo yes", wantErr: true},
		{line: "/undo confirm please", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			parsed := parseInput(c.line, nil)

			if parsed.kind != kindCommand || parsed.command != "undo" {
				t.Fatalf("parse = %v/%q, want a kindCommand named undo", parsed.kind, parsed.command)
			}
			if (parsed.err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", parsed.err, c.wantErr)
			}
			if c.wantErr {
				if !strings.Contains(parsed.err.Error(), undoUsage) {
					t.Errorf("the error does not teach the grammar: %v", parsed.err)
				}
				return
			}
			if action := verbArgsOf[undoAction](parsed); action != c.want {
				t.Errorf("action = %v, want %v", action, c.want)
			}
		})
	}
}

// The verb is idle-only in the registry, and argument-taking: the menu's "— idle only" tag and what
// ⏎ does are one rule, and a row that stopped reading its arguments would swallow "confirm".
func TestUndoIsAnIdleOnlyArgumentTakingVerb(t *testing.T) {
	t.Parallel()

	spec, ok := commandByName("undo")
	if !ok {
		t.Fatal("commandSpecs carries no undo row")
	}
	if spec.whileRunning || !spec.takesArgs {
		t.Errorf("commandSpec = %+v, want an idle-only verb that reads its arguments", spec)
	}
}

// ----------------------------------------------------------------------------
// Routing
// ----------------------------------------------------------------------------

func TestUndoPreviewsTheStepAndStashesItsGeneration(t *testing.T) {
	eng := &fakeEngine{undoStep: scriptedStep(7), undoStepOK: true}

	m, view := runUndoLine(t, newTestModelEng(t, eng, testOpts), "/undo")

	if m.undoGeneration != 7 {
		t.Errorf("stashed generation = %d, want 7 — the confirm has nothing to quote", m.undoGeneration)
	}
	if len(eng.undoReverts) != 0 {
		t.Errorf("UndoRevert calls = %v, want none: a preview must touch no file", eng.undoReverts)
	}
	// Every recorded path is disclosed, with what would happen to it: the note IS the
	// authorization surface, so a summary the human cannot check would not be one.
	for _, want := range []string{"exchange 3", "restore", "/w/a.go", "delete", "/w/new.go",
		"skip", "/w/b.go", "edited since", "/undo confirm"} {
		if !strings.Contains(view, want) {
			t.Errorf("preview missing %q:\n%s", want, view)
		}
	}
}

func TestUndoConfirmRevertsAtThePreviewedGeneration(t *testing.T) {
	eng := &fakeEngine{
		undoStep:   scriptedStep(7),
		undoStepOK: true,
		undoReport: undo.Report{
			Ordinal:  3,
			Restored: []string{"/w/a.go"},
			Deleted:  []string{"/w/new.go"},
			Skipped:  []undo.Skipped{{Path: "/w/b.go", Reason: "edited since"}},
		},
	}

	m, _ := runUndoLine(t, newTestModelEng(t, eng, testOpts), "/undo")
	m, view := runUndoLine(t, m, "/undo confirm")

	if len(eng.undoReverts) != 1 || eng.undoReverts[0] != 7 {
		t.Fatalf("UndoRevert calls = %v, want exactly [7] — the confirm must quote the preview", eng.undoReverts)
	}
	// The counts say what happened; the skip is named, because it is the one outcome that leaves
	// the file holding what the agent wrote.
	for _, want := range []string{"undone", "exchange 3", "1 restored", "1 removed", "1 skipped",
		"/w/b.go", "edited since"} {
		if !strings.Contains(view, want) {
			t.Errorf("report missing %q:\n%s", want, view)
		}
	}
	if m.undoGeneration != 0 {
		t.Errorf("stashed generation = %d, want 0 — a spent preview authorises nothing", m.undoGeneration)
	}
}

func TestUndoConfirmOnAStaleGenerationRePreviewsInsteadOfReverting(t *testing.T) {
	eng := &fakeEngine{undoStep: scriptedStep(7), undoStepOK: true}

	m, _ := runUndoLine(t, newTestModelEng(t, eng, testOpts), "/undo")

	// The journal moved between the preview and the confirmation — another exchange wrote files —
	// so the engine refuses the stamp the human is quoting and offers what is on top NOW.
	eng.undoErr = fmt.Errorf("%w: previewed at generation 7, journal is at 9", undo.ErrStaleGeneration)
	eng.undoStep = scriptedStep(9)
	m, view := runUndoLine(t, m, "/undo confirm")

	if len(eng.undoReverts) != 1 || eng.undoReverts[0] != 7 {
		t.Fatalf("UndoRevert calls = %v, want exactly [7]: the refusal must not be retried", eng.undoReverts)
	}
	if m.undoGeneration != 9 {
		t.Errorf("stashed generation = %d, want 9 — the re-preview must be the one confirmable now", m.undoGeneration)
	}
	if !strings.Contains(view, "nothing was undone") {
		t.Errorf("the note does not say the revert did not happen:\n%s", view)
	}
	// It is a fresh preview, not a bare refusal: the human is asked to confirm again.
	for _, want := range []string{"exchange 3", "/w/a.go", "/undo confirm"} {
		if !strings.Contains(view, want) {
			t.Errorf("re-preview missing %q:\n%s", want, view)
		}
	}
}

func TestUndoWithNothingRecordedSaysSoAndNamesTheEnginesReason(t *testing.T) {
	for _, line := range []string{"/undo", "/undo confirm"} {
		for _, reason := range []string{"", "git not found"} {
			t.Run(line+" "+reason, func(t *testing.T) {
				// The empty journal answers both surfaces: a preview reports no step, a revert refuses.
				eng := &fakeEngine{undoStepOK: false, undoErr: undo.ErrNothingToUndo, undoNote: reason}

				m, view := runUndoLine(t, newTestModelEng(t, eng, testOpts), line)

				if m.undoGeneration != 0 {
					t.Errorf("stashed generation = %d, want 0 — there is no step to confirm", m.undoGeneration)
				}
				// The line the human reads is the builder's, whole: with snapshots in force the
				// emptiness stands alone, and without them the engine's reason rides with it in
				// parentheses, so a narrower journal never reads as a lost one.
				if want := undoNothingNote(reason); !strings.Contains(unwrapped(view), want) {
					t.Errorf("note is not %q:\n%s", want, view)
				}
			})
		}
	}
}

func TestUndoArgumentErrorReportsTheUsageLineAndTouchesNothing(t *testing.T) {
	eng := &fakeEngine{undoStep: scriptedStep(7), undoStepOK: true}

	_, view := runUndoLine(t, newTestModelEng(t, eng, testOpts), "/undo sideways")

	if len(eng.undoReverts) != 0 {
		t.Errorf("UndoRevert calls = %v, want none on a parse error", eng.undoReverts)
	}
	if !strings.Contains(view, "sideways") || !strings.Contains(view, "usage:") {
		t.Errorf("the transcript does not teach the grammar after a mistyped line:\n%s", view)
	}
}

// /undo mutates the workspace, so a line typed while the model works earns the standing answer
// instead of running — the group it would revert is the one the running Step is still filling.
func TestUndoIsRefusedWhileTheModelWorks(t *testing.T) {
	eng := &fakeEngine{undoStep: scriptedStep(7), undoStepOK: true}
	m := newTestModelEng(t, eng, testOpts)
	m, _ = typeCommand(t, m, "open the exchange")
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}

	m, _ = typeCommand(t, m, "/undo")

	if m.undoGeneration != 0 {
		t.Errorf("stashed generation = %d, want 0 — the command never ran", m.undoGeneration)
	}
	if got := plain(m.View()); !strings.Contains(got, commandsAtIdleNote) {
		t.Errorf("the refusal note is missing from the transcript:\n%s", got)
	}
}

// ----------------------------------------------------------------------------
// /redo — the same two steps over the stack a confirmed undo fills
// ----------------------------------------------------------------------------

func TestRedoParsesItsTwoForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line    string
		want    undoAction
		wantErr bool
	}{
		{line: "/redo", want: undoPreviewOnly},
		{line: "/redo confirm", want: undoConfirm},
		{line: "/redo yes", wantErr: true},
		{line: "/redo confirm please", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			parsed := parseInput(c.line, nil)

			if parsed.kind != kindCommand || parsed.command != "redo" {
				t.Fatalf("parse = %v/%q, want a kindCommand named redo", parsed.kind, parsed.command)
			}
			if (parsed.err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", parsed.err, c.wantErr)
			}
			if c.wantErr {
				// The error teaches /redo's own grammar, never the verb it mirrors.
				if !strings.Contains(parsed.err.Error(), redoUsage) {
					t.Errorf("the error does not teach the grammar: %v", parsed.err)
				}
				return
			}
			if action := verbArgsOf[undoAction](parsed); action != c.want {
				t.Errorf("action = %v, want %v", action, c.want)
			}
		})
	}
}

// /redo writes to the human's files exactly as /undo does, so it carries /undo's registry flags.
func TestRedoIsAnIdleOnlyArgumentTakingVerb(t *testing.T) {
	t.Parallel()

	spec, ok := commandByName("redo")
	if !ok {
		t.Fatal("commandSpecs carries no redo row")
	}
	if spec.whileRunning || !spec.takesArgs {
		t.Errorf("commandSpec = %+v, want an idle-only verb that reads its arguments", spec)
	}
}

func TestRedoPreviewsTheStepAndStashesItsOwnGeneration(t *testing.T) {
	eng := &fakeEngine{redoStep: scriptedStep(7), redoStepOK: true}

	m, view := runUndoLine(t, newTestModelEng(t, eng, testOpts), "/redo")

	if m.redoGeneration != 7 {
		t.Errorf("stashed generation = %d, want 7 — the confirm has nothing to quote", m.redoGeneration)
	}
	if m.undoGeneration != 0 {
		t.Errorf("undo stamp = %d, want 0 — a redo preview must not authorise an undo", m.undoGeneration)
	}
	if len(eng.redoReverts) != 0 {
		t.Errorf("RedoRevert calls = %v, want none: a preview must touch no file", eng.redoReverts)
	}
	for _, want := range []string{"/redo — exchange 3", "restore", "/w/a.go", "delete", "/w/new.go",
		"skip", "/w/b.go", "edited since", "/redo confirm"} {
		if !strings.Contains(view, want) {
			t.Errorf("preview missing %q:\n%s", want, view)
		}
	}
}

func TestRedoConfirmRePlaysAtThePreviewedGeneration(t *testing.T) {
	eng := &fakeEngine{
		redoStep:   scriptedStep(7),
		redoStepOK: true,
		redoReport: undo.Report{
			Ordinal:  1,
			Restored: []string{"/w/a.go"},
			Deleted:  []string{"/w/new.go"},
			Skipped:  []undo.Skipped{{Path: "/w/b.go", Reason: "edited since"}},
		},
	}

	m, _ := runUndoLine(t, newTestModelEng(t, eng, testOpts), "/redo")
	m, view := runUndoLine(t, m, "/redo confirm")

	if len(eng.redoReverts) != 1 || eng.redoReverts[0] != 7 {
		t.Fatalf("RedoRevert calls = %v, want exactly [7] — the confirm must quote the preview", eng.redoReverts)
	}
	if len(eng.undoReverts) != 0 {
		t.Errorf("UndoRevert calls = %v, want none — /redo drives its own door", eng.undoReverts)
	}
	for _, want := range []string{"redone", "exchange 1", "1 restored", "1 removed", "1 skipped",
		"/w/b.go", "edited since"} {
		if !strings.Contains(view, want) {
			t.Errorf("report missing %q:\n%s", want, view)
		}
	}
	if m.redoGeneration != 0 {
		t.Errorf("stashed generation = %d, want 0 — a spent preview authorises nothing", m.redoGeneration)
	}
}

func TestRedoConfirmOnAStaleGenerationRePreviewsInsteadOfReplaying(t *testing.T) {
	eng := &fakeEngine{redoStep: scriptedStep(7), redoStepOK: true}

	m, _ := runUndoLine(t, newTestModelEng(t, eng, testOpts), "/redo")

	// The journal moved between the preview and the confirmation, so the engine refuses the stamp
	// the human is quoting and offers what is on top of the redo stack NOW.
	eng.redoErr = fmt.Errorf("%w: previewed at generation 7, journal is at 9", undo.ErrStaleGeneration)
	eng.redoStep = scriptedStep(9)
	m, view := runUndoLine(t, m, "/redo confirm")

	if len(eng.redoReverts) != 1 || eng.redoReverts[0] != 7 {
		t.Fatalf("RedoRevert calls = %v, want exactly [7]: the refusal must not be retried", eng.redoReverts)
	}
	if m.redoGeneration != 9 {
		t.Errorf("stashed generation = %d, want 9 — the re-preview must be the one confirmable now", m.redoGeneration)
	}
	if !strings.Contains(view, "nothing was redone") {
		t.Errorf("the note does not say the redo did not happen:\n%s", view)
	}
	for _, want := range []string{"exchange 3", "/w/a.go", "/redo confirm"} {
		if !strings.Contains(view, want) {
			t.Errorf("re-preview missing %q:\n%s", want, view)
		}
	}
}

func TestRedoWithNothingUndoneSaysSoAndStashesNothing(t *testing.T) {
	for _, line := range []string{"/redo", "/redo confirm"} {
		t.Run(line, func(t *testing.T) {
			// The empty stack answers both surfaces: a preview reports no step, a redo refuses.
			eng := &fakeEngine{redoStepOK: false, redoErr: undo.ErrNothingToRedo}

			m, view := runUndoLine(t, newTestModelEng(t, eng, testOpts), line)

			if m.redoGeneration != 0 {
				t.Errorf("stashed generation = %d, want 0 — there is no step to confirm", m.redoGeneration)
			}
			if !strings.Contains(view, "nothing to redo") {
				t.Errorf("note missing the answer itself:\n%s", view)
			}
		})
	}
}

// A stamp left by an /undo preview must not travel to the other stack: a `/redo confirm` typed
// after it quotes zero, which the engine's stale guard refuses, so the human previews first.
func TestRedoConfirmDoesNotSpendTheUndoStamp(t *testing.T) {
	eng := &fakeEngine{
		undoStep: scriptedStep(7), undoStepOK: true,
		redoStep: scriptedStep(7), redoStepOK: true,
		redoErr: fmt.Errorf("%w: previewed at generation 0, journal is at 7", undo.ErrStaleGeneration),
	}

	m, _ := runUndoLine(t, newTestModelEng(t, eng, testOpts), "/undo")
	_, view := runUndoLine(t, m, "/redo confirm")

	if len(eng.redoReverts) != 1 || eng.redoReverts[0] != 0 {
		t.Fatalf("RedoRevert calls = %v, want exactly [0] — the undo preview stamps only /undo", eng.redoReverts)
	}
	if !strings.Contains(view, "nothing was redone") {
		t.Errorf("the stale confirmation did not earn a re-preview:\n%s", view)
	}
}

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

// runUndoLine drives one /undo line through the real key path and returns the model plus the
// plain-text View, so a test asserts on what the human actually sees. It takes the model rather
// than building one, because the two-step grammar only means anything across two lines.
func runUndoLine(t *testing.T, m Model, line string) (Model, string) {
	t.Helper()
	m.input.SetValue(line)
	m, cmd := stepCmd(t, m, keyEnter())
	if cmd != nil {
		t.Error("/undo returned a Cmd; it is synchronous and must not launch a worker")
	}
	if m.state != stateIdle {
		t.Errorf("state = %v, want idle (/undo must not launch a worker)", m.state)
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
			if parsed.undo != c.want {
				t.Errorf("action = %v, want %v", parsed.undo, c.want)
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

func TestUndoWithNothingRecordedSaysSoAndNamesTheJournalsLifetime(t *testing.T) {
	for _, line := range []string{"/undo", "/undo confirm"} {
		t.Run(line, func(t *testing.T) {
			// The empty journal answers both surfaces: a preview reports no step, a revert refuses.
			eng := &fakeEngine{undoStepOK: false, undoErr: undo.ErrNothingToUndo}

			m, view := runUndoLine(t, newTestModelEng(t, eng, testOpts), line)

			if m.undoGeneration != 0 {
				t.Errorf("stashed generation = %d, want 0 — there is no step to confirm", m.undoGeneration)
			}
			for _, want := range []string{"nothing to undo", "starts empty each run"} {
				if !strings.Contains(view, want) {
					t.Errorf("note missing %q — an empty journal must not read as a lost one:\n%s", want, view)
				}
			}
		})
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
// The note builders (pure)
// ----------------------------------------------------------------------------

func TestUndoPreviewNoteDisclosesEveryPathAndTheWayToApplyIt(t *testing.T) {
	t.Parallel()

	got := undoPreviewNote(scriptedStep(7))

	for _, want := range []string{
		"/undo — exchange 3",
		"  restore /w/a.go",
		"  delete  /w/new.go",
		"  skip    /w/b.go — edited since",
		undoConfirmHint,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preview missing %q:\n%s", want, got)
		}
	}
}

func TestUndoReportNoteCountsTheOutcomeAndNamesEverySkip(t *testing.T) {
	t.Parallel()

	got := undoReportNote(undo.Report{
		Ordinal:  2,
		Restored: []string{"/w/a.go", "/w/c.go"},
		Deleted:  []string{"/w/new.go"},
		Skipped: []undo.Skipped{
			{Path: "/w/b.go", Reason: "edited since"},
			{Path: "/w/d.go", Reason: "permission denied"},
		},
	})

	for _, want := range []string{
		"undone — exchange 2: 2 restored, 1 removed, 2 skipped",
		"  skip    /w/b.go — edited since",
		"  skip    /w/d.go — permission denied",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q:\n%s", want, got)
		}
	}
}

func TestUndoNothingNoteSaysTheJournalIsPerProcess(t *testing.T) {
	t.Parallel()

	got := undoNothingNote()

	// "nothing to undo" on a resumed session must not read as a journal that lost what it held.
	for _, want := range []string{"nothing to undo", "memory, not storage", "before this process"} {
		if !strings.Contains(got, want) {
			t.Errorf("note missing %q:\n%s", want, got)
		}
	}
}

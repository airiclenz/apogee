package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/heartbeat"
)

// ----------------------------------------------------------------------------
// /model picker harness
// ----------------------------------------------------------------------------

// The picker is driven the way every other overlay is: synthetic Msgs into Update, asserting the
// state, the transcript, and what the seams were called with. The rebind seam is the SAME fake the
// heartbeat tests use (fakeRebind), which is the point of the design — a picked model travels the
// orchestration a beat-observed one does.

// offerBeat is one observation of a reachable server serving active out of a multi-model offering —
// upBeat's sibling for the picker, whose rows are derived from AvailableModels.
func offerBeat(active string, window int, models ...heartbeat.ModelSummary) heartbeat.Beat {
	return heartbeat.Beat{
		Reachable:       true,
		ActiveModel:     active,
		ContextWindow:   window,
		AvailableModels: models,
	}
}

// twoModelBeat is the fixture most picker tests open on: the bound test-model beside a second
// model the server also serves.
func twoModelBeat() heartbeat.Beat {
	return offerBeat("test-model", 32768,
		heartbeat.ModelSummary{ID: "test-model", ContextWindow: 32768},
		heartbeat.ModelSummary{ID: "other-model", ContextWindow: 16384},
	)
}

// typeCommand puts line in the input box and presses ⏎, the way a human invokes a whole-input
// command.
func typeCommand(t *testing.T, m Model, line string) (Model, tea.Cmd) {
	t.Helper()
	m.input.SetValue(line)
	return stepCmd(t, m, keyEnter())
}

// seededPicker is a ready model with both upstream seams wired and one two-model beat folded, with
// the seed rebind that beat drove forgotten — so a test asserts only the calls IT caused.
func seededPicker(t *testing.T, opts Options) (Model, *fakeRebind) {
	t.Helper()
	rb := &fakeRebind{}
	m := wireRebind(t, opts, &fakeHeartbeat{}, rb)
	m = foldBeatMsg(t, m, twoModelBeat())
	rb.calls = nil // the first beat's own seed is not what these tests are about
	return m, rb
}

// ----------------------------------------------------------------------------
// Opening the picker
// ----------------------------------------------------------------------------

// /model lists what the server advertises, marks the row the session is bound to, and opens on it.
func TestModelPickerListsTheOffering(t *testing.T) {
	m, _ := seededPicker(t, testOpts)

	m, cmd := typeCommand(t, m, "/model")
	if cmd != nil {
		t.Error("/model returned a Cmd; opening the picker launches no worker")
	}
	if !m.picker.open || m.picker.kind != pickerModel {
		t.Fatalf("picker = {open:%v kind:%v}, want an open model picker", m.picker.open, m.picker.kind)
	}
	if m.picker.selected != 0 {
		t.Errorf("selected = %d, want 0 — the picker opens on the bound model's row", m.picker.selected)
	}

	got := plain(m.View())
	for _, want := range []string{"switch model — test-host", "test-model", "other-model", "16k", pickerHint} {
		if !strings.Contains(got, want) {
			t.Errorf("the pane is missing %q:\n%s", want, got)
		}
	}
	rows := m.pickerRows()
	if len(rows) != 2 {
		t.Fatalf("rows = %v, want one per advertised model", rows)
	}
	if !strings.HasSuffix(rows[0], currentRowSuffix) {
		t.Errorf("rows[0] = %q, want the bound model marked %q", rows[0], currentRowSuffix)
	}
	if strings.HasSuffix(rows[1], currentRowSuffix) {
		t.Errorf("rows[1] = %q, want no current marker on a model the session is not bound to", rows[1])
	}
}

// A beat landing under an open picker refreshes the offering in place, and a selection the shorter
// list can no longer hold is clamped rather than left pointing past the end.
func TestModelPickerFollowsTheOfferingWhileOpen(t *testing.T) {
	m, _ := seededPicker(t, testOpts)
	m, _ = typeCommand(t, m, "/model")
	m = step(t, m, keyDown())
	if m.picker.selected != 1 {
		t.Fatalf("precondition: selected = %d, want the second row", m.picker.selected)
	}

	// The server drops back to serving only what the session is bound to.
	m = foldBeatMsg(t, m, upBeat("test-model", 32768))

	if !m.picker.open {
		t.Fatal("the picker closed on a beat; a refreshed offering must not dismiss it")
	}
	if m.picker.selected != 0 {
		t.Errorf("selected = %d, want 0 — the selection is clamped into the shorter list", m.picker.selected)
	}
	if got := m.pickerRows(); len(got) != 1 {
		t.Errorf("rows = %v, want the refreshed one-model offering", got)
	}
}

// Esc closes the picker and moves nothing.
func TestModelPickerEscCloses(t *testing.T) {
	m, rb := seededPicker(t, testOpts)
	m, _ = typeCommand(t, m, "/model")

	m = step(t, m, keyEsc())

	if m.picker.open {
		t.Error("esc left the picker open")
	}
	if len(rb.calls) != 0 {
		t.Errorf("rebind calls = %v, want none — esc binds nothing", rb.calls)
	}
}

// ----------------------------------------------------------------------------
// Accepting a row
// ----------------------------------------------------------------------------

// Accepting another row drives the EXISTING rebind orchestration: the seam is called with the
// picked id and its window, the display adopts what came back, the start-up box is restated, and
// the change is worded by rebindNote — no second set of strings.
func TestModelPickerAcceptRebindsThroughTheSeam(t *testing.T) {
	m, rb := seededPicker(t, testOpts)
	m, _ = typeCommand(t, m, "/model")
	m = step(t, m, keyDown())

	m, cmd := stepCmd(t, m, keyEnter())

	if cmd != nil {
		t.Error("an accepted row returned a Cmd; the rebind runs on the Update loop")
	}
	if m.picker.open {
		t.Error("the picker stayed open after an accept")
	}
	want := []rebindCall{{model: "other-model", window: 16384}}
	if len(rb.calls) != 1 || rb.calls[0] != want[0] {
		t.Fatalf("rebind calls = %v, want %v", rb.calls, want)
	}
	if m.opts.Model != "other-model" || m.opts.ContextWindow != 16384 {
		t.Errorf("opts = {%q %d}, want the picked binding adopted", m.opts.Model, m.opts.ContextWindow)
	}
	if got := plain(m.View()); !strings.Contains(got, "model changed: test-model → other-model") {
		t.Errorf("the change is not worded in the transcript:\n%s", got)
	}
	// The box's facts were frozen when it was seeded; applyRebind restates it in place.
	if got := plainTranscript(m); !strings.Contains(got, "other-model") {
		t.Errorf("the start-up box was not restated with the new binding:\n%s", got)
	}
}

// The flap-back pin: a pick records itself as the last OBSERVATION, so the next beat reporting the
// picked model measures as "nothing new" and binds nothing back. Without it, a multi-model server
// still resolving the old discovery hint would yank the session back within one Interval.
func TestModelPickerPickSurvivesTheNextBeat(t *testing.T) {
	m, rb := seededPicker(t, testOpts)
	m, _ = typeCommand(t, m, "/model")
	m = step(t, m, keyDown())
	m, _ = stepCmd(t, m, keyEnter())
	notesBefore := len(noteTexts(m))

	m = foldBeatMsg(t, m, offerBeat("other-model", 16384,
		heartbeat.ModelSummary{ID: "test-model", ContextWindow: 32768},
		heartbeat.ModelSummary{ID: "other-model", ContextWindow: 16384},
	))

	if len(rb.calls) != 1 {
		t.Errorf("rebind calls = %v, want only the pick's own — the beat had nothing new to report", rb.calls)
	}
	if m.opts.Model != "other-model" {
		t.Errorf("opts.Model = %q, want the session still on the picked model", m.opts.Model)
	}
	if got := noteTexts(m); len(got) != notesBefore {
		t.Errorf("notes = %v, want nothing added by a beat that changed nothing", got)
	}
}

// A refused rebind leaves every binding exactly where it was and says so once, in the heartbeat's
// own words (rebindFailNote).
func TestModelPickerAcceptReportsARefusedRebind(t *testing.T) {
	m, rb := seededPicker(t, testOpts)
	rb.answer = func(string, int) (RebindResult, error) { return RebindResult{}, errors.New("engine busy") }
	m, _ = typeCommand(t, m, "/model")
	m = step(t, m, keyDown())

	m, _ = stepCmd(t, m, keyEnter())

	if m.opts.Model != "test-model" || m.opts.ContextWindow != 32768 {
		t.Errorf("opts = {%q %d}, want the bindings unmoved by a refused rebind", m.opts.Model, m.opts.ContextWindow)
	}
	want := rebindFailNote("test-model", "other-model", errors.New("engine busy"))
	if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
		t.Errorf("last note = %v, want %q", got, want)
	}
}

// Accepting the row the session is already on is ANSWERED, not ignored — an explicit act deserves a
// reply — and drives no seam.
func TestModelPickerAcceptCurrentRowIsANote(t *testing.T) {
	m, rb := seededPicker(t, testOpts)
	m, _ = typeCommand(t, m, "/model")

	m, _ = stepCmd(t, m, keyEnter())

	if m.picker.open {
		t.Error("the picker stayed open after an accept")
	}
	if len(rb.calls) != 0 {
		t.Errorf("rebind calls = %v, want none — the session is already on that model", rb.calls)
	}
	if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != "already bound to test-model" {
		t.Errorf("notes = %v, want the already-bound answer", got)
	}
}

// ----------------------------------------------------------------------------
// The argument form
// ----------------------------------------------------------------------------

func TestModelCommandArgumentForm(t *testing.T) {
	t.Run("known id switches without an overlay", func(t *testing.T) {
		m, rb := seededPicker(t, testOpts)

		m, _ = typeCommand(t, m, "/model other-model")

		if m.picker.open {
			t.Error("the argument form opened an overlay; it takes the id directly")
		}
		want := rebindCall{model: "other-model", window: 16384}
		if len(rb.calls) != 1 || rb.calls[0] != want {
			t.Fatalf("rebind calls = %v, want [%v]", rb.calls, want)
		}
		if m.opts.Model != "other-model" {
			t.Errorf("opts.Model = %q, want the named model bound", m.opts.Model)
		}
	})

	t.Run("unknown id points at the bare form", func(t *testing.T) {
		m, rb := seededPicker(t, testOpts)

		m, _ = typeCommand(t, m, "/model nope")

		if m.picker.open {
			t.Error("an unknown id opened an overlay")
		}
		if len(rb.calls) != 0 {
			t.Errorf("rebind calls = %v, want none", rb.calls)
		}
		want := `unknown model "nope" — /model with no argument lists what the server serves`
		if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
			t.Errorf("notes = %v, want %q", got, want)
		}
	})

	t.Run("surplus arguments earn the usage line", func(t *testing.T) {
		m, rb := seededPicker(t, testOpts)

		m, _ = typeCommand(t, m, "/model a b")

		if m.picker.open {
			t.Error("a usage error opened an overlay")
		}
		if len(rb.calls) != 0 {
			t.Errorf("rebind calls = %v, want none", rb.calls)
		}
		if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != modelUsage {
			t.Errorf("notes = %v, want %q", got, modelUsage)
		}
	})
}

// /model is idle-only by the commandSpecs table, so a line typed mid-run earns the standing answer
// instead of running — the tag the dropdown shows and what ⏎ does are one rule.
func TestModelCommandIsIdleOnly(t *testing.T) {
	if spec, ok := commandByName("model"); !ok || spec.whileRunning || !spec.takesArgs {
		t.Fatalf("commandSpec = %+v, want an idle-only verb that reads its arguments", spec)
	}
	m := newTestModelEng(t, &fakeEngine{}, testOpts)
	m, _ = typeCommand(t, m, "open the exchange")
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}

	m, _ = typeCommand(t, m, "/model")

	if m.picker.open {
		t.Error("the picker opened mid-run; /model is idle-only")
	}
	if got := plain(m.View()); !strings.Contains(got, commandsAtIdleNote) {
		t.Errorf("the refusal note is missing from the transcript:\n%s", got)
	}
}

// ----------------------------------------------------------------------------
// Degrades — each an honest note and no overlay
// ----------------------------------------------------------------------------

func TestModelCommandDegradesWithAnHonestNote(t *testing.T) {
	t.Run("heartbeat unwired", func(t *testing.T) {
		m := newTestModelEng(t, &fakeEngine{}, testOpts)

		m, _ = typeCommand(t, m, "/model")

		assertPickerDegrade(t, m, "/model needs the upstream monitor — not wired")
	})

	t.Run("server offline", func(t *testing.T) {
		rb := &fakeRebind{}
		m := wireRebind(t, testOpts, &fakeHeartbeat{}, rb)
		m = foldBeatMsg(t, m, twoModelBeat())
		m = foldBeatMsg(t, m, downBeat("dial tcp: refused"))
		m = foldBeatMsg(t, m, downBeat("dial tcp: refused"))
		if !m.hb.offline {
			t.Fatalf("precondition: the model is not offline after two failed beats")
		}
		rb.calls = nil

		m, _ = typeCommand(t, m, "/model")

		assertPickerDegrade(t, m, m.upstreamBlockNote())
		if len(rb.calls) != 0 {
			t.Errorf("rebind calls = %v, want none while offline", rb.calls)
		}
	})

	t.Run("display-frozen heartbeat", func(t *testing.T) {
		m := wireHeartbeat(t, testOpts, &fakeHeartbeat{}) // no Rebind seam
		m = foldBeatMsg(t, m, twoModelBeat())

		m, _ = typeCommand(t, m, "/model")

		assertPickerDegrade(t, m, "model switching is unavailable — the display is read-only")
	})

	t.Run("nothing advertised yet", func(t *testing.T) {
		m := wireRebind(t, testOpts, &fakeHeartbeat{}, &fakeRebind{})
		m = foldBeatMsg(t, m, heartbeat.Beat{Reachable: true, ActiveModel: "test-model", ContextWindow: 32768})

		m, _ = typeCommand(t, m, "/model")

		assertPickerDegrade(t, m, "the server has not advertised any models yet")
	})
}

// assertPickerDegrade pins one degrade: the overlay never opened and the last note is the honest
// line that says why.
func assertPickerDegrade(t *testing.T, m Model, want string) {
	t.Helper()
	if m.picker.open {
		t.Error("the picker opened; a degrade must open no overlay")
	}
	if m.renderPicker() != "" {
		t.Error("a pane was painted for a picker that never opened")
	}
	if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
		t.Errorf("notes = %v, want %q", got, want)
	}
}

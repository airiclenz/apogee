package tui

import (
	"errors"
	"reflect"
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

// ----------------------------------------------------------------------------
// /server harness
// ----------------------------------------------------------------------------

// twoServers is the /server fixture: the endpoint testOpts launched on, under the alias its footer
// shows, beside a second configured server to move to.
var twoServers = []ServerChoice{
	{Name: "test-host", Endpoint: "http://localhost:1234"},
	{Name: "remote", Endpoint: "http://remote:8080"},
}

// remoteWindow is the `context-window:` pin the fake switch reports back — GLOBAL config, so it
// survives the move, which is what lets a test tell an adopted result from a cleared one.
const remoteWindow = 8192

// fakeSwitch stands in for the composition root's switch closure: it records every server name it
// was asked to move to and answers with what the binary would have returned — the entry's endpoint,
// its name as the new alias, and the surviving window pin. It is called synchronously on the test's
// own goroutine, so it needs no guard.
type fakeSwitch struct {
	calls  []string
	answer func(name string) (ServerSwitchResult, error)
}

func (f *fakeSwitch) switchTo(name string) (ServerSwitchResult, error) {
	f.calls = append(f.calls, name)
	if f.answer != nil {
		return f.answer(name)
	}
	for _, choice := range twoServers {
		if choice.Name == name {
			return ServerSwitchResult{
				Endpoint:      choice.Endpoint,
				HostAlias:     choice.Name,
				ContextWindow: remoteWindow,
			}, nil
		}
	}
	return ServerSwitchResult{}, errors.New("unknown server " + name)
}

// seededServers is a ready model with all three upstream seams wired and one beat folded — the
// state a human is in when they type /server.
func seededServers(t *testing.T, sw *fakeSwitch) (Model, *fakeRebind) {
	t.Helper()
	opts := testOpts
	opts.Servers = twoServers
	opts.SwitchServer = sw.switchTo
	return seededPicker(t, opts)
}

// ----------------------------------------------------------------------------
// The server picker
// ----------------------------------------------------------------------------

// /server lists the configured servers, marks the one this session is on (by endpoint, the identity
// the binary assembled the list by) and opens on it.
func TestServerPickerListsTheConfiguredServers(t *testing.T) {
	m, _ := seededServers(t, &fakeSwitch{})

	m, cmd := typeCommand(t, m, "/server")

	if cmd != nil {
		t.Error("/server returned a Cmd; opening the picker switches nothing yet")
	}
	if !m.picker.open || m.picker.kind != pickerServer {
		t.Fatalf("picker = {open:%v kind:%v}, want an open server picker", m.picker.open, m.picker.kind)
	}
	if m.picker.selected != 0 {
		t.Errorf("selected = %d, want 0 — the picker opens on the server the session is on", m.picker.selected)
	}
	got := plain(m.View())
	for _, want := range []string{"switch server", "test-host", "http://remote:8080", pickerHint} {
		if !strings.Contains(got, want) {
			t.Errorf("the pane is missing %q:\n%s", want, got)
		}
	}
	rows := m.pickerRows()
	if len(rows) != 2 {
		t.Fatalf("rows = %v, want one per configured server", rows)
	}
	if !strings.HasSuffix(rows[0], currentRowSuffix) {
		t.Errorf("rows[0] = %q, want the current server marked %q", rows[0], currentRowSuffix)
	}
	if strings.HasSuffix(rows[1], currentRowSuffix) {
		t.Errorf("rows[1] = %q, want no current marker on a server the session is not on", rows[1])
	}
}

// The happy path, end to end: the seam is called with the picked name, the display adopts what came
// back, the model is UNBOUND, the box is restated, the move is worded — and the new chain's first
// beat fires at once and binds through the ordinary rebind path, announcing itself (a switch is not
// a launch, so the quiet first-contact seed does not apply).
func TestServerSwitchHappyPath(t *testing.T) {
	sw := &fakeSwitch{}
	m, rb := seededServers(t, sw)
	oldGen := m.hb.gen
	m, _ = typeCommand(t, m, "/server")
	m = step(t, m, keyDown())

	m, cmd := stepCmd(t, m, keyEnter())

	if want := []string{"remote"}; !reflect.DeepEqual(sw.calls, want) {
		t.Fatalf("switch calls = %v, want %v", sw.calls, want)
	}
	if m.picker.open {
		t.Error("the picker stayed open after an accept")
	}
	if m.opts.Endpoint != "http://remote:8080" || m.opts.HostAlias != "remote" {
		t.Errorf("opts = {%q %q}, want the switch result adopted", m.opts.Endpoint, m.opts.HostAlias)
	}
	if m.opts.ContextWindow != remoteWindow {
		t.Errorf("opts.ContextWindow = %d, want the surviving pin %d", m.opts.ContextWindow, remoteWindow)
	}
	if m.opts.Model != "" {
		t.Errorf("opts.Model = %q, want the model unbound until the new server's first beat", m.opts.Model)
	}
	if m.hb.gen == oldGen || !m.hb.switched || len(m.hb.models) != 0 || m.hb.everOnline {
		t.Errorf("hb = %+v, want a fresh generation, the switched mark, and no memory of the old server", m.hb)
	}
	if box := m.transcript.entries[0]; box.kind != entryStartup || box.startup.Host != "remote" || box.startup.Model != "" {
		t.Errorf("start-up box = %+v, want it restated on the new server with no model", box.startup)
	}
	want := "switching server: test-host → remote (http://remote:8080)"
	if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
		t.Errorf("notes = %v, want %q", got, want)
	}
	if len(rb.calls) != 0 {
		t.Errorf("rebind calls = %v, want none — the switch binds nothing; the first beat does", rb.calls)
	}

	// The returned Cmd is the new chain's first beat, fired NOW rather than one Interval later.
	if cmd == nil {
		t.Fatal("the switch returned no Cmd — the new server would not be observed for a full Interval")
	}
	beat, ok := cmd().(beatMsg)
	if !ok {
		t.Fatalf("the switch's Cmd yielded %T, want the new chain's first beatMsg", cmd())
	}
	if beat.gen != m.hb.gen {
		t.Errorf("the first beat carries generation %d, want the switched-in %d", beat.gen, m.hb.gen)
	}

	m = foldBeatMsg(t, m, upBeat("remote-model", remoteWindow))

	if want := []rebindCall{{model: "remote-model", window: remoteWindow}}; !reflect.DeepEqual(rb.calls, want) {
		t.Fatalf("rebind calls = %v, want %v — the first beat completes the switch", rb.calls, want)
	}
	if n := countNotes(m, "connected: remote-model"); n != 1 {
		t.Errorf("connected notes = %d, want exactly 1 — a post-switch seed is news; notes = %q",
			n, noteTexts(m))
	}
	if m.blockedUpstream() {
		t.Error("the upstream is still blocked after the new server bound a model")
	}
}

// Everything still in flight on the old chain lands inert: the switch retired that generation, so a
// beat or a tick from the server the session just left changes nothing and schedules nothing.
func TestServerSwitchRetiresTheOldChain(t *testing.T) {
	m, rb := seededServers(t, &fakeSwitch{})
	oldGen := m.hb.gen
	m, _ = typeCommand(t, m, "/server remote")
	notesBefore := len(noteTexts(m))

	stale, cmd := stepCmd(t, m, beatMsg{gen: oldGen, beat: upBeat("test-model", 32768)})
	if cmd != nil {
		t.Error("a beat from the retired chain re-armed it — two chains would beat at once")
	}
	stale, cmd = stepCmd(t, stale, heartbeatTickMsg{gen: oldGen})
	if cmd != nil {
		t.Error("a tick from the retired chain issued a beat")
	}

	if stale.opts.Model != "" || stale.hb.everOnline {
		t.Errorf("the old chain moved the switched session: model %q, everOnline %v",
			stale.opts.Model, stale.hb.everOnline)
	}
	if len(rb.calls) != 0 {
		t.Errorf("rebind calls = %v, want none from a retired chain", rb.calls)
	}
	if got := noteTexts(stale); len(got) != notesBefore {
		t.Errorf("notes = %v, want nothing added by a retired chain", got)
	}
}

// In the gap between the switch and the first bind there is nothing to send to, and the refusal
// names the NEW endpoint — the async cold start's own wording, reached by a second route.
func TestServerSwitchBlocksSendsUntilTheFirstBind(t *testing.T) {
	sw := &fakeSwitch{}
	opts := testOpts
	opts.Servers = twoServers
	opts.SwitchServer = sw.switchTo
	rb := &fakeRebind{}
	eng := &fakeEngine{}
	opts.Rebind = rb.rebind
	opts.Heartbeat = (&fakeHeartbeat{}).beat
	m := newTestModelEng(t, eng, opts)
	m = foldBeatMsg(t, m, twoModelBeat())

	m, _ = typeCommand(t, m, "/server remote")

	if !m.blockedUpstream() {
		t.Fatal("a session with no model bound is not blocked after a switch")
	}
	m.input.SetValue("what is the capital of France?")
	m, cmd := stepCmd(t, m, keyEnter())
	if cmd != nil || len(eng.submitted) != 0 {
		t.Errorf("a send reached the engine in the unbound gap (submitted %d)", len(eng.submitted))
	}
	want := "cannot send — still connecting to http://remote:8080"
	if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
		t.Errorf("notes = %v, want %q", got, want)
	}
}

// The offline debounce goes back to its cold-start posture with the rest of the heartbeat state: a
// new server that is not there says so on its FIRST failed beat, because nothing observed about the
// old server is evidence about this one.
func TestServerSwitchBelievesTheFirstFailureOnTheNewServer(t *testing.T) {
	m, _ := seededServers(t, &fakeSwitch{})
	m, _ = typeCommand(t, m, "/server remote")

	m = foldBeatMsg(t, m, downBeat("connection refused"))

	if !m.hb.offline {
		t.Error("the first failed beat on a freshly switched-to server did not go offline")
	}
	if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != offlineNote("connection refused") {
		t.Errorf("notes = %v, want %q", got, offlineNote("connection refused"))
	}
}

// A refused switch moves NOTHING: the seam is validate-then-commit all the way down, so the note is
// the whole of the answer.
func TestServerSwitchReportsARefusedSwitch(t *testing.T) {
	sw := &fakeSwitch{answer: func(string) (ServerSwitchResult, error) {
		return ServerSwitchResult{}, errors.New("an exchange is in flight")
	}}
	m, _ := seededServers(t, sw)
	before := m.opts
	beforeGen, beforeBox := m.hb.gen, m.transcript.entries[0]

	m, _ = typeCommand(t, m, "/server remote")

	if m.opts.Endpoint != before.Endpoint || m.opts.HostAlias != before.HostAlias ||
		m.opts.Model != before.Model || m.opts.ContextWindow != before.ContextWindow {
		t.Errorf("upstream opts = {%q %q %q %d}, want them unmoved by a refused switch",
			m.opts.Endpoint, m.opts.HostAlias, m.opts.Model, m.opts.ContextWindow)
	}
	if m.hb.gen != beforeGen || m.hb.switched {
		t.Errorf("hb = %+v, want the live chain untouched by a refused switch", m.hb)
	}
	if got := m.transcript.entries[0]; got.startup != beforeBox.startup {
		t.Errorf("start-up box = %+v, want it unchanged", got.startup)
	}
	want := "could not switch server: an exchange is in flight"
	if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
		t.Errorf("notes = %v, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// /server — the answers that are notes
// ----------------------------------------------------------------------------

func TestServerCommandAnswersWithoutSwitching(t *testing.T) {
	t.Run("already on it", func(t *testing.T) {
		sw := &fakeSwitch{}
		m, _ := seededServers(t, sw)

		m, _ = typeCommand(t, m, "/server")
		m, _ = stepCmd(t, m, keyEnter())

		if len(sw.calls) != 0 {
			t.Errorf("switch calls = %v, want none — the session is already on that server", sw.calls)
		}
		if m.picker.open {
			t.Error("the picker stayed open after an accept")
		}
		want := "already on test-host (http://localhost:1234)"
		if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
			t.Errorf("notes = %v, want %q", got, want)
		}
	})

	t.Run("unknown name lists the configured ones", func(t *testing.T) {
		sw := &fakeSwitch{}
		m, _ := seededServers(t, sw)

		m, _ = typeCommand(t, m, "/server nope")

		if len(sw.calls) != 0 {
			t.Errorf("switch calls = %v, want none", sw.calls)
		}
		assertPickerDegrade(t, m, `unknown server "nope" — configured: test-host, remote`)
	})

	t.Run("surplus arguments earn the usage line", func(t *testing.T) {
		sw := &fakeSwitch{}
		m, _ := seededServers(t, sw)

		m, _ = typeCommand(t, m, "/server a b")

		if len(sw.calls) != 0 {
			t.Errorf("switch calls = %v, want none", sw.calls)
		}
		assertPickerDegrade(t, m, serverUsage)
	})

	t.Run("no servers configured", func(t *testing.T) {
		// An empty list and an unwired seam are ONE situation for the human, so they are one line.
		for _, tc := range []struct {
			name string
			opts Options
		}{
			{name: "empty list", opts: func() Options {
				o := testOpts
				o.SwitchServer = (&fakeSwitch{}).switchTo
				return o
			}()},
			{name: "unwired seam", opts: func() Options {
				o := testOpts
				o.Servers = twoServers
				return o
			}()},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m, _ := seededPicker(t, tc.opts)

				m, _ = typeCommand(t, m, "/server")

				assertPickerDegrade(t, m, noServersNote)
			})
		}
	})
}

// /server is idle-only by the commandSpecs table: it ends in an engine mutation Agent.SwitchUpstream
// allows only at a quiescent boundary, so a line typed mid-run earns the standing answer.
func TestServerCommandIsIdleOnly(t *testing.T) {
	if spec, ok := commandByName("server"); !ok || spec.whileRunning || !spec.takesArgs {
		t.Fatalf("commandSpec = %+v, want an idle-only verb that reads its arguments", spec)
	}
	sw := &fakeSwitch{}
	opts := testOpts
	opts.Servers = twoServers
	opts.SwitchServer = sw.switchTo
	m := newTestModelEng(t, &fakeEngine{}, opts)
	m, _ = typeCommand(t, m, "open the exchange")
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}

	m, _ = typeCommand(t, m, "/server remote")

	if m.picker.open {
		t.Error("the picker opened mid-run; /server is idle-only")
	}
	if len(sw.calls) != 0 {
		t.Errorf("switch calls = %v, want none mid-run", sw.calls)
	}
	if got := plain(m.View()); !strings.Contains(got, commandsAtIdleNote) {
		t.Errorf("the refusal note is missing from the transcript:\n%s", got)
	}
}

// ----------------------------------------------------------------------------
// /load — the Launch-profile picker (ADR 0029 D3)
// ----------------------------------------------------------------------------
//
// The harness is the launcher fake in actuation_test.go: /load's rows come from a seam rather than
// from Model state, so these tests script what the launcher's config defines and assert what the
// overlay makes of it. The accept path stops at the LATCH — what happens after it is the actuation
// suite's subject.

// seededLoad is a ready model with the heartbeat, the rebind and the four launcher seams wired —
// the state a human is in when they type /load.
func seededLoad(t *testing.T, fake *fakeLauncher) Model {
	t.Helper()
	m, _ := wireLauncher(t, fake)
	return m
}

// /load lists what the launcher's config defines, in the launcher's own order, and each row carries
// the facts the choice is made on: the backend, the configured context window, the port when the
// profile lives somewhere other than this session's server, and the running mark.
func TestLoadPickerListsTheLaunchProfiles(t *testing.T) {
	fake := newLauncher()
	m := seededLoad(t, fake)

	m, cmd := typeCommand(t, m, "/load")

	if cmd != nil {
		t.Error("/load returned a Cmd; opening the picker actuates nothing")
	}
	if !m.picker.open || m.picker.kind != pickerLoad {
		t.Fatalf("picker = {open:%v kind:%v}, want an open load picker", m.picker.open, m.picker.kind)
	}
	if m.picker.selected != 0 {
		t.Errorf("selected = %d, want the first row — the launcher's order puts favourites first",
			m.picker.selected)
	}
	if got := fake.listCount(); got != 1 {
		t.Errorf("the profile list was read %d times, want exactly one fresh read at open", got)
	}
	want := []string{"alpha — llamacpp · 32k", "beta — ollama · 8k (:8081) · running"}
	if got := m.pickerRows(); !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	got := plain(m.View())
	for _, want := range []string{loadPickerTitle, "alpha — llamacpp", "beta — ollama", pickerHint} {
		if !strings.Contains(got, want) {
			t.Errorf("the pane is missing %q:\n%s", want, got)
		}
	}
}

// The rows are re-read on EVERY open (ADR 0029 D4), so a profile added in the launcher's own TUI a
// moment ago is offered here without restarting apogee.
func TestLoadPickerReadsTheProfilesFreshOnEveryOpen(t *testing.T) {
	fake := newLauncher()
	m := seededLoad(t, fake)
	m, _ = typeCommand(t, m, "/load")
	m = step(t, m, keyEsc())

	fake.profiles = append(fake.profiles, LaunchProfileChoice{Name: "gamma", Backend: "lmstudio"})
	m, _ = typeCommand(t, m, "/load")

	if got := fake.listCount(); got != 2 {
		t.Errorf("the profile list was read %d times, want one read per open", got)
	}
	if got := m.pickerRows(); len(got) != 3 || !strings.HasPrefix(got[2], "gamma") {
		t.Errorf("rows = %v, want the profile added since the last open", got)
	}
}

// ⏎ on a row hands off to the actuation latch: the overlay closes, the latch is taken for THAT
// profile, and the blocking verb rides the returned Cmd (actuation.go owns everything after this).
func TestLoadPickerAcceptTakesTheLatch(t *testing.T) {
	m := seededLoad(t, newLauncher())
	m, _ = typeCommand(t, m, "/load")
	m = step(t, m, keyDown())

	m, cmd := stepCmd(t, m, keyEnter())

	if m.picker.open {
		t.Error("the picker stayed open after an accept")
	}
	if !m.actuation.inFlight || m.actuation.verb != verbLoad || m.actuation.profile != "beta" {
		t.Fatalf("actuation = %+v, want the latch held for the picked profile", m.actuation)
	}
	if cmd == nil {
		t.Error("the accept returned no Cmd — the blocking verb would never run")
	}
}

// Esc closes the picker and actuates nothing.
func TestLoadPickerEscCloses(t *testing.T) {
	fake := newLauncher()
	m := seededLoad(t, fake)
	m, _ = typeCommand(t, m, "/load")

	m = step(t, m, keyEsc())

	if m.picker.open {
		t.Error("esc left the picker open")
	}
	if m.actuation.inFlight {
		t.Error("esc took the actuation latch")
	}
	if got := fake.loaded(); len(got) != 0 {
		t.Errorf("loads = %v, want none — esc activates nothing", got)
	}
}

func TestLoadCommandArgumentForm(t *testing.T) {
	t.Run("known name activates without an overlay", func(t *testing.T) {
		m := seededLoad(t, newLauncher())

		m, cmd := typeCommand(t, m, "/load beta")

		if m.picker.open {
			t.Error("the argument form opened an overlay; it takes the name directly")
		}
		if !m.actuation.inFlight || m.actuation.profile != "beta" {
			t.Fatalf("actuation = %+v, want the latch held for the named profile", m.actuation)
		}
		if cmd == nil {
			t.Error("the argument form returned no Cmd — the blocking verb would never run")
		}
	})

	t.Run("unknown name lists the defined ones", func(t *testing.T) {
		m := seededLoad(t, newLauncher())

		m, _ = typeCommand(t, m, "/load nope")

		if m.actuation.inFlight {
			t.Error("an unknown name took the latch")
		}
		assertPickerDegrade(t, m, `unknown launch profile "nope" — configured: alpha, beta`)
	})

	t.Run("surplus arguments earn the usage line", func(t *testing.T) {
		m := seededLoad(t, newLauncher())

		m, _ = typeCommand(t, m, "/load a b")

		if m.actuation.inFlight {
			t.Error("a usage error took the latch")
		}
		assertPickerDegrade(t, m, loadUsage)
	})
}

// Each rung of the degrade ladder is one honest sentence and no overlay: the integration itself, the
// config it reads, and the profiles that config was supposed to hold.
func TestLoadCommandDegradesWithAnHonestNote(t *testing.T) {
	t.Run("launcher not configured", func(t *testing.T) {
		// All four seams are wired together or not at all, so the nil check speaks for all of them.
		m, _ := seededPicker(t, testOpts)

		m, _ = typeCommand(t, m, "/load")

		assertPickerDegrade(t, m, noLauncherNote)
	})

	t.Run("the config could not be read", func(t *testing.T) {
		fake := newLauncher()
		fake.listErr = errors.New("no launcher config at /home/x/.config/llama-launcher/config.yaml")
		m := seededLoad(t, fake)

		m, _ = typeCommand(t, m, "/load")

		assertPickerDegrade(t, m, "no launcher config at /home/x/.config/llama-launcher/config.yaml")
	})

	t.Run("no profiles defined", func(t *testing.T) {
		m := seededLoad(t, &fakeLauncher{})

		m, _ = typeCommand(t, m, "/load")

		assertPickerDegrade(t, m, noProfilesNote)
	})

	t.Run("the argument form takes the same ladder", func(t *testing.T) {
		// A degrade must answer BOTH forms: an argument reaching the accept path with no seam would
		// actuate nothing and say nothing, which is the one outcome a command must never have.
		m, _ := seededPicker(t, testOpts)

		m, _ = typeCommand(t, m, "/load alpha")

		if m.actuation.inFlight {
			t.Error("the argument form took the latch with no seam wired")
		}
		assertPickerDegrade(t, m, noLauncherNote)
	})
}

// /load is idle-only by the commandSpecs table: it ends in a blocking launcher verb that changes the
// server the running Exchange is talking to, so a line typed mid-run earns the standing answer.
func TestLoadCommandIsIdleOnly(t *testing.T) {
	if spec, ok := commandByName("load"); !ok || spec.whileRunning || !spec.takesArgs {
		t.Fatalf("commandSpec = %+v, want an idle-only verb that reads its arguments", spec)
	}
	fake := newLauncher()
	opts := testOpts
	opts.LaunchProfiles = fake.list
	opts.LoadProfile = fake.load
	m := newTestModelEng(t, &fakeEngine{}, opts)
	m, _ = typeCommand(t, m, "open the exchange")
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}

	m, _ = typeCommand(t, m, "/load alpha")

	if m.picker.open {
		t.Error("the picker opened mid-run; /load is idle-only")
	}
	if m.actuation.inFlight {
		t.Error("the latch was taken mid-run")
	}
	if got := plain(m.View()); !strings.Contains(got, commandsAtIdleNote) {
		t.Errorf("the refusal note is missing from the transcript:\n%s", got)
	}
}

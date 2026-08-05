package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// ----------------------------------------------------------------------------
// The pre-bound session — first boot asks, an empty list guides (prebound.go)
// ----------------------------------------------------------------------------

// fakeBind stands in for the composition root's bind closure: it records every name it was asked to
// construct on and answers with what the binary would have returned — the entry's endpoint, its name
// as the alias, and the global window pin. Called synchronously on the test's goroutine, so it needs
// no guard.
type fakeBind struct {
	calls  []string
	answer func(name string) (ServerSwitchResult, error)
}

func (f *fakeBind) bind(name string) (ServerSwitchResult, error) {
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

// fakeRecorder stands in for the `server:` splice writer behind [Options.RecordServerChoice]. saved
// is what it reports having done: true is the binary's answer for a name the `servers:` list holds,
// and the zero value is its silent skip for one it does not.
type fakeRecorder struct {
	names []string
	saved bool
	err   error
}

func (f *fakeRecorder) record(name string) (bool, error) {
	f.names = append(f.names, name)
	return f.saved, f.err
}

// preboundOpts is the Options the binary hands a session it could not resolve a server for: the
// configured list and both server seams wired, the heartbeat wired (it answers the zero Beat until
// something is bound), and NO upstream facts at all — no endpoint, no alias, no model.
func preboundOpts(reason PreboundReason, name string) Options {
	opts := testOpts
	opts.Endpoint, opts.HostAlias, opts.Model, opts.ContextWindow = "", "", "", 0
	opts.Servers = twoServers
	opts.Prebound = PreboundStart{Reason: reason, Name: name}
	return opts
}

// preboundModel is a ready 80×24 model in the pre-bound state, with every seam a real one wired.
func preboundModel(t *testing.T, reason PreboundReason, name string,
	bind *fakeBind, rec *fakeRecorder,
) (Model, *fakeEngine) {
	t.Helper()
	opts := preboundOpts(reason, name)
	opts.SwitchServer = (&fakeSwitch{}).switchTo
	opts.BindServer = bind.bind
	opts.RecordServerChoice = rec.record
	opts.Heartbeat = (&fakeHeartbeat{}).beat
	opts.Rebind = (&fakeRebind{}).rebind
	eng := &fakeEngine{}
	return newTestModelEng(t, eng, opts), eng
}

// Both askable reasons open the `/server` picker unasked, over the configured list, under a notice
// saying why it came up. The stale one names the entry that went missing — the whole reason it is a
// reason of its own.
func TestPreboundStartOpensTheServerPickerWithItsNotice(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason PreboundReason
		stale  string
		want   string
	}{
		{"first boot", PreboundFirstBoot, "", firstBootNotice},
		{"stale choice", PreboundStaleChoice, "old-box", `server: "old-box" names no configured entry`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := preboundModel(t, tc.reason, tc.stale, &fakeBind{}, &fakeRecorder{})

			if !m.picker.open || m.picker.kind != pickerServer {
				t.Fatalf("picker = %+v, want the server picker open at start-up", m.picker)
			}
			if m.settings.open {
				t.Error("the settings pane opened too; only one surface asks")
			}
			if n := m.pickerCount(); n != len(twoServers) {
				t.Errorf("picker rows = %d, want the %d configured entries", n, len(twoServers))
			}
			notes := noteTexts(m)
			if len(notes) == 0 || !strings.Contains(notes[len(notes)-1], tc.want) {
				t.Errorf("notes = %v, want the last to carry %q", notes, tc.want)
			}
			if !m.prebound() {
				t.Error("the model does not report itself pre-bound")
			}
		})
	}
}

// Nothing is observed while nothing is bound: the seam is wired (it is the same holder the bind
// fills), but a beat against no Monitor would paint the session offline against an endpoint nobody
// has named yet.
func TestPreboundSessionIssuesNoBeat(t *testing.T) {
	m, _ := preboundModel(t, PreboundFirstBoot, "", &fakeBind{}, &fakeRecorder{})

	if cmd := m.beatCmd(); cmd != nil {
		t.Error("a pre-bound session issued a beat; there is no Monitor behind the seam yet")
	}
	if cmd := m.Init(); cmd != nil {
		if _, ok := cmdMsg(cmd).(beatMsg); ok {
			t.Error("Init fired the session's first beat with nothing bound")
		}
	}
}

// ⏎ on a row is the whole of the first boot: the engine is constructed on that entry, the choice is
// recorded as the one the next session starts on, the display adopts what came back, and the state
// is over.
func TestPreboundChoiceBindsRecordsAndEndsTheState(t *testing.T) {
	bind, rec := &fakeBind{}, &fakeRecorder{saved: true}
	m, _ := preboundModel(t, PreboundFirstBoot, "", bind, rec)

	m = step(t, m, keyDown()) // highlight the second entry, so the accept cannot pass by luck
	m, cmd := stepCmd(t, m, keyEnter())

	if want := []string{"remote"}; !reflect.DeepEqual(bind.calls, want) {
		t.Fatalf("bind calls = %v, want %v", bind.calls, want)
	}
	if want := []string{"remote"}; !reflect.DeepEqual(rec.names, want) {
		t.Errorf("recorded names = %v, want %v — the choice is what the next session starts on", rec.names, want)
	}
	if m.prebound() {
		t.Error("the session is still pre-bound after a committed bind")
	}
	if m.picker.open {
		t.Error("the picker stayed open after an accept")
	}
	if m.opts.Endpoint != "http://remote:8080" || m.opts.HostAlias != "remote" {
		t.Errorf("opts = {%q %q}, want the bind result adopted", m.opts.Endpoint, m.opts.HostAlias)
	}
	if m.opts.ContextWindow != remoteWindow {
		t.Errorf("opts.ContextWindow = %d, want the surviving pin %d", m.opts.ContextWindow, remoteWindow)
	}
	if box := m.transcript.entries[0]; box.kind != entryStartup || box.startup.Host != "remote" {
		t.Errorf("start-up box = %+v, want it restated on the server just bound", box.startup)
	}
	// One line for one act: the server bound, and — because that choice was written — the key that
	// now remembers it, stated at the end of the same note.
	if want := "server bound: remote (http://remote:8080)" + savedChoiceClause; countNotes(m, want) != 1 {
		t.Errorf("notes = %v, want exactly one %q", noteTexts(m), want)
	}
	// A bind is a COLD start, not a switch: nothing was ever observed, so the first beat that lands
	// seeds quietly under the box restated a few rows above (foldServerBind).
	if m.hb.switched || m.hb.everOnline || m.hb.failures != 0 {
		t.Errorf("hb = %+v, want the cold-start posture a first bind is", m.hb)
	}
	if cmd == nil {
		t.Fatal("the bind returned no Cmd — the new server would not be observed for a full Interval")
	}
	beat, ok := cmdMsg(cmd).(beatMsg)
	if !ok {
		t.Fatalf("the bind's Cmd yielded %T, want the first chain's beatMsg", cmdMsg(cmd))
	}
	if beat.gen != m.hb.gen {
		t.Errorf("the first beat carries generation %d, want the armed %d", beat.gen, m.hb.gen)
	}
}

// "/server <name>" is the same accept by another route, so it binds too rather than trying to switch
// an engine that does not exist.
func TestPreboundArgumentFormBinds(t *testing.T) {
	bind, rec := &fakeBind{}, &fakeRecorder{}
	m, _ := preboundModel(t, PreboundStaleChoice, "old-box", bind, rec)
	m = step(t, m, keyEsc())

	m, _ = typeCommand(t, m, "/server remote")

	if want := []string{"remote"}; !reflect.DeepEqual(bind.calls, want) {
		t.Fatalf("bind calls = %v, want %v", bind.calls, want)
	}
	if m.prebound() || m.opts.Endpoint != "http://remote:8080" {
		t.Errorf("the argument form did not bind: prebound %v, endpoint %q", m.prebound(), m.opts.Endpoint)
	}
}

// A refused construction leaves the session exactly as unbound as it was — the seam is
// validate-then-commit — and says so.
func TestPreboundBindFailureLeavesTheSessionUnbound(t *testing.T) {
	bind := &fakeBind{answer: func(string) (ServerSwitchResult, error) {
		return ServerSwitchResult{}, errors.New("auto needs a confinement backend")
	}}
	rec := &fakeRecorder{}
	m, _ := preboundModel(t, PreboundFirstBoot, "", bind, rec)

	m, cmd := stepCmd(t, m, keyEnter())

	if !m.prebound() || m.opts.Endpoint != "" {
		t.Errorf("a refused bind moved the session: prebound %v, endpoint %q", m.prebound(), m.opts.Endpoint)
	}
	if cmd != nil {
		t.Error("a refused bind returned a Cmd; nothing was constructed to observe")
	}
	if len(rec.names) != 0 {
		t.Errorf("recorded %v, want nothing — no choice took effect", rec.names)
	}
	want := "could not bind server: auto needs a confinement backend"
	if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
		t.Errorf("notes = %v, want %q", got, want)
	}
}

// The recording is best-effort persistence of something already true: a failed write warns and the
// session stays on the server it just bound.
func TestPreboundRecordFailureWarnsAndTheBindStands(t *testing.T) {
	bind := &fakeBind{}
	rec := &fakeRecorder{err: errors.New("permission denied")}
	m, _ := preboundModel(t, PreboundFirstBoot, "", bind, rec)

	m = step(t, m, keyEnter())

	if m.prebound() || m.opts.HostAlias != "test-host" {
		t.Errorf("a failed recording undid the bind: prebound %v, alias %q", m.prebound(), m.opts.HostAlias)
	}
	if n := countNotes(m, "could not record the server choice: permission denied"); n != 1 {
		t.Errorf("notes = %v, want one recording warning", noteTexts(m))
	}
}

// esc closes the pane that asked and leaves the session's own fact on the status line — the licence
// layout.md gives every surface that gives way.
func TestPreboundEscLeavesTheStatusLineFact(t *testing.T) {
	m, _ := preboundModel(t, PreboundFirstBoot, "", &fakeBind{}, &fakeRecorder{})

	m = step(t, m, keyEsc())

	if m.picker.open {
		t.Fatal("esc did not close the picker")
	}
	if !m.prebound() {
		t.Error("closing the picker bound something")
	}
	if got := plain(m.View()); !strings.Contains(got, noServerBoundFact) {
		t.Errorf("the frame does not carry %q:\n%s", noServerBoundFact, got)
	}
}

// A message typed while pre-bound reaches no engine: the ask comes back up carrying the same notice
// it opened with, and the message stays in the box for the ⏎ that follows the choice.
func TestPreboundSubmitReopensThePicker(t *testing.T) {
	m, eng := preboundModel(t, PreboundFirstBoot, "", &fakeBind{}, &fakeRecorder{})
	m = step(t, m, keyEsc())
	notesBefore := len(noteTexts(m))

	m.input.SetValue("what is the capital of France?")
	m, cmd := stepCmd(t, m, keyEnter())

	if cmd != nil || len(eng.submitted) != 0 {
		t.Errorf("a send reached the engine with nothing bound (submitted %d)", len(eng.submitted))
	}
	if !m.picker.open || m.picker.kind != pickerServer {
		t.Errorf("picker = %+v, want the ask back up", m.picker)
	}
	if got := noteTexts(m); len(got) != notesBefore+1 || got[len(got)-1] != firstBootNotice {
		t.Errorf("notes = %v, want one more, %q", got, firstBootNotice)
	}
	if got := m.input.Value(); got != "what is the capital of France?" {
		t.Errorf("input = %q, want the message left where the human wrote it", got)
	}
}

// The two verbs that open an Exchange without a message answer to the same truth, in words that name
// the state rather than an endpoint that does not exist.
func TestPreboundRefusesTheExchangeVerbs(t *testing.T) {
	m, eng := preboundModel(t, PreboundFirstBoot, "", &fakeBind{}, &fakeRecorder{})
	m = step(t, m, keyEsc())

	m, cmd := typeCommand(t, m, "/compact")

	if cmd != nil || eng.compactCalls != 0 {
		t.Errorf("/compact reached the engine with nothing bound (%d calls)", eng.compactCalls)
	}
	if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != firstBootNotice {
		t.Errorf("notes = %v, want %q", got, firstBootNotice)
	}
}

// Nothing configured is a different question, so it gets a different surface: the `/settings` pane,
// whose read-only `servers` row already points at the file that has to be edited. No picker opens —
// there is nothing in it — and the guidance rides the status line.
func TestPreboundNoServersOpensSettings(t *testing.T) {
	opts := preboundOpts(PreboundNoServers, "")
	opts.Servers = nil
	opts.BindServer = (&fakeBind{}).bind
	opts.SettingsRows = func() []SettingRow { return settingsTestRows(6) }
	m := newTestModelEng(t, &fakeEngine{}, opts)

	if !m.settings.open {
		t.Fatal("the settings pane did not open for a config with no servers")
	}
	if m.picker.open {
		t.Error("a picker opened over an empty list")
	}
	m = step(t, m, keyEsc())
	if got := plain(m.View()); !strings.Contains(got, noServersConfiguredFact) {
		t.Errorf("the frame does not carry %q:\n%s", noServersConfiguredFact, got)
	}
}

// With no pane to open — no rows provider, the same degrade /settings itself takes — the guidance is
// still said, and a message typed against it earns the same sentence rather than a picker over
// nothing.
func TestPreboundNoServersGuidesWithoutAPane(t *testing.T) {
	opts := preboundOpts(PreboundNoServers, "")
	opts.Servers = nil
	opts.BindServer = (&fakeBind{}).bind
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, opts)

	if m.settings.open || m.picker.open {
		t.Error("a pane opened with nothing to show in it")
	}
	if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != noServersConfiguredFact {
		t.Errorf("notes = %v, want %q", got, noServersConfiguredFact)
	}

	m.input.SetValue("hello")
	m, cmd := stepCmd(t, m, keyEnter())

	if cmd != nil || len(eng.submitted) != 0 {
		t.Errorf("a send reached the engine with nothing configured (submitted %d)", len(eng.submitted))
	}
	if m.picker.open {
		t.Error("a picker opened over an empty list")
	}
	if got := noteTexts(m); got[len(got)-1] != noServersConfiguredFact {
		t.Errorf("notes = %v, want %q", got, noServersConfiguredFact)
	}
}

// An ordinary bound session is untouched by any of this: the zero PreboundStart is the whole of the
// opt-in, so no pane opens unasked and the status line stays the queue's.
func TestBoundStartOpensNothing(t *testing.T) {
	m, _ := seededServers(t, &fakeSwitch{})

	if m.prebound() || m.picker.open || m.settings.open {
		t.Errorf("a bound session started with a pane up: prebound %v, picker %+v", m.prebound(), m.picker)
	}
	if got := plain(m.View()); strings.Contains(got, noServerBoundFact) {
		t.Errorf("a bound session paints the pre-bound fact:\n%s", got)
	}
}

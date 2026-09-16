package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// /fork — the picker, the cut and the switch
// ----------------------------------------------------------------------------

// The three prompts every fork test forks among, in transcript order.
var forkPrompts = [3]string{"first ask", "second ask", "third ask"}

// newForkModel is a ready, idle model wired to a session host whose transcript holds three answered
// prompts — three fork points, none folded, none aborted.
func newForkModel(t *testing.T, eng *fakeEngine, host *fakeSessionHost) Model {
	t.Helper()
	m := newSessionModel(t, eng, host)
	for i, text := range forkPrompts {
		m.transcript.addUser(text, nil)
		m.transcript.apply(domain.MessageEvent{Text: "answer " + strings.Repeat("!", i+1)})
	}
	return m
}

// openForkPicker submits /fork and asserts the picker opened on the fork kind.
func openForkPicker(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = typeCommand(t, m, "/fork")
	if !m.picker.open || m.picker.kind != pickerFork {
		t.Fatalf("picker = %+v, want the fork kind open", m.picker)
	}
	return m
}

// assertNoFork is the shared negative: the picker is closed, no fork reached the host, and the
// transcript's last note is want.
func assertNoFork(t *testing.T, m Model, host *fakeSessionHost, want string) {
	t.Helper()
	if m.picker.open {
		t.Error("the picker is open; want no overlay")
	}
	if got := host.forkCalls(); len(got) != 0 {
		t.Errorf("host saw %d forks; want none", len(got))
	}
	if got := lastNote(m); got != want {
		t.Errorf("last note = %q, want %q", got, want)
	}
}

// /fork lists the session's prompts, numbered in transcript order, and ⏎ on one forks there: the
// engine is cut by the row's drop count, the parent is saved first, the fork reaches the host behind
// that save with the prefix through the chosen prompt, and when the write lands the TUI switches to
// the child — the host's active id is the child's, the engine was restored with the cut state, and
// the note names the parent.
func TestForkPickerListsPromptsAndForks(t *testing.T) {
	t.Parallel()

	eng := &fakeEngine{}
	host := &fakeSessionHost{}
	m := newForkModel(t, eng, host)
	m = openForkPicker(t, m)

	rows := m.pickerOfferingRows()
	if len(rows) != 3 {
		t.Fatalf("rows = %v, want one per prompt", rows)
	}
	for i, row := range rows {
		wantNumber, wantText := []string{"1.", "2.", "3."}[i], forkPrompts[i]
		if len(row) != 2 || row[0] != wantNumber || row[1] != wantText {
			t.Errorf("row %d = %v, want [%q %q]", i, row, wantNumber, wantText)
		}
	}
	if got := m.pickerTitle(); got != forkPickerTitle {
		t.Errorf("title = %q, want %q", got, forkPickerTitle)
	}

	m = step(t, m, keyDown()) // highlight row 2
	m, cmd := stepCmd(t, m, keyEnter())

	if m.picker.open {
		t.Error("the picker stayed open after ⏎")
	}
	if got := eng.cutCalls; len(got) != 1 || got[0] != 1 {
		t.Fatalf("CutSnapshot drop counts = %v, want [1] — one Exchange after the second prompt", got)
	}
	if cmd == nil {
		t.Fatal("⏎ dispatched nothing; want the parent's Save with the fork queued behind it")
	}
	if len(m.pendingWrites) != 1 || m.pendingWrites[0].kind != writeFork {
		t.Fatalf("pending writes = %+v; want the fork alone, waiting behind the save", m.pendingWrites)
	}
	m = runWrites(t, m, cmd)

	saves, forks := host.savedCalls(), host.forkCalls()
	if len(saves) != 1 || len(forks) != 1 {
		t.Fatalf("host saw %d saves and %d forks; want one of each, the save first", len(saves), len(forks))
	}
	if forks[0].parentID != saves[0].id {
		t.Errorf("fork stamped parent %q; want the id the idle save minted, %q", forks[0].parentID, saves[0].id)
	}
	if forks[0].title != sessionTitle(forkPrompts[0]) {
		t.Errorf("child title = %q; want the parent's, %q", forks[0].title, sessionTitle(forkPrompts[0]))
	}
	var users []string
	for _, e := range forks[0].transcript {
		if e.Kind == "user" {
			users = append(users, e.Text)
		}
	}
	if want := []string{forkPrompts[0], forkPrompts[1]}; strings.Join(users, "|") != strings.Join(want, "|") {
		t.Errorf("the child's transcript holds prompts %v; want %v — ending before the third", users, want)
	}
	if got := host.ActiveID(); got == "" || got == saves[0].id {
		t.Errorf("active id = %q after the fork landed; want the child's, not the parent's %q", got, saves[0].id)
	}
	if got := eng.restores(); len(got) != 1 {
		t.Errorf("RestoreSession ran %d times; want once, with the cut state", len(got))
	}
	if m.sessionName != sessionTitle(forkPrompts[0]) {
		t.Errorf("session name = %q; want the parent's title carried onto the child", m.sessionName)
	}
	if !m.autoTitleFired {
		t.Error("the naming call is armed on the child; want it latched off as after a resume")
	}
	want := "forked from " + saves[0].id + " — " + sessionTitle(forkPrompts[0])
	if got := lastNote(m); got != want {
		t.Errorf("last note = %q, want %q", got, want)
	}
}

// A session whose every prompt lies before its last fold has no State to stand at: /fork says so
// and opens nothing.
func TestForkRefusesAFoldedSession(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	m := newForkModel(t, &fakeEngine{}, host)
	m.transcript.addCompacted(runRef{})

	m, _ = typeCommand(t, m, "/fork")

	assertNoFork(t, m, host, forkFoldedNote)
}

// A session that has not spoken has nothing to fork.
func TestForkRefusesAnEmptySession(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	m := newSessionModel(t, &fakeEngine{}, host)

	m, _ = typeCommand(t, m, "/fork")

	assertNoFork(t, m, host, nothingToForkNote)
}

// Esc closes the picker and forks nothing.
func TestForkEscClosesThePicker(t *testing.T) {
	t.Parallel()

	eng := &fakeEngine{}
	host := &fakeSessionHost{}
	m := newForkModel(t, eng, host)
	m = openForkPicker(t, m)

	m, cmd := stepCmd(t, m, keyEsc())

	if m.picker.open {
		t.Error("the picker is still open after esc")
	}
	if cmd != nil || len(m.pendingWrites) != 0 || len(eng.cutCalls) != 0 || len(host.forkCalls()) != 0 {
		t.Errorf("esc did something: cmd=%v pending=%d cuts=%v forks=%d; want nothing",
			cmd != nil, len(m.pendingWrites), eng.cutCalls, len(host.forkCalls()))
	}
}

// /fork is idle-only by the commandSpecs table — the cut reads the engine, which is the Model's own
// only at idle — so a line typed mid-run is queued to run at idle instead of opening the picker.
func TestForkIsIdleOnly(t *testing.T) {
	t.Parallel()

	if spec, ok := commandByName("fork"); !ok || spec.whileRunning || spec.touchesServer || spec.takesArgs {
		t.Fatalf("commandSpec = %+v, want a bare idle-only verb that touches no server", spec)
	}
	m := newForkModel(t, &fakeEngine{}, &fakeSessionHost{})
	m, _ = typeCommand(t, m, "open the exchange")
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}

	m, _ = typeCommand(t, m, "/fork")

	if m.picker.open {
		t.Error("the picker opened mid-run; /fork is idle-only")
	}
	if got := plain(m.View()); !strings.Contains(got, "queued command: /fork") {
		t.Errorf("the queued-command row is missing from the band:\n%s", got)
	}
}

// Without a session host there is no record to cut a child from: /fork is refused up front with
// the /sessions posture rather than queueing a write the queue would drop.
func TestForkRefusesWithoutASessionHost(t *testing.T) {
	t.Parallel()

	m := newTestModelEng(t, &fakeEngine{}, testOpts)
	seedConversation(&m)

	m, cmd := typeCommand(t, m, "/fork")

	if cmd != nil || m.picker.open {
		t.Errorf("cmd=%v picker=%v; want no Cmd and no overlay", cmd != nil, m.picker.open)
	}
	if got := lastNote(m); got != noSessionHostNote {
		t.Errorf("last note = %q, want %q", got, noSessionHostNote)
	}
}

// A prompt sent between ⏎ and the fork's completion fold opens an Exchange the resume's restore
// would refuse: the switch is skipped, the record is already written, and the note names the
// child so /sessions can reach it. The active id stays the parent's.
func TestForkSkipsTheSwitchWhenBusy(t *testing.T) {
	t.Parallel()

	eng := &fakeEngine{}
	host := &fakeSessionHost{}
	m := newForkModel(t, eng, host)
	m = openForkPicker(t, m)
	m, saveCmd := stepCmd(t, m, keyEnter()) // row 1: the parent's Save dispatches, the fork waits
	if saveCmd == nil {
		t.Fatal("⏎ dispatched no save")
	}

	m, _ = typeCommand(t, m, "keep going") // the human sends a prompt before the write lands
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}
	m = runWrites(t, m, saveCmd) // the save lands, the fork is pumped, lands and folds

	saves, forks := host.savedCalls(), host.forkCalls()
	if len(saves) != 1 || len(forks) != 1 {
		t.Fatalf("host saw %d saves and %d forks; want one of each", len(saves), len(forks))
	}
	if got := eng.restores(); len(got) != 0 {
		t.Errorf("RestoreSession ran %d times mid-Exchange; want the switch skipped", len(got))
	}
	if got := host.ActiveID(); got != saves[0].id {
		t.Errorf("active id = %q; want the parent's %q unchanged", got, saves[0].id)
	}
	childID := ""
	for id := range host.stored {
		childID = id
	}
	if childID == "" || childID == saves[0].id {
		t.Fatalf("stored = %v; want the child record written beside the parent", host.stored)
	}
	want := "forked as " + childID + " — resume it from /sessions"
	if got := lastNote(m); got != want {
		t.Errorf("last note = %q, want %q", got, want)
	}
}

// A fork the host refused is said out loud — the human is waiting on it — and the session stays
// where it was.
func TestForkNotesAHostRefusal(t *testing.T) {
	t.Parallel()

	eng := &fakeEngine{}
	host := &fakeSessionHost{forkErr: errForkRefused}
	m := newForkModel(t, eng, host)
	m = openForkPicker(t, m)
	m, cmd := stepCmd(t, m, keyEnter())

	m = runWrites(t, m, cmd)

	if got := eng.restores(); len(got) != 0 {
		t.Errorf("RestoreSession ran %d times after a refused fork; want none", len(got))
	}
	if got := lastNote(m); got != "could not fork: "+errForkRefused.Error() {
		t.Errorf("last note = %q, want the refusal", got)
	}
}

var errForkRefused = errors.New("the store refused the child")

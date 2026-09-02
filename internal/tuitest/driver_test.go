package tuitest

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The driver's whole job is to be a terminal: bytes in through the key parser, bytes out into the
// emulator, and the terminal's own answers handed back so the renderer's queries do not hang. Each
// half is asserted against a REAL tea.Program here, because a driver that agrees with a hand-built
// Msg proves nothing about the parser the shipped binary runs.

// probe is the model the driver tests drive: it records every Msg it is handed and paints a fixed
// frame. Its recorder is a pointer so the value-copy Bubble Tea does on every Update keeps writing
// to the same log (ADR 0011's rule, from the other side).
type probe struct {
	log  *msgLog
	view string
}

func (p probe) Init() tea.Cmd { return nil }

func (p probe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	p.log.add(msg)
	return p, nil
}

func (p probe) View() tea.View { return tea.NewView(p.view) }

// msgLog is the probe's recorder.
type msgLog struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (l *msgLog) add(msg tea.Msg) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, msg)
}

// keys returns every key press recorded so far, in order.
func (l *msgLog) keys() []tea.KeyPressMsg {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []tea.KeyPressMsg
	for _, msg := range l.msgs {
		if k, ok := msg.(tea.KeyPressMsg); ok {
			out = append(out, k)
		}
	}
	return out
}

// sizes returns every window size recorded so far, in order.
func (l *msgLog) sizes() []tea.WindowSizeMsg {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []tea.WindowSizeMsg
	for _, msg := range l.msgs {
		if s, ok := msg.(tea.WindowSizeMsg); ok {
			out = append(out, s)
		}
	}
	return out
}

// count is how many messages of any kind have been recorded.
func (l *msgLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.msgs)
}

// driveProbe starts a probe program under a fresh driver and returns both. The probe binds no quit
// key — what is under test is the input path, not a model — so the run is ended on cleanup by
// [Driver.Kill].
//
// [CheckLeaks] comes first, before anything is started, so its cleanup runs last. It is what makes
// a driver that leaves a goroutine behind fail HERE rather than in whichever later test happens to
// scan next: a killed program's read loop outliving its teardown was exactly that bug.
func driveProbe(t *testing.T, view string) (*Driver, *msgLog) {
	t.Helper()

	CheckLeaks(t)
	drv := NewDriver(t, Size{W: 40, H: 8})
	log := &msgLog{}
	ctx, cancel := context.WithCancel(context.Background())
	opts := append(drv.ProgramOptions(), tea.WithContext(ctx), tea.WithOutput(drv.Output()))
	prog := tea.NewProgram(probe{log: log, view: view}, opts...)
	drv.Attach(prog, cancel)
	go func() {
		_, err := prog.Run()
		drv.Finished(err)
	}()
	// Kill rather than cancel: it ends the input before it cancels, which is what keeps a killed
	// program's cancel reader from being closed under a live read loop (see Driver.Kill).
	t.Cleanup(drv.Kill)
	// The program is up once its first frame has reached the emulator.
	drv.WaitText(view)
	return drv, log
}

// TestKeysDecodeAsIntended is the pin under every Press in the suite: each Key's bytes, fed through
// a real program, arrive as the key it is named after. It is the reason the constants can be byte
// strings at all — if bubbletea's parser changes its mind about a sequence, this fails rather than
// some distant pane test that silently stopped pressing what it thought it pressed.
func TestKeysDecodeAsIntended(t *testing.T) {
	drv, log := driveProbe(t, "keys")

	cases := []struct {
		key  Key
		code rune
		mod  tea.KeyMod
		name string
	}{
		{key: Enter, code: tea.KeyEnter, name: "enter"},
		{key: Tab, code: tea.KeyTab, name: "tab"},
		{key: ShiftTab, code: tea.KeyTab, mod: tea.ModShift, name: "shift+tab"},
		{key: Space, code: tea.KeySpace, name: "space"},
		{key: Backspace, code: tea.KeyBackspace, name: "backspace"},
		{key: Up, code: tea.KeyUp, name: "up"},
		{key: Down, code: tea.KeyDown, name: "down"},
		{key: Right, code: tea.KeyRight, name: "right"},
		{key: Left, code: tea.KeyLeft, name: "left"},
		{key: PgUp, code: tea.KeyPgUp, name: "pgup"},
		{key: PgDown, code: tea.KeyPgDown, name: "pgdown"},
		{key: AltUp, code: tea.KeyUp, mod: tea.ModAlt, name: "alt+up"},
		{key: AltDown, code: tea.KeyDown, mod: tea.ModAlt, name: "alt+down"},
		{key: CtrlC, code: 'c', mod: tea.ModCtrl, name: "ctrl+c"},
		{key: F1, code: tea.KeyF1, name: "f1"},
		{key: F2, code: tea.KeyF2, name: "f2"},
		{key: F3, code: tea.KeyF3, name: "f3"},
		{key: F4, code: tea.KeyF4, name: "f4"},
		{key: F5, code: tea.KeyF5, name: "f5"},
		{key: F6, code: tea.KeyF6, name: "f6"},
		{key: F7, code: tea.KeyF7, name: "f7"},
		{key: F8, code: tea.KeyF8, name: "f8"},
		{key: F9, code: tea.KeyF9, name: "f9"},
		{key: F10, code: tea.KeyF10, name: "f10"},
		{key: F11, code: tea.KeyF11, name: "f11"},
		{key: F12, code: tea.KeyF12, name: "f12"},
		// Esc is last on purpose: it resolves only after the reader's escape timeout, so a key
		// pressed behind it would arrive first and make the order a lie.
		{key: Esc, code: tea.KeyEscape, name: "esc"},
	}

	for i, tc := range cases {
		drv.Press(tc.key)
		want := i + 1
		drv.WaitFor(func() bool { return len(log.keys()) >= want },
			Awaiting("the "+tc.name+" key press to reach the model"))
	}

	got := log.keys()
	if len(got) < len(cases) {
		t.Fatalf("recorded %d key presses, want %d", len(got), len(cases))
	}
	for i, tc := range cases {
		if got[i].Code != tc.code || got[i].Mod != tc.mod {
			t.Errorf("%s (%q) decoded as %q (code %q, mod %d); want code %q, mod %d",
				tc.name, string(tc.key), got[i].String(), got[i].Code, got[i].Mod, tc.code, tc.mod)
		}
	}
}

// TestDriverTypeDeliversRunesInOrder: what a driver types is what the model reads, in order, one
// key press per rune — including the non-ASCII ones, which is where a byte-at-a-time input would
// come apart.
func TestDriverTypeDeliversRunesInOrder(t *testing.T) {
	drv, log := driveProbe(t, "type")

	const typed = "héllo"
	drv.Type(typed)
	want := len([]rune(typed))
	drv.WaitFor(func() bool { return len(log.keys()) >= want },
		Awaiting("every typed rune to reach the model"))

	var read strings.Builder
	for _, k := range log.keys() {
		read.WriteString(k.Text)
	}
	if read.String() != typed {
		t.Errorf("the model read %q; want the typed %q", read.String(), typed)
	}
}

// TestDriverBurstOfKeysArrivesWhole: six presses with no wait between them are six key presses at
// the model, not one. It is the pin that says a lost keystroke in a driver test is the program's
// doing and not the driver's — the question that otherwise costs an afternoon.
func TestDriverBurstOfKeysArrivesWhole(t *testing.T) {
	drv, log := driveProbe(t, "burst")

	const burst = 6
	for range burst {
		drv.Press(Down)
	}
	drv.WaitFor(func() bool { return len(log.keys()) >= burst },
		Awaiting("all six presses of a burst to reach the model"))

	for i, k := range log.keys()[:burst] {
		if k.Code != tea.KeyDown {
			t.Errorf("burst key %d = %q; want down", i, k.String())
		}
	}
}

// TestDriverResizeReachesTheModel: Resize is two things at once — the emulator reflows, and the
// program is told. A driver that did only the first would show a test a frame the program never
// painted.
func TestDriverResizeReachesTheModel(t *testing.T) {
	drv, log := driveProbe(t, "resize")

	drv.Resize(60, 20)
	drv.WaitFor(func() bool {
		for _, s := range log.sizes() {
			if s.Width == 60 && s.Height == 20 {
				return true
			}
		}
		return false
	}, Awaiting("the WindowSizeMsg for 60x20"))

	if f := drv.Frame(); f.Width() != 60 || f.Height() != 20 {
		t.Errorf("the frame is %dx%d after Resize(60, 20)", f.Width(), f.Height())
	}
}

// TestDriverPumpsTerminalAnswers is the half that is easiest to forget and hangs everything when it
// is missing: the renderer asks the terminal what it is (DA1), and the driver hands back what the
// emulator answered. Asserted from the program's side — the answer arrives as a decoded event, not
// as a key press, and the probe records every Msg it is handed.
func TestDriverPumpsTerminalAnswers(t *testing.T) {
	drv, log := driveProbe(t, "answers")

	before := log.count()
	// A primary device-attributes query. The emulator answers it (x/vt handles DA1 itself); the
	// pump is what turns that answer back into program input.
	if _, err := drv.Screen().Write([]byte("\x1b[c")); err != nil {
		t.Fatalf("Screen().Write(DA1 query) = %v, want nil", err)
	}
	drv.WaitFor(func() bool { return log.count() > before },
		Awaiting("the terminal's DA1 answer to reach the program"))
}

// TestDriverQuitReturnsTheRunError: Quit hands back what the run returned, and does not hang doing
// it. The probe quits on Ctrl+C like any tea program without a key map of its own would not, so the
// quit is driven here by the same two presses a human makes and the model's own Quit command.
func TestDriverQuitReturnsTheRunError(t *testing.T) {
	CheckLeaks(t)
	drv := NewDriver(t, Size{W: 30, H: 5})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opts := append(drv.ProgramOptions(), tea.WithContext(ctx), tea.WithOutput(drv.Output()))
	prog := tea.NewProgram(quitOnSecondCtrlC{}, opts...)
	drv.Attach(prog, cancel)
	go func() {
		_, err := prog.Run()
		drv.Finished(err)
	}()
	drv.WaitText("press ctrl+c twice")

	if err := drv.Quit(); err != nil {
		t.Errorf("Quit: %v; want the clean run result", err)
	}
}

// quitOnSecondCtrlC is apogee's own quit gesture, reduced to the part a driver has to be able to
// perform: one Ctrl+C arms, the second ends the program.
type quitOnSecondCtrlC struct{ armed bool }

func (m quitOnSecondCtrlC) Init() tea.Cmd { return nil }

func (m quitOnSecondCtrlC) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "ctrl+c" {
		if m.armed {
			return m, tea.Quit
		}
		m.armed = true
	}
	return m, nil
}

func (m quitOnSecondCtrlC) View() tea.View { return tea.NewView("press ctrl+c twice") }

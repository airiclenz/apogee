package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Mini-language model harness extensions
// ----------------------------------------------------------------------------

// newTestModelEng builds a ready, idle model over a caller-supplied engine and options, so a
// test can inspect the engine (clear/compact/submit calls) the commands drive.
func newTestModelEng(t *testing.T, eng Engine, opts Options) Model {
	t.Helper()
	m := newModel(context.Background(), eng, opts)
	return step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
}

func keyTab() tea.KeyPressMsg  { return tea.KeyPressMsg{Code: tea.KeyTab} }
func keyDown() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyDown} }
func keyUp() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyUp} }

// keyRune is a printable keypress (Code + Text), the way the textarea reads typed input.
func keyRune(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// ----------------------------------------------------------------------------
// /command routing
// ----------------------------------------------------------------------------

func TestClearCommandClearsEngineKeepsTranscript(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("/clear")
	m, cmd := stepCmd(t, m, keyEnter())

	if eng.clearCalls != 1 {
		t.Errorf("ClearContext calls = %d, want 1", eng.clearCalls)
	}
	if m.state != stateIdle {
		t.Errorf("state = %v, want idle (/clear must not launch a worker)", m.state)
	}
	if cmd != nil {
		t.Error("/clear returned a Cmd; it should not launch a worker")
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("input not cleared: %q", v)
	}
	if got := plain(m.View()); !strings.Contains(got, "context cleared") {
		t.Errorf("transcript missing the cleared note:\n%s", got)
	}
}

func TestClearCommandSurfacesEngineError(t *testing.T) {
	eng := &fakeEngine{clearFn: func() error { return domain.ErrInputPending }}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("/clear")
	m = step(t, m, keyEnter())
	if got := plain(m.View()); !strings.Contains(got, "could not clear context") {
		t.Errorf("transcript missing the clear-failure note:\n%s", got)
	}
}

func TestCompactCommandLaunchesWorker(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("/compact")
	m, cmd := stepCmd(t, m, keyEnter())

	if m.state != stateRunning {
		t.Errorf("state = %v, want running (/compact opens a summary call)", m.state)
	}
	if cmd == nil {
		t.Fatal("/compact returned no Cmd; the worker was not launched")
	}
	// Drain the worker Cmd so eng.Compact actually runs on the worker path.
	drainCmd(t, m, cmd)
	if eng.compactCalls != 1 {
		t.Errorf("Compact calls = %d, want 1", eng.compactCalls)
	}
}

func TestCompactDoneAddsNoteAndResetsGauge(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, testOpts)
	m.state = stateRunning // the /compact worker is in flight
	m.ctxUsed = 4200       // the gauge is lit from before compaction

	m = step(t, m, compactDoneMsg{Err: nil})

	if m.state != stateIdle {
		t.Errorf("state = %v after compactDoneMsg, want idle", m.state)
	}
	if m.ctxUsed != 0 {
		t.Errorf("ctxUsed = %d, want 0 (the gauge resets so the next turn re-measures)", m.ctxUsed)
	}
	if got := plain(m.View()); !strings.Contains(got, "context compacted") {
		t.Errorf("transcript missing the success note:\n%s", got)
	}
}

func TestCompactDoneSurfacesError(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, testOpts)
	m.state = stateRunning
	m.ctxUsed = 4200

	m = step(t, m, compactDoneMsg{Err: errors.New("upstream boom")})

	if m.state != stateIdle {
		t.Errorf("state = %v after a failed compact, want idle", m.state)
	}
	if m.ctxUsed != 4200 {
		t.Errorf("ctxUsed = %d, want it unchanged on failure (nothing was compacted)", m.ctxUsed)
	}
	if got := plain(m.View()); !strings.Contains(got, "compact: upstream boom") {
		t.Errorf("transcript missing the failure note:\n%s", got)
	}
}

// A skipped compaction (the reducer's Result.Skipped — conversation too small to fold) folded
// nothing, so the gauge must stay lit and the note must say so plainly rather than falsely
// claiming a compaction. This pins the 2b truthfulness fix at the TUI seam.
func TestCompactDoneSkippedLeavesGaugeAndSaysNothing(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, testOpts)
	m.state = stateRunning // the /compact worker is in flight
	m.ctxUsed = 4200       // the gauge is lit from before; a skip must leave it alone

	m = step(t, m, compactDoneMsg{Skipped: true})

	if m.state != stateIdle {
		t.Errorf("state = %v after a skipped compact, want idle", m.state)
	}
	if m.ctxUsed != 4200 {
		t.Errorf("ctxUsed = %d, want unchanged (a skip folded nothing)", m.ctxUsed)
	}
	got := plain(m.View())
	if !strings.Contains(got, "nothing to compact") {
		t.Errorf("transcript missing the skip note:\n%s", got)
	}
	if strings.Contains(got, "context compacted") {
		t.Errorf("a skip wrongly claimed a compaction:\n%s", got)
	}
}

// A cancelled /compact folds nothing, so — unlike a committed compaction, which zeroes the
// gauge — the cancel must leave ctxUsed exactly as it was, discard the open Exchange
// (AbortExchange, so the next input is accepted), and record the "cancelled" note. The outcome
// is classified from Compact's error (startCompact), so a cancel never masquerades as a
// gauge-resetting compaction.
func TestCancelledCompactLeavesGaugeUntouched(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.state = stateRunning // the /compact worker is in flight
	m.ctxUsed = 4200       // the gauge is lit from before compaction
	m.cancel = func() {}   // stand in for the live worker

	m = step(t, m, cancelledMsg{Result: domain.StepResult{Status: domain.StatusCancelled}})

	if m.state != stateIdle {
		t.Fatalf("state = %v after a cancelled compact, want idle", m.state)
	}
	if m.ctxUsed != 4200 {
		t.Errorf("ctxUsed = %d, want unchanged (a cancelled compact folded nothing)", m.ctxUsed)
	}
	if got := eng.aborts(); got != 1 {
		t.Errorf("AbortExchange called %d times, want 1 (the cancel discards the open Exchange)", got)
	}
	if got := plain(m.View()); !strings.Contains(got, "cancelled") {
		t.Errorf("transcript missing the cancelled note:\n%s", got)
	}
}

func TestContinueCommandLaunchesWorker(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("/continue")
	m, cmd := stepCmd(t, m, keyEnter())

	if m.state != stateRunning {
		t.Errorf("state = %v, want running (/continue opens an agent turn)", m.state)
	}
	if cmd == nil {
		t.Error("/continue returned no Cmd; the worker was not launched")
	}
	if got := plain(m.View()); !strings.Contains(got, "/continue") {
		t.Errorf("transcript missing the /continue user line:\n%s", got)
	}
}

func TestMessageWithFileRefsSubmitsRefs(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()} // immediately ExchangeComplete when driven
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("review @main.go now")
	m, cmd := stepCmd(t, m, keyEnter())
	if m.state != stateRunning {
		t.Fatalf("state = %v, want running", m.state)
	}
	// Drain the worker Cmd chain so Submit actually runs, then inspect the payload.
	drainCmd(t, m, cmd)
	if len(eng.submitted) != 1 {
		t.Fatalf("Submit calls = %d, want 1", len(eng.submitted))
	}
	in := eng.submitted[0]
	if in.Text != "review @main.go now" {
		t.Errorf("submitted text = %q, want the literal message", in.Text)
	}
	if !reflect.DeepEqual(in.FileRefs, []string{"main.go"}) {
		t.Errorf("submitted FileRefs = %v, want [main.go]", in.FileRefs)
	}
}

// drainCmd runs a Cmd, recursively executing any BatchMsg children, so the worker goroutine
// Cmd inside submit's tea.Batch actually drives the (scripted) engine to completion.
func drainCmd(t *testing.T, m Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			drainCmd(t, m, c)
		}
	}
}

// ----------------------------------------------------------------------------
// Autocomplete overlay
// ----------------------------------------------------------------------------

func TestComputeAutocompleteCommands(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/c") // clear, compact, continue all start with "c"
	ac := m.computeAutocomplete()
	if !ac.active || ac.kind != acCommand {
		t.Fatalf("overlay = {active:%v kind:%v}, want active command", ac.active, ac.kind)
	}
	var got []string
	for _, it := range ac.items {
		got = append(got, it.value)
	}
	if !reflect.DeepEqual(got, []string{"clear", "compact", "continue"}) {
		t.Errorf("suggestions = %v, want all three commands", got)
	}
}

func TestComputeAutocompleteNarrowsAndExact(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/cl") // only "clear"
	ac := m.computeAutocomplete()
	if len(ac.items) != 1 || ac.items[0].value != "clear" {
		t.Fatalf("suggestions = %v, want [clear]", ac.items)
	}
	// A plain message is not a command, and yields no overlay.
	m.input.SetValue("hello there")
	if ac := m.computeAutocomplete(); ac.active {
		t.Error("plain text opened the overlay")
	}
}

func TestAutocompleteAcceptWithTab(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/cl")
	m.autocomplete = m.computeAutocomplete() // simulate the post-edit recompute
	m = step(t, m, keyTab())
	if got := m.input.Value(); got != "/clear " {
		t.Errorf("after tab-accept input = %q, want %q", got, "/clear ")
	}
	if m.autocomplete.active {
		t.Error("overlay still open after accept")
	}
}

func TestAutocompleteNavigateThenAccept(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/c")
	m.autocomplete = m.computeAutocomplete() // [clear, compact, continue], selected 0
	m = step(t, m, keyDown())                // → compact
	if m.autocomplete.selected != 1 {
		t.Fatalf("after down selected = %d, want 1", m.autocomplete.selected)
	}
	m = step(t, m, keyUp())   // → clear
	m = step(t, m, keyDown()) // → compact
	m = step(t, m, keyTab())
	if got := m.input.Value(); got != "/compact " {
		t.Errorf("accepted %q, want /compact ", got)
	}
}

func TestAutocompleteEscDismisses(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/c")
	m.autocomplete = m.computeAutocomplete()
	m = step(t, m, keyEsc())
	if m.autocomplete.active {
		t.Error("esc did not dismiss the overlay")
	}
	if m.input.Value() != "/c" {
		t.Errorf("esc altered the input: %q", m.input.Value())
	}
	if m.state != stateIdle {
		t.Errorf("esc changed state to %v; at idle it should be a no-op beyond closing the overlay", m.state)
	}
}

func TestAutocompleteEnterExactSubmits(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("/clear") // exactly the only suggestion
	m.autocomplete = m.computeAutocomplete()
	m = step(t, m, keyEnter()) // exact match ⇒ Enter submits, not re-completes
	if eng.clearCalls != 1 {
		t.Errorf("ClearContext calls = %d, want 1 (exact-match Enter should run the command)", eng.clearCalls)
	}
	if m.autocomplete.active {
		t.Error("overlay still open after submit")
	}
}

func TestAutocompleteOpensWhenTypingSlash(t *testing.T) {
	m := newTestModel(t)
	m = step(t, m, keyRune('/'))
	if !m.autocomplete.active {
		t.Fatalf("typing / did not open the overlay (input=%q)", m.input.Value())
	}
	if got := plain(m.View()); !strings.Contains(got, "/compact") {
		t.Errorf("overlay not rendered in the view:\n%s", got)
	}
}

// ----------------------------------------------------------------------------
// @file autocomplete + the bounded workspace walk
// ----------------------------------------------------------------------------

func TestWorkspaceFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"), "package main")
	mustWrite(t, filepath.Join(dir, "README.md"), "# readme")
	mustWrite(t, filepath.Join(dir, "internal", "loop.go"), "package internal")
	mustWrite(t, filepath.Join(dir, ".git", "config"), "[core]") // hidden dir, must be skipped
	mustWrite(t, filepath.Join(dir, ".env"), "SECRET=1")         // hidden file, must be skipped

	all := workspaceFiles(dir, "", 20)
	want := []string{"README.md", "internal/loop.go", "main.go"}
	if !reflect.DeepEqual(all, want) {
		t.Errorf("workspaceFiles(all) = %v, want %v (.git/.env excluded)", all, want)
	}
	if got := workspaceFiles(dir, "loop", 20); !reflect.DeepEqual(got, []string{"internal/loop.go"}) {
		t.Errorf("filtered walk = %v, want [internal/loop.go]", got)
	}
	if got := workspaceFiles(dir, "", 2); len(got) != 2 {
		t.Errorf("cap not honoured: got %d, want 2", len(got))
	}
	if got := workspaceFiles("", "", 20); got != nil {
		t.Errorf("empty root = %v, want nil", got)
	}
}

func TestComputeAutocompleteFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"), "package main")
	opts := testOpts
	opts.Workspace = dir
	m := newTestModelEng(t, &fakeEngine{}, opts)

	m.input.SetValue("look at @main")
	ac := m.computeAutocomplete()
	if !ac.active || ac.kind != acFile {
		t.Fatalf("overlay = {active:%v kind:%v}, want active file", ac.active, ac.kind)
	}
	if len(ac.items) != 1 || ac.items[0].value != "main.go" || ac.items[0].label != "@main.go" {
		t.Fatalf("file suggestions = %+v, want one main.go", ac.items)
	}
	// Accept splices the @path at the token boundary, preserving the prefix text.
	m.autocomplete = ac
	m = step(t, m, keyTab())
	if got := m.input.Value(); got != "look at @main.go " {
		t.Errorf("accepted %q, want %q", got, "look at @main.go ")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

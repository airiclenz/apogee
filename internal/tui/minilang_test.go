package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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
	m := newModel(context.Background(), eng, opts, nil)
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

// /clear starts a fresh session: it clears the engine's memory and wipes the scrollback down to
// only the re-seeded start-up box, with NO "context cleared" note — the reprinted box is the signal.
// It stays idle (no worker) and clears the input. Seeded conversation proves the wipe.
func TestClearResetsSessionView(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	seedConversation(&m)

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
	if n := len(m.transcript.entries); n != 1 {
		t.Fatalf("transcript has %d entries after /clear, want exactly 1 (only the re-seeded start-up box)", n)
	}
	if k := m.transcript.entries[0].kind; k != entryStartup {
		t.Errorf("entries[0].kind = %v after /clear, want entryStartup", k)
	}
	got := plain(m.View())
	if strings.Contains(got, "context cleared") {
		t.Errorf("/clear left a 'context cleared' note; the reprinted box is the signal:\n%s", got)
	}
	if strings.Contains(got, seededAssistantText) {
		t.Errorf("/clear left the prior conversation in the view:\n%s", got)
	}
}

// /new aliases /clear: it shares the startNewSession seam, so it clears the engine and resets the
// view down to the re-seeded start-up box exactly as /clear does.
func TestNewCommandAliasesClear(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	seedConversation(&m)

	m.input.SetValue("/new")
	m, cmd := stepCmd(t, m, keyEnter())

	if eng.clearCalls != 1 {
		t.Errorf("ClearContext calls = %d, want 1 (/new must forward to the same clear logic)", eng.clearCalls)
	}
	if m.state != stateIdle {
		t.Errorf("state = %v, want idle (/new must not launch a worker)", m.state)
	}
	if cmd != nil {
		t.Error("/new returned a Cmd; it should not launch a worker")
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("input not cleared: %q", v)
	}
	if n := len(m.transcript.entries); n != 1 {
		t.Fatalf("transcript has %d entries after /new, want exactly 1 (proves /new shares the reset seam)", n)
	}
	if k := m.transcript.entries[0].kind; k != entryStartup {
		t.Errorf("entries[0].kind = %v after /new, want entryStartup", k)
	}
}

// On a ClearContext error the view is NOT reset: the seeded conversation survives and the failure is
// noted, so a fresh-looking view can never lie about an engine that still remembers.
func TestClearCommandSurfacesEngineError(t *testing.T) {
	eng := &fakeEngine{clearFn: func() error { return domain.ErrInputPending }}
	m := newTestModelEng(t, eng, testOpts)
	seedConversation(&m)
	before := len(m.transcript.entries)

	m.input.SetValue("/clear")
	m = step(t, m, keyEnter())

	if got := plain(m.View()); !strings.Contains(got, "could not clear context") {
		t.Errorf("transcript missing the clear-failure note:\n%s", got)
	}
	// The error note is appended to the intact scrollback, so the count grows by exactly one.
	if got := len(m.transcript.entries); got != before+1 {
		t.Errorf("transcript entries = %d after a failed /clear, want %d (the seeded conversation survives, plus the error note)", got, before+1)
	}
	if got := plain(m.View()); !strings.Contains(got, seededAssistantText) {
		t.Errorf("a failed /clear wrongly wiped the prior conversation:\n%s", got)
	}
}

// seededAssistantText is the assistant reply seedConversation folds in, so the reset tests can assert
// on a distinctive string that must vanish (or survive) with the scrollback.
const seededAssistantText = "the number is 7"

// seedConversation folds a user message and an assistant reply into the model's transcript so a
// reset test starts with more than the lone start-up box. It takes *Model so the mutation lands on
// the caller's value.
func seedConversation(m *Model) {
	m.transcript.addUser("remember the number 7", nil)
	m.transcript.apply(domain.MessageEvent{Text: seededAssistantText})
}

const seededUserText = "remember the number 7"

// transcriptHasUser reports whether decoded entries hold a user message with the given text.
func transcriptHasUser(entries []entry, want string) bool {
	for _, e := range entries {
		if e.kind == entryUser && e.text == want {
			return true
		}
	}
	return false
}

// With a wired SessionHost, /clear closes the outgoing session into history: one final Save carrying
// the outgoing conversation, then a Rotate so the NEXT Turn opens a fresh session id rather than
// clobbering the one just closed (session-system plan §6). Both ride the record-write queue, so the
// test drains it exactly as the Update loop would.
func TestClearClosesSessionIntoHistoryAndRotates(t *testing.T) {
	host := &fakeSessionHost{}
	m := newSessionModel(t, &fakeEngine{}, host)
	seedConversation(&m)

	m.input.SetValue("/clear")
	m, cmd := stepCmd(t, m, keyEnter())
	m = runWrites(t, m, cmd)

	calls := host.savedCalls()
	if len(calls) != 1 {
		t.Fatalf("Save calls on /clear = %d; want 1 (the outgoing session's final flush)", len(calls))
	}
	entries, err := decodeTranscript(calls[0].transcript)
	if err != nil {
		t.Fatalf("decode flushed transcript: %v", err)
	}
	if !transcriptHasUser(entries, seededUserText) {
		t.Error("the flushed transcript did not carry the outgoing conversation")
	}
	if got := host.rotateCount(); got != 1 {
		t.Errorf("Rotate calls = %d; want 1 (the closed session must not be reused)", got)
	}
	closedID := calls[0].id

	// The next Turn's per-Turn save opens a FRESH session id, proving the rotate took effect.
	seedConversation(&m)
	m = driveOneSave(t, m, domain.Session{})
	calls = host.savedCalls()
	newID := calls[len(calls)-1].id
	if newID == "" || newID == closedID {
		t.Errorf("next Turn save id = %q; want a fresh id distinct from the closed %q", newID, closedID)
	}
}

// /clear with nothing beyond the start-up box saves nothing (no conversation worth resuming) but STILL
// rotates: Rotate is unconditional-on-success and idempotent on an inactive session, so a stale active
// id can never leak into the next conversation.
func TestClearWithoutConversationRotatesButDoesNotSave(t *testing.T) {
	host := &fakeSessionHost{}
	m := newSessionModel(t, &fakeEngine{}, host)

	m.input.SetValue("/clear")
	m, cmd := stepCmd(t, m, keyEnter())
	m = runWrites(t, m, cmd)

	if n := len(host.savedCalls()); n != 0 {
		t.Errorf("Save calls with only the start-up box = %d; want 0", n)
	}
	if got := host.rotateCount(); got != 1 {
		t.Errorf("Rotate calls = %d; want 1 (unconditional so no stale id leaks into the next session)", got)
	}
}

// A save already in flight when /clear lands must not outlive the Rotate that closes the session it
// belongs to. With the Rotate written straight through, that save reached an already-inactive host
// and minted a SECOND id for the outgoing conversation — a duplicate record the fresh session then
// went on updating as its own (audit 2026-08-01 follow-up). Queued behind the flush, it cannot:
// one conversation, one id. Asserted at the fold layer, because the recording host serialises its
// own calls and so cannot see the ordering the Model is responsible for.
func TestClearRotateWaitsForAnInFlightSave(t *testing.T) {
	host := &fakeSessionHost{}
	m := newSessionModel(t, &fakeEngine{}, host)
	seedConversation(&m)

	// A per-Turn save goes out and is left in flight: its Cmd is held rather than run, so the host
	// has not even minted the session's id yet when /clear arrives.
	m, saveCmd := stepCmd(t, m, turnSnapshotMsg{Sess: domain.Session{}})
	if saveCmd == nil {
		t.Fatal("the per-Turn snapshot scheduled no save")
	}

	m.input.SetValue("/clear")
	m, cmd := stepCmd(t, m, keyEnter())
	if cmd != nil {
		t.Fatal("/clear dispatched a record write beside the in-flight save")
	}
	if got := host.rotateCount(); got != 0 {
		t.Fatalf("Rotate calls while a save was in flight = %d; want 0 — that save would mint a second id", got)
	}

	// Drain the queue the way the Update loop does: the in-flight save lands, then the closing
	// flush, then the rotate.
	m, cmd = stepCmd(t, m, cmdMsg(saveCmd))
	m = runWrites(t, m, cmd)
	if got := host.rotateCount(); got != 1 {
		t.Fatalf("Rotate calls after the drain = %d; want 1", got)
	}

	ids := map[string]bool{}
	for _, c := range host.savedCalls() {
		ids[c.id] = true
	}
	if len(ids) != 1 {
		t.Errorf("the outgoing conversation was filed under %d ids (%v); want exactly one", len(ids), ids)
	}

	// …and the rotate still took effect: the next Turn's save opens a fresh id.
	seedConversation(&m)
	m = driveOneSave(t, m, domain.Session{})
	calls := host.savedCalls()
	if newID := calls[len(calls)-1].id; ids[newID] {
		t.Errorf("next Turn save id = %q; want one distinct from the closed session's %v", newID, ids)
	}
}

// A failed ClearContext leaves the session OPEN: the view is untouched and — critically — no Rotate
// fires, so the outgoing session's id stays live and later Turns keep updating its file. The final Save
// runs before ClearContext, so it did happen; it is harmless (the session was closing anyway).
func TestClearErrorDoesNotRotate(t *testing.T) {
	eng := &fakeEngine{clearFn: func() error { return domain.ErrInputPending }}
	host := &fakeSessionHost{}
	m := newSessionModel(t, eng, host)
	seedConversation(&m)
	before := len(m.transcript.entries)

	m.input.SetValue("/clear")
	m, cmd := stepCmd(t, m, keyEnter())
	m = runWrites(t, m, cmd)

	if got := host.rotateCount(); got != 0 {
		t.Errorf("Rotate calls after a failed clear = %d; want 0 (the session stays open)", got)
	}
	if n := len(host.savedCalls()); n != 1 {
		t.Errorf("Save calls on the error path = %d; want 1 (the harmless pre-ClearContext flush)", n)
	}
	if got := len(m.transcript.entries); got != before+1 {
		t.Errorf("transcript entries after a failed /clear = %d; want %d (conversation survives + one note)", got, before+1)
	}
	if !hasEntry(m, entryUser, seededUserText) {
		t.Error("a failed /clear wrongly wiped the outgoing conversation")
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
	m.input.SetValue(`review @main.go and @"docs/my plan.md" now`)
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
	if in.Text != `review @main.go and @"docs/my plan.md" now` {
		t.Errorf("submitted text = %q, want the literal message (quoted @token included)", in.Text)
	}
	// The text keeps the literal tokens; the refs carry clean, unquoted paths.
	if want := []string{"main.go", "docs/my plan.md"}; !reflect.DeepEqual(in.FileRefs, want) {
		t.Errorf("submitted FileRefs = %v, want %v", in.FileRefs, want)
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
// The sole-token typo guard
// ----------------------------------------------------------------------------
//
// The parse-layer rule is command_test.go's (TestParseInputSoleUnknownSlash); these pin what the
// two ⏎ paths DO with it. The valid counterpart — an input that is only a KNOWN skill token still
// sends — is skill_test.go's TestSubmitBareSkillTokenSends.

// ⏎ on an input that is nothing but one unrecognised "/word" refuses: the note names the token,
// the line stays in the box for a one-character fix, and nothing reaches the model. The silent send
// that made a mistyped (or not-yet-existing) verb look broken is exactly ISSUES #12's complaint.
func TestUnknownSoleSlashRefusedAtIdle(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, skillOpts())
	m.input.SetValue("/code-adit")
	m, cmd := stepCmd(t, m, keyEnter())

	if m.state != stateIdle || cmd != nil {
		t.Fatalf("a refused token drove something: state = %v, cmd != nil = %v", m.state, cmd != nil)
	}
	if got := eng.submits(); got != 0 {
		t.Errorf("Submit calls = %d, want 0 — the typo must never reach the model", got)
	}
	if got := m.input.Value(); got != "/code-adit" {
		t.Errorf("input = %q, want the line kept exactly as typed", got)
	}
	if n := countNotes(m, "unknown command or skill: /code-adit"); n != 1 {
		t.Errorf("refusal notes = %d, want exactly 1; notes = %q", n, noteTexts(m))
	}
}

// The same refusal while a worker runs: a mistyped invocation is no more queued for the model than
// it is sent to it, and the running Exchange is left entirely alone.
func TestUnknownSoleSlashRefusedWhileRunning(t *testing.T) {
	m := runningModel(t)
	m.input.SetValue("/code-adit")
	next, cmd := stepCmd(t, m, keyEnter())

	if cmd != nil {
		t.Error("a refused token returned a Cmd; nothing may be driven while a worker runs")
	}
	if len(next.pendingInterjections) != 0 {
		t.Errorf("staged rows = %+v, want none — a typo is not a message", next.pendingInterjections)
	}
	if got := next.input.Value(); got != "/code-adit" {
		t.Errorf("input = %q, want the line kept exactly as typed", got)
	}
	if n := countNotes(next, "unknown command or skill: /code-adit"); n != 1 {
		t.Errorf("refusal notes = %d, want exactly 1; notes = %q", n, noteTexts(next))
	}
}

// More than the one token is an ordinary message, whatever it opens with: the guard claims only
// the input a human can have meant as an invocation and nothing else.
func TestUnknownSlashWithMoreWordsStillSends(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, skillOpts())
	m.input.SetValue("/code-adit the parser")
	m, cmd := stepCmd(t, m, keyEnter())

	if m.state != stateRunning {
		t.Fatalf("state = %v, want running — a multi-token line is a message", m.state)
	}
	drainCmd(t, m, cmd)
	if len(eng.submitted) != 1 {
		t.Fatalf("Submit calls = %d, want 1", len(eng.submitted))
	}
	if got := eng.submitted[0].Text; got != "/code-adit the parser" {
		t.Errorf("submitted text = %q, want the line verbatim", got)
	}
}

// ----------------------------------------------------------------------------
// Autocomplete overlay
// ----------------------------------------------------------------------------

func TestComputeAutocompleteCommands(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/c") // clear, color-scheme, compact, confine, continue all start with "c"
	ac := m.computeAutocomplete(m.caretByteOffset())
	if !ac.active || ac.kind != acCommand {
		t.Fatalf("overlay = {active:%v kind:%v}, want active command", ac.active, ac.kind)
	}
	var got []string
	for _, it := range ac.items {
		got = append(got, it.value)
	}
	if !reflect.DeepEqual(got, []string{"clear", "color-scheme", "compact", "confine", "continue"}) {
		t.Errorf("suggestions = %v, want every c-command in menu (alphabetical) order", got)
	}
}

func TestComputeAutocompleteNarrowsAndExact(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/cl") // only "clear"
	ac := m.computeAutocomplete(m.caretByteOffset())
	if len(ac.items) != 1 || ac.items[0].value != "clear" {
		t.Fatalf("suggestions = %v, want [clear]", ac.items)
	}
	// A plain message is not a command, and yields no overlay.
	m.input.SetValue("hello there")
	if ac := m.computeAutocomplete(m.caretByteOffset()); ac.active {
		t.Error("plain text opened the overlay")
	}
}

func TestComputeAutocompleteOffersNewAlias(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/n") // only "new" begins with "n"
	ac := m.computeAutocomplete(m.caretByteOffset())

	if len(ac.items) != 1 || ac.items[0].value != "new" {
		t.Fatalf("suggestions = %v, want [new]", ac.items)
	}
}

func TestComputeAutocompleteOffersConfine(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/conf") // only "confine" begins with "conf"
	ac := m.computeAutocomplete(m.caretByteOffset())

	if len(ac.items) != 1 || ac.items[0].value != "confine" {
		t.Fatalf("suggestions = %v, want [confine]", ac.items)
	}
	if !containsString(ac.items[0].cells, "/confine") {
		t.Errorf("cells = %q, want the verb in its own column", ac.items[0].cells)
	}
}

func TestConfineArgumentErrorNeverTouchesTheEngine(t *testing.T) {
	// A mistyped /confine line is reported, never acted on: the one command that can widen
	// Auto's blast radius must not toggle anything the user did not spell correctly.
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("/confine sideways")
	m, cmd := stepCmd(t, m, keyEnter())

	if got := eng.confinesSet(); len(got) != 0 {
		t.Errorf("SetConfineToWorkspace calls = %v, want none on a parse error", got)
	}
	if m.state != stateIdle {
		t.Errorf("state = %v, want idle (/confine must not launch a worker)", m.state)
	}
	if cmd != nil {
		t.Error("/confine returned a Cmd; it should not launch a worker")
	}
	if got := eng.submits(); got != 0 {
		t.Errorf("Submit calls = %d, want 0 (a bad /confine is not forwarded to the agent)", got)
	}
}

// Accepting a command row is an ACTION, not a completion: the verb runs on the spot and its token
// is consumed. Here the box held nothing else, so it comes back empty.
func TestAutocompleteAcceptWithTab(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("/cl")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset()) // simulate the post-edit recompute
	m = step(t, m, keyTab())
	if eng.clearCalls != 1 {
		t.Errorf("ClearContext calls = %d, want 1 (accepting a command row runs it)", eng.clearCalls)
	}
	if got := m.input.Value(); got != "" {
		t.Errorf("after tab-accept input = %q, want the accepted token consumed", got)
	}
	if m.autocomplete.active {
		t.Error("overlay still open after accept")
	}
}

func TestAutocompleteNavigateThenAccept(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("/c")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset()) // [clear, color-scheme, compact, confine, continue], selected 0
	m = step(t, m, keyDown())                                   // → color-scheme
	if m.autocomplete.selected != 1 {
		t.Fatalf("after down selected = %d, want 1", m.autocomplete.selected)
	}
	m = step(t, m, keyUp())   // → clear
	m = step(t, m, keyDown()) // → color-scheme
	m = step(t, m, keyDown()) // → compact
	m, cmd := stepCmd(t, m, keyTab())
	if m.state != stateRunning || cmd == nil {
		t.Fatalf("accepting compact did not run it: state = %v, cmd != nil = %v", m.state, cmd != nil)
	}
	drainCmd(t, m, cmd)
	if eng.compactCalls != 1 {
		t.Errorf("Compact calls = %d, want 1", eng.compactCalls)
	}
}

func TestAutocompleteEscDismisses(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/c")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
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
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
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
// The merged "/" menu: token-scoped, and commands run at accept
// ----------------------------------------------------------------------------

// The "/" region is scoped to a TOKEN, not to the whole line: a draft that already holds text still
// opens the menu on its trailing "/word" — the second ISSUES #12 symptom ("slash commands don't
// work if I already typed something in the prompt editor").
func TestSlashMenuOpensAfterADraft(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("fix the parser /comp")
	ac := m.computeAutocomplete(m.caretByteOffset())

	if !ac.active || ac.kind != acCommand {
		t.Fatalf("overlay = {active:%v kind:%v}, want the command menu open mid-draft", ac.active, ac.kind)
	}
	if len(ac.items) != 1 || ac.items[0].value != "compact" {
		t.Fatalf("suggestions = %+v, want [compact]", ac.items)
	}
	if want := len("fix the parser "); ac.tokenStart != want {
		t.Errorf("tokenStart = %d, want %d (the token's start, not the line's)", ac.tokenStart, want)
	}
}

// Accepting a command row from a draft RUNS the command and hands the draft back minus the verb:
// invoking a command never destroys what was being written, and never sends it either.
func TestAcceptCommandRunsItAndKeepsTheDraft(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("fix the parser /comp")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
	m, cmd := stepCmd(t, m, keyTab())

	if m.state != stateRunning || cmd == nil {
		t.Fatalf("accepting /compact did not run it: state = %v, cmd != nil = %v", m.state, cmd != nil)
	}
	want := "fix the parser "
	if got := m.input.Value(); got != want {
		t.Errorf("editor = %q, want the draft minus the accepted token (%q)", got, want)
	}
	if off := caretOffset(m.input.Value(), m.input.Line(), m.input.Column()); off != len(want) {
		t.Errorf("caret offset = %d, want %d (the end of what remains)", off, len(want))
	}
	if m.autocomplete.active {
		t.Error("overlay still open after the command ran")
	}
	drainCmd(t, m, cmd)
	if len(eng.submitted) != 0 {
		t.Errorf("the draft was sent to the model: %+v", eng.submitted)
	}
}

// ⏎ on an exactly typed command token MID-DRAFT executes through the accept path — the command
// runs, the draft stays, nothing is sent. (The whole-input form keeps falling through to submit:
// TestAutocompleteEnterExactSubmits.)
func TestEnterOnMidDraftCommandRunsAndKeepsTheDraft(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("hold on /clear")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
	m, cmd := stepCmd(t, m, keyEnter())

	if eng.clearCalls != 1 {
		t.Fatalf("ClearContext calls = %d, want 1 (⏎ on an exact mid-draft verb runs it)", eng.clearCalls)
	}
	if cmd != nil {
		t.Error("/clear returned a Cmd; it should not launch a worker")
	}
	if got, want := m.input.Value(), "hold on "; got != want {
		t.Errorf("editor = %q, want the draft preserved (%q)", got, want)
	}
	if got := eng.submits(); got != 0 {
		t.Errorf("Submit calls = %d, want 0 — the draft must not be sent by a command", got)
	}
}

// The one arg-taking verb never fires from the menu: accepting /confine completes to "/confine "
// and waits, because arguments are the whole-input form's business ("/confine off --save").
func TestAcceptConfineSplicesWithoutFiring(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("/conf")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
	before := len(m.transcript.entries)
	m, cmd := stepCmd(t, m, keyTab())

	if got := m.input.Value(); got != "/confine " {
		t.Errorf("accepted %q, want %q", got, "/confine ")
	}
	if cmd != nil {
		t.Error("accepting /confine launched something")
	}
	if got := len(m.transcript.entries); got != before {
		t.Errorf("transcript grew by %d entries; accepting /confine must not report anything yet", got-before)
	}
	if got := eng.confinesSet(); len(got) != 0 {
		t.Errorf("SetConfineToWorkspace calls = %v, want none from a completion", got)
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

	// Exercised through the fileCache (the only surviving path — the standalone workspaceFiles
	// helper was removed as dead code; newModel always installs the cache).
	now := time.Now()
	all := (&fileCache{}).suggest(dir, "", 20, now)
	want := []string{"README.md", "internal/loop.go", "main.go"}
	if !reflect.DeepEqual(all, want) {
		t.Errorf("workspace listing = %v, want %v (.git/.env excluded)", all, want)
	}
	if got := (&fileCache{}).suggest(dir, "loop", 20, now); !reflect.DeepEqual(got, []string{"internal/loop.go"}) {
		t.Errorf("filtered walk = %v, want [internal/loop.go]", got)
	}
	if got := (&fileCache{}).suggest(dir, "", 2, now); len(got) != 2 {
		t.Errorf("cap not honoured: got %d, want 2", len(got))
	}
	if got := (&fileCache{}).suggest("", "", 20, now); got != nil {
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
	ac := m.computeAutocomplete(m.caretByteOffset())
	if !ac.active || ac.kind != acFile {
		t.Fatalf("overlay = {active:%v kind:%v}, want active file", ac.active, ac.kind)
	}
	if len(ac.items) != 1 || ac.items[0].value != "main.go" ||
		!reflect.DeepEqual(ac.items[0].cells, popupRow{"@main.go"}) {
		t.Fatalf("file suggestions = %+v, want one single-cell main.go row", ac.items)
	}
	// Accept splices the @path at the token boundary, preserving the prefix text.
	m.autocomplete = ac
	m = step(t, m, keyTab())
	if got := m.input.Value(); got != "look at @main.go " {
		t.Errorf("accepted %q, want %q", got, "look at @main.go ")
	}
}

// ----------------------------------------------------------------------------
// Quoted @file refs — the overlay's half of the grammar
// ----------------------------------------------------------------------------

// The detector reads both shapes of the ref token: bare rows must behave exactly as they always
// did, quoted rows keep the overlay alive across the spaces the bare rule tokenizes on. Every row
// puts the caret at the end of the value — the forward-typing case, unchanged by caret-awareness.
func TestCaretFileTokenAtTheEnd(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		start   int
		partial string
		ok      bool
	}{
		{"no token", "plain text", 0, "", false},
		{"at start", "@main", 0, "main", true},
		{"after text", "look at @main", 8, "main", true},
		{"bare @ alone", "look at @", 8, "", true},
		{"trailing space closes it", "look at @main.go ", 0, "", false},
		{"email is not a token", "mail foo@bar.com", 0, "", false},
		{"open quote spans the space", `see @"my pl`, 4, "my pl", true},
		{"open single quote", "see @'my pl", 4, "my pl", true},
		{"open quote right-trimmed", `@"my pl `, 0, "my pl", true},
		{"closing quote flush at the end", `@"a b.md"`, 0, "a b.md", true},
		{"space after the closing quote closes it", `@"a b.md" `, 0, "", false},
		{"prose after the closing quote", `@"a b.md" and more`, 0, "", false},
		{"bare token after a closed quote", `@"a b.md" @ma`, 10, "ma", true},
		{"quoted email is not a token", `mail foo@"bar baz`, 0, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, end, partial, ok := caretFileToken(c.value, len(c.value))
			if ok != c.ok || partial != c.partial || (ok && start != c.start) {
				t.Errorf("caretFileToken(%q) = (%d, %q, %v), want (%d, %q, %v)",
					c.value, start, partial, ok, c.start, c.partial, c.ok)
			}
			if ok && end != len(c.value) {
				t.Errorf("caretFileToken(%q) end = %d, want %d (the token reaches the caret)", c.value, end, len(c.value))
			}
		})
	}
}

// Typing an open quote keeps the dropdown listing across spaces, the row shows the quoted token
// it will insert, and accepting splices exactly that (plus the trailing space that closes it).
func TestComputeAutocompleteQuotedFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "my plan.md"), "# plan")
	mustWrite(t, filepath.Join(dir, "main.go"), "package main")
	opts := testOpts
	opts.Workspace = dir
	m := newTestModelEng(t, &fakeEngine{}, opts)

	m.input.SetValue(`look at @"my pl`)
	ac := m.computeAutocomplete(m.caretByteOffset())
	if !ac.active || ac.kind != acFile {
		t.Fatalf("overlay = {active:%v kind:%v}, want active file", ac.active, ac.kind)
	}
	if len(ac.items) != 1 || ac.items[0].value != "my plan.md" ||
		!reflect.DeepEqual(ac.items[0].cells, popupRow{`@"my plan.md"`}) {
		t.Fatalf("file suggestions = %+v, want one quoted my plan.md row", ac.items)
	}
	m.autocomplete = ac
	m = step(t, m, keyTab())
	if got, want := m.input.Value(), `look at @"my plan.md" `; got != want {
		t.Errorf("accepted %q, want %q", got, want)
	}
	if m.autocomplete.active {
		t.Error("overlay still open after accepting a completed ref")
	}
}

// Quoting is decided by the PATH, not by how the user started typing: a bare partial completing
// to a spaced path still splices the quoted form, because only that form resolves.
func TestAcceptAutocompleteQuotesSpacedPath(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "my plan.md"), "# plan")
	opts := testOpts
	opts.Workspace = dir
	m := newTestModelEng(t, &fakeEngine{}, opts)

	m.input.SetValue("look at @my")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
	m = step(t, m, keyTab())
	if got, want := m.input.Value(), `look at @"my plan.md" `; got != want {
		t.Errorf("accepted %q, want %q", got, want)
	}
}

// A fully typed quoted token counts as an exact match in either dialect, so ⏎ submits instead of
// re-completing — and the ref reaching the engine is the clean, unquoted path.
func TestAutocompleteQuotedFileEnterExactSubmits(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "my plan.md"), "# plan")
	opts := testOpts
	opts.Workspace = dir
	eng := &fakeEngine{stepFn: scriptedSteps()} // immediately ExchangeComplete when driven
	m := newTestModelEng(t, eng, opts)

	m.input.SetValue("look at @'my plan.md'")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
	if !m.autocompleteExactMatch() {
		t.Errorf("single-quoted token not an exact match (items=%+v)", m.autocomplete.items)
	}

	m.input.SetValue(`look at @"my plan.md"`)
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
	if !m.autocompleteExactMatch() {
		t.Fatalf("double-quoted token not an exact match (items=%+v)", m.autocomplete.items)
	}
	m, cmd := stepCmd(t, m, keyEnter())
	if m.state != stateRunning {
		t.Fatalf("state = %v, want running (exact-match Enter submits, not re-completes)", m.state)
	}
	drainCmd(t, m, cmd)
	if len(eng.submitted) != 1 {
		t.Fatalf("Submit calls = %d, want 1", len(eng.submitted))
	}
	if want := []string{"my plan.md"}; !reflect.DeepEqual(eng.submitted[0].FileRefs, want) {
		t.Errorf("submitted FileRefs = %v, want %v", eng.submitted[0].FileRefs, want)
	}
}

// ----------------------------------------------------------------------------
// Caret-aware completion: the token AT the caret, mid-buffer included
// ----------------------------------------------------------------------------

// caretAt puts the prompt caret at a byte offset into the current value and re-derives the overlay
// from there — exactly what the Update loop does after every edit (recomputeAutocomplete), so a
// test can reach a mid-buffer caret without spelling out the arrow keys that walked it there.
func caretAt(t *testing.T, m Model, off int) Model {
	t.Helper()
	m.caretToOffset(off)
	return m.recomputeAutocomplete()
}

// caretToken is the whole caret-awareness rule: the word the caret stands in or immediately after,
// empty when it stands in whitespace.
func TestCaretToken(t *testing.T) {
	const value = "fix /rev the parser"
	cases := []struct {
		name  string
		caret int
		want  string
	}{
		{"inside the first word", 1, "fix"},
		{"immediately after a word", 3, "fix"},
		{"at a word's first byte", 4, "/rev"},
		{"inside a token", 6, "/rev"},
		{"immediately after a token", 8, "/rev"},
		{"at the next word's first byte", 9, "the"},
		{"at the very end", len(value), "parser"},
		{"past the end clamps", len(value) + 5, "parser"},
		{"before the start clamps", -3, "fix"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, end := caretToken(value, c.caret)
			if got := value[start:end]; got != c.want {
				t.Errorf("caretToken(%q, %d) = %q, want %q", value, c.caret, got, c.want)
			}
		})
	}
	// A caret INSIDE a run of whitespace stands in no word at all: the range comes back empty, and
	// every region declines it.
	if start, end := caretToken("fix  it", 4); start != end {
		t.Errorf("caretToken on whitespace = [%d,%d), want an empty range", start, end)
	}
}

// The merged menu follows the CARET, not the end of the buffer: going back to a half-typed token
// with prose already written after it offers exactly what the end of the draft would.
func TestCompletionFollowsTheCaretMidBuffer(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("fix /rev the parser please")
	m = caretAt(t, m, len("fix /rev"))

	ac := m.autocomplete
	if !ac.active || ac.kind != acCommand {
		t.Fatalf("overlay = {active:%v kind:%v}, want the merged menu open on the caret's token", ac.active, ac.kind)
	}
	if len(ac.items) != 1 || ac.items[0].value != "review" || !ac.items[0].skill {
		t.Fatalf("rows = %+v, want the single review skill row", ac.items)
	}
	if got := m.input.Value()[ac.tokenStart:ac.tokenEnd]; got != "/rev" {
		t.Errorf("region = %q, want the caret's token %q — not the rest of the line", got, "/rev")
	}
}

// Accepting mid-buffer splices over the token's own range: what was written on BOTH sides survives,
// the caret lands just after what was written, and no separator is doubled.
func TestAcceptSplicesInPlaceAndReSeatsTheCaret(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("fix /rev the parser please")
	m = caretAt(t, m, len("fix /rev"))
	m = step(t, m, keyTab())

	const want = "fix /review the parser please"
	if got := m.input.Value(); got != want {
		t.Fatalf("after accept input = %q, want %q (the prose after the token untouched)", got, want)
	}
	if off, wantOff := m.caretByteOffset(), len("fix /review"); off != wantOff {
		t.Errorf("caret offset = %d, want %d (just after the spliced token)", off, wantOff)
	}
	if got := m.promptEditor.submitParse(m.knownSkillID).skillIDs; !reflect.DeepEqual(got, []string{"review"}) {
		t.Errorf("the spliced token parses to %v, want [review]", got)
	}
}

// The "@" region is caret-aware in both of its shapes — the bare word and the quoted path whose
// spaces the bare rule would tokenize on.
func TestFileCompletionAtACaretMidBuffer(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"), "package main")
	mustWrite(t, filepath.Join(dir, "my plan.md"), "# plan")
	opts := testOpts
	opts.Workspace = dir
	m := newTestModelEng(t, &fakeEngine{}, opts)

	m.input.SetValue("read @mai then stop")
	m = caretAt(t, m, len("read @mai"))
	if ac := m.autocomplete; !ac.active || ac.kind != acFile || len(ac.items) != 1 || ac.items[0].value != "main.go" {
		t.Fatalf("bare @ mid-buffer overlay = %+v", ac)
	}
	m = step(t, m, keyTab())
	if got, want := m.input.Value(), "read @main.go then stop"; got != want {
		t.Errorf("bare accept = %q, want %q", got, want)
	}

	m.input.SetValue(`read @"my pl" then stop`)
	m = caretAt(t, m, len(`read @"my pl`)) // inside the quotes, with the closing quote already typed
	if ac := m.autocomplete; !ac.active || ac.kind != acFile || len(ac.items) != 1 || ac.items[0].value != "my plan.md" {
		t.Fatalf("quoted @ mid-buffer overlay = %+v", ac)
	}
	m = step(t, m, keyTab())
	if got, want := m.input.Value(), `read @"my plan.md" then stop`; got != want {
		t.Errorf("quoted accept = %q, want %q", got, want)
	}
}

// A caret elsewhere on the line must not resurrect a menu for a token it has left.
func TestNoMenuWhenTheCaretLeavesTheToken(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("/rev the parser")

	if m = caretAt(t, m, len("/rev")); !m.autocomplete.active {
		t.Fatal("the menu is shut with the caret at the end of its own token")
	}
	for _, off := range []int{len("/rev "), len("/rev the")} {
		if m = caretAt(t, m, off); m.autocomplete.active {
			t.Errorf("caret at %d (outside the token) still shows a menu: %+v", off, m.autocomplete.items)
		}
	}
}

// A command invoked from the MIDDLE of a draft runs and leaves both sides of it standing — with the
// two separators the cut would strand collapsed back into the one word-space the human typed.
func TestCommandRunsFromTheMiddleOfADraft(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("fix the parser /clear and ship it")
	m = caretAt(t, m, len("fix the parser /clear"))
	m, cmd := stepCmd(t, m, keyEnter()) // an exact token mid-draft executes through the accept path

	if eng.clearCalls != 1 {
		t.Fatalf("ClearContext calls = %d, want 1 (⏎ on an exact mid-draft verb runs it)", eng.clearCalls)
	}
	if cmd != nil {
		t.Error("/clear returned a Cmd; it should not launch a worker")
	}
	const want = "fix the parser and ship it"
	if got := m.input.Value(); got != want {
		t.Errorf("editor = %q, want the draft with the verb cut out (%q)", got, want)
	}
	if off, wantOff := m.caretByteOffset(), len("fix the parser "); off != wantOff {
		t.Errorf("caret offset = %d, want %d (where the cut token stood)", off, wantOff)
	}
	if got := eng.submits(); got != 0 {
		t.Errorf("Submit calls = %d, want 0 — the draft must not be sent by a command", got)
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

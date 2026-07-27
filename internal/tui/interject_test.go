package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/skills"
)

// ----------------------------------------------------------------------------
// The interjection mailbox and its between-Steps drain (ADR 0025)
// ----------------------------------------------------------------------------

// recorder captures the ordering the delivery contract is about: which Steps ran, and where the
// delivery report landed between them. The worker drives synchronously on the calling goroutine
// in these tests, so a plain slice is enough.
type recorder struct {
	order []string
	msgs  []tea.Msg
}

// notify is the seam the worker sends its per-Turn and delivery Msgs through.
func (r *recorder) notify(msg tea.Msg) {
	r.msgs = append(r.msgs, msg)
	if _, ok := msg.(interjectedMsg); ok {
		r.order = append(r.order, "interjected")
	}
}

// delivered returns the rows carried by the single interjectedMsg the worker sent, and whether it
// sent one at all — the distinction between "delivered nothing" and "reported nothing".
func (r *recorder) delivered(t *testing.T) ([]queuedInterjection, bool) {
	t.Helper()
	var got []queuedInterjection
	var seen bool
	for _, msg := range r.msgs {
		if im, ok := msg.(interjectedMsg); ok {
			if seen {
				t.Fatalf("interjectedMsg sent more than once; want one per drained boundary")
			}
			got, seen = im.items, true
		}
	}
	return got, seen
}

// staged builds a queued row the way the staging half will: the raw editor text kept verbatim
// beside the parsed input the engine consumes.
func staged(id int, text string) queuedInterjection {
	return queuedInterjection{id: id, raw: text, input: domain.UserInput{Text: text}}
}

// TestWorkerDrainsBoxBetweenSteps is the delivery contract: rows staged while a Turn runs are
// committed at the NEXT between-Steps boundary — after the Turn that was in flight, before the
// Step that carries them upstream — in the order they were typed, and reported once.
func TestWorkerDrainsBoxBetweenSteps(t *testing.T) {
	t.Parallel()
	box := newInterjectBox()
	rec := &recorder{}
	eng := &fakeEngine{}
	eng.stepFn = func(_ context.Context, call int) (domain.StepResult, error) {
		rec.order = append(rec.order, "step")
		if call == 0 {
			// The human types two remarks while the first Turn runs.
			box.push(staged(1, "also check the tests"))
			box.push(staged(2, "and the docs"))
			return domain.StepResult{Status: domain.StatusTurnComplete}, nil
		}
		return domain.StepResult{Status: domain.StatusExchangeComplete}, nil
	}

	msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "go"}, box, rec.notify)

	if _, ok := msg.(exchangeDoneMsg); !ok {
		t.Fatalf("terminal msg = %T; want exchangeDoneMsg", msg)
	}
	texts := make([]string, 0, 2)
	for _, in := range eng.interjections() {
		texts = append(texts, in.Text)
	}
	if len(texts) != 2 || texts[0] != "also check the tests" || texts[1] != "and the docs" {
		t.Fatalf("delivered texts = %q; want both remarks, oldest first", texts)
	}
	// The report rides between the Turn that was running and the Step that carries the remarks:
	// the Update loop moves the rows into the transcript at the moment the model saw them.
	want := []string{"step", "interjected", "step"}
	if len(rec.order) != len(want) {
		t.Fatalf("order = %v; want %v", rec.order, want)
	}
	for i := range want {
		if rec.order[i] != want[i] {
			t.Fatalf("order = %v; want %v", rec.order, want)
		}
	}
	got, seen := rec.delivered(t)
	if !seen {
		t.Fatal("no interjectedMsg sent; the delivered rows must be reported")
	}
	if len(got) != 2 || got[0].id != 1 || got[1].id != 2 {
		t.Errorf("reported rows = %+v; want ids 1,2 in delivery order", got)
	}
	if got[0].raw != "also check the tests" {
		t.Errorf("reported raw = %q; want the verbatim editor text", got[0].raw)
	}
}

// TestWorkerEmptyBoxDeliversNothing pins the quiet path: with nothing staged the worker neither
// touches the engine's Interject nor reports a delivery — an Exchange nobody interjected into is
// byte-for-byte the pre-interjection drive. A nil box (the /compact worker, which drives no
// Exchange) behaves identically rather than panicking.
func TestWorkerEmptyBoxDeliversNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		box  *interjectBox
	}{
		{name: "empty box", box: newInterjectBox()},
		{name: "no box", box: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{}
			eng := &fakeEngine{
				stepFn: scriptedSteps(
					stepResult{res: domain.StepResult{Status: domain.StatusTurnComplete}},
					stepResult{res: domain.StepResult{Status: domain.StatusExchangeComplete}},
				),
			}

			msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "go"}, tc.box, rec.notify)

			if _, ok := msg.(exchangeDoneMsg); !ok {
				t.Fatalf("terminal msg = %T; want exchangeDoneMsg", msg)
			}
			if got := eng.interjections(); len(got) != 0 {
				t.Errorf("Interject calls = %d; want none", len(got))
			}
			if _, seen := rec.delivered(t); seen {
				t.Error("interjectedMsg sent for an empty drain; want no report at all")
			}
		})
	}
}

// TestWorkerInterjectErrorHoldsRemainder proves the honest degradation: a refused delivery stops
// the drain where it failed and the report names only what actually landed — nothing. The rows are
// not lost, because the report is what the Model reconciles against; a row missing from it stays on
// the display queue and goes out at the terminal flush.
func TestWorkerInterjectErrorHoldsRemainder(t *testing.T) {
	t.Parallel()
	box := newInterjectBox()
	rec := &recorder{}
	refused := errors.New("no open exchange")
	eng := &fakeEngine{interjectFn: func(domain.UserInput) error { return refused }}
	eng.stepFn = func(_ context.Context, call int) (domain.StepResult, error) {
		if call == 0 {
			box.push(staged(1, "first"))
			box.push(staged(2, "second"))
			return domain.StepResult{Status: domain.StatusTurnComplete}, nil
		}
		return domain.StepResult{Status: domain.StatusExchangeComplete}, nil
	}

	msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "go"}, box, rec.notify)

	if _, ok := msg.(exchangeDoneMsg); !ok {
		t.Fatalf("terminal msg = %T; want exchangeDoneMsg (a refused interjection never fails the Exchange)", msg)
	}
	attempts := eng.interjections()
	if len(attempts) != 1 || attempts[0].Text != "first" {
		t.Fatalf("Interject attempts = %+v; want the drain to stop at the first refusal", attempts)
	}
	got, seen := rec.delivered(t)
	if !seen {
		t.Fatal("no interjectedMsg sent; a drain that delivered nothing must still report, so the rows stay held")
	}
	if len(got) != 0 {
		t.Errorf("reported rows = %+v; want none — nothing was committed", got)
	}
}

// TestDrainWaitsForTheExchangeToOpen pins FIFO order across the Submit window. Submit only QUEUES
// the input — the Exchange opens inside the first Step — so a row staged between the ⏎ that
// launched the worker and that Step has nothing to land in yet. Draining it there would refuse it
// (ErrNoOpenExchange) after it had already left the mailbox, so a row staged later would reach the
// model FIRST. The engine here models the real Agent: Interject refuses until a Step has opened the
// Exchange.
func TestDrainWaitsForTheExchangeToOpen(t *testing.T) {
	t.Parallel()
	box := newInterjectBox()
	rec := &recorder{}
	eng := &fakeEngine{}
	var open bool // the Exchange the first Step opens (Agent.step → openExchange)
	var refusals int
	eng.interjectFn = func(domain.UserInput) error {
		if !open {
			refusals++
			return domain.ErrNoOpenExchange
		}
		return nil
	}
	eng.stepFn = func(_ context.Context, call int) (domain.StepResult, error) {
		open = true
		rec.order = append(rec.order, "step")
		if call == 0 {
			box.push(staged(2, "and the docs")) // typed while the first Turn runs
			return domain.StepResult{Status: domain.StatusTurnComplete}, nil
		}
		return domain.StepResult{Status: domain.StatusExchangeComplete}, nil
	}
	// The ⏎ that stages this one lands in the window between submit() and the worker's first Step.
	box.push(staged(1, "also check the tests"))

	msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "go"}, box, rec.notify)

	if _, ok := msg.(exchangeDoneMsg); !ok {
		t.Fatalf("terminal msg = %T; want exchangeDoneMsg", msg)
	}
	if refusals != 0 {
		t.Errorf("Interject refusals = %d; want none — the worker must not drain before the Exchange opens", refusals)
	}
	want := []string{"step", "interjected", "step"}
	if !reflect.DeepEqual(rec.order, want) {
		t.Fatalf("order = %v; want %v (nothing is delivered before the first Step)", rec.order, want)
	}
	texts := make([]string, 0, 2)
	for _, in := range eng.interjections() {
		texts = append(texts, in.Text)
	}
	if !reflect.DeepEqual(texts, []string{"also check the tests", "and the docs"}) {
		t.Fatalf("delivered texts = %q; want both remarks in the order they were typed", texts)
	}
	got, seen := rec.delivered(t)
	if !seen || len(got) != 2 || got[0].id != 1 || got[1].id != 2 {
		t.Errorf("reported rows = %+v (seen=%t); want ids 1,2 in one report", got, seen)
	}
}

// TestResumeDrainsBeforeItsFirstStep is the other half of that window. A resumed Exchange is
// already open at entry — the model launches driveResume only when eng.InExchange() — so a row
// staged before the first Step has somewhere to land, and holding it back would defer the human's
// remark by a whole Turn for no reason. The two drive paths therefore differ at exactly one flag.
func TestResumeDrainsBeforeItsFirstStep(t *testing.T) {
	t.Parallel()
	box := newInterjectBox()
	box.push(staged(1, "while you were away"))
	rec := &recorder{}
	eng := &fakeEngine{inExchange: true}
	eng.stepFn = func(_ context.Context, _ int) (domain.StepResult, error) {
		rec.order = append(rec.order, "step")
		return domain.StepResult{Status: domain.StatusExchangeComplete}, nil
	}

	msg := driveResume(context.Background(), eng, box, rec.notify)

	if _, ok := msg.(exchangeDoneMsg); !ok {
		t.Fatalf("terminal msg = %T; want exchangeDoneMsg", msg)
	}
	want := []string{"interjected", "step"}
	if !reflect.DeepEqual(rec.order, want) {
		t.Fatalf("order = %v; want %v (the resumed Exchange is open before the first Step)", rec.order, want)
	}
	if got := eng.interjections(); len(got) != 1 || got[0].Text != "while you were away" {
		t.Fatalf("delivered = %+v; want the single staged row", got)
	}
	if got, seen := rec.delivered(t); !seen || len(got) != 1 || got[0].id != 1 {
		t.Errorf("reported rows = %+v (seen=%t); want the row that landed", got, seen)
	}
}

// ----------------------------------------------------------------------------
// Typing while the model works — routing, staging, the delivery fold (ADR 0025)
// ----------------------------------------------------------------------------

// runningModel drives a fresh model through a real submit, so it is left in exactly the state the
// human types into: stateRunning, with this Exchange's mailbox live. The worker Cmd is discarded —
// these tests drive the folds directly, never the engine.
func runningModel(t *testing.T) Model {
	t.Helper()
	m := newTestModelEng(t, &fakeEngine{}, testOpts)
	m.input.SetValue("open the exchange")
	m, _ = stepCmd(t, m, keyEnter())
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}
	if m.box == nil {
		t.Fatal("precondition: no mailbox for the running Exchange")
	}
	return m
}

// stageRow types text and presses ⏎ at whatever state the model is in.
func stageRow(t *testing.T, m Model, text string) Model {
	t.Helper()
	m.input.SetValue(text)
	return step(t, m, keyEnter())
}

// TestTypingWhileRunningEditsInput is the routing change itself: printable keys reach the textarea
// while a worker runs, instead of scrolling the transcript past a refused box.
func TestTypingWhileRunningEditsInput(t *testing.T) {
	m := runningModel(t)
	for _, r := range "hi" {
		m = step(t, m, keyRune(r))
	}
	if got := m.input.Value(); got != "hi" {
		t.Fatalf("input = %q; want the typed text — keys must edit the box while running", got)
	}
}

// TestEnterWhileRunningStagesRow replaces TestModelSubmitWhileRunningIsNoOp: ⏎ while running is no
// longer a no-op, but it still launches nothing. The message becomes a staged row — on the display
// queue AND in the Exchange's mailbox — and the editor is cleared for the next one.
func TestEnterWhileRunningStagesRow(t *testing.T) {
	m := runningModel(t)
	box := m.box

	m.input.SetValue("also check the tests")
	next, cmd := stepCmd(t, m, keyEnter())

	if next.state != stateRunning {
		t.Errorf("state = %v; want still running (staging launches no worker)", next.state)
	}
	if cmd != nil {
		t.Error("a second worker Cmd was launched while one was running")
	}
	if got := next.input.Value(); got != "" {
		t.Errorf("input = %q; want an emptied editor after staging", got)
	}
	if n := len(next.pendingInterjections); n != 1 {
		t.Fatalf("staged rows = %d; want 1", n)
	}
	row := next.pendingInterjections[0]
	if row.raw != "also check the tests" || row.input.Text != "also check the tests" {
		t.Errorf("staged row = %+v; want the verbatim text in both halves", row)
	}
	if row.id == 0 {
		t.Error("staged row carries no id; the delivery report reconciles by id")
	}
	// The mailbox got the same row, which is what lets the worker deliver it at its next boundary.
	staged := box.drainAll()
	if len(staged) != 1 || staged[0].id != row.id {
		t.Fatalf("mailbox = %+v; want exactly the staged row", staged)
	}
	if got := plain(next.View()); !strings.Contains(got, "also check the tests") {
		t.Errorf("the staged row is not shown above the input box:\n%s", got)
	}
}

// An @file reference in an interjection reaches the engine as a ref, exactly as it does in a
// submitted message — the refs resolve at delivery, so a mid-run "@main.go" is as useful as one
// typed at idle.
func TestStagedRowCarriesFileRefs(t *testing.T) {
	m := stageRow(t, runningModel(t), "look at @main.go too")
	if n := len(m.pendingInterjections); n != 1 {
		t.Fatalf("staged rows = %d; want 1", n)
	}
	in := m.pendingInterjections[0].input
	if !reflect.DeepEqual(in.FileRefs, []string{"main.go"}) {
		t.Errorf("FileRefs = %v; want the extracted ref", in.FileRefs)
	}
}

// TestCommandWhileRunningRefusedWithNote pins the one thing that does NOT queue: a /command is
// idle-only, so it earns a note and keeps the human's line in the box for the moment the Exchange
// ends.
func TestCommandWhileRunningRefusedWithNote(t *testing.T) {
	m := runningModel(t)
	m.input.SetValue("/clear")
	next, cmd := stepCmd(t, m, keyEnter())

	if cmd != nil {
		t.Error("a refused command returned a Cmd; nothing may be driven while a worker runs")
	}
	if got := next.input.Value(); got != "/clear" {
		t.Errorf("input = %q; want the command preserved for the human to re-send at idle", got)
	}
	if len(next.pendingInterjections) != 0 {
		t.Errorf("staged rows = %+v; want none — commands are never queued", next.pendingInterjections)
	}
	if got := plain(next.View()); !strings.Contains(got, commandsAtIdleNote) {
		t.Errorf("the refusal note is missing from the transcript:\n%s", got)
	}
}

// TestBackspaceEmptyPopsNewestIntoEditor: Backspace on an empty box takes the NEWEST row back,
// verbatim and editable, leaving the older one queued — and withdraws it from the mailbox, so a
// row taken back can never still be delivered.
func TestBackspaceEmptyPopsNewestIntoEditor(t *testing.T) {
	m := runningModel(t)
	box := m.box
	m = stageRow(t, m, "first remark")
	m = stageRow(t, m, "second remark")

	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})

	if got := m.input.Value(); got != "second remark" {
		t.Fatalf("editor = %q; want the newest row restored verbatim", got)
	}
	if n := len(m.pendingInterjections); n != 1 || m.pendingInterjections[0].raw != "first remark" {
		t.Fatalf("staged rows = %+v; want only the older one left", m.pendingInterjections)
	}
	staged := box.drainAll()
	if len(staged) != 1 || staged[0].raw != "first remark" {
		t.Fatalf("mailbox = %+v; want the popped row withdrawn from it", staged)
	}
}

// A row already drained by the worker is NOT popped back: its delivery is out of the Model's
// hands, and handing the human an editor copy would invite sending the same message twice.
func TestBackspaceDoesNotPopADrainedRow(t *testing.T) {
	m := runningModel(t)
	m = stageRow(t, m, "in flight")
	m.box.drainAll() // the worker took it at a between-Steps boundary

	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})

	if got := m.input.Value(); got != "" {
		t.Errorf("editor = %q; want it left empty — the row is the worker's now", got)
	}
	if n := len(m.pendingInterjections); n != 1 {
		t.Errorf("staged rows = %d; want the row still queued until its delivery report lands", n)
	}
}

// The queue pop takes precedence over the skill-chip pop, and the chips are still reachable once
// the queue is empty — the two rarely coexist, and "newest first" decides when they do.
func TestBackspacePopsQueueBeforeSkillChips(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, testOpts)
	m.pendingSkills = []string{"review"}
	m.pendingInterjections = []queuedInterjection{staged(1, "held row")}

	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.input.Value(); got != "held row" {
		t.Fatalf("editor = %q; want the queued row popped before the chip", got)
	}
	if len(m.pendingSkills) != 1 {
		t.Fatalf("skills = %v; want the chip untouched while a row was still queued", m.pendingSkills)
	}

	m.input.SetValue("")
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(m.pendingSkills) != 0 {
		t.Errorf("skills = %v; want the chip popped once the queue is empty", m.pendingSkills)
	}
}

// TestInterjectedMsgMovesRowToTranscript is the delivery fold: the reported row leaves the queue
// and lands in the scrollback as its own block, while the sticky header keeps naming the prompt
// that OPENED the Exchange — an interjection is not a new section.
func TestInterjectedMsgMovesRowToTranscript(t *testing.T) {
	m := runningModel(t)
	m = stageRow(t, m, "also check the tests")
	m = stageRow(t, m, "and the docs")
	before := len(m.userBlocks)
	rows := m.pendingInterjections

	m = step(t, m, interjectedMsg{items: rows[:1]})

	if n := len(m.pendingInterjections); n != 1 || m.pendingInterjections[0].raw != "and the docs" {
		t.Fatalf("staged rows = %+v; want only the undelivered one left", m.pendingInterjections)
	}
	last := m.transcript.entries[len(m.transcript.entries)-1]
	if last.kind != entryInterjected || last.text != "also check the tests" {
		t.Fatalf("tail entry = %+v; want the delivered remark as an interjected block", last)
	}
	if len(m.userBlocks) != before {
		t.Errorf("userBlocks = %d, was %d; an interjection must not become the sticky header",
			len(m.userBlocks), before)
	}
	view := plain(m.View())
	if !strings.Contains(view, glyphInterject+" also check the tests") {
		t.Errorf("the delivered remark is not rendered with its ⧖ marker:\n%s", view)
	}
}

// A report naming nothing (a drain whose deliveries were all refused) changes nothing: the rows
// stay queued for the terminal flush.
func TestEmptyInterjectedMsgKeepsRowsQueued(t *testing.T) {
	m := stageRow(t, runningModel(t), "held")
	m = step(t, m, interjectedMsg{})
	if n := len(m.pendingInterjections); n != 1 {
		t.Fatalf("staged rows = %d; want the row still queued after an empty report", n)
	}
	for _, e := range m.transcript.entries {
		if e.kind == entryInterjected {
			t.Fatal("an empty report wrote an interjected block; nothing was committed")
		}
	}
}

// TestStatusLineShowsQueuedCount: the status line says how much is waiting, and says nothing once
// the queue drains.
func TestStatusLineShowsQueuedCount(t *testing.T) {
	m := runningModel(t)
	if got := plain(m.View()); strings.Contains(got, "queued") {
		t.Errorf("an empty queue still says 'queued':\n%s", got)
	}
	m = stageRow(t, m, "one")
	m = stageRow(t, m, "two")
	if got := plain(m.View()); !strings.Contains(got, "2 queued") {
		t.Errorf("status line missing the queued count:\n%s", got)
	}
	m = step(t, m, interjectedMsg{items: m.pendingInterjections})
	if got := plain(m.View()); strings.Contains(got, "queued") {
		t.Errorf("the count survived a full delivery:\n%s", got)
	}
}

// TestPasteWhileRunningTypes replaces TestPasteIgnoredWhileRunning: a paste is an edit, and the
// box is editable while the model works.
func TestPasteWhileRunningTypes(t *testing.T) {
	m := runningModel(t)
	m.input.SetValue("keep")
	m = step(t, m, tea.PasteMsg{Content: " and this"})
	if got := m.input.Value(); got != "keep and this" {
		t.Fatalf("input = %q; want the pasted text appended while running", got)
	}
}

// TestScrollWhileRunningViaPgKeysAndWheel: the transcript scroll keys are ceded to typing while
// the model works, so the two routes that remain must both still work.
func TestScrollWhileRunningViaPgKeysAndWheel(t *testing.T) {
	scrolled := func(t *testing.T, msg tea.Msg) {
		t.Helper()
		m := runningModel(t)
		for i := 0; i < 40; i++ {
			m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), 0)
		}
		m.refreshViewport()
		m.viewport.GotoBottom()
		before := m.viewport.YOffset()
		if before == 0 {
			t.Fatal("precondition: viewport not scrolled; cannot observe a scroll back up")
		}
		next := step(t, m, msg)
		if got := next.viewport.YOffset(); got >= before {
			t.Errorf("%T did not scroll while running: offset %d → %d", msg, before, got)
		}
	}
	t.Run("pgup", func(t *testing.T) { scrolled(t, tea.KeyPressMsg{Code: tea.KeyPgUp}) })
	t.Run("wheel", func(t *testing.T) { scrolled(t, tea.MouseWheelMsg{Button: tea.MouseWheelUp}) })

	// The letter keys that used to scroll now type, which is the deliberate trade.
	m := step(t, runningModel(t), keyRune('k'))
	if got := m.input.Value(); got != "k" {
		t.Errorf("input = %q; want 'k' typed rather than scrolling the transcript", got)
	}
}

// TestFileAutocompleteOpensWhileRunning: the "@file" overlay is offered in an interjection (the
// ref is useful there), while the command and skill regions stay idle-only — offering a completion
// that would be refused would be the overlay lying about what ⏎ does.
func TestFileAutocompleteOpensWhileRunning(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture file: %v", err)
	}
	opts := testOpts
	opts.Workspace = dir
	opts.Skills = fakeSkillCatalog{skills: []skills.Skill{{ID: "review", DisplayName: "Review"}}}

	m := newTestModelEng(t, &fakeEngine{}, opts)
	m.input.SetValue("go")
	m, _ = stepCmd(t, m, keyEnter()) // → running, with a live mailbox

	m.input.SetValue("check @mai")
	m = step(t, m, keyRune('n'))
	if !m.autocomplete.active || m.autocomplete.kind != acFile {
		t.Fatalf("autocomplete = %+v; want the file region open while running", m.autocomplete)
	}

	m.input.SetValue("/clea")
	m = step(t, m, keyRune('r'))
	if m.autocomplete.active {
		t.Errorf("autocomplete = %+v; want no command region while running", m.autocomplete)
	}

	m.input.SetValue("/skill re")
	m = step(t, m, keyRune('v'))
	if m.autocomplete.active {
		t.Errorf("autocomplete = %+v; want no skill picker while running", m.autocomplete)
	}
}

// TestApprovalAndAskKeysUnchanged: the two rendezvous states keep the keyboard they always had —
// a/d/s decide, the ask box takes the answer, and neither stages anything.
func TestApprovalAndAskKeysUnchanged(t *testing.T) {
	t.Run("approval decides and stages nothing", func(t *testing.T) {
		m := runningModel(t)
		reply := make(chan domain.ApprovalDecision, 1)
		m = step(t, m, approvalReqMsg{Request: domain.ApprovalRequest{Tool: "write_file"}, Reply: reply})
		m = step(t, m, keyRune('a'))
		select {
		case got := <-reply:
			if got != domain.ApprovalAllow {
				t.Fatalf("decision = %v; want allow", got)
			}
		default:
			t.Fatal("'a' did not send the approval decision")
		}
		if m.input.Value() != "" {
			t.Errorf("input = %q; the approval keys must not type into the box", m.input.Value())
		}
		if len(m.pendingInterjections) != 0 {
			t.Errorf("staged rows = %+v; want none from an approval keypress", m.pendingInterjections)
		}
	})

	t.Run("ask answers rather than queues", func(t *testing.T) {
		m := runningModel(t)
		reply := make(chan domain.AskAnswer, 1)
		m = step(t, m, askReqMsg{Request: domain.AskRequest{Question: "which file?"}, Reply: reply})
		for _, r := range "42" {
			m = step(t, m, keyRune(r))
		}
		m = step(t, m, keyEnter())
		select {
		case got := <-reply:
			if got.Text != "42" {
				t.Fatalf("answer = %q; want the typed text", got.Text)
			}
		default:
			t.Fatal("⏎ at awaitingAsk did not answer the question")
		}
		if len(m.pendingInterjections) != 0 {
			t.Errorf("staged rows = %+v; an answer must never be queued", m.pendingInterjections)
		}
		if m.state != stateRunning {
			t.Errorf("state = %v; want running once the answer is away", m.state)
		}
	})
}

// The prompt's placeholder tells the truth about what ⏎ will do: it swaps to the queue legend when
// an Exchange opens, back to the send legend while the box is borrowed for an ask_user answer, and
// back again at the terminal fold.
func TestPlaceholderFollowsTheExchange(t *testing.T) {
	m := runningModel(t)
	if got := m.input.Placeholder; got != runningPlaceholder {
		t.Errorf("placeholder = %q; want the running legend", got)
	}

	reply := make(chan domain.AskAnswer, 1)
	m = step(t, m, askReqMsg{Request: domain.AskRequest{Question: "which file?"}, Reply: reply})
	if got := m.input.Placeholder; got != idlePlaceholder {
		t.Errorf("placeholder = %q; want the send legend while the box holds an answer", got)
	}
	m = step(t, m, keyEnter()) // answer away; the box is the human's own again
	if got := m.input.Placeholder; got != runningPlaceholder {
		t.Errorf("placeholder = %q; want the running legend once the answer is away", got)
	}

	m = step(t, m, exchangeDoneMsg{})
	if got := m.input.Placeholder; got != idlePlaceholder {
		t.Errorf("placeholder = %q; want the idle legend once the worker returned", got)
	}
}

// TestInterjectBoxRaceClean drives the one place the Update and worker goroutines share state:
// pushes racing drains. Under -race it proves the mutex covers both sides, and the accounting
// proves the box neither loses a row nor hands one out twice.
func TestInterjectBoxRaceClean(t *testing.T) {
	t.Parallel()
	const (
		pushers = 8
		perGo   = 50
		drains  = 4
	)
	box := newInterjectBox()

	var mu sync.Mutex
	seen := map[int]int{}
	collect := func(items []queuedInterjection) {
		mu.Lock()
		defer mu.Unlock()
		for _, it := range items {
			seen[it.id]++
		}
	}

	var wg sync.WaitGroup
	for p := range pushers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perGo {
				box.push(staged(p*perGo+i, "row"))
			}
		}()
	}
	for range drains {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perGo {
				collect(box.drainAll())
			}
		}()
	}
	wg.Wait()
	collect(box.drainAll()) // whatever the racing drains left behind

	if len(seen) != pushers*perGo {
		t.Fatalf("drained %d distinct rows; want every pushed row (%d)", len(seen), pushers*perGo)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("row %d drained %d times; want exactly once", id, n)
		}
	}
}

package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

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

	msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "go"}, box, rec.notify, nil)

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

			msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "go"}, tc.box, rec.notify, nil)

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

	msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "go"}, box, rec.notify, nil)

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

	msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "go"}, box, rec.notify, nil)

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

	msg := driveResume(context.Background(), eng, box, rec.notify, nil)

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

// TestCancelledDriveSkipsTheDrain is the first half of "Esc discards nothing" (ADR 0025 decision 7):
// a cancel that has already landed when the worker reaches a between-Steps boundary must not commit
// the mailbox into the Exchange it is about to scrap — AbortExchange drops everything committed
// there, so the rows would be sent by nobody and held by nobody. Skipped, they stay in the mailbox,
// are never reported, and so are never taken off the display queue: the terminal fold holds them for
// the next ⏎. The uncancelled counterpart is every other drain test above, which passes a live ctx.
func TestCancelledDriveSkipsTheDrain(t *testing.T) {
	t.Parallel()
	box := newInterjectBox()
	box.push(staged(1, "also check the tests"))
	rec := &recorder{}
	eng := &fakeEngine{inExchange: true}
	eng.stepFn = func(context.Context, int) (domain.StepResult, error) {
		return domain.StepResult{Status: domain.StatusCancelled}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Esc landed while the Turn ran: the worker reaches the boundary already cancelled

	msg := driveResume(ctx, eng, box, rec.notify, nil)

	if _, ok := msg.(cancelledMsg); !ok {
		t.Fatalf("terminal msg = %T; want cancelledMsg", msg)
	}
	if got := eng.interjections(); len(got) != 0 {
		t.Errorf("Interject calls = %+v; want none — the Exchange is about to be scrapped", got)
	}
	if _, seen := rec.delivered(t); seen {
		t.Error("a delivery was reported on a cancelled drive; the Model would take the row off the queue for it")
	}
	if left := box.drainAll(); len(left) != 1 || left[0].id != 1 {
		t.Errorf("mailbox = %+v; want the row left staged, for the stop to hold", left)
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

// TestCommandWhileRunningRefusedWithNote pins the thing that does NOT queue: a MUTATING /command
// needs a quiescent engine, so it earns a note and keeps the human's line in the box for the moment
// the Exchange ends. (The reporting verbs run right there — TestReportingCommandsRunWhileRunning.)
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

// Backspace on an empty box pops the queue and nothing else: with the chips retired there is no
// second staging area behind it, so a second press on an emptied queue is an ordinary no-op.
func TestBackspaceOnEmptyPopsOnlyTheQueue(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, testOpts)
	m.pendingInterjections = []queuedInterjection{staged(1, "held row")}

	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.input.Value(); got != "held row" {
		t.Fatalf("editor = %q; want the queued row popped back into the box", got)
	}
	if n := len(m.pendingInterjections); n != 0 {
		t.Fatalf("staged rows = %d; want the popped row off the queue", n)
	}

	m.input.SetValue("")
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.input.Value(); got != "" {
		t.Errorf("editor = %q; want an empty box — nothing else is staged to pop", got)
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

// ----------------------------------------------------------------------------
// The staged-row band above the input box
// ----------------------------------------------------------------------------

// TestQueuedStripRendersAsIndentedBand pins the band's shape against a real two-row queue: one
// blank framing row above and one below, every row painted out to the full window width so no seam
// shows the terminal's own background, and every content row indented into the body column behind
// its ⧖ marker, oldest first.
func TestQueuedStripRendersAsIndentedBand(t *testing.T) {
	m := runningModel(t)
	m = stageRow(t, m, "one")
	m = stageRow(t, m, "two")

	lines := strings.Split(ansi.Strip(m.renderPendingInterjections()), "\n")
	if len(lines) != 4 {
		t.Fatalf("band lines = %d (%q); want two content rows framed by a blank row each side", len(lines), lines)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != m.width {
			t.Errorf("line %d width = %d, want the full %d — the band must be painted edge to edge:\n%q",
				i, got, m.width, line)
		}
	}
	if strings.TrimSpace(lines[0]) != "" || strings.TrimSpace(lines[3]) != "" {
		t.Errorf("frame rows = %q / %q; want a blank band row above and below the group", lines[0], lines[3])
	}
	for i, text := range []string{"one", "two"} {
		want := bodyIndent + glyphInterject + " " + text
		if !strings.HasPrefix(lines[i+1], want) {
			t.Errorf("content line %d = %q; want it to start %q (indented, oldest first)", i, lines[i+1], want)
		}
	}
}

// TestQueuedStripOverflowMarkerRidesInsideTheBand: past the cap the "… N more queued" marker is an
// ordinary band row — indented, padded, and inside the frame rather than bare above it.
func TestQueuedStripOverflowMarkerRidesInsideTheBand(t *testing.T) {
	m := runningModel(t)
	for i := 0; i <= maxQueuedRows; i++ {
		m = stageRow(t, m, fmt.Sprintf("row %d", i))
	}

	lines := strings.Split(ansi.Strip(m.renderPendingInterjections()), "\n")
	if len(lines) != maxQueuedRows+3 {
		t.Fatalf("band lines = %d (%q); want the cap plus the marker plus two frame rows", len(lines), lines)
	}
	if strings.TrimSpace(lines[0]) != "" {
		t.Errorf("first line = %q; want the opening frame row above the marker", lines[0])
	}
	marker := lines[1]
	if want := bodyIndent + "… 1 more queued"; !strings.HasPrefix(marker, want) {
		t.Errorf("marker line = %q; want it to start %q", marker, want)
	}
	if got := ansi.StringWidth(marker); got != m.width {
		t.Errorf("marker width = %d, want the full %d — it is a band row like any other", got, m.width)
	}
}

// TestQueuedRowUsesThemeBandStyle pins the look to the theme field rather than to raw SGR bytes:
// the whole padded line — text, indent, and pad — goes through queuedText in one Render, which is
// what makes the background reach the window edge.
func TestQueuedRowUsesThemeBandStyle(t *testing.T) {
	m := stageRow(t, runningModel(t), "one")

	line := bodyIndent + glyphInterject + " one"
	line += strings.Repeat(" ", m.width-ansi.StringWidth(line))
	got := strings.Split(m.renderPendingInterjections(), "\n")[1]
	if want := m.th.queuedText.Render(line); got != want {
		t.Errorf("content row = %q; want the theme's queuedText band over %q", got, line)
	}
}

// TestQueuedStripHeightCountsItsFrame: View shrinks the transcript viewport by the strip's
// lipgloss.Height, so the frame rows must be inside the strip's own string for the accounting to
// stay right — and the chrome below it must survive the taller strip at the standard 80×24.
func TestQueuedStripHeightCountsItsFrame(t *testing.T) {
	m := runningModel(t)
	m = stageRow(t, m, "one")
	m = stageRow(t, m, "two")

	if got, want := lipgloss.Height(m.renderPendingInterjections()), 4; got != want {
		t.Errorf("strip height = %d, want %d (two content rows plus the frame)", got, want)
	}
	view := plain(m.View())
	if !strings.Contains(view, "2 queued") {
		t.Errorf("the status line lost its queued count under the taller band:\n%s", view)
	}
	if !strings.Contains(view, "╭") {
		t.Errorf("the input box lost its top border under the taller band:\n%s", view)
	}
}

// TestQueuedBandShrinksIntoTheFrameBudget is the band's half of the frame's ONE row allocation. The
// band used to be off the budget entirely — a fixed band of up to six rows (the cap, its marker and
// the two framing rows) taken from a viewport that had already promised those rows to whatever pane
// was open — so a dropdown beside a staged queue composed a frame four rows past the terminal's
// last row, and five staged messages overflowed a twelve-row terminal with no pane open at all.
//
// It now spends what [Model.frameRowPlan] leaves it, and the rows the BUDGET drops are counted in
// the same "… N more queued" marker the CAP's are: one wording for one fact, so a shorter window
// costs the band rows but never the knowledge that they are waiting. The marker outranks the content
// it describes — a budget that can seat one row spends it on the count, not on one of five — and
// under three rows the band is not drawn at all, the case the status line's "N queued" carries.
func TestQueuedBandShrinksIntoTheFrameBudget(t *testing.T) {
	cases := []struct {
		height     int  // the terminal's rows (the viewport gets height − 8)
		staged     int  // messages waiting to go out
		dropdown   bool // a "/" menu open beside the band, taking its four rows first
		wantHeight int  // the band's rows, framing rows included; 0 = not drawn
		wantHidden int  // the count its marker must state; 0 = no marker
	}{
		// Nothing else in the frame: the band shrinks against the transcript alone.
		{12, 2, false, 4, 0}, // viewport 4: two rows and their frame, exactly
		{12, 5, false, 4, 4}, // …the same four rows spent on the marker and ONE row
		{13, 5, false, 5, 3}, // viewport 5: one more row of content
		{14, 5, false, 6, 2}, // viewport 6: the band's own cap binds again
		{16, 5, false, 6, 2}, // roomier, and the cap still binds
		{24, 2, false, 4, 0}, // the ordinary case is untouched
		{12, 1, false, 3, 0}, // one row and its frame is the smallest band drawn
		// …and beside a pane, which takes its irreducible four rows first.
		{12, 2, true, 0, 0}, // viewport 4, all of it the dropdown's: no band at all
		{13, 5, true, 0, 0}, // viewport 5: still nothing to spare
		{14, 2, true, 0, 0}, // viewport 6: two rows is under the band's own floor of three
		{16, 5, true, 4, 4}, // viewport 8: four rows left, spent on the marker and one row
		{20, 5, true, 6, 2}, // viewport 12: the band gets its full worst case
	}

	for _, c := range cases {
		name := fmt.Sprintf("%d rows/%d staged", c.height, c.staged)
		if c.dropdown {
			name += "/dropdown"
		}
		t.Run(name, func(t *testing.T) {
			m := withStagedRows(modelWithOverlayRoomAt(t, 80, c.height, testOpts), c.staged)
			if c.dropdown {
				m.input.SetValue("/")
				m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
			}

			band := m.renderPendingInterjections()
			if band == "" {
				if c.wantHeight != 0 {
					t.Fatalf("band not drawn at all, want %d rows", c.wantHeight)
				}
				return
			}
			flat := ansi.Strip(band)
			if got := lipgloss.Height(band); got != c.wantHeight {
				t.Errorf("band is %d rows, want %d:\n%s", got, c.wantHeight, flat)
			}
			// Every row it did seat is a real staged message, newest kept: the marker replaces the
			// OLDEST rows, so the row nearest the box is still the one Backspace takes back.
			for i := c.staged - (c.wantHeight - 2 - min(c.wantHidden, 1)); i < c.staged; i++ {
				if want := fmt.Sprintf("staged remark %02d", i); !strings.Contains(flat, want) {
					t.Errorf("band is missing %q — the rows it seats must be the newest:\n%s", want, flat)
				}
			}
			marker := fmt.Sprintf("… %d more queued", c.wantHidden)
			if c.wantHidden == 0 {
				if strings.Contains(flat, "more queued") {
					t.Errorf("band counts rows it is not holding back:\n%s", flat)
				}
				return
			}
			if !strings.Contains(flat, marker) {
				t.Errorf("band dropped %d rows without the %q marker:\n%s", c.wantHidden, marker, flat)
			}
		})
	}
}

// TestSuppressedBandKeepsItsCountOnTheStatusLine is the other half of the band's give-way contract.
// The band is the FIRST surface the frame's row allocation drops, and layout.md's licence for
// dropping it is that "the status line's N queued readout is what carries the count instead" — so
// on the windows where the band is gone that readout is the only thing the frame says about the
// queue, and it has to actually be ON the frame.
//
// It was not. The status line composed its left slot at full length and clipped the whole thing to
// the window, which puts the count on the END of the clip: at 20 columns a running turn's activity
// phrase pushed it off the row, so a short AND narrow terminal — one split pane, the same terminal
// the band is being dropped for — showed neither the band nor its count. The slot now spends its
// width in the order it is read for (statusLeft), so the phrase is what gives way to the count.
func TestSuppressedBandKeepsItsCountOnTheStatusLine(t *testing.T) {
	// Each state opens a pane beside the queue, because a pane is what takes the rows the band would
	// otherwise have had: at twelve and thirteen rows its four leave the band nothing at all.
	dropdown := func(m Model) Model {
		m.input.SetValue("/")
		m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
		m.layout()
		return m
	}
	states := []struct {
		name  string
		place func(t *testing.T, m Model) Model
	}{
		{"running, a / menu open", func(t *testing.T, m Model) Model {
			t.Helper()
			m.state = stateRunning
			m.setActivity(runRef{}, actTool, "reading")
			return dropdown(m)
		}},
		{"awaiting approval", func(t *testing.T, m Model) Model {
			t.Helper()
			m.state = stateRunning
			return step(t, m, approvalReqMsg{Request: domain.ApprovalRequest{Tool: "write_file"}})
		}},
		{"idle, held over a stop", func(t *testing.T, m Model) Model { return dropdown(m) }},
	}

	// 20 columns is the width the band's own frame properties do not reach and the one a half-height
	// tmux pane really is; the wider two are there to pin that nothing regressed above it.
	for _, s := range states {
		for _, width := range []int{20, narrowOverlayWindow, 80} {
			for _, height := range []int{smallestOverlayWindow, 13} {
				t.Run(fmt.Sprintf("%s/%d×%d", s.name, width, height), func(t *testing.T) {
					m := withStagedRows(modelWithOverlayRoomAt(t, width, height, testOpts), 5)
					m = s.place(t, m)

					view := plain(m.View())
					if strings.Contains(view, "staged remark") {
						t.Fatalf("the band seated a row at %d×%d — test premise broken:\n%s", width, height, view)
					}
					if !strings.Contains(view, "5 queued") {
						t.Errorf("the band is gone and the status line does not carry %q either:\n%s", "5 queued", view)
					}
					for _, row := range strings.Split(view, "\n") {
						if got := m.th.measure.Width(row); got > width {
							t.Errorf("row %q is %d cells on a %d-column terminal", row, got, width)
						}
					}
				})
			}
		}
	}
}

// TestBandShapeSeatsTheHintLast is the two surfaces' share of ONE band. The staged rows are a fact
// the human put there and the skill-suggestion row is advice about the draft, so the queue takes
// its rows out of the budget FIRST and the hint is offered only what is left — and beside a seated
// queue what is left is the group's lower framing row, which the hint takes over rather than adding
// a row of its own (bandPlan.height, layout.md). A window too short to seat the staged rows is too
// short to spend on advice instead: the hint is refused there, not promoted into the rows the queue
// was denied.
func TestBandShapeSeatsTheHintLast(t *testing.T) {
	cases := []struct {
		name       string
		staged     int
		hints      bool
		budget     int
		wantShown  int
		wantHidden int
		wantHint   bool
		wantHeight int
	}{
		{"hint alone", 0, true, 6, 0, 0, true, 2},
		{"hint alone on its floor", 0, true, 2, 0, 0, true, 2},
		{"hint alone under its floor", 0, true, 1, 0, 0, false, 0},
		{"no hint wanted", 0, false, 6, 0, 0, false, 0},
		{"hint rides the queue's framing row", 2, true, 4, 2, 0, true, 4},
		{"hint beside a queue that is itself shedding rows", 5, true, 4, 1, 4, true, 4},
		{"queue seated, hint not wanted", 2, false, 4, 2, 0, false, 4},
		{"queue denied: the hint is denied with it", 3, true, 2, 0, 0, false, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan := bandShape(c.staged, c.budget, c.hints)

			if plan.shown != c.wantShown || plan.hidden != c.wantHidden || plan.hint != c.wantHint {
				t.Errorf(
					"bandShape(%d, %d, %v) = {shown %d, hidden %d, hint %v}, want {shown %d, hidden %d, hint %v}",
					c.staged, c.budget, c.hints,
					plan.shown, plan.hidden, plan.hint,
					c.wantShown, c.wantHidden, c.wantHint,
				)
			}
			if got := plan.height(); got != c.wantHeight {
				t.Errorf("plan.height() = %d, want %d", got, c.wantHeight)
			}
			if plan.height() > c.budget {
				t.Errorf("the band claims %d rows out of a %d-row budget", plan.height(), c.budget)
			}
		})
	}
}

// TestHintRowIsNearestTheInputBox pins where the two band surfaces land relative to each other. The
// hint is about the DRAFT and the draft is in the box, so the hint row is the last row of the group
// — below the staged rows, directly above the box — and the group is framed by exactly one blank
// band row above it, never by one between the two surfaces: the staged strip gives up its lower
// framing row so the block still reads as one object.
func TestHintRowIsNearestTheInputBox(t *testing.T) {
	var rec suggestCall
	m := withStagedRows(modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec))), 2)
	m = typeDraft(t, m, "audit the parser")
	if len(m.skillHints) == 0 {
		t.Fatal("no hints on the model; nothing to place")
	}

	ov := m.frameOverlays()
	rows := strings.Split(ansi.Strip(ov.queued+"\n"+ov.hint), "\n")

	if want := bandShape(2, m.transcriptBudget(), true).height(); len(rows) != want {
		t.Fatalf("the band composed %d rows, want the %d its plan grants:\n%s", len(rows), want, strings.Join(rows, "\n"))
	}
	if strings.TrimSpace(rows[0]) != "" {
		t.Errorf("the group does not open on its blank framing row: %q", rows[0])
	}
	for i, row := range rows[1 : len(rows)-1] {
		if !strings.Contains(row, glyphInterject) {
			t.Errorf("row %d of the group is not a staged row: %q", i+1, row)
		}
	}
	if last := rows[len(rows)-1]; !strings.Contains(last, glyphSkill) {
		t.Errorf("the row nearest the input box is not the hint: %q", last)
	}
}

// TestQueuedStripEmptyWithoutAQueue: no queue, no band — not even the framing rows, which would
// otherwise leak two blank black lines into every idle frame.
func TestQueuedStripEmptyWithoutAQueue(t *testing.T) {
	if got := runningModel(t).renderPendingInterjections(); got != "" {
		t.Errorf("strip = %q with nothing staged; want the empty string", got)
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
			m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), runRef{})
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

// TestAutocompleteOpensWhileRunning: both regions are offered in an interjection — the first
// ISSUES #12 symptom was the "/" namespace vanishing exactly when the human was composing the
// message to send next. A @ref and a skill token are message content that rides the interjection; a
// command row is offered too, and the ones that need a boundary carry the "— idle only" tag rather
// than being hidden, so the menu says what accepting them will do.
func TestAutocompleteOpensWhileRunning(t *testing.T) {
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
	if !m.autocomplete.active || m.autocomplete.kind != acCommand {
		t.Fatalf("autocomplete = %+v; want the merged command menu open while running", m.autocomplete)
	}
	if len(m.autocomplete.items) != 1 || m.autocomplete.items[0].value != "clear" {
		t.Fatalf("rows = %+v, want [clear]", m.autocomplete.items)
	}
	if cells := m.autocomplete.items[0].cells; !containsString(cells, idleOnlyTag) {
		t.Errorf("row cells = %q, want the %q tag cell — /clear cannot run mid-Step", cells, idleOnlyTag)
	}
	if got := plain(m.View()); !strings.Contains(got, idleOnlyTag) {
		t.Errorf("the rendered dropdown is missing the idle-only tag:\n%s", got)
	}

	m.input.SetValue("check /re")
	m = step(t, m, keyRune('v'))
	if !m.autocomplete.active || m.autocomplete.kind != acCommand {
		t.Fatalf("autocomplete = %+v; want the merged menu open mid-draft while running", m.autocomplete)
	}
	if len(m.autocomplete.items) != 1 || !m.autocomplete.items[0].skill {
		t.Fatalf("rows = %+v, want the single review skill row", m.autocomplete.items)
	}
}

// A reporting verb answers on the spot while the model works: /version and /skills touch no engine
// boundary at all, so ⏎ on one prints its note, empties the box (the whole-input form IS the command
// line), and leaves the worker and the queue exactly where they were — nothing staged, nothing sent.
func TestReportingCommandsRunWhileRunning(t *testing.T) {
	opts := testOpts
	opts.Version = "9.9.9-test"
	cases := []struct {
		verb string
		want string
	}{
		{"/version", "apogee 9.9.9-test"},
		{"/skills", "no skills found"}, // no catalog is wired: the empty-catalog note is the report
	}
	for _, c := range cases {
		t.Run(c.verb, func(t *testing.T) {
			m := newTestModelEng(t, &fakeEngine{}, opts)
			m.input.SetValue("open the exchange")
			m, _ = stepCmd(t, m, keyEnter())
			if m.state != stateRunning {
				t.Fatalf("precondition: state = %v, want running", m.state)
			}
			m = stageRow(t, m, "hold that thought") // a staged row the command must not disturb
			m.input.SetValue(c.verb)
			next, cmd := stepCmd(t, m, keyEnter())

			if cmd != nil {
				t.Error("a reporting command returned a Cmd; it must not drive a worker")
			}
			if next.state != stateRunning {
				t.Errorf("state = %v; want the running Exchange untouched", next.state)
			}
			if got := next.input.Value(); got != "" {
				t.Errorf("input = %q; want the box emptied — the line was nothing but the verb", got)
			}
			if n := len(next.pendingInterjections); n != 1 {
				t.Errorf("staged rows = %d; want the queue untouched (1)", n)
			}
			if got := plain(next.View()); !strings.Contains(got, c.want) {
				t.Errorf("transcript missing the %s report %q:\n%s", c.verb, c.want, got)
			}
		})
	}
}

// /confine is per-FORM while running: the status report reads Engine.ConfineToWorkspace (goroutine-
// safe, like SetMode) and answers immediately, while "off" would swap Auto's blast radius under a
// Step that is already dispatching tool calls — so it is refused, the engine untouched.
func TestConfineStatusRunsWhileRunningButOffIsRefused(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("open the exchange")
	m, _ = stepCmd(t, m, keyEnter())
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}

	m.input.SetValue("/confine")
	m = step(t, m, keyEnter())
	if got := plain(m.View()); !strings.Contains(got, "/confine — auto mode's blast radius") {
		t.Errorf("the status report is missing while running:\n%s", got)
	}
	if got := eng.confinesSet(); len(got) != 0 {
		t.Errorf("SetConfineToWorkspace calls = %v, want none (status reports only)", got)
	}

	m.input.SetValue("/confine off")
	m = step(t, m, keyEnter())
	if got := m.input.Value(); got != "/confine off" {
		t.Errorf("input = %q; want the refused line preserved", got)
	}
	if got := eng.confinesSet(); len(got) != 0 {
		t.Errorf("SetConfineToWorkspace calls = %v, want none — the mutating form is idle-only", got)
	}
	if got := plain(m.View()); !strings.Contains(got, commandsAtIdleNote) {
		t.Errorf("the refusal note is missing from the transcript:\n%s", got)
	}
}

// Accepting a tagged row from the dropdown while running answers with the note and touches NOTHING
// else: the draft (the verb token included — it was never consumed) is exactly as it was, so ⏎ on
// the very same line works the moment the Exchange ends.
func TestAcceptIdleOnlyCommandWhileRunningNotesAndKeepsTheDraft(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("open the exchange")
	m, _ = stepCmd(t, m, keyEnter())

	m.input.SetValue("fix the parser /clear")
	m.input.MoveToEnd()
	m, _ = m.recomputeAutocomplete()
	if !m.autocomplete.active {
		t.Fatalf("precondition: the menu did not open on the trailing token")
	}
	next, cmd := stepCmd(t, m, keyTab())

	if cmd != nil {
		t.Error("a refused accept returned a Cmd; nothing may be driven while a worker runs")
	}
	if eng.clearCalls != 0 {
		t.Errorf("ClearContext calls = %d, want 0 — /clear cannot run mid-Step", eng.clearCalls)
	}
	if got, want := next.input.Value(), "fix the parser /clear"; got != want {
		t.Errorf("editor = %q, want the draft untouched (%q)", got, want)
	}
	if next.autocomplete.active {
		t.Error("the overlay stayed open after the accept was answered")
	}
	if got := plain(next.View()); !strings.Contains(got, commandsAtIdleNote) {
		t.Errorf("the refusal note is missing from the transcript:\n%s", got)
	}
}

// A skill accepted from the merged menu while running splices its inline token like any other text,
// and the staged row carries the id out to the engine — a skill is message content, so it rides the
// interjection rather than answering to the command policy.
func TestAcceptSkillWhileRunningStagesTheID(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("open the exchange")
	m, _ = stepCmd(t, m, keyEnter())
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}

	m.input.SetValue("also /revi")
	m.input.MoveToEnd()
	m, _ = m.recomputeAutocomplete()
	if !m.autocomplete.active || m.autocomplete.kind != acCommand {
		t.Fatalf("autocomplete = %+v; want the merged menu open on the skill token", m.autocomplete)
	}
	m = step(t, m, keyTab())
	if got, want := m.input.Value(), "also /review "; got != want {
		t.Errorf("editor = %q, want the spliced token (%q)", got, want)
	}

	m = step(t, m, keyEnter())
	if n := len(m.pendingInterjections); n != 1 {
		t.Fatalf("staged rows = %d, want 1", n)
	}
	if got := m.pendingInterjections[0].input.SkillIDs; !reflect.DeepEqual(got, []string{"review"}) {
		t.Errorf("staged SkillIDs = %v, want [review]", got)
	}
}

// TestApprovalAndAskKeysUnchanged: the two rendezvous states keep the keyboard they always had —
// a/d/s decide, the ask box takes the answer, and neither stages anything.
func TestApprovalAndAskKeysUnchanged(t *testing.T) {
	t.Run("approval decides and stages nothing", func(t *testing.T) {
		m := runningModel(t)
		reply := make(chan domain.ApprovalDecision, 1)
		m = step(t, m, approvalReqMsg{Request: domain.ApprovalRequest{Tool: "write_file"}, Reply: reply})
		m = armApproval(t, m) // the decision keys go live one arming tick after the pane opens
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

// ----------------------------------------------------------------------------
// Flush orchestration — auto-send on a natural completion, hold on a stop (ADR 0025)
// ----------------------------------------------------------------------------

// heldModel leaves a model at idle with rows the queue is HOLDING: it opens an Exchange, stages
// each text into it, then cancels — the Esc path, which by decision keeps the queue standing. The
// engine it returns is scripted to complete any drive at once, so the flush Cmd can be drained.
func heldModel(t *testing.T, texts ...string) (Model, *fakeEngine) {
	t.Helper()
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("open the exchange")
	m, _ = stepCmd(t, m, keyEnter())
	for _, text := range texts {
		m = stageRow(t, m, text)
	}
	m = step(t, m, cancelledMsg{})
	if m.state != stateIdle {
		t.Fatalf("precondition: state = %v, want idle after the cancel", m.state)
	}
	if len(m.pendingInterjections) != len(texts) {
		t.Fatalf("precondition: %d rows held, want %d — a stop must not flush", len(m.pendingInterjections), len(texts))
	}
	return m, eng
}

// lastEntry returns the transcript's tail entry — what the send or the fold just wrote.
func lastEntry(t *testing.T, m Model) entry {
	t.Helper()
	if len(m.transcript.entries) == 0 {
		t.Fatal("the transcript is empty")
	}
	return m.transcript.entries[len(m.transcript.entries)-1]
}

// TestExchangeDoneFlushesQueue is the auto-send: rows still staged when the Exchange completes
// under its own power open the NEXT Exchange, as one message joining them oldest-first, with no
// keypress in between.
func TestExchangeDoneFlushesQueue(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("open the exchange")
	m, _ = stepCmd(t, m, keyEnter())
	m = stageRow(t, m, "also check the tests")
	m = stageRow(t, m, "and the docs")

	next, cmd := stepCmd(t, m, exchangeDoneMsg{})

	if next.state != stateRunning {
		t.Fatalf("state = %v; want running — the flush opens a new Exchange", next.state)
	}
	if n := len(next.pendingInterjections); n != 0 {
		t.Errorf("staged rows = %d; want the queue emptied by the flush", n)
	}
	const want = "also check the tests\n\nand the docs"
	if e := lastEntry(t, next); e.kind != entryUser || e.text != want {
		t.Errorf("tail entry = %+v; want one user block carrying %q", e, want)
	}
	drainCmd(t, next, cmd) // run the worker Cmd so Submit actually lands
	if n := len(eng.submitted); n != 1 {
		t.Fatalf("Submit calls = %d; want exactly one new Exchange", n)
	}
	if got := eng.submitted[0].Text; got != want {
		t.Errorf("submitted text = %q; want the blank-line join in FIFO order", got)
	}
	if got := eng.interjections(); len(got) != 0 {
		t.Errorf("Interject calls = %+v; a flushed message opens an Exchange, it does not interject", got)
	}
}

// TestCompactDoneFlushes: a compaction is a natural completion too. /compact drives no Exchange
// (it carries no mailbox), so a row typed while it runs has been waiting for exactly this boundary.
func TestCompactDoneFlushes(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("/compact")
	m, _ = stepCmd(t, m, keyEnter())
	if m.state != stateRunning || m.box != nil {
		t.Fatalf("precondition: state = %v, box = %v; want a running compaction with no mailbox", m.state, m.box)
	}
	m = stageRow(t, m, "now the tests")

	next, cmd := stepCmd(t, m, compactDoneMsg{})

	if next.state != stateRunning {
		t.Fatalf("state = %v; want running — the compaction's completion flushed the queue", next.state)
	}
	if n := len(next.pendingInterjections); n != 0 {
		t.Errorf("staged rows = %d; want the queue emptied", n)
	}
	if e := lastEntry(t, next); e.kind != entryUser || e.text != "now the tests" {
		t.Errorf("tail entry = %+v; want the flushed row as a user block", e)
	}
	drainCmd(t, next, cmd)
	if n := len(eng.submitted); n != 1 || eng.submitted[0].Text != "now the tests" {
		t.Errorf("submitted = %+v; want the single flushed message", eng.submitted)
	}
}

// TestCancelHoldsWithSingleNote: Esc stops everything, the queue included. Nothing is sent, the
// rows stay exactly where they were, and the hold is stated once — not once per keypress after it.
func TestCancelHoldsWithSingleNote(t *testing.T) {
	m := runningModel(t)
	m = stageRow(t, m, "one")
	m = stageRow(t, m, "two")

	next, _ := stepCmd(t, m, cancelledMsg{})

	if next.state != stateIdle {
		t.Fatalf("state = %v; want idle — a stop launches nothing", next.state)
	}
	if n := len(next.pendingInterjections); n != 2 {
		t.Fatalf("staged rows = %d; want both held by the stop", n)
	}
	note := heldNote(2)
	if got := countNotes(next, note); got != 1 {
		t.Errorf("hold note %q written %d times; want exactly once", note, got)
	}
	view := plain(next.View())
	if !strings.Contains(view, note) {
		t.Errorf("the hold note is missing from the transcript:\n%s", view)
	}
	if !strings.Contains(view, "2 queued") {
		t.Errorf("the held count is missing from the idle status line:\n%s", view)
	}
	// Typing on does not re-state the hold: the note belongs to the transition, not to the state.
	next = step(t, next, keyRune('x'))
	if got := countNotes(next, note); got != 1 {
		t.Errorf("hold note written %d times after a later keypress; want still once", got)
	}
}

// TestCancelDropsADeliveredRow: the cancel lands AFTER the worker committed a row. Sent is sent
// (owner ruling 2026-08-03) — the fold's AbortExchange drops that row from the conversation with the
// rest of the scrapped Exchange and it stays dropped: the queue holds only the row the worker never
// delivered, the hold note counts only that one, and the next ⏎ does not re-send what the model
// already read. The transcript's ⧖ record stays put as the surviving evidence of that delivery.
func TestCancelDropsADeliveredRow(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("open the exchange")
	m, _ = stepCmd(t, m, keyEnter())
	m = stageRow(t, m, "also check the tests")
	m = stageRow(t, m, "and the docs")
	m = step(t, m, interjectedMsg{items: m.pendingInterjections[:1]}) // the worker committed the first
	if n := len(m.pendingInterjections); n != 1 {
		t.Fatalf("precondition: staged rows = %d; want the delivered row off the queue", n)
	}

	next, _ := stepCmd(t, m, cancelledMsg{})

	if n := len(next.pendingInterjections); n != 1 {
		t.Fatalf("staged rows = %d; want only the undelivered row — a delivered one is not re-queued", n)
	}
	if got := next.pendingInterjections[0].raw; got != "and the docs" {
		t.Errorf("queue = %q; want the undelivered row alone", got)
	}
	if note := heldNote(1); countNotes(next, note) != 1 {
		t.Errorf("hold note %q not written exactly once; it counts the undelivered row only", note)
	}
	var interjected int
	for _, e := range next.transcript.entries {
		if e.kind == entryInterjected {
			interjected++
		}
	}
	if interjected != 1 {
		t.Errorf("interjected transcript blocks = %d; want the delivery record left standing", interjected)
	}

	sent, cmd := stepCmd(t, next, keyEnter()) // ⏎ on the empty box sends what the stop held
	if sent.state != stateRunning {
		t.Fatalf("state = %v; want running — the held queue sends on ⏎", sent.state)
	}
	drainCmd(t, sent, cmd)
	const want = "and the docs"
	if n := len(eng.submitted); n != 1 || eng.submitted[0].Text != want {
		t.Errorf("submitted = %+v; want only the undelivered row (%q) — the delivered one is not re-sent",
			eng.submitted, want)
	}
}

// TestNaturalCompletionKeepsDeliveredRowsDelivered pins the same rule at the other terminal fold: a
// row committed into an Exchange that ended under its own power is history. It is not re-staged, not
// re-sent, and a later Exchange's stop cannot resurrect it either.
func TestNaturalCompletionKeepsDeliveredRowsDelivered(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("open the exchange")
	m, _ = stepCmd(t, m, keyEnter())
	m = stageRow(t, m, "also check the tests")
	m = step(t, m, interjectedMsg{items: m.pendingInterjections})

	next, cmd := stepCmd(t, m, exchangeDoneMsg{})

	if next.state != stateIdle {
		t.Fatalf("state = %v; want idle — an empty queue flushes nothing", next.state)
	}
	if n := len(next.pendingInterjections); n != 0 {
		t.Fatalf("staged rows = %+v; want none — the row was delivered, not held", next.pendingInterjections)
	}
	drainCmd(t, next, cmd)
	if n := len(eng.submitted); n != 0 {
		t.Errorf("Submit calls = %d; want none — a delivered row must not be sent a second time", n)
	}

	next.input.SetValue("next question") // a second Exchange, stopped with Esc
	next, _ = stepCmd(t, next, keyEnter())
	stopped, _ := stepCmd(t, next, cancelledMsg{})
	if n := len(stopped.pendingInterjections); n != 0 {
		t.Errorf("staged rows = %+v; want none — an earlier Exchange's delivery is not this stop's to undo",
			stopped.pendingInterjections)
	}
}

// TestErrorHolds: a loop fault holds the queue exactly as a stop does, and clearing the error
// takes its own ⏎ — the first press dismisses, the second sends what was held.
func TestErrorHolds(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("open the exchange")
	m, _ = stepCmd(t, m, keyEnter())
	m = stageRow(t, m, "still worth sending")

	m, _ = stepCmd(t, m, errMsg{Err: errors.New("upstream fell over")})

	if m.state != stateErrored {
		t.Fatalf("state = %v; want errored", m.state)
	}
	if n := len(m.pendingInterjections); n != 1 {
		t.Fatalf("staged rows = %d; want the row held by the fault", n)
	}
	note := heldNote(1)
	if got := countNotes(m, note); got != 1 {
		t.Errorf("hold note %q written %d times; want exactly once", note, got)
	}

	m, cmd := stepCmd(t, m, keyEnter()) // first press: dismiss the error
	if m.state != stateIdle {
		t.Fatalf("state = %v; want idle once the error is dismissed", m.state)
	}
	if cmd != nil {
		t.Error("dismissing the error launched something; the held queue waits for its own ⏎")
	}
	if n := len(m.pendingInterjections); n != 1 {
		t.Fatalf("staged rows = %d; want the row still held after the dismissal", n)
	}

	m, cmd = stepCmd(t, m, keyEnter()) // second press: send it
	if m.state != stateRunning {
		t.Fatalf("state = %v; want running — the second ⏎ sends the held queue", m.state)
	}
	drainCmd(t, m, cmd)
	if n := len(eng.submitted); n != 1 || eng.submitted[0].Text != "still worth sending" {
		t.Errorf("submitted = %+v; want the held row sent by the second press", eng.submitted)
	}
}

// TestIdleEnterEmptyInputSendsHeld: with rows held, ⏎ on an EMPTY box is a send, not a no-op.
func TestIdleEnterEmptyInputSendsHeld(t *testing.T) {
	m, eng := heldModel(t, "first", "second")

	next, cmd := stepCmd(t, m, keyEnter())

	if next.state != stateRunning {
		t.Fatalf("state = %v; want running — ⏎ on a held queue sends it", next.state)
	}
	if n := len(next.pendingInterjections); n != 0 {
		t.Errorf("staged rows = %d; want the queue emptied by the send", n)
	}
	const want = "first\n\nsecond"
	if e := lastEntry(t, next); e.kind != entryUser || e.text != want {
		t.Errorf("tail entry = %+v; want one user block carrying %q", e, want)
	}
	drainCmd(t, next, cmd)
	if n := len(eng.submitted); n != 1 || eng.submitted[0].Text != want {
		t.Errorf("submitted = %+v; want the join in FIFO order", eng.submitted)
	}
}

// TestIdleEnterMergesEditorLast: what is in the box goes out WITH the held rows, and last — it is
// the newest thing the human wrote. The @file references of both halves are unioned, once each.
func TestIdleEnterMergesEditorLast(t *testing.T) {
	m, eng := heldModel(t, "look at @a.go", "and @b.go")
	m.input.SetValue("plus @a.go once more")

	next, cmd := stepCmd(t, m, keyEnter())

	const want = "look at @a.go\n\nand @b.go\n\nplus @a.go once more"
	if e := lastEntry(t, next); e.kind != entryUser || e.text != want {
		t.Errorf("tail entry = %+v; want the editor's text joined LAST: %q", e, want)
	}
	if got := next.input.Value(); got != "" {
		t.Errorf("input = %q; want the box emptied by the send", got)
	}
	drainCmd(t, next, cmd)
	if n := len(eng.submitted); n != 1 {
		t.Fatalf("Submit calls = %d; want one merged message", n)
	}
	in := eng.submitted[0]
	if in.Text != want {
		t.Errorf("submitted text = %q; want %q", in.Text, want)
	}
	if !reflect.DeepEqual(in.FileRefs, []string{"a.go", "b.go"}) {
		t.Errorf("submitted FileRefs = %v; want the union in first-seen order, de-duplicated", in.FileRefs)
	}
}

// TestJoinedInterjectionsRebasesSkillSpans: a flush composes several messages into ONE block, so
// the token offsets each row measured in its own text have to move onto the composition. Left
// un-rebased, a second row's accent would paint a run of the first row's prose — the spans are
// byte offsets into the joined text, and only the join knows where each row landed in it.
func TestJoinedInterjectionsRebasesSkillSpans(t *testing.T) {
	t.Parallel()
	known := knownSkills("refocus", "code-audit")
	held := parseInput("/refocus the docs", known)
	tail := parseInput("then /code-audit it", known)

	m := Model{pendingInterjections: []queuedInterjection{{
		id:         1,
		raw:        "/refocus the docs",
		input:      domain.UserInput{Text: held.text, SkillIDs: held.skillIDs},
		skillSpans: held.skillSpans,
	}}}

	in, spans := m.joinedInterjections(tail)

	if len(spans) != 2 {
		t.Fatalf("joined spans = %v; want one per token, both rows", spans)
	}
	for i, want := range []string{"/refocus", "/code-audit"} {
		sp := spans[i]
		if sp.end > len(in.Text) {
			t.Fatalf("span %v runs past the joined text %q", sp, in.Text)
		}
		if got := in.Text[sp.start:sp.end]; got != want {
			t.Errorf("span %v locates %q in the joined text; want %q", sp, got, want)
		}
	}
}

// TestQuitDeferredBeatsFlush: a quit requested while the model works exits at the terminal fold.
// It must not be overtaken by the flush — staged rows are session-ephemeral, and opening a fresh
// Exchange into a program that is leaving would either be abandoned or delay the exit.
func TestQuitDeferredBeatsFlush(t *testing.T) {
	m := runningModel(t)
	m = stageRow(t, m, "never sent")
	m, quitCmd := ctrlCQuit(t, m)
	if !m.quitting {
		t.Fatal("precondition: the quit was not deferred to the terminal fold")
	}
	if quitCmd != nil {
		t.Fatal("precondition: a busy quit returned a Cmd instead of deferring")
	}

	next, cmd := stepCmd(t, m, exchangeDoneMsg{})

	if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); !isQuit {
		t.Fatalf("terminal Cmd = %T; want tea.Quit — the deferred exit outranks the flush", cmdMsg(cmd))
	}
	if next.state == stateRunning {
		t.Error("the flush launched a new Exchange during a deferred quit")
	}
}

// TestClearKeepsHeldRows: /clear starts a fresh session, and a held queue is not part of the
// session it clears — the rows are outgoing input the human wrote and has not unwritten.
func TestClearKeepsHeldRows(t *testing.T) {
	m, _ := heldModel(t, "keep me")
	m.input.SetValue("/clear")

	next, cmd := stepCmd(t, m, keyEnter())

	if cmd != nil {
		t.Error("/clear returned a Cmd; it must stay synchronous and idle")
	}
	if n := len(next.pendingInterjections); n != 1 || next.pendingInterjections[0].raw != "keep me" {
		t.Fatalf("staged rows = %+v; want the held row to survive the reset", next.pendingInterjections)
	}
	if got := plain(next.View()); !strings.Contains(got, "1 queued") {
		t.Errorf("the fresh session forgot to say what is still queued:\n%s", got)
	}
}

// TestEndToEndInterjectionScript is the whole feature in one fold-level script: two remarks typed
// mid-run, one delivered at the boundary the worker passed, the other flushed when the Exchange
// completes. The transcript must read in DELIVERY order — no duplicates, no reordering — because
// that is the record's whole claim: what the model saw, and when.
func TestEndToEndInterjectionScript(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("refactor the parser")
	m, _ = stepCmd(t, m, keyEnter())

	m = step(t, m, eventMsg{Event: domain.MessageEvent{Text: "turn one"}})
	m = stageRow(t, m, "also check the tests")
	m = stageRow(t, m, "and the docs")
	m = step(t, m, interjectedMsg{items: m.pendingInterjections[:1]}) // the worker delivered the first
	m = step(t, m, eventMsg{Event: domain.MessageEvent{Text: "turn two"}})

	next, cmd := stepCmd(t, m, exchangeDoneMsg{}) // …and the second flushes here

	want := []entry{
		{kind: entryUser, text: "refactor the parser"},
		{kind: entryAssistant, text: "turn one"},
		{kind: entryInterjected, text: "also check the tests"},
		{kind: entryAssistant, text: "turn two"},
		{kind: entryUser, text: "and the docs"},
	}
	got := next.transcript.entries[1:] // entries[0] is the start-up box
	if len(got) != len(want) {
		t.Fatalf("transcript = %+v; want exactly %d entries after the start-up box", got, len(want))
	}
	for i := range want {
		if got[i].kind != want[i].kind || got[i].text != want[i].text {
			t.Fatalf("transcript[%d] = {kind:%v text:%q}; want {kind:%v text:%q}",
				i, got[i].kind, got[i].text, want[i].kind, want[i].text)
		}
	}
	if n := len(next.pendingInterjections); n != 0 {
		t.Errorf("staged rows = %d; want none — one was delivered, one was flushed", n)
	}
	drainCmd(t, next, cmd)
	if n := len(eng.submitted); n != 1 || eng.submitted[0].Text != "and the docs" {
		t.Errorf("submitted = %+v; want the flushed remark as the new Exchange's opening message", eng.submitted)
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

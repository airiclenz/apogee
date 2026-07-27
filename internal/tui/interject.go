package tui

import (
	"fmt"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The interjection mailbox (ADR 0025)
// ----------------------------------------------------------------------------

// queuedInterjection is one message the human typed while the model was working, staged for
// delivery into the running Exchange. It carries three things because three different consumers
// need different halves of it:
//
//   - id names the row across the two copies of the queue — the Model's display slice and the
//     mailbox the worker drains — so the delivery fold can remove exactly the rows that landed
//     without depending on order or on pointer identity. Ids are minted by the staging side
//     (the Update goroutine) and are unique within a session, never reused.
//   - raw is the pre-parse editor text, kept verbatim so popping the newest row back into the
//     editor restores what the human actually typed (@refs and all), not a reconstruction of it.
//   - input is the parsed form the engine consumes; its @file references resolve at DELIVERY,
//     inside Agent.Interject, so the model reads a file as it stands then rather than as it
//     stood when the remark was typed.
type queuedInterjection struct {
	id    int
	raw   string
	input domain.UserInput
}

// interjectBox is the per-Exchange mailbox: the Update goroutine pushes staged rows into it and
// the worker goroutine drains them between Steps, which is the ONE place the two goroutines
// touch the same state. Everywhere else the split is clean — the Model owns the display rows,
// the worker owns the engine — so this mutex is the whole of the concurrency in the interjection
// path (the engine needs none: Agent.Interject is called at the between-Steps boundary, where the
// worker owns the conversation outright).
//
// It is held BY POINTER on the Model. The Model is value-copied on every Update (ADR 0011), and a
// sync.Mutex copied by value would hand each copy its own lock — silently unsynchronising the two
// goroutines rather than failing loudly (doc.go's no-copy invariant).
//
// A nil box is the "no worker to deliver to" state and is deliberately usable: push is a no-op and
// drainAll yields nothing, so staging a row while /compact runs (a worker that drives no Exchange
// and so takes no box) stages the display row without wedging it into a mailbox nobody empties.
// Such a row reaches the model through the terminal flush instead.
type interjectBox struct {
	mu    sync.Mutex
	items []queuedInterjection
}

// newInterjectBox returns an empty mailbox for one Exchange. A box is never reused across
// Exchanges: the worker that drains it is the only reader, and it dies with that worker.
func newInterjectBox() *interjectBox {
	return &interjectBox{}
}

// push stages it for delivery at the next between-Steps boundary. Called from the Update
// goroutine. A nil box drops the row silently — see the type doc: the display copy on the Model
// is what makes that safe, not luck.
func (b *interjectBox) push(it queuedInterjection) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items = append(b.items, it)
}

// withdraw takes one staged row back out of the mailbox before the worker can deliver it, and
// reports whether the row is still the Model's to take. Called from the Update goroutine — it is
// the mailbox half of the Backspace pop, which lifts the newest row back into the editor.
//
// A row already drained (its delivery is in the worker's hands, or has happened) is NOT withdrawn:
// false says so, and the caller leaves the row where it is rather than handing the human an editor
// copy of a message the model is about to read. A nil box holds nothing to race over — no worker is
// draining it — so the row is unambiguously the Model's and it reports true.
func (b *interjectBox) withdraw(id int) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, it := range b.items {
		if it.id != id {
			continue
		}
		b.items = append(b.items[:i], b.items[i+1:]...)
		return true
	}
	return false
}

// drainAll removes and returns every staged row, oldest first — FIFO, because the human wrote
// them in that order and the model should read them in it. Called from the worker goroutine.
// It returns nil when there is nothing staged (or the box is nil), which is the caller's cue to
// skip the delivery notification entirely.
//
// The drain is unconditional: rows leave the mailbox even if a delivery later in the batch fails.
// That is deliberate — the Model's display copy is the queue of record, and a row that did not
// land simply stays on it (reported by the delivery Msg naming only what DID land) and reaches the
// model through the terminal flush. A row is never silently lost, and never delivered twice.
func (b *interjectBox) drainAll() []queuedInterjection {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.items) == 0 {
		return nil
	}
	out := b.items
	b.items = nil
	return out
}

// ----------------------------------------------------------------------------
// Staging — the Update-goroutine half: type, queue, take back, and record on arrival
// ----------------------------------------------------------------------------

// commandsAtIdleNote is what a /command typed while the model works earns instead of a queue
// slot. Commands are idle-only by construction (runCommand drives the engine at a boundary the
// Update loop owns), so queueing one would only defer a refusal — and the human's line is left in
// the box, because they may well want to send it the moment the Exchange ends.
const commandsAtIdleNote = "commands run at idle — not queued"

// stageInterjection is what ⏎ does while a worker runs: it turns the editor's contents into a
// staged row and empties the box, launching nothing (the single-worker invariant is untouched —
// the running worker delivers the row at its next between-Steps boundary, ADR 0025).
//
// Three outcomes, and the input's fate is the difference between them: a /command is REFUSED with
// a note and the line is left exactly where it was (commandsAtIdleNote); a blank box stages
// nothing at all; anything else is queued and the editor is cleared, the same way a submit clears
// it. The row carries both halves — the verbatim editor text for the Backspace restore, and the
// parsed input the engine consumes, whose @file references deliberately stay unresolved until
// delivery, so the model reads the file as it stands then.
func (m Model) stageInterjection() (tea.Model, tea.Cmd) {
	parsed, _ := m.promptEditor.submitParse()
	if parsed.kind == kindCommand {
		m.transcript.addNote(commandsAtIdleNote)
		m.refreshViewport()
		return m, nil
	}
	if parsed.text == "" {
		return m, nil
	}
	m.interjectSeq++
	row := queuedInterjection{
		id:    m.interjectSeq,
		raw:   m.input.Value(),
		input: domain.UserInput{Text: parsed.text, FileRefs: parsed.fileRefs},
	}
	// The display copy and the mailbox are written together, which is what makes the two halves
	// reconcilable: the row exists for the human the moment it exists for the worker.
	m.pendingInterjections = append(m.pendingInterjections, row)
	m.box.push(row)
	m.promptEditor.reset()
	m.layout() // the emptied box shrinks back; the strip above it gains a row
	return m, nil
}

// popInterjection lifts the NEWEST staged row back into the editor, reporting whether it did. It
// is Backspace's meaning on an empty box: the same key that deletes the last character deletes the
// last thing queued, and hands it back editable rather than merely discarding it (latest-wins, the
// mirror of the oldest-first delivery order).
//
// It withdraws the row from the mailbox first and gives up if that fails: a row already drained is
// in the worker's hands — committed, or about to be — and handing the human an editor copy of a
// message the model has just read would invite sending it twice. The delivery report moves that
// row into the transcript a moment later, which is the honest answer to the keypress.
//
// It is deliberately confined to the states where the queue is the human's to edit — idle (a held
// queue) and running (a live one). At an approval or an ask the box belongs to the decision in
// front of it, and popping a queued message into the answer field would answer the wrong question.
func (m Model) popInterjection() (Model, bool) {
	if m.state != stateIdle && m.state != stateRunning {
		return m, false
	}
	n := len(m.pendingInterjections)
	if n == 0 {
		return m, false
	}
	row := m.pendingInterjections[n-1]
	if !m.box.withdraw(row.id) {
		return m, false
	}
	m.pendingInterjections = m.pendingInterjections[:n-1]
	m.input.SetValue(row.raw)
	m.input.MoveToEnd()
	m = m.recomputeAutocomplete() // the restored text may re-open the "@file" overlay it was typed with
	m.layout()                    // the box regrows around the restored text; the strip loses a row
	return m, true
}

// foldInterjected records the rows the worker COMMITTED (interjectedMsg) and takes them off the
// queue. Each becomes its own transcript block at this point in the scrollback — 1:1 with the
// messages the engine appended, in delivery order — so the transcript says what the model saw and
// when, rather than what was typed and when.
//
// Reconciliation is by id and only by id: a row the report does not name stays queued, which is
// what makes a refused delivery degrade to "held" instead of "lost". An empty report (a drain that
// delivered nothing) therefore correctly changes nothing at all.
func (m *Model) foldInterjected(items []queuedInterjection) {
	if len(items) == 0 {
		return
	}
	delivered := make(map[int]bool, len(items))
	for _, it := range items {
		delivered[it.id] = true
		m.transcript.addInterjected(it.input.Text)
	}
	kept := make([]queuedInterjection, 0, len(m.pendingInterjections))
	for _, row := range m.pendingInterjections {
		if !delivered[row.id] {
			kept = append(kept, row)
		}
	}
	m.pendingInterjections = kept
	m.refreshViewport()
}

// maxQueuedRows caps how many staged rows the strip above the input box shows at once. The strip
// steals its height from the transcript viewport (View's shrink accounting), so an unbounded queue
// would squeeze the conversation off the screen — the maxAutocompleteItems / maxInputRows posture.
// Past the cap the NEWEST rows are the ones shown, under a "… N more queued" marker: the row
// nearest the box is the one Backspace takes back, and the count says nothing was dropped.
const maxQueuedRows = 3

// renderPendingInterjections draws the staged rows shown directly above the input box — dim ⧖
// lines in delivery order (oldest first, newest nearest the box), in the same slot the
// attached-skill chips use. It returns "" when nothing is queued, so View treats it exactly like
// the chips and dropdown slots.
func (m Model) renderPendingInterjections() string {
	n := len(m.pendingInterjections)
	if n == 0 {
		return ""
	}
	shown := m.pendingInterjections
	rows := make([]string, 0, maxQueuedRows+1)
	if n > maxQueuedRows {
		shown = shown[n-maxQueuedRows:]
		rows = append(rows, m.queuedRow(fmt.Sprintf("… %d more queued", n-maxQueuedRows)))
	}
	for _, it := range shown {
		rows = append(rows, m.queuedRow(glyphInterject+" "+queuedRowText(it.raw)))
	}
	return strings.Join(rows, "\n")
}

// queuedRow renders one line of the staged-row strip: dim note styling, clipped ANSI-aware to the
// window so a long message never breaks the chrome's layout (the renderSkillChips posture).
func (m Model) queuedRow(text string) string {
	return m.th.noteText.Render(ansi.Truncate(text, max(1, m.width), "…"))
}

// queuedRowText flattens one staged row's raw text to a single line: the strip is chrome, not
// content, and a queued message may well be multi-line. The message itself is untouched — this is
// only how the waiting row is previewed.
func queuedRowText(raw string) string {
	return strings.Join(strings.Fields(raw), " ")
}

// queuedSegment is the status line's "N queued" readout, rendered whenever anything is waiting to
// go out — including at idle, where a queue held over from a stop or an error must keep saying so.
// afterPhrase asks for the " · " separator, for the states whose slot already carries words.
func (m Model) queuedSegment(afterPhrase bool) string {
	n := len(m.pendingInterjections)
	if n == 0 {
		return ""
	}
	text := fmt.Sprintf("%d queued", n)
	if afterPhrase {
		text = " · " + text
	}
	return m.th.statusBar.Render(text)
}

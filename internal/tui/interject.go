package tui

import (
	"fmt"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The interjection mailbox (ADR 0025)
// ----------------------------------------------------------------------------

// queuedInterjection is one message the human typed while the model was working, staged for
// delivery into the running Exchange. It carries four things because four different consumers
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
//   - skillSpans is where input.Text carries its "/tokens" ([skillSpan]) — display's half, kept
//     off domain.UserInput because the engine has no use for an offset (the wire-silent boundary,
//     ADR 0031) and captured HERE because a delivered row becomes a transcript block later, by
//     which time the parse that located them is long gone.
type queuedInterjection struct {
	id         int
	raw        string
	input      domain.UserInput
	skillSpans []skillSpan
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
// Such a row reaches the model at the terminal boundary instead — flushed into a new Exchange on a
// natural completion, held for the next ⏎ after a stop or a fault (flushAfterCompletion).
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
// land simply stays on it (reported by the delivery Msg naming only what DID land) and goes out at
// the terminal boundary instead (flushAfterCompletion). A row is never silently lost, and never
// delivered twice.
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

// commandsAtIdleNote is what an IDLE-ONLY /command typed while the model works earns instead of a
// queue slot: the verbs that drive the engine at a boundary the Update loop owns (/clear, /new,
// /sessions, /compact, /continue, /confine off|on) cannot run mid-Step, and queueing one would only
// defer a refusal. The human's line is left in the box, because they may well want to send it the
// moment the Exchange ends. The reporting verbs are not refused at all — they run right there
// (parsedInput.safeWhileRunning), which is why this note is no longer about "commands" as a class.
const commandsAtIdleNote = "commands run at idle — not queued"

// stageInterjection is what ⏎ does while a worker runs: it turns the editor's contents into a
// staged row and empties the box, launching nothing (the single-worker invariant is untouched —
// the running worker delivers the row at its next between-Steps boundary, ADR 0025).
//
// Five outcomes, and the input's fate is the difference between them: a REPORTING /command runs on
// the spot — /version, /skills, /confine status answer while the model works, because they touch no
// boundary (commandRunnable), and the box empties exactly as it does at idle since a whole-input
// invocation IS the command line; any other /command is REFUSED with a note and the line is left
// exactly where it was (refuseIdleOnlyCommand); a lone /word that names nothing is refused the same
// way, with the typo guard's note (refuseUnknownSlash — a mistyped invocation must no more be queued
// for the model than sent to it); a blank box stages nothing at all; anything else is queued and the
// editor is cleared, the same way a submit clears it. The row carries both halves — the verbatim
// editor text for the Backspace restore, and the parsed input the engine consumes, whose @file
// references deliberately stay unresolved until delivery, so the model reads the file as it stands
// then.
func (m Model) stageInterjection() (tea.Model, tea.Cmd) {
	// The line verbatim, before anything empties the box: an interjection is an input the human sent,
	// so it is recorded for ↑ exactly as a submitted message is (recall.go, decision 3). Read here for
	// submit's reason — only the paths that queue or run reach the record; the refusals below leave
	// the line standing and record nothing.
	sent := m.input.Value()
	var record tea.Cmd

	parsed := m.promptEditor.submitParse(m.knownSkillID)
	if parsed.kind == kindCommand {
		if !m.commandRunnable(parsed) {
			return m.refuseIdleOnlyCommand()
		}
		// Nothing else was typed (the whole-input command rule), so emptying the box is exactly
		// stripping the verb — submit's reading at idle, and runCommand touches the editor in neither.
		m.promptEditor.reset()
		if parsed.recallable() {
			// The same carve-out submit makes, so noRecall means "never recorded" rather than
			// "not recorded at idle". No verb reaches here carrying the flag today — /clear and
			// /new are both idle-only, so commandRunnable refuses them above — which is exactly
			// why the guard is written now: it costs nothing and it cannot be forgotten later.
			m, record = m.recordSend(sent)
		}
		next, cmd := m.runCommand(parsed)
		return next, tea.Batch(cmd, record)
	}
	if parsed.kind == kindUnknownSlash {
		return m.refuseUnknownSlash(parsed)
	}
	if parsed.text == "" {
		return m, nil
	}
	m.interjectSeq++
	row := queuedInterjection{
		id:  m.interjectSeq,
		raw: m.input.Value(),
		// The row carries the full parse — the skill /tokens included. A skill is message content
		// like an @ref, so it rides the interjection and is resolved at delivery (agent/interject.go
		// prepends the bodies exactly as the loop does at open).
		input:      domain.UserInput{Text: parsed.text, FileRefs: parsed.fileRefs, SkillIDs: parsed.skillIDs},
		skillSpans: parsed.skillSpans,
	}
	// The display copy and the mailbox are written together, which is what makes the two halves
	// reconcilable: the row exists for the human the moment it exists for the worker.
	m.pendingInterjections = append(m.pendingInterjections, row)
	m.box.push(row)
	m.promptEditor.reset()
	m, record = m.recordSend(sent) // the row is queued: the human sent this line, so ↑ hands it back
	m.layout()                     // the emptied box shrinks back; the strip above it gains a row
	return m, record
}

// popInterjection lifts the NEWEST staged row back into the editor, reporting whether it did — and
// carrying out the Cmd the restored draft owes, which is a skill-catalog re-scan when the text it put
// back leaves the caret in a "/" menu region (recomputeAutocomplete). It
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
func (m Model) popInterjection() (Model, tea.Cmd, bool) {
	if !m.state.live() {
		return m, nil, false
	}
	n := len(m.pendingInterjections)
	if n == 0 {
		return m, nil, false
	}
	row := m.pendingInterjections[n-1]
	if !m.box.withdraw(row.id) {
		return m, nil, false
	}
	m.pendingInterjections = m.pendingInterjections[:n-1]
	m.input.SetValue(row.raw)
	m.input.MoveToEnd()
	var reload tea.Cmd
	m, reload = m.recomputeAutocomplete() // the restored text may re-open the overlay it was typed with
	m.layout()                            // the box regrows around the restored text; the strip loses a row
	return m, reload, true
}

// foldInterjected records the rows the worker COMMITTED (interjectedMsg) and takes them off the
// queue. Each becomes its own transcript block at this point in the scrollback — 1:1 with the
// messages the engine appended, in delivery order — so the transcript says what the model saw and
// when, rather than what was typed and when.
//
// Reconciliation is by id and only by id: a row the report does not name stays queued, which is
// what makes a refused delivery degrade to "held" instead of "lost". An empty report (a drain that
// delivered nothing) therefore correctly changes nothing at all.
//
// A folded row is committed history from here on. If a stop later scraps the Exchange it was
// committed into (AbortExchange), the row dies with it exactly as every other message committed into
// that Exchange does — it does not come back to the queue, and the ⧖ transcript block is the
// surviving record of what the model read (sent is sent, owner ruling 2026-08-03).
func (m *Model) foldInterjected(items []queuedInterjection) {
	if len(items) == 0 {
		return
	}
	delivered := make(map[int]bool, len(items))
	for _, it := range items {
		delivered[it.id] = true
		m.transcript.addInterjected(it.input.Text, it.skillSpans)
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

// ----------------------------------------------------------------------------
// Flush and hold — what a terminal boundary does with whatever is still staged
// ----------------------------------------------------------------------------

// flushAfterCompletion is the terminal ruling for a NATURAL completion: an Exchange that reached
// its own quiescent boundary, or a compaction that landed. Rows still staged there have run out of
// boundaries to be delivered at — they were typed while the model wrote its final answer, or during
// a /compact that drives no Exchange at all — so they go out now, as one new Exchange, rather than
// waiting for a keypress the human has no reason to expect is needed (owner decision 1).
//
// done is the Cmd the terminal fold already produced (the idle session save, or tea.Quit), and it
// rides out alongside the launch. That ordering matters and is already established by the time
// this runs: saveAtIdle takes its engine Snapshot synchronously inside finishWorker, so the record
// it persists is the COMPLETED Exchange, not the one this flush is about to open.
//
// A deferred quit wins outright. finishWorker returns tea.Quit for a quit requested while busy, and
// launching a fresh Exchange into a program that is exiting would either be abandoned or delay the
// exit; staged rows are session-ephemeral by decision (ADR 0025), so the honest outcome is to leave.
func (m Model) flushAfterCompletion(done tea.Cmd) (tea.Model, tea.Cmd) {
	if m.quitting || len(m.pendingInterjections) == 0 {
		return m, done
	}
	next, cmd := m.flushInterjections()
	return next, tea.Batch(done, cmd)
}

// flushInterjections opens a new Exchange carrying everything still staged and empties the queue.
// It is the auto-send half of the contract; the idle ⏎ half lives in submit(), which joins the
// editor's own text in last and shares the same composition (joinedInterjections) and the same
// launch (launchExchange).
//
// It reads the editor NOT AT ALL. A completion can land while the human is mid-sentence, and a
// half-typed line is not something they asked to send — it stays in the box, and the staged rows
// (which they did press ⏎ on) are what goes.
func (m Model) flushInterjections() (tea.Model, tea.Cmd) {
	in, spans := m.joinedInterjections(parsedInput{})
	m.pendingInterjections = nil
	m.detached = false // the flushed prompt re-arms follow-the-tail, exactly as a typed one does
	m.transcript.addUser(in.Text, spans)
	m.layout() // the strip above the box loses its rows; the new prompt opens at the top
	return m.launchExchange(in)
}

// joinedInterjections composes the ONE unmarked user message a flush sends: the staged rows'
// texts oldest first — the order the human wrote them — with tail's text last when a ⏎ on a
// non-empty box is what triggered the flush, separated by blank lines. Their @file references and
// their skill references are unioned in the same order and de-duplicated, so a path — or a skill —
// named in two rows is resolved once.
//
// One message rather than several is the point: exactly one unmarked user message opens an
// Exchange, so the derived opening (internal/domain) stays trivially correct and every mechanism
// reading it sees the whole request as the shared context it is. Mid-run delivery is 1:1 for the
// opposite reason — there the Exchange is already open and each remark lands at a distinct
// boundary, which is what the transcript records.
//
// It joins each row's PARSED text, not the verbatim editor line kept for the Backspace pop: a row
// delivered mid-run sends exactly that text, and a row that merely happened to be flushed instead
// must not reach the model differently for it.
//
// It returns the display's half beside the engine's: the rows' skill spans RE-BASED onto the
// composition, since each row located its "/tokens" in its own text and the block this composes is
// one message. The offsets are byte counts of the joined string — every seated text plus the
// separator that follows it — which is why the join runs through interjectionJoin rather than a
// literal here and a literal there.
func (m Model) joinedInterjections(tail parsedInput) (domain.UserInput, []skillSpan) {
	texts := make([]string, 0, len(m.pendingInterjections)+1)
	var refs, ids []string
	var spans []skillSpan
	offset := 0 // where the NEXT seated text will begin in the joined string
	seenRef, seenID := make(map[string]bool), make(map[string]bool)
	add := func(text string, fileRefs, skillIDs []string, skillSpans []skillSpan) {
		if text != "" {
			for _, sp := range skillSpans {
				spans = append(spans, skillSpan{start: sp.start + offset, end: sp.end + offset})
			}
			offset += len(text) + len(interjectionJoin)
			texts = append(texts, text)
		}
		for _, ref := range fileRefs {
			if !seenRef[ref] {
				seenRef[ref] = true
				refs = append(refs, ref)
			}
		}
		for _, id := range skillIDs {
			if !seenID[id] {
				seenID[id] = true
				ids = append(ids, id)
			}
		}
	}
	for _, row := range m.pendingInterjections {
		add(row.input.Text, row.input.FileRefs, row.input.SkillIDs, row.skillSpans)
	}
	add(tail.text, tail.fileRefs, tail.skillIDs, tail.skillSpans)
	return domain.UserInput{Text: strings.Join(texts, interjectionJoin), FileRefs: refs, SkillIDs: ids}, spans
}

// interjectionJoin separates the staged rows a flush composes into one message: a blank line, so
// several remarks read as several paragraphs rather than one run-on. It is a named constant because
// the span re-basing counts its bytes (joinedInterjections) — a separator changed in one of the two
// places and not the other would slide every accent after the first row.
const interjectionJoin = "\n\n"

// noteHeldQueue records that a stop or a loop error left the queue standing. It is called from the
// two terminal folds that do NOT flush, so it fires exactly once per hold — at the transition into
// it, never on the keypresses that follow — and says nothing at all when nothing is staged.
func (m *Model) noteHeldQueue() {
	if n := len(m.pendingInterjections); n > 0 {
		m.transcript.addNote(heldNote(n))
	}
}

// heldNote words the hold. Esc stops EVERYTHING, including what was waiting to go out (owner
// decision 2), so the note has two jobs: say that nothing was sent, and say what will send it. The
// count is the same one the status line carries, and the ⏎ it names is the ordinary send key on a
// box the human may leave empty.
func heldNote(n int) string {
	if n == 1 {
		return "1 queued message held — ⏎ sends it"
	}
	return fmt.Sprintf("%d queued messages held — ⏎ sends them", n)
}

// maxQueuedRows caps how many staged rows the strip above the input box shows at once — the band's
// own TASTE, the way maxAutocompleteItems is the dropdown's. The screen's answer to it is
// [Model.frameRowPlan], which the band shares with every open pane: the strip steals its height
// from the transcript viewport, so an unbounded queue — or a capped one on a short window — would
// squeeze the conversation off the screen and then the input box off the frame. Past whichever
// bound binds first the NEWEST rows are the ones shown, under a "… N more queued" marker: the row
// nearest the box is the one Backspace takes back, and the count says nothing was dropped. The cap
// counts CONTENT rows only, so at the cap the band is six rows on screen — the cap, the overflow
// marker, and the two blank rows framing the group — which is its worst case and not its promise:
// the frame's grant cuts it further wherever the window cannot pay for six.
const maxQueuedRows = 3

// bandFrame is the band's two blank framing rows, one above the group and one below. They belong to
// the band (layout.md), so they are part of what it spends its row grant on.
const bandFrame = 2

// bandPlan is the staged band's share of the frame's rows ([Model.frameRowPlan]): the staged rows
// it draws and the rows it is holding back, which its one "… N more queued" marker states.
type bandPlan struct {
	shown  int
	hidden int
}

// height is the rows the band occupies: its two framing rows, the staged rows it seats, and the
// marker row when it is holding any back. A band with nothing to show and nothing to count is not
// drawn at all — no rows, and no frame either.
func (b bandPlan) height() int {
	if b.shown == 0 && b.hidden == 0 {
		return 0
	}
	h := bandFrame + b.shown
	if b.hidden > 0 {
		h++
	}
	return h
}

// bandShape spends a row budget on n staged messages. The NEWEST rows are the ones kept, capped by
// the band's own taste (maxQueuedRows) and then by the budget; whatever is left over is counted on a
// marker row, which costs one row more than the content it describes — so the marker outranks the
// rows it is describing, and a grant that seats one row spends it on the count rather than on one of
// five. Under three rows there is no honest band left (its frame, one row and a marker do not fit),
// so it is not drawn at all and the status line's "N queued" readout carries the count instead.
func bandShape(n, budget int) bandPlan {
	if n <= 0 || budget < bandFrame+1 {
		return bandPlan{}
	}
	shown := min(n, maxQueuedRows, budget-bandFrame)
	if shown < n {
		shown = max(0, min(shown, budget-bandFrame-1)) // the marker takes a row of the band's own
	}
	return bandPlan{shown: shown, hidden: n - shown}
}

// renderPendingInterjections draws the staged rows shown directly above the input box, as one band:
// faint ⧖ lines in delivery order (oldest first, newest nearest the box), each indented into the
// body column and painted edge to edge on black, framed by one blank band row above and one below
// so the group separates from the chrome it sits between. It returns "" when nothing is queued —
// no queue, no band, no frame — so View treats it exactly like the dropdown slot.
//
// How many of those rows it draws is the FRAME's call, not the queue's (bandShape): the band is one
// of the surfaces sharing the viewport with whatever pane is open, and on a window that cannot seat
// both it is the band that gives way — a passive reminder, against a pane the human is acting on.
// Rows the budget drops are counted in the very same "… N more queued" marker the cap's are, one
// wording for one fact; and a window with no rows at all for the band leaves the count to the status
// line's "N queued" readout, which is in every frame the band could have been in — and which is
// composed TO the window's width so that a narrow one sheds the activity phrase around the count
// rather than the count off the end of the row ([Model.statusLeft]).
func (m Model) renderPendingInterjections() string {
	if len(m.pendingInterjections) == 0 {
		return ""
	}
	band := m.frameRowPlan(m.openPanes()).band
	if band.height() == 0 {
		return ""
	}
	rows := make([]string, 0, band.height())
	rows = append(rows, m.queuedRow(""))
	if band.hidden > 0 {
		rows = append(rows, m.queuedRow(bodyIndent+fmt.Sprintf("… %d more queued", band.hidden)))
	}
	for _, it := range m.pendingInterjections[len(m.pendingInterjections)-band.shown:] {
		rows = append(rows, m.queuedRow(bodyIndent+glyphInterject+" "+queuedRowText(it.raw)))
	}
	rows = append(rows, m.queuedRow(""))
	return strings.Join(rows, "\n")
}

// queuedRow renders one line of the staged-row band: clipped ANSI-aware to the window so a long
// message never breaks the chrome's layout (the statusLine posture), then padded with spaces
// back out to that same width and styled as a whole. Text, indent, and pad alike therefore carry
// the black background, so the row reads as one solid bar instead of leaving the terminal's own
// background showing past the text — the statusLine posture. A "" text renders a blank band row.
//
// Both the clip and the pad go through m.th.measure, the package's width authority (width.go),
// because this row is composed TO the window width and then painted: measuring it in a method the
// painter is not on puts the bar's right edge a cell off the frame's on any row carrying VARIATION
// SELECTOR-16, and cutting in a different method than it measured in is that same defect one step
// later (ADR 0030 §3).
func (m Model) queuedRow(text string) string {
	w := max(1, m.width)
	line := m.th.measure.Truncate(text, w, "…")
	if pad := w - m.th.measure.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return m.th.queuedText.Render(line)
}

// queuedRowText flattens one staged row's raw text to a single line: the band is chrome, not
// content, and a queued message may well be multi-line — one row per staged message is what the
// cap counts. The message itself is untouched — this is only how the waiting row is previewed.
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

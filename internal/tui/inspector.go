package tui

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// /inspect — the raw-protocol pane (command.go owns the verb)
// ----------------------------------------------------------------------------
//
// What the engine actually PUT ON THE WIRE, and what came back: the marshalled request body of a
// model call and the response payload it was answered with, as the provider built and read them
// (domain.WireEvent). It is the view for the question the transcript cannot answer — the model
// behaved in a way the rendered conversation does not explain, and the only thing that settles it
// is the bytes.
//
// It is ARMED, and off by default: the engine captures nothing unless `ui.inspector` says so, read
// once at start-up (internal/config, ADR 0035). With the key off this file still runs — the fold is
// a type switch that never matches, the ring stays empty — and `/inspect` opens on a single row
// naming the key rather than on an empty pane, because "nothing is here" and "nothing is being
// recorded" are different answers and only one of them is actionable.
//
// The records live in a bounded ring on the Model, BESIDE the transcript and never in it: a wire
// record is not a conversation entry, it says nothing the reader of a transcript asked for, and
// folding one in would disturb the entry pairing the transcript is built on (fold.go's WireEvent
// row states the same rule from the other side). The ring survives /clear for the same reason
// transcript.debug does — it is a diagnostic view of the RUN, not of the session's memory.
//
// The pane is the /usage report's shape: non-modal, scrollable, esc and the four scroll keys its
// whole keyboard, rows derived at render time from the ring. Its rows are FLAT — one payload line
// per row, elided at the pane's width like every other unwrapped row (popupRowBlocks) — rather
// than wrapped: a single JSON line longer than the pane's whole row budget seats NOTHING when rows
// wrap (popupRowWindowFrom), and a raw-protocol view that goes blank on a big request body is
// worse than one that cuts a long line off at the border.

// wireRecord is one half of one Upstream round-trip as the Inspector holds it: which half, the
// Turn and depth of the agent that made the call, and the payload — escape-stripped and
// pretty-printed ONCE, when the event was folded.
//
// The lines are kept formatted rather than raw for one reason: the pane re-derives its rows on
// every frame, and a frame is painted for every streamed token, so parsing twenty JSON bodies per
// repaint would put the Inspector's cost on a hot path it has no business being on. It bounds
// memory as a side effect — hidden says how many lines the cap dropped, so the pane can state the
// cut rather than make it silently.
type wireRecord struct {
	direction string
	turn      int
	depth     int
	lines     []string
	hidden    int
}

// inspectorPane is the /inspect overlay's state: whether it is up, and how far its row list is
// scrolled. The rows themselves are derived at render time from the ring, so there is nothing here
// to keep in step with them. Its zero value is "closed at the top", so it lives inline in the
// value-copied Model like the usage report (ADR 0011).
type inspectorPane struct {
	open bool
	top  int // the first row the window shows (popupSpec.rowTop) — what the scroll keys move
}

// inspectorTitle names the pane, and inspectorHint spells the keys it owns.
const (
	inspectorTitle = "raw wire traffic"
	inspectorHint  = "↑/↓ scroll · esc close"
)

// maxWireRecords is the ring's whole bound: the most recent twenty half-round-trips. A request
// body repeats the entire conversation, so the ring is deliberately short — twenty covers the tail
// of a debugging session (a handful of calls, both directions) without holding a session's worth
// of prompts in memory for a pane that is closed almost all of the time.
const maxWireRecords = 20

// maxWireRecordLines is how much of ONE record the ring keeps. A tool-carrying request body runs
// to a few hundred pretty-printed lines and a pane shows a dozen at a time, so a cap here bounds
// both the memory the ring holds and the rows a frame composes, and what it costs is a tail the
// reader would have had to page a hundred windows to reach. The cut is never silent: the dropped
// count goes on the record and the pane spells it in the package's one elision phrase.
const maxWireRecordLines = 100

// maxInspectorRows is the pane's own taste for how many rows it shows at once — a little taller
// than the /usage report, because these rows are payload and a JSON object read three lines at a
// time is not read at all. [Model.popupBudget] cuts it down to what the window can seat.
const maxInspectorRows = 16

// inspectorDisarmedRow is what the pane says when nothing has been captured and the key that
// captures is off: the one actionable sentence, naming the key and saying when a change to it
// bites (arming is read at start-up — internal/config's `ui.inspector`).
const inspectorDisarmedRow = "capture is off — set ui.inspector: true, then restart"

// inspectorEmptyRow is the same slot with the key ON: the capture is armed and no model call has
// been made yet, which is a wait rather than a thing to fix.
const inspectorEmptyRow = "armed — the next model call lands here"

// runInspectCommand drives the /inspect verb: it opens the pane and does nothing else.
// Synchronous like /usage — no engine call, no worker, no I/O — and safe while a worker works, for
// the same reason and a stronger one: everything it shows is already on the Model, and a request
// the human wants to READ is one the agent has just sent.
//
// It opens at the END of the list. The rows are newest-last (a log's order), so the record worth
// reading is the last one, and a pane that opened on the oldest of twenty bodies would ask for a
// hundred page-downs before it said anything. The top is set past the last row and CLAMPED to the
// last full window when the pane is composed (inspectorSpec) — the window is the frame's answer for
// this paint, not something this verb can know.
func (m Model) runInspectCommand() (tea.Model, tea.Cmd) {
	rows, _ := m.inspectorRows()
	m.inspector = inspectorPane{open: true, top: len(rows)}
	m.layout()
	return m, nil
}

// foldWire files one Event into the Inspector's ring, and folds nothing else: a WireEvent is
// recorded, every other variant passes through untouched. It is called from foldEvent like every
// other fold (fold.go) and it is the ONLY writer of the ring.
//
// The payload crosses stripEscapes on the way in — it is the least trusted string in the program,
// being bytes an upstream server sent — so no ESC byte from the wire can reach the terminal
// through this pane, on the same terms the transcript takes model text on (transcript.go).
//
// The slice is REBUILT rather than appended into, the settingEdits idiom: the Model is copied by
// value on every Update (ADR 0011), and a ring that shared a backing array with a copy of itself
// would let one frame's fold overwrite a row another frame is drawing.
func (m Model) foldWire(e domain.Event) Model {
	we, ok := e.(domain.WireEvent)
	if !ok {
		return m
	}
	lines, hidden := wirePayloadLines(stripEscapes(we.Payload))
	keep := m.wire
	if len(keep) >= maxWireRecords {
		keep = keep[len(keep)-maxWireRecords+1:]
	}
	next := make([]wireRecord, 0, len(keep)+1)
	next = append(next, keep...)
	m.wire = append(next, wireRecord{
		direction: we.Direction,
		turn:      we.Turn,
		depth:     we.Depth,
		lines:     lines,
		hidden:    hidden,
	})
	return m
}

// wirePayloadLines formats one payload for the ring: every line of it pretty-printed where it is
// JSON and left exactly as it arrived where it is not, capped at maxWireRecordLines with the
// dropped count reported.
//
// It is line-wise rather than whole-payload because the two directions have different shapes and
// both are honest: a request is ONE marshalled body, while a response is the stream's raw `data:`
// payloads newline-joined (domain.WireEvent), each its own JSON document, plus whatever sentinel
// the server closed with. Indenting each line independently is what shows both as the protocol
// rather than as one malformed blob.
func wirePayloadLines(payload string) (lines []string, hidden int) {
	trimmed := strings.Trim(payload, "\n")
	if trimmed == "" {
		return nil, 0
	}
	for _, raw := range strings.Split(trimmed, "\n") {
		lines = append(lines, prettyWireLine(raw)...)
	}
	if len(lines) > maxWireRecordLines {
		hidden = len(lines) - maxWireRecordLines
		lines = lines[:maxWireRecordLines]
	}
	return lines, hidden
}

// prettyWireLine expands one line into its indented JSON form, or hands it straight back when it is
// not JSON at all — an SSE sentinel, a plain-text error body, a truncated stream. Nothing here may
// fail: this is a VIEW of what a server said, and a server that said something unparseable is
// exactly what the reader opened the pane to see.
func prettyWireLine(line string) []string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return []string{""}
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(trimmed), "", "  "); err != nil {
		return []string{line}
	}
	return strings.Split(buf.String(), "\n")
}

// inspectorKey is the pane's whole key contract: esc closes it, ↑/↓ scroll a row at a time and
// pgup/pgdown a drawn window at a time. handled is false for every other key — the pane is NOT
// modal (usage.go states the doctrine), so the box behind it stays live and a printable key opens a
// message exactly as it would with no pane up.
//
// The arithmetic reads the window the pane was COMPOSED with rather than a second derivation of it:
// the rows are flat, so the budget popupBudget granted is the row count on the screen, and the
// clamped top is the one the painter will use — which is what lets the first ↑ after an open land
// one row above the last window instead of somewhere in the middle of the ring.
func (m Model) inspectorKey(msg tea.KeyPressMsg) (bool, tea.Model, tea.Cmd) {
	if !m.inspector.open {
		return false, m, nil
	}
	key := msg.String()
	if key == "esc" {
		return true, m.dismissInspector(), nil
	}
	// The scroll vocabulary is the /usage report's own table (usageScrollStep), spent rather than
	// copied: the two panes answer to the same four keys with the same two step sizes, and a second
	// table would be a place for them to drift apart one key at a time.
	step, byPage, scrolls := usageScrollStep(key)
	if !scrolls {
		return false, m, nil
	}
	spec, seated := m.inspectorSpec()
	if !seated {
		return false, m, nil // nothing of the pane is on the screen for a key to move
	}
	shown := spec.maxRows
	if byPage {
		step *= max(1, shown)
	}
	m.inspector.top = clampInt(spec.rowTop+step, 0, max(0, len(spec.rows)-shown))
	return true, m, nil
}

// dismissInspector takes the pane off the frame and gives its rows back to the transcript. The
// scroll goes with it: the next /inspect opens on the newest record again, which is where the
// question is asked from.
func (m Model) dismissInspector() Model {
	m.inspector = inspectorPane{}
	m.layout()
	return m
}

// renderInspector paints the pane, or "" when it is closed or the frame cannot seat it.
func (m Model) renderInspector() string {
	if !m.inspector.open {
		return ""
	}
	spec, seated := m.inspectorSpec()
	if !seated {
		return "" // the frame cannot seat this pane beside its siblings (frameRowPlan)
	}
	return renderPopup(m.th, spec, m.width)
}

// inspectorSpec composes the pane's [popupSpec] for THIS frame — the rows, the budget the frame
// granted and the window the scroll landed on. ok is false when the frame cannot seat the pane at
// all. It is a step of its own because the painter is not its only reader: the KEYS are budgeted
// against the very window that was drawn (inspectorKey), rather than against a second guess at it.
func (m Model) inspectorSpec() (popupSpec, bool) {
	rows, kinds := m.inspectorRows()
	maxBody, shown, seated := m.popupBudget(paneInspector, len(rows), maxInspectorRows, popupChrome, popupFloor{})
	if !seated {
		return popupSpec{}, false
	}
	return popupSpec{
		title:       inspectorTitle,
		maxBodyRows: maxBody,
		rows:        rows,
		rowKinds:    kinds,
		selected:    -1, // a report has no selection: nothing here is chosen (the popup module's convention)
		// Clamped to the LAST full window rather than to the last row, the /usage rule: it is what
		// lands an opening /inspect on the newest record (runInspectCommand sets the top past the end),
		// and it corrects a stale offset — the grant shrank, or a record arrived — rather than painting
		// one row over an empty pane.
		rowTop:    clampInt(m.inspector.top, 0, max(0, len(rows)-shown)),
		hint:      inspectorHint,
		maxRows:   shown,
		scrollbar: m.popupScrollbarOn(),
	}, true
}

// inspectorRows composes the report: for each record in the ring, oldest first, a header row naming
// the direction and the agent that made the call, then the payload's lines, then — where the cap
// cut one — the elision the package words every hidden-lines statement with. An empty ring is ONE
// row, and which one depends on whether anything is being captured at all.
//
// The kinds are composed in the same pass rather than derived from the rows afterwards: a header is
// a header because of where it was put, and a payload line that happened to look like one would be
// styled as a section label by any rule read back off the text.
func (m Model) inspectorRows() ([]popupRow, []popupRowKind) {
	if len(m.wire) == 0 {
		row := inspectorEmptyRow
		if !m.opts.Inspector {
			row = inspectorDisarmedRow
		}
		return []popupRow{{row}}, []popupRowKind{popupRowPlain}
	}
	rows := make([]popupRow, 0, len(m.wire)*2)
	kinds := make([]popupRowKind, 0, len(m.wire)*2)
	for _, rec := range m.wire {
		rows = append(rows, popupRow{wireRecordHeader(rec)})
		kinds = append(kinds, popupRowHeading)
		for _, line := range rec.lines {
			rows = append(rows, popupRow{line})
			kinds = append(kinds, popupRowPlain)
		}
		if rec.hidden > 0 {
			rows = append(rows, popupRow{popupElisionMarker(rec.hidden)})
			kinds = append(kinds, popupRowPlain)
		}
	}
	return rows, kinds
}

// wireRecordHeader names one record: which half of the round-trip it is and which Turn made it,
// plus the depth of a delegated run — absent at depth 0, so an undelegated session's headers carry
// no column that is always the same number.
func wireRecordHeader(rec wireRecord) string {
	head := rec.direction + " · turn " + strconv.Itoa(rec.turn)
	if rec.depth > 0 {
		head += " · depth " + strconv.Itoa(rec.depth)
	}
	return head
}

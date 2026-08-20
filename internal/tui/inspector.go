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
// The pane is the /usage report's shape because it IS that shape: both are reportPanes
// (reportpane.go), non-modal and scrollable, esc and the four scroll keys their whole keyboard, rows
// derived at render time — from the ring here, from the folds there. What is written in this file is
// the ring and what it says; the entries below are the shared module's functions under this pane's
// own name.
//
// Its rows are FLAT — one payload line per row, elided at the pane's width like every other
// unwrapped row (popupRowBlocks) — rather than wrapped: a single JSON line longer than the pane's
// whole row budget seats NOTHING when rows wrap (popupRowWindowFrom), and a raw-protocol view that
// goes blank on a big request body is worse than one that cuts a long line off at the border.

// wireRecord is one half of one Upstream round-trip as the Inspector holds it: which half, the
// Turn, depth and spawning call id of the agent that made the call, and the payload —
// escape-stripped and pretty-printed ONCE, when the event was folded.
//
// depth and callID together name the WIRE STREAM the half belongs to (domain.EventBase): a
// fan-out interleaves runs in one ring, and the call id is what separates two siblings a shared
// depth would braid. Turn orders the halves inside a stream; it does not identify one.
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
	callID    string
	lines     []string
	hidden    int
}

// inspectorPane is the /inspect overlay's state — a reportPane (reportpane.go) under the name of the
// pane that keeps it: whether the pane is up, and how far its record list is scrolled.
type inspectorPane = reportPane

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

// inspectorNoReplyRow is what stands under a request the ring holds no answer for once a later
// record of the SAME wire stream has landed (hasUnrecordedReply): the reply was not RECORDED, which
// is a different fact from the reply not arriving. A non-streaming success body is decoded straight off the connection and never captured
// (the provider's pinned design, internal/provider/wire.go), so a gap here is the recorder's
// silence and not the Upstream's — and a flat log that left the reader to infer it from absence
// reads as a lost response every time the stream was off.
const inspectorNoReplyRow = "· no response recorded — a non-streaming reply is decoded off the connection"

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
		callID:    we.CallID,
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

// The report module's functions under this pane's name (reportpane.go). Each one is the shared body
// with inspectReport filled in: naming them here is what lets the frame, the keyboard and the pointer
// go on addressing the /inspect pane as itself while there is only one report left to maintain.

// renderInspector paints the pane, or "" when it is closed or the frame cannot seat it.
func (m Model) renderInspector() string { return m.renderReport(inspectReport) }

// inspectorSpec composes the pane's [popupSpec] for THIS frame — its rows, the budget the frame
// granted and the window the scroll landed on ([Model.reportSpec]).
func (m Model) inspectorSpec() (popupSpec, bool) {
	return m.reportSpec(inspectReport, inspectContent(m.inspectorRows()))
}

// inspectorKey is the pane's whole key contract: esc closes it, ↑/↓ scroll a row at a time and
// pgup/pgdown a drawn window at a time (reportKey).
func (m Model) inspectorKey(msg tea.KeyPressMsg) (bool, tea.Model, tea.Cmd) {
	return m.reportKey(inspectReport, msg)
}

// dismissInspector takes the pane off the frame and gives its rows back to the transcript. The scroll
// goes with it: the next /inspect opens on the newest record again, which is where the question is
// asked from.
func (m Model) dismissInspector() Model { return m.dismissReport(inspectReport) }

// inspectorPaneRect is where the open pane is drawn: the screen row its top border lands on and how
// many rows it takes.
func (m Model) inspectorPaneRect() (y0, h int, ok bool) { return m.reportPaneRect(inspectReport) }

// inspectorWindow is the row window the pane is showing as the frame DREW it.
func (m Model) inspectorWindow() (reportWindow, bool) { return m.reportWindow(inspectReport) }

// handleInspectorClick answers a left-click while the pane is up: inside the box it is claimed and
// nothing happens, outside it the pane is dismissed and the click goes on.
func (m Model) handleInspectorClick(pre Model, msg tea.MouseClickMsg) (Model, bool) {
	return m.handleReportClick(inspectReport, pre, msg)
}

// inspectorWheel scrolls the record list one row per notch while the pointer is over it.
func (m Model) inspectorWheel(msg tea.MouseWheelMsg) (Model, bool) {
	return m.reportWheel(inspectReport, msg)
}

// inspectContent is what the pane tells the shared module about itself for one frame: its name, the
// keys it spells, how tall it likes to be, and the record rows with the kinds composed beside them.
// It words no empty state of its own — an empty ring is a ROW here, and which one depends on whether
// anything is being captured at all (inspectorRows).
func inspectContent(rows []popupRow, kinds []popupRowKind) reportContent {
	return reportContent{
		title:  inspectorTitle,
		hint:   inspectorHint,
		rowCap: maxInspectorRows,
		rows:   rows,
		kinds:  kinds,
	}
}

// inspectorRows composes the report: for each record in the ring, oldest first, a header row naming
// the direction and the agent that made the call, then the payload's lines, then — where the cap
// cut one — the elision the package words every hidden-lines statement with, then — where the ring
// went on without recording the answer — the note that says so (hasUnrecordedReply). An empty ring
// is ONE row, and which one depends on whether anything is being captured at all.
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
	for i, rec := range m.wire {
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
		if hasUnrecordedReply(m.wire, i) {
			rows = append(rows, popupRow{inspectorNoReplyRow})
			kinds = append(kinds, popupRowPlain)
		}
	}
	return rows, kinds
}

// hasUnrecordedReply says whether the record at index i is a request the ring will never show an
// answer for. The successor rule that settles it applies WITHIN one wire stream — the pair
// (depth, callID), the run identity every event carries (domain.EventBase) — and never across the
// whole ring: the ring is filled in arrival order by the one writer (foldWire), so a fan-out
// interleaves runs and the half that merely follows a request may belong to a sibling and say
// nothing about it. Inside a stream the halves stay in round-trip order, so the record that
// follows a request THERE is its own answer or nothing.
//
// A request its stream has not gone past never qualifies: its call may still be in flight, and a
// pane that called a live request unanswered would be wrong for exactly as long as the answer
// took. Turn is no part of the key — it orders records inside a stream, and two concurrent runs
// share it.
//
// Accepted residual: UNROUTED concurrent sub-agents speak over their parent's connection, whose
// tap is bound to the parent (internal/agent/construct.go, internal/agent/subagent.go), so their
// records carry the parent's (depth, callID) and braid into one stream — the note can still land
// under the wrong request of such a pair. No field on the event separates them; a ROUTED spawn
// (ADR 0045) builds its own client and tap and is separated.
func hasUnrecordedReply(records []wireRecord, i int) bool {
	rec := records[i]
	if rec.direction != domain.WireDirectionRequest {
		return false
	}
	for _, next := range records[i+1:] {
		if next.depth != rec.depth || next.callID != rec.callID {
			continue
		}
		return next.direction != domain.WireDirectionResponse
	}
	return false
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

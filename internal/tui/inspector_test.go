package tui

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// /inspect — the raw-protocol pane and its ring (inspector.go)
// ----------------------------------------------------------------------------

// wireEvent is one captured half of an Upstream round-trip, as the engine reports it — from the
// top-level agent, which was spawned by no call and so carries no call id.
func wireEvent(direction, payload string, turn, depth int) domain.WireEvent {
	return wireEventOfCall(direction, payload, turn, depth, "")
}

// wireEventOfCall is the same half stamped with the spawning call id of the agent that emitted it:
// the other half of the (depth, callID) wire stream the Inspector pairs a request to its reply
// within.
func wireEventOfCall(direction, payload string, turn, depth int, callID string) domain.WireEvent {
	return domain.WireEvent{
		EventBase: domain.EventBase{Turn: turn, Depth: depth, CallID: callID},
		Direction: direction,
		Payload:   payload,
	}
}

// inspectorModel folds the given events into a fresh model with the capture armed and the pane
// open — the state every assertion below is about.
func inspectorModel(t *testing.T, events ...domain.Event) Model {
	t.Helper()
	m := newTestModel(t)
	m.opts.Inspector = true
	for _, e := range events {
		m = m.foldEvent(e)
	}
	m.inspector = inspectorPane{open: true}
	return m
}

// TestWireRingKeepsTheLatestRecordsInOrder pins the bound: the ring holds the most recent
// maxWireRecords halves of a round-trip and drops the oldest, in arrival order — the tail of a
// debugging session, never the head of one.
func TestWireRingKeepsTheLatestRecordsInOrder(t *testing.T) {
	m := newTestModel(t)
	const sent = maxWireRecords + 5
	for i := range sent {
		m = m.foldEvent(wireEvent(domain.WireDirectionRequest, `{"n":`+strconv.Itoa(i)+`}`, i, 0))
	}

	if len(m.wire) != maxWireRecords {
		t.Fatalf("ring holds %d records, want the %d most recent", len(m.wire), maxWireRecords)
	}
	for i, rec := range m.wire {
		if want := sent - maxWireRecords + i; rec.turn != want {
			t.Errorf("record %d is turn %d, want %d — the ring dropped the wrong end or reordered", i, rec.turn, want)
		}
	}
}

// TestWireFoldDisturbsNothingElse is the rule the ring exists to keep: a wire record is not a
// conversation entry, so folding one appends no transcript entry, moves no gauge and says nothing
// on the status line. (fold_test.go's WireEvent row states the same fact for the fold table; this
// one holds it while the ring itself is filling.)
func TestWireFoldDisturbsNothingElse(t *testing.T) {
	m := newTestModel(t)
	before := len(m.transcript.entries)

	m = m.foldEvent(wireEvent(domain.WireDirectionResponse, `{"choices":[]}`, 1, 0))

	if got := len(m.transcript.entries); got != before {
		t.Errorf("transcript grew from %d to %d entries; a wire record is not an entry", before, got)
	}
	if len(m.wire) != 1 {
		t.Fatalf("ring holds %d records, want the one that was folded", len(m.wire))
	}
}

// TestWirePayloadReachesThePaneStrippedAndPretty pins both halves of what the fold does to a
// payload: the ESC bytes an upstream server can put in it never reach the ring (and so never reach
// the terminal), and the JSON is expanded once, at fold time, so the pane never parses on a repaint.
func TestWirePayloadReachesThePaneStrippedAndPretty(t *testing.T) {
	m := inspectorModel(t, wireEvent(domain.WireDirectionRequest, "{\"model\":\"\x1b[31mred\x1b[0m\"}", 1, 0))

	rec := m.wire[0]
	joined := strings.Join(rec.lines, "\n")
	if strings.ContainsRune(joined, 0x1b) {
		t.Errorf("an ESC byte survived into the ring: %q", joined)
	}
	if len(rec.lines) < 3 {
		t.Errorf("payload lines = %q, want the body expanded onto its own lines", rec.lines)
	}
	if pane := strip(m.renderInspector()); !strings.Contains(pane, `"model"`) {
		t.Errorf("the pane does not show the request body:\n%s", pane)
	}
}

// TestWirePayloadKeepsANonJSONLineAsItArrived is the other half of the same rule: a stream's
// sentinel, or an error body that is not JSON at all, is shown exactly as the server sent it. A
// raw-protocol view that hid what it could not parse would hide the very thing it was opened for.
func TestWirePayloadKeepsANonJSONLineAsItArrived(t *testing.T) {
	lines, hidden := wirePayloadLines("{\"a\":1}\n[DONE]")

	if hidden != 0 {
		t.Errorf("hidden = %d, want nothing dropped from a two-line payload", hidden)
	}
	if got := lines[len(lines)-1]; got != "[DONE]" {
		t.Errorf("last line = %q, want the sentinel verbatim", got)
	}
}

// TestWireRecordCapsItsLinesAndSaysSo pins the per-record cap: a body past maxWireRecordLines keeps
// its head and reports the count it dropped, in the package's one elision phrase — the cut is
// stated on the pane rather than made silently.
func TestWireRecordCapsItsLinesAndSaysSo(t *testing.T) {
	payload := "{\"a\":[" + strings.Repeat("1,", maxWireRecordLines) + "1]}"
	m := inspectorModel(t, wireEvent(domain.WireDirectionRequest, payload, 1, 0))

	rec := m.wire[0]
	if len(rec.lines) != maxWireRecordLines {
		t.Fatalf("record kept %d lines, want the cap of %d", len(rec.lines), maxWireRecordLines)
	}
	if rec.hidden <= 0 {
		t.Fatalf("hidden = %d, want the dropped lines counted", rec.hidden)
	}
	rows, _ := m.inspectorRows()
	last := rows[len(rows)-1][0]
	if want := popupElisionMarker(rec.hidden); last != want {
		t.Errorf("last row = %q, want the elision marker %q", last, want)
	}
}

// TestInspectorRowsHeadEveryRecord pins what the pane is made of: one heading row per record naming
// the direction, the turn and — for a delegated run only — the depth, with the payload's lines
// under it, oldest first.
func TestInspectorRowsHeadEveryRecord(t *testing.T) {
	m := inspectorModel(t,
		wireEvent(domain.WireDirectionRequest, `{"a":1}`, 2, 0),
		wireEvent(domain.WireDirectionResponse, `{"b":2}`, 2, 1),
	)

	rows, kinds := m.inspectorRows()
	if len(rows) != len(kinds) {
		t.Fatalf("%d rows against %d kinds — the two are composed in one pass", len(rows), len(kinds))
	}
	var headings []string
	for i, row := range rows {
		if kinds[i] == popupRowHeading {
			headings = append(headings, row[0])
		}
	}
	want := []string{"request · turn 2", "response · turn 2 · depth 1"}
	if len(headings) != len(want) {
		t.Fatalf("headings = %q, want one per record %q", headings, want)
	}
	for i := range want {
		if headings[i] != want[i] {
			t.Errorf("heading %d = %q, want %q", i, headings[i], want[i])
		}
	}

	pane := strip(m.renderInspector())
	for _, head := range want {
		if !strings.Contains(pane, head) {
			t.Errorf("the pane does not draw the header %q:\n%s", head, pane)
		}
	}
}

// TestInspectorNamesAnUnrecordedReply pins the note ratified for the flat log: a request the ring
// went on PAST without recording an answer carries one row saying so, because a non-streaming
// reply is decoded off the connection and never captured — absence on its own reads as a response
// that was lost. The newest request never carries it: its call may still be in flight.
func TestInspectorNamesAnUnrecordedReply(t *testing.T) {
	m := inspectorModel(t,
		wireEvent(domain.WireDirectionRequest, `{"a":1}`, 1, 0),
		wireEvent(domain.WireDirectionRequest, `{"b":2}`, 2, 0),
	)

	rows, kinds := m.inspectorRows()

	var notes, headings []int
	for i, row := range rows {
		switch {
		case row[0] == inspectorNoReplyRow:
			notes = append(notes, i)
			if kinds[i] != popupRowPlain {
				t.Errorf("the note at row %d is kind %v, want the plain kind the elision marker uses", i, kinds[i])
			}
		case kinds[i] == popupRowHeading:
			headings = append(headings, i)
		}
	}
	if len(notes) != 1 {
		t.Fatalf("%d note rows for two unanswered requests, want one — the newest may still be in flight", len(notes))
	}
	if len(headings) != 2 {
		t.Fatalf("headings at %v, want one per record", headings)
	}
	if notes[0] < headings[0] || notes[0] >= headings[1] {
		t.Errorf("the note sits at row %d, want it inside the first record (headings at %v)", notes[0], headings)
	}

	if pane := strip(m.renderInspector()); !strings.Contains(pane, "no response recorded") {
		t.Errorf("the pane does not say the reply was never recorded:\n%s", pane)
	}
}

// TestInspectorSaysNothingWhenTheReplyWasRecorded is the other half of the same rule: a request the
// ring DID record an answer for is a complete round-trip and gets no note, so the row never becomes
// noise under every request in a streaming session.
func TestInspectorSaysNothingWhenTheReplyWasRecorded(t *testing.T) {
	m := inspectorModel(t,
		wireEvent(domain.WireDirectionRequest, `{"a":1}`, 1, 0),
		wireEvent(domain.WireDirectionResponse, `{"b":2}`, 1, 0),
	)

	rows, _ := m.inspectorRows()

	for i, row := range rows {
		if row[0] == inspectorNoReplyRow {
			t.Errorf("row %d names an unrecorded reply for a request the ring answered", i)
		}
	}
}

// recordsWithNoReplyNote reports the index of every RECORD the pane draws the unrecorded-reply note
// under, counting records by the heading row each one opens with.
func recordsWithNoReplyNote(t *testing.T, m Model) []int {
	t.Helper()
	rows, kinds := m.inspectorRows()
	var noted []int
	record := -1
	for i, row := range rows {
		switch {
		case kinds[i] == popupRowHeading:
			record++
		case row[0] == inspectorNoReplyRow:
			noted = append(noted, record)
		}
	}
	return noted
}

// TestInspectorPairsTheNoteByWireStream pins the pairing rule the flat log needed once runs go
// concurrent: the successor that settles whether a request went unanswered is the next record of
// the SAME wire stream — the (depth, callID) pair — and never just the next record of the ring,
// which in a fan-out belongs to somebody else.
func TestInspectorPairsTheNoteByWireStream(t *testing.T) {
	const (
		parent  = "" // depth 0 was spawned by no call
		child   = "call-child"
		sibling = "call-sibling"
	)

	cases := []struct {
		name   string
		events []domain.Event
		want   []int
	}{
		{
			name: "a fan-out that answered both calls notes neither",
			events: []domain.Event{
				wireEventOfCall(domain.WireDirectionRequest, `{"a":1}`, 1, 0, parent),
				wireEventOfCall(domain.WireDirectionRequest, `{"b":2}`, 1, 1, child),
				wireEventOfCall(domain.WireDirectionResponse, `{"c":3}`, 1, 1, child),
				wireEventOfCall(domain.WireDirectionResponse, `{"d":4}`, 1, 0, parent),
			},
			want: nil,
		},
		{
			name: "the delegate answered while the parent's call is still out",
			events: []domain.Event{
				wireEventOfCall(domain.WireDirectionRequest, `{"a":1}`, 1, 0, parent),
				wireEventOfCall(domain.WireDirectionRequest, `{"b":2}`, 1, 1, child),
				wireEventOfCall(domain.WireDirectionResponse, `{"c":3}`, 1, 1, child),
			},
			want: nil,
		},
		{
			name: "concurrent siblings at one depth are told apart by call id",
			events: []domain.Event{
				wireEventOfCall(domain.WireDirectionRequest, `{"a":1}`, 1, 1, child),
				wireEventOfCall(domain.WireDirectionRequest, `{"b":2}`, 1, 1, sibling),
				wireEventOfCall(domain.WireDirectionResponse, `{"c":3}`, 1, 1, sibling),
				wireEventOfCall(domain.WireDirectionResponse, `{"d":4}`, 1, 1, child),
			},
			want: nil,
		},
		{
			name: "two calls of one stream still note the first",
			events: []domain.Event{
				wireEventOfCall(domain.WireDirectionRequest, `{"a":1}`, 1, 1, child),
				wireEventOfCall(domain.WireDirectionRequest, `{"b":2}`, 2, 1, child),
			},
			want: []int{0},
		},
		{
			name: "a parent that asks again across a fan-out is noted",
			events: []domain.Event{
				wireEventOfCall(domain.WireDirectionRequest, `{"a":1}`, 1, 0, parent),
				wireEventOfCall(domain.WireDirectionRequest, `{"b":2}`, 1, 1, child),
				wireEventOfCall(domain.WireDirectionResponse, `{"c":3}`, 1, 1, child),
				wireEventOfCall(domain.WireDirectionRequest, `{"d":4}`, 2, 0, parent),
			},
			want: []int{0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := inspectorModel(t, tc.events...)

			noted := recordsWithNoReplyNote(t, m)

			if !slices.Equal(noted, tc.want) {
				t.Errorf("the note lands under records %v, want %v", noted, tc.want)
			}
		})
	}
}

// TestInspectorDisarmedNamesTheKey is the ratified off-state: with nothing captured and the key
// off, the pane says which key arms it instead of drawing an empty box. With the key ON the same
// empty ring is a WAIT, not a thing to fix, so it says that instead.
func TestInspectorDisarmedNamesTheKey(t *testing.T) {
	m := newTestModel(t)
	m.inspector = inspectorPane{open: true}

	pane := strip(m.renderInspector())
	if !strings.Contains(pane, "ui.inspector") {
		t.Errorf("the disarmed pane does not name the key that arms it:\n%s", pane)
	}

	m.opts.Inspector = true
	if armed := strip(m.renderInspector()); strings.Contains(armed, "ui.inspector") {
		t.Errorf("an armed but empty pane tells the human to set a key that is already set:\n%s", armed)
	}
}

// TestInspectVerbOpensThePaneAndEscCloses pins the verb's routing: /inspect opens the pane and
// launches nothing, the frame budgets it as one of its own (openPanes) and stacks it, and esc — one
// of its five keys — closes it while leaving the draft in the box untouched, because the pane is a
// report and not a modal.
func TestInspectVerbOpensThePaneAndEscCloses(t *testing.T) {
	m := newTestModel(t)
	m.opts.Inspector = true
	m = m.foldEvent(wireEvent(domain.WireDirectionRequest, `{"a":1}`, 1, 0))
	m.input.SetValue("/inspect")
	m, cmd := stepCmd(t, m, keyEnter())

	if !m.inspector.open {
		t.Fatal("/inspect did not open the pane")
	}
	if m.state != stateIdle || cmd != nil {
		t.Errorf("state = %v, cmd = %v; /inspect drives no worker", m.state, cmd)
	}
	if !m.openPanes().has(paneInspector) {
		t.Error("the open pane is not in the frame's pane set — it would be drawn on rows nothing budgeted")
	}
	if !strings.Contains(strip(m.frameOverlays().inspector), inspectorTitle) {
		t.Error("the frame does not stack the pane it opened")
	}

	m.input.SetValue("half a draft")
	rowsBehind := m.transcriptRows()
	if m = step(t, m, keyEsc()); m.inspector.open {
		t.Error("esc did not close the pane")
	}
	if m.frameOverlays().inspector != "" {
		t.Error("the closed pane is still stacked in the frame")
	}
	if got := m.transcriptRows(); got <= rowsBehind {
		t.Errorf("transcript rows = %d after the close, want more than the %d the pane left it", got, rowsBehind)
	}
	if got := m.input.Value(); got != "half a draft" {
		t.Errorf("draft = %q, want it untouched — the pane is a report, not a modal", got)
	}
}

// TestInspectOpensOnTheNewestRecord pins where the pane lands: on the LAST full window, so the
// record worth reading — the newest, which is also the one the reader just watched go wrong — is on
// the screen without a hundred page-downs, and the scroll keys then move from THERE.
func TestInspectOpensOnTheNewestRecord(t *testing.T) {
	var events []domain.Event
	for i := range maxWireRecords {
		events = append(events, wireEvent(domain.WireDirectionRequest, `{"n":`+strconv.Itoa(i)+`}`, i, 0))
	}
	m := inspectorModel(t, events...)
	m.inspector = inspectorPane{} // reopen through the verb itself, which is what sets the scroll
	next, _ := m.runInspectCommand()
	m = next.(Model)

	spec, seated := m.inspectorSpec()
	if !seated {
		t.Fatal("the frame seated no pane for a full ring")
	}
	if len(spec.rows) <= spec.maxRows {
		t.Fatalf("precondition: %d rows into a window of %d — the ring must overflow the pane for a scroll to mean anything",
			len(spec.rows), spec.maxRows)
	}
	if spec.rowTop+spec.maxRows != len(spec.rows) {
		t.Fatalf("window [%d,%d) of %d rows, want the last full window", spec.rowTop, spec.rowTop+spec.maxRows, len(spec.rows))
	}

	up := step(t, m, keyUp())
	if got, want := up.inspector.top, spec.rowTop-1; got != want {
		t.Errorf("top = %d after ↑ from the end, want %d — the key moves from the window that was drawn", got, want)
	}
	if page := step(t, m, keyPgUp()); page.inspector.top != max(0, spec.rowTop-spec.maxRows) {
		t.Errorf("top = %d after pgup, want a full window back from %d", page.inspector.top, spec.rowTop)
	}
	if down := step(t, m, keyDown()); down.inspector.top != spec.rowTop {
		t.Errorf("top = %d after ↓ at the end, want it clamped at the last full window %d", down.inspector.top, spec.rowTop)
	}
}

// TestInspectorLeavesEveryOtherKeyAlone is the non-modal half of the contract: the pane owns esc and
// the four scroll keys and NOTHING else, so a printable key types into the box behind it exactly as
// it would with no pane up.
func TestInspectorLeavesEveryOtherKeyAlone(t *testing.T) {
	m := inspectorModel(t, wireEvent(domain.WireDirectionRequest, `{"a":1}`, 1, 0))

	m = step(t, m, keySpace())
	if !m.inspector.open {
		t.Error("a printable key closed the pane; it claims only esc and the scroll keys")
	}
	if got := m.input.Value(); got != " " {
		t.Errorf("draft = %q, want the key to have reached the box behind the pane", got)
	}
}

// ----------------------------------------------------------------------------
// The readable rendering (wireReadableLines)
// ----------------------------------------------------------------------------

// TestReadableRequestSummarisesTheEnvelope pins what a request becomes in the readable rendering:
// ONE line naming how much conversation was replayed, how many tools were offered and which model
// was asked — and a field the body does not carry is left out rather than reported as a zero.
func TestReadableRequestSummarisesTheEnvelope(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "all three fields",
			payload: `{"model":"gpt-oss-20b","messages":[{"role":"system"},{"role":"user"}],"tools":[{"type":"function"}]}`,
			want:    "2 messages · 1 tools · model gpt-oss-20b",
		},
		{
			name:    "tools absent",
			payload: `{"model":"gpt-oss-20b","messages":[{"role":"user"}]}`,
			want:    "1 messages · model gpt-oss-20b",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines, hidden := wireReadableLines(domain.WireDirectionRequest, tc.payload)

			if hidden != 0 {
				t.Errorf("hidden = %d, want nothing dropped from a one-line summary", hidden)
			}
			if len(lines) != 1 || lines[0] != tc.want {
				t.Errorf("readable = %q, want the single summary line %q", lines, tc.want)
			}
		})
	}
}

// TestReadableRequestFallsBackToTheBody is the rule that keeps the rendering honest on a body it
// cannot summarise: a request carrying none of messages/tools/model — and an undecodable one alike
// — is shown exactly as raw mode shows it, dropped count and all. A readable view that answered a
// body it did not understand with an empty pane would hide the body worth reading.
func TestReadableRequestFallsBackToTheBody(t *testing.T) {
	m := inspectorModel(t, wireEvent(domain.WireDirectionRequest, `{"a":1}`, 1, 0))

	rec := m.wire[0]
	if !slices.Equal(rec.readable, rec.lines) {
		t.Errorf("readable = %q, want the pretty lines %q verbatim", rec.readable, rec.lines)
	}
	if rec.readableHidden != rec.hidden {
		t.Errorf("readableHidden = %d, want the pretty count %d", rec.readableHidden, rec.hidden)
	}

	lines, _ := wireReadableLines(domain.WireDirectionRequest, "not json at all")
	if len(lines) != 1 || lines[0] != "not json at all" {
		t.Errorf("readable = %q, want the undecodable body verbatim", lines)
	}
}

// TestReadableMergesConsecutiveDeltas pins the passage rule: a stream that arrives as one JSON
// document per token is read back as the sentences it spelled, one passage per kind, and an empty
// delta in the middle of a run contributes nothing without breaking it — a keep-alive chunk did not
// end the sentence.
func TestReadableMergesConsecutiveDeltas(t *testing.T) {
	payload := strings.Join([]string{
		`{"choices":[{"delta":{"reasoning_content":"weighing "}}]}`,
		`{"choices":[{"delta":{"reasoning_content":""}}]}`,
		`{"choices":[{"delta":{"reasoning_content":"the options"}}]}`,
		`{"choices":[{"delta":{"content":"here is "}}]}`,
		`{"choices":[{"delta":{"content":"the answer"}}]}`,
	}, "\n")

	lines, hidden := wireReadableLines(domain.WireDirectionResponse, payload)

	want := []string{
		readableThinkingPrefix + "weighing the options",
		readableTextPrefix + "here is the answer",
	}
	if hidden != 0 {
		t.Errorf("hidden = %d, want nothing dropped from five short deltas", hidden)
	}
	if !slices.Equal(lines, want) {
		t.Errorf("readable = %q, want one passage per kind %q", lines, want)
	}
}

// TestReadableNamesAToolCallWithoutItsArguments pins the ratified tool-call identity: the function
// name and the first twelve runes of the enclosing call id, on a passage of its own. The arguments
// are elided — they arrive as fragments across chunks and raw mode has them in full — and the
// passage closes the run it interrupted rather than joining it.
func TestReadableNamesAToolCallWithoutItsArguments(t *testing.T) {
	payload := strings.Join([]string{
		`{"choices":[{"delta":{"content":"calling"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"id":"call_abcdefghijklmnop","function":{"name":"read_file","arguments":"{\"path\":\"secret.txt\"}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"\"}"}}]}}]}`,
	}, "\n")

	lines, _ := wireReadableLines(domain.WireDirectionResponse, payload)

	want := []string{
		readableTextPrefix + "calling",
		readableToolCallPrefix + "read_file call_abcdefg",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("readable = %q, want the named call on its own passage %q", lines, want)
	}
	if joined := strings.Join(lines, "\n"); strings.Contains(joined, "secret.txt") {
		t.Errorf("the arguments reached the readable rendering:\n%s", joined)
	}
}

// TestReadableKeepsWhatIsNotADeltaChunk is the other half of the classifier's contract: a line that
// spells no delta is never dropped and never guessed at. A usage-only chunk and an in-band error
// keep their pretty form, a malformed document and the stream's sentinel arrive exactly as the
// server sent them, and each of them closes the passage it followed.
func TestReadableKeepsWhatIsNotADeltaChunk(t *testing.T) {
	usage := `{"choices":[],"usage":{"total_tokens":7}}`
	malformed := `{"choices":[{"delta":`
	payload := strings.Join([]string{
		`{"choices":[{"delta":{"content":"done"}}]}`,
		usage,
		malformed,
		"[DONE]",
	}, "\n")

	lines, _ := wireReadableLines(domain.WireDirectionResponse, payload)

	want := append([]string{readableTextPrefix + "done"}, prettyWireLine(usage)...)
	want = append(want, malformed, "[DONE]")
	if !slices.Equal(lines, want) {
		t.Errorf("readable = %q, want the unclassifiable lines kept %q", lines, want)
	}
}

// TestReadableWrapsALongPassage pins the fold-time wrap: a passage longer than readableWrapColumn
// is broken into rows no wider than it, the kind prefix on the FIRST row only and two spaces under
// it, so the pane — which elides rather than wraps — shows the whole passage instead of its head.
func TestReadableWrapsALongPassage(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{name: "no space to break on", text: strings.Repeat("a", 400)},
		{name: "broken on words", text: strings.TrimSpace(strings.Repeat("word ", 80))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := `{"choices":[{"delta":{"content":` + strconv.Quote(tc.text) + `}}]}`

			lines, _ := wireReadableLines(domain.WireDirectionResponse, payload)

			if len(lines) < 5 {
				t.Fatalf("a 400-rune passage wrapped into %d rows, want at least 5", len(lines))
			}
			if !strings.HasPrefix(lines[0], readableTextPrefix) {
				t.Errorf("first row = %q, want the kind prefix %q", lines[0], readableTextPrefix)
			}
			for i, line := range lines {
				if n := len([]rune(line)); n > readableWrapColumn {
					t.Errorf("row %d is %d runes wide, want at most %d", i, n, readableWrapColumn)
				}
				if i == 0 {
					continue
				}
				if strings.HasPrefix(line, readableTextPrefix) {
					t.Errorf("row %d = %q, want the prefix on the first row only", i, line)
				}
				if !strings.HasPrefix(line, readableContinuationIndent) {
					t.Errorf("row %d = %q, want the continuation indent %q", i, line, readableContinuationIndent)
				}
			}
		})
	}
}

// TestReadableCountsItsOwnHiddenLines pins the ratified cap: maxWireRecordLines applies to EACH
// rendering separately, with its own dropped count. A streamed reply is hundreds of pretty-printed
// lines and a handful of readable ones, and a pane that borrowed the pretty count would announce an
// elision the readable rows never made.
func TestReadableCountsItsOwnHiddenLines(t *testing.T) {
	chunks := make([]string, 0, 20)
	for range 20 {
		chunks = append(chunks, `{"choices":[{"delta":{"content":"x"}}]}`)
	}
	m := inspectorModel(t, wireEvent(domain.WireDirectionResponse, strings.Join(chunks, "\n"), 1, 0))

	rec := m.wire[0]
	if len(rec.lines) != maxWireRecordLines || rec.hidden <= 0 {
		t.Fatalf("pretty rendering kept %d lines with %d hidden, want the cap with a dropped tail", len(rec.lines), rec.hidden)
	}
	if want := []string{readableTextPrefix + strings.Repeat("x", 20)}; !slices.Equal(rec.readable, want) {
		t.Errorf("readable = %q, want the merged passage %q", rec.readable, want)
	}
	if rec.readableHidden != 0 {
		t.Errorf("readableHidden = %d, want nothing dropped from a one-row rendering", rec.readableHidden)
	}
}

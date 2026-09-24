package tui

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
// What the pane shows OF that ring follows the view (runview.go): with a run view open it is the
// viewed delegation's own wire stream and nothing else, named in the title; at the top level it is
// the whole ring as it was recorded. A fan-out braids several runs into one arrival-ordered ring
// (foldWire is the ring's one writer), and a reader who opened a child is asking about that child —
// so the scope is the view's, there is no key for it, and closing the view is what widens it back.
//
// The pane is the /usage report's shape because it IS that shape: both are reportPanes
// (reportpane.go), non-modal and scrollable, rows derived at render time — from the ring here, from
// the folds there — and esc, the four scroll keys and this pane's own ctrl+r their whole keyboard.
// What is written in this file is the ring and what it says; the entries below are the shared
// module's functions under this pane's own name.
//
// ctrl+r is the one key /usage has no use for: this pane's records carry TWO renderings of the same
// bytes (below), the readable one is what it opens on, and the chord flips to the pretty-printed
// protocol and back. Which rendering is showing is pane state and nothing else — nothing is
// persisted, and a closed pane forgets it (reportPane's zero value).
//
// Its rows are FLAT AT THE PANE — one stored line per row, elided at the pane's width like every
// other unwrapped row (popupRowBlocks) — rather than wrapped there: a single JSON line longer than
// the pane's whole row budget seats NOTHING when rows wrap (popupRowWindowFrom), and a raw-protocol
// view that goes blank on a big request body is worse than one that cuts a long line off at the
// border. The one rendering that arrives already broken to width is the READABLE one: it carries
// prose rather than protocol, so it is hard-wrapped at readableWrapColumn when the record is folded
// (wrapReadable) and reaches the pane as flat rows like everything else.
//
// The pane holds a SECOND ring beside the wire one: the upstream attempts (ADR 0085) — one row per
// HTTP attempt a model call made, grouped under the request id its retries share, with the server,
// the attempt index, the ttfb, ttft and whole-attempt clocks, the generation rate and the outcome.
// It is not armed: the engine emits every attempt's measurement whatever `ui.inspector` says, so the
// attempts list with the capture off too, ABOVE the wire half — which then still carries its own
// disarmed row (or the armed-and-empty one), because the key that captures the bytes is the one
// actionable answer to their absence and a list of attempts answers a different question. The run
// view scopes the attempts exactly as it scopes the wire records, by the run that made the call
// ([runRef]: its run id, with depth and callID).

// wireRecord is one half of one Upstream round-trip as the Inspector holds it: which half, the
// Turn, depth, spawning call id and run id of the agent that made the call, and the payload in BOTH of the
// renderings the pane can show — escape-stripped and rendered ONCE, when the event was folded: the
// pretty-printed protocol (lines) and the readable one (readable, wireReadableLines).
//
// depth, callID and runID together name the WIRE STREAM the half belongs to (domain.EventBase,
// [wireRecord.run]): a fan-out interleaves runs in one ring, the call id separates two siblings a
// shared depth would braid, and the run id separates two whose call ids collide. Turn orders the halves inside a stream; it does not identify one.
//
// Both renderings are kept formatted rather than raw for one reason: the pane re-derives its rows
// on every frame, and a frame is painted for every streamed token, so parsing twenty JSON bodies
// per repaint would put the Inspector's cost on a hot path it has no business being on — which is
// why the readable rendering is folded here too rather than derived from lines at the paint. It
// bounds memory as a side effect — hidden and readableHidden say how many lines each cap dropped,
// so the pane can state the cut rather than make it silently.
type wireRecord struct {
	direction      string
	turn           int
	depth          int
	callID         string
	runID          string
	lines          []string
	hidden         int
	readable       []string
	readableHidden int
}

// run is the run that made the record's call — its wire stream — the same ref [runOf] builds from
// its event's base.
func (rec wireRecord) run() runRef {
	return runRef{depth: rec.depth, spawn: rec.callID, id: rec.runID}
}

// inspectorPane is the /inspect overlay's state — a reportPane (reportpane.go) under the name of the
// pane that keeps it: whether the pane is up, how far its record list is scrolled, whether it is
// following the tail of the ring, and which of the two renderings ctrl+r left it showing.
type inspectorPane = reportPane

// inspectorTitle names the pane; inspectorHint and inspectorRawHint spell the keys it owns, one per
// rendering. Each names what ctrl+r would switch TO rather than what is on the screen: the hint
// answers "what else can I do here", and the rows below it already say which rendering they are.
const (
	inspectorTitle   = "raw wire traffic"
	inspectorHint    = "↑/↓ scroll · ctrl+r raw · esc close"
	inspectorRawHint = "↑/↓ scroll · ctrl+r readable · esc close"
)

// maxWireRecords is the ring's whole bound: the most recent twenty half-round-trips. A request
// body repeats the entire conversation, so the ring is deliberately short — twenty covers the tail
// of a debugging session (a handful of calls, both directions) without holding a session's worth
// of prompts in memory for a pane that is closed almost all of the time.
const maxWireRecords = 20

// maxWireRecordLines is how much of ONE record the ring keeps. A tool-carrying request body runs
// to a few hundred pretty-printed lines and a pane shows a dozen at a time, so a cap here bounds
// both the memory the ring holds and the rows a frame composes, and what it costs is a tail the
// reader would have had to page a hundred windows to reach. It applies to EACH of the record's two
// renderings separately, because they are different lengths of the same traffic and a count taken
// off one would misstate the other. The cut is never silent: the dropped count goes on the record —
// one per rendering — and the pane spells it in the package's one elision phrase.
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

// inspectorScopedEmptyRow is that same armed slot narrowed to ONE run (scopedWire): the pane is
// showing the delegation the human is looking at and the ring holds nothing of its stream. Every
// cause of that is in the sentence, because the pane cannot tell them apart — the run has not
// called yet, its records fell off the twenty-record ring, or it is an UNROUTED delegation whose
// halves carry its parent's identity and are filed under the parent (hasUnrecordedReply's residual,
// ADR 0045) — and it names the way back to the whole ring, since what is empty here is the SCOPE
// and not the capture. Capture being off is a different answer and keeps its own row
// (inspectorDisarmedRow): that one is actionable, and this one displaces nothing.
const inspectorScopedEmptyRow = "no records for this run — not called yet, rotated out of the ring, " +
	"or an unrouted delegation speaking over its parent's connection; close the view for the whole ring"

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
// It opens at the END of the list it is actually going to show — the SCOPED one where a run view is
// open (inspectorRows). The rows are newest-last (a log's order), so the record worth
// reading is the last one, and a pane that opened on the oldest of twenty bodies would ask for a
// hundred page-downs before it said anything. The top is set past the last row and the pane opens
// FOLLOWING ([reportKind.follows]), so the window is the last full one on every paint — the frame's
// answer for this paint, not something this verb can know — and the records the agent goes on
// sending arrive under a pane that is already looking at them.
func (m Model) runInspectCommand() (tea.Model, tea.Cmd) {
	rows, _ := m.inspectorRows()
	m.inspector = inspectorPane{open: true, top: len(rows), follow: true}
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
	payload := stripEscapes(we.Payload)
	lines, hidden := wirePayloadLines(payload)
	readable, readableHidden := wireReadableLines(we.Direction, payload)
	keep := m.wire
	if len(keep) >= maxWireRecords {
		keep = keep[len(keep)-maxWireRecords+1:]
	}
	next := make([]wireRecord, 0, len(keep)+1)
	next = append(next, keep...)
	m.wire = append(next, wireRecord{
		direction:      we.Direction,
		turn:           we.Turn,
		depth:          we.Depth,
		callID:         we.CallID,
		runID:          we.RunID,
		lines:          lines,
		hidden:         hidden,
		readable:       readable,
		readableHidden: readableHidden,
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

// ----------------------------------------------------------------------------
// The readable rendering (the second one every record carries)
// ----------------------------------------------------------------------------
//
// The pretty-printed protocol above answers "what exactly went over the wire". The readable
// rendering answers the question the reader usually opened the pane WITH — what did the model say,
// think and call — and it answers it from the same bytes: a request becomes the one line that
// describes its envelope, and a response becomes the passages its deltas spell, because a stream
// that arrives as three hundred one-token JSON documents is a sentence nobody can read as one.
//
// It is computed at the FOLD, beside the pretty one and never instead of it (foldWire): the pane
// picks a rendering per frame, and neither is ever derived on the paint path.

// The readable rendering's row prefixes: the first row of a passage carries the one that names its
// kind, every row after it carries readableContinuationIndent, and a passage read back off the pane
// is therefore one paragraph rather than a column of unattributed lines.
const (
	readableThinkingPrefix     = "· thinking "
	readableTextPrefix         = "· "
	readableToolCallPrefix     = "· tool call "
	readableContinuationIndent = "  "
)

// readableWrapColumn is the width the readable rendering is hard-wrapped to, in runes, when the
// record is folded. The pane elides rather than wraps (the header comment above), so prose that
// arrived as one 4000-rune passage would show as one cut-off row; wrapping it here is what makes it
// readable at every pane width the frame can grant, at the cost of a fixed column rather than the
// live one — which is the trade the pane's flat rows already made.
const readableWrapColumn = 96

// maxToolCallIDRunes is how much of a tool call's wire id the readable rendering keeps: enough to
// tell two calls of the same tool apart in one stream, short enough that the name stays the thing
// the eye lands on. The arguments are elided entirely — raw mode has them in full.
const maxToolCallIDRunes = 12

// sseChunk is the streamed-completion chunk as the readable rendering reads it, narrowed to the
// members it classifies by. It MIRRORS the provider's own decode shape
// (internal/provider/stream.go) rather than importing it because that type is unexported there; the
// mirror is deliberately partial — a member the classifier does not read is a member this pane does
// not need to track. Partial in WIDTH only: the members it does mirror match the provider's named
// shape and precedence exactly, so a payload the engine classified one way cannot read as another
// here.
type sseChunk struct {
	Choices []struct {
		Delta sseDelta `json:"delta"`
	} `json:"choices"`
}

// sseDelta is the incremental payload of one streamed choice, mirroring the provider's own
// sseDelta. It is a named type rather than an inline struct so the thinking-channel precedence can
// hang on it as a method, exactly as it does there.
//
// ReasoningContent and Reasoning are ONE channel in two wire spellings, not two channels:
// llama.cpp, vLLM and LM Studio send `reasoning_content`, while Ollama and OpenRouter send
// `reasoning` for the same thinking stream. Neither may be "simplified" away — dropping either
// spelling makes a whole server's reasoning render as nothing at all, which is precisely what this
// pane's never-hide contract forbids. Reasoning is json.RawMessage and not a string on purpose: a
// server is free to send a non-string under that key (OpenRouter's terminal chunk sends null), and
// a string field would fail the whole chunk's Unmarshal — dropping its CONTENT along with its
// reasoning, straight into the prettyWireLine fallback.
type sseDelta struct {
	Content          string          `json:"content"`
	ReasoningContent string          `json:"reasoning_content"`
	Reasoning        json.RawMessage `json:"reasoning"`
	ToolCalls        []struct {
		ID       string `json:"id"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tool_calls"`
}

// thinking returns this delta's reasoning under the same precedence the provider applies:
// reasoning_content wins wherever it is NON-EMPTY, and the `reasoning` alias is read only
// otherwise. The test is non-emptiness and never key presence — LM Studio always sends a
// reasoning_content key and leaves it empty when the model did not reason, so a presence test would
// block the fallback outright. Anything under `reasoning` that is not a JSON string — null, an
// object, an absent field — reads as no reasoning at all rather than as literal bytes. The
// precedence is per chunk: nothing latches onto the first spelling a stream happens to use.
func (d sseDelta) thinking() string {
	if d.ReasoningContent != "" {
		return d.ReasoningContent
	}
	var text string
	if err := json.Unmarshal(d.Reasoning, &text); err != nil {
		return ""
	}
	return text
}

// wireReadableLines renders one payload the readable way for the ring: a request as its envelope
// summary, a response as the passages its deltas spell, capped at maxWireRecordLines with its own
// dropped count — the same bound the pretty rendering takes, counted separately because the two are
// different lengths of the same traffic.
//
// It never fails and never drops a line it could not classify: a request body it cannot summarise
// falls back to wirePayloadLines whole (count and all), and any response line that is not a delta
// chunk goes through in its pretty form. What the reader is looking at is what a server actually
// sent, and the one thing this pane may never do is hide the part that did not fit the shape.
func wireReadableLines(direction, payload string) (lines []string, hidden int) {
	if direction == domain.WireDirectionRequest {
		summary, ok := wireRequestSummary(payload)
		if !ok {
			return wirePayloadLines(payload)
		}
		return []string{summary}, 0
	}
	lines = wireResponsePassages(payload)
	if len(lines) > maxWireRecordLines {
		hidden = len(lines) - maxWireRecordLines
		lines = lines[:maxWireRecordLines]
	}
	return lines, hidden
}

// wireRequestSummary reduces a request body to the one line that says what was asked of the model:
// how many messages the conversation was replayed as, whether a system prompt rode above them, how
// many tools it was offered, and which model it went to. A field the body does not carry is left
// out rather than reported as zero, and a body carrying none of them is no summary at all — ok is
// false, and the caller shows the body as it stands.
//
// The lengths are counted off json.RawMessage rather than a decoded shape on purpose: this is a
// VIEW of a request some provider dialect built, and a summary that failed whenever a message
// carried a member this pane never modelled would fail on exactly the bodies worth looking at.
//
// `system` is the Messages wire's hoisted system prompt (ADR 0078): on the openai wire it is a
// message among the messages and needs no word of its own, while on the anthropic wire it is a
// top-level member the message count would otherwise silently leave out — so it is named beside
// the count (`system + 2 messages`), never folded into it.
func wireRequestSummary(payload string) (summary string, ok bool) {
	var body struct {
		System   *json.RawMessage   `json:"system"`
		Messages *[]json.RawMessage `json:"messages"`
		Tools    *[]json.RawMessage `json:"tools"`
		Model    *string            `json:"model"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &body); err != nil {
		return "", false
	}
	var parts []string
	switch {
	case body.Messages != nil && body.System != nil:
		parts = append(parts, "system + "+strconv.Itoa(len(*body.Messages))+" messages")
	case body.Messages != nil:
		parts = append(parts, strconv.Itoa(len(*body.Messages))+" messages")
	case body.System != nil:
		parts = append(parts, "system")
	}
	if body.Tools != nil {
		parts = append(parts, strconv.Itoa(len(*body.Tools))+" tools")
	}
	if body.Model != nil {
		parts = append(parts, "model "+*body.Model)
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, " · "), true
}

// wireResponsePassages turns a captured response into the passages it spells. Consecutive deltas
// of one kind are one passage, because a token is not a thought and a stream that showed one row
// per token would be the raw rendering with extra steps; an empty delta contributes nothing and
// does NOT break the run, since a keep-alive chunk in the middle of a sentence did not end the
// sentence.
//
// The capture comes in one of two shapes, and the readable rendering reads both (ADR 0078): on the
// openai wire the stream's raw `data:` payloads newline-joined (domain.WireEvent), each line a
// chat-completion chunk; on the anthropic wire the SSE body as received, framing and all, each
// `data:` line an event keyed on its `type`. The shape is read off the capture itself
// (isSSEFramed) — a framed stream's non-`data:` lines are the framing its parser never reads (the
// `event:` line repeats the payload's type, the blank line separates events) and contribute no row,
// while every payload goes through one of the two decoders (readablePassages.readChunk,
// readMessagesEvent).
//
// A tool call is its own passage, named and identified but never argued: the arguments arrive as
// fragments across chunks and raw mode already has them in full. Any payload neither decoder
// claims — the [DONE] sentinel, a blank line, a truncated document, a usage-only chunk, an in-band
// error member, an event type this pane does not know — closes the open passage and goes through
// in its pretty form, which for a line that is not JSON is the line exactly as it arrived
// (prettyWireLine). What the reader is looking at is what a server actually sent, and the one
// thing this pane may never do is hide the part that did not fit the shape.
func wireResponsePassages(payload string) []string {
	trimmed := strings.Trim(payload, "\n")
	if trimmed == "" {
		return nil
	}
	framed := isSSEFramed(trimmed)
	var p readablePassages
	for _, raw := range strings.Split(trimmed, "\n") {
		data := strings.TrimSpace(raw)
		if framed {
			if !strings.HasPrefix(data, sseDataPrefix) {
				continue
			}
			// The fallback below keeps the payload without its framing: the prefix is not the
			// protocol, and with it in the way json.Indent would refuse the body entirely.
			raw = strings.TrimPrefix(data, sseDataPrefix)
			data = raw
		}
		if p.readChunk(data) || p.readMessagesEvent(data) {
			continue
		}
		p.keep(raw)
	}
	p.close()
	return p.rows
}

// sseDataPrefix is the SSE line prefix carrying a payload; every other line is framing. It mirrors
// the provider's own constant (internal/provider/wire_anthropic_stream.go) for the same reason
// sseChunk mirrors its decode shape: that one is unexported there.
const sseDataPrefix = "data: "

// isSSEFramed reports whether a captured response kept its SSE framing — the shape every wire but
// openai records (provider streamCapture: "on any other wire the body as received"). The openai
// capture never carries a `data:` prefix, because joining the payloads is precisely what stripped
// it; a capture that does is therefore a framed one, and its lines are read as framing plus
// payloads rather than as payloads alone.
func isSSEFramed(trimmed string) bool {
	return strings.HasPrefix(trimmed, sseDataPrefix) || strings.Contains(trimmed, "\n"+sseDataPrefix)
}

// readablePassages is the accumulator one response's readable rendering is built in: the rows
// rendered so far and the passage still open — its kind prefix and the text its deltas have
// spelled. It is a type rather than the closures it began as because two decoders now feed it,
// one per wire, and the passage rule (one run per kind, closed by whatever is not of that kind) is
// theirs to share, not to copy.
type readablePassages struct {
	rows   []string
	prefix string
	text   string
}

// close renders the open run, if there is one, and leaves nothing open behind it.
func (p *readablePassages) close() {
	if p.prefix == "" {
		return
	}
	p.rows = append(p.rows, wrapReadable(p.prefix, p.text, readableWrapColumn)...)
	p.prefix, p.text = "", ""
}

// extend adds one delta to the open run of its kind, opening a run when the kind changed.
func (p *readablePassages) extend(kind, delta string) {
	if delta == "" {
		return
	}
	if p.prefix != kind {
		p.close()
		p.prefix = kind
	}
	p.text += delta
}

// row closes the open run and renders one passage of its own — a tool call, a stop reason, a
// usage line — wrapped like any other.
func (p *readablePassages) row(prefix, text string) {
	p.close()
	p.rows = append(p.rows, wrapReadable(prefix, text, readableWrapColumn)...)
}

// keep closes the open run and passes one unclassified line through in its pretty form.
func (p *readablePassages) keep(line string) {
	p.close()
	p.rows = append(p.rows, prettyWireLine(line)...)
}

// readChunk reads one payload as a chat-completion chunk (sseChunk) and folds its delta in,
// reporting whether the payload was one: a document that does not decode, or decodes without a
// choice, is not this decoder's to render.
func (p *readablePassages) readChunk(data string) bool {
	var chunk sseChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil || len(chunk.Choices) == 0 {
		return false
	}
	delta := chunk.Choices[0].Delta
	p.extend(readableThinkingPrefix, delta.thinking())
	p.extend(readableTextPrefix, delta.Content)
	for _, call := range delta.ToolCalls {
		if call.Function.Name == "" {
			continue
		}
		p.row(readableToolCallPrefix, toolCallLabel(call.Function.Name, call.ID))
	}
	return true
}

// The Messages stream event types the readable rendering classifies by, as each payload's `type`
// names them, and the content_block_delta fragment types under them. They mirror the provider's
// own vocabulary (internal/provider/wire_anthropic_stream.go), unexported there like the rest.
const (
	messagesEventStart      = "message_start"
	messagesEventBlockStart = "content_block_start"
	messagesEventBlockDelta = "content_block_delta"
	messagesEventBlockStop  = "content_block_stop"
	messagesEventDelta      = "message_delta"
	messagesEventStop       = "message_stop"
	messagesEventPing       = "ping"

	messagesBlockText     = "text"
	messagesBlockThinking = "thinking"
	messagesBlockToolUse  = "tool_use"

	messagesDeltaText      = "text_delta"
	messagesDeltaThinking  = "thinking_delta"
	messagesDeltaInputJSON = "input_json_delta"
	messagesDeltaSignature = "signature_delta"
)

// The readable rendering's row prefixes for what the Messages wire says OUTSIDE its content
// blocks: the served model and the input accounting (message_start), the stop reason and the
// output accounting (message_delta). The openai wire says these in a usage-only chunk that goes
// through pretty; here they are typed events of their own and read as such.
const (
	readableModelPrefix = "· model "
	readableStopPrefix  = "· stop "
	readableUsagePrefix = "· usage "
)

// messagesEvent is one Messages stream payload as the readable rendering reads it, narrowed to
// the members it classifies by — the MIRROR of the provider's anthropicEvent
// (internal/provider/wire_anthropic_stream.go), partial in width like sseChunk and for the same
// reason. Which members an event populates depends on its type: Message on message_start,
// ContentBlock on content_block_start, Delta on content_block_delta and message_delta, Usage on
// message_delta.
type messagesEvent struct {
	Type    string `json:"type"`
	Message *struct {
		Model string         `json:"model"`
		Usage *messagesUsage `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Thinking string `json:"thinking"`
		ID       string `json:"id"`
		Name     string `json:"name"`
	} `json:"content_block"`
	Delta *struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		Thinking   string `json:"thinking"`
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage *messagesUsage `json:"usage"`
}

// messagesUsage is the Messages wire's token accounting as the rendering reads it: the input side
// arrives on message_start (cache reads counted apart from input_tokens on this wire), the output
// side on message_delta — the same split the provider assembles its Usage from.
type messagesUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	CacheReadTokens int `json:"cache_read_input_tokens"`
}

// readMessagesEvent reads one payload as a Messages stream event keyed on its type and folds it
// in, reporting whether it rendered the payload. Text and thinking fragments extend a passage like
// chat-completion deltas; a tool_use block is named at its start and its input fragments are
// elided like the openai arguments; block and message stops close the open passage; a ping is the
// keep-alive and contributes nothing. An event whose type this pane does not know, a fragment type
// it does not know, an error event, and a known event carrying nothing it can say are all NOT
// rendered — false hands them to the caller's pretty fallback, so nothing the server said is lost.
func (p *readablePassages) readMessagesEvent(data string) bool {
	var ev messagesEvent
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return false
	}
	switch ev.Type {
	case messagesEventStart:
		return p.readMessageStart(ev)
	case messagesEventBlockStart:
		return p.readBlockStart(ev)
	case messagesEventBlockDelta:
		return p.readBlockDelta(ev)
	case messagesEventDelta:
		return p.readMessageDelta(ev)
	case messagesEventBlockStop, messagesEventStop:
		p.close()
		return true
	case messagesEventPing:
		return true
	}
	return false
}

// readMessageStart renders message_start as the served model and the input accounting, one row
// each, and claims the event only when it had at least one of them to say.
func (p *readablePassages) readMessageStart(ev messagesEvent) bool {
	if ev.Message == nil {
		return false
	}
	said := false
	if ev.Message.Model != "" {
		p.row(readableModelPrefix, ev.Message.Model)
		said = true
	}
	if u := ev.Message.Usage; u != nil && u.InputTokens > 0 {
		parts := []string{strconv.Itoa(u.InputTokens) + " input tokens"}
		if u.CacheReadTokens > 0 {
			parts = append(parts, strconv.Itoa(u.CacheReadTokens)+" cached")
		}
		p.row(readableUsagePrefix, strings.Join(parts, " · "))
		said = true
	}
	return said
}

// readBlockStart opens a content block: a tool_use block is the named tool-call passage, a text
// or thinking block extends its kind with whatever text the start already carried (nothing, on a
// stream). A block type this pane does not know is not claimed.
func (p *readablePassages) readBlockStart(ev messagesEvent) bool {
	block := ev.ContentBlock
	if block == nil {
		return false
	}
	switch block.Type {
	case messagesBlockToolUse:
		if block.Name == "" {
			return false
		}
		p.row(readableToolCallPrefix, toolCallLabel(block.Name, block.ID))
	case messagesBlockText:
		p.extend(readableTextPrefix, block.Text)
	case messagesBlockThinking:
		p.extend(readableThinkingPrefix, block.Thinking)
	default:
		return false
	}
	return true
}

// readBlockDelta folds one content_block_delta fragment in by its type: text and thinking extend
// their passages; a tool call's input JSON and a thinking block's signature are classified and
// elided — the arguments because raw mode has them in full, the signature because it is opaque
// bytes the model never spelled. A fragment type this pane does not know is not claimed.
func (p *readablePassages) readBlockDelta(ev messagesEvent) bool {
	delta := ev.Delta
	if delta == nil {
		return false
	}
	switch delta.Type {
	case messagesDeltaText:
		p.extend(readableTextPrefix, delta.Text)
	case messagesDeltaThinking:
		p.extend(readableThinkingPrefix, delta.Thinking)
	case messagesDeltaInputJSON, messagesDeltaSignature:
	default:
		return false
	}
	return true
}

// readMessageDelta renders message_delta as the stop reason and the output accounting, one row
// each, and claims the event only when it had at least one of them to say.
func (p *readablePassages) readMessageDelta(ev messagesEvent) bool {
	said := false
	if ev.Delta != nil && ev.Delta.StopReason != "" {
		p.row(readableStopPrefix, ev.Delta.StopReason)
		said = true
	}
	if ev.Usage != nil && ev.Usage.OutputTokens > 0 {
		p.row(readableUsagePrefix, strconv.Itoa(ev.Usage.OutputTokens)+" output tokens")
		said = true
	}
	return said
}

// toolCallLabel names one call in the readable rendering: the tool's name, and as much of the wire
// id as tells two calls of it apart. A fragment that carried no id at all is named on its own
// rather than trailed by an empty column.
func toolCallLabel(name, id string) string {
	short := []rune(id)
	if len(short) > maxToolCallIDRunes {
		short = short[:maxToolCallIDRunes]
	}
	if len(short) == 0 {
		return name
	}
	return name + " " + string(short)
}

// wrapReadable breaks one passage into rows: the prefix on the first row, two spaces on every row
// after it, nothing wider than column runes, and a break taken at a space wherever the passage
// offers one within the budget and mid-word where it does not — a 4000-rune JSON blob a model
// streamed as prose still has to fit.
//
// Newlines inside the passage are the model's own paragraphing and start a row of their own; the
// count is in RUNES rather than display cells, which is the measure a caller can take without a
// theme.
//
// The column is a PARAMETER rather than the constant it began as, because the two callers measure
// different things by it. The /inspect ring wraps at fold time, before any width is known, and
// passes readableWrapColumn — the fixed column its rows have always used, and its rendering does
// not change by a byte. The /thinking pane wraps at paint time and passes the pane's real row
// budget for that frame (thinkingpane.go): its rows are prose with no raw toggle behind them, so a
// column wider than the pane would be text the popup's truncation cuts off unrecoverably.
func wrapReadable(prefix, text string, column int) []string {
	var rows []string
	lead := prefix
	for _, segment := range strings.Split(text, "\n") {
		rows = appendReadableSegment(rows, lead, segment, column)
		lead = readableContinuationIndent
	}
	return rows
}

// appendReadableSegment appends one newline-free segment's rows to rows: the first after lead, every
// later one after readableContinuationIndent.
//
// A segment that fits its budget is kept as the bytes it came in. One that does not is decoded to
// runes ONCE and cut by rune offset from there, so a line of L runes costs O(L) however many rows
// it spans — re-decoding the remainder at every cut made a 64 KB line cost O(L²). The decode is
// also why a cut segment's invalid UTF-8 comes out as U+FFFD on every row, as it always has.
func appendReadableSegment(rows []string, lead, segment string, column int) []string {
	budget := max(column-utf8.RuneCountInString(lead), 1)
	if utf8.RuneCountInString(segment) <= budget {
		return append(rows, strings.TrimRight(lead+segment, " "))
	}
	runes := []rune(segment)
	for {
		row, rest := cutReadable(lead, runes, column)
		rows = append(rows, strings.TrimRight(row, " "))
		if len(rest) == 0 {
			return rows
		}
		runes = rest
		lead = readableContinuationIndent
	}
}

// cutReadable takes the next row off one segment's runes: everything that fits after lead within
// column runes, and whatever is left over. It prefers the last space at or before the budget and
// cuts mid-rune-run only when the segment offers none, so a wrapped paragraph breaks on words
// wherever words exist. The rest is a subslice of runes, never a copy.
func cutReadable(lead string, runes []rune, column int) (row string, rest []rune) {
	budget := max(column-utf8.RuneCountInString(lead), 1)
	if len(runes) <= budget {
		return readableRow(lead, runes), nil
	}
	for i := budget; i > 0; i-- {
		if runes[i] != ' ' {
			continue
		}
		rest = runes[i:]
		for len(rest) > 0 && rest[0] == ' ' {
			rest = rest[1:]
		}
		return readableRow(lead, runes[:i]), rest
	}
	return readableRow(lead, runes[:budget]), runes[budget:]
}

// readableRow spells lead and runes as one string in a single allocation.
func readableRow(lead string, runes []rune) string {
	size := len(lead)
	for _, r := range runes {
		size += utf8.RuneLen(r)
	}
	var row strings.Builder
	row.Grow(size)
	row.WriteString(lead)
	for _, r := range runes {
		row.WriteRune(r)
	}
	return row.String()
}

// ----------------------------------------------------------------------------
// The upstream attempts (the pane's second ring)
// ----------------------------------------------------------------------------

// attemptRecord is one upstream HTTP attempt as the Inspector holds it: the run and Turn that made
// the call, the request id its retries share, and the attempt's row rendered ONCE at the fold —
// the pane re-derives its rows on every frame, and formatting five clocks per attempt per streamed
// token is the hot-path cost wireRecord keeps its renderings formatted to avoid.
type attemptRecord struct {
	turn      int
	depth     int
	callID    string
	runID     string
	requestID string
	row       string
}

// run is the run that made the attempt's call, the same ref [runOf] builds from its event's base.
func (rec attemptRecord) run() runRef {
	return runRef{depth: rec.depth, spawn: rec.callID, id: rec.runID}
}

// maxAttemptRecords is the attempt ring's bound. An attempt is one short row, not a replayed
// conversation, so the ring runs longer than the wire one: fifty covers the retries of a flaky
// stretch without holding a session's worth of measurements for a pane that is mostly closed.
const maxAttemptRecords = 50

// attemptUnreached is what an attempt row prints for a clock the attempt never reached (the
// event's zero) and for a rate it carries none of — never a zero that reads as a measurement.
const attemptUnreached = "—"

// foldAttempt files one Event into the attempt ring, and folds nothing else: an
// UpstreamAttemptEvent is recorded, every other variant passes through untouched. It is called
// from foldEvent beside foldWire and is the ONLY writer of the ring, which it REBUILDS rather than
// appends into, on foldWire's value-copy terms (ADR 0011).
func (m Model) foldAttempt(e domain.Event) Model {
	ae, ok := e.(domain.UpstreamAttemptEvent)
	if !ok {
		return m
	}
	keep := m.attempts
	if len(keep) >= maxAttemptRecords {
		keep = keep[len(keep)-maxAttemptRecords+1:]
	}
	next := make([]attemptRecord, 0, len(keep)+1)
	next = append(next, keep...)
	m.attempts = append(next, attemptRecord{
		turn:      ae.Turn,
		depth:     ae.Depth,
		callID:    ae.CallID,
		runID:     ae.RunID,
		requestID: ae.RequestID,
		row:       attemptRow(ae),
	})
	return m
}

// attemptRow renders one attempt: its index within the call, the server it went to, the three
// clocks timed from its send (first byte, first model delta, the attempt's end), the generation
// rate and the outcome — "#1 · box · ttfb 90ms · ttft 1.2s · total 3.4s · 42 tok/s · ok". The
// server name crosses stripEscapes like every other string this pane shows.
func attemptRow(ae domain.UpstreamAttemptEvent) string {
	return strings.Join([]string{
		"#" + strconv.Itoa(ae.Index),
		stripEscapes(ae.Server),
		"ttfb " + attemptClock(ae.TTFB),
		"ttft " + attemptClock(ae.TTFT),
		"total " + attemptClock(ae.Duration),
		attemptRate(ae),
		stripEscapes(ae.Outcome),
	}, " · ")
}

// attemptClock reads one clock the way the picker's summary does (formatSummaryDuration), or
// attemptUnreached where the attempt never reached it.
func attemptClock(d time.Duration) string {
	if d <= 0 {
		return attemptUnreached
	}
	return formatSummaryDuration(d)
}

// attemptRate is the attempt's generation rate — reported output tokens over the span from its
// first model delta to its last, the per-attempt rate the server stats take their median of
// (internal/serverstats) — or attemptUnreached where the server reported no tokens or the span is
// empty: never an estimate.
func attemptRate(ae domain.UpstreamAttemptEvent) string {
	span := ae.Last - ae.TTFT
	if ae.OutputTokens <= 0 || ae.TTFT <= 0 || span <= 0 {
		return attemptUnreached + " tok/s"
	}
	rate := float64(ae.OutputTokens) / span.Seconds()
	return strconv.FormatFloat(rate, 'f', 0, 64) + " tok/s"
}

// scopedAttempts is scopedWire for the attempt ring: the whole ring at the top level, only the
// viewed run's attempts while a run view is open, and a FRESH slice when scoped (ADR 0011).
func (m Model) scopedAttempts() []attemptRecord {
	if !m.inRunView() {
		return m.attempts
	}
	viewed := m.viewedRun()
	scoped := make([]attemptRecord, 0, len(m.attempts))
	for _, rec := range m.attempts {
		if rec.run() == viewed {
			scoped = append(scoped, rec)
		}
	}
	return scoped
}

// attemptGroupKey names one model call's attempts: the run that made it and the request id its
// retries share. The run is part of the key because the id is random per call and a fan-out
// braids calls of several runs into one ring; keying on it too costs nothing and keeps two runs'
// groups apart even where an id is empty.
type attemptGroupKey struct {
	run       runRef
	requestID string
}

// attemptRows composes the attempt half of the pane: one heading per model call, in the order the
// calls first appeared, naming the request id, Turn and — off the top level — depth, then that
// call's attempts in arrival order. A retried call is therefore ONE group however many other
// calls' attempts landed between its retries.
func attemptRows(records []attemptRecord) ([]popupRow, []popupRowKind) {
	if len(records) == 0 {
		return nil, nil
	}
	var order []attemptGroupKey
	groups := make(map[attemptGroupKey][]attemptRecord, len(records))
	for _, rec := range records {
		key := attemptGroupKey{run: rec.run(), requestID: rec.requestID}
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], rec)
	}
	rows := make([]popupRow, 0, len(records)+len(order))
	kinds := make([]popupRowKind, 0, len(records)+len(order))
	for _, key := range order {
		members := groups[key]
		rows = append(rows, popupRow{attemptGroupHeader(members[0])})
		kinds = append(kinds, popupRowHeading)
		for _, rec := range members {
			rows = append(rows, popupRow{rec.row})
			kinds = append(kinds, popupRowPlain)
		}
	}
	return rows, kinds
}

// attemptGroupHeader names one model call's group: "attempts · request <id> · turn N", plus the
// depth of a delegated run on wireRecordHeader's terms.
func attemptGroupHeader(rec attemptRecord) string {
	head := "attempts · request " + rec.requestID + " · turn " + strconv.Itoa(rec.turn)
	if rec.depth > 0 {
		head += " · depth " + strconv.Itoa(rec.depth)
	}
	return head
}

// inspectContent is what the pane tells the shared module about itself for one frame: its name, the
// keys it spells, how tall it likes to be, and the record rows with the kinds composed beside them.
// It words no empty state of its own — an empty ring is a ROW here, and which one depends on whether
// anything is being captured at all (inspectorRows).
//
// It is a METHOD because the rendering ctrl+r selected is Model state, and the rows and the hint are
// two halves of ONE answer about it: composed apart, a pane could spell one rendering's keys over the
// other's rows. The shared module reaches it through the kind's row alone ([reportRows]), so
// there is no second composition to drift from it. The TITLE joins them for the same reason once the pane scopes:
// a box called "raw wire traffic" over one delegation's records would misname what is under it, so
// the run's name is composed HERE, beside the rows it belongs to, and the unscoped title stays the
// bare constant it has always been.
func (m Model) inspectContent() reportContent {
	hint := inspectorHint
	if m.inspector.raw {
		hint = inspectorRawHint
	}
	title := inspectorTitle
	if m.inRunView() {
		title += " · " + m.runLabel(m.viewedRun())
	}
	rows, kinds := m.inspectorRows()
	return reportContent{
		title:  title,
		hint:   hint,
		rowCap: maxInspectorRows,
		rows:   rows,
		kinds:  kinds,
	}
}

// scopedWire is the record list the pane speaks for in THIS frame: the whole ring at the top level,
// and only the viewed delegation's records while a run view is open — the record's run
// ([wireRecord.run]) against the runRef the view is rooted at (runOf's mapping, ADR 0039), which is
// one `==` because those three facts are the run identity every event carries.
//
// It is a slice and not a filter passed around because everything the pane composes has to see the
// SAME list: the headers, the elision counts and the unanswered-request note are all statements
// about a list, and a note paired over the whole ring while the rows came from one run's share of it
// would put a sibling's silence under this run's request.
//
// The scoped slice is FRESH (ADR 0011): the Model is copied by value on every Update, so a slice
// handed back over the ring's own backing array would let a later fold write into rows a frame is
// still drawing. Unscoped there is nothing to build — the ring itself is the answer, read-only.
func (m Model) scopedWire() []wireRecord {
	if !m.inRunView() {
		return m.wire
	}
	viewed := m.viewedRun()
	scoped := make([]wireRecord, 0, len(m.wire))
	for _, rec := range m.wire {
		if rec.run() == viewed {
			scoped = append(scoped, rec)
		}
	}
	return scoped
}

// inspectorRows composes the report: first the upstream attempts the pane speaks for, grouped by
// model call (attemptRows over scopedAttempts — nothing at all when there are none), then for each
// record the pane speaks for (scopedWire), oldest first,
// a header row naming the direction and the agent that made the call, then the payload's lines,
// then — where the cap cut one — the elision the package words every hidden-lines statement with,
// then — where the list went on without recording the answer — the note that says so
// (hasUnrecordedReply, asked over that same list). An empty wire list is ONE row under the attempts, and which one depends
// on why it is empty: capture off is the one actionable answer and keeps the slot wherever it
// applies, an empty ring at the top level is a wait, and an empty SCOPE with capture on is a fact
// about the run being looked at.
//
// WHICH lines is the pane's mode (ctrl+r): the readable rendering unless the pane is raw. The
// elision marker is taken off the SAME rendering's dropped count — the two caps are counted
// separately (maxWireRecordLines), so a marker borrowed from the other one would announce a cut
// these rows never made.
//
// The kinds are composed in the same pass rather than derived from the rows afterwards: a header is
// a header because of where it was put, and a payload line that happened to look like one would be
// styled as a section label by any rule read back off the text.
func (m Model) inspectorRows() ([]popupRow, []popupRowKind) {
	rows, kinds := attemptRows(m.scopedAttempts())
	records := m.scopedWire()
	if len(records) == 0 {
		row := inspectorEmptyRow
		switch {
		case !m.opts.UI.Inspector:
			row = inspectorDisarmedRow
		case m.inRunView():
			row = inspectorScopedEmptyRow
		}
		return append(rows, popupRow{row}), append(kinds, popupRowPlain)
	}
	for i, rec := range records {
		lines, hidden := rec.readable, rec.readableHidden
		if m.inspector.raw {
			lines, hidden = rec.lines, rec.hidden
		}
		rows = append(rows, popupRow{wireRecordHeader(rec)})
		kinds = append(kinds, popupRowHeading)
		for _, line := range lines {
			rows = append(rows, popupRow{line})
			kinds = append(kinds, popupRowPlain)
		}
		if hidden > 0 {
			rows = append(rows, popupRow{popupElisionMarker(hidden)})
			kinds = append(kinds, popupRowPlain)
		}
		if hasUnrecordedReply(records, i) {
			rows = append(rows, popupRow{inspectorNoReplyRow})
			kinds = append(kinds, popupRowPlain)
		}
	}
	return rows, kinds
}

// hasUnrecordedReply says whether the record at index i is a request the pane's list will never show
// an answer for. It is asked over the very list the rows were composed from (scopedWire) and never
// over the ring behind it, so a scoped pane pairs within what it is showing. The successor rule that
// settles it applies WITHIN one wire stream — the run ([wireRecord.run]: run id, depth and callID),
// the run identity every event carries (domain.EventBase) — and never across a list that holds more
// than one: records arrive in arrival order from the one writer (foldWire), so a fan-out
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
// records carry the parent's run and braid into one stream — the note can still land
// under the wrong request of such a pair. No field on the event separates them; a ROUTED spawn
// (ADR 0045) builds its own client and tap and is separated.
func hasUnrecordedReply(records []wireRecord, i int) bool {
	rec := records[i]
	if rec.direction != domain.WireDirectionRequest {
		return false
	}
	for _, next := range records[i+1:] {
		if next.run() != rec.run() {
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

package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The transcript (phase-2 detail plan §3 C6)
// ----------------------------------------------------------------------------

// transcript is the scrollback model: an append-only list of typed entries plus a
// single in-progress assistant buffer fed by streamed TokenEvents. It is the C6
// rendering model the viewport displays. apply folds the full event stream into it (P2.3):
// tokens grow the in-progress buffer, which is finalised on a MessageEvent or the first
// ToolCallEvent of a Turn and discarded on a StreamResetEvent; tool calls, results,
// approvals, and recovered faults append their own entries. It renders only — no agent
// logic lives here (C5).
type transcript struct {
	entries []entry // committed, in display order
	pending string  // in-progress assistant tokens for the current Turn (a plain string,
	// not a strings.Builder: the Model is a value type copied on every
	// Update, and a Builder forbids the copy — it panics copyCheck)
	streaming bool // whether pending holds an un-committed assistant buffer
	turn      int  // the latest Turn index seen (drives the status line)
	debug     bool // when set, MechanismFiredEvents are recorded (a hidden debug view)
}

// entryKind tags a transcript entry so the renderer can prefix and style it. The set
// mirrors the C6 entry kinds (user / assistant / tool call+result / error / note).
type entryKind int

const (
	entryUser entryKind = iota
	entryAssistant
	entryToolCall
	entryToolResult
	entryError
	entryNote
)

// entry is one committed line-block in the transcript. text is the body; depth is the
// sub-agent nesting level (Phase 3 — Phase 2 tolerates depth > 0 without rendering it
// richly, per the plan's Depth > 0 rule).
type entry struct {
	kind  entryKind
	text  string
	depth int
}

// addUser appends a user message — the text the human submitted to open or continue the
// Exchange. Called from the submit path, not the event fold.
func (t *transcript) addUser(text string) {
	t.entries = append(t.entries, entry{kind: entryUser, text: text})
}

// addNote appends a neutral note (e.g. "cancelled") — a transcript record of a UI-level
// event that is not itself an engine Event.
func (t *transcript) addNote(text string) {
	t.entries = append(t.entries, entry{kind: entryNote, text: text})
}

// apply folds one engine Event into the transcript (the C6 rule). The switch is
// exhaustive over all eight variants so the set stays honest as the engine evolves. Each
// case records the Turn index (it drives the status line) and then folds the event: tokens
// grow the in-progress buffer; a StreamReset discards it; a Message commits it (canonical
// text); the first ToolCall of a Turn finalises the pre-tool narration before recording the
// call; results, approvals, and recovered faults append their own entries; a
// MechanismFired is surfaced only in the debug view. It renders only — no agent logic (C5).
func (t *transcript) apply(e domain.Event) {
	switch e := e.(type) {
	case domain.TokenEvent:
		t.turn = e.Turn
		t.appendToken(e.Text)
	case domain.StreamResetEvent:
		t.turn = e.Turn
		t.discardPending()
	case domain.MessageEvent:
		t.turn = e.Turn
		t.commitAssistant(e.Text, e.Depth)
	case domain.ToolCallEvent:
		t.turn = e.Turn
		t.finalizeNarration(e.Depth)
		t.addToolCall(e.Call, e.Depth)
	case domain.ToolResultEvent:
		t.turn = e.Turn
		t.addToolResult(e.Result, e.Depth)
	case domain.ApprovalEvent:
		t.turn = e.Turn
		t.addApproval(e.Request, e.Decision, e.Depth)
	case domain.MechanismFiredEvent:
		t.turn = e.Turn
		t.addMechanism(e)
	case domain.ErrorEvent:
		t.turn = e.Turn
		t.addError(e.Source, e.Err, e.Depth)
	default:
		// An unknown future variant: tolerate it. The set is sealed and additively
		// versioned, so an unrecognised Event is rendered as nothing rather than a panic.
	}
}

// appendToken grows the in-progress assistant buffer with one streamed chunk. The buffer
// is committed by commitAssistant (a MessageEvent) or finalizeNarration (the first
// ToolCall of the Turn), and is never rendered as a committed entry until then.
func (t *transcript) appendToken(text string) {
	t.streaming = true
	t.pending += text
}

// discardPending drops the in-progress assistant buffer for the current Turn. A
// StreamResetEvent signals the loop is re-streaming the Turn (an ActionRetry post-response
// decision re-called the Upstream), so the tokens accumulated so far are superseded and
// must never be committed (events.go contract). The re-stream's tokens arrive next and the
// Turn's MessageEvent carries the final, accepted text.
func (t *transcript) discardPending() {
	t.streaming = false
	t.pending = ""
}

// commitAssistant finalises the streamed buffer into a committed assistant entry on a
// MessageEvent. The MessageEvent's text is canonical (it carries the full, accepted
// message), so it is preferred over the accumulated tokens; the tokens are a live preview
// that should reconcile to the same text (§0 event-sequence rule). An empty canonical text
// falls back to the accumulated tokens so nothing streamed is lost.
func (t *transcript) commitAssistant(canonical string, depth int) {
	text := canonical
	if text == "" {
		text = t.pending
	}
	t.entries = append(t.entries, entry{kind: entryAssistant, text: text, depth: depth})
	t.streaming = false
	t.pending = ""
}

// finalizeNarration commits the in-progress buffer as the pre-tool narration when the first
// ToolCallEvent of a Turn arrives (the C6 rule). A tool Turn emits no MessageEvent, so the
// streamed tokens are the canonical narration text. Only the first ToolCall finalises:
// afterwards streaming is false, so the Turn's remaining ToolCalls add no empty entry. A
// Turn that streamed nothing before its tool call commits nothing.
func (t *transcript) finalizeNarration(depth int) {
	if !t.streaming {
		return
	}
	text := t.pending
	t.streaming = false
	t.pending = ""
	if text == "" {
		return
	}
	t.entries = append(t.entries, entry{kind: entryAssistant, text: text, depth: depth})
}

// addToolCall appends a tool-call entry: the tool name followed by its pretty-printed
// arguments. The model's requested call is shown verbatim (a malformed argument is rendered
// as-is rather than hidden — the human approving a write must see exactly what was asked).
func (t *transcript) addToolCall(call domain.ToolCall, depth int) {
	t.entries = append(t.entries, entry{kind: entryToolCall, text: formatToolCall(call), depth: depth})
}

// addToolResult appends the paired tool-result entry. A result the tool flagged as an error
// (IsError) is a normal in-band outcome the model will see and react to — not a recovered
// fault (that is ErrorEvent) — so it is marked but still rendered as a result.
func (t *transcript) addToolResult(result domain.ToolResult, depth int) {
	text := result.Content
	if result.IsError {
		text = "error: " + text
	}
	t.entries = append(t.entries, entry{kind: entryToolResult, text: text, depth: depth})
}

// addApproval records an Approval observationally — the decision already came back through
// the C3 reply channel, so this is a transcript record of what was decided, not the gate.
func (t *transcript) addApproval(req domain.ApprovalRequest, decision domain.ApprovalDecision, depth int) {
	text := fmt.Sprintf("approval %s: %s", decision, req.Tool)
	t.entries = append(t.entries, entry{kind: entryNote, text: text, depth: depth})
}

// addMechanism records a fired Mechanism, but only in the debug view (off by default).
// There is no Mechanism catalogue until Phase 4, so a fired event is observability noise
// for the product UI; the switch handles it now so a Phase-4 Mechanism needs no retrofit.
func (t *transcript) addMechanism(e domain.MechanismFiredEvent) {
	if !t.debug {
		return
	}
	text := fmt.Sprintf("mechanism %s @ %s: %s", e.Mechanism, e.Hook, e.Action)
	t.entries = append(t.entries, entry{kind: entryNote, text: text, depth: e.Depth})
}

// addError appends a recovered-fault notice (ADR 0007 — an ErrorEvent does not stop the
// loop). source is the tool name / mechanism ID / "loop"; msg is the error text.
func (t *transcript) addError(source, msg string, depth int) {
	t.entries = append(t.entries, entry{kind: entryError, text: source + ": " + msg, depth: depth})
}

// ----------------------------------------------------------------------------
// Formatting helpers
// ----------------------------------------------------------------------------

// formatToolCall renders a tool call as "<tool> <arguments>", with the arguments
// pretty-printed. A call with no arguments shows just the tool name.
func formatToolCall(call domain.ToolCall) string {
	args := prettyJSON(call.Arguments)
	if args == "" {
		return call.Tool
	}
	return call.Tool + " " + args
}

// prettyJSON re-renders raw JSON arguments as indented, human-readable text. Empty or null
// arguments render as nothing; arguments that do not parse are returned trimmed-but-verbatim
// so a malformed tool argument is still shown rather than silently dropped.
func prettyJSON(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(trimmed), "", "  "); err != nil {
		return trimmed
	}
	return buf.String()
}

// ----------------------------------------------------------------------------
// Rendering
// ----------------------------------------------------------------------------

// render joins the committed entries and the in-progress assistant buffer into the body
// the viewport displays. The viewport soft-wraps to its width, so render leaves wrapping
// to it and only labels each entry.
func (t *transcript) render() string {
	var b strings.Builder
	for i, e := range t.entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(renderEntry(e))
	}
	if t.streaming {
		if len(t.entries) > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(renderEntry(entry{kind: entryAssistant, text: t.pending}))
	}
	return b.String()
}

// entryLabels maps each entry kind to its transcript prefix. They are short and plain so
// the rendered text stays readable and the substring assertions in the tests stay stable.
var entryLabels = map[entryKind]string{
	entryUser:       "you",
	entryAssistant:  "apogee",
	entryToolCall:   "tool",
	entryToolResult: "result",
	entryError:      "error",
	entryNote:       "·",
}

// renderEntry formats one entry as "<label>  <body>" with the label styled. depth > 0
// (a sub-agent's event, Phase 3) indents the block — including the continuation lines of a
// multi-line body — so a nested stream does not corrupt the top-level layout, without yet
// rendering the nesting richly.
func renderEntry(e entry) string {
	label := labelStyle(e.kind).Render(entryLabels[e.kind])
	indent := strings.Repeat("  ", e.depth)
	body := e.text
	if indent != "" {
		body = strings.ReplaceAll(body, "\n", "\n"+indent)
	}
	return indent + label + "  " + body
}

// labelStyle is the lipgloss style for an entry's label. It is intentionally minimal
// (a weight cue only) — themed colours are a later polish, and keeping it spare keeps the
// rendered transcript legible under any terminal profile.
func labelStyle(k entryKind) lipgloss.Style {
	switch k {
	case entryUser:
		return lipgloss.NewStyle().Bold(true)
	case entryError:
		return lipgloss.NewStyle().Bold(true)
	default:
		return lipgloss.NewStyle().Faint(true)
	}
}

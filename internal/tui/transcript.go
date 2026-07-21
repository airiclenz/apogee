package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

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

// entry is one committed line-block in the transcript. text is the body (for the text
// kinds); depth is the sub-agent nesting level (Phase 3). A tool call carries its
// presentation view and a callID so the paired result can be folded into the same block:
// callID matches the result by ToolCall.ID, and done marks the call once its result has
// arrived (so a re-used tool pairs each result with the right call).
type entry struct {
	kind   entryKind
	text   string
	depth  int
	callID string
	tool   toolView
	done   bool
	skills []string // entryUser only: display names of the skills attached to this send
}

// addUser appends a user message — the text the human submitted to open or continue the
// Exchange, plus the display names of any skills attached to it (rendered as chips on the
// block so the attachment stays visible after the send; nil when none). Called from the submit
// path, not the event fold. The skill display names are untrusted (they come from a
// repo-supplied SKILL.md front-matter), so they are escape-stripped like model text — an
// attacker cannot smuggle a terminal control sequence into the transcript through a chip.
func (t *transcript) addUser(text string, skills []string) {
	t.entries = append(t.entries, entry{kind: entryUser, text: text, skills: stripEscapesAll(skills)})
}

// addNote appends a neutral note (e.g. "cancelled") — a transcript record of a UI-level
// event that is not itself an engine Event.
func (t *transcript) addNote(text string) {
	t.entries = append(t.entries, entry{kind: entryNote, text: text})
}

// apply folds one engine Event into the transcript (the C6 rule). The switch covers the
// eight transcript-rendered variants of the eleven-variant Event set, so the rendered set
// stays honest as the engine evolves; the other three are not transcript entries
// (ReasoningEvent feeds the activity line, UsageEvent the status-line stats, AuditEvent
// nothing in the TUI) and fall to the default case with every future variant. Each
// case folds its event: tokens grow the in-progress buffer; a StreamReset discards it; a
// Message commits it (canonical text); the first ToolCall of a Turn finalises the pre-tool
// narration before recording the call; results, approvals, and recovered faults append
// their own entries; a MechanismFired is surfaced only in the debug view. It renders only —
// no agent logic (C5).
func (t *transcript) apply(e domain.Event) {
	switch e := e.(type) {
	case domain.TokenEvent:
		t.appendToken(e.Text)
	case domain.StreamResetEvent:
		t.discardPending()
	case domain.MessageEvent:
		t.commitAssistant(e.Text, e.Depth)
	case domain.ToolCallEvent:
		t.finalizeNarration(e.Depth)
		t.addToolCall(e.Call, e.Depth)
	case domain.ToolResultEvent:
		t.addToolResult(e.Result, e.Depth)
	case domain.ApprovalEvent:
		t.addApproval(e.Request, e.Decision, e.Depth)
	case domain.MechanismFiredEvent:
		t.addMechanism(e)
	case domain.ErrorEvent:
		t.addError(e.Source, e.Err, e.Depth)
	default:
		// An unknown future variant: tolerate it. The set is sealed and additively
		// versioned, so an unrecognised Event is rendered as nothing rather than a panic.
	}
}

// appendToken grows the in-progress assistant buffer with one streamed chunk. The buffer
// is committed by commitAssistant (a MessageEvent) or finalizeNarration (the first
// ToolCall of the Turn), and is never rendered as a committed entry until then. The chunk is
// escape-stripped as it lands (stripEscapes) so no ESC byte from the model's stream ever
// reaches the terminal — even split across two chunks, since the byte is removed per chunk.
func (t *transcript) appendToken(text string) {
	t.streaming = true
	t.pending += stripEscapes(text)
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
	// canonical is the MessageEvent's untrusted model text; strip its escapes (t.pending was
	// already stripped as it streamed, so a double-strip there is a cheap no-op).
	text = stripEscapes(text)
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

// addToolCall appends a tool-call entry: the presentation view (friendly label + target)
// built from the model's requested call, plus the call ID the paired result folds into. The
// view shows the call verbatim where it cannot summarise it (a malformed argument is rendered
// as-is rather than hidden — the human approving a write must see exactly what was asked).
func (t *transcript) addToolCall(call domain.ToolCall, depth int) {
	t.entries = append(t.entries, entry{
		kind:   entryToolCall,
		depth:  depth,
		callID: call.ID,
		tool:   presentToolCall(call),
	})
}

// addToolResult folds a tool result into its call's block. It scans from the tail for the
// most recent un-paired tool-call entry with a matching CallID and enriches that call's view
// with the result's one-line summary, marking it done. A result the tool flagged as an error
// (IsError) is a normal in-band outcome the model reacts to — not a recovered fault (that is
// ErrorEvent) — so it is summarised, not raised. A result that matches no open call (the
// defensive orphan case) is appended as a standalone result block so its outcome is not lost.
func (t *transcript) addToolResult(result domain.ToolResult, depth int) {
	for i := len(t.entries) - 1; i >= 0; i-- {
		e := &t.entries[i]
		if e.kind == entryToolCall && !e.done && e.callID == result.CallID {
			e.tool.enrichWithResult(result)
			e.done = true
			return
		}
	}
	text := result.Content
	if result.IsError {
		text = "error: " + text
	}
	t.entries = append(t.entries, entry{kind: entryToolResult, text: text, depth: depth})
}

// hasOpenToolCall reports whether any tool-call entry is still waiting for its result — the
// signal the live status line uses to stay on the tool phrase while a batch of calls runs
// (foldActivity). It reads the same call/result pairing addToolResult maintains, so a call is
// "open" from the moment it is recorded until its result folds into it. A call whose result
// never arrived (a run cancelled mid-tool) stays open forever, which at worst holds the tool
// phrase one event longer after some later result; the next reasoning/token/message event
// moves it on.
func (t *transcript) hasOpenToolCall() bool {
	for i := len(t.entries) - 1; i >= 0; i-- {
		if e := &t.entries[i]; e.kind == entryToolCall && !e.done {
			return true
		}
	}
	return false
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

// stripEscapes removes the ESC control byte (0x1b) from untrusted text so a model- or
// repo-supplied string can never introduce a terminal escape sequence — an OSC 52 clipboard
// write (\x1b]52;...), a CSI cursor/screen game — at the transcript boundary. Every ANSI
// sequence begins with ESC, so dropping that one byte neutralises the sequence regardless of
// how a streamed chunk split it, while leaving ordinary text (including \n and \t) intact. The
// styling the renderer adds afterwards is applied by lipgloss to already-stripped text, so its
// own escapes are unaffected. Not exploitable in the current layout (the footer always renders
// after transcript content, and the cellbuf drops non-SGR escapes when printable cells follow),
// but a trailing-position escape DOES survive the cellbuf — this closes that gap at the source.
func stripEscapes(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s // the overwhelmingly common case: no ESC, no allocation
	}
	return strings.ReplaceAll(s, "\x1b", "")
}

// stripEscapesAll escape-strips every string in xs, returning a new slice (nil for nil), so a
// batch of untrusted labels (attached-skill display names) is sanitized in one call.
func stripEscapesAll(xs []string) []string {
	if xs == nil {
		return nil
	}
	out := make([]string, len(xs))
	for i, s := range xs {
		out[i] = stripEscapes(s)
	}
	return out
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

package tui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The transcript (phase-2 detail plan §3 C6)
// ----------------------------------------------------------------------------

// transcript is the scrollback model: an append-only list of typed entries plus a
// single in-progress assistant buffer fed by streamed TokenEvents. It is the C6
// rendering model the viewport displays. P2.2 builds the structure and folds the
// streaming-text path (tokens → assistant message); the full event fold — discarding the
// buffer on a StreamResetEvent, finalising it on the first ToolCallEvent of a Turn, and
// the rich tool/result/mechanism entries — lands in P2.3. The structure is stable for it.
type transcript struct {
	entries   []entry         // committed, in display order
	pending   strings.Builder // in-progress assistant tokens for the current Turn
	streaming bool            // whether pending holds an un-committed assistant buffer
	turn      int             // the latest Turn index seen (drives the status line)
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
// exhaustive over all eight variants so the set stays honest as the engine evolves; P2.2
// implements the streaming-text and error paths and records each Turn index, leaving the
// tool/approval/mechanism/reset bodies for P2.3 (no default-Mechanism emits ActionRetry in
// Phase 2, so deferring the StreamResetEvent discard is safe). It renders only — it holds
// no agent logic (C5).
func (t *transcript) apply(e domain.Event) {
	switch e := e.(type) {
	case domain.TokenEvent:
		t.turn = e.Turn
		t.appendToken(e.Text)
	case domain.StreamResetEvent:
		t.turn = e.Turn
		// P2.3: discard the in-progress token buffer for the Turn — an ActionRetry
		// re-stream supersedes the tokens accumulated so far (events.go contract).
	case domain.MessageEvent:
		t.turn = e.Turn
		t.commitAssistant(e.Text)
	case domain.ToolCallEvent:
		t.turn = e.Turn
		// P2.3: finalise the pre-tool narration (first ToolCall of the Turn), then
		// render the tool call (tool name + pretty-printed Arguments).
	case domain.ToolResultEvent:
		t.turn = e.Turn
		// P2.3: render the paired tool result.
	case domain.ApprovalEvent:
		t.turn = e.Turn
		// P2.3: record the decision observationally (the gate itself is the C3 rendezvous).
	case domain.MechanismFiredEvent:
		t.turn = e.Turn
		// P2.3: surface only in a debug view (no Mechanism catalogue until Phase 4).
	case domain.ErrorEvent:
		t.turn = e.Turn
		t.addError(e.Source, e.Err)
	default:
		// An unknown future variant: tolerate it. The set is sealed and additively
		// versioned, so an unrecognised Event is rendered as nothing rather than a panic.
	}
}

// appendToken grows the in-progress assistant buffer with one streamed chunk. The buffer
// is committed by commitAssistant (a MessageEvent) — or, in P2.3, by the first ToolCall of
// the Turn — and is never rendered as a committed entry until then.
func (t *transcript) appendToken(text string) {
	t.streaming = true
	t.pending.WriteString(text)
}

// commitAssistant finalises the streamed buffer into a committed assistant entry. The
// MessageEvent's text is canonical (it carries the full, accepted message), so it is
// preferred over the accumulated tokens; the tokens are a live preview that should
// reconcile to the same text (§0 event-sequence rule). An empty canonical text falls back
// to the accumulated tokens so nothing streamed is lost.
func (t *transcript) commitAssistant(canonical string) {
	text := canonical
	if text == "" {
		text = t.pending.String()
	}
	t.entries = append(t.entries, entry{kind: entryAssistant, text: text})
	t.streaming = false
	t.pending.Reset()
}

// addError appends a recovered-fault notice (ADR 0007 — an ErrorEvent does not stop the
// loop). source is the tool name / mechanism ID / "loop"; msg is the error text.
func (t *transcript) addError(source, msg string) {
	t.entries = append(t.entries, entry{kind: entryError, text: source + ": " + msg})
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
		b.WriteString(renderEntry(entry{kind: entryAssistant, text: t.pending.String()}))
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
// (a sub-agent's event, Phase 3) is indented so a nested stream does not corrupt the
// top-level layout, without yet rendering the nesting richly.
func renderEntry(e entry) string {
	label := labelStyle(e.kind).Render(entryLabels[e.kind])
	indent := strings.Repeat("  ", e.depth)
	return indent + label + "  " + e.text
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

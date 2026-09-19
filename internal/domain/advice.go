package domain

import (
	"strconv"
	"unicode/utf8"
)

// ----------------------------------------------------------------------------
// The advise slot and its provenance ledger (ADR 0076 D6)
// ----------------------------------------------------------------------------
//
// An advise Reaction returns text the model sees. For a tool-shaped Moment the text lands as
// a fenced trailer on the closing tool result, so the request prefix is untouched and a local
// server's prefix cache survives the Turn. The engine rides the same seam for its own
// structural text — an ENGINE NOTE (RenderEngineNote, Message.WithEngineNote), fenced under its
// own header on the closing tool result of a request that needs it (a capped delegate's wrap-up
// directive, Request.NoteOnTail) and recorded on the same ledger, so one strip and one staleness
// guard cover both. Three facts hold this together and live here:
//
//   - the fence header is derived from PROVENANCE, never from handler output, so nothing
//     out-of-process can forge an engine header by printing one;
//   - each injected span is recorded on the message it advised (Message.Advice), which is the
//     provenance ledger a bench attributes effect through — the TUI's /advice pane shows the
//     same rows to the human, read off the ReactionFiredEvent each injection books;
//   - spans are EPHEMERAL: the session record is written from the content before the first
//     fence, so a resumed conversation carries no advice and a replay never re-reads a stale
//     SHA or timestamp.

// AdviceSpan is one advice injection's ledger row: which Reaction produced the text, the
// origin and Moment it produced it under, the Turn it landed in, and where its fence begins.
//
// Offset is the byte index in the advised Message's Content at which the rendered fence
// starts — so Content[:Offset] is the message as it stood before any advice, and the first
// span's Offset is the cut the session-record strip makes. A ledger is append-ordered:
// spans[0] is the earliest injection, hence the earliest cut.
//
// Topic is set on exactly one kind of row: an ENGINE NOTE (Message.WithEngineNote) — structural
// text the engine itself fences onto a tool result, such as a capped delegate's wrap-up
// directive, its step- and token-budget notices (internal/agent, stepnotice.go) or the cut a
// settled Exchange marks on its last tool result (internal/agent, turn.go). No Reaction
// produced it, so Reaction, Moment and Turn are zero on that row and
// Origin is OriginEngine; the fence it records is RenderEngineNote's, whose header names the
// topic, never a Reaction id. It shares the ledger so the one strip (recordContent) and the one
// staleness guard (dropStaleAdvice) cover everything a message carries past its own content.
type AdviceSpan struct {
	Reaction string
	Origin   Origin
	Moment   Moment
	Turn     int
	Offset   int
	Topic    string
}

// AdviceCap is the byte ceiling on one advise handler's text (ADR 0076 D7). Output past it is
// truncated with a marker, never spilled to a file.
const AdviceCap = 8 << 10

// adviceTruncationMarker is appended by CapAdvice when, and only when, it cut.
const adviceTruncationMarker = "\n[advice truncated at 8 KiB]"

// AdviceFencePrefix and AdviceFenceClosePrefix are the fixed openings of the fence's header and
// closing lines — the bytes every rendered fence starts its two structural lines with, before the
// span's own fields fill in the rest. They are exported so the workspace context-file guard
// (internal/agent, forgesStandingStructure) can refuse a repo file that spells either one: the
// header is derived from provenance in-process, and these are what keep an out-of-process file
// from printing a convincing copy of it.
const (
	AdviceFencePrefix      = "[advice — reaction "
	AdviceFenceClosePrefix = "[end advice — "
)

// EngineNoteFencePrefix and EngineNoteFenceClosePrefix open the two structural lines of an engine
// note's fence (RenderEngineNote): `[engine — <topic>]` … `[end engine — <topic>]`. It is a fence
// of its own, deliberately NOT the advice fence: that header names the Reaction that spoke, and an
// engine note has none — what it names is the engine's own topic, so the model can tell a
// structural instruction from a Reaction's advice by the header alone.
const (
	EngineNoteFencePrefix      = "[engine — "
	EngineNoteFenceClosePrefix = "[end engine — "
)

// RenderAdvice renders one advice span as the fenced block appended to a message's Content.
// Every field of the header comes from span — the caller's text is fenced, never trusted to
// name itself — so a handler printing its own "[advice — reaction …]" line lands inside the
// fence rather than beside it.
//
// The leading blank line separates the fence from whatever the message already said; text is
// emitted verbatim between the header and the closing line, so a caller that wants it capped
// passes it through CapAdvice first.
func RenderAdvice(span AdviceSpan, text string) string {
	return "\n\n" + AdviceFencePrefix + span.Reaction + " (" + string(span.Origin) +
		" origin) at " + string(span.Moment) + ", turn " + strconv.Itoa(span.Turn) + "]\n" +
		text + "\n" + AdviceFenceClosePrefix + span.Reaction + "]"
}

// CapAdvice truncates text to at most AdviceCap bytes and appends the truncation marker when
// it cut; text already within the cap is returned unchanged. The cut lands on a rune boundary,
// so capping never leaves a half-encoded rune for the model to read.
func CapAdvice(text string) string {
	if len(text) <= AdviceCap {
		return text
	}
	cut := AdviceCap
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + adviceTruncationMarker
}

// RenderEngineNote renders one engine note as the fenced block appended to a message's Content:
// a blank line, the header naming topic, text verbatim, and the closing line naming topic again.
// The header is built here from the caller's topic — never from text — so a note's body that
// prints its own header lands inside the fence, exactly as RenderAdvice fences a handler's.
func RenderEngineNote(topic, text string) string {
	return "\n\n" + EngineNoteFencePrefix + topic + "]\n" + text + "\n" + EngineNoteFenceClosePrefix + topic + "]"
}

// WithEngineNote returns a copy of m whose Content carries text as a rendered engine-note fence
// (RenderEngineNote) and whose ledger records the note — an AdviceSpan with Topic set and
// Origin OriginEngine — at the offset that fence begins, so recordContent strips it with the
// advice and dropStaleAdvice retires it with the advice. The ledger is copied, as WithAdvice
// copies it. It never checks for an earlier note on the same topic: that is the request's
// business (Request.NoteOnTail), which owns the idempotency.
func (m Message) WithEngineNote(topic, text string) Message {
	span := AdviceSpan{Origin: OriginEngine, Offset: len(m.Content), Topic: topic}
	m.Content += RenderEngineNote(topic, text)
	m.Advice = append(append([]AdviceSpan(nil), m.Advice...), span)
	return m
}

// hasEngineNote reports whether m's ledger already carries an engine note on topic.
func (m Message) hasEngineNote(topic string) bool {
	for _, span := range m.Advice {
		if span.Topic == topic {
			return true
		}
	}
	return false
}

// NoteMessage fences text onto the committed message at index i as an engine note on topic
// (Message.WithEngineNote) — the history-side counterpart of Request.NoteOnTail, for a note that
// must ride a message already in the conversation rather than a request under construction: a
// settled Exchange's cut on its last tool result (internal/agent, turnLifecycle.settle). The
// ledger row it records is what keeps the note ephemeral — recordContent strips it from the
// session record and dropStaleAdvice retires it with a rewrite — so a noted history is a noted
// history only for the live conversation. An out-of-range i is ignored; a fresh note bumps the
// revision like every other mutation.
func (c *Conversation) NoteMessage(i int, topic, text string) {
	if i < 0 || i >= len(c.messages) {
		return
	}
	c.messages[i] = c.messages[i].WithEngineNote(topic, text)
	c.revision++
}

// WithAdvice returns a copy of m whose Content carries text as a rendered advice fence and
// whose ledger records span at the offset that fence begins. The caller's Offset is ignored —
// it is the message, not the handler, that decides where the span sits.
//
// The ledger is copied, so a caller still holding the original Message is unaffected, matching
// WithExtra's contract.
func (m Message) WithAdvice(span AdviceSpan, text string) Message {
	span.Offset = len(m.Content)
	m.Content += RenderAdvice(span, text)
	m.Advice = append(append([]AdviceSpan(nil), m.Advice...), span)
	return m
}

// recordContent is the Content a session record carries for m: everything before the first
// advice fence, so a snapshot never persists advice and a resume has nothing to drop.
//
// It clamps rather than slices blindly. A message's Content can be rewritten shorter after it
// was advised — the prune replaces an old tool result with a stub through
// Conversation.SetMessageContent — and a span's Offset does not follow it. An Offset past the
// end therefore means the fence is already gone: the content is written whole.
func (m Message) recordContent() string {
	if len(m.Advice) == 0 {
		return m.Content
	}
	offset := m.Advice[0].Offset
	if offset < 0 || offset > len(m.Content) {
		return m.Content
	}
	return m.Content[:offset]
}

// dropStaleAdvice clears the ledger when m's Content no longer reaches the first span's fence.
// Rewriting a message's content out from under its spans (the prune's stub) invalidates every
// offset at once, so the whole ledger goes rather than a corrected subset: what the model saw
// is no longer in the message to attribute.
func (m *Message) dropStaleAdvice() {
	if len(m.Advice) > 0 && m.Advice[0].Offset > len(m.Content) {
		m.Advice = nil
	}
}

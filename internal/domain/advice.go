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
// server's prefix cache survives the Turn. Three facts hold this together and live here:
//
//   - the fence header is derived from PROVENANCE, never from handler output, so nothing
//     out-of-process can forge an engine header by printing one;
//   - each injected span is recorded on the message it advised (Message.Advice), which is the
//     provenance ledger a bench attributes effect through and /settings can show;
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
type AdviceSpan struct {
	Reaction string
	Origin   Origin
	Moment   Moment
	Turn     int
	Offset   int
}

// AdviceCap is the byte ceiling on one advise handler's text (ADR 0076 D7). Output past it is
// truncated with a marker, never spilled to a file.
const AdviceCap = 8 << 10

// adviceTruncationMarker is appended by CapAdvice when, and only when, it cut.
const adviceTruncationMarker = "\n[advice truncated at 8 KiB]"

// RenderAdvice renders one advice span as the fenced block appended to a message's Content.
// Every field of the header comes from span — the caller's text is fenced, never trusted to
// name itself — so a handler printing its own "[advice — reaction …]" line lands inside the
// fence rather than beside it.
//
// The leading blank line separates the fence from whatever the message already said; text is
// emitted verbatim between the header and the closing line, so a caller that wants it capped
// passes it through CapAdvice first.
func RenderAdvice(span AdviceSpan, text string) string {
	return "\n\n[advice — reaction " + span.Reaction + " (" + string(span.Origin) +
		" origin) at " + string(span.Moment) + ", turn " + strconv.Itoa(span.Turn) + "]\n" +
		text + "\n[end advice — " + span.Reaction + "]"
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

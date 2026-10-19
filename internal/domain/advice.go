package domain

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// ----------------------------------------------------------------------------
// The advise slot and its provenance ledger (ADR 0076 D6)
// ----------------------------------------------------------------------------
//
// An advise Reaction returns text the model sees. For a tool-shaped Moment the text lands as
// a fenced trailer on the closing tool result, so the request prefix is untouched and a local
// server's prefix cache survives the Turn. For session-started (ADR 0076, 2026-10-19 amendment)
// it lands as a LEADING fence inside the user message that opens the session's next Exchange,
// ahead of the user's own text — still one user message, never an added one
// (Conversation.AdviseUserMessage). The engine rides the same seam for its own
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
//   - spans are EPHEMERAL: the session record is written from the content with every fence's
//     range removed (Message.ContentWithoutAdvice) — the leading fences cut from the front, the
//     trailers cut from the first one on — so a resumed conversation carries no advice and a
//     replay never re-reads a stale SHA or timestamp.

// AdviceSpan is one advice injection's ledger row: which Reaction produced the text, the
// origin and Moment it produced it under, the Turn it landed in, and where its fence begins.
//
// Offset is the byte index in the advised Message's Content at which the rendered fence
// starts. A TRAILING span (Leading false, every tool-shaped injection and every engine note)
// fences after the text, so the first trailing span's Offset is the cut the session-record strip
// makes: everything from it on is advice. A ledger is append-ordered, so the first trailing row
// is the earliest trailer, hence the earliest cut.
//
// A LEADING span (Leading true, the session-start slot on a user message) fences ahead of the
// text instead, and End is the byte index just past its fence and the blank line that parts it
// from the text: Content[Offset:End] is the range the strip removes. Leading spans tile the front
// of the content in ledger order — the first at 0, each next one at the previous one's End — so
// the user's own text begins at the last leading span's End. End is zero on a trailing span.
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
	Leading  bool
	End      int
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

// fenceSeparator is the blank line that parts a fence from the message text beside it: before a
// trailing fence, after a leading one.
const fenceSeparator = "\n\n"

// RenderAdvice renders one advice span as the fenced block a message's Content carries: appended
// after the text for a trailing span, set ahead of it for a leading one (span.Leading). Every
// field of the header comes from span — the caller's text is fenced, never trusted to name
// itself — so a handler printing its own "[advice — reaction …]" line lands inside the fence
// rather than beside it.
//
// A blank line separates the fence from the message's own text: before a trailing fence, after a
// leading one. Text is emitted verbatim between the header and the closing line, so a caller that
// wants it capped passes it through CapAdvice first.
func RenderAdvice(span AdviceSpan, text string) string {
	fence := AdviceFencePrefix + span.Reaction + " (" + string(span.Origin) +
		" origin) at " + string(span.Moment) + ", turn " + strconv.Itoa(span.Turn) + "]\n" +
		text + "\n" + adviceFenceClose(span)
	if span.Leading {
		return fence + fenceSeparator
	}
	return fenceSeparator + fence
}

// adviceFenceClose is the closing line of span's advice fence.
func adviceFenceClose(span AdviceSpan) string {
	return AdviceFenceClosePrefix + span.Reaction + "]"
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
// settled Exchange's cut on its last tool result, or on a cancelled recipe launch's opening
// (internal/agent, turnLifecycle.settle). The
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

// AdviseUserMessage fences text as leading advice ahead of the user message at index i
// (Message.WithAdvice with span.Leading set) — the session-start slot: the advice rides inside
// the user message that opens the session's next Exchange, ahead of the user's own text, so the
// request stays one user message and its prefix up to that message is untouched (ADR 0076,
// 2026-10-19 amendment). Successive calls land in call order, each after the fences already
// ahead of the text. The span is as ephemeral as a trailer: recordContent strips it from the
// session record, so a resume never carries it.
//
// It reports whether the advice landed: an out-of-range i, or a message that is not a user
// message, is refused and leaves the conversation — and its revision — as it was. A landing bumps
// the revision like every other mutation.
func (c *Conversation) AdviseUserMessage(i int, span AdviceSpan, text string) bool {
	if i < 0 || i >= len(c.messages) || c.messages[i].Role != RoleUser {
		return false
	}
	span.Leading = true
	c.messages[i] = c.messages[i].WithAdvice(span, text)
	c.revision++
	return true
}

// WithAdvice returns a copy of m whose Content carries text as a rendered advice fence and
// whose ledger records span at the offset that fence begins. The caller's Offset and End are
// ignored — it is the message, not the handler, that decides where the span sits.
//
// A trailing span (span.Leading false) is appended after everything the content already holds. A
// leading span is inserted ahead of the text, after any leading fences already there, and every
// row whose fence sits at or past the insertion point has its offsets shifted by the inserted
// length — so each row's fence still stands where its row says (fenceStands) and the strip still
// finds it.
//
// The ledger is copied, so a caller still holding the original Message is unaffected, matching
// WithExtra's contract.
func (m Message) WithAdvice(span AdviceSpan, text string) Message {
	if span.Leading {
		return m.withLeadingAdvice(span, text)
	}
	span.Offset, span.End = len(m.Content), 0
	m.Content += RenderAdvice(span, text)
	m.Advice = append(append([]AdviceSpan(nil), m.Advice...), span)
	return m
}

// withLeadingAdvice is WithAdvice for a leading span: the fence goes in at the end of the leading
// range (leadingEnd), and the rows at or past that point move with the bytes they fence.
func (m Message) withLeadingAdvice(span AdviceSpan, text string) Message {
	at := m.leadingEnd()
	fence := RenderAdvice(span, text)
	span.Offset, span.End = at, at+len(fence)

	ledger := make([]AdviceSpan, 0, len(m.Advice)+1)
	for _, row := range m.Advice {
		if row.Offset >= at {
			row.Offset += len(fence)
			if row.Leading {
				row.End += len(fence)
			}
		}
		ledger = append(ledger, row)
	}
	m.Content = m.Content[:at] + fence + m.Content[at:]
	m.Advice = append(ledger, span)
	return m
}

// ContentWithoutAdvice is m's Content with every advice and engine-note fence removed: the leading
// fences cut from the front and everything from the first trailing fence on cut from the back — for
// a user message, the prompt exactly as the user wrote it. A reader that judges what the user ASKED
// (the Floor guards' intent and emptiness tests, the Loop-break task quote) reads this, never
// Content, so advice words cannot move it. It is also what the session record carries
// (recordContent).
//
// It cuts only at VERIFIED offsets. A message's Content can be rewritten after it was advised —
// the prune replaces an old tool result with a stub through Conversation.SetMessageContent — and
// a span's Offset does not follow it: over a short noted body ("ok") a longer stub still reaches
// the offset, and cutting there would persist a truncated stub. The leading range is removed only
// while every leading span's fence still stands in place (leadingEnd), and the trailing cut is
// made only while the first trailing span's fence still opens at its offset (fenceStands);
// otherwise that side's fence is already gone and the content is kept whole on it.
func (m Message) ContentWithoutAdvice() string {
	start, end := m.leadingEnd(), len(m.Content)
	for _, span := range m.Advice {
		if span.Leading {
			continue
		}
		if m.fenceStands(span) && span.Offset >= start {
			end = span.Offset
		}
		break
	}
	return m.Content[start:end]
}

// recordContent is the Content a session record carries for m: ContentWithoutAdvice, so a
// snapshot never persists advice and a resume has nothing to drop.
func (m Message) recordContent() string {
	return m.ContentWithoutAdvice()
}

// leadingEnd is where m's own text begins past its leading fences: the last leading span's End
// when the leading spans tile the front of the content (the first at 0, each next at the previous
// one's End) and every one of their fences stands, else 0 — a leading range that no longer holds
// is not cut.
func (m Message) leadingEnd() int {
	end := 0
	for _, span := range m.Advice {
		if !span.Leading {
			continue
		}
		if span.Offset != end || !m.fenceStands(span) {
			return 0
		}
		end = span.End
	}
	return end
}

// dropStaleAdvice clears the ledger when any span's fence no longer stands at its offset
// (fenceStands) — the content fell short of the offset, or a rewrite put other bytes there.
// Rewriting a message's content out from under its spans (the prune's stub) invalidates every
// offset at once, so the whole ledger goes rather than a corrected subset: what the model saw
// is no longer in the message to attribute.
func (m *Message) dropStaleAdvice() {
	for _, span := range m.Advice {
		if !m.fenceStands(span) {
			m.Advice = nil
			return
		}
	}
}

// fenceStands reports whether m's Content still opens span's fence at span's Offset: the header
// RenderEngineNote writes for an engine note (Topic set), the header RenderAdvice writes for
// advice otherwise, each through the field that names the row. A leading span must also still
// close at its End — its closing line and the blank line after it end there — since the strip
// removes everything up to it. An offset outside the content never stands.
func (m Message) fenceStands(span AdviceSpan) bool {
	if span.Offset < 0 || span.Offset > len(m.Content) {
		return false
	}
	if !strings.HasPrefix(m.Content[span.Offset:], spanFenceHeader(span)) {
		return false
	}
	if !span.Leading {
		return true
	}
	if span.End < span.Offset || span.End > len(m.Content) {
		return false
	}
	return strings.HasSuffix(m.Content[span.Offset:span.End], "\n"+adviceFenceClose(span)+fenceSeparator)
}

// spanFenceHeader is the opening every rendered fence of span's kind starts with, up to the
// field that names the row — the bytes fenceStands matches at the span's offset. A leading fence
// opens on its header line itself; a trailing one opens on the blank line before it.
func spanFenceHeader(span AdviceSpan) string {
	if span.Topic != "" {
		return "\n\n" + EngineNoteFencePrefix + span.Topic + "]"
	}
	header := AdviceFencePrefix + span.Reaction + " ("
	if span.Leading {
		return header
	}
	return fenceSeparator + header
}

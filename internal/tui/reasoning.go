package tui

import (
	"unicode/utf8"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The retained reasoning tail (nothing renders it)
// ----------------------------------------------------------------------------
//
// NOTHING IN THE VIEW READS THIS BUFFER. It is the retention seam a future reasoning display will
// be built on, landed ahead of that display deliberately: where the bytes are stripped, when the
// buffer starts over and what bounds it are the rules that decide whether such a display can be
// safe at all, and settling them here, under tests, is cheaper than discovering them inside a
// renderer that is already painting. A surface that only wants to say "the model is reasoning"
// needs none of it — ARRIVAL of a [domain.ReasoningEvent] is that signal, and foldActivity paints
// "thinking" off it without reading Text at all (activity.go, events.go).
//
// Three rules, and each one is why this is a type rather than a string field on the Model:
//
//   - It is STRIPPED at this seam, not at its producer. Text on a ReasoningEvent is raw model
//     output that may carry ESC bytes (events.go says so explicitly), so it crosses stripEscapes on
//     the way in exactly as the visible token path does — doc.go's escape-strip-at-the-seam
//     invariant, extended to the one entrance reasoning has.
//   - It is BOUNDED. A Turn may reason for an hour, and the Model is copied on every Update
//     (ADR 0011), so an unbounded tail would grow every one of those copies. The buffer keeps the
//     LAST [reasoningTailCap] bytes and drops the front, because a tail shows the newest text and
//     the oldest is what any display would have scrolled off anyway.
//   - It holds ONE agent's reasoning at a time — the rule the activity line already lives by
//     (activity.go). A fan-out runs delegates concurrently (ADR 0039) and their chunks interleave
//     in one Event stream, so a buffer that concatenated them would read as a sentence nobody
//     wrote. The acting identity rides beside the text and a change of identity starts it over.
//
// The text is a plain string and never a strings.Builder: the Model reaches this by value on every
// Update (ADR 0011; doc.go; TestModelNoBuilderByValue).

// reasoningTailCap is how many bytes of reasoning the tail retains — generous for a display of the
// newest few lines, small enough that the value-copied Model carries it for nothing. Bytes rather
// than lines (the previewTailLines idea in render.go, one level down): reasoning arrives as a token
// stream with no line discipline of its own, so a chunk boundary is not a line boundary and a
// line-counted bound would be a bound on nothing in particular.
const reasoningTailCap = 4096

// reasoningTail is the current Turn's reasoning as the view retains it: the newest
// reasoningTailCap bytes, escape-stripped, of the chunks the acting agent has revealed so far.
// depth and spawn are WHOSE reasoning it holds — the emitting agent's nesting level and the id of
// the sub_agent call that spawned it (domain.EventBase.CallID), read with exactly the meaning
// activity.depth and activity.spawn carry — and a change in that pair starts the tail over.
//
// Nothing renders it; see the file narration above for what it is for. A plain value type of
// strings and ints, so it rides safely in the value-copied Model (ADR 0011).
type reasoningTail struct {
	text  string
	depth int
	spawn string
}

// append records one revealed chunk of reasoning from the agent depth and spawn identify. The
// chunk is escape-stripped at this seam, a chunk from an agent other than the one the tail holds
// starts the tail over, and what remains is bounded to reasoningTailCap bytes from the front.
func (r *reasoningTail) append(chunk string, depth int, spawn string) {
	if r.depth != depth || r.spawn != spawn {
		*r = reasoningTail{depth: depth, spawn: spawn}
	}
	r.text = keepLastBytes(r.text+stripEscapes(chunk), reasoningTailCap)
}

// reset empties the tail and forgets whose reasoning it held — the posture at every Turn boundary
// (foldReasoning) and at every worker boundary (launchExchange, [Model.finishWorker]).
func (r *reasoningTail) reset() {
	*r = reasoningTail{}
}

// keepLastBytes returns the last limit bytes of s, cut on a rune boundary: a multi-byte rune the
// cut lands inside is dropped whole rather than leaving its continuation bytes at the front, where
// they would decode as U+FFFD and paint a replacement glyph nobody's model wrote. Input already
// within the bound is returned untouched, so the ordinary case allocates nothing.
func keepLastBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	s = s[len(s)-limit:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// foldReasoning folds one engine Event into the retained reasoning tail — the fourth fold foldEvent
// runs (fold.go). Its position among the others is free, and that is worth stating in a file whose
// neighbours' order is load-bearing: it reads nothing another fold establishes, and nothing in the
// view reads what it writes.
//
// Two Events end a tail, for opposite reasons. A StreamResetEvent SUPERSEDES the Turn's stream —
// the loop is re-calling upstream, so the chunks so far describe an answer that will never exist
// (events.go). A MessageEvent COMMITS it — the reasoning is already preserved on the assistant
// message the engine just recorded (reasoning_content), so a second copy held here would only be
// the stale one. Neither is depth-scoped: the tail speaks for one agent at a time, so the boundary
// that ends any agent's Turn ends what this is holding. It mutates the local copy and returns it,
// like every Update fold.
//
// Every other variant leaves the tail alone, a PruneEvent included: pruning rewrites the tool
// results the ENGINE keeps, and the reasoning held here is the current Turn's own chunks, which
// no pruning pass has ever touched.
func (m Model) foldReasoning(e domain.Event) Model {
	switch e := e.(type) {
	case domain.ReasoningEvent:
		m.reasoning.append(e.Text, e.Depth, e.EventBase.CallID)
	case domain.StreamResetEvent, domain.MessageEvent:
		m.reasoning.reset()
	}
	return m
}

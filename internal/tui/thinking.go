package tui

import (
	"unicode/utf8"

	"github.com/airiclenz/apogee/internal/domain"
)

// The thinking board — what the /thinking pane reads
//
// This file holds the session's thinking: one record per agent per Turn, of the reasoning chunks
// that agent revealed while that Turn ran (CONTEXT.md, "Thinking channel"). It is the RETENTION
// half of the /thinking pane — the pane composes and paints rows out of what is kept here, and
// keeps none of its own — so the rules below are the rules the display lives by.
//
// It replaces the single-buffer reasoning tail this seam first landed as. That tail was retention
// with no reader, kept deliberately so its rules could settle under tests before a display was
// painting against them; the board is that display's retention and a strict superset of the tail
// (the same entrance, the same rune-safe cut, the same per-agent rule, a larger bound, plus
// completed-Turn history), so the two never stand side by side.
//
// Four rules, and each one is why this is a type rather than a pair of fields on the Model:
//
//   - Every chunk is STRIPPED at this seam, not at its producer. Text on a ReasoningEvent is raw
//     model output that may carry ESC bytes (events.go says so explicitly), so it crosses
//     stripEscapes on the way in exactly as the visible token path does — doc.go's
//     escape-strip-at-the-seam invariant, extended to the one entrance thinking has.
//   - Every record is BOUNDED, and so is the board. A Turn may reason for an hour and the Model is
//     copied on every Update (ADR 0011), so a record keeps the LAST [thinkingRecordCap] bytes and
//     drops the front, and the board keeps the newest [maxThinkingRecords] committed records and
//     drops the oldest. The two caps multiply into the ceiling the value-copied Model carries.
//   - A record holds ONE agent's thinking — the rule the activity line already lives by
//     (activity.go). A fan-out runs delegates concurrently (ADR 0039) and their chunks interleave
//     in one Event stream, so a buffer that concatenated them would read as a sentence nobody
//     wrote. The board therefore holds one IN-FLIGHT record per run, found by the run identity on
//     the chunk's own event, and both boundary Events are run-scoped: unscoped, a retrying delegate
//     would drop a sibling's in-flight text and a committing one would commit whichever agent
//     happened to stream last.
//   - A record is one TURN's thinking. The Turn index rides in from the event (domain.EventBase),
//     it is what the pane's heading spells, and a chunk arriving under a different Turn than the
//     run's live record commits that record and opens the next — the belt beside the MessageEvent
//     brace.
//
// The text is a plain string and never a strings.Builder, and the two collections are slices of
// values rather than maps: the Model reaches this by value on every Update (ADR 0011; doc.go;
// TestModelNoBuilderByValue), a map field would alias across those copies where a slice of values
// does not, and the in-flight slice is bounded by the concurrent fan-out to a handful anyway.

// thinkingRecordCap is how many bytes of thinking ONE record retains. Generous, because this text
// is READ rather than merely retained — a pane the human scrolls wants the Turn, not its last few
// lines — and still small enough that [maxThinkingRecords] of it is a ceiling the value-copied
// Model carries without thinking about it. Bytes rather than lines (the previewTailLines idea in
// render.go, one level down): reasoning arrives as a token stream with no line discipline of its
// own, so a chunk boundary is not a line boundary and a line-counted bound would be a bound on
// nothing in particular.
const thinkingRecordCap = 64 << 10

// maxThinkingRecords is how many COMPLETED records the board keeps, newest last. Full session
// history is the ratified scope, and this is the bound that makes "full" affordable: past it the
// OLDEST record is dropped, because a pane opens on the newest and the oldest is what a reader
// would have scrolled off.
const maxThinkingRecords = 64

// thinkingRecord is one agent's thinking for one Turn: whose it is, which Turn it belongs to, and
// the escape-stripped chunks that agent revealed, in the order the engine revealed them.
//
// run is the {depth, spawn} run identity every fold in the view keys on (runRef — transcript.go),
// where the ZERO value is the human's own top-level conversation; turn is domain.EventBase.Turn off
// the chunk's own event, the same stamp the Inspector's wire records carry (inspector.go). A plain
// value type of strings and ints, so it rides safely in the value-copied Model (ADR 0011).
type thinkingRecord struct {
	run  runRef
	turn int
	text string
}

// thinkingBoard is the session's thinking as the view retains it: the completed records, plus the
// in-flight one for each run currently thinking.
//
// done is in COMPLETION order, newest last — which is the order the /thinking pane opens on — and
// is bounded to [maxThinkingRecords]. live holds at most one record per run: a fan-out interleaves
// its delegates' chunks in one stream (ADR 0039), so one shared in-flight record would shred each
// agent's Turn into a record per interleaved chunk and spend the board's whole capacity in seconds.
//
// It is written by exactly one fold ([Model.foldThinking]) and by the two worker boundaries no
// Event announces (launchExchange, [Model.finishWorker]).
type thinkingBoard struct {
	done []thinkingRecord
	live []thinkingRecord
}

// append records one revealed chunk of thinking from the run that emitted it, under the Turn the
// event stamps. The chunk is escape-stripped at this seam; a chunk whose Turn differs from the
// one the run's live record holds commits that record and opens the next; a run with no live
// record gets one; and what remains is bounded to [thinkingRecordCap] bytes from the front.
func (b *thinkingBoard) append(chunk string, run runRef, turn int) {
	i := b.liveIndex(run)
	if i >= 0 && b.live[i].turn != turn {
		b.commitAt(i)
		i = -1
	}
	if i < 0 {
		b.live = append(b.live, thinkingRecord{run: run, turn: turn})
		i = len(b.live) - 1
	}
	b.live[i].text = keepLastBytes(b.live[i].text+stripEscapes(chunk), thinkingRecordCap)
}

// commit moves one run's in-flight record onto the completed list, if that run has one. A run that
// was not thinking is not an error: most Turns commit a message without ever having reasoned.
func (b *thinkingBoard) commit(run runRef) {
	if i := b.liveIndex(run); i >= 0 {
		b.commitAt(i)
	}
}

// commitAll moves EVERY in-flight record onto the completed list and leaves nothing live — the
// posture at a worker boundary, where the run is over however it ended and what each agent thought
// before dying is exactly what the pane is for. Records land in the order they opened.
func (b *thinkingBoard) commitAll() {
	for _, rec := range b.live {
		b.push(rec)
	}
	b.live = nil
}

// drop forgets one run's in-flight record without committing it — the posture for a superseded
// Turn, whose chunks describe an answer that will never exist (domain.StreamResetEvent). Other
// runs' records are untouched: a delegate retrying must not destroy a sibling's in-flight text.
func (b *thinkingBoard) drop(run runRef) {
	if i := b.liveIndex(run); i >= 0 {
		b.live = removeRecord(b.live, i)
	}
}

// commitAt pushes live record i onto the completed list and takes it out of the in-flight slice.
func (b *thinkingBoard) commitAt(i int) {
	b.push(b.live[i])
	b.live = removeRecord(b.live, i)
}

// push appends one completed record and enforces the board's record cap by dropping the oldest.
// The over-cap trim copies into a fresh array rather than resliding the old one in place, so no
// earlier copy of the value-copied Model can see its own records rewritten under it (ADR 0011).
func (b *thinkingBoard) push(rec thinkingRecord) {
	b.done = append(b.done, rec)
	if len(b.done) > maxThinkingRecords {
		b.done = append([]thinkingRecord(nil), b.done[len(b.done)-maxThinkingRecords:]...)
	}
}

// liveIndex returns the index of the in-flight record for run, or -1 when that run has none. A
// linear scan and not a map lookup: the slice is bounded by the concurrent fan-out (ADR 0039) to a
// handful, and a map field would alias across the value-copied Model where this does not.
func (b thinkingBoard) liveIndex(run runRef) int {
	for i := range b.live {
		if b.live[i].run == run {
			return i
		}
	}
	return -1
}

// removeRecord returns recs without element i, order preserved. The capped reslice is what keeps
// the tail append from writing into any array the caller's copy still shares.
func removeRecord(recs []thinkingRecord, i int) []thinkingRecord {
	return append(recs[:i:i], recs[i+1:]...)
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

// foldThinking folds one engine Event into the thinking board — one of the five folds foldEvent
// runs (fold.go). Its position among the others is free, and that is worth stating in a file whose
// neighbours' order is load-bearing: it reads nothing another fold establishes, and what it writes
// is read by the /thinking pane alone.
//
// Two Events end an in-flight record, for opposite reasons, and BOTH are scoped to the run that
// emitted them. A StreamResetEvent SUPERSEDES that run's Turn — the loop is re-calling upstream, so
// the chunks so far describe an answer that will never exist (events.go) — and the record is
// dropped. A MessageEvent COMMITS it: the Turn is over, and what the engine keeps on the assistant
// message it just recorded (reasoning_content) is history's concern rather than the view's. Under a
// fan-out, an unscoped rule would have one delegate's retry destroy a sibling's in-flight text and
// one delegate's message commit whichever agent happened to stream last (ADR 0039).
//
// Every other variant leaves the board alone, a PruneEvent included: pruning rewrites the tool
// results the ENGINE keeps, and the thinking held here is the agents' own chunks, which no pruning
// pass has ever touched. It mutates the local copy and returns it, like every Update fold.
func (m Model) foldThinking(e domain.Event) Model {
	switch e := e.(type) {
	case domain.ReasoningEvent:
		m.thinking.append(e.Text, runOf(e.EventBase), e.Turn)
	case domain.StreamResetEvent:
		m.thinking.drop(runOf(e.EventBase))
	case domain.MessageEvent:
		m.thinking.commit(runOf(e.EventBase))
	}
	return m
}

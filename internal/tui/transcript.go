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

// transcript is the scrollback model: a list of typed entries in display order plus a
// single in-progress assistant buffer fed by streamed TokenEvents. It grows at the end for
// everything the human and the top-level agent do, and at the end of a RUN for everything a
// delegate does (place), which is what keeps N concurrent children one block each. It is the C6
// rendering model the viewport displays. apply folds the full event stream into it (P2.3):
// tokens grow the in-progress buffer, which is finalised on a MessageEvent or the first
// ToolCallEvent of a Turn and discarded on a StreamResetEvent; tool calls, results,
// approvals, and recovered faults append their own entries. It renders only — no agent
// logic lives here (C5).
type transcript struct {
	entries []entry   // committed, in display order
	pending streamBuf // in-progress assistant tokens for the current Turn (a chunk list, never a
	// strings.Builder: the Model is a value type copied on every Update, and a
	// Builder forbids the copy — it panics copyCheck. It is appended in bounded
	// copies and joined once, at its commit point)
	streaming bool // whether pending holds an un-committed assistant buffer
	// pendingRun is WHOSE buffer pending holds: the run whose tokens filled it ([runRef] — the
	// nesting level and the spawning call id). It is the streaming path's half of the routing every
	// other assistant-text event already does through its Event's own base (apply), and it is needed
	// because the buffer is a single slot shared by every run: without it a delegate's live answer
	// paints as a top-level block until its MessageEvent lands, and a delegate that never sends one
	// leaves its text to be committed as the PARENT's. It means nothing while streaming is false.
	pendingRun runRef
	// parked is the in-progress text of the runs that are NOT holding the buffer right now — one
	// slot each, kept until the run commits it or ends (park / unpark). It exists because siblings
	// stream at once (ADR 0039): their token batches alternate through the single buffer above, and
	// without somewhere to set the displaced text aside every alternation would commit a fragment,
	// shredding two answers into a column of one-chunk blocks. It is normally empty — a serial
	// session never displaces anything — and it is written copy-on-write, so a Model copy that is
	// discarded rather than returned cannot leave its edit behind (ADR 0011).
	parked []parkedText
	debug  bool // when set, MechanismFiredEvents are recorded (a hidden debug view)
	// ws is the project root a tool card's paths are printed relative to (workspacepath.go), resolved
	// once at construction from Options.Workspace. It lives here because addToolCall and
	// addToolResult are reached through apply, which folds an Event with no Model in sight; it is a
	// fact about the RUN rather than about the conversation, so reset preserves it as it does debug.
	// The zero value shortens nothing, which is what a hand-built test transcript gets.
	ws workspaceRoot
	// root is the run the transcript is currently PAINTED at: the delegation whose own entries fill
	// the view, with everything above and beside it left out (render.go, [transcript.setRoot]). The
	// zero value is the whole transcript — the human's own conversation with every run folded into
	// it — which is what every session paints until a reader opens a run view (ADR 0063).
	//
	// It is view state and nothing more: no entry moves, no depth is rewritten, and the same
	// transcript rooted back at the zero value paints exactly what it painted before. That is what
	// lets a view be opened and left with no record of it anywhere but here.
	root runRef
	// paints memoises the per-block paints renderView produced last time, so a repaint mid-stream
	// costs the live tail rather than the whole scrollback (paintcache.go). It is a POINTER for the
	// reason entries is a shared backing array: the Model is copied by value on every Update (ADR
	// 0011), and every copy has to reach the one cache. It is nil on a hand-built transcript, which
	// renders uncached — every cache method is nil-safe.
	paints *paintCache
}

// streamChunkBytes is how large a [streamBuf] chunk grows before the next append starts a new one.
// It is the bound on what one append may copy: small enough that a per-flush concatenation costs
// nothing beside the render it feeds, large enough that a whole reply is a handful of chunks rather
// than one per 30 ms flush (sink.go, tokenCoalesceWindow).
const streamChunkBytes = 16 << 10

// streamBuf is the in-progress assistant text: the chunks it arrived in, plus the total byte count.
// It is deliberately not one growing string — `pending += chunk` re-copied everything the model had
// said on every sink flush (every 30 ms) and again on every park/unpark hand-over between
// concurrent siblings (ADR 0039), so receiving a long reply cost the square of its length. Here an
// append concatenates into the last chunk while that chunk is under streamChunkBytes and starts a
// new one otherwise, so one append copies at most a chunk; the text is joined exactly once, at the
// commit point that words an entry (String) or as a tail for the live preview (tail).
//
// It holds a slice header and an int and NO pointer to itself, so it rides the value-copied Model
// legally — which is why it is not a strings.Builder or a bytes.Buffer (ADR 0011: a Builder panics
// on the copy). For the same reason an append is copy-on-write over the chunk headers, as stash is
// over parked: a Model copy that is discarded rather than returned must not leave its edit in the
// live one. That copy is bounded by the NUMBER of chunks, never by the bytes they hold.
type streamBuf struct {
	chunks []string // the text in order; every chunk but the last has reached streamChunkBytes
	n      int      // total bytes held across chunks
}

// append grows the buffer by s, concatenating into the last chunk while it is still under
// streamChunkBytes and starting a new chunk otherwise.
func (b *streamBuf) append(s string) {
	if s == "" {
		return
	}
	b.n += len(s)
	if last := len(b.chunks) - 1; last >= 0 && len(b.chunks[last]) < streamChunkBytes {
		next := append(make([]string, 0, len(b.chunks)), b.chunks...)
		next[last] += s
		b.chunks = next
		return
	}
	b.chunks = append(append(make([]string, 0, len(b.chunks)+1), b.chunks...), s)
}

// appendBuf grows the buffer by everything other holds, chunk by chunk — the hand-over takePending
// makes when a run's parked text is joined back onto the live buffer it was displaced from, at no
// whole-text copy.
func (b *streamBuf) appendBuf(other streamBuf) {
	for _, chunk := range other.chunks {
		b.append(chunk)
	}
}

// Len is the number of bytes the buffer holds.
func (b streamBuf) Len() int { return b.n }

// empty reports whether the buffer holds nothing at all.
func (b streamBuf) empty() bool { return b.n == 0 }

// String joins the chunks into the whole text in one allocation. It is the COMMIT-point read — the
// moment the buffer becomes an entry's words — and a repaint must never take it: the renderer asks
// tail instead, which is the whole reason the buffer has this shape.
func (b streamBuf) String() string { return strings.Join(b.chunks, "") }

// tail returns the last lines+1 raw lines of the buffer, walking the chunks from the END and
// joining only the ones the cut reaches; a buffer holding fewer lines than that is returned whole.
// It is the live preview's input (render.go, previewTail), so a repaint costs a viewport rather
// than a reply. The one extra line is the margin previewTail needs: it holds the trailing blank
// lines back before taking its own last lines, so the cut here must sit above what it keeps.
func (b streamBuf) tail(lines int) string {
	need := lines + 1
	for i := len(b.chunks) - 1; i >= 0; i-- {
		chunk := b.chunks[i]
		for end := len(chunk); end > 0; {
			nl := strings.LastIndexByte(chunk[:end], '\n')
			if nl < 0 {
				break
			}
			if need--; need == 0 {
				return strings.Join(append([]string{chunk[nl+1:]}, b.chunks[i+1:]...), "")
			}
			end = nl
		}
	}
	return b.String()
}

// runRef is WHERE an event came from — the two facts on [domain.EventBase] that together name the
// agent that emitted it: its sub-agent nesting level, and the id of the sub_agent tool call that
// spawned it. The zero value is the human's own top-level conversation (depth 0, no spawning call),
// which is what a hand-built transcript and every event of a session that delegated nothing carry.
//
// Depth alone stopped being an identity when siblings started running at once (ADR 0039): two
// children of one reply stand at the SAME depth and their events interleave, so every fold that
// used to key on depth — which buffer these tokens grow, which run this entry belongs to, which run
// a context reading fills — keys on the pair instead. It is comparable, so "the same run" is one
// `==`.
type runRef struct {
	depth int
	spawn string
}

// runOf reads the run an Event was emitted by off its base. It is the one place the transcript
// turns an Event into a run identity, so the fold helpers below all speak in runs.
func runOf(base domain.EventBase) runRef {
	return runRef{depth: base.Depth, spawn: base.CallID}
}

// parkedText is one run's in-progress assistant text, set aside because ANOTHER run's tokens took
// the single live buffer while it was streaming (transcript.parked). It is committed when that run
// next commits — its MessageEvent, its next tool call — or when the run ends without ever having
// done so (closeRun), which is what keeps a cancelled delegate's half-sentence its own.
type parkedText struct {
	run  runRef
	text streamBuf
}

// entry is one committed line-block in the transcript. text is the body (for the text
// kinds); depth is the sub-agent nesting level (Phase 3). A tool call carries its
// presentation view and a callID so the paired result can be folded into the same block:
// callID matches the result by ToolCall.ID, and done marks the call once its result has
// arrived (so a re-used tool pairs each result with the right call). A presented document
// carries no text at all: its facts live in presented, the view render.go composes from.
//
// A scheduled Firing (entrySchedule) borrows those same three slots and nothing else: its tool
// holds the firing block's view, its callID is the SCHEDULE's id — the key the completed / failed
// Event pairs by, one open block per Schedule since a Schedule fires serially — and done marks the
// Firing returned (schedule.go, foldScheduleEvent). It is deliberately not an entryToolCall: a
// Firing is a separate headless run rather than this session's tool, so the pairing the live status
// line, the tool-call grouping and the sub-agent span all key on stays uncontaminated by it
// (ADR 0033).
//
// ephemeral marks an entry as display-only: it renders exactly like its kind normally does, but
// encodeTranscript never writes it to the session record. It generalizes the entryStartup
// exclusion — the box is opening chrome that is re-seeded on every launch, and a resume-time
// notice is the same thing one kind over: re-derived from live state at the moment the view is
// rebuilt, so persisting it would append a fresh copy on every resume until the record was a
// column of "resumed:" notes.
//
// phase is the DELEGATION's own lifecycle state on a sub_agent call block, as its child reported it
// (domain.SubAgentPhaseEvent, addSubAgentPhase): "" for a delegation that is still only requested,
// started once its child is actually running, finished once that child's report is in hand. It is a
// different fact from done beside it, and both are kept: done is the CALL/RESULT PAIRING the whole
// transcript keys on — the result folded in, the run closed — and it lands with the group's trailing
// result burst, long after an early finisher stopped working (ADR 0039 decision 4). The phase is the
// timing that burst deliberately does not carry, so the display asks it rather than done wherever the
// question is "is this delegation over?" (subAgentReported).
//
// It is view-only and unpersisted for expanded's reasons: a replayed record's delegations all carry
// their results already, so a resumed session reads their doneness off the pairing exactly as it did
// before the phase existed.
//
// expanded is the block's VIEW state and nothing else (the kinds carriesBlockState admits, setExpanded
// its one writer): false — the zero value, and therefore the default for every entry however it was
// born, folded mid-flight or replayed from a record — is the collapsed, compact paint; true paints
// the block's retained body in full (layout.md, "Collapsed and expanded blocks"). It sits beside done
// because it is the same kind of per-entry fact the painter reads off the shared entries slice,
// and it is deliberately absent from the wire form — the state is the view's alone, so a resumed
// session paints everything collapsed and /clear forgets it with everything else.
//
// typeExpanded is the second, INDEPENDENT view state of the same entry, and it means something only
// where the entry heads a run inside a super-group (toolSuperGroup): whether that run's TYPE ROW is
// open, showing its member rows, or closed to the one aggregated row the umbrella lists it as
// (docs/layout/tool-layout.md, "Grouped tools expanded 1st step"). It is a separate field rather
// than a re-use of expanded because the two levels nest — a type row is opened to reveal its
// members, and a member is then opened to reveal its own body — so one flag could not say which of
// the two steps the reader took. It is view-only and unpersisted for exactly expanded's reasons.
//
// taskExpanded is the THIRD such state, and it means something only on the head of a run whose VIEW
// is open (ADR 0063): whether the task that run was handed, painted as the user row it is directly
// under the breadcrumb (render.go's rooted paint), stands folded to the collapsed cap or open in
// full. It is a separate field for typeExpanded's reason and one more: expanded is refused outright
// on a head with a run to open (setExpanded), so the in-view fold could not borrow it without
// re-opening the door that refusal closes — a run has two shapes and no third, and a task the
// reader unfolded inside a view is a fact about the view, not a rail in the conversation below it.
// It is view-only and unpersisted for exactly expanded's reasons.
//
// ctxUsed / ctxLimit are the CHILD's context fill on the head of a sub-agent run (applyUsage): how
// much of its window the delegate had filled when it last reported, and the window that reading
// filled — the CHILD's own, which for a run routed to the Sub-agent server is the Delegation
// target's and not the session's (ADR 0045). They are a pair by necessity — a fill says nothing
// without its limit — so they are captured together at fold time and frozen there, which is what
// keeps a finished run's history out of reach of a later window rebind.
type entry struct {
	kind   entryKind
	text   string
	depth  int
	callID string
	// spawnCallID is the RUN this entry belongs to: the id of the sub_agent call that spawned
	// the agent whose event folded into it (domain.EventBase.CallID), empty for the human's own
	// top-level conversation. It is what tells one delegated run's entries from another's when
	// several children run at once (ADR 0039) — depth cannot, because siblings share it — and it
	// is a different fact from callID above, which is the entry's OWN tool call.
	spawnCallID string
	tool        toolView
	done        bool
	// the head of a sub-agent run only: the delegation's lifecycle phase as its child reported it
	// (domain.SubAgentPhaseEvent); view-only liveness beside done's pairing, never persisted
	phase    domain.SubAgentPhase
	expanded bool // view-only block state: false = collapsed (the default); never persisted
	// view-only state of the TYPE ROW this entry heads inside a super-group; never persisted
	typeExpanded bool
	// view-only fold of the TASK this entry's run was handed, as the run's own view paints it
	// above the child's work (render.go's rooted paint); never persisted
	taskExpanded bool
	ephemeral    bool // display-only: rendered, never persisted (see encodeTranscript)
	// entryUser / entryInterjected: where the skills this message invoked sit IN text — one span
	// per occurrence
	skillSpans []skillSpan
	presented  presentedView
	startup    startupView // entryStartup only: the one-time start-up box's logo + session facts
	// the head of a sub-agent run only: the child's latest context reading and the CHILD's own
	// window it filled (the Delegation target's where the run was routed), frozen together when
	// the reading folded (applyUsage)
	ctxUsed  int
	ctxLimit int
	// ctxModel is the head of a sub-agent run only, and only where it has something to say: the
	// model the child ran on WHEN that was not the session's own at the moment the reading folded
	// — a delegation routed to the Sub-agent server (ADR 0045) — and empty when the two matched.
	// The comparison is frozen with the fill rather than made at paint for the reason the fill is:
	// what a run says about itself is what was true while it ran, so a later /model switch, a
	// rebind, or a resume into a differently-bound session cannot rewrite a finished run's history.
	ctxModel string
	// the head of a sub-agent run only: the CHILD's cumulative token accounting for the whole run
	// (usageTotals, fold.go), folded latest-wins from the same readings — including the maintenance
	// ones the fill above skips. It is what makes a delegate's spend reportable per agent long after
	// its run closed, where ctxUsed only ever says how full its window was at the end.
	usage usageTotals
}

// skillSpan is one invoked "/token" LOCATED in a sent message's text: the byte range [start,end)
// the token occupies, so the block can paint that very run of the text in the skill colour instead
// of restating the invocation beside it.
//
// It is a send-time VERDICT, captured where the parse layer resolved the token against the catalog
// (parseInput → [refs.Span]) and carried with the message from then on — never a lookup the painter
// repeats. That is what makes it the record of what the model was actually given: a replayed
// session paints the same tokens it painted the day they were sent, even if the skill has since
// been renamed, removed, or shadowed by a workspace that no longer ships it.
//
// Every occurrence has its own span, which is exactly where spans part company with the invocation
// itself: a skill named twice in one message is invoked once (the de-duped id list the engine is
// handed) and painted twice. Spans are the ONLY thing a sent entry keeps about its skills — the
// block says what was invoked by lighting up the words the human wrote, so there is nothing left
// for a display name to do here.
type skillSpan struct{ start, end int }

// spansWithin keeps the spans that still LOCATE a run of text — well formed, and inside its bounds
// — and drops the rest. It is the transcript boundary's check on an offset, the counterpart of
// stripEscapes on a string: spans arrive from the parse layer, but also from a session file on
// disk, whose text is re-stripped on the way in (fromWireEntry) and so need no longer be byte-for-
// byte the text the offsets were measured against. A dropped span simply paints nothing, which is
// the plain block an entry recorded before spans existed already renders as.
func spansWithin(text string, spans []skillSpan) []skillSpan {
	var kept []skillSpan
	for _, sp := range spans {
		if sp.start >= 0 && sp.start < sp.end && sp.end <= len(text) {
			kept = append(kept, sp)
		}
	}
	return kept
}

// presentedView is the presentation model of a shown document (entryPresented only): the
// deliverable's own name, where it lives, and what the host managed to do with it. It is the
// [toolView] of a presentation — the entry holds the facts and render.go turns them into lines,
// so the wording and the shape stay table-testable without a Model (ADR 0019 §2, rung 0).
//
// Path and Location are carried VERBATIM and rendered as plain text: terminal linkification is
// the whole mechanism, so nothing here may clip, wrap or decorate them.
type presentedView struct {
	Title    string               // the model's optional label; empty when it named none
	Path     string               // the workspace-relative path — always present; its own line under a title, else beside the ▤ marker
	Location string               // the served URL (rung 2); empty on every other rung
	Method   domain.PresentMethod // the rung reached, which the closing status line words
	Reason   string               // why a tried rung did not deliver; empty when none was
}

// startupView is the presentation model of the one-time start-up box (entryStartup only): the
// embedded logo art plus the session facts the box shows. Like [presentedView] the entry holds the
// facts and render.go composes the card, so the box's shape and wording stay table-testable without
// a Model (ADR 0019 §2, rung 0). The box is seeded as entries[0] (newModel, and re-seeded by
// startNewSession on /clear) and rendered fresh at the live width on every repaint, so it reflows on
// resize and reprints on a session reset.
//
// Host and Model trace to config / the CLI, so addStartup escape-strips them as addPresented does
// its untrusted halves — defence in depth even though they are not model output. Logo (this
// program's own embedded asset), Context ([format.Tokens] of an int), and Version (its own build value)
// are trusted and pass through.
type startupView struct {
	Logo    string // the embedded block-art "APOGEE" wordmark
	Host    string // the upstream host label (HostAlias, or the endpoint when none)
	Model   string // the display model id (displayModel-ed)
	Context string // the formatted context-window size ([format.Tokens], e.g. "32k"); "" when unknown
	Version string // the resolved build version (Options.Version)
}

// place commits one folded entry into the run it belongs to, which is where the transcript's
// display order stops being the order events arrived in. A depth-0 entry — everything the human
// says and everything the top-level agent does — is appended, exactly as every entry always was.
// An entry carrying a spawnCallID is INSERTED at the end of its own run's stretch instead, so a
// concurrent fan-out's children each grow one contiguous block of entries behind their own
// sub_agent call block (ADR 0039 decision 6) rather than one interleaved braid behind whichever
// call happened to be announced last.
//
// An entry the run rule cannot answer for is appended — but past any HOST NOTES parked at the tail,
// when it continues the run or fan-out group those notes interrupted (tailBeforeHostNotes). A note
// answers the human and lands at the end of the list the moment it is worded, which is in the
// middle of whatever a delegate is doing; leaving it where it fell would make it a permanent
// divider through the run behind it.
//
// Contiguity is the point: every rule downstream of here — the run span, the railed frame around
// it, the folded tool run, the click surface's member offsets — reads a block off adjacent
// entries, and keeping the grouping in the LIST is what lets all of them stay exactly as they were.
// In a serial session the run's stretch IS the tail of the list, so nothing moves and nothing is
// inserted.
//
// The paint cache is the one thing an insertion invalidates: its rows are keyed by entry index, on
// the standing assumption that the list only ever grows at the end (paintcache.go), and everything
// from the insertion point on has just moved up one. dropFrom is that assumption's guard.
func (t *transcript) place(e entry) {
	at := t.runEnd(e.spawnCallID)
	if at >= len(t.entries) {
		at = t.tailBeforeHostNotes(e)
	}
	if at >= len(t.entries) {
		t.entries = append(t.entries, e)
		return
	}
	t.entries = append(t.entries, entry{})
	copy(t.entries[at+1:], t.entries[at:])
	t.entries[at] = e
	t.paints.dropFrom(at)
}

// runEnd is the index one past the last entry of the run that the sub_agent call spawn opened —
// the insertion point for that run's next entry, and the point its live preview paints at
// (renderView). The run's stretch is its head's [subAgentSpan], so this asks the same derivation
// the painter asks and the two cannot disagree about where a run ends.
//
// The end of the LIST is the answer for the top-level conversation (no spawning call), and for a
// spawning call this transcript has no head for — a replayed record written before the id existed,
// a hand-built test transcript. Both are the append every entry made before runs were grouped.
func (t *transcript) runEnd(spawn string) int {
	if spawn == "" {
		return len(t.entries)
	}
	for i := len(t.entries) - 1; i >= 0; i-- {
		if h := t.entries[i]; h.headsRunFor(spawn) {
			return i + 1 + subAgentSpan(t.entries, i)
		}
	}
	return len(t.entries)
}

// tailBeforeHostNotes is the index a tail-bound entry takes when host notes are parked at the end
// of the list: the START of that trailing note block when the entry continues a run — or a fan-out
// group — that was still open when the notes landed, and the end of the list otherwise. It is the
// half of place's contiguity rule that runEnd cannot state, because it is about the entries the
// fold commits WITHOUT a spawning call id.
//
// Two shapes break without it, and both are permanent once they happen. A sibling delegation
// announced after the note lands behind it, and [subAgentGroup] reads adjacency of BLOCKS, so the
// fan-out splits into two groups the reader was promised as one. A delegated entry whose event
// carries no call id — a serial session's child, a replayed record — lands behind it too, outside
// its head's [subAgentSpan] and so outside a collapsed run's elision, railed to nothing.
//
// The notes slide to the tail instead of the work sliding around them: a note is the HOST speaking
// to the human, so it belongs after the stretch it interrupted rather than inside a delegate's run
// (design call 4 of docs/plans/"2026-08-18 - 00 - open-defects-plan"). Nothing about the note
// itself changes — same depth 0, same text, same unrailed block.
func (t *transcript) tailBeforeHostNotes(e entry) int {
	at := len(t.entries)
	for at > 0 && isHostNote(t.entries[at-1]) {
		at--
	}
	if at == len(t.entries) || at == 0 || !t.continuesOpenRun(e, at) {
		return len(t.entries)
	}
	return at
}

// continuesOpenRun reports whether e belongs to the run, or the fan-out group, that was still open
// when the host notes standing at at landed. It is asked of an entry that has not been placed yet,
// so it reads the list as it will be around at rather than around an index of its own.
//
// A DELEGATED entry (depth above 0) qualifies when the block enclosing that position is a still-open
// delegation: the run the notes interrupted is the only run it can belong to, and the head's span
// has to reach it. A DELEGATION at the notes' own depth qualifies when the block before them is a
// still-open delegation too: it is the next member of that fan-out, and members group only while
// they stand next to each other.
//
// Everything else answers no and lands after the notes, in the order it happened — a fresh top-level
// block belongs after the note, not in front of it.
func (t *transcript) continuesOpenRun(e entry, at int) bool {
	head := -1
	switch {
	case e.depth > 0:
		head = enclosingBlock(t.entries, at, e.depth)
	case e.headsRun():
		head = prevSiblingAt(t.entries, at, e.depth)
	}
	return subAgentHeads(t.entries, head) && !t.entries[head].done
}

// isHostNote reports whether e is a host note: a note or a Firing block standing at depth 0 and
// belonging to no run (addNote, addEphemeralNote, addFiring, and a top-level approval). Those are
// the entries the program itself puts in the scrollback while the conversation is elsewhere, and
// the ones place has to step over. A note carrying a run — an approval or a fired Mechanism inside
// a delegation — is that run's own record and stays exactly where its run puts it.
func isHostNote(e entry) bool {
	return e.kind.isHostNote() && e.depth == 0 && e.spawnCallID == ""
}

// enclosingBlock is the index of the nearest block SHALLOWER than depth before at — the delegation
// head an entry at that depth landing there would hang off — or −1 when the position opens its
// level. Everything at depth or deeper before at belongs to that same enclosing block, so the walk
// steps over it.
func enclosingBlock(entries []entry, at, depth int) int {
	for j := at - 1; j >= 0; j-- {
		if entries[j].depth < depth {
			return j
		}
	}
	return -1
}

// runName is the short name the model gave the delegation that the sub_agent call spawn opened, or
// "" when it gave none. It is the status line's answer to "which delegate is this?" (activity.spawn
// → activity.text): with a fan-out running, the slot names ONE delegate at a time, and the depth it
// used to name it by is shared by every sibling.
//
// It reads the run's HEAD, the same entry runEnd derives a run's stretch from, so the phrase in the
// status line and the header of the block the work is landing in cannot come to say different
// things about whose work it is. The name is the head's live agentName rather than its Target: on a
// named call the two agree, but on an unnamed one the Target is the delegated task, and a status
// line that swapped a whole sentence in for "sub-agent" would push the context gauge off the row.
//
// "" is also the answer for the top-level conversation (no spawning call) and for a spawning call
// this transcript has no head for — a replayed record, a hand-built test transcript, a child whose
// first event beat its parent's tool call in. All three read as unnamed, which is exactly the
// phrase the status line drew before names existed.
func (t *transcript) runName(spawn string) string {
	if spawn == "" {
		return ""
	}
	for i := len(t.entries) - 1; i >= 0; i-- {
		if h := t.entries[i]; h.headsRunFor(spawn) {
			return h.tool.agentName
		}
	}
	return ""
}

// setRoot roots the paint at run r — the run view's whole mechanism (ADR 0063): from here the
// renderer paints that delegation's own entries as if they were the conversation, under the
// breadcrumb header naming the way back up. The zero value roots it at the whole transcript again,
// which is how a view is left.
//
// It invalidates nothing by hand. The root is part of every paint's key (paintcache.go), so a row
// memoised at one root is never served at another — the rooted picture of an entry is a different
// picture, rebased to the root's depth and wrapped to the wider column that leaves.
func (t *transcript) setRoot(r runRef) {
	t.root = r
}

// displace empties the live buffer slot for the run whose event is arriving, by the rule that fits
// the switch. It is the one place the transcript decides what a run hand-over MEANS, so the three
// events that can trigger one — a token, a message, a tool call — cannot come to disagree.
//
// A switch between runs at the SAME depth is concurrent siblings alternating through the single
// slot (ADR 0039): the displaced text is parked, because the run that streamed it is still going
// and committing here would shred its answer into one block per token batch.
//
// A switch across depths is the serial hand-over the buffer was built for, and keeps its original
// rule: the previous streamer is finished with the slot — a parent cannot stream while its delegate
// does — so its text is committed at once, inside its OWN run. That is what keeps an abandoned
// delegate's half-sentence the child's: without it, a delegate that faulted before its MessageEvent
// would leave text for the parent's next event to adopt as a top-level answer, or to overwrite.
func (t *transcript) displace(run runRef) {
	if !t.streaming || t.pendingRun == run {
		return
	}
	if t.pendingRun.depth == run.depth {
		t.park()
		return
	}
	open := t.pendingRun
	text := trimBlankLines(t.takePending(open))
	if text == "" {
		return
	}
	t.place(entry{kind: entryAssistant, text: text, depth: open.depth, spawnCallID: open.spawn})
}

// park sets the live buffer aside under its own run instead of committing it, and empties the
// slot for the run whose tokens are arriving. It is what a SIBLING switch does now that children
// interleave (displace): the text waits for its own run's commit point.
func (t *transcript) park() {
	if !t.streaming {
		return
	}
	if !t.pending.empty() {
		t.stash(t.pendingRun, t.pending)
	}
	t.streaming = false
	t.pending = streamBuf{}
	t.pendingRun = runRef{}
}

// stash grows the run's parked text, opening a slot for a run that has none. It rebuilds the slice
// rather than writing through it: the Model is copied by value on every Update (ADR 0011), and a
// shared backing array would carry a discarded copy's edit into the live one.
func (t *transcript) stash(run runRef, text streamBuf) {
	next := append([]parkedText(nil), t.parked...)
	for i := range next {
		if next[i].run == run {
			next[i].text.appendBuf(text)
			t.parked = next
			return
		}
	}
	t.parked = append(next, parkedText{run: run, text: text})
}

// unpark removes the run's parked text and returns it (an empty buffer when the run has none) — the
// read every commit point makes before it words its entry, so nothing this run streamed is left
// behind. It hands back the buffer itself, not its text: a hand-over moves a slice header.
func (t *transcript) unpark(run runRef) streamBuf {
	for i := range t.parked {
		if t.parked[i].run != run {
			continue
		}
		text := t.parked[i].text
		next := append([]parkedText(nil), t.parked[:i]...)
		t.parked = append(next, t.parked[i+1:]...)
		return text
	}
	return streamBuf{}
}

// takePending drains everything the run has streamed and not yet committed — its parked text plus
// the live buffer when the run is the one holding it — leaving both empty. It is the shared first
// half of the three commit points (commitAssistant, finalizeNarration, closeRun), so a run's text
// can only ever be committed once and only ever into its own block.
//
// It is the buffer's one join point: the parked text and the live buffer are appended chunk-wise
// and rendered to a string exactly once, here, where the words become an entry.
func (t *transcript) takePending(run runRef) string {
	text := t.unpark(run)
	if t.streaming && t.pendingRun == run {
		text.appendBuf(t.pending)
		t.streaming = false
		t.pending = streamBuf{}
		t.pendingRun = runRef{}
	}
	return text.String()
}

// addUser appends a user message — the text the human submitted to open or continue the
// Exchange, plus spans, where any skill invocations sit in that text ([skillSpan]; nil when none),
// so the block can light the tokens up where they were said. Called from the submit path, not the
// event fold.
//
// The spans must be offsets into THIS text: a caller that composes the message from several
// parsed inputs (joinedInterjections) re-bases them onto the composition, and spansWithin drops
// any that still fail to land.
func (t *transcript) addUser(text string, spans []skillSpan) {
	t.entries = append(t.entries, entry{
		kind:       entryUser,
		text:       text,
		skillSpans: spansWithin(text, spans),
	})
}

// addInterjected appends a message the human interjected into the running Exchange, at the point
// in the scrollback where the model actually RECEIVED it (ADR 0025) — the delivery fold calls it,
// never the staging keypress, so the transcript stays an honest record of what the model saw and
// when. It reads as the human speaking (the user block's styling) but leads with the ⧖ marker
// rather than ❯, and it is deliberately NOT an entryUser: a mid-Exchange remark must not become
// the sticky header (renderView records only entryUser blocks as such), because the prompt the
// on-screen work belongs to is still the one that opened the Exchange.
//
// It carries the spans of the skills the remark invoked exactly as addUser does, and for the same
// reason: a skill rides an interjection (ADR 0027), so the delivered block must record what the
// model was given, and a delivered remark differs from a flushed one only in when it landed.
func (t *transcript) addInterjected(text string, spans []skillSpan) {
	t.entries = append(t.entries, entry{
		kind:       entryInterjected,
		text:       text,
		skillSpans: spansWithin(text, spans),
	})
}

// addUserAt commits a message the human addressed to a RUNNING sub-agent, at the point in the
// scrollback where that child actually received it (ADR 0063). The delivery fold calls it off the
// child's domain.ChildInterjectionEvent, never the staging keypress, so the block stands for what
// the child was given and when — the same honesty addInterjected keeps for the top-level Exchange.
//
// It IS an entryUser, unlike addInterjected: as far as that child is concerned the message is the
// human speaking to it, under the same ❯ its parent's prompts wear. It still never becomes the
// sticky header, and the depth is what keeps it from becoming one — renderView records a userBlock
// only for a depth-0 prompt, because the prompt the on-screen work belongs to is the human's own
// top-level one whatever a delegate is being told (render.go).
//
// It goes in through [transcript.place] like every other delegated entry rather than being
// appended, and that is load-bearing with siblings live: an appended entry lands past the LAST
// run's stretch, where [subAgentSpan] would read it as the sibling's.
func (t *transcript) addUserAt(depth int, spawn string, in domain.UserInput) {
	t.place(entry{
		kind:        entryUser,
		text:        in.Text,
		depth:       depth,
		spawnCallID: spawn,
	})
}

// addNote appends a neutral note (e.g. "cancelled") — a transcript record of a UI-level
// event that is not itself an engine Event.
//
// It escape-strips at the SEAM, on behalf of every caller, rather than trusting each producer to
// remember (stripEscapes). A note is worded from the least trustworthy strings in the program — a
// repo SKILL.md's front matter (/skills), the model id a server advertises (rebindNote), a
// launcher profile name, an error string quoting a workspace path — and the per-producer
// discipline that preceded this had in fact missed several of them. A caller that strips first is
// harmless: stripEscapes is idempotent, and it hands its input straight back unallocated whenever
// there is nothing to rewrite — no control character, no DEL, no invalid UTF-8 byte.
func (t *transcript) addNote(text string) {
	t.entries = append(t.entries, entry{kind: entryNote, text: stripEscapes(text)})
}

// addEphemeralNote appends a note that the human sees but the session record never keeps. It is
// addNote in every respect the renderer can observe — same kind, same styling, same position in
// the scrollback — and differs only at the persistence seam, where encodeTranscript skips it.
//
// It is for notices that are RE-DERIVED at each startup or resume rather than earned by the
// conversation: the "resumed: <title>" line, its no-scrollback degrade variant, the
// interrupted-mid-exchange note, and the "context: …" notice naming the workspace files the session
// loaded. Each of those is recomputed from live state every time the view is
// rebuilt, so persisting one adds nothing on the way back in and accumulates a duplicate on the way
// out — five resumes, five stored "resumed:" notes. A note that records something that actually
// happened in the session (a cancellation, a failed save, a server switch) belongs in addNote.
//
// It escape-strips exactly as addNote does, and for a sharper reason: its two biggest callers word
// their notice from a stored session title (resumeLoaded, replayResumed) and from the workspace
// context-file names the session loaded — untrusted DISK input in both cases, since no codec
// sanitizes a session record's Meta and a repo names its own files.
func (t *transcript) addEphemeralNote(text string) {
	t.entries = append(t.entries, entry{kind: entryNote, text: stripEscapes(text), ephemeral: true})
}

// addPresented records the presentation entry for one shown document — rung 0 of the ladder,
// and the reason a failed mechanism above it is never an error (ADR 0019 §4). Like addUser it is
// called from the Update loop rather than the event fold: a presentation is the HOST's act, not
// an engine Event.
//
// The title is untrusted model text, so it is escape-stripped and clipped like any other model
// string reaching the terminal. The path and the URL are escape-stripped too — a filename is
// filesystem data, not this program's — but never clipped: a truncated path is a link that no
// longer opens, which is worse than a long one.
//
// The entry carries the presenting agent's own depth and run, and goes in through [transcript.place]
// like every delegated entry the fold commits. Both halves are load-bearing for a child's document:
// the depth is what rails the block at the level the rest of that run is drawn at (renderBlock), and
// placing it inside the run's stretch is what keeps the stretch CONTIGUOUS — a depth-0 append landing
// between a child's entries would end [subAgentSpan] there and cut the rail off mid-run.
func (t *transcript) addPresented(msg presentedMsg) {
	t.place(entry{
		kind:        entryPresented,
		depth:       msg.Depth,
		spawnCallID: msg.SpawnCallID,
		presented: presentedView{
			Title:    clipDetail(stripEscapes(msg.Title)),
			Path:     stripEscapes(msg.Path),
			Location: stripEscapes(msg.Location),
			Method:   msg.Method,
			Reason:   clipDetail(stripEscapes(msg.Reason)),
		},
	})
}

// addStartup appends the one-time start-up box — the logo and the session's host / model /
// context / version (startupView). It is seeded by newModel as entries[0] (and re-seeded by
// startNewSession when /clear starts a fresh session), not folded from an engine Event: the box is
// the HOST's opening frame, like addPresented's record of a host act. Host
// and Model are escape-stripped (they trace to config / the CLI) so a control sequence can never
// reach the terminal through them; the logo, context ([format.Tokens] of an int), and version are this
// program's own values and pass through untouched.
func (t *transcript) addStartup(v startupView) {
	v.Host = stripEscapes(v.Host)
	v.Model = stripEscapes(v.Model)
	t.entries = append(t.entries, entry{kind: entryStartup, startup: v})
}

// refreshStartup re-states the one-time start-up box's facts in place, leaving it exactly where it
// sits in the scrollback. The box is seeded once (addStartup) from the display Options as they stood
// at construction, and its startupView is a frozen copy: without this a session whose model is bound
// LATE — the async cold start, where the first heartbeat is startup discovery — would keep a box
// saying "connecting" at the top of the scrollback until the next /clear re-seeded it. Only the
// first box is restated; there is never a second (the /clear path resets the transcript before
// re-seeding, and a resumed scrollback carries no start-up entry — the codec never persists one).
// A transcript with no box yet is left untouched.
//
// Host and Model are escape-stripped exactly as addStartup strips them: the facts come from the same
// Options, and a fact that arrived from the server (the observed model id) is even less this
// program's own than the configured one.
func (t *transcript) refreshStartup(v startupView) {
	v.Host = stripEscapes(v.Host)
	v.Model = stripEscapes(v.Model)
	for i := range t.entries {
		if t.entries[i].kind == entryStartup {
			t.entries[i].startup = v
			return
		}
	}
}

// reset returns the transcript to its empty state — no committed entries and no in-progress
// assistant buffer — while preserving the debug flag (a hidden view toggle, not conversation) and
// the workspace root (a fact about the run, which /clear does not move).
// It is the /clear + /new "start a new session" primitive: the caller re-seeds the one-time
// start-up box with addStartup so the fresh view matches a launch. It does NOT touch the engine's
// memory (ClearContext) — that is the caller's separate, fallible step (model.startNewSession).
func (t *transcript) reset() {
	t.entries = nil
	t.pending = streamBuf{}
	t.streaming = false
	t.pendingRun = runRef{}
	t.parked = nil
	// The block-paint cache is keyed by ENTRY INDEX, and this is the one path that makes an index
	// mean something else: the caller re-fills the list (a fresh start-up box, a replayed
	// scrollback) before anything renders again, so pruning against the entry count at the next
	// render would find index 3 occupied and hand back the previous session's paint (paintcache.go).
	t.paints.clear()
	// t.debug and t.ws are deliberately preserved across a session reset.
}

// replay appends already-decoded committed entries after whatever the transcript already holds —
// the resume path (decodeTranscript) repainting a stored scrollback beneath the freshly-seeded
// start-up box. It is append-only and never touches the in-progress pending buffer: the entries
// are committed history, while streaming state belongs to this fresh process. The entries were
// escape-stripped on decode, so nothing untrusted from disk reaches the terminal unfiltered.
func (t *transcript) replay(entries []entry) {
	t.entries = append(t.entries, entries...)
}

// hasPrompt reports whether the transcript holds at least one committed user message. It is THE
// save-gate predicate — persist and saveAtIdle (the closing flushes with it) both funnel through it — so a session
// earns a history record only once a prompt was actually sent. Everything this program can put on
// screen by itself leaves the gate shut: the one-time start-up box, slash-command notes (/confine's
// status line, the /skills catalogue, a /model actuation note, the /sessions browser's notices),
// error notices, and the re-derived ephemeral chrome. Without that rule a launch spent poking at
// slash commands and then quitting files a "Session <date>" record reading 0 messages.
//
// entryInterjected is deliberately excluded, mirroring the rationale on userTexts: an interjection
// is a remark steering an Exchange that an entryUser opened (addInterjected), so a transcript
// holding one always holds that opening entry too — on a resume included, because the stored
// scrollback carries it. Counting interjections could therefore never change the answer, and
// leaving them out keeps the gate exactly "a prompt exists" ⇔ "an entryUser exists".
//
// Accepted consequence: resuming a LEGACY record that carries no transcript blob leaves the
// scrollback with no entryUser, so quitting without prompting skips the final quit-flush. Nothing
// is lost — that record is already on disk; only a cosmetic ctxUsed/UpdatedAt refresh is missed.
func (t *transcript) hasPrompt() bool {
	for i := range t.entries {
		if t.entries[i].kind == entryUser {
			return true
		}
	}
	return false
}

// userMessageCount reports how many committed user messages the transcript holds — the browsable
// "N msgs" count the session record carries (session.Meta.UserMsgs).
func (t *transcript) userMessageCount() int {
	n := 0
	for i := range t.entries {
		if t.entries[i].kind == entryUser {
			n++
		}
	}
	return n
}

// firstUserText returns the text of the first committed user message, or "" when none has been
// sent yet. The session title is derived from it (sessionTitle) — the first thing the human asked
// is the most recognisable label for the session in the history browser.
func (t *transcript) firstUserText() string {
	for i := range t.entries {
		if t.entries[i].kind == entryUser {
			return t.entries[i].text
		}
	}
	return ""
}

// userTexts returns the text of every committed user message, oldest first, and nil when nothing
// has been asked yet. It is the session's user side as a bare `/rename` reads it (runRename): the
// naming call selects its own bounded window out of this (title.Prompt), so what is owed here is
// the whole ordered list rather than a pre-trimmed one.
//
// Interjections are deliberately left out, following firstUserText's line. An entryInterjected is a
// remark steering work already under way (addInterjected) — it is not a request that opened an
// Exchange, which is why it is not an entryUser in the first place — so counting it among the
// session's requests would let a mid-Exchange "wrong file" outweigh the task it was correcting.
func (t *transcript) userTexts() []string {
	var texts []string
	for i := range t.entries {
		if t.entries[i].kind == entryUser {
			texts = append(texts, t.entries[i].text)
		}
	}
	return texts
}

// presentedStatus is the short line that closes a presentation entry. A rung that was tried and
// did not deliver says so and states that the path still stands — the entry is the one thing the
// ladder can always promise, so the wording never leaves the user wondering whether anything
// happened. Everything else is a hint about what to do next: an opened document needs none beyond
// the fact, and a path or a URL is one cmd+click away in every terminal that linkifies (Zed,
// VS Code, iTerm2, WezTerm, kitty).
func presentedStatus(v presentedView) string {
	if v.Reason != "" {
		return v.Reason + " — path shown"
	}
	if v.Method == domain.PresentOpened {
		return "opened on your machine"
	}
	return "cmd+click to open"
}

// apply folds one engine Event into the transcript (the C6 rule). The switch covers the
// ten transcript-rendered variants of the fourteen-variant Event set, so the rendered set
// stays honest as the engine evolves; the other four append no entry (ReasoningEvent feeds
// the activity line, AuditEvent and WireEvent nothing the transcript draws, and a UsageEvent is a
// reading rather than a block — it lands ON an entry the run already has, through applyUsage,
// which foldEvent calls with the window a fill needs and apply cannot see) and fall to the default
// case with every future variant. Each
// case folds its event: tokens grow the in-progress buffer at the depth that emitted them (the
// routing every other assistant-text case does through its own e.Depth); a StreamReset discards
// the buffer its own level owns; a
// Message commits it (canonical text); the first ToolCall of a Turn finalises the pre-tool
// narration before recording the call; results, approvals, and recovered faults append
// their own entries; a SubAgentPhase appends none — like a reading it lands ON the block a
// delegation already has, marking it running or folding in the report its child just returned
// (addSubAgentPhase); a ChildInterjection commits the message it reports INSIDE the child's run
// when it landed and a note saying it never did when it did not (addChildInterjection); a
// MechanismFired is surfaced only in the debug view. It renders only —
// no agent logic (C5).
func (t *transcript) apply(e domain.Event) {
	switch e := e.(type) {
	case domain.TokenEvent:
		t.appendToken(e.Text, runOf(e.EventBase))
	case domain.StreamResetEvent:
		t.discardPending(runOf(e.EventBase))
	case domain.MessageEvent:
		t.commitAssistant(e.Text, runOf(e.EventBase))
	case domain.ToolCallEvent:
		run := runOf(e.EventBase)
		t.finalizeNarration(run)
		t.addToolCall(e.Call, e.ResolvedPath, run)
	case domain.ToolResultEvent:
		t.addToolResult(e.Result, runOf(e.EventBase))
	case domain.SubAgentPhaseEvent:
		t.addSubAgentPhase(e)
	case domain.ChildInterjectionEvent:
		t.addChildInterjection(e)
	case domain.ApprovalEvent:
		t.addApproval(e.Request, e.Decision, runOf(e.EventBase))
	case domain.MechanismFiredEvent:
		t.addMechanism(e)
	case domain.ErrorEvent:
		t.addError(e.Source, e.Err, runOf(e.EventBase))
	default:
		// An unknown future variant: tolerate it. The set is sealed and additively
		// versioned, so an unrecognised Event is rendered as nothing rather than a panic.
	}
}

// applyUsage folds a sub-agent's readings onto the run it belongs to — its context FILL and its
// cumulative TOTALS, which travel on the same Event and land on the same head. It is the transcript's
// half of the UsageEvent, and the one fold apply cannot perform from the Event alone: a reading
// is a FILL, and a fill means nothing beside the window it fills, which is a fact about the
// Model rather than about the Event (foldEvent hands window in). Anything that is not a
// Depth > 0 UsageEvent folds nothing: a Depth 0 reading is the human's own conversation, and
// that one belongs to the status gauge alone (foldStats).
//
// A reading belongs to the still-open run its own SPAWNING CALL opened (domain.EventBase.CallID,
// stamped on every delegated event): with siblings running at once the depth no longer picks a run
// out — two children fill two windows at depth 1, and the most recent open head is simply whichever
// was announced last. A reading carrying no call id at all — a legacy record, a hand-built test
// stream — still falls back to the depth rule this fold was born with: the most recent still-open
// head standing at depth N-1. It is never transitive either way: each agent fills its OWN window,
// so a nested run's reading stops at the nested head and says nothing about its parent's fill. A
// reading that matches no open run — one that arrived after its report, or before its call — folds
// nothing at all, as it did before this entry field existed.
//
// The reading is the LATEST total and never a running sum: every Turn reports the whole context
// it filled, so the newest number IS the fill. A total the server omitted falls back to
// prompt+completion, the preference foldStats already reads usage by; a reading of nothing
// leaves the previous one standing rather than blanking a run that had reported.
//
// The child's cumulative totals are latest-wins for a different reason: the CHILD keeps the running
// sum (each sub-agent counts its own calls from zero, domain.UsageEvent), so the head holds its
// newest report rather than adding events up here. They and the fill are folded independently —
// a maintenance reading advances the totals while leaving the fill standing, and an event stamped
// by an agent that has counted nothing advances neither.
// The child's MODEL folds with the fill and on the fill's terms: it is stamped on the same reading
// (domain.UsageEvent), and it is kept only when it differs from sessionModel — the model the session
// itself is bound to — because a delegation that ran where everything else ran is not news. Deciding
// it here, once, is what lets a finished run keep saying which model filled it after the session has
// rebound to another (ADR 0045).
//
// window is the SESSION's window and only the fallback: the reading names the window it actually
// filled (childWindow), because a routed delegation fills the Delegation target's window rather
// than the session's and a fill measured against the wrong limit is a wrong number on screen.
func (t *transcript) applyUsage(e domain.Event, window int, sessionModel string) {
	usage, ok := e.(domain.UsageEvent)
	if !ok || usage.Depth <= 0 {
		return
	}
	total := usage.TotalTokens
	if total == 0 {
		total = usage.PromptTokens + usage.CompletionTokens
	}
	// The two readings part company on the Maintenance flag, for the reason foldStats gives on the
	// gauge: a maintenance call's prompt is the summarizer's own, so it says nothing about how full
	// the child's window stands — but its tokens were really spent, so the totals take it.
	fills := total > 0 && !usage.Maintenance
	totals, counted := usageReading(usage)
	if !fills && !counted {
		return
	}
	head := t.openSubAgentHead(usage.CallID, usage.Depth)
	if head == nil {
		return
	}
	if fills {
		head.ctxUsed, head.ctxLimit = total, childWindow(usage, window)
	}
	if counted {
		head.usage = totals
	}
	// Taken from every reading this fold accepts that names a model, whether or not it moved the
	// fill: a maintenance reading was still produced by the child's own model. An event naming none
	// — an agent bound before its first heartbeat — leaves whatever was established standing rather
	// than blanking it. Stripped here because the fold IS the seam this server-advertised id enters
	// the view through (doc.go): the gauge paints it beside the fill, and subagentblock.go paints
	// what the seams stored.
	if usage.Model != "" && usage.Model != sessionModel {
		head.ctxModel = stripEscapes(usage.Model)
	}
}

// childWindow answers which limit a delegated fill is measured against: the one the reading itself
// carries — the emitting agent's own bound window, which for a routed child is the Delegation
// target's and not the session's (ADR 0045) — falling back to sessionWindow when the reading names
// none. A reading names none in exactly the cases where the session's window IS the child's answer:
// a child that inherited the parent's Config verbatim on a build before the stamp existed, and a
// record decoded from such a session. So the fallback is the old behaviour preserved where it was
// right, not a guess where it is wrong.
func childWindow(usage domain.UsageEvent, sessionWindow int) int {
	if usage.ContextWindow > 0 {
		return usage.ContextWindow
	}
	return sessionWindow
}

// openSubAgentHead picks the still-open run head a delegated reading belongs to: the one its own
// spawning call opened (callID), or — for a reading carrying no call id, a legacy record or a
// hand-built stream — the most recent open head standing one level above depth. It returns nil
// when nothing matches, which is the reading that arrived after its run reported or before its
// call: it folds nothing at all, exactly as applyUsage describes.
func (t *transcript) openSubAgentHead(callID string, depth int) *entry {
	for i := len(t.entries) - 1; i >= 0; i-- {
		head := &t.entries[i]
		if !head.opensRun() {
			continue
		}
		if callID != "" {
			if head.callID != callID {
				continue
			}
		} else if head.depth != depth-1 {
			continue
		}
		return head
	}
	return nil
}

// appendToken grows the in-progress assistant buffer with one streamed chunk emitted by run. The
// buffer is committed by commitAssistant (a MessageEvent) or finalizeNarration (the first
// ToolCall of the Turn), and is never rendered as a committed entry until then. The chunk is
// escape-stripped as it lands (stripEscapes) so no ESC byte from the model's stream ever
// reaches the terminal — even split across two chunks, since the byte is removed per chunk.
//
// A chunk from a DIFFERENT run than the one already buffered PARKS that run's text and resumes
// this run's own, because the single slot is now contended: concurrent siblings' batches alternate
// through it (ADR 0039), and a switch no longer means the previous streamer is finished. Parking is
// what keeps each run's answer whole and its own — the text waits for its run's own commit point
// instead of being committed as a fragment here, or left for another run's event to adopt.
func (t *transcript) appendToken(text string, run runRef) {
	t.displace(run)
	if !t.streaming {
		t.pending = t.unpark(run) // resume what this run streamed before it lost the slot
	}
	t.streaming = true
	t.pendingRun = run
	t.pending.append(stripEscapes(text))
}

// discardPending drops the in-progress assistant buffer when the agent at depth re-streams its
// Turn. A StreamResetEvent signals the loop is re-streaming (an ActionRetry post-response
// decision re-called the Upstream), so the tokens accumulated so far are superseded and
// must never be committed (events.go contract). The re-stream's tokens arrive next and the
// Turn's MessageEvent carries the final, accepted text.
//
// It drops only what the resetting agent OWNS: a re-stream is one run's Turn starting over, so a
// delegate's reset may not wipe what its parent had streamed — nor, now that siblings share the
// slot, what a sibling had. Its own parked text goes with it, being the same superseded stream one
// alternation back. A reset arriving with no buffer open clears the slot either way, which is what
// it already meant.
func (t *transcript) discardPending(run runRef) {
	t.unpark(run)
	if t.streaming && t.pendingRun != run {
		return
	}
	t.streaming = false
	t.pending = streamBuf{}
	t.pendingRun = runRef{}
}

// commitAssistant finalises the streamed buffer into a committed assistant entry on a
// MessageEvent. The MessageEvent's text is canonical (it carries the full, accepted
// message), so it is preferred over the accumulated tokens; the tokens are a live preview
// that should reconcile to the same text (§0 event-sequence rule). A canonical text that is
// blank falls back to the accumulated tokens so nothing streamed is lost, and a text that is
// blank either way commits no entry at all — a lone ✦ marker line is itself an unneeded line.
//
// A buffer left open by a DIFFERENT run is parked first (appendToken's rule), so a message from
// the parent neither adopts a delegate's residue nor silently drops it, and a sibling still
// streaming keeps every token it has sent.
func (t *transcript) commitAssistant(canonical string, run runRef) {
	t.displace(run)
	buffered := t.takePending(run)
	// canonical is the MessageEvent's untrusted model text; strip its escapes (the buffer was
	// already stripped as it streamed, so a double-strip there is a cheap no-op), then drop the
	// blank lines the model padded the message with, so the block sits exactly one blank line
	// from its neighbours instead of two or three (layout.md).
	text := trimBlankLines(stripEscapes(canonical))
	if text == "" {
		// The fallback commits the BUFFER, which is this run's own by construction — takePending
		// drains no other run's text.
		text = trimBlankLines(buffered)
	}
	if text == "" {
		return
	}
	t.place(entry{kind: entryAssistant, text: text, depth: run.depth, spawnCallID: run.spawn})
}

// finalizeNarration commits the in-progress buffer as the pre-tool narration when the first
// ToolCallEvent of a Turn arrives (the C6 rule). A tool Turn emits no MessageEvent, so the
// streamed tokens are the canonical narration text. Only the first ToolCall finalises:
// afterwards streaming is false, so the Turn's remaining ToolCalls add no empty entry. A
// Turn that streamed nothing — or only blank lines — before its tool call commits nothing.
//
// It finalises the ARRIVING run's narration and only that: with siblings streaming at once the
// buffer in hand may belong to another child entirely, and committing that one here would tear its
// answer in half at a moment that has nothing to do with it. A buffer held by another run is parked
// instead — so the live preview stops showing text whose Turn has moved on, and the text itself
// waits for that run's own commit point (or for its run to end, closeRun).
func (t *transcript) finalizeNarration(run runRef) {
	t.displace(run)
	text := trimBlankLines(t.takePending(run))
	if text == "" {
		return
	}
	t.place(entry{kind: entryAssistant, text: text, depth: run.depth, spawnCallID: run.spawn})
}

// closeRun commits what a finished run streamed and never committed — the half-sentence a delegate
// was midway through when it faulted, was cancelled, or was displaced from the buffer by a sibling
// and then reported. It runs as the run's report folds into its head (addToolResult), which is the
// last moment the text can still be placed inside the block it belongs to.
//
// head is the run's own call block, taken by value: place may insert, and an insertion invalidates
// every pointer into the entries slice.
func (t *transcript) closeRun(head entry) {
	if !head.headsRun() {
		return
	}
	run := runRef{depth: head.depth + 1, spawn: head.callID}
	if t.streaming && t.pendingRun == run {
		t.park()
	}
	text := trimBlankLines(t.unpark(run).String())
	if text == "" {
		return
	}
	t.place(entry{kind: entryAssistant, text: text, depth: run.depth, spawnCallID: run.spawn})
}

// addToolCall appends a tool-call entry: the presentation view (friendly label + target)
// built from the model's requested call, plus the call ID the paired result folds into. The
// view shows the call verbatim where it cannot summarise it (a malformed argument is rendered
// as-is rather than hidden — the human approving a write must see exactly what was asked). What it
// does restate is the SPELLING of the path the call names: the workspace root is shortened out of
// the target and out of a summary the block worded itself (t.ws), which changes how the call reads
// and nothing about what was requested — and never out of what the block quotes, neither a body nor
// a one-line output promoted onto the branch, where a path is content the approver must see as it
// stands (toolView.shortenPaths).
//
// The entry's two ids are different facts and both are kept: callID is the call the block IS —
// what the paired result folds into, and, for a sub_agent call, what its own children's entries
// group behind — while spawnCallID is the run this block sits IN (place).
// resolved is the engine's disclosure for this call — where its path argument really points, when
// that is not where the argument says (domain.ToolCallEvent.ResolvedPath) — and empty on every
// ordinary call. It reaches the block through the presenter, which spells it beside the target.
func (t *transcript) addToolCall(call domain.ToolCall, resolved string, run runRef) {
	t.place(entry{
		kind:        entryToolCall,
		depth:       run.depth,
		callID:      call.ID,
		spawnCallID: run.spawn,
		tool:        presentToolCall(call, resolved, t.ws),
	})
}

// addToolResult folds a tool result into its call's block. It scans from the tail for the
// most recent un-paired tool-call entry with a matching CallID and enriches that call's view
// with the result's one-line summary, marking it done. A result the tool flagged as an error
// (IsError) is a normal in-band outcome the model reacts to — not a recovered fault (that is
// ErrorEvent) — so it is summarised, not raised. A result that matches no open call (the
// defensive orphan case) is appended as a standalone result block so its outcome is not lost.
func (t *transcript) addToolResult(result domain.ToolResult, run runRef) {
	for i := len(t.entries) - 1; i >= 0; i-- {
		e := &t.entries[i]
		if e.kind == entryToolCall && !e.done && e.callID == result.CallID {
			// A delegation whose finished phase already folded THIS result into the view is enriched
			// once and no more (addSubAgentPhase): the fold appends the report's lines to the body
			// (toolBody.with), so a second one would say the whole report twice. Everything else the
			// pairing does still happens here — the burst is what closes the call and its run.
			if e.phase != domain.SubAgentFinished {
				e.tool.enrichWithResult(result, t.ws)
			}
			e.done = true
			// A delegation's result is its run's last word: whatever the child streamed and never
			// committed is committed now, inside the run, before the block settles (closeRun). The
			// head is copied out first — closeRun may place an entry, which moves the slice.
			t.closeRun(t.entries[i])
			return
		}
	}
	// The orphan branch is the one path a result takes WITHOUT passing enrichWithResult's seam, so
	// it strips the content itself — it is raw tool output, which a malicious repo controls.
	text := stripEscapes(result.Content)
	if result.IsError {
		text = "error: " + text
	}
	t.place(entry{kind: entryToolResult, text: text, depth: run.depth, spawnCallID: run.spawn})
}

// addSubAgentPhase folds a delegation's lifecycle boundary onto the block that delegation IS
// (domain.SubAgentPhaseEvent): its child started running, or its child finished and the report rides
// the event. The block is found by the event's own call id — the id of the sub_agent call that
// spawned the child, which is exactly the id addToolResult pairs the eventual result by — so a
// delegation is marked wherever it sits, however many siblings are running beside it (ADR 0039).
//
// It exists because the result burst is not a liveness signal: a group's results arrive together, in
// call order, after the last child has joined, so without this a member that finished first would go
// on reading as working until its siblings caught up. The phase is what the display asks instead
// (subAgentReported), and the finished phase's payload is what lets that member's report be read the
// moment it lands rather than at the end of the group.
//
// Folding the result here is the ONE thing that must not happen twice: the authoritative
// ToolResultEvent still follows, and enrichWithResult appends to the body rather than replacing it.
// The entry's phase is what tells the pairing so (addToolResult). A delegation already paired — a
// phase arriving after its own result, which the orderings make impossible but the fold does not
// assume — keeps the view it has and takes the phase alone.
//
// Nothing is appended, ever: a phase is a fact about a block the transcript already holds, and an
// event naming no such block (a phase for a run this view never saw) folds nothing at all.
func (t *transcript) addSubAgentPhase(e domain.SubAgentPhaseEvent) {
	for i := len(t.entries) - 1; i >= 0; i-- {
		en := &t.entries[i]
		if !en.headsRunFor(e.CallID) {
			continue
		}
		en.phase = e.Phase
		if e.Phase == domain.SubAgentFinished && !en.done {
			en.tool.enrichWithResult(e.Result, t.ws)
		}
		return
	}
}

// addChildInterjection folds one delivery report for a message the human addressed to a running
// sub-agent (domain.ChildInterjectionEvent, ADR 0063). Every message the mailbox accepted gets
// exactly one, so what the human was shown as queued is always accounted for on screen.
//
// A message that LANDED becomes that child's own user block, inside its run, at the boundary it
// actually reached (addUserAt) — a collapsed run elides it with the rest of its span, and an open
// one shows it railed where the child read it. One that did NOT land becomes a host note instead:
// the child ended before the boundary its message was waiting for, so there is no run left to put
// it in and nothing the child ever saw to record. The note names the delegation, falling back to
// the status line's own word for an unnamed one, and escape-strips through addNote like every
// other note worded from model-supplied text.
func (t *transcript) addChildInterjection(e domain.ChildInterjectionEvent) {
	if e.Landed {
		t.addUserAt(e.Depth, e.CallID, e.Input)
		return
	}
	name := t.runName(e.CallID)
	if name == "" {
		name = subAgentActivityName
	}
	t.addNote(name + " finished before your message landed")
}

// hasOpenToolCall reports whether any tool-call entry is still waiting for its result — the
// signal the live status line uses to stay on the tool phrase while a batch of calls runs. Its
// caller is foldEvent (fold.go), which reads it straight after apply and hands the answer to
// foldActivity, so the phrase can never be derived from a pairing that has not happened yet.
// It reads the same call/result pairing addToolResult maintains, so a call is "open" from the
// moment it is recorded until its result folds into it. A call whose result
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

// setExpanded puts one block into the collapsed or the expanded paint and reports whether it found
// a block to set. index addresses t.entries, and only a kind carriesBlockState admits carries one:
// every other kind paints one way whatever is asked of it, and an index outside the slice is a
// caller resolving a click against a paint the transcript has already grown past. Both answer false
// and change nothing, because this sits on the repaint path where a panic is the whole session.
//
// It is the one writer of entry.expanded, and it writes THROUGH the entries slice exactly as
// addToolResult marks done: the Model is copied by value on every Update (ADR 0011), so per-entry
// state on the shared backing array is how a view fact survives the copy without a map or a
// no-copy type on the Model. Nothing here touches the engine, the call/result pairing, or the
// session record — an expanded block is a way of looking at the scrollback, not a change to it.
//
// It is stated as a SET rather than only a flip because the flip is composed from it
// (toggleExpanded), which keeps one writer and one pair of guards behind both, and because a caller
// that knows which state it wants says so rather than flipping and hoping. No click surface asks for
// one direction any more: the `+N more lines` count that could only ever open now rides the leader
// row's outcome slot instead of a line of its own (collapsedRemainder), and every marked line is a
// toggle.
//
// Holding the state is not the same as showing it. What an expanded block actually paints is the
// painter's business (render.go), and a block with nothing to hide — a prompt that fits inside the
// collapsed row cap at the current width — simply paints the same either way. That is deliberate:
// the gate is about which kinds OWN a block state, so a resize can change whether a body collapses
// without the transcript ever hearing about it.
//
// A RUN owns no such state at all. Under ADR 0063 a delegation has two shapes and neither is a fold
// of the other: the collapsed row it wears in the conversation, and the run VIEW that expanding it
// opens ([Model.openRunAt], the one reach every click and ⏎ funnels through). So a head with a run
// to open answers false here, and the flag stays where it was — a stale one replayed from a record,
// or written by a caller that has not asked the redirect first, cannot re-open a rail the painter
// no longer draws.
//
// The predicate is the redirect's, word for word: entries behind the head, or a head that has not
// reported yet. A delegation that is OVER and left nothing behind it is not a run — it keeps the
// ordinary block's inline toggle onto the prompt it carried (unframedSubAgentView), which is the
// one meaning left for the flag on a sub_agent call.
//
// What that refusal does NOT cover is the fold INSIDE the run's own view: the task the run was
// handed is painted there as a user row and folds like any other tall prompt, and that fold has a
// state of its own the framed head does allow ([transcript.setTaskExpanded], entry.taskExpanded).
// The two are different acts — one opens a rail in the conversation, the other opens a row inside
// the view already on screen — so refusing the first must not leave the second advertising a
// see-more marker nothing can act on.
func (t *transcript) setExpanded(index int, expanded bool) bool {
	if index < 0 || index >= len(t.entries) || !t.entries[index].kind.carriesBlockState() {
		return false
	}
	if head := t.entries[index]; head.headsRun() && (subAgentSpan(t.entries, index) > 0 || !head.done) {
		return false
	}
	t.entries[index].expanded = expanded
	return true
}

// toggleExpanded flips one block between its collapsed and expanded paint and reports whether it
// found a block to flip — the meaning of a click on a block's header line. The kind and range
// guards are setExpanded's, so an index that names no block answers false from one place; the
// bound here only makes the READ of the current state safe.
func (t *transcript) toggleExpanded(index int) bool {
	if index < 0 || index >= len(t.entries) {
		return false
	}
	return t.setExpanded(index, !t.entries[index].expanded)
}

// setTypeExpanded opens or closes the TYPE ROW of the run headed by entries[index] — the second,
// independent level of a super-group's state (entry.typeExpanded) — and reports whether it found a
// run head to set. Only a tool call can head a run (sameLabelRun), so every other kind answers false
// and changes nothing, as does an index outside the slice: this sits where setExpanded sits, on the
// path a click and a repaint share, where a panic is the whole session.
//
// It does NOT ask whether the entry heads a run TODAY. Membership is derived from the entries at
// query time and moves as they append (toolSuperGroup), so a flag written on an entry that later
// stops heading a run is simply a flag nothing reads — where a gate on the live derivation would
// make the same click succeed or fail depending on what the model called next. The flag survives
// either way, which is what lets a reader open a type row and keep it open while the umbrella grows
// beneath it.
func (t *transcript) setTypeExpanded(index int, expanded bool) bool {
	if index < 0 || index >= len(t.entries) || t.entries[index].kind != entryToolCall {
		return false
	}
	t.entries[index].typeExpanded = expanded
	return true
}

// toggleTypeExpanded flips one type row between its two states and reports whether it found a run
// head to flip — the meaning of a click on a type row. The kind and range guards are
// setTypeExpanded's, so an index that heads nothing answers false from one place; the bound here
// only makes the READ of the current state safe.
func (t *transcript) toggleTypeExpanded(index int) bool {
	if index < 0 || index >= len(t.entries) {
		return false
	}
	return t.setTypeExpanded(index, !t.entries[index].typeExpanded)
}

// setTaskExpanded opens or closes the TASK row of the run headed by entries[index] — the fold a
// rooted paint draws directly under its breadcrumb (render.go), whose click surface is
// targetTask — and reports whether it found a run head to set. Only a delegation has a task a view
// can paint (entry.headsRun), so every other kind answers false and changes nothing, as does an
// index outside the slice: this sits where setExpanded sits, on the path a click and a repaint
// share, where a panic is the whole session.
//
// It is a state of its OWN rather than the head's expanded flag because that flag is refused on a
// head with a run to open ([transcript.setExpanded]) — the refusal that keeps a delegation to its
// two shapes under ADR 0063. The refusal stands; this is not a way around it. A run in the
// conversation still cannot be flipped open in place, and what this writes is read by exactly one
// paint, the view rooted at that very run, where the row it folds is a prompt and not a rail.
//
// Like setTypeExpanded, it does not ask whether the view is open TODAY: the flag is simply a fact
// nothing paints while the run is a collapsed row in the conversation, and it is still there when
// the reader opens the view again.
func (t *transcript) setTaskExpanded(index int, expanded bool) bool {
	if index < 0 || index >= len(t.entries) || !t.entries[index].headsRun() {
		return false
	}
	t.entries[index].taskExpanded = expanded
	return true
}

// toggleTaskExpanded flips a view's task row between its two states and reports whether it found a
// run head to flip — the meaning of a click, or the block cursor's ⏎, on that row. The kind and
// range guards are setTaskExpanded's, so an index that heads no run answers false from one place;
// the bound here only makes the READ of the current state safe.
func (t *transcript) toggleTaskExpanded(index int) bool {
	if index < 0 || index >= len(t.entries) {
		return false
	}
	return t.setTaskExpanded(index, !t.entries[index].taskExpanded)
}

// closeSuperGroup closes every open child of the umbrella headed by entries[head] — what a click on
// its header means (design call 9, docs/layout/tool-layout.md: "the umbrella's floor is its type
// rows … clicking the umbrella header closes all open children"). It reports whether anything was
// open to close, so a click on a header with nothing behind it repaints nothing.
//
// BOTH levels are cleared, the type rows and the members beneath them, because "closed" has to mean
// the same thing whichever way a reader got there: a member left open under a type row that was
// closed first is invisible but still open, and a header that left it that way would reopen it the
// next time the type row was.
//
// head must be the umbrella's own head — the index [transcript.renderView] painted the header at and
// marked its lines with. The derivation is only correct at a run boundary (toolSuperGroup), which is
// exactly what a painted head is; asked anywhere else it would answer about a different umbrella,
// and an index that heads no umbrella at all answers with no runs and so changes nothing.
func (t *transcript) closeSuperGroup(head int) bool {
	changed := false
	for _, r := range toolSuperGroup(t.entries, head) {
		for i := r.at; i < r.at+r.n; i++ {
			if t.entries[i].expanded || t.entries[i].typeExpanded {
				t.entries[i].expanded, t.entries[i].typeExpanded = false, false
				changed = true
			}
		}
	}
	return changed
}

// toolRun is one RUN of tool calls: the maximal stretch of adjacent entries carrying the same
// friendly Label at the same sub-agent depth, every one of them foldable into a member row
// (groupable). It is the unit both folded shapes are built from — a same-label group IS one run, and
// a super-group is two or more of them under one umbrella — and a lone call is a run of 1
// (docs/layout/tool-layout.md, "Vocabulary").
//
// It is stated as an index pair rather than as the views it covers because the INDEX is what the
// levels above it need: the head entry is where the run's type-row state lives (typeExpanded) and
// what a click on that row resolves to, and each member is one entry, so a member row's own entry is
// at + n.
type toolRun struct {
	at int // index into the entries slice of the run's first call
	n  int // how many calls the run holds; never 0
}

// superGroup is the umbrella: the adjacent runs that fold under one "✦ Tools (N calls)" header, in
// TIME ORDER, which is the order they appear in the transcript. Calls are never reordered to merge
// same-type calls that were not adjacent (docs/layout/tool-layout.md): `read, terminal, read` is
// three runs and therefore three rows, not two.
type superGroup []toolRun

// calls is N — the total number of calls the umbrella holds, which its header states. It is also the
// number of ENTRIES the umbrella covers, because every member of every run is exactly one entry and
// the runs are adjacent by construction: a walk over the entries skips the whole umbrella by adding
// it to the head's index.
func (g superGroup) calls() int {
	n := 0
	for _, r := range g {
		n += r.n
	}
	return n
}

// sameLabelRun is the length of the run entries[i] opens: the adjacent calls that carry the same
// friendly Label at the same sub-agent depth and can each be a member row. It answers 0 when
// entries[i] opens no run at all — anything that is not a tool call, and a call the presenter marked
// solo or left without a target to lead a row (groupable).
//
// Any other entry between two calls ends the run where it stands, since the scan only ever walks
// forward over ADJACENT entries: narration, a note, an approval, an error. Two different tools
// sharing a label group all the same — the reader groups by what the row says, not by tool id.
func sameLabelRun(entries []entry, i int) int {
	if i < 0 || i >= len(entries) {
		return 0
	}
	head := entries[i]
	if head.kind != entryToolCall || !groupable(head.tool) {
		return 0
	}
	n := 1
	for j := i + 1; j < len(entries); j++ {
		e := entries[j]
		if e.kind != entryToolCall || e.depth != head.depth || e.tool.Label != head.tool.Label || !groupable(e.tool) {
			break
		}
		n++
	}
	return n
}

// toolSuperGroup is the umbrella entries[i] heads, or nil when it heads none: the adjacent
// same-depth runs of DIFFERENT labels starting there. Two runs are the floor (design call 1) —
// one run is the same-label group that already had a header of its own, and a run of 1 is a lone
// call, so a Read followed by a Terminal is an umbrella of two rows.
//
// Adjacent runs differ in label BY CONSTRUCTION rather than by a test here: sameLabelRun is maximal,
// so a call carrying the previous run's label was already absorbed by it and a call that opens a new
// run necessarily carries a different one.
//
// The breakers are the group's own, one rule stated once: anything that is not a call opening a run
// at the umbrella's OWN depth ends it where it stands. That covers the non-tool entries a same-label
// run already broke on — narration, a note, an approval, an error — and, through groupable's solo
// mark, a sub-agent call, whose block heads a whole run of its own and never joins an umbrella (spec
// Rules: "a sub-agent block or group breaks the run"); the deeper entries such a run leaves behind
// break it by depth, as does a nested call at any other level.
//
// Membership is DERIVED here and stored nowhere, which is what makes formation live (design call 2):
// the umbrella exists the moment the second run's first call is placed — the running call being its
// last row, spinner star and all — and grows as further calls append, with no membership recorded
// anywhere that could fall out of date behind them.
func toolSuperGroup(entries []entry, i int) superGroup {
	n := sameLabelRun(entries, i)
	if n == 0 {
		return nil
	}
	runs := superGroup{{at: i, n: n}}
	for at := i + n; at < len(entries) && entries[at].depth == entries[i].depth; {
		m := sameLabelRun(entries, at)
		if m == 0 {
			break
		}
		runs = append(runs, toolRun{at: at, n: m})
		at += m
	}
	if len(runs) < 2 {
		return nil
	}
	return runs
}

// subAgentBlock is one member of a sub-agent group: the index of the delegation's call entry and
// the length of the run nested beneath it ([subAgentSpan]). The span is carried because it is what
// separates one member from the next — a delegation's whole span lies between its call and its
// neighbour's — so a walk over the group's members steps by 1+span rather than by 1.
type subAgentBlock struct {
	at   int // index into the entries slice of the delegation's own call entry
	span int // how many nested entries the delegation left behind it; 0 for one that produced none
}

// subAgentHeads reports whether entries[i] is a delegation's call block — the head a sub-agent run
// hangs off. An index outside the list answers false, which is what lets callers hand it the result
// of a walk that may have found nothing.
func subAgentHeads(entries []entry, i int) bool {
	return i >= 0 && i < len(entries) && entries[i].headsRun()
}

// headsRun reports whether e is a delegation's call block — the head a sub-agent run hangs off, and
// the entry every other question about a run is asked of ([subAgentSpan], [subAgentFramed],
// [subAgentReported]). It is [subAgentHeads] asked of an entry rather than of a position — what
// place asks of an entry it has not committed yet — and it reads the card's own rule
// ([toolView.headsRun]), so the block and the entry carrying it can never disagree about what a
// delegation is.
func (e entry) headsRun() bool {
	return e.kind == entryToolCall && e.tool.headsRun()
}

// opensRun is [entry.headsRun] narrowed to a run still OPEN, and reading `done` for that — rather
// than the phase [subAgentReported] reads — is the whole of the distinction. The sites asking this
// are asking whether the CALL is still waiting for its result: the head a delegated reading folds
// into (openSubAgentHead), the enclosing run a streaming preview would be drawn inside
// (insideCollapsedRunAtDepth). A child's own FINISHED phase does not
// close that question — in a fan-out the results burst together once every child has joined (ADR
// 0039 decision 4), so a member that reported first is still an unpaired head for as long as its
// slowest sibling runs, and it is exactly that head a late reading, or an entry arriving beneath
// it, still belongs to. "Has this delegation reported?" is the other question and has an answer of
// its own ([subAgentReported]); neither may be spelled with the other's conjunct.
func (e entry) opensRun() bool {
	return e.headsRun() && !e.done
}

// headsRunFor reports whether e is the head of the run the sub_agent call callID opened — what a
// walk asks when it is looking for one NAMED run rather than for any. The id is compared as given:
// a caller handing it an empty one matches a head that carries none, exactly as the inline
// comparison it replaces did.
func (e entry) headsRunFor(callID string) bool {
	return e.headsRun() && e.callID == callID
}

// subAgentGroup is the group entries[i] opens, or nil when it opens none: the adjacent delegations
// standing at the same depth, each with the run nested beneath it (docs/layout/tool-layout.md,
// Rules — "Sub-agent calls group with each other"). Two are the floor, as they are for every other
// folded shape: a lone delegation is the block it always was.
//
// Adjacency here is adjacency of BLOCKS, not of entries, which is the whole reason this rule cannot
// be [sameLabelRun] with the solo mark lifted: a delegation's span sits between its call and the
// next call at its own depth, so two delegations are never neighbouring entries once either of them
// has done any work. The walk therefore steps over each member's span, and everything else the
// group rule needs follows from that: an entry of any other kind between two delegations — a
// narration, a note, an approval — is not a head at the group's depth and ends it where it stands,
// and so does a call to any other tool.
//
// A delegation with NO span joins like any other (a child refused at the depth bound, one that
// faulted before its first event): it is a delegation, its row says so, and the reader grouping two
// of them is reading the same list. That is the one rule the presenter's solo mark used to state
// from the other side (presentToolCall) — solo still keeps a delegation out of a MIXED super-group
// (design call 12), which is a different question and is answered where it always was (groupable).
func subAgentGroup(entries []entry, i int) []subAgentBlock {
	if !subAgentHeads(entries, i) {
		return nil
	}
	var group []subAgentBlock
	for at := i; subAgentHeads(entries, at) && entries[at].depth == entries[i].depth; {
		span := subAgentSpan(entries, at)
		group = append(group, subAgentBlock{at: at, span: span})
		at += 1 + span
	}
	if len(group) < 2 {
		return nil
	}
	return group
}

// subAgentGroupAt is the group entries[i] BELONGS to and its position in it — what the painter asks
// at a block head, where [subAgentGroup] alone would answer about the members from i onward and so
// would grow a second "✦ Sub-Agent (N)" header at every member of one group.
//
// It walks back to the group's first member before walking forward, and the backward step is exact
// for the same reason the forward one is: a member's span is contiguous and ends where the next
// member begins (transcript.place), so the entry at the group's own depth immediately before i is
// the previous member's head whenever there is one.
//
// ok is false for anything that is in no group at all, which is every entry that is not a
// delegation and every delegation standing alone.
func subAgentGroupAt(entries []entry, i int) (group []subAgentBlock, pos int, ok bool) {
	if !subAgentHeads(entries, i) {
		return nil, 0, false
	}
	first := i
	for {
		prev := prevSibling(entries, first)
		if !subAgentHeads(entries, prev) || entries[prev].depth != entries[i].depth {
			break
		}
		first = prev
	}
	group = subAgentGroup(entries, first)
	for k := range group {
		if group[k].at == i {
			return group, k, true
		}
	}
	return nil, 0, false
}

// prevSibling is the index of the entry standing at entries[i]'s own depth immediately before it —
// the head of the block that ends where i begins — or −1 when i opens its level. Everything nested
// deeper than i belongs to whatever block precedes it, so the walk skips it whole.
func prevSibling(entries []entry, i int) int {
	if i <= 0 || i >= len(entries) {
		return -1
	}
	return prevSiblingAt(entries, i, entries[i].depth)
}

// prevSiblingAt is [prevSibling] asked of a POSITION and a depth rather than of an entry already in
// the list — what place asks on behalf of an entry it has not committed yet: the head of the block
// standing at depth that ends where at begins, or −1 when nothing at that depth precedes it.
func prevSiblingAt(entries []entry, at, depth int) int {
	j := at - 1
	for j >= 0 && entries[j].depth > depth {
		j--
	}
	if j < 0 || entries[j].depth != depth {
		return -1
	}
	return j
}

// addApproval records an Approval observationally — the decision already came back through
// the C3 reply channel, so this is a transcript record of what was decided, not the gate.
//
// The tool name is the MODEL's — a dynamic MCP tool is named by its server and an unregistered one
// is echoed raw — so it is escape-stripped like every other note text. This entry is built here
// rather than through addNote (it carries a depth), which is exactly the kind of bypass that left
// producers unstripped before.
func (t *transcript) addApproval(req domain.ApprovalRequest, decision domain.ApprovalDecision, run runRef) {
	text := fmt.Sprintf("approval %s: %s", decision, stripEscapes(req.Tool))
	t.place(entry{kind: entryNote, text: text, depth: run.depth, spawnCallID: run.spawn})
}

// addMechanism records a fired Mechanism, but only in the debug view (off by default).
// There is no Mechanism catalogue until Phase 4, so a fired event is observability noise
// for the product UI; the switch handles it now so a Phase-4 Mechanism needs no retrofit.
func (t *transcript) addMechanism(e domain.MechanismFiredEvent) {
	if !t.debug {
		return
	}
	text := fmt.Sprintf("mechanism %s @ %s: %s", e.Mechanism, e.Hook, e.Action)
	run := runOf(e.EventBase)
	t.place(entry{kind: entryNote, text: text, depth: run.depth, spawnCallID: run.spawn})
}

// addError appends a recovered-fault notice (ADR 0007 — an ErrorEvent does not stop the
// loop). source is the tool name / mechanism ID / "loop"; msg is the error text.
//
// Both halves are escape-stripped at this seam, as addNote strips its own: an error text routinely
// quotes what failed — a path, a command, an upstream body, an MCP server's message — so it is
// untrusted for exactly the reasons the tool card's content is, and source is the model's own tool
// name when a tool faulted.
func (t *transcript) addError(source, msg string, run runRef) {
	t.place(entry{
		kind:        entryError,
		text:        stripEscapes(source + ": " + msg),
		depth:       run.depth,
		spawnCallID: run.spawn,
	})
}

// ----------------------------------------------------------------------------
// Formatting helpers
// ----------------------------------------------------------------------------

// flattenField folds a FIELD onto one line, each newline, each tab and each carriage return
// becoming the space that stands where the break was. It is [lineEditor.flattenLine]'s rule at a
// DISPLAY seam rather than an input one, over the same three characters, and it exists because all
// three reach it: stripEscapes deliberately KEEPS the newline and the tab, and the callers that
// hand this a model's own bytes unstripped — a skill's display name and summary (skills.go), a
// pop-up title, a tool-argument label (toolargs.go) — let the carriage return through as well.
// On a surface that paints one row per line (popupBodyWrapped), a string that keeps its newlines
// paints as many rows as it likes, and a tab is the same forgery sideways. Nothing measures a tab:
// lipgloss counts it as one cell while the terminal expands it to the next tab stop, so a field
// carrying one is laid out at one width and drawn at another — the label beside it slides, and a
// clip that believed the first number cuts in the wrong place. A carriage return is the forgery
// backwards: the terminal returns the cursor to column 0, so what follows it overwrites the row
// already drawn instead of continuing it.
//
// That is the right answer for a VALUE and the wrong one for a field. A value's line breaks are the
// thing the human is reading — a command, a patch, a commit message — and the approval pane hangs
// them under a label of their own, indented, where they cannot be mistaken for the pane's own
// structure (argumentValueIndent). A field is a NAME: an argument's key, the task of the sub-agent
// asking, the reason a gate fired. Nothing in a name is layout, so every line break in one is a row
// the pane did not author — and on the approval pane a row is where "Reason:" lives, rendered in
// the same th.popupBody style whatever wrote it. Flattening is the line between the two.
//
// One rune for one rune, so what a later clip counts is what the row will hold (clipRunes on the
// Sub-agent line): a field flattened here is one row wide, and the clip bounds that row.
func flattenField(s string) string {
	if !strings.ContainsAny(s, "\n\t\r") {
		return s // the ordinary case, unallocated
	}
	return fieldBreaks.Replace(s)
}

// fieldBreaks is flattenField's substitution, built once: each character it folds is a single byte
// in and a single byte out, so the replacer walks the field in one pass and leaves every other byte
// — an invalid one included — exactly as it found it.
var fieldBreaks = strings.NewReplacer("\n", " ", "\t", " ", "\r", " ")

// blankLine reports whether ln carries nothing visible — it is empty or whitespace only. It is
// the single definition of "blank" the layout's blank-line hygiene rests on: the commit-time
// trim, the streaming preview's trim, and the markdown collapse all ask this one question.
func blankLine(ln string) bool {
	return strings.TrimSpace(ln) == ""
}

// trimBlankLines drops the leading and trailing blank lines of s, leaving its interior intact.
// Model text routinely arrives padded with a trailing "\n\n" (and sometimes a leading one); each
// such line renders as a blank row on top of the renderer's own one-line block separator, so the
// transcript grows two- and three-line gaps. Trimming at the commit boundary makes layout.md's
// "exactly one empty line between blocks" true rather than aspirational.
func trimBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	i := 0
	for i < len(lines) && blankLine(lines[i]) {
		i++
	}
	j := len(lines)
	for j > i && blankLine(lines[j-1]) {
		j--
	}
	return strings.Join(lines[i:j], "\n")
}

// trimTrailingBlankLines drops only the trailing blank lines of s. It is the render-time trim for
// the still-streaming buffer: a mid-stream "\n\n" may be a paragraph break the model is about to
// continue, so the buffer itself is never touched and a leading blank line is left alone — only
// the trailing gap, which would otherwise wobble as tokens arrive, is held back from the display.
func trimTrailingBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	j := len(lines)
	for j > 0 && blankLine(lines[j-1]) {
		j--
	}
	return strings.Join(lines[:j], "\n")
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

package tui

// ----------------------------------------------------------------------------
// The block-paint cache (streaming render performance)
// ----------------------------------------------------------------------------
//
// [transcript.renderView] repaints the whole scrollback on every repaint, and while a reply
// streams a repaint happens for every batch of tokens the sink delivers — so a settled block
// twenty messages up was re-parsed as markdown, re-styled and re-wrapped dozens of times a
// second for a paint that never changed a byte. This is the memo that stops that: one
// [blockPaint] per block, keyed by everything the paint depends on, so a steady-state repaint
// costs the LIVE TAIL rather than the whole history.
//
// It is a VALIDATION cache, not an invalidated one: nothing in the transcript notifies it. The
// mutators (addToolResult, setExpanded, the appenders) stay exactly as they were, and a paint
// whose inputs moved is simply not found — the key it was stored under no longer matches the key
// the render computes. That is the whole safety argument, and it holds only as long as the key
// really does name every input, which is what [paintKey] documents field by field.
//
// The one input a key cannot name is the CONTENT of an entry, so the cache leans on the
// transcript's own shape: entries are append-only and their content is immutable once committed
// (transcript.go). The two exceptions are handled rather than assumed — an entry whose innards are
// rewritten in place flips a flag in the same breath — `done` (transcript.addToolResult,
// schedule.foldScheduleEvent) or, for a delegation whose report arrives on its finished phase ahead
// of that pairing, `phase` (transcript.addSubAgentPhase) — which the key reads; and
// transcript.refreshStartup rewrites
// entries[0].startup with NO flag change at all, which is why an entryStartup block is never
// cached. The remaining case is wholesale replacement — transcript.reset drops every entry and
// the caller re-fills the list inside the same Update — and reset clears the cache outright,
// because a head index that is re-used by a different session's entry would otherwise match a key
// that is no longer about it.
//
// "Append-only" is the standing shape and no longer the whole of it: a concurrent fan-out's entries
// are placed at the end of their own RUN rather than at the end of the list (transcript.place, ADR
// 0039), which moves every later entry one index up. That is the same "index i is a different block
// now" hazard as a truncation, and it is answered the same way — dropFrom, from the insertion point.
//
// Storage is a POINTER on the transcript for the reason the entries backing array is shared: the
// Bubble Tea Model is copied by value on every Update (ADR 0011), so a cache held by value would
// be a fresh empty cache in every copy and would never hit. Every copy of the Model points at the
// one cache and writes through it, exactly as setExpanded writes through the entries slice. Its
// methods are all nil-receiver-safe, so a hand-built transcript (every test that writes
// `&transcript{}`) simply renders uncached, and a cold render is always available as the oracle a
// warm one is checked against.

import (
	"strconv"

	"github.com/airiclenz/apogee/internal/domain"
)

// blockShape names which shape [transcript.resolveBlock] answered with for a block — the shape
// itself, not anything derived from it. Every value is spent by that one resolver, as the shape
// field of the [resolvedBlock] it returns, so the enum and the decision that picks from it cannot
// drift apart.
//
// It was redundant when there were three, and deliberately kept: a head that changed branches also
// changed the length of its span, so the flags string already differed (a sub-agent call whose first
// nested entry has not arrived is an ordinary one-entry tool block, and becomes a two-entry run's
// head the moment it does). That redundancy was an accident of the branches renderView happened to
// have, not a property of the design — and the cost of relying on it was that a fourth painter
// sharing a span length with a third would serve the wrong paint with no test able to see it.
// Naming the branch costs an int.
type blockShape int

const (
	shapeEntry         blockShape = iota // renderEntryLines — one entry, one block
	shapeToolRun                         // renderToolBlock over a folded run of same-label calls
	shapeSubAgentRun                     // renderSubAgentRun — the call plus the run nested under it
	shapeToolSuper                       // renderSuperGroup — the umbrella over a run of 2+ groupable calls
	shapeSubAgentGroup                   // renderSubAgentGroup — the folded list of adjacent delegations
)

// entryState is the half of one entry a painter reads that MOVES: the view flags a click flips, the
// pairing that lands a result, and the reading a delegation folds in while its child works. It is
// stated apart from the content beside it because it is exactly the half [paintKey] has to name —
// the cache's whole safety argument (the banner above) is that everything ELSE a painter reads is
// immutable once committed, so a fact landing here is a fact the key packs (spanFlags, spanFills)
// and a fact landing beside it in [paintInput] is one the transcript's append-only shape covers.
type entryState struct {
	expanded     bool                 // view-only block state: false = collapsed (entry.expanded)
	done         bool                 // the call has been paired with its result (entry.done)
	typeExpanded bool                 // view-only state of the type row this entry heads in a super-group
	phase        domain.SubAgentPhase // a delegation head's lifecycle phase, as its child reported it

	// the head of a sub-agent run only: the child's frozen context reading and the model it ran on
	// where that was not the session's own (entry.ctxUsed, entry.ctxLimit, entry.ctxModel)
	ctxUsed  int
	ctxLimit int
	ctxModel string
}

// paintInput is one entry as a PAINTER may see it: exactly the fields the block painters are
// allowed to read, and never the entry itself — the entries backing array is shared by every copy
// of the Model (ADR 0011), so a painter is handed what it needs to draw and nothing it could write
// through — the discipline memberFlags and superRunViews already followed one field at a time, now
// stated once for every field a painter has.
//
// The line it draws is the one [transcript.renderView] already stands on: the WALK reads the entries
// — where a block ends is a question about the list (subAgentGroupAt, subAgentSpan, toolSuperGroup,
// sameLabelRun) — and everything downstream of a block's boundaries reads this record instead.
//
// It is also what turns [paintKey]'s completeness from a remembered rule into a compile-visible
// decision. A painter can paint no fact that is not stated here, and a fact stated here arrives
// through [entry.painted]'s unkeyed literal, which stops compiling until the new field is filled in
// — at the one place where "does this move?" has to be answered, next to the two halves that answer
// it: content the append-only rule covers, state the key must pack.
type paintInput struct {
	kind       entryKind
	depth      int
	text       string
	tool       toolView
	skillSpans []skillSpan
	presented  presentedView
	startup    startupView

	// the mutable half, EMBEDDED so a painter reads in.expanded exactly as it read e.expanded and
	// the key can be derived from precisely this much of the record
	entryState
}

// painted states one entry as the painters' input record. The literal is deliberately UNKEYED: a
// field added to either record fails to compile here until it is stated, which is the mechanism the
// record exists for (see [paintInput]).
func (e entry) painted() paintInput {
	return paintInput{
		e.kind, e.depth, e.text, e.tool, e.skillSpans, e.presented, e.startup,
		entryState{e.expanded, e.done, e.typeExpanded, e.phase, e.ctxUsed, e.ctxLimit, e.ctxModel},
	}
}

// paintInputs states a whole block's entries as the painters' input records, in the order the block
// covers them. [transcript.renderView] builds one of these per block and hands the SAME value to
// [blockKey] and to the painter, so what the key names and what the paint reads cannot part company.
func paintInputs(entries []entry) []paintInput {
	ins := make([]paintInput, len(entries))
	for i := range entries {
		ins[i] = entries[i].painted()
	}
	return ins
}

// headsRun is [entry.headsRun] asked of the record instead of the entry — the same two fields, read
// where a painter can reach them, so the block and the entry carrying it cannot disagree about what
// a delegation is.
func (in paintInput) headsRun() bool {
	return in.kind == entryToolCall && in.tool.headsRun()
}

// paintKey is everything one block's paint depends on besides the immutable content of its
// entries. Two renders that compute the same key MUST produce the same lines and the same target
// marks; a field missing here is a stale paint on screen, so the list is deliberately generous —
// depth and kind are derivable from the entries the key already covers, and are named anyway
// because the cost of naming them is a struct field and the cost of omitting one is a wrong frame.
//
// Its per-entry terms are derived from [paintInput] and from nothing else (blockKey, spanFlags,
// spanFills): the record states what a painter may read, so "is this list complete?" is answered by
// reading one struct rather than by remembering what five painter files touch.
//
// It is a comparable struct compared with ==: every field is a scalar or a string, which is what
// lets the whole check be one equality test rather than a walk.
type paintKey struct {
	shape blockShape // which painter branch drew it
	kind  entryKind  // the head entry's kind — the branch renderEntryLines takes, and the cacheability gate
	depth int        // the sub-agent nesting level the block is railed at
	width int        // the width the block was wrapped to

	// measure is the display-width authority the paint was laid out with (width.go). It is the one
	// part of [theme] this key NAMES, and the only one that has to be named: the terminal's mode-2027
	// answer switches it from WcWidth to GraphemeWidth mid-session (model.go, tea.ModeReportMsg),
	// which re-wraps everything, and the key would otherwise serve paints wrapped by the other
	// method.
	//
	// The COLOURS move too, and are deliberately not named here. A colour-scheme switch rebuilds
	// every style at once (ADR 0040), which is the "theme changes" case this comment used to say did
	// not exist — and the alternative it named is the one taken: the switch CLEARS the cache outright
	// (settingsApplyLocal's applyColorScheme) rather than growing this key into a full theme
	// identity. That keeps a steady-state repaint comparing four scalars and a string, and it is
	// sound for the same reason transcript.reset's clear is: after it, nothing memoised in the
	// previous palette remains to be found.
	measure widthAuthority

	// root is the run the paint was rooted at ([transcript.setRoot], render.go), and the zero value
	// is the whole transcript. It is named because a rooted paint of the SAME entry is a different
	// picture of it: every row is rebased to the root's depth, so the block loses its rail and is
	// wrapped to the wider column that leaves. Two roots never key a stored row apart by index alone
	// — a row is stored by head index, and the same entry has one — so without this a view opened on
	// a child would serve the railed paint the top level had left behind for it.
	root runRef

	span  int    // how many entries the paint covers — a run that grew a member is a different block
	live  bool   // whether the block still holds an open call (blockState.live), which is what makes the star blink
	blink bool   // the frame's star phase, folded in ONLY while live — a settled block paints identically at either phase
	flags string // one byte per covered entry: bit 0 expanded, bit 1 done, bit 2 typeExpanded (spanFlags)

	// The context readings of the entries this paint covers, which a collapsed delegation states on
	// its summary line (subAgentFill). They are the one input that moves with NO other movement the
	// key can see: a UsageEvent appends no entry, extends no span and flips no flag, so a run whose
	// delegate just reported would otherwise keep serving the previous figure until its next tool
	// call. Both halves of each reading are named because both are painted, and the limit moves on
	// its own when the window rebinds under a run that has not reported since.
	//
	// It covers every entry rather than only the head because a block can now state more than one
	// reading: a folded group of delegations paints one summary line per member
	// (renderSubAgentGroup), and a reading landing on the second of them moves no field of the
	// first (spanFills).
	fills string
}

// spanFlags packs the per-entry view state the painters read — expanded, done, the type row's own
// typeExpanded, and a delegation's lifecycle phase — into one comparable string, one byte per entry.
// Those four are the flag-shaped fields of [entryState] — the record's own statement of what one
// entry can move — and the readings beside them are packed by [spanFills], having values rather than
// bits. The phase takes two bits rather than one because it has three states and each is a different paint:
// a delegation not yet started, one running, and one whose child has reported ahead of the group's
// result burst (entry.phase). Every per-entry view FACT
// belongs here, whether or not a painter reads it yet: a state a key ignores is a stale paint served
// after a click that changed something, and that is a failure no golden can see, since the paint it
// asserts is the one the painter would have produced anyway.
//
// It is a string rather than a bitmask because a folded tool run or a sub-agent span has no fixed
// length, and a mask that silently stopped covering the 65th entry of a span would be a stale paint
// rather than a missed optimisation. A one-entry span (the
// overwhelmingly common case) converts through the runtime's single-byte string table and so costs
// no allocation.
func spanFlags(ins []paintInput) string {
	b := make([]byte, len(ins))
	for i := range ins {
		var f byte
		if ins[i].expanded {
			f |= 1
		}
		if ins[i].done {
			f |= 2
		}
		if ins[i].typeExpanded {
			f |= 4
		}
		switch ins[i].phase {
		case domain.SubAgentStarted:
			f |= 8
		case domain.SubAgentFinished:
			f |= 16
		}
		b[i] = f
	}
	return string(b)
}

// spanFills packs the context readings of the entries a paint covers into one comparable string:
// each reading as "<offset>:<used>/<limit>@<model>;", and nothing at all for an entry that carries
// none. It is [spanFlags]' shape for a fact that is a group of values rather than a bit — the same
// reason that one is a string: a block's span has no fixed length, and a key that stopped covering
// the members past some bound would be a stale paint rather than a missed optimisation.
//
// The model belongs in the key because it is a CELL of the same summary line (subAgentSummary) and
// it does not always move with the numbers: a maintenance reading names the child's model while
// leaving its fill exactly where it stood, so a key covering only the pair would hold a paint that
// has lost a cell. It is also why an entry earns a term with the model alone.
//
// The overwhelmingly common block carries no reading anywhere (only a delegation's head does), and
// answers "" without allocating.
func spanFills(ins []paintInput) string {
	var b []byte
	for i := range ins {
		e := ins[i]
		if e.ctxUsed <= 0 && e.ctxLimit <= 0 && e.ctxModel == "" {
			continue
		}
		b = strconv.AppendInt(b, int64(i), 10)
		b = append(b, ':')
		b = strconv.AppendInt(b, int64(e.ctxUsed), 10)
		b = append(b, '/')
		b = strconv.AppendInt(b, int64(e.ctxLimit), 10)
		b = append(b, '@')
		b = append(b, e.ctxModel...)
		b = append(b, ';')
	}
	return string(b)
}

// paintRow is one memoised block: the paint, and the key it was painted under. The key is stored
// WITH the paint rather than used as the map's own key so a head index holds at most one row —
// a block whose state moved replaces its row instead of leaving the old one to accumulate.
type paintRow struct {
	key   paintKey
	paint blockPaint
}

// paintCache is the memo itself: rows by HEAD ENTRY INDEX — the index renderView stamps onto the
// block's click surface, and the only index a multi-entry block has that is its own.
//
// hits and misses are unexported diagnostics for the tests that pin the reuse property (the point
// of the cache is not that the output is right — a cold render proves that — but that the second
// render did not repaint what did not move). Nothing in production reads them.
type paintCache struct {
	rows   map[int]paintRow
	hits   int
	misses int
}

// newPaintCache returns a ready cache. Production builds exactly one, in newModel, and hands it to
// the transcript; everything else runs against a nil cache and renders cold.
func newPaintCache() *paintCache { return &paintCache{rows: make(map[int]paintRow)} }

// lookup returns the paint stored for this head index when it was stored under this exact key, and
// counts the outcome. A nil cache answers "no" without counting: it is not missing, it is absent.
func (c *paintCache) lookup(head int, key paintKey) (blockPaint, bool) {
	if c == nil {
		return blockPaint{}, false
	}
	if row, ok := c.rows[head]; ok && row.key == key {
		c.hits++
		return row.paint, true
	}
	c.misses++
	return blockPaint{}, false
}

// store memoises a freshly painted block. The paint's slices are handed over whole and are never
// written to again — renderView copies elements OUT of a block (appendBlock) and never appends to
// one — so the stored value stays valid however many renders reuse it.
func (c *paintCache) store(head int, key paintKey, paint blockPaint) {
	if c == nil {
		return
	}
	c.rows[head] = paintRow{key: key, paint: paint}
}

// miss records a block that was painted without consulting the cache at all (an uncacheable kind),
// so the counters stay an honest account of "paints this render performed".
func (c *paintCache) miss() {
	if c != nil {
		c.misses++
	}
}

// prune drops every row whose head index no longer addresses an entry. It is the truncation guard:
// a row past the end is a row about a transcript that no longer exists.
func (c *paintCache) prune(entries int) {
	c.dropFrom(entries)
}

// dropFrom drops every row at or after index — every row whose head entry has just stopped being
// the entry it was memoised about. It is the guard for the one movement the validation key cannot
// see: [transcript.place] INSERTS a delegated entry at the end of its own run rather than at the
// end of the list (ADR 0039), and everything from that point on has shifted one index up, so a row
// left behind would be served for a block it is no longer about — with nothing in the key to
// notice, since two blocks of the same kind, depth, width and state key identically.
//
// It is the same cut prune makes, which is why prune is stated in terms of it: a truncation is an
// insertion's mirror, and both are "index i means something else now".
func (c *paintCache) dropFrom(index int) {
	if c == nil {
		return
	}
	for head := range c.rows {
		if head >= index {
			delete(c.rows, head)
		}
	}
}

// clear empties the cache. transcript.reset calls it, and that is the whole of the wholesale-
// replacement guard: /clear and a session switch both drop every entry and re-fill the list before
// anything renders again, so pruning against the entry COUNT would not notice that index 3 is now a
// different conversation's message. The counters are left alone — they measure the cache's
// lifetime, not one session's.
func (c *paintCache) clear() {
	if c == nil {
		return
	}
	clear(c.rows)
}

// blockKey builds the key for the block these records are the input of — ins[0] alone for an
// ordinary entry, the whole folded run or the head plus its sub-agent span otherwise. It takes the
// very value the painter is handed ([paintInputs]) and reads nothing else about the transcript,
// which is what makes "the key names every input" checkable by reading one record rather than by
// remembering what five painter files touch.
//
// live is the caller's because it is the PAINTER's own liveness rule and each branch has a
// different one (blockState.live); root is the caller's for the same reason one level up — it is a
// fact about the paint being composed rather than about the records — and everything else is read
// off the records and the frame.
func blockKey(shape blockShape, ins []paintInput, th theme, width int, blink, live bool, root runRef) paintKey {
	return paintKey{
		shape:   shape,
		kind:    ins[0].kind,
		depth:   ins[0].depth,
		width:   width,
		measure: th.measure,
		root:    root,
		span:    len(ins),
		live:    live,
		blink:   blink && live, // a settled block's paint does not depend on the phase; folding it in anyway would miss on every phase flip
		flags:   spanFlags(ins),
		fills:   spanFills(ins),
	}
}

// paintBlock is the cache's one entry point from the renderer: return the memoised paint when the
// key still matches, otherwise draw it and memoise the result. draw is a closure so the expensive
// call — the markdown parse, the styling, the wrap — is not made at all on a hit, which is the
// entire point.
func (t *transcript) paintBlock(head int, key paintKey, draw func() blockPaint) blockPaint {
	if !key.kind.cacheable() {
		t.paints.miss()
		return draw()
	}
	if paint, ok := t.paints.lookup(head, key); ok {
		return paint
	}
	paint := draw()
	t.paints.store(head, key, paint)
	return paint
}

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
// rewritten in place flips `done` in the same breath (transcript.addToolResult,
// schedule.foldScheduleEvent), which the key reads; and transcript.refreshStartup rewrites
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

// blockShape names which of [transcript.renderView]'s three painter branches produced a paint — the
// branch itself, not anything derived from it.
//
// Today it is redundant, and deliberately kept: a head that changes branches also changes the length
// of its span, so the flags string already differs (a sub-agent call whose first nested entry has not
// arrived is an ordinary one-entry tool block, and becomes a two-entry run's head the moment it
// does). That redundancy is an accident of the three branches renderView happens to have, not a
// property of the design — and the cost of relying on it is that a fourth painter sharing a span
// length with a third would serve the wrong paint with no test able to see it. Naming the branch
// costs an int.
type blockShape int

const (
	shapeEntry       blockShape = iota // renderEntryLines — one entry, one block
	shapeToolRun                       // renderToolBlock over a folded run of same-label calls
	shapeSubAgentRun                   // renderSubAgentRun — the call plus the run nested under it
)

// paintKey is everything one block's paint depends on besides the immutable content of its
// entries. Two renders that compute the same key MUST produce the same lines and the same target
// marks; a field missing here is a stale paint on screen, so the list is deliberately generous —
// depth and kind are derivable from the entries the key already covers, and are named anyway
// because the cost of naming them is a struct field and the cost of omitting one is a wrong frame.
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

	span  int    // how many entries the paint covers — a run that grew a member is a different block
	live  bool   // whether the block still holds an open call (blockState.live), which is what makes the star blink
	blink bool   // the frame's star phase, folded in ONLY while live — a settled block paints identically at either phase
	flags string // one byte per covered entry: bit 0 expanded, bit 1 done, bit 2 typeExpanded (spanFlags)

	// The head's context reading, which a collapsed sub-agent run states on its summary line
	// (subAgentFill). It is the one input that moves with NO other movement the key can see: a
	// UsageEvent appends no entry, extends no span and flips no flag, so a run whose delegate just
	// reported would otherwise keep serving the previous figure until its next tool call. Both
	// halves are named because both are painted, and the limit moves on its own when the window
	// rebinds under a run that has not reported since.
	ctxUsed  int
	ctxLimit int
}

// cacheable reports whether a block with this key may be stored at all. Exactly one kind may not:
// [transcript.refreshStartup] rewrites the start-up box's facts in place without touching a single
// field the key reads, so a cached box would keep saying "connecting" after the model bound late.
// The box is one small block at the very top of the scrollback and is not what the cache is for.
func (k paintKey) cacheable() bool { return k.kind != entryStartup }

// spanFlags packs the per-entry view state the painters read — expanded, done, and the type row's
// own typeExpanded — into one comparable string, one byte per entry. Every per-entry view FACT
// belongs here, whether or not a painter reads it yet: a state a key ignores is a stale paint served
// after a click that changed something, and that is a failure no golden can see, since the paint it
// asserts is the one the painter would have produced anyway.
//
// It is a string rather than a bitmask because a folded tool run or a sub-agent span has no fixed
// length, and a mask that silently stopped covering the 65th entry of a span would be a stale paint
// rather than a missed optimisation. A one-entry span (the
// overwhelmingly common case) converts through the runtime's single-byte string table and so costs
// no allocation.
func spanFlags(entries []entry) string {
	b := make([]byte, len(entries))
	for i := range entries {
		var f byte
		if entries[i].expanded {
			f |= 1
		}
		if entries[i].done {
			f |= 2
		}
		if entries[i].typeExpanded {
			f |= 4
		}
		b[i] = f
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

// blockKey builds the key for the block headed by entries[head] and covering n entries — the head
// alone for an ordinary entry, the whole folded run or the head plus its sub-agent span otherwise.
// live is the caller's because it is the PAINTER's own liveness rule and each branch has a
// different one (blockState.live); everything else is read off the entries and the frame.
func (t *transcript) blockKey(shape blockShape, head, n int, th theme, width int, blink, live bool) paintKey {
	return paintKey{
		shape:    shape,
		kind:     t.entries[head].kind,
		depth:    t.entries[head].depth,
		width:    width,
		measure:  th.measure,
		span:     n,
		live:     live,
		blink:    blink && live, // a settled block's paint does not depend on the phase; folding it in anyway would miss on every phase flip
		flags:    spanFlags(t.entries[head : head+n]),
		ctxUsed:  t.entries[head].ctxUsed,
		ctxLimit: t.entries[head].ctxLimit,
	}
}

// paintBlock is the cache's one entry point from the renderer: return the memoised paint when the
// key still matches, otherwise draw it and memoise the result. draw is a closure so the expensive
// call — the markdown parse, the styling, the wrap — is not made at all on a hit, which is the
// entire point.
func (t *transcript) paintBlock(head int, key paintKey, draw func() blockPaint) blockPaint {
	if !key.cacheable() {
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

package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/format"
)

// ----------------------------------------------------------------------------
// The line-oriented transcript renderer (P2.7 — TUI presentation pass)
// ----------------------------------------------------------------------------
//
// The renderer turns the transcript into the flat slice of physical lines the viewport
// shows. It returns []string (not a joined string) for two reasons: tool results carry
// embedded newlines, so the caller feeds viewport.SetContentLines without re-splitting; and
// the sticky-header overlay and the mouse both address the paint by physical line, which
// they can only do over the exact lines the viewport stores. Every element is a single
// physical line (no embedded newline), so a line index means the same thing to the painter,
// the viewport and a click.
//
// The look mirrors layout.md: the last user prompt is a full-width white-on-dark-gray block;
// the assistant and tool headers lead with ✦; a tool header carries its label — and, over a
// grouped run, its member count — with the target leading a ┝/┕ tree branch beneath it, so a
// single call and a grouped run share one grammar; one blank line separates every block. Sub-agent depth (Phase 3) indents a whole block by two
// columns per level — the tree-branch and depth-indent primitives are built here now so the
// P3.14 sub-agent renderer extends these seams rather than reworking them.

// userBlock is the line range a single user prompt occupies within the rendered lines: its
// first line index and its physical-line count. The sticky-header overlay treats each as a
// section header that freezes at the top of the viewport while its replies are on screen.
type userBlock struct{ start, count int }

// renderedTranscript is the renderer's output: the physical lines, the line range of every user
// block, and what each line is to a motionless click. The caller follows the tail unless the
// human has scrolled away, overlays the user block owning the top row as a sticky header, and
// resolves a click through targets (model.go).
type renderedTranscript struct {
	lines      []string
	userBlocks []userBlock
	// targets is PARALLEL to lines — one entry per physical line, the zero value for every line
	// that is not part of a block's click surface. It is built in lockstep with line emission by
	// the painter itself and is never re-derived by a second reader: a click resolves against the
	// exact accounting the paint used (ADR 0030's rule, one authority per measurement).
	targets []lineTarget
}

// targetKind says what one rendered line is to a motionless click (layout.md, "Collapsed and
// expanded blocks"): nothing at all — the overwhelmingly common case, and the zero value — a
// TOGGLE line, or a synthesized remainder MARKER line. A click on a toggle line flips the state of
// the entry the mark names; a click on a marker expands the block whose body the marker is counting
// for, and never collapses it.
//
// targetHeader is named for the line it started on and is no longer only that line: a single tool
// block wears it on EVERY row it paints — its header, its target rows, its body — and a grouped
// block's MEMBER rows wear it too, each naming its own call rather than the block's head, which is
// how a group of ten opens one of them (renderToolBlock, renderToolGroup). What the kind means has
// not moved: it is the toggle, whatever line it lands on.
type targetKind int

const (
	targetNone targetKind = iota
	targetHeader
	targetMarker
)

// lineTarget is one rendered line's click surface: what the line is, and the index into
// transcript.entries of the entry whose expanded state a click there flips
// (transcript.toggleExpanded) — the block's head for every shape but a grouped run, where it is the
// member the row belongs to. The zero value is "no target", which is what every line outside a
// toggleable block carries, so a lookup needs no second sentinel.
type lineTarget struct {
	kind  targetKind
	entry int
}

// lineMark is what one painted line is to a click as the block's OWN painter states it: the kind,
// and which of the block's entries a click there flips, said as an OFFSET from the block's head. A
// single block marks everything 0 — it has one entry and the head is it — and a grouped block marks
// each member row with the member's index, which is that call's offset by construction
// (toolCallRun walks adjacent entries forward, so views[n] is entries[head+n]).
//
// The offset is relative for the reason the kinds carry no entry index at all: a painter knows the
// shape it is drawing and not where in the scrollback it sits, and [transcript.renderView] alone
// turns the pair into an absolute entry. The zero value is "the head, no target", which is what
// every line outside a click surface carries.
type lineMark struct {
	kind   targetKind
	member int
}

// blockPaint is one painted block: its physical lines and, parallel to them, what each line is to
// a click. A block painter says WHAT each of its lines is and WHICH of its own entries owns it;
// [transcript.renderView] alone resolves that to an entry index as it lays the block into the
// transcript — which is why no painter needs to know where in the entry list it sits.
//
// The two slices are grown only through [blockPaint.add], [blockPaint.addFor] and
// [blockPaint.join], so they cannot drift out of lockstep: every line that is appended is marked in
// the same call that appends it.
type blockPaint struct {
	lines   []string
	targets []lineMark
}

// plainPaint is the paint of a block that carries no click surface at all — an assistant answer, a
// note, a start-up box, a ⤷ descent label. Everything the renderer emits that can
// never be toggled goes through here, so "no target" is stated once rather than spelled out at each
// producer. The two kinds that CAN be toggled — a tool block, and a prompt tall enough to collapse —
// mark their own lines as they emit them.
func plainPaint(lines []string) blockPaint {
	return blockPaint{lines: lines, targets: make([]lineMark, len(lines))}
}

// add appends lines that all carry the same target kind and belong to the block's HEAD — the shape
// every painter but the grouped one draws. A WRAPPED header is the reason it takes a slice rather
// than a line: every physical line a header occupies is part of the same click surface (layout.md —
// the click lands on the header, not on its first row), and the same holds for a remainder marker
// narrow enough to wrap.
func (p *blockPaint) add(lines []string, kind targetKind) {
	p.addFor(0, lines, kind)
}

// addFor appends lines belonging to the block's member'th entry — [blockPaint.add] with the offset
// said out loud, for the one painter whose lines do not all belong to its head (renderToolGroup).
// It exists rather than a stamping pass over finished lines because the lines and their marks have
// to be one act (ADR 0030): a second walk deriving whose row is whose would be a second accounting,
// and the two would part company the first time the member shape changed.
func (p *blockPaint) addFor(member int, lines []string, kind targetKind) {
	p.lines = append(p.lines, lines...)
	for range lines {
		p.targets = append(p.targets, lineMark{kind: kind, member: member})
	}
}

// join appends another paint whole — its lines and its own target marks — so a block composed of
// sub-paints (a tool block of one branch or of many) keeps their marks without re-deriving them.
func (p *blockPaint) join(q blockPaint) {
	p.lines = append(p.lines, q.lines...)
	p.targets = append(p.targets, q.targets...)
}

// railed frames the paint for its sub-agent depth (railLines) while carrying its target marks
// through untouched: the rail prefixes each line in place and adds none, so the marks stay in
// lockstep with the lines they belong to.
func (p blockPaint) railed(th theme, depth int) blockPaint {
	return blockPaint{lines: railLines(th, p.lines, depth), targets: p.targets}
}

// renderView renders the committed entries plus any in-progress assistant buffer into the
// viewport's lines, recording the line range of every user block. Blocks are separated by one
// line (layout.md), railed at the depth the two blocks share so a sub-agent run's frame is
// continuous through its separators (railSpacer).
//
// The in-progress buffer is painted by the SAME rules as a committed block of its depth
// (transcript.pendingDepth): railed where it was streamed, announced by the ⤷ descent label when it
// opens a level, and elided outright while it streams inside a collapsed run (insideCollapsedRun) —
// there the head already blinks and carries the live gist, and the status line names the delegate.
//
// blink is this frame's phase of the live star ([spinnerAnim.blink]): it reaches only the header
// glyph of a block that still holds an open call, and every other line of the transcript paints
// identically at either phase. It is a PARAMETER rather than transcript state because the phase
// belongs to the frame being drawn and not to the scrollback — the same entries painted a tick
// later are the same entries (ADR 0011: the Model is copied by value, and the renderer stays a
// pure function of what it is handed).
func (t *transcript) renderView(th theme, width int, blink bool) renderedTranscript {
	if width < 1 {
		width = 1
	}
	var lines []string
	var targets []lineTarget
	var userBlocks []userBlock

	// prevBlockDepth is the depth of the block appended last — the left half of the next
	// spacer's join. It is deliberately per-APPENDED-BLOCK rather than per-entry (the loop's
	// prevDepth below): the ⤷ label blocks the descent loop emits carry depths of their own,
	// and a spacer's rail follows the blocks it actually sits between.
	prevBlockDepth := 0
	// head is the index into t.entries of the block's FIRST entry — the one a click on the block
	// toggles wherever the block has a single state, and the base the painter's per-line member
	// offsets are added to where it does not (a grouped run, whose members each own their state).
	// It is spent only on the lines the block itself marked as a click surface, so a block that
	// marks none — every kind but a tool block — may pass whatever index it sits at.
	appendBlock := func(isUser bool, depth, head int, block blockPaint) {
		if len(lines) > 0 {
			lines = append(lines, railSpacer(th, min(prevBlockDepth, depth)))
			targets = append(targets, lineTarget{}) // a separator belongs to neither block
		}
		if isUser {
			userBlocks = append(userBlocks, userBlock{start: len(lines), count: len(block.lines)})
		}
		// The line and its mark are appended in ONE loop, and the mark is read defensively, so the
		// map is exactly as long as the lines whatever a painter handed over. That length is the
		// mouse's whole safety: a target index that outlived its line is a click toggling some
		// other block, on a path where the alternative to a rule is a panic mid-repaint.
		for i, ln := range block.lines {
			lines = append(lines, ln)
			target := lineTarget{}
			if i < len(block.targets) && block.targets[i].kind != targetNone {
				mark := block.targets[i]
				target = lineTarget{kind: mark.kind, entry: head + mark.member}
			}
			targets = append(targets, target)
		}
		prevBlockDepth = depth
	}

	// Drop memoised paints for entries the transcript no longer has (paintcache.go). It runs
	// before the loop so a render never reads a row about a block that is gone.
	t.paints.prune(len(t.entries))

	prevDepth := 0
	// previewAt is the index the live buffer paints AT — the end of the run that filled it
	// (transcript.runEnd), which is where that run's blocks end rather than where the list does. In
	// a serial session, and for the human's own conversation, the two are the same index and the
	// preview lands exactly where it always did; while siblings run at once it lands inside the
	// child that is talking instead of after whichever child was announced last. −1 means the
	// buffer is not painted this frame at all: nothing is streaming, or the run holding it is
	// collapsed and its whole span elided with it.
	previewAt := -1
	if t.streaming && !insideCollapsedRun(t.entries, t.pendingRun) {
		previewAt = t.runEnd(t.pendingRun.spawn)
	}
	// paintPreview appends the in-progress buffer as a block of its own run, at index at. The
	// buffer is trimmed of its trailing blank lines for display only: the buffer keeps them (a
	// mid-stream "\n\n" may be a paragraph break about to be continued), but the preview must not
	// grow a wobbling gap above the footer. An empty buffer still renders its lone marker line, so
	// the human sees that streaming has begun.
	paintPreview := func(at int) {
		// The live buffer is painted at the depth that FILLED it (transcript.pendingRun), like
		// every committed block above — the descent label included, which the preview owes itself
		// when the delegate has streamed before producing any entry to announce the level.
		if t.pendingRun.depth > prevDepth {
			for d := prevDepth + 1; d <= t.pendingRun.depth; d++ {
				appendBlock(false, d, at, plainPaint(renderSubAgentLabel(th, d, width)))
			}
		}
		preview := renderEntryLines(th, entry{
			kind:  entryAssistant,
			text:  trimTrailingBlankLines(t.pending),
			depth: t.pendingRun.depth,
		}, width, blink)
		appendBlock(false, t.pendingRun.depth, at, preview)
		prevDepth = t.pendingRun.depth
	}

	for i := 0; i < len(t.entries); i++ {
		// The preview is painted the moment the walk reaches its run's end. The test is >= rather
		// than == because the walk SKIPS index ranges — a collapsed run's span, a folded tool run's
		// members — and a preview whose index fell inside one would otherwise never be painted.
		if previewAt >= 0 && i >= previewAt {
			paintPreview(i)
			previewAt = -1
		}
		e := t.entries[i]
		// Open a ⤷ sub-agent label whenever a run descends to a deeper level than the
		// previous block — a 0→1 (or 1→2) transition announces the nested section once,
		// per level, until the stream climbs back out (P3.14).
		if e.depth > prevDepth {
			for d := prevDepth + 1; d <= e.depth; d++ {
				appendBlock(false, d, i, plainPaint(renderSubAgentLabel(th, d, width)))
			}
		}
		// A sub-agent run is ONE block while it is collapsed (layout.md): its head paints with the
		// cascading summary and the whole span is then skipped outright, which is what elides the
		// inner blocks, their ⤷ labels, and every rail and spacer among them — nothing is painted
		// and afterwards taken back, and the descent logic above never fires because it never sees
		// a deeper entry. Expanded, only the head is painted here and the loop walks into the span
		// exactly as it always has, so every inner block keeps its OWN state and a nested run
		// collapses inside an expanded parent by this same rule, at every depth.
		if span := subAgentSpan(t.entries, i); span > 0 {
			// The paint covers the head AND its span: the collapsed summary counts the work behind
			// the header (subAgentSummary) and the star asks the span whether anything is still open,
			// so a nested entry arriving or landing its result is a different block (paintcache.go).
			key := t.blockKey(shapeSubAgentRun, i, span+1, th, width, blink,
				!e.done || anyOpenCall(t.entries[i+1:i+1+span]))
			appendBlock(false, e.depth, i, t.paintBlock(i, key, func() blockPaint {
				return renderSubAgentRun(th, e, t.entries[i+1:i+1+span], width, blink)
			}))
			if !e.expanded {
				i += span
			}
		} else if run := toolCallRun(t.entries, i); len(run) > 1 {
			// Consecutive same-label tool calls fold into one block at render time, so a batch of
			// reads is one header plus an aligned branch per file. The entry list is untouched: a
			// call that arrives mid-stream joins its group on the next repaint for free, and a run
			// is same-depth by construction, so the label logic above fires exactly as before.
			//
			// The group's liveness is the group's, not its head's: a batch of reads whose first call
			// has landed and whose last has not is still working, and the one star over them all says
			// so. The run is entries[i:i+len(run)] by construction (toolCallRun walks adjacent
			// entries forward), so the views' own entries are what the rule reads — and the same
			// construction is what lets the members' EXPANDED flags be read off the run in view order
			// and their rows be marked back by offset (blockPaint.addFor).
			//
			// Every one of those flags is in the paint key already: blockKey spans the whole run and
			// spanFlags packs expanded at bit 0 of each covered entry, so opening the tenth member of a
			// group is a different key and a fresh paint (paintcache.go).
			key := t.blockKey(shapeToolRun, i, len(run), th, width, blink,
				anyOpenCall(t.entries[i:i+len(run)]))
			block := t.paintBlock(i, key, func() blockPaint {
				return renderToolBlock(th, run, railedWidth(width, e.depth), blockState{
					expanded: e.expanded,
					live:     key.live,
					blink:    blink,
					members:  memberFlags(t.entries[i : i+len(run)]),
				}).railed(th, e.depth)
			})
			appendBlock(false, e.depth, i, block)
			i += len(run) - 1
		} else {
			// A tool call is the only single-entry kind with a live star, and entrySchedule paints
			// static by construction (renderEntryLines), so everything else keys as settled.
			key := t.blockKey(shapeEntry, i, 1, th, width, blink, e.kind == entryToolCall && !e.done)
			appendBlock(e.kind == entryUser, e.depth, i, t.paintBlock(i, key, func() blockPaint {
				return renderEntryLines(th, e, width, blink)
			}))
		}
		prevDepth = e.depth
	}
	if previewAt >= 0 {
		paintPreview(len(t.entries))
	}
	return renderedTranscript{lines: lines, userBlocks: userBlocks, targets: targets}
}

// renderLines is the line slice alone — the viewport content and the substring-test surface — at
// the star's SETTLED phase: the blink is a fact about the frame being drawn, and a caller that has
// no frame (a width probe, a substring assertion) has no phase either.
func (t *transcript) renderLines(th theme, width int) []string {
	return t.renderView(th, width, false).lines
}

// renderEntryLines renders one committed entry into its physical lines, framed for its
// sub-agent depth, plus what each of those lines is to a click. The user prompt is a full-width
// block; everything else hangs off a marker. A Depth > 0 entry is wrapped to the narrower column
// left of its rail gutter, then each line is prefixed by the rail (P3.14) so the nested block
// reads as a framed sub-section.
//
// Four kinds carry a click surface, for the one reason: they have two states to toggle between. A
// tool call, a scheduled Firing and a stray result share ONE block painter and so one click
// surface: every row of a block that hides something, with the remainder marker keeping its
// open-only meaning and, inside a folded run, each member's rows marked for that member. A prompt
// tall enough to collapse marks every row it paints too, and always has — the whole block is the
// toggle in both shapes (layout.md, "Collapsed and expanded blocks"). Every other kind comes back as plainPaint: a note or an answer paints one way
// whatever is asked of it, and a click there keeps its selection meaning. blink narrows further
// still — a tool call is the only entry that can still be WAITING for something, so it is the only
// header with a star to blink (layout.md, "The live star").
func renderEntryLines(th theme, e entry, width int, blink bool) blockPaint {
	inner := railedWidth(width, e.depth)
	switch e.kind {
	case entryUser:
		return renderUserBlock(th, glyphUser+" ", e.text, e.skillSpans, inner, e.expanded).railed(th, e.depth)
	case entryInterjected:
		// The human's mid-Exchange remark: the same block the prompt gets — it is the same voice —
		// under the ⧖ marker that says it arrived while the model was already working (ADR 0025).
		// Its skill tokens light up the sent block's way, for the same reason: a skill rides an
		// interjection (ADR 0027), and the record of what the model was given must not depend on
		// whether the remark was delivered mid-run or flushed into a new Exchange.
		return renderUserBlock(th, glyphInterject+" ", e.text, e.skillSpans, inner, e.expanded).railed(th, e.depth)
	case entryAssistant:
		marker := glyphAssistant + " "
		body := renderMarkdownBody(th, e.text, inner-th.measure.Width(marker))
		return plainPaint(railLines(th, withMarker(th, marker, body), e.depth))
	case entryToolCall:
		return renderToolBlock(th, []toolView{e.tool}, inner, blockState{
			expanded: e.expanded,
			live:     !e.done,
			blink:    blink,
		}).railed(th, e.depth)
	case entrySchedule:
		// A Firing wears the tool block's shape under the /sessions tag's ⟳ (layout.md, "The firing
		// block"), so one Firing reads the same in the chat and in the browser. live and blink stay
		// false BY CONSTRUCTION rather than by accident: the spinner belongs to the worker driving
		// this session's Exchange and the session is idle while a Firing runs, so an animated header
		// here would claim work is happening in this session. What says the run is going is the
		// block's own static summary (schedule.go, scheduleRunningSummary).
		return renderToolBlock(th, []toolView{e.tool}, inner, blockState{
			expanded: e.expanded,
			glyph:    scheduleTagGlyph,
		}).railed(th, e.depth)
	case entryToolResult:
		return renderOrphanResult(th, e.text, inner, e.expanded).railed(th, e.depth)
	case entryError:
		return plainPaint(railLines(th, hangingWrap(th, th.errorText, glyphAssistant+" ", e.text, inner), e.depth))
	case entryNote:
		return plainPaint(railLines(th, hangingWrap(th, th.noteText, "· ", e.text, inner), e.depth))
	case entryPresented:
		return plainPaint(railLines(th, renderPresentedBlock(th, e.presented, inner), e.depth))
	case entryStartup:
		return plainPaint(railLines(th, renderStartupBox(th, e.startup, inner), e.depth))
	default:
		return blockPaint{}
	}
}

// renderSubAgentLabel renders the one-line ⤷ sub-agent header that opens a contiguous run of
// sub-agent (Depth > 0) blocks (P3.14). It is itself framed at the run's depth, so the label
// sits inside the same rail as the block it announces.
func renderSubAgentLabel(th theme, depth, width int) []string {
	inner := railedWidth(width, depth)
	body := hangingWrap(th, th.subRail, glyphSubLabel+" ", subAgentLabel, inner)
	return railLines(th, body, depth)
}

// subAgentToolName is the raw tool id whose call block heads a sub-agent run. The span rule matches
// on the view's retained name (toolView.name, which the codec round-trips) rather than on the
// "Sub-Agent" label, so a relabelling cannot silently switch the rule off and a third-party tool
// that happens to share the label cannot switch it on.
const subAgentToolName = "sub_agent"

// subAgentSpan is the length of the run entries[i] heads: the maximal following stretch of entries
// nested deeper than it. That stretch IS the run — the transcript records a sub-agent's work
// head-first and folds the report back into the head (transcript.addToolResult), so everything the
// child did lies between the call and the next entry standing at the parent's own depth. Nothing
// marks the span; it is derived at paint from the depths already on the entries, exactly as
// renderView's ⤷ descent labels are.
//
// It answers 0 for anything that is not a sub-agent call, and for a run that produced no nested
// entry at all (a child that failed before its first event) — either way the head is an ordinary
// tool block with nothing behind it, and renderView paints it as one.
func subAgentSpan(entries []entry, i int) int {
	head := entries[i]
	if head.kind != entryToolCall || head.tool.name != subAgentToolName {
		return 0
	}
	n := 0
	for j := i + 1; j < len(entries) && entries[j].depth > head.depth; j++ {
		n++
	}
	return n
}

// insideCollapsedRun reports whether a block about to be painted at depth would land inside a
// sub-agent run that is currently COLLAPSED — the question subAgentSpan answers for committed
// entries, asked on behalf of the one block that is not in the list: the live streaming preview
// (renderView). A collapsed run stands alone and everything railed beneath it is elided
// (layout.md), and a delegate's answer is beneath it from its first streamed token, not only once
// its MessageEvent commits an entry the span rule can see.
//
// It keys on the HEAD rather than on the span being non-empty, because a child that has streamed
// but not yet called a tool has produced no nested entry at all — subAgentSpan is 0 there, and a
// rule reading it would let exactly the first tokens through. Every enclosing run is asked, so a
// nested run streaming inside a collapsed parent is elided by the parent's state as well as by its
// own: the chain is walked by SPAWNING CALL — this run's head, then the run that head sits in, up
// to the top — which is what keeps the answer exact while siblings run at once (ADR 0039), where
// the most recent open head at a level names whichever child was announced last rather than the one
// that is talking.
//
// A run with no spawning call to walk from — a hand-built test transcript, a record replayed from a
// blob written before the id was stamped — still answers by the depth rule this began as: the most
// recent still-open head at each enclosing level.
func insideCollapsedRun(entries []entry, run runRef) bool {
	if run.spawn == "" {
		return insideCollapsedRunAtDepth(entries, run.depth)
	}
	for spawn := run.spawn; spawn != ""; {
		head, ok := runHead(entries, spawn)
		if !ok {
			return false
		}
		if !head.expanded {
			return true
		}
		spawn = head.spawnCallID // this run is open: the run it sits in may still be collapsed
	}
	return false
}

// runHead finds the sub_agent call block that opened the run spawn names.
func runHead(entries []entry, spawn string) (entry, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		if h := entries[i]; h.kind == entryToolCall && h.callID == spawn && h.tool.name == subAgentToolName {
			return h, true
		}
	}
	return entry{}, false
}

// insideCollapsedRunAtDepth is insideCollapsedRun's answer for a run with no spawning call id: each
// enclosing level's most recent still-open sub_agent head, which is the only handle a stream of
// depths alone offers. It was the whole rule while delegation was serialized (ADR 0014) and is
// exact wherever it still applies — a session with one delegate at a time, and every replayed
// record, whose entries are settled and stream nothing.
func insideCollapsedRunAtDepth(entries []entry, depth int) bool {
	for level := depth - 1; level >= 0; level-- {
		for i := len(entries) - 1; i >= 0; i-- {
			head := entries[i]
			if head.kind != entryToolCall || head.done ||
				head.depth != level || head.tool.name != subAgentToolName {
				continue
			}
			if !head.expanded {
				return true
			}
			break // this level's run is open: a deeper one may still be collapsed
		}
	}
	return false
}

// renderSubAgentRun paints the head block of a sub-agent run — the whole of what a COLLAPSED run
// shows, since renderView elides its span, and the opening block of an expanded one. The two states
// differ in exactly what layout.md says they do: collapsed, the head's summary slot carries the
// cascading summary of the work behind it; expanded, the head is an ordinary block with its full
// report and the span paints as it always has.
//
// A COLLAPSED run's head is ONE summarised line: the report body is elided along with the span,
// because the summary slot already carries that report's first line and no block says the same
// thing twice in two adjacent rows (layout.md, "A sub-agent run collapses to its call block"). The
// count in that line is what says work is hidden behind the header, so the run needs no
// "+N more lines" marker to say it too — and the header is a target however short the report is
// (blockState.elides), so nothing is unreachable.
//
// The head's view is COPIED before its summary is replaced and its body dropped, so both are
// paint-time acts on facts the entry keeps whole — the same discipline the body's truncation follows
// (collapsedDetails), and the reason expanding shows the report the run actually returned.
// A run is live until its REPORT lands, and its span is asked as well as its head: a report that
// never arrived (a child cancelled mid-tool) leaves the head open, and — the mirror case — a head
// already reported over a call that never got its result leaves work still standing behind the
// star. Either way the block still contains an open call, which is exactly what layout.md makes the
// star's rule.
func renderSubAgentRun(th theme, head entry, span []entry, width int, blink bool) blockPaint {
	view := head.tool
	if !head.expanded {
		view.Summary = subAgentSummary(th.measure, head, span)
		view.Details = toolBody{} // the zero body: no lines, and so nothing to lay out beneath
	}
	return renderToolBlock(th, []toolView{view}, railedWidth(width, head.depth), blockState{
		expanded: head.expanded,
		elides:   true,
		live:     !head.done || anyOpenCall(span),
		blink:    blink,
	}).railed(th, head.depth)
}

// subAgentSummary words a collapsed run's one line: how much work happened in there, how full the
// delegate's own context got doing it, then its gist (layout.md, "A sub-agent run collapses to its
// call block"). The count is TRANSITIVE by construction — the span holds every entry of every level
// below the head, so counting its tool calls counts the grandchildren too, and the same rule read at
// a deeper head gives that level's own total without a second rule.
//
// The fill is the exact opposite: it is the head's OWN frozen reading (subAgentFill) and never a
// nested run's, because each agent fills a window of its own. It sits between the count and the gist
// so the gist — the one part with no bound on its length — is what a narrow terminal clips.
//
// A run with nothing to say beyond the count — no reading yet, no call open, or a report that
// carried no line at all — keeps the count alone rather than trailing an empty separator.
//
// The line is marked QUOTED (branchSummary) for what its last cell is: the child's own report, or
// the phrase for the call it has open. Nothing respells it in either case — this is composed at
// paint, long after the shortening seam ran on the way in — so the mark is a statement about the
// text rather than a switch, and it is the one that stays true if a seam ever reads it.
func subAgentSummary(measure widthAuthority, head entry, span []entry) branchSummary {
	calls := 0
	for i := range span {
		if span[i].kind == entryToolCall {
			calls++
		}
	}
	text := plural(calls, "tool call")
	if fill := subAgentFill(head); fill != "" {
		text += " · " + fill
	}
	if gist := subAgentGist(measure, head, span); gist != "" {
		text += " · " + gist
	}
	return quotedSummary(detailLine{Text: text})
}

// subAgentFill spells the run head's context reading as the gauge spells one — "12k/32k", the same
// coarse form the status line's window uses ([format.Tokens]), so the two readings on screen
// are read in one language. It is the pair frozen on the entry when the reading folded
// (transcript.applyUsage), which is why a finished run keeps saying what it filled.
//
// A missing half means no cell at all: a fill without its limit is a number with no scale, and the
// status gauge hides itself on the very same condition rather than showing one. That is also what a
// run whose child never reported paints — and what a session recorded before the reading existed
// decodes to (transcriptcodec.go), so nothing needs a migration to look right.
func subAgentFill(head entry) string {
	if head.ctxUsed <= 0 || head.ctxLimit <= 0 {
		return ""
	}
	return format.Tokens(head.ctxUsed) + "/" + format.Tokens(head.ctxLimit)
}

// subAgentGist is the second half of a collapsed run's summary, in the two tempi layout.md gives it.
// While the run works — the head has no result yet — it is the live phrase for the call the span has
// open: verb and shortened target together (toolPhrase, activity.go), worded from the same view the
// status line reads its verb from, but KEEPING the target the status line sheds. Inside a collapsed
// run the gist is the only live view of what the child is touching, since the block that would have
// named that target is elided. It is read off the span at paint rather than kept as a second copy of
// the activity state. The MOST RECENT open call is the honest one when several are open at once:
// it is the work the child turned to last.
//
// Once the report lands the head has a gist of its own and that is the line — the summary a short
// report was compressed to, or, where the report was long enough to become a body, its first line.
//
// measure is the width authority the live phrase's target cap is spent through (toolPhrase,
// activity.go), threaded down from the theme the render layer already carries.
func subAgentGist(measure widthAuthority, head entry, span []entry) string {
	if head.done {
		if head.tool.Summary.Text != "" {
			return head.tool.Summary.Text
		}
		if body := head.tool.Details; body.len() > 0 {
			return body.all()[0].Text
		}
		return ""
	}
	for i := len(span) - 1; i >= 0; i-- {
		if e := span[i]; e.kind == entryToolCall && !e.done {
			return toolPhrase(measure, e.tool)
		}
	}
	return ""
}

// renderUserBlock renders something the human said as a full-width white-on-dark-gray block: the
// marker on the first line, a hanging two-column indent on wrapped continuation lines, and
// the dark-gray background padded across the whole width on every line. The skills the message
// invoked are shown IN it: spans locates their "/tokens" in text and those very runs are painted
// in the skill violet (userBlockCellSpans), so the record of what the model was given is the
// sentence the human wrote rather than a badge restating it beside them.
//
// marker is the block's lead ("❯ " for a submitted prompt, "⧖ " for a delivered interjection):
// the two are the same voice and so share one shape, and the glyph is the whole of the
// difference the reader needs.
//
// A body that soft-wraps past promptCollapsedRows rows COLLAPSES to that many, the last of them
// truncated to make room for the right-aligned see-more marker counting what is left behind
// (promptSeeMore). expanded is the entry's own view state (transcript.setExpanded) and opens the
// body whole, closed again by the see-less marker the block then trails. Both are paint-time acts
// on a body the entry keeps in full — the trigger is measured against the width being painted, so
// a resize alone can collapse a prompt or open one, and nothing about the entry changes
// (layout.md, "Collapsed and expanded blocks").
//
// The accent is painted onto rows the block SHOWS: a token on a row the collapse hid simply is not
// painted, and one on the truncated row carrying the see-more marker is held inside that row's own
// content (promptMarkerContentCells), so an accent can never reach across the gap and recolour the
// marker. The transcript's drag-selection is shaded onto the composed frame afterwards
// (highlightTranscript, mouse.go), which is what keeps a selected token reading as SELECTED.
//
// The block is a click surface exactly when it has two shapes to move between, and then it is a
// click surface WHOLE: every row it paints is marked targetHeader — the marker row and the see-less
// row among them — because layout.md makes the whole prompt the toggle rather than one line of it.
// The mark is state-INDEPENDENT for the tool block's reason: an expanded prompt keeps it, which is
// the click that closes it again. A body inside the cap marks nothing at all, so a click on an
// ordinary prompt keeps its selection meaning.
func renderUserBlock(th theme, marker, text string, spans []skillSpan, width int, expanded bool) blockPaint {
	// The spans are stated in the text's OWN offsets, so they are re-based before the text is
	// expanded, and the expanded text is what both the wrap and the accent map are handed — a block
	// whose rows held spaces where the spans still counted a tab would light up the wrong run
	// (expandTabs).
	spans = expandTabsInSpans(text, spans)
	text = expandTabs(text)
	var out []string
	trailer := ""
	collapsible := false
	if text != "" {
		body := hangingPrefixes(th, marker, text, width)
		collapsible = len(body) > promptCollapsedRows
		shown, hidden := body, 0
		switch {
		case !expanded:
			shown, hidden = splitAtCap(body, promptCollapsedRows)
		case collapsible:
			trailer = promptMarkerRow(th, "", promptSeeLess, width)
		}
		accents := userBlockCellSpans(th, marker, text, width, spans)
		for i, ln := range shown {
			row, limit := "", th.measure.Width(ln) // limit: the cells of this row the block's own content holds
			if hidden > 0 && i == len(shown)-1 {
				row = promptMarkerRow(th, ln, promptSeeMore(hidden), width)
				limit = min(limit, promptMarkerContentCells(th, promptSeeMore(hidden), width))
			} else {
				// Squared in the authority's measure, the way promptMarkerRow below pads its own row,
				// rather than by a lipgloss Width style: lipgloss pads — and past its width WRAPS — in
				// GraphemeWidth whatever the painter is doing (ADR 0030). Now that wrapText breaks in
				// the painter's measure, a line it calls exactly width cells wide can measure wider to
				// lipgloss (any VARIATION SELECTOR-16 cluster does), and a Width style would fold that
				// one line into two — smuggling a "\n" into a single element of a []string the whole
				// line-oriented renderer counts rows with.
				row = th.userBlock.Render(squareLine(th.measure, ln, width))
			}
			out = append(out, accentRow(th, row, i, accents, limit))
		}
	}
	if trailer != "" {
		out = append(out, trailer)
	}
	kind := targetNone
	if collapsible {
		kind = targetHeader
	}
	var paint blockPaint
	paint.add(out, kind)
	return paint
}

// promptMarkerRow composes one row of a user block that carries a collapse marker near its right
// edge: the row's own content on the left, the highlighted marker held promptMarkerMargin columns
// off the block's right edge, and the block's dark-gray field spanning both the gap before it and
// that margin after — three independently styled segments on one line (the footerContent idiom), so
// the marker keeps its own colour while the row stays a solid block. The margin matters because the
// marker carries a background of its own: run flush to the edge and its highlight would touch the
// block's boundary and read as clipped. content is the unstyled row text ("" for the see-less row,
// which carries none).
//
// The content is truncated with the house ellipsis to leave promptMarkerGap columns clear before
// the marker, which is what makes the collapsed shape exactly promptCollapsedRows rows: the marker
// rides a content row instead of taking one of its own. A width too narrow for the marker itself
// truncates the marker rather than overrunning the block — the row is never wider than the block it
// belongs to, at any width the painter is handed.
func promptMarkerRow(th theme, content, marker string, width int) string {
	inner := max(0, width-promptMarkerMargin) // the columns left once the right margin is reserved
	tail := th.promptToggle.Render(th.measure.Truncate(marker, inner, "…"))
	tw := th.measure.Width(tail)
	content = th.measure.Truncate(content, promptMarkerContentCells(th, marker, width), "…")
	pad := strings.Repeat(" ", max(0, inner-tw-th.measure.Width(content)))
	margin := strings.Repeat(" ", min(promptMarkerMargin, width))
	return th.userBlock.Render(content+pad) + tail + th.userBlock.Render(margin)
}

// promptMarkerContentCells is how many columns of its OWN content a row carrying marker keeps: the
// block's width less the right margin, the marker itself and the gap held clear before it. It is
// the truncation promptMarkerRow applies, named so the accent pass can respect the same bound —
// shade past it and a token would recolour the gap and then the marker, which is apogee talking
// rather than the human (renderUserBlock).
func promptMarkerContentCells(th theme, marker string, width int) int {
	inner := max(0, width-promptMarkerMargin)
	tail := th.promptToggle.Render(th.measure.Truncate(marker, inner, "…"))
	return max(0, inner-th.measure.Width(tail)-promptMarkerGap)
}

// skillCellSpan is one row-slice of an accented "/token" in a sent user block: the body row it
// falls on (indexed into the block's wrapped rows BEFORE any collapse, so the caller can drop the
// ones its collapse hid) and the display-cell range [c0,c1) the token covers there. It is
// [inputCellSpan]'s transcript twin — the same idea against the other wrap.
type skillCellSpan struct{ row, c0, c1 int }

// userBlockCellSpans maps a sent message's [skillSpan]s — byte ranges into its own text, captured
// at send time — onto the rows and display cells the user block draws them at. A token confined to
// one row yields one span; a token straddling a soft-wrap yields one per row it spans, so it lights
// up on both halves exactly as the prompt box's accent does (inputaccent.go).
//
// The rows are re-wrapped here rather than passed in, deliberately: wrapText is a pure function of
// the same three arguments hangingPrefixes just gave it, so this asks the SAME oracle the block's
// rows came off rather than trying to unpick a marker prefix back off them.
//
// The mapping itself is an alignment walk, because the wrap DROPS characters — the space it broke
// at, the newline it broke on — and inserts none. Each row's runes are matched forward against the
// text from where the previous row left off, which yields every rune's own byte offset in the text
// and so the cell columns of any range of it. A row that fails to align (text and rows out of step,
// which nothing in wrapText should produce) simply stops the walk: the block then paints plain,
// which is what an entry recorded before spans existed already looks like.
func userBlockCellSpans(th theme, marker, text string, width int, spans []skillSpan) []skillCellSpan {
	if len(spans) == 0 || text == "" {
		return nil
	}
	lead := th.measure.Width(marker) // the marker, and on every later row the blank indent matching it
	var out []skillCellSpan
	pos := 0 // how far into text the walk has consumed
	for r, row := range wrapText(th, text, max(1, width-lead)) {
		runes := alignRow(text, &pos, row)
		for _, sp := range spans {
			lo, hi := -1, -1
			for _, ru := range runes {
				if ru.src < sp.start || ru.src >= sp.end {
					continue
				}
				if lo < 0 {
					lo = ru.at
				}
				hi = ru.end
			}
			if lo < 0 {
				continue // no part of this token landed on this row
			}
			out = append(out, skillCellSpan{
				row: r,
				c0:  lead + th.measure.Width(row[:lo]),
				c1:  lead + th.measure.Width(row[:hi]),
			})
		}
	}
	return out
}

// alignedRune is one rune of a wrapped row placed back in the text it was wrapped from: where it
// sits in the row (at, end — byte offsets, so the row can be sliced for a width) and where it came
// from in the text (src — the offset a span is stated in).
type alignedRune struct{ at, end, src int }

// alignRow walks one wrapped row against the text it came from, advancing *pos past what the row
// consumed. A rune the wrap dropped is skipped over on the way: the search for each of the row's
// runes runs forward through the text until it matches, which is exact for a wrap that only ever
// drops whitespace at the break it took. It stops at the first rune it cannot find, leaving the
// rest of the row unmapped rather than guessing.
func alignRow(text string, pos *int, row string) []alignedRune {
	out := make([]alignedRune, 0, len(row))
	for i, ch := range row {
		for *pos < len(text) {
			r, size := utf8.DecodeRuneInString(text[*pos:])
			if r == ch {
				break
			}
			*pos += size
		}
		if *pos >= len(text) {
			break
		}
		out = append(out, alignedRune{at: i, end: i + utf8.RuneLen(ch), src: *pos})
		*pos += utf8.RuneLen(ch)
	}
	return out
}

// accentRow paints the skill accent onto one composed row of a user block: every mapped cell span
// belonging to row index, clamped to limit — the cells that row's own content occupies — and
// re-styled in place (shadeCells, the accentTokens idiom). The flanking cells keep the block's own
// styling, so the row stays one solid dark-gray band with a violet run in it.
func accentRow(th theme, row string, index int, accents []skillCellSpan, limit int) string {
	for _, a := range accents {
		if a.row != index {
			continue
		}
		c0 := clampInt(a.c0, 0, limit)
		c1 := clampInt(a.c1, c0, limit)
		if c1 <= c0 {
			continue
		}
		row = shadeCells(th.measure, row, c0, c1, th.skillAccent)
	}
	return row
}

// renderPresentedBlock renders a presented document (ADR 0019, rung 0) — the one block that is
// deliberately NOT shaped like a tool card, because a deliverable is the point of the work and
// not plumbing. It leads with the ▤ marker and the document's title where the model gave one,
// then the workspace-relative path, then the served URL if there is one, then a dim status line:
//
//	▤ Architecture review
//	  docs/review.html
//	  http://192.168.64.2:51234/d/…/review.html
//	  cmd+click to open
//
// The path and the URL are emitted RAW — no style, no wrap, no clip, one token per line — and
// that is the whole mechanism: the terminal is what turns them into something clickable, so a
// hanging indent inserted mid-token or an SGR run wrapped around them would break the only rung
// that always works. width is therefore ignored for those two lines: an overlong path soft-wraps
// in the viewport rather than being hard-wrapped here.
// The marker keeps the title's styling even when there is no title, so the block opens the same
// way either way.
func renderPresentedBlock(th theme, v presentedView, width int) []string {
	// The two raw lines are the one surface in this block that no style and no wrap passes, so a TAB
	// in a path survived every measure the transcript took of the line and was still a tab when the
	// viewport rendered the whole frame — which spent four cells on it that nothing had counted
	// (expandTabs). Settling them here keeps "emitted raw" a statement about styling and wrapping,
	// which is what makes the token clickable, rather than about a control byte the terminal never
	// gets to see anyway. The title and the status line go through hangingWrap, which expands its
	// own.
	v.Path, v.Location = expandTabs(v.Path), expandTabs(v.Location)

	marker := glyphPresented + " "

	var out []string
	if v.Title != "" {
		out = append(out, hangingWrap(th, th.presentTitle, marker, v.Title, width)...)
		out = append(out, bodyIndent+v.Path)
	} else {
		out = append(out, th.presentTitle.Render(marker)+v.Path)
	}
	if v.Location != "" {
		out = append(out, bodyIndent+v.Location)
	}
	return append(out, hangingWrap(th, th.noteText, bodyIndent, presentedStatus(v), width)...)
}

// startupWideMinGap is the smallest gap, in columns, the wide start-up layout keeps between the logo
// and the right-aligned info block. When the content cannot fit the logo, this gap, and the info
// block side by side, renderStartupBox falls back to the stacked layout instead. It is the switch's
// only tuning knob — raise it if the two-column layout engages while still looking cramped.
const startupWideMinGap = 4

// startupInfoRow is one label/value pair of the start-up box's info block (host / model / context /
// version). An empty value drops the row.
type startupInfoRow struct{ label, value string }

// renderStartupBox renders the one-time start-up card, choosing a layout by the width it is handed.
// It reuses the prompt box's rounded border glyphs through th.startupBorder while dropping the black
// fill, so the card reads as the same chrome without the input box's solid field. It is
// [renderPresentedBlock]'s sibling — the entry holds the facts, this composes the lines.
//
// When there is room, the WIDE layout paints the logo on the left and a right-aligned
// host / model / context / version block on the right (renderStartupWide). When the width does not
// allow it, the STACKED fallback paints the original card — logo, a blank line, then host / model /
// version below it, no context (renderStartupStacked).
//
// Either way the card spans the full content width: width is the same railed inner budget every
// other transcript entry is laid out to (transcriptWidth), so the box's right border lands on the
// exact column the rest of the transcript's content ends at. The border and its padding fold INTO
// that width, so the rendered lines are exactly width columns. Both layouts frame their lines with
// drawBox rather than with th.startupBorder.Width(width): the rows are DRAWN in the painter's own
// measure, so a card whose info block carries a VARIATION SELECTOR-16 grapheme stays as many painted
// rows as it composed instead of having lipgloss fold one of them in two (ADR 0030 §5).
func renderStartupBox(th theme, v startupView, width int) []string {
	// inner is the content-column budget inside the rounded border and its padding — the room the
	// two layouts actually lay out to. GetHorizontalFrameSize tracks the border + padding, so the
	// arithmetic follows the style rather than a hard-coded 4.
	inner := width - th.startupBorder.GetHorizontalFrameSize()

	// The facts are composed into the info rows PLAIN — only the label is styled (startupInfoLine) —
	// so a TAB in one of them was measured as nothing by every width this card takes (the label
	// column, the info block's width, the layout switch, the row fit and drawBox's own squaring) and
	// was still a tab when the viewport rendered the frame, four cells the card had not budgeted for:
	// the row overran its own border, which then painted right of every other row's (expandTabs).
	// Expanded here rather than at the row, because the two layouts build their rows separately and
	// both measure before they compose. A host or a model id comes from config or the CLI, where a
	// stray tab is a typo away.
	v.Host, v.Model = expandTabs(v.Host), expandTabs(v.Model)
	v.Context, v.Version = expandTabs(v.Context), expandTabs(v.Version)

	rows := make([]startupInfoRow, 0, 4)
	for _, r := range []startupInfoRow{
		{"host", v.Host}, {"model", v.Model}, {"context", v.Context}, {"version", v.Version},
	} {
		if r.value != "" { // an unknown fact (context 0) drops its row, mirroring the footer's nonEmpty
			rows = append(rows, r)
		}
	}

	logo := strings.Split(v.Logo, "\n")
	logoW := 0
	for _, ln := range logo {
		logoW = max(logoW, th.measure.Width(ln))
	}
	labelW := startupLabelWidth(th, rows)
	infoW := startupInfoWidth(th, rows, labelW)

	if inner >= logoW+startupWideMinGap+infoW {
		return renderStartupWide(th, logo, rows, labelW, infoW, width, inner)
	}
	return renderStartupStacked(th, v, width, inner)
}

// renderStartupWide paints the wide start-up card: the logo on the left, the info block right-
// aligned against the right content edge (left column inner-infoW), so the widest info row sits
// flush against the right padding and shorter rows trail off toward it. Logo line i pairs with info
// row i (top-aligned) and whichever side is shorter blank-fills — the four logo lines pair directly
// with the four info rows, so there is no blank spacer. drawBox pads every line out to the card's
// content budget, in the painter's own measure (renderStartupBox's contract). The caller guarantees
// inner ≥ logoW + startupWideMinGap + infoW, so the per-line pad count is at least
// startupWideMinGap.
func renderStartupWide(th theme, logo []string, rows []startupInfoRow, labelW, infoW, width, inner int) []string {
	left := inner - infoW // the info block's left column
	n := max(len(logo), len(rows))
	lines := make([]string, 0, n)
	for i := 0; i < n; i++ {
		logoLine := ""
		if i < len(logo) {
			logoLine = logo[i]
		}
		line := logoLine + strings.Repeat(" ", max(0, left-th.measure.Width(logoLine)))
		if i < len(rows) {
			line += startupInfoLine(th, rows[i], labelW)
		}
		lines = append(lines, line)
	}
	return drawBox(th.measure, th.startupBorder, lines, width)
}

// renderStartupStacked paints the narrow fallback: the logo, one blank line, then the host / model /
// version rows stacked below it (no context), dim labels aligned in a column and plain values. This
// is the card's original layout, kept for widths too narrow for the two-column wide layout.
//
// It fits its OWN info rows to inner, the card's content budget, rather than handing the box a row
// it has no room for. drawBox squares every row it is given, so an over-long one comes back cut at
// the border and says nothing about it: on a card of 29 columns or less a host of
// "192.168.64.1:1111" was painted as "192.168.64.1:111" — a port silently one digit short, which
// reads as a fact rather than as a cut. Fitting here ends such a row in the same "…" every other
// overflowing surface in this package carries (truncateToWidth), so what was eaten is visible.
// The value is not wrapped onto a further line instead: a host, a model name and a version are
// single unbreakable tokens, so a wrap would only move the same cut one line down. A row that
// already fits comes back untouched, which is every row at the widths this card is normally drawn
// at. The logo above them is left to the box: block art has no tail to elide, and the widths where
// it overruns are far below the ones where the info rows do.
func renderStartupStacked(th theme, v startupView, width, inner int) []string {
	content := strings.Split(v.Logo, "\n")
	content = append(content, "") // one blank line between the logo and the info rows

	rows := []startupInfoRow{{"host", v.Host}, {"model", v.Model}, {"version", v.Version}}
	labelW := startupLabelWidth(th, rows)
	for _, r := range rows {
		content = append(content, truncateToWidth(th, startupInfoLine(th, r, labelW), inner))
	}
	return drawBox(th.measure, th.startupBorder, content, width)
}

// startupInfoLine renders one info row — the dim label padded to the block's label column, two
// spaces, then the plain value — shared by both start-up layouts so their rows never drift.
func startupInfoLine(th theme, r startupInfoRow, labelW int) string {
	padded := r.label + strings.Repeat(" ", max(0, labelW-th.measure.Width(r.label)))
	return th.noteText.Render(padded) + "  " + r.value
}

// startupLabelWidth is the widest label among the info rows — the column every value aligns past.
func startupLabelWidth(th theme, rows []startupInfoRow) int {
	w := 0
	for _, r := range rows {
		w = max(w, th.measure.Width(r.label))
	}
	return w
}

// startupInfoWidth is the info block's rendered width: the widest label-padded row (labelW, two
// spaces, then the value). It is the block the wide layout right-aligns and the term the width
// switch measures against.
func startupInfoWidth(th theme, rows []startupInfoRow, labelW int) int {
	w := 0
	for _, r := range rows {
		w = max(w, labelW+2+th.measure.Width(r.value))
	}
	return w
}

// renderToolBlock renders one tool-call block — a single call or a whole grouped run — in the
// one uniform shape layout.md sketches: a ✦ header carrying the **label alone — never a target**
// (plus the ▶/▼ state indicator, below, where the header is one), then one ┝/┕
// branch per call whose first column is that call's target. The target never sits on the header,
// so the target COLUMN does not move the moment a second call joins the block — what changes around
// it is the block's shape, never the place a reader's eye is already resting (layout.md, "What stays
// standalone"). The caller frames the block for depth (renderView
// and renderEntryLines apply the rail) — width is already the railed inner column.
//
// The label is styled (bold gold) before the header is wrapped — the markdown.go posture:
// the authority's wrap is SGR-aware and its measure strips ANSI, so baking the style into the text
// leaves the soft-wrap and sticky-offset arithmetic untouched.
//
// Every branch row takes the one shape the canon spec draws (docs/layout/tool-layout.md): the
// target on the left, the outcome slot flush against the row's right edge, and a dotted leader
// flexing between the two (leaderRow). The outcomes therefore line up down the block's edge without
// a target column to pad to — the leader absorbs whatever the targets differ by, which is what lets
// a block of one and a block of ten put their outcomes in the same place. An empty slice renders
// nothing — every caller passes at least one view, and a renderer on the repaint path must not be
// the thing that panics if one ever does not.
//
// A block of MANY is a different shape from a block of one, and it hands off at the top
// (renderToolGroup): its header carries the member count and no state of its own, and each member
// is held to a single row with an indicator of its own at the block's right edge. The two shapes
// share this entry point — and the row shape itself — because what a caller has is a slice of views
// and which shape that is, is this function's answer.
//
// state is the SINGLE block's view state, and its expanded half changes exactly one thing: an
// expanded block paints its retained body whole while a collapsed one paints the compact shape,
// remainder marker and all (renderToolBranch). Its live half reaches one glyph and no shape at all:
// the header's leading star (state.star), ✦ once the block has settled and blinking against a bare
// cell while it still holds an open call.
//
// It also marks the block's CLICK SURFACE as it emits it, because the lines and the marks have to
// be one act: a second pass over the finished lines would be a second derivation of the same
// accounting, and the two would drift the first time the shape changed (ADR 0030's rule). That
// surface is the block WHOLE — its header, the clipped target rows beneath it, and, open, the full
// target and every body line — the shape the prompt block has always had (renderUserBlock): a
// reader who wants the rest of a Run's output clicks the output, not the one row of the block that
// happens to be its header. The one exception is the synthesized `+N more lines` marker, which
// keeps its OPEN-ONLY meaning (targetMarker): it is a line of the collapsed paint alone, so a click
// there can only mean "show me the rest".
//
// The surface exists when the collapsed paint HIDES something — either inside the views
// (blockHidesWhenCollapsed) or outside them (state.elides, the sub-agent run's span) — because a
// block with nothing to reveal has nothing to toggle, so a short bodiless call keeps a click's
// selection meaning down every row it paints. The mark is state-independent by design: an expanded
// block still marks its rows, which is what lets the same click collapse it again.
//
// The BRANCH ROW wears that same answer: the ▶/▼ state indicator lands at the row's right edge,
// after the outcome slot, under the very predicate that marks the lines — so the affordance and the
// click-target rule cannot come to disagree, a block that wears an indicator being clickable and a
// block that does not not, with one condition behind both. Unlike the mark, the glyph is
// state-DEPENDENT: it is what says which way the click will go (stateIndicator). It is styled apart
// from the row (th.toolIndicator) so it reads as chrome beside the text rather than as the last word
// of it. The one shape that keeps the glyph on its HEADER is the targetless one, which paints no
// branch row for it to sit at the edge of (docs/layout/tool-layout.md, "Single tool collapsed").
func renderToolBlock(th theme, views []toolView, width int, state blockState) blockPaint {
	if len(views) == 0 {
		return blockPaint{}
	}
	// The promote-guard runs before anything is asked of the views, both shapes through the one
	// call: what it changes is what the block hides, and every question below is asked of that.
	views = guardPromotions(th, views, toolRowCells(th, width))
	if len(views) > 1 {
		return renderToolGroup(th, views, width, state)
	}
	// toggle is the block's whole click surface in one value — targetHeader when there is something
	// behind the block and targetNone when there is not — settled once and spent on every row the
	// block emits, so the header, the branches and the body cannot come to disagree about whether
	// this block is clickable.
	toggle := targetNone
	label := th.toolLabel.Render(views[0].Label)
	if state.elides || blockHidesWhenCollapsed(th, views, width) {
		toggle = targetHeader
		if views[0].Target == "" {
			label += " " + th.toolIndicator.Render(stateIndicator(state.expanded))
		}
	}
	var out blockPaint
	out.add(hangingWrap(th, th.toolHeader, state.star()+" ", label, width), toggle)
	for i, tv := range views {
		out.join(renderToolBranch(th, tv, branchMarker(i == len(views)-1), width, state.expanded, toggle))
	}
	return out
}

// renderToolGroup paints a folded run of same-label calls — the sketch's "✦ Terminal (3)" block
// (docs/layout/tool-layout.md). It is a list, and everything about the shape follows from that: the
// header names the label and how many calls carry it, and a COLLAPSED member gets exactly ONE row
// so ten grouped calls read as ten lines rather than as ten blocks that happen to share a star.
//
// The header wears the count in the faint indicator tone rather than the label's bold gold
// (design call 6): "(3)" is the block's own arithmetic, not part of the tool's name, and a reader
// scanning the gold down the left edge should not read the number as one. It wears no state
// indicator and is NOT a click target, because a group has no single state to toggle — expansion
// belongs to the members, each of which owns its own.
//
// That ownership is the whole of what this function does with state beyond the star: each member is
// painted by ITS OWN entry's flag (state.memberExpanded, filled by renderView from the run's
// entries), and each member's rows are marked for its OWN entry (blockPaint.addFor, whose offset is
// the member index because views[n] is entries[head+n]). So one member opens inside a group of ten
// and the other nine hold still, in the paint and under the mouse alike.
//
// A member that hides nothing — a short target, no body — is marked targetNone and keeps a click's
// selection meaning, the same rule the single block's header answers to (blockHidesWhenCollapsed).
//
// state's own expanded flag reaches nothing here: it is the head entry's, and inside a group the
// head is just the first member.
func renderToolGroup(th theme, views []toolView, width int, state blockState) blockPaint {
	label := th.toolLabel.Render(views[0].Label) + " " +
		th.toolIndicator.Render(fmt.Sprintf(groupCountFormat, len(views)))
	var out blockPaint
	out.add(hangingWrap(th, th.toolHeader, state.star()+" ", label, width), targetNone)
	room := toolRowCells(th, width)
	for i, tv := range views {
		rows, hides := renderGroupMember(th, tv, branchMarker(i == len(views)-1), width, room,
			state.memberExpanded(i))
		kind := targetNone
		if hides {
			kind = targetHeader
		}
		out.addFor(i, rows, kind)
	}
	return out
}

// renderGroupMember paints one member of a grouped block, in whichever of its two states the
// member's own entry is in, and reports whether the COLLAPSED paint hides anything — which is both
// what makes the member wear an indicator and what makes its rows a click target (renderToolGroup).
//
// Collapsed, it is one screen row whatever the call is carrying — leaderRow's shape, the same one
// the ungrouped branch line takes: the branch marker, the call's target, the dotted leader, and the
// outcome flush against the room's right edge; a ▶ then right-aligns at the block's edge when the
// member hides anything.
//
// The order the room is spent in is the rule: the OUTCOME is never dropped. It is what happened —
// "12 lines", "+2 −3", "error: …" — and a member row that showed more of a long path at the cost of
// it would be showing the less useful half. So the slot's cells are reserved first, the leader
// flexes down to its floor, and only then does the target give way, ending in " …" (design call 4).
//
// The indicator field is reserved on every member, hidden one or not, so the ▶s line up down the
// block's right edge and a member does not gain three columns of target by having nothing to reveal.
// Whether the member WEARS one is the same question the single block asks — does the collapsed paint
// hide anything (blockHidesWhenCollapsed) — asked here of one call: a body, or a target the row's own
// width cut. A call still in flight has neither and paints a bare row.
//
// The hidden answer is taken from the COLLAPSED arithmetic in both states, which is why the clip is
// composed even when the member is open: an expanded member is expanded precisely because its
// collapsed paint hid something, and it has to stay a click target so the same click closes it —
// the state-independence the single block's mark has always had.
//
// room is the row less the indicator field, settled once for the whole block, so no member can lay
// itself out against a different one, and the field the ▶ sits in is held clear down an OPEN member
// too (renderExpandedMember) — a row that re-wrapped on being opened would move out from under the
// very click that opened it.
func renderGroupMember(th theme, tv toolView, marker string, width, room int, expanded bool) (lines []string, hides bool) {
	row := leaderRow(th, tv, marker, room, expanded)
	if hides = tv.Details.len() > 0; !hides {
		return []string{row}, false
	}
	if expanded {
		return renderExpandedMember(th, tv, marker, width, room), true
	}
	return []string{indicatorRow(th, row, width, glyphCollapsed)}, true
}

// renderExpandedMember paints an OPEN member of a grouped block — the sketch's "middle one
// expanded" (docs/layout/tool-layout.md): the branch marker and the call's whole branch text, ▼
// right-aligned on that first row, then every continuation row and every body line under a │ gutter
// standing in for the blank hanging indent, closed by a right-aligned see-less marker.
//
// The gutter is what makes the open member read as one thing rather than as a member followed by
// loose output, and it is painted in the DETAIL tone (design call 8): its shape is the sub-agent
// rail's and its meaning is not, so an open member inside a nested run must not wear the rail's
// gold — the two frames are read at a glance and confusing them would misattribute the body.
//
// The first row is the whole branch ROW, not the bare target: the outcome is what happened and
// opening a member must not take it away, and composing it through the same leaderRow the collapsed
// row and the ungrouped block both use keeps the outcome in the column it already occupied — under
// the very click that opened the member. That is also why it is still laid out against room: the
// indicator field stays clear down the member and the ▼ lands in the column the ▶ vacated. The row
// therefore keeps the one-row budget in both states; what opening a member buys is its BODY, which
// is the half a collapsed member was hiding.
//
// It grows no "+N more lines" marker: the marker counts what a collapsed paint left out, and this
// paint leaves nothing out. The see-less marker closes it instead, worded from the prompt block's
// own constant so the transcript has one vocabulary for "close this" (design call 7).
func renderExpandedMember(th theme, tv toolView, marker string, width, room int) []string {
	row := leaderRow(th, tv, marker, room, true)
	out := []string{indicatorRow(th, row, width, glyphExpanded)}
	for _, d := range tv.Details.all() {
		out = append(out, gutteredWrap(th, detailStyle(th, d.Kind, true), memberGutter, memberGutter, d.Text, room)...)
	}
	return append(out, seeLessRow(th, width))
}

// gutteredWrap is hangingWrap with a CONTINUATION prefix of its own: the first row leads with
// marker and every row after it with gutter, where hangingWrap would indent them by the marker's
// width in blanks. The prefixes are painted in the detail tone and the wrapped text in its own
// style, so a diff line keeps its red or green while the gutter beside it stays chrome.
//
// Both prefixes are measured, and the text is wrapped to the room left by the FIRST of them, so a
// gutter that is not the marker's width would still leave every row the same text column. Today
// they are the same width by construction (memberGutter is branchMarker's shape), and stating it
// this way is what keeps that a fact about the glyphs rather than an assumption in the arithmetic.
func gutteredWrap(th theme, style lipgloss.Style, marker, gutter, text string, width int) []string {
	rows := wrapText(th, text, max(1, width-th.measure.Width(marker)))
	out := make([]string, len(rows))
	for i, ln := range rows {
		prefix := gutter
		if i == 0 {
			prefix = marker
		}
		out[i] = th.toolDetail.Render(prefix) + style.Render(ln)
	}
	return out
}

// indicatorRow right-aligns one state glyph at the block's edge on an already-painted row — the
// member's ▶ or ▼, in the field groupIndicatorCells reserved for it. The pad is measured over the
// styled row (th.measure strips ANSI, width.go), so a row carrying colour lands in the same column
// as a plain one.
func indicatorRow(th theme, row string, width int, glyph string) string {
	pad := strings.Repeat(" ", max(0, width-th.measure.Width(glyph)-th.measure.Width(row)))
	return row + pad + th.toolIndicator.Render(glyph)
}

// seeLessRow is the row that closes an open member: the gutter, then the see-less marker flush
// against the block's right edge. It borrows the prompt block's WORDING and not its treatment —
// promptSeeLess is the one vocabulary (design call 7), while the style is the tool block's own
// marker tone, the same a "+N more lines" wears, because both are things apogee wrote onto a block
// rather than lines the tool produced.
func seeLessRow(th theme, width int) string {
	pad := strings.Repeat(" ",
		max(0, width-th.measure.Width(memberGutter)-th.measure.Width(promptSeeLess)))
	return th.toolDetail.Render(memberGutter) + pad + th.toolMarker.Render(promptSeeLess)
}

// memberGutter is the continuation prefix an open member's rows carry where a hanging wrap would
// put blanks: the branch marker's own two-column indent, then the gutter glyph and its space, so
// the gutter stands exactly under the ┝ it continues. Being branchMarker's shape is what keeps an
// open member's text in the column its collapsed row used.
const memberGutter = "  " + glyphMemberGutter + " "

// The dotted leader and the room it flexes in (docs/layout/tool-layout.md, "Width and overflow").
//
// glyphLeaderDot is one cell of the run that carries the eye from a row's target to the outcome
// slot at its right edge. leaderGap is the blank held on each side of that run, so the dots never
// touch the text they run between. leaderMinDots is the floor the run flexes down to before the
// LEFT target starts giving way — one dot, because a leader that vanished entirely would leave two
// unrelated words abutting and read as a single phrase.
const (
	glyphLeaderDot = "⋯" // U+22EF MIDLINE HORIZONTAL ELLIPSIS — deliberately one glyph per cell, so the run's length IS its cell count
	leaderGap      = 1
	leaderMinDots  = 1
)

// promoteMinTargetCells is the promote-guard's floor (design call 5, docs/layout/tool-layout.md,
// "Width and overflow"): how many cells of TARGET a row must still be able to show for a one-line
// tool output to be allowed into its outcome slot at all. Below it the line is demoted back into
// the body and the presenter's typed stat takes the slot (toolView.demoted).
//
// It is the one place the overflow order is not simply "the outcome wins". The order's premise is
// that what happened is the half worth keeping, and that premise fails for a promotion: a promoted
// line IS the body, moved up for want of anywhere better, so trading the path or the command that
// produced it for one more line of that body reads as a row about nothing. Fifteen cells is the
// span a shortened path's tail needs to still identify a file ("…/render.go" and a little), which
// is what the guard is protecting rather than any exact count.
const promoteMinTargetCells = 15

// guardPromotions settles the promote-guard for a whole block, before one row of it is painted: a
// view whose promoted one-line output would leave the row less than promoteMinTargetCells of target
// is replaced by its demoted reading, the line back in the body and the typed stat in the slot
// (toolView.demoted).
//
// It runs HERE, at the block's entrance, rather than inside leaderRow, because demotion changes what
// the block IS and not merely how one row prints: a demoted call has a body, so it now hides
// something when collapsed, and that is the very question the header's indicator, the click surface
// and the remainder marker are all answered from (blockHidesWhenCollapsed). A guard applied at the
// row would leave those three saying the call had nothing to reveal while the paint had just hidden
// a line.
//
// The answer depends on the WIDTH alone and never on the block's state, which is the leader row's
// standing promise read one level up: a row that promoted its line collapsed and demoted it open
// would move out from under the click that opened it.
//
// The slice is copied only where a view actually changed, so the ordinary block — nothing promoted,
// or everything promoted comfortably — reaches the painter as the very slice the entries handed
// over. The copy is shallow and that is enough: demoted rebuilds the body it changes rather than
// writing through the one the entry shares (toolView.demoted).
func guardPromotions(th theme, views []toolView, room int) []toolView {
	out, copied := views, false
	for i, tv := range views {
		if !guardRefuses(th, tv, room) {
			continue
		}
		if !copied {
			out, copied = append([]toolView(nil), views...), true
		}
		out[i] = tv.demoted()
	}
	return out
}

// guardRefuses asks the promote-guard's question of one call at one width: laid out as leaderRow
// will lay it out, does the promoted line leave promoteMinTargetCells of target standing beside the
// floor of one dot? The arithmetic is that function's own — the slot and its gap reserved first,
// then the gap and the dot the leader may not go below — so the guard and the row cannot come to
// different answers about the same width.
//
// Two calls are never refused. One that promoted nothing has only one reading of its outcome
// (toolView.promotable), and one with NO TARGET has no branch row at all: its lines are the branches
// themselves (renderToolBranch), so there is no target for a long line to crowd out and nothing for
// the guard to protect.
//
// The marker is measured as the closing ┕ because both branch markers are one glyph in the same
// four-cell frame (branchMarker) — a member's row keeps the same budget wherever in the block it
// sits, which is what lets one answer settle a block.
func guardRefuses(th theme, tv toolView, room int) bool {
	if !tv.promotable() || tv.Target == "" {
		return false
	}
	avail := room - th.measure.Width(branchMarker(true))
	tail := leaderGap + th.measure.Width(tv.Summary.Text)
	return avail-tail-leaderGap-leaderMinDots < promoteMinTargetCells
}

// leaderRow paints one call's branch row whole — the shape every single block and every group
// member takes (docs/layout/tool-layout.md): the branch marker, the call's target, a dotted leader,
// and the outcome slot flush against the right of room.
//
// It returns a painted row rather than text-and-a-style because the row is no longer one voice: the
// target speaks in the block's state tone, the leader in its own damped `tool-leader` role, and the
// outcome in whatever its own kind and verdict call for (summaryStyle). A caller that wrapped this
// afterwards would be wrapping a styled string; nothing needs to, because the row is one row by
// construction — that is what the overflow order below buys.
//
// The order room is spent in IS design call 4, and it is the whole of the arithmetic: the outcome
// slot is reserved first and always prints whole, the leader then flexes down to leaderMinDots, and
// only then is the target cut, ending in " …" (clipCells). A row too narrow for even that drops the
// target outright rather than the outcome — what happened is the half worth keeping, and a row with
// nothing left to give still says it.
//
// expanded reaches the TONES alone (detailTone, summaryStyle): a row is the same shape in both
// states, which is what lets the same click that opened a member close it without the row moving
// out from under the pointer.
func leaderRow(th theme, tv toolView, marker string, room int, expanded bool) string {
	avail := max(1, room-th.measure.Width(marker))
	slot := tv.Summary.Text
	// The last resort under design call 4, past the point it words: an outcome WIDER than the row
	// itself is cut too. A slot that printed whole there would not print whole anywhere — it would
	// run past the frame and the viewport would fold it into a second row, taking the block out of
	// its budget and the ▶ out from under the pointer. Everything above it still holds: the dots go
	// first, the target next, and only an outcome with no row left to stand in is touched at all.
	if slotCells := avail - leaderGap - leaderMinDots; th.measure.Width(slot) > slotCells {
		slot, _ = clipCells(th, slot, max(1, slotCells))
	}
	tail := 0
	if slot != "" {
		tail = leaderGap + th.measure.Width(slot)
	}
	target := ""
	if budget := avail - tail - leaderGap - leaderMinDots; budget >= 1 {
		target, _ = clipCells(th, expandTabs(tv.Target), budget)
		// A budget too narrow to hold even the clip tail comes back WIDER than it was given —
		// fitClipTail appends the tail whatever room is left — and those cells would push the row
		// past its width and fold it onto a second line. The target is dropped outright instead,
		// which is the order above taken one step further: a row this narrow has nothing left to
		// give up but the target, and what happened is the half worth keeping.
		if th.measure.Width(target) > budget {
			target = ""
		}
	}
	lead, leadCells := "", 0
	if target != "" {
		lead = detailTone(th, expanded).Render(target) + strings.Repeat(" ", leaderGap)
		leadCells = th.measure.Width(target) + leaderGap
	}
	dots := max(leaderMinDots, avail-leadCells-tail)
	row := detailTone(th, expanded).Render(marker) + lead +
		th.toolLeader.Render(strings.Repeat(glyphLeaderDot, dots))
	if slot != "" {
		row += strings.Repeat(" ", leaderGap) + summaryStyle(th, tv.Summary, expanded).Render(slot)
	}
	return row
}

// toolRowCells is the room a branch or member row lays itself out in: the block's width less the
// field the ▶/▼ is held in at its right edge (groupIndicatorCells). The field is reserved on every
// row, one wearing an indicator or not, so the outcome slots line up down the block's edge whatever
// each row has to reveal — and so a row does not move sideways the moment it gains something.
func toolRowCells(th theme, width int) int {
	return max(1, width-groupIndicatorCells(th))
}

// summaryStyle is the tone the outcome slot takes. A summary that says the call FAILED is red —
// design call 11 makes that red the only failure marking, so no glyph and no header changes with it
// — and every other summary keeps the branch line's own tone, the diff kinds included (detailStyle).
func summaryStyle(th theme, s branchSummary, expanded bool) lipgloss.Style {
	if failedSummary(s.Text) {
		return th.errorText
	}
	return detailStyle(th, s.Kind, expanded)
}

// failedSummary reads the outcome's own wording for a verdict of failure (errorSummaryPrefix and
// the two bare verdicts beside it). It asks the TEXT because that is where the fact is: a summary
// carries no verdict flag, and inventing one to be derived from the same words would be a second
// answer to a question already settled at the presenter's seam.
func failedSummary(text string) bool {
	return strings.HasPrefix(text, errorSummaryPrefix) ||
		text == deniedSummary || text == cancelledSummary
}

// clipCells fits text into ONE row of at most cells columns, ending it in clipTail when it had to
// cut, and reports the cut. It is clipWrap's arithmetic with no marker and no style — the same
// hangingPrefixes wrap and the same fitted tail — for the caller that has to keep composing after
// the clip instead of painting what comes back (groupMemberText).
//
// It goes through the wrap rather than truncating the string, so a target carrying a newline is cut
// at its first line like any other overlong one: wrapText keeps the text's own line breaks, and a
// second row is a cut however it arose.
func clipCells(th theme, text string, cells int) (string, bool) {
	rows := hangingPrefixes(th, "", text, cells)
	if len(rows) <= 1 {
		return rows[0], false
	}
	return fitClipTail(th, rows[0], cells), true
}

// The GROUPED block's own numbers (docs/layout/tool-layout.md).
//
// groupIndicatorGap is the field held clear between a row's outcome slot and its ▶, and it is
// reserved on every row so the indicators line up down the right edge whether or not each row wears
// one. A member's own budget is not among these numbers any more: one row is not the group's rule
// but the row shape's, since a leader row fills its width exactly by construction (leaderRow).
//
// groupCountFormat is the "(N)" the header carries beside the label for N ≥ 2 — a lone groupable
// call is painted as a single block and counts nothing.
const (
	groupIndicatorGap = 3
	groupCountFormat  = "(%d)"
)

// groupIndicatorCells is how many columns a member row gives up at its right edge to the indicator
// field: the gap plus the widest of the two glyphs it may show. Both are measured because the field
// must not change width when a member opens (▶ → ▼, item 5) — a member whose text re-wrapped on
// being expanded would move under the very click that expanded it.
func groupIndicatorCells(th theme) int {
	return groupIndicatorGap + max(th.measure.Width(glyphCollapsed), th.measure.Width(glyphExpanded))
}

// blockState is what a tool block's painter is told beyond the views themselves: which of the two
// paints to draw, and whether the collapsed one hides anything the views cannot account for.
//
// elides is the sub-agent run's case, and today its only one. A collapsed run's whole span — every
// inner block, every ⤷ label, every rail — is left unpainted by [transcript.renderView] before the
// head block ever reaches the painter, so nothing among the views records that there is something
// behind the header. The toggle-target rule has to know anyway: layout.md makes a run with a span
// clickable however short its own report is. Like the mark itself it is state-INDEPENDENT — an
// expanded run sets it too, which is what leaves the header clickable so the same click closes it.
//
// live and blink are the LIVE STAR's two halves, and they are deliberately separate: live is a fact
// about the block (something in it is still waiting for a result — anyOpenCall), blink is a fact
// about the frame (the spinner's phase this repaint was asked for — spinnerAnim.blink). Only their
// conjunction blanks the star's cell, so a settled block is immune to the phase and a live one needs
// no clock of its own.
//
// glyph replaces the header's leading star outright, for a block that borrows this shape without
// borrowing the star's meaning — today the scheduled Firing's ⟳ (renderEntryLines). Its ZERO VALUE
// is the star, so every existing caller keeps the glyph it always painted without saying so, and a
// block that names one has no live state to express: a Firing runs in a session of its own.
//
// members is the GROUPED shape's answer to the same question, one flag per view in the block's own
// order. A group has no state of its own — its header toggles nothing — and each member opens and
// closes alone (design call 6), so the flag a member is painted by is its own entry's rather than
// the head's. It is nil for every single block, where expanded above is the whole of the state, and
// read only through [blockState.memberExpanded] so a short slice is a collapsed member and never a
// panic on the repaint path.
type blockState struct {
	expanded bool
	elides   bool
	live     bool
	blink    bool
	glyph    string
	members  []bool
}

// memberExpanded is the n'th member's own view state — false wherever the caller named none, which
// is every single block and every hand-built test transcript. It is a method rather than an index
// because this runs on the repaint path, where the alternative to a bound is a panic mid-frame.
func (s blockState) memberExpanded(n int) bool {
	return n >= 0 && n < len(s.members) && s.members[n]
}

// star is the glyph the block's header leads with (layout.md, "The live star"): ✦ for a block that
// has everything it was waiting for, and ✦ alternating with a bare cell on the frame's blink phase
// while it does not. The blinked-out phase is a SPACE rather than an empty string — it holds the
// glyph's column, so the label beside it never shifts left and back twice a second. The zero value
// is a settled block at the settled phase, which is why every caller with nothing running — a stray
// result's block, a width probe — keeps the star the transcript has always led with without saying
// so.
//
// An overridden glyph answers before the live/blink conjunction is even asked, which is what makes a
// borrowed block's header STATIC by construction rather than by its caller remembering to leave two
// fields false.
func (s blockState) star() string {
	if s.glyph != "" {
		return s.glyph
	}
	if s.live && s.blink {
		return " "
	}
	return glyphAssistant
}

// stateIndicator is the glyph a TOGGLEABLE header trails its label with: ▼ for an expanded block,
// ▶ for a collapsed one (layout.md, "Collapsed and expanded blocks"). It answers for the state
// alone — whether a header wears one at all is the toggle-target rule's, asked once in
// renderToolBlock — so the two questions stay one condition and one glyph apart.
func stateIndicator(expanded bool) string {
	if expanded {
		return glyphExpanded
	}
	return glyphCollapsed
}

// anyOpenCall reports whether any of these entries is a tool call still waiting for its result —
// what makes the block they belong to LIVE, and so what makes its header star blink. It is
// transcript.hasOpenToolCall's rule read over one block's own entries instead of over the whole
// scrollback: the status line asks whether ANYTHING is still running, a header asks whether the
// work behind THAT star is.
func anyOpenCall(entries []entry) bool {
	for i := range entries {
		if entries[i].kind == entryToolCall && !entries[i].done {
			return true
		}
	}
	return false
}

// memberFlags is a grouped block's per-member view state, read off the run's own entries in view
// order (blockState.members). It is a copy rather than the entries themselves because a painter is
// handed what it needs to draw and nothing it could write through: the flag is owned by the shared
// entries backing array and moved only by transcript.setExpanded (ADR 0011).
func memberFlags(entries []entry) []bool {
	flags := make([]bool, len(entries))
	for i := range entries {
		flags[i] = entries[i].expanded
	}
	return flags
}

// blockHidesWhenCollapsed reports whether a block's collapsed paint leaves anything unshown — the
// whole of the toggle-target rule: a header is a click target exactly when there is something
// behind it. It asks the very functions that do the hiding — collapsedCall for the lines a cap
// drops, clipDetails for a branch line the row budget cuts — rather than re-deriving any of it, so
// the rule cannot answer differently from the paint.
//
// A CUT TARGET is deliberately not among the counts. It used to be, back when the branch line could
// spend a second row and opening the block gave the whole path back; a leader row is one row in both
// states by construction (leaderRow), so an over-long target ends in " …" whichever way the block is
// folded and expanding it would reveal nothing. The canon spec says the same thing from the other
// end: a row with nothing to expand carries no indicator at all (docs/layout/tool-layout.md), and an
// affordance that opens onto the row it was already showing is one a reader learns to distrust.
//
// The width is still an argument because the TARGETLESS shape's branch lines are cut by it
// (clipDetails): a block that hides nothing at 200 columns hides a tail at 60, and the indicator,
// the click target and the paint all have to say so together.
//
// BOTH shapes answer through it, the targetless one included: an unregistered tool's verbatim
// arguments, a registered call that arrived without its target, a stray result — all spend the same
// collapsed budget (layout.md, "Collapsed and expanded blocks"), so a call whose argument blob
// overflows its cap is a block that hides something, and so is a two-line one whose lines the width
// cuts. One call in a block with something to reveal makes the whole block a target — the header
// belongs to the block, not to a branch.
//
// It is the SINGLE block's rule: a group's header toggles nothing and asks nothing here, its members
// each wearing an indicator of their own under this same question asked of one call
// (renderGroupMember). The slice stays a slice because the question is about a block's views rather
// than about one of them, and item 5's per-member state is where that distinction is spent.
func blockHidesWhenCollapsed(th theme, views []toolView, width int) bool {
	for _, tv := range views {
		shown, _, truncated := collapsedCall(tv)
		if truncated {
			return true
		}
		if tv.Target == "" {
			if _, clipped := clipDetails(th, shown, width); clipped {
				return true
			}
		}
	}
	return false
}

// renderToolBranch renders one call of a tool block as its branch line (plus whatever hangs
// beneath it). Two shapes, and they are the whole grammar:
//
//   - a call WITH a target — the branch is the target, and when the call has a Summary, the
//     target padded to the block's column, one space, then that summary ("┕ main.go 1 - 154",
//     "┕ main.go +2 -2"). A call still in flight has no summary yet and shows the bare target;
//     the block repaints whole once the result folds in. Its Details, if any, are the block's
//     body and lay out beneath the branch at the branch marker's own width — not as ┝/┕ branches
//     of their own, because only calls are (a Run's output, a diff body under its diffstat) —
//     painted whole when the block is expanded and not at all when it is collapsed.
//   - a call with NO target — the only shape with no target line: the header stands alone and
//     the detail lines are themselves the ┝/┕ branches, the summary last since it has no branch
//     line to ride (an unregistered tool's labelled arguments then its "error: …"
//     outcome, a stray result). Collapsed, that branch LIST is what the cap falls on — the
//     block has no body to cap instead — each surviving line clipped to a row of its own
//     (clipDetails), and the remainder marker hangs beneath it.
//
// The shape follows from which halves of the outcome are filled and never from how many Details
// there are: a body of one line and a body of ten lay out the same way.
//
// The block's state reaches BOTH shapes. An expanded call lays out every line the entry retained,
// soft-wrapping whatever is overlong, and grows no remainder marker. A COLLAPSED targeted call is
// the row budget's (layout.md, "Collapsed and expanded blocks"): its branch is the ONE leader row
// (leaderRow), no body line is painted at all, and the marker
// counts the body WHOLE — the sketch's "+5 more lines" over a five-line output. So a collapsed
// block stands at most three rows tall whatever tool filled it and however long its target is,
// which is the point: a scrollback of tool calls reads as a list rather than as a wall. A
// collapsed targetless call caps its branch list instead, since there the lines ARE the branches —
// collapsedBodyCap of them, one clipped row each — and lands on the same three rows.
//
// toggle is the block's own click surface, settled once by renderToolBlock and spent on every row
// a branch emits: the branch line, the body under it, the targetless shape's branch list. A click
// anywhere on a block that hides something flips it, which is the prompt block's rule read over the
// other collapsible shape in the transcript — a body a reader is looking at is the likeliest place
// for the pointer to be when they want it gone. The synthesized remainder marker is the exception
// and is laid out on its own so the mark lands on exactly the marker's physical lines (all of them,
// should it ever wrap) and on nothing else: it belongs to the collapsed paint, so it OPENS and
// never closes (targetMarker).
func renderToolBranch(th theme, tv toolView, marker string, width int, expanded bool, toggle targetKind) blockPaint {
	if tv.Target == "" {
		if expanded {
			var out blockPaint
			out.add(renderDetails(th, branchDetails(tv), width), toggle)
			return out
		}
		shown, remainder, truncated := collapsedCall(tv)
		var out blockPaint
		rows, _ := clipDetails(th, shown, width)
		out.add(rows, toggle)
		if truncated {
			// The marker rides the branch marker's own width, the indent a targeted block's body
			// already sits at, so the affordance sits under the lines it counts either way.
			out.add(hangingWrap(th, th.toolMarker, strings.Repeat(" ", th.measure.Width(branchMarker(true))),
				remainder.Text, width), targetMarker)
		}
		return out
	}
	indent := th.measure.Width(marker)
	var out blockPaint
	row := leaderRow(th, tv, marker, toolRowCells(th, width), expanded)
	if toggle != targetNone {
		row = indicatorRow(th, row, width, stateIndicator(expanded))
	}
	out.add([]string{row}, toggle)
	if expanded {
		out.add(renderSubDetails(th, tv.Details.all(), indent, width), toggle)
		return out
	}
	if _, remainder, truncated := collapsedDetails(tv.Details); truncated {
		// The marker is painted in its OWN style role rather than through the body's detailStyle:
		// it is a paint artefact, not a line the tool wrote, and a body line that happens to open
		// with "+" must not be able to look like one. It rides the body's indent all the same, so
		// the affordance sits under the lines it counts.
		out.add(hangingWrap(th, th.toolMarker, strings.Repeat(" ", indent), remainder.Text, width), targetMarker)
	}
	return out
}

// The COLLAPSED block's row budget — the house numbers behind "a collapsed block stands at most
// three rows tall": its header, its branch row, and the remainder marker beneath it, whatever tool
// filled it and however long its target is (layout.md, "Collapsed and expanded blocks";
// docs/layout/tool-layout.md).
//
// A targeted call's branch is not among them because it can only ever be ONE row: the leader shape
// fills the width exactly and cuts the target to make it (leaderRow). The marker counts the body
// WHOLE, since a collapsed block paints no body line at all. Nothing of the output is previewed:
// one preview line of a hundred said little and cost every block a row, while the marker's count
// says the same thing in the row the block was going to spend anyway.
//
// collapsedBodyCap is the TARGETLESS shape's cap — how many of its branch lines survive the
// collapse (collapsedCall), the block having no body to cap instead. It spends the same three rows
// the other way round: TWO branch lines and the marker beneath them, since there the branch lines
// ARE the content and there is no target line above them to read them against.
//
// collapsedBranchRows is what one of those surviving lines may spend — one row, and the clip takes
// the rest (clipDetails). It is what holds the targetless shape to the budget at all: two branch
// lines are two rows only while neither soft-wraps, and an MCP call's argument blob wraps at any
// width a terminal actually has.
//
// Both are paint-time caps on content the entry keeps in full, which is why they live beside the
// painter and not beside diffBody, the producer that used to apply the diff's own.
const (
	collapsedBodyCap    = 2
	collapsedBranchRows = 1
)

// The collapsed prompt's numbers and wording (layout.md, "Collapsed and expanded blocks"). A
// prompt whose body soft-wraps to MORE than promptCollapsedRows rows paints that many rows and
// counts the rest, with promptSeeMoreFormat right-aligned on the last of them a promptMarkerMargin
// off the edge — the row's own text truncated first so promptMarkerGap columns stay clear between
// the two. Expanded, the body paints
// whole and promptSeeLess trails it on a row of its own, because a full body leaves no row the
// marker could ride without cutting content.
//
// They are constants and deliberately not configuration (no `ui:` key): the shape is the
// transcript's grammar, and a reader who wants a different one changes it here.
const (
	promptCollapsedRows = 3
	promptMarkerGap     = 2
	promptMarkerMargin  = 1                 // block field kept clear to the marker's right, so it never reads as clipped
	promptSeeMoreFormat = "see more (+%s)…" // %s is the hidden-row count, pluralised (plural)
	promptSeeMoreNoun   = "line"            // what promptSeeMoreFormat counts
	promptSeeLess       = "see less…"
)

// promptSeeMore words the marker a collapsed prompt carries on its last row for the hidden rows
// behind it: "see more (+1 line)…", "see more (+7 lines)…". It is the prompt's sentence about the
// same number a tool block words as "+N more lines" — one count, two voices (splitAtCap).
func promptSeeMore(hidden int) string {
	return fmt.Sprintf(promptSeeMoreFormat, plural(hidden, promptSeeMoreNoun))
}

// splitAtCap splits a body's lines at a collapsed paint's cap: the lines the compact shape SHOWS,
// and how many it leaves unshown — 0 when the body already fits, which is exactly "this paint hides
// nothing". It is the shown/hidden arithmetic alone, held apart from any one block's caps and
// wording so the collapsed paints that need it — a tool call's detail body, a long prompt's wrapped
// rows — cannot come to disagree about where the seam falls or how much sits behind it.
//
// What counts a remainder out loud stays the caller's: a tool block's `+N more lines` and a
// prompt's see-more marker are different sentences about the same number.
//
// A negative cap is clamped rather than left to panic on the slice: this runs on the repaint path,
// where a panic is the whole session.
func splitAtCap[T any](lines []T, limit int) (shown []T, hidden int) {
	if limit < 0 {
		limit = 0
	}
	if len(lines) <= limit {
		return lines, 0
	}
	return lines[:limit], len(lines) - limit
}

// collapsedDetails is the collapsed paint of a retained body: NO line of it, and the synthesized
// "+N more lines" marker counting all of it (truncated says whether there is a body at all; the
// marker is meaningless when there is not). The rows a collapsed block has to spend go to its
// target (its one leader row), so a body is a thing a click reveals rather than a thing the
// scrollback previews — which is why the shown slice it still returns is always empty, kept only so
// the two collapsed shapes answer in one signature.
//
// Truncation is a render-time act on facts the entry keeps whole (layout.md), so the marker is
// composed on every repaint and never stored — which is what makes it identifiable as a paint
// artefact rather than a body line, and lets the painter mark it as its own click target instead of
// sniffing the finished lines for the wording.
//
// The split is also half the toggle-target rule's oracle: truncated is "the collapsed paint hides
// body", which — with a clipped target, the other half — is what makes a header clickable
// (blockHidesWhenCollapsed, through collapsedCall).
//
// Nothing about the lines is examined at this seam, which is worth having: this runs on every
// repaint and twice per call, since the toggle-target rule asks it as well as the branch does, over
// a body the entry retains whole; a cap read off the lines here would walk a command's whole output
// once a frame.
//
// It is the BODY's collapsed paint; the targetless shape has no body and caps its branch list
// instead, through the same wording (collapsedCall).
func collapsedDetails(body toolBody) (shown []detailLine, remainder detailLine, truncated bool) {
	return collapseAtCap(body.all(), collapsedBodyRows)
}

// collapsedBodyRows is how many body lines a collapsed targeted block paints: none. It is a named
// zero rather than a bare literal because it is a DECISION — the collapsed block's three content
// rows go to the target and the marker — and a reader meeting the number in collapsedDetails is
// owed the reason (docs/layout/tool-layout.md).
const collapsedBodyRows = 0

// collapsedCall is the collapsed paint of ONE call, whichever of the two shapes it takes — the
// single authority both the painter and the toggle-target rule ask (renderToolBranch,
// blockHidesWhenCollapsed), so the shape question is answered in one place and the two cannot come
// to disagree about what a collapsed block hides.
//
// A call WITH a target hides its body whole, which is what would otherwise lay out beneath the
// branch line. A call with NO target caps its BRANCH list — body plus the summary closing it
// (branchDetails) — because there the lines are the branches themselves and a block with no target
// line has rows to spend on them. Which lines are cut is the only thing the shape decides; neither
// can grow taller than the block's own budget.
//
// It answers for the lines alone. A targeted call also hides when the row budget CLIPS its branch,
// which is a fact about the width rather than about the entry, and lives with the clip that takes
// it (collapsedBranch).
func collapsedCall(tv toolView) (shown []detailLine, remainder detailLine, truncated bool) {
	if tv.Target == "" {
		return collapseAtCap(branchDetails(tv), collapsedBodyCap)
	}
	return collapsedDetails(tv.Details)
}

// collapseAtCap cuts lines at a collapsed paint's cap and words what it leaves behind — the seam
// and the sentence, held in one place so every collapsed shape counts its remainder the same way.
// Lines already inside the cap come back whole and grow no marker. Where the cut falls is
// splitAtCap's, shared with the other collapsed paints.
//
// The marker says "+N more lines" and nothing else. It used to open with an ellipsis, back when it
// followed a body line the paint had cut in the middle; now a clipped row says its own continuation
// with the " …" the clip fits onto it (clipTail) and the marker counts only what never got a row at
// all, so the two marks stay one fact each (docs/layout/tool-layout.md).
func collapseAtCap(lines []detailLine, limit int) (shown []detailLine, remainder detailLine, truncated bool) {
	shown, hidden := splitAtCap(lines, limit)
	if hidden == 0 {
		return shown, detailLine{}, false
	}
	return shown, detailLine{Text: "+" + plural(hidden, "more line")}, true
}

// branchDetails is what a targetless call hangs off its header: the body, plus the summary as
// its last line. A targetless block has no branch line for a summary to ride, so the outcome
// simply closes the branch list — which is where an "error: …" on an unregistered tool has
// always sat, after the arguments that provoked it.
//
// It lays the summary's LINE out and drops the mark that came with it: whose words the line is
// decided how it was spelled at the presenter's seam (branchSummary), which is long settled by the
// time anything is painted.
func branchDetails(tv toolView) []detailLine {
	if tv.Summary.Text == "" {
		return tv.Details.all()
	}
	out := make([]detailLine, 0, tv.Details.len()+1)
	out = append(out, tv.Details.all()...)
	return append(out, tv.Summary.detailLine)
}

// branchMarker is the tree marker leading a branch line: ┕ closes a block, ┝ continues it. Its
// display width is also the sub-content indent, so detail text laid out beneath a branch lines
// up with the target on it.
func branchMarker(last bool) string {
	if last {
		return "  " + glyphBranchLast + " "
	}
	return "  " + glyphBranch + " "
}

// renderSubDetails lays a call's detail lines out beneath its branch line, indented to the
// branch marker's width and styled by kind, so they read as that branch's content rather than
// as siblings of it.
//
// It is the EXPANDED paint's alone — a collapsed block paints no body line at all
// (collapsedBodyRows) — so its lines take the open tone outright rather than through a parameter
// that could only ever be true (detailStyle).
func renderSubDetails(th theme, details []detailLine, indent, width int) []string {
	pad := strings.Repeat(" ", indent)
	out := make([]string, 0, len(details))
	for _, d := range details {
		out = append(out, hangingWrap(th, detailStyle(th, d.Kind, true), pad, d.Text, width)...)
	}
	return out
}

// toolCallRun returns the consecutive tool-call entries starting at entries[i] that fold into one
// grouped block, as their presentation views: same sub-agent depth, same friendly Label, every
// member groupable. Any other entry between two calls — narration, a note, an approval, an error —
// ends the run, since the scan only ever walks forward over adjacent entries. Two different tools
// sharing a label (a single and a multi find-and-replace are both "Replace") do group: the reader
// groups by what the header says, not by tool id. It returns nil when entries[i] is not a groupable
// tool call, and a one-view run when nothing follows it — the caller renders both as single blocks.
func toolCallRun(entries []entry, i int) []toolView {
	head := entries[i]
	if head.kind != entryToolCall || !groupable(head.tool) {
		return nil
	}
	views := []toolView{head.tool}
	for j := i + 1; j < len(entries); j++ {
		e := entries[j]
		if e.kind != entryToolCall || e.depth != head.depth || e.tool.Label != head.tool.Label || !groupable(e.tool) {
			break
		}
		views = append(views, e.tool)
	}
	return views
}

// groupable reports whether a tool call can be shown as one member row of a grouped block: it
// needs a Target to sit in the aligned column, and it must not be marked solo by the presenter that
// built it (toolView.solo). Nothing else — a body no longer disqualifies a call, because a member
// row no longer has to hold everything the call has to say: it shows one clipped line and keeps its
// body behind its own indicator (renderGroupMember, design call 3). So a batch of Runs with output
// and a batch of edits with their diffs group exactly as a batch of reads always has, which is what
// a scrollback of ten same-label calls needed most.
//
// A call with NO target still keeps its own block: there the detail lines ARE the branches
// (renderToolBranch), so there is nothing for the aligned column to align. And solo is the
// never-group mechanism the body exclusion used to be by accident — an answered question's record
// is a card in its own right (askUserAnswerRecord) and a sub-agent call heads a whole run
// (subAgentToolName), and both now say so instead of relying on the shape rule to keep them out.
//
// It never counts detail lines: the block's shape does not depend on how many there are, and
// neither may this.
func groupable(tv toolView) bool {
	return tv.Target != "" && !tv.solo
}

// renderOrphanResult renders a tool result that matched no pending call (a defensive
// fallback — normally a result folds into its call by CallID). It reads as a result block:
// a ✦ result header — the bare word styled like any tool label — with the raw content hanging
// off branches. It is targetless by construction, so it renders through the block renderer's
// no-target shape. The caller frames it for depth — width is already the railed inner column.
//
// It collapses like every other block: the targetless shape caps its branches at the house budget
// (collapsedCall), so a long stray result has a second state to show and its header is a toggle
// target as soon as it overflows — which is why the paint travels back whole, click surface
// included, instead of being flattened to its lines. Its live half stays false by construction: a
// result with no call to fold into is waiting for nothing.
func renderOrphanResult(th theme, text string, width int, expanded bool) blockPaint {
	details := make([]detailLine, 0)
	for _, ln := range splitLines(text) {
		details = append(details, detailLine{Text: ln})
	}
	return renderToolBlock(th, []toolView{{Label: "result", Details: newToolBody(details)}}, width,
		blockState{expanded: expanded})
}

// renderDetails renders tool-detail lines as ┝/┕ tree branches (the last line gets ┕),
// styled by their kind (the open detail tone, or red/green for the diff kinds). This is the
// targetless shape only: where a call has a target, the target owns the branch and its details lay
// out beneath it (renderToolBranch).
//
// Like renderSubDetails it is the EXPANDED paint — the collapsed twin of this shape is clipDetails,
// under the row budget and in the dim tone — so its lines take the open tone outright.
func renderDetails(th theme, details []detailLine, width int) []string {
	var out []string
	for i, d := range details {
		out = append(out, hangingWrap(th, detailStyle(th, d.Kind, true), branchMarker(i == len(details)-1), d.Text, width)...)
	}
	return out
}

// clipDetails is renderDetails under the collapsed block's row budget: every branch line gets
// collapsedBranchRows rows and the clip takes the rest (clipWrap, which ends a cut row in " …"), so
// a collapsed targetless block spends as many rows on its branch list as the cap left it lines. It
// is what keeps that shape inside the four-row budget the targeted one is held to — unclipped, one
// argument blob's first line soft-wrapped the block as tall as the terminal was narrow.
//
// It REPORTS the cut for the same reason collapsedBranch does: whether a collapsed targetless block
// hides anything is width-dependent once its lines can be cut, and the indicator, the click target
// and the paint all have to agree about it (blockHidesWhenCollapsed). Each shape asks the clipper
// that paints it, so neither can drift from what is on screen.
func clipDetails(th theme, details []detailLine, width int) (lines []string, clipped bool) {
	out := make([]string, 0, len(details))
	for i, d := range details {
		rows, cut := clipWrap(th, detailStyle(th, d.Kind, false), branchMarker(i == len(details)-1), d.Text,
			width, collapsedBranchRows)
		out = append(out, rows...)
		clipped = clipped || cut
	}
	return out, clipped
}

// detailStyle maps a detail kind and its block's STATE to a style: plain detail takes the tone of
// the state (detailTone); the diff kinds are red/green in both, because their colour says which way
// a line went and an emphasis step layered onto that would be a second thing the same colour means
// (view_diff's body is their producer — diffBody).
func detailStyle(th theme, kind detailKind, expanded bool) lipgloss.Style {
	switch kind {
	case detailDiffAdded:
		return th.diffAdded
	case detailDiffRemoved:
		return th.diffRemoved
	default:
		return detailTone(th, expanded)
	}
}

// detailTone is the plain-detail gray a tool block's text takes in each of its two states — the
// collapsed dim, or the step brighter an open block reads in (design call 9; the scheme's `muted`
// and `muted-bright` roles). It is the ONE place the state reaches the colour, so the target, the summary and
// the body of one block cannot come to disagree about how loudly they are speaking.
//
// It answers for a block's TEXT alone. The chrome — the ▶/▼ indicator, the `+N more lines` marker,
// an open member's │ gutter — keeps its own role in both states: those are apogee's marks on the
// block rather than what the block has to say, and brightening them with the content would make the
// affordances shout exactly where the content was meant to.
func detailTone(th theme, expanded bool) lipgloss.Style {
	if expanded {
		return th.toolDetailBright
	}
	return th.toolDetail
}

// ----------------------------------------------------------------------------
// Wrapping primitives
// ----------------------------------------------------------------------------

// hangingWrap word-wraps text under a leading marker, then styles each physical line: the
// marker leads the first line and a same-width blank indent leads every continuation line, so
// a wrapped block stays aligned under its marker (the ✦/┝ hanging indent of layout.md). The
// style colours the whole line; widths are ANSI-agnostic, so styling never perturbs the
// soft-wrap arithmetic.
func hangingWrap(th theme, style lipgloss.Style, marker, text string, width int) []string {
	prefixed := hangingPrefixes(th, marker, text, width)
	out := make([]string, len(prefixed))
	for i, ln := range prefixed {
		out[i] = style.Render(ln)
	}
	return out
}

// hangingPrefixes word-wraps text to the width left of the marker and prepends the marker to
// the first line and a matching blank indent to the rest, returning the unstyled lines. It is
// shared by the styled hanging wrap and the user block (which then pads each line to a
// full-width background).
func hangingPrefixes(th theme, marker, text string, width int) []string {
	mw := th.measure.Width(marker)
	indent := strings.Repeat(" ", mw)
	lines := wrapText(th, text, max(1, width-mw))
	out := make([]string, len(lines))
	for i, ln := range lines {
		if i == 0 {
			out[i] = marker + ln
		} else {
			out[i] = indent + ln
		}
	}
	return out
}

// clipTail is what a row cut short ends in: one space and one ellipsis, the sketch's own spelling
// (docs/layout/tool-layout.md). It is a CONTINUATION mark, not a marker — it says "this line goes
// on", where the "+N more lines" marker says how much never got a line at all.
const clipTail = " …"

// clipWrap is hangingWrap under a row budget. It wraps and styles exactly as hangingWrap does —
// the same hangingPrefixes path, so the same wrapText, the same expandTabs and the same hanging
// continuation indent — and then keeps at most maxRows physical rows, ending the last kept row in
// clipTail when it dropped any. Handed text that fits, it returns hangingWrap's own lines and
// clipped false, so a caller can reach for it unconditionally.
//
// It REPORTS the clip rather than leaving the caller to infer one. Whether a collapsed block hides
// anything is width-dependent once a target can be cut, and the indicator, the click target and the
// paint all have to agree about it; asking this once and passing the answer along is what keeps them
// from each re-deriving it — and from drifting apart when only one of them is changed.
//
// The tail is FITTED, not appended: the kept row is re-cut so the row and its tail together measure
// within width in the width authority's measure, which is the measure the frame is painted in
// (ADR 0030). Appending to a row the wrap had already filled to the column would overrun the width
// by the tail, and the viewport would fold the row into the very second row the budget was spending.
func clipWrap(th theme, style lipgloss.Style, marker, text string, width, maxRows int) (lines []string, clipped bool) {
	if maxRows < 1 {
		return nil, true // no row to spend: everything is hidden, and nothing is left to say so
	}
	prefixed := hangingPrefixes(th, marker, text, width)
	if clipped = len(prefixed) > maxRows; clipped {
		prefixed = prefixed[:maxRows]
		prefixed[maxRows-1] = fitClipTail(th, prefixed[maxRows-1], width)
	}
	out := make([]string, len(prefixed))
	for i, ln := range prefixed {
		out[i] = style.Render(ln)
	}
	return out, clipped
}

// fitClipTail re-cuts one wrapped row so the row plus clipTail measures within width. The row is
// still unstyled here — the cut lands on the text the wrap produced, before any style has been past
// it, which is the same order every other measurement in this package takes.
//
// It trims the trailing spaces the cut leaves behind: a break can hand back the space it fell on,
// and "grep  …" reads as a slip where "grep …" reads as a sentence continuing. A width too narrow
// to seat even the tail leaves the tail alone rather than half of it — a lone "…" one column short
// of the edge is still the honest mark, and no row can be narrower than what it must say.
func fitClipTail(th theme, row string, width int) string {
	room := max(0, width-th.measure.Width(clipTail))
	return strings.TrimRight(th.measure.Truncate(row, room, ""), " ") + clipTail
}

// wrapText word-wraps text to limit columns, hard-breaking any word longer than the limit
// and preserving the text's own newlines. An empty string yields a single empty line so a
// just-opened assistant buffer still renders its marker.
//
// It breaks with the width authority (th.measure.Wrap), so the break is CHOSEN in the same measure
// the cap below is enforced in and the painter draws in — ADR 0030's rule for this package. It used
// to break with the package-level ansi.Wrap, which is hard-wired to ansi.GraphemeWidth whatever the
// painter is doing: on the painter's default WcWidth that measured a VARIATION SELECTOR-16 cluster
// two cells against the one the terminal paints, so every wrapped surface — transcript prose,
// pop-up bodies, table cells — took its break a cell earlier than it needed on such a line. On
// content the two measures agree about (everything without VS16) the two wraps are identical, which
// is why this is a rename rather than a re-layout. A caller that then pads with a lipgloss Width
// style hands the gain straight back — lipgloss folds in GraphemeWidth — which is why the user
// block below squares its own rows and why the pop-up pane still does not (TODO.md).
//
// No line it returns is wider than limit in the width authority's measure — layout.md's absolute
// cap, enforced here rather than assumed. The upstream wrap does not hold it on its own, in either
// measure: the breakpoint branch lacks the full-line checks its default branch has, on the wcwidth
// path (x/ansi@v0.11.7/wrap.go:406-419) and on the grapheme path (:352-361) alike, so a run of
// breakpoints keeps growing a word onto an already-full line — a wrap of "| --- | --- | --- |" at
// limit 3 comes back with a five-cell first line, and of "----" with a four-cell one. Every line
// that comes back over the limit is therefore hard-broken down to it, which is also what makes the
// docstring's "hard-breaking any word longer than the limit" true rather than aspirational. The one
// thing no break can divide is a single grapheme wider than the limit — a CJK glyph at limit 1 —
// and that keeps a line to itself.
func wrapText(th theme, text string, limit int) []string {
	if limit < 1 {
		limit = 1
	}
	// Tabs are settled BEFORE the break is chosen: a tab the wrap counts as nothing is four cells
	// once a style has been past the line, and the cap above would then hold in the measure and
	// break in the paint (expandTabs).
	text = expandTabs(text)
	wrapped := strings.Split(th.measure.Wrap(text, limit, ""), "\n")
	out := make([]string, 0, len(wrapped))
	for _, ln := range wrapped {
		if th.measure.Width(ln) <= limit {
			out = append(out, ln)
			continue
		}
		// preserveSpace keeps this pass purely additive — it inserts breaks and drops nothing, so
		// a segment's own leading indentation survives the cap. The break the hard wrap opens
		// ahead of an over-wide leading grapheme would otherwise surface as a blank row.
		segs := strings.Split(th.measure.Hardwrap(ln, limit, true), "\n")
		if len(segs) > 1 && segs[0] == "" {
			segs = segs[1:]
		}
		out = append(out, segs...)
	}
	return out
}

// tabCells is how many spaces one TAB becomes when this package expands it: lipgloss's own
// tabWidthDefault (lipgloss/v2@v2.0.5/style.go:14, maybeConvertTabs), so a line handed to a style
// with its tabs already expanded paints exactly what that style would have made of it anyway.
const tabCells = 4

// expandTabs replaces every TAB in s with the spaces a lipgloss style would otherwise put there.
//
// A TAB is ZERO cells to the width authority, and zero to the painter too — ultraviolet drops the
// control byte rather than advancing to a tab stop — so on that pair alone the two agree and there
// would be nothing to settle. What breaks the agreement is what sits BETWEEN them: every
// lipgloss.Style.Render rewrites "\t" into tabCells spaces on its way past (maybeConvertTabs), after
// the authority measured the line and before the painter ever sees it. A user block therefore
// composed four cells more than it had measured, per tab in the text: the row overran the width the
// block was given, the viewport folded that one row into two painted ones, and the skill accent —
// shaded at cells counted in the authority's measure — landed four columns left of the token it
// names.
//
// Expanding before anything measures is what puts the three back in step: the authority counts the
// spaces, the style finds no tab left to rewrite, and the painter paints the very cells that were
// counted. This is not the content normalisation ADR 0030 rules out — that ruling is about VS16,
// where folding the content would change what the user sees and would overrule the terminal's own
// measure. A tab has no display width for anyone to have an opinion about, and the spaces are what
// the block was already painting; only the counting of them was wrong.
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	return strings.ReplaceAll(s, "\t", strings.Repeat(" ", tabCells))
}

// expandTabsInSpans re-bases spans — byte offsets into text — onto expandTabs(text): every TAB
// before an offset grows the text by tabCells-1 bytes, so the offset moves by that much per tab
// preceding it. Call it while text still holds its tabs, on the way to handing the expanded text to
// the wrap and to the accent map, so both address the same string.
//
// Offsets are clamped to text before they are counted against, so a span that never went through
// the transcript boundary's own check (spansWithin) cannot slice out of range here either.
func expandTabsInSpans(text string, spans []skillSpan) []skillSpan {
	if len(spans) == 0 || !strings.Contains(text, "\t") {
		return spans
	}
	shift := func(off int) int {
		off = clampInt(off, 0, len(text))
		return off + strings.Count(text[:off], "\t")*(tabCells-1)
	}
	out := make([]skillSpan, 0, len(spans))
	for _, sp := range spans {
		out = append(out, skillSpan{start: shift(sp.start), end: shift(sp.end)})
	}
	return out
}

// railWidth is the column cost of one sub-agent rail gutter ("│ " — the rail glyph plus one
// space), the amount each nesting level steals from the usable text width (P3.14).
const railWidth = 2

// railedWidth is the usable text width inside a Depth-level block: the full width less one
// rail gutter per level. Depth 0 is the common case and returns width unchanged; deeper
// levels are floored at one column so wrapping never divides by zero.
func railedWidth(width, depth int) int {
	if depth <= 0 {
		return width
	}
	return max(1, width-depth*railWidth)
}

// railSpacer is the one separating line between two adjacent blocks, framed for the run the two
// of them share: depth is the JOIN of their depths (the shallower one), so the rail is drawn only
// as deep as both sides reach. Depth 0 — the flat transcript, and either side of a sub-agent run's
// boundary — is the bare "" the layout has always used, so a top-level transcript renders exactly
// as before; deeper joins draw the gutter alone, which is what makes a run's frame continuous
// through its separators instead of breaking at every block.
//
// The gutter's trailing space is trimmed BEFORE it is styled, so a spacer's visible text is "│"
// at depth 1 and "│ │" at depth 2 — never a styled trailing blank, which would leave an invisible
// SGR run hanging off the right of an otherwise empty row.
func railSpacer(th theme, depth int) string {
	if depth <= 0 {
		return ""
	}
	return th.subRail.Render(strings.TrimRight(strings.Repeat(glyphSubRail+" ", depth), " "))
}

// railLines frames a Depth-level block: it prepends one styled "│ " rail gutter per nesting
// level to each physical line, so a sub-agent's nested block reads as a vertical-ruled
// sub-section (P3.14). Depth 0 is the common case and returns the lines untouched, so the
// flat top-level transcript renders exactly as before. The rail is styled in the subRail role's
// tool-header gold and sits left of any per-line background (e.g. the user block's), matching
// the marker hanging indent.
func railLines(th theme, lines []string, depth int) []string {
	if depth <= 0 {
		return lines
	}
	gutter := th.subRail.Render(strings.Repeat(glyphSubRail+" ", depth))
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = gutter + ln
	}
	return out
}

// ----------------------------------------------------------------------------
// Chrome layout helpers
// ----------------------------------------------------------------------------

// inputContentRows reports how many visual rows the input value occupies at innerWidth, mirroring
// the textarea's own wrap so the box sizes to exactly what the widget draws. It is the sum over
// logical lines of wrapRowStarts' row count (inputaccent.go) — the widget's own decomposition, which
// wraps each logical line independently and adds the counts (its totalVisualLines,
// bubbles/v2@v2.1.0/textarea/textarea.go:1666-1674). Delegating means the box's HEIGHT and the rows
// the accent pass paints on are read off one ruler; they used to be two separate derivations of the
// widget's wrap, and this one was an approximation (ansi.Wordwrap + ansi.Hardwrap) that disagreed
// with the widget on roughly 41% of prompt-shaped drafts, mostly by under-counting — "hello world"
// at width 5 is four widget rows and the old count said three.
//
// The trailing row a width-filling line keeps for a caret past its last cell comes with the mirror.
// Under-counting it leaves the box one row too short at a width-fill boundary — the source of the
// prompt-box scroll artifact the layout re-seat then can no longer reach (fixed in a7afbf1; its
// regression is [TestPromptScrollClampedWhileGrowing]). An empty value is one row.
//
// The count is deliberately unclamped: [promptEditor.rows] holds it to [minInputRows, maxInputRows],
// and past that cap the widget scrolls internally rather than the box growing further.
//
// KNOWN DIVERGENCE: both mirrors are still wrong on tabs, which the widget expands. See TODO.md,
// "The TUI width authority — what it did not convert".
//
// WIDGET MIRROR — deliberately NOT the width authority. This is one of the package's mirrors of a
// third-party widget's internal math, and a mirror's oracle is the widget, never apogee's
// painter-facing measure (width.go): the textarea wraps with uniseg.StringWidth
// (bubbles/v2@v2.1.0/textarea/textarea.go:1805-1852), which is what wrapRowStarts measures with
// (runesWidth) and is grapheme-clustered, unlike ansi.WcWidth. Sizing the box in the painter's
// measure would size it to something the widget never draws. The same rule governs the caret
// mirrors in inputaccent.go / mouse.go.
func inputContentRows(value string, innerWidth int) int {
	if innerWidth < 1 {
		innerWidth = 1
	}
	total := 0
	for _, line := range strings.Split(value, "\n") {
		total += len(wrapRowStarts([]rune(line), innerWidth))
	}
	return total
}

// clampInt clamps n to [lo, hi].
func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

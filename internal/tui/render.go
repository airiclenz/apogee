package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	lipgloss "charm.land/lipgloss/v2"
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
// the assistant and tool headers lead with ✦; a tool header carries its label alone and the
// target leads a ┝/┕ tree branch beneath it, so a single call and a grouped run share one
// shape; one blank line separates every block. Sub-agent depth (Phase 3) indents a whole block by two
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
// block HEADER line, or a synthesized remainder MARKER line. A click on a header toggles its
// block; a click on a marker expands the block whose body the marker is counting for.
type targetKind int

const (
	targetNone targetKind = iota
	targetHeader
	targetMarker
)

// lineTarget is one rendered line's click surface: what the line is, and the index into
// transcript.entries of the block's HEAD entry — the entry whose expanded state a click there
// flips (transcript.toggleExpanded). The zero value is "no target", which is what every line
// outside a toggleable block's header and marker carries, so a lookup needs no second sentinel.
type lineTarget struct {
	kind  targetKind
	entry int
}

// blockPaint is one painted block: its physical lines and, parallel to them, what each line is to
// a click. A block painter says WHAT each of its lines is; [transcript.renderView] alone says
// WHOSE, stamping the head entry's index as it lays the block into the transcript — which is why
// the kinds here carry no entry index and no painter needs to know where in the entry list it sits.
//
// The two slices are grown only through [blockPaint.add] and [blockPaint.join], so they cannot
// drift out of lockstep: every line that is appended is marked in the same call that appends it.
type blockPaint struct {
	lines   []string
	targets []targetKind
}

// plainPaint is the paint of a block that carries no click surface at all — an assistant answer, a
// note, a start-up box, a ⤷ descent label, a stray result. Everything the renderer emits that can
// never be toggled goes through here, so "no target" is stated once rather than spelled out at each
// producer. The two kinds that CAN be toggled — a tool block, and a prompt tall enough to collapse —
// mark their own lines as they emit them.
func plainPaint(lines []string) blockPaint {
	return blockPaint{lines: lines, targets: make([]targetKind, len(lines))}
}

// add appends lines that all carry the same target kind. A WRAPPED header is the reason it takes a
// slice rather than a line: every physical line a header occupies is part of the same click
// surface (layout.md — the click lands on the header, not on its first row), and the same holds
// for a remainder marker narrow enough to wrap.
func (p *blockPaint) add(lines []string, kind targetKind) {
	p.lines = append(p.lines, lines...)
	for range lines {
		p.targets = append(p.targets, kind)
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
	// head is the index into t.entries of the entry whose block state a click on this block
	// toggles. It is stamped only onto the lines the block itself marked as a click surface, so a
	// block that marks none — every kind but a tool block — may pass whatever index it sits at.
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
			if i < len(block.targets) && block.targets[i] != targetNone {
				target = lineTarget{kind: block.targets[i], entry: head}
			}
			targets = append(targets, target)
		}
		prevBlockDepth = depth
	}

	// Drop memoised paints for entries the transcript no longer has (paintcache.go). It runs
	// before the loop so a render never reads a row about a block that is gone.
	t.paints.prune(len(t.entries))

	prevDepth := 0
	for i := 0; i < len(t.entries); i++ {
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
			// entries forward), so the views' own entries are what the rule reads.
			key := t.blockKey(shapeToolRun, i, len(run), th, width, blink,
				anyOpenCall(t.entries[i:i+len(run)]))
			block := t.paintBlock(i, key, func() blockPaint {
				return renderToolBlock(th, run, railedWidth(width, e.depth), blockState{
					expanded: e.expanded,
					live:     key.live,
					blink:    blink,
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
	if t.streaming {
		// The in-progress buffer is trimmed of its trailing blank lines for display only: the
		// buffer keeps them (a mid-stream "\n\n" may be a paragraph break about to be continued),
		// but the preview must not grow a wobbling gap above the footer. An empty buffer still
		// renders its lone marker line, so the human sees that streaming has begun.
		preview := renderEntryLines(th, entry{kind: entryAssistant, text: trimTrailingBlankLines(t.pending)}, width, blink)
		appendBlock(false, 0, len(t.entries), preview)
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
// Three kinds carry a click surface, for the one reason: they have two states to toggle between. A
// tool call and a scheduled Firing mark their header line and the remainder marker beneath it — one
// block painter, so one click surface; a prompt tall enough to collapse marks EVERY row it paints,
// because there the whole block is the toggle (layout.md, "Collapsed and expanded blocks"). Every other kind comes back as plainPaint: a note or an answer paints one way
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
		return plainPaint(railLines(th, renderOrphanResult(th, e.text, inner), e.depth))
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
// "… +N more lines" marker to say it too — and the header is a target however short the report is
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
		view.Summary = subAgentSummary(head, span)
		view.Details = toolBody{} // the zero body: no lines, and so nothing to lay out beneath
	}
	return renderToolBlock(th, []toolView{view}, railedWidth(width, head.depth), blockState{
		expanded: head.expanded,
		elides:   true,
		live:     !head.done || anyOpenCall(span),
		blink:    blink,
	}).railed(th, head.depth)
}

// subAgentSummary words a collapsed run's one line: how much work happened in there, then its gist
// (layout.md, "A sub-agent run collapses to its call block"). The count is TRANSITIVE by
// construction — the span holds every entry of every level below the head, so counting its tool
// calls counts the grandchildren too, and the same rule read at a deeper head gives that level's
// own total without a second rule.
//
// A run with nothing to say beyond the count — no call open yet, or a report that carried no line
// at all — keeps the count alone rather than trailing an empty separator.
//
// The line is marked QUOTED (branchSummary) for what its second half is: the child's own report,
// or the phrase for the call it has open. Nothing respells it in either case — this is composed at
// paint, long after the shortening seam ran on the way in — so the mark is a statement about the
// text rather than a switch, and it is the one that stays true if a seam ever reads it.
func subAgentSummary(head entry, span []entry) branchSummary {
	calls := 0
	for i := range span {
		if span[i].kind == entryToolCall {
			calls++
		}
	}
	text := plural(calls, "tool call")
	if gist := subAgentGist(head, span); gist != "" {
		text += " · " + gist
	}
	return quotedSummary(detailLine{Text: text})
}

// subAgentGist is the second half of a collapsed run's summary, in the two tempi layout.md gives it.
// While the run works — the head has no result yet — it is the live phrase for the call the span has
// open, the same composition the status line shows for that call (toolPhrase, activity.go), read off
// the span at paint rather than kept as a second copy of the activity state. The MOST RECENT open
// call is the honest one when several are open at once: it is the work the child turned to last.
//
// Once the report lands the head has a gist of its own and that is the line — the summary a short
// report was compressed to, or, where the report was long enough to become a body, its first line.
func subAgentGist(head entry, span []entry) string {
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
			return toolPhrase(e.tool)
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
// one uniform shape layout.md sketches: a ✦ header carrying the **label alone**, then one ┝/┕
// branch per call whose first column is that call's target. The target never sits on the header,
// so a block does not visually reshape the moment a second call joins it: a block of one is
// byte-identical in shape to a block of many. The caller frames the block for depth (renderView
// and renderEntryLines apply the rail) — width is already the railed inner column.
//
// The label is styled (bold orange) before the header is wrapped — the markdown.go posture:
// the authority's wrap is SGR-aware and its measure strips ANSI, so baking the style into the text
// leaves the soft-wrap and sticky-offset arithmetic untouched.
//
// Targets are padded to the block's widest so the detail column lines up; widths are display
// cells (th.measure, width.go), so a multi-byte path pads correctly. A block of one pads to itself,
// which is no padding at all. An empty slice renders nothing — every caller passes at least one
// view, and a renderer on the repaint path must not be the thing that panics if one ever does not.
//
// state is the block's view state, and its expanded half changes exactly one thing: an expanded
// block paints its retained body whole while a collapsed one paints the compact shape, remainder
// marker and all (renderToolBranch). A GROUPED run is the degenerate case — its members carry no
// body by definition (groupable), so both states paint identically and the head entry's state is
// passed only so the two callers share one call shape (layout.md, "Collapsed and expanded blocks").
// Its live half reaches one glyph and no shape at all: the header's leading star (state.star), ✦
// once the block has settled and blinking against a bare cell while it still holds an open call.
//
// It also marks the block's CLICK SURFACE as it emits it — every physical line of the header, and
// any remainder marker a branch synthesized — because the lines and the marks have to be one act:
// a second pass over the finished lines would be a second derivation of the same accounting, and
// the two would drift the first time the shape changed (ADR 0030's rule). The header is marked
// when the collapsed paint HIDES something — either inside the views (blockHidesWhenCollapsed) or
// outside them (state.elides, the sub-agent run's span) — because a block with nothing to reveal
// has nothing to toggle, so a body-less group's header keeps a click's selection meaning. The mark
// is state-independent by design: an expanded block still marks its header, which is what lets the
// same click collapse it again.
func renderToolBlock(th theme, views []toolView, width int, state blockState) blockPaint {
	if len(views) == 0 {
		return blockPaint{}
	}
	header := targetNone
	if state.elides || blockHidesWhenCollapsed(views) {
		header = targetHeader
	}
	var out blockPaint
	out.add(hangingWrap(th, th.toolHeader, state.star()+" ", th.toolLabel.Render(views[0].Label), width), header)
	// The column is measured over EXPANDED targets, for the reason expandTabs gives: a tab weighs
	// nothing here while the wrap downstream spends four cells on it, so a column set from raw
	// targets is a column the branch lines cannot land on (renderToolBranch pads to the same
	// expanded measure).
	column := 0
	for _, tv := range views {
		column = max(column, th.measure.Width(expandTabs(tv.Target)))
	}
	for i, tv := range views {
		out.join(renderToolBranch(th, tv, column, branchMarker(i == len(views)-1), width, state.expanded))
	}
	return out
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
type blockState struct {
	expanded bool
	elides   bool
	live     bool
	blink    bool
	glyph    string
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

// blockHidesWhenCollapsed reports whether a block's collapsed paint leaves anything unshown — the
// whole of the toggle-target rule: a header is a click target exactly when there is something
// behind it. It asks collapsedDetails, the function that does the hiding, rather than re-deriving
// the caps, so the rule cannot answer differently from the paint.
//
// A call with no target hides nothing however long its body is: its detail lines ARE the block's
// ┝/┕ branches (renderToolBranch's targetless shape), and an unregistered tool's verbatim
// arguments or a stray result's lines are never capped. One call in a block with something to
// reveal makes the whole block a target — the header belongs to the block, not to a branch.
func blockHidesWhenCollapsed(views []toolView) bool {
	for _, tv := range views {
		if tv.Target == "" {
			continue
		}
		if _, _, truncated := collapsedDetails(tv.Details); truncated {
			return true
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
//     truncated to what the collapsed shape shows when the block is collapsed, which is the one
//     place that truncation happens (collapsedDetails), and painted whole when it is expanded.
//   - a call with NO target — the only shape with no target line: the header stands alone and
//     the detail lines are themselves the ┝/┕ branches, the summary last since it has no branch
//     line to ride (an unregistered tool's pretty-printed arguments then its "error: …"
//     outcome, a stray result).
//
// The shape follows from which halves of the outcome are filled and never from how many Details
// there are: a body of one line and a body of ten lay out the same way. Anything overlong
// soft-wraps under its marker like any other detail line — nothing is clipped for alignment's
// sake.
// The block's state reaches only the body: an expanded block lays out every line the entry
// retained and grows no remainder marker, a collapsed one paints collapsedDetails. The targetless
// shape ignores the state entirely, because it hides nothing in either — its detail lines ARE the
// block's branches.
//
// The synthesized remainder marker is marked as a click target as it is laid out, and it is laid
// out on its own so the mark lands on exactly the marker's physical lines (all of them, should it
// ever wrap) and on nothing else. Neither the branch line nor a body line is a target: a click on
// what is already shown keeps its selection meaning.
func renderToolBranch(th theme, tv toolView, column int, marker string, width int, expanded bool) blockPaint {
	if tv.Target == "" {
		return plainPaint(renderDetails(th, branchDetails(tv), width))
	}
	// The target is expanded before it is measured AND before it is padded, so the pad is computed
	// over the very string the wrap goes on to hand the style (expandTabs). Measured raw it read as
	// nothing, wrapText then spent four cells per tab on it, and the summary opened that far right of
	// the column its siblings opened theirs in — the column being the only thing that lines a block's
	// summaries up, since nothing is drawn between them.
	target := expandTabs(tv.Target)
	text, style := target, th.toolDetail
	if tv.Summary.Text != "" {
		pad := strings.Repeat(" ", max(0, column-th.measure.Width(target)))
		text += pad + " " + tv.Summary.Text
		style = detailStyle(th, tv.Summary.Kind)
	}
	indent := th.measure.Width(marker)

	var out blockPaint
	out.add(hangingWrap(th, style, marker, text, width), targetNone)
	if expanded {
		out.add(renderSubDetails(th, tv.Details.all(), indent, width), targetNone)
		return out
	}
	shown, remainder, truncated := collapsedDetails(tv.Details)
	out.add(renderSubDetails(th, shown, indent, width), targetNone)
	if truncated {
		out.add(renderSubDetails(th, []detailLine{remainder}, indent, width), targetMarker)
	}
	return out
}

// diffDetailCap bounds how many diff lines a COLLAPSED block paints — enough to read a focused
// change, not enough for a rewrite to flood the transcript. It is a paint-time cap on a body
// the entry keeps in full (layout.md, "Collapsed and expanded blocks"), which is why it lives
// beside the painter and not beside diffBody, the producer that used to apply it.
const diffDetailCap = 20

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
// same number a tool block words as "… +N more lines" — one count, two voices (splitAtCap).
func promptSeeMore(hidden int) string {
	return fmt.Sprintf(promptSeeMoreFormat, plural(hidden, promptSeeMoreNoun))
}

// splitAtCap splits a body's lines at a collapsed paint's cap: the lines the compact shape SHOWS,
// and how many it leaves unshown — 0 when the body already fits, which is exactly "this paint hides
// nothing". It is the shown/hidden arithmetic alone, held apart from any one block's caps and
// wording so the collapsed paints that need it — a tool call's detail body, a long prompt's wrapped
// rows — cannot come to disagree about where the seam falls or how much sits behind it.
//
// What counts a remainder out loud stays the caller's: a tool block's `… +N more lines` and a
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

// collapsedDetails is the collapsed paint of a retained body, split at the seam a click cares
// about: the lines the compact shape SHOWS, and the synthesized "… +N more lines" marker counting
// what it hides (truncated says whether it hides anything at all; the marker is meaningless when
// it does not). Truncation is a render-time act on facts the entry keeps whole (layout.md), so the
// marker is composed here on every repaint and never stored — which is what makes it identifiable
// as a paint artefact rather than a body line, and lets the painter mark it as its own click
// target instead of sniffing the finished lines for the wording.
//
// The split is also the toggle-target rule's oracle: truncated is exactly "the collapsed paint
// hides something", which is what makes a header clickable (blockHidesWhenCollapsed).
//
// Two flavours, told apart by the kind the body settled when its lines were paired with it
// (toolBody, toolpresent.go): a diff body — one carrying at least one tagged line, which every
// body diffBody produces does — keeps diffDetailCap lines, so a focused change reads in place; any
// other multi-line body keeps its first line alone, the gist a Run's output is worth in the chat.
// A body already inside its cap paints whole and grows no marker. Which cap applies and how the
// remainder is worded are this function's; where the cut falls is splitAtCap's, shared with the
// other collapsed paints.
//
// The kind is READ, never re-derived here. This runs on every repaint and twice per call — the
// toggle-target rule asks it as well as the branch — over a body the entry now retains whole, so
// sniffing the lines at this seam would walk a command's whole output once a frame. It takes the
// BODY, which is why reading rather than deriving is safe: the lines and the kind that caps them
// are one value and cannot be handed in disagreeing.
//
// It is the BODY's rule, not every detail line's: the targetless shape has no body — its detail
// lines ARE the block's ┝/┕ branches (renderDetails), an unregistered tool's verbatim arguments
// among them — and hiding those would hide what the model asked for, which no block does.
func collapsedDetails(body toolBody) (shown []detailLine, remainder detailLine, truncated bool) {
	limit := 1
	if body.isDiff() {
		limit = diffDetailCap
	}
	shown, hidden := splitAtCap(body.all(), limit)
	if hidden == 0 {
		return shown, detailLine{}, false
	}
	return shown, detailLine{Text: "… +" + plural(hidden, "more line")}, true
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
func renderSubDetails(th theme, details []detailLine, indent, width int) []string {
	pad := strings.Repeat(" ", indent)
	out := make([]string, 0, len(details))
	for _, d := range details {
		out = append(out, hangingWrap(th, detailStyle(th, d.Kind), pad, d.Text, width)...)
	}
	return out
}

// toolCallRun returns the consecutive tool-call entries starting at entries[i] that fold into one
// grouped block, as their presentation views: same sub-agent depth, same friendly Label, every
// member groupable. Any other entry between two calls — narration, a note, an approval, an error —
// ends the run, since the scan only ever walks forward over adjacent entries. Two different tools
// sharing a label (a single and a multi find-and-replace are both "Edit File") do group: the reader
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

// groupable reports whether a tool call can be shown as one branch line of a grouped block: it
// needs a Target to sit in the aligned column, an empty body so nothing hangs beneath that line,
// and a plain-kind Summary to follow the target on it. That admits the common cases — a finished
// read, an "error: …" outcome, and a call still in flight whose summary has not landed yet (the
// zero detailLine is plain and empty) — while a call carrying a body (a Run and its output, a
// diff body under its "+2 -2" diffstat) or no target at all keeps its own block, where it has the
// room it needs. It never counts detail lines: the block's shape does not depend on how many
// there are, and neither may this.
func groupable(tv toolView) bool {
	return tv.Target != "" && tv.Details.len() == 0 && tv.Summary.Kind == detailPlain
}

// renderOrphanResult renders a tool result that matched no pending call (a defensive
// fallback — normally a result folds into its call by CallID). It reads as a result block:
// a ✦ result header — the bare word styled like any tool label — with the raw content hanging
// off branches. It is targetless by construction, so it renders through the block renderer's
// no-target shape. The caller frames it for depth — width is already the railed inner column.
//
// It paints collapsed and stays that way: the targetless shape hides nothing (its lines are the
// branches themselves), so a stray result has no second state to show and is no toggle target —
// which is why it takes the block's lines alone and leaves its (empty) click surface behind.
func renderOrphanResult(th theme, text string, width int) []string {
	details := make([]detailLine, 0)
	for _, ln := range splitLines(text) {
		details = append(details, detailLine{Text: ln})
	}
	return renderToolBlock(th, []toolView{{Label: "result", Details: newToolBody(details)}}, width, blockState{}).lines
}

// renderDetails renders tool-detail lines as ┝/┕ tree branches (the last line gets ┕),
// styled by their kind (plain dim, or red/green for the diff kinds). This is the targetless
// shape only: where a call has a target, the target owns the branch and its details lay out
// beneath it (renderToolBranch).
func renderDetails(th theme, details []detailLine, width int) []string {
	var out []string
	for i, d := range details {
		out = append(out, hangingWrap(th, detailStyle(th, d.Kind), branchMarker(i == len(details)-1), d.Text, width)...)
	}
	return out
}

// detailStyle maps a detail kind to its style: plain detail is dim; the diff kinds are
// red/green (view_diff's body is their producer — diffBody).
func detailStyle(th theme, kind detailKind) lipgloss.Style {
	switch kind {
	case detailDiffAdded:
		return th.diffAdded
	case detailDiffRemoved:
		return th.diffRemoved
	default:
		return th.toolDetail
	}
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
// tool-header orange and sits left of any per-line background (e.g. the user block's), matching
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

package tui

import (
	"fmt"

	"github.com/airiclenz/apogee/internal/format"
)

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
		view = collapsedSubAgentView(th.measure, head, span)
	}
	return renderToolBlock(th, []toolView{view}, railedWidth(width, head.depth), blockState{
		expanded: head.expanded,
		elides:   true,
		live:     !head.done || anyOpenCall(span),
		blink:    blink,
	}).railed(th, head.depth)
}

// subAgentMember is one row of a folded sub-agent group as the painter needs it: the delegation's
// own call entry, the run nested beneath it, and where that entry sits relative to the block's
// head. It is the paint-time reading of [subAgentBlock], which names the same member by index — the
// indexes are what a click resolves through and the entries are what is drawn, so the two shapes
// are kept apart rather than one being made to serve both (renderView builds these).
//
// It carries the head and its span rather than a finished view for [renderSubAgentRun]'s reason:
// what a COLLAPSED delegation's row says is derived from both (subAgentSummary), and deriving it
// inside the painter is what keeps the work behind the paint cache instead of on every frame.
type subAgentMember struct {
	head   entry   // the delegation's own call entry: its view, and its expanded state
	span   []entry // the run nested beneath it; empty for a delegation that produced none
	offset int     // the member's entry, as an offset from the block's head (blockPaint.addFor)

	// last marks the GROUP's final member, whose row closes the list with ┕. It is the group's
	// answer and not the block's: a group interrupted by an open delegation's span paints its
	// remaining rows in a second block, and the ┕ still belongs to the last row of the whole list.
	last bool
}

// subAgentGroupLabel is the header a folded group of delegations wears, ahead of its count
// (docs/layout/tool-layout.md, Rules: "✦ Sub-Agent (N)"). It is read off the members' own registry
// label rather than restated, so the group and a lone delegation cannot come to name the tool
// differently; this constant is only the fallback for a group whose views carry no label at all.
const subAgentGroupLabel = "Sub-Agent"

// renderSubAgentGroup paints a folded group of adjacent delegations — "✦ Sub-Agent (3)" over one
// row per agent, the agent's name on the left and its verdict in the outcome slot
// (docs/layout/tool-layout.md, Rules). It is the same list the same-label group is
// (renderToolGroup), and the member rows go through the very painter that block's do, so a
// delegation reads as a row of a list wherever it is folded.
//
// What is NOT here is the half that makes a delegation different: an open member's SPAN. Expanding
// one reveals the whole nested run — its ⤷ label, its rails, its own blocks, each with its own
// state — and those are entries in their own right, painted by [transcript.renderView]'s ordinary
// walk exactly as they are under a lone expanded delegation. So a group interrupted by an open
// member paints its rows up to that member here and its remaining rows in a second block of this
// same shape after the span, which is why count and last are stated separately: count opens the
// header and is 0 on the continuation block, while last belongs to the whole group's final row.
//
// An open member therefore carries no see-less footer of its own: the row that closes it would sit
// ABOVE the span it opened rather than at the end of it, which is an affordance pointing at the
// wrong thing (item 4's rule). Its own leader row keeps the click in both states, as every member's
// does. A member with no span at all is an ordinary group member and takes that painter whole,
// footer included — its body is the whole of what it hides.
//
// What a row SAYS is not the group's business at all: each member is read exactly as the lone block
// it folded from (collapsedSubAgentView), so folding changes the frame around a delegation and
// never the record of it.
func renderSubAgentGroup(th theme, count int, members []subAgentMember, width int, state blockState) blockPaint {
	var out blockPaint
	if count > 0 {
		label := th.toolLabel.Render(groupLabelOf(members)) + " " +
			th.toolIndicator.Render(fmt.Sprintf(groupCountFormat, count))
		out.add(hangingWrap(th, th.toolHeader, state.star()+" ", label, width), targetNone)
	}
	room := toolRowCells(th, width)
	for _, m := range members {
		marker, spanned := branchMarker(m.last), len(m.span) > 0
		view := m.head.tool
		if spanned && !m.head.expanded {
			view = collapsedSubAgentView(th.measure, m.head, m.span)
		}
		view = guardPromotions(th, []toolView{view}, room, marker)[0]
		rows, hides := renderSubAgentMemberRows(th, view, marker, width, room, m.head.expanded, spanned)
		kind := targetNone
		if hides {
			kind = targetHeader
		}
		out.addFor(m.offset, rows, kind)
	}
	return out
}

// groupLabelOf is the label a folded group of delegations names itself with: the members' own, off
// the first of them, falling back to the constant for a view built before the registry knew the
// tool (a replayed record, a hand-built test transcript).
func groupLabelOf(members []subAgentMember) string {
	if len(members) > 0 && members[0].head.tool.Label != "" {
		return members[0].head.tool.Label
	}
	return subAgentGroupLabel
}

// renderSubAgentMemberRows paints one member of a folded sub-agent group and reports whether the
// collapsed row hides anything — which is both what makes it wear an indicator and what makes it a
// click target, the same question every folded shape asks (renderGroupMember).
//
// A delegation that left a RUN behind it always hides something, whatever its own report says: the
// span is what expanding reveals. Its row is one line collapsed, and open it is that same line plus
// the report the delegate returned, under the member gutter — the span itself follows outside this
// block (renderSubAgentGroup).
//
// A delegation with no span is an ordinary member and goes through the ordinary painter, so a
// refused delegation folds, opens and closes exactly as a read or a terminal call does.
func renderSubAgentMemberRows(th theme, tv toolView, marker string, width, room int,
	expanded, spanned bool) (lines []string, hides bool) {
	if !spanned {
		return renderGroupMember(th, tv, marker, memberGutter, width, room, expanded)
	}
	row := leaderRow(th, tv, marker, room, expanded, noRemainder)
	if !expanded {
		return []string{indicatorRow(th, row, width, glyphCollapsed)}, true
	}
	out := []string{indicatorRow(th, row, width, glyphExpanded)}
	for _, d := range tv.Details.all() {
		out = append(out, gutteredWrap(th, detailStyle(th, d.Kind, true), memberGutter, memberGutter,
			d.Text, room)...)
	}
	return out, true
}

// collapsedSubAgentView is what a COLLAPSED delegation shows of itself, wherever it is folded: its
// own header view with the cascading summary in the outcome slot and no body at all. The body is
// dropped because the summary already carries the report's first line and no block says the same
// thing twice in two adjacent rows; the copy is a paint-time act on facts the entry keeps whole,
// which is why expanding shows the report the delegation actually returned.
//
// One reading serves the lone block and the folded group's member row alike (renderSubAgentRun,
// renderSubAgentGroup): grouping changes the frame a delegation is drawn in, and a second wording
// of "what does a collapsed delegation say" would part company with this one — taking the per-child
// live tail a fan-out is observed through (ADR 0039) with it.
func collapsedSubAgentView(measure widthAuthority, head entry, span []entry) toolView {
	view := head.tool
	view.Summary = subAgentSummary(measure, head, span)
	view.Details = toolBody{} // the zero body: no lines, and so nothing to lay out beneath
	return view
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

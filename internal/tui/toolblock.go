package tui

import (
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

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
// cell while it still holds an open call. Its marker reaches the ROW's left edge, where an expanded
// lone delegation hangs the ┌─┶ that opens its frame in place of the tree's ┕ (blockState.marker).
//
// It also marks the block's CLICK SURFACE as it emits it, because the lines and the marks have to
// be one act: a second pass over the finished lines would be a second derivation of the same
// accounting, and the two would drift the first time the shape changed (ADR 0030's rule). That
// surface is the block WHOLE — its header, the leader row beneath it, and, open, every body line —
// the shape the prompt block has always had (renderUserBlock): a reader who wants the rest of a
// Terminal call's output clicks the output, not the one row of the block that happens to be its
// header. It has no exception: the `+N more lines` count rides the leader row's outcome slot
// (collapsedRemainder), so the block emits no line that means anything other than "toggle me".
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
	views = guardPromotions(th, views, toolRowCells(th, width), branchMarker(true))
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
		out.join(renderToolBranch(th, tv, state.branchMarkerIn(i, len(views)), width, state.expanded, toggle))
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
		rows, hides := renderGroupMember(th, tv, branchMarker(i == len(views)-1), memberGutter, width, room,
			state.memberExpanded(i))
		kind := targetNone
		if hides {
			kind = targetHeader
		}
		out.addFor(i, rows, kind)
	}
	return out
}

// toolRunView is one RUN of a super-group as the painter needs it: the calls it folds, whether its
// type row is open, and each member's own state. It is the paint-time reading of [toolRun], which
// names the same run by index — the indexes are what the transcript resolves a click through, and
// the views are what the painter draws, so the two shapes are kept apart rather than one being made
// to serve both (superRunViews builds these from those).
type toolRunView struct {
	views    []toolView
	expanded bool   // the run's TYPE ROW state (entry.typeExpanded on its head)
	members  []bool // each member's own expanded flag, in view order
}

// memberExpanded is the n'th member's own view state, bounded for the repaint path exactly as
// [blockState.memberExpanded] is: a short slice is a collapsed member and never a panic mid-frame.
func (r toolRunView) memberExpanded(n int) bool {
	return n >= 0 && n < len(r.members) && r.members[n]
}

// The umbrella header's own wording (design call 9, docs/layout/tool-layout.md, "Vocabulary"):
// "✦ Tools (7 calls)". superGroupLabel takes the label's bold gold like any tool name — it is what
// the block IS — and the count beside it the faint tone every group count wears, because N is the
// block's own arithmetic rather than part of a name.
const (
	superGroupLabel = "Tools"
	superCallNoun   = "call"
)

// renderSuperGroup paints the umbrella: adjacent runs of DIFFERENT tools folded under one
// "✦ Tools (N calls)" header, one row per run in time order (docs/layout/tool-layout.md, "Grouped
// tools collapsed (different types / super-group)"). It is the same list the same-label group is,
// one level up — the rows stand for runs instead of for calls — and everything about the shape
// follows from that:
//
//   - a TYPE ROW is the leader row about a whole run (leaderRowIn): the label and its "(N)" where a
//     call would put its target, the run's aggregate in the outcome slot (runAggregate), and a ▶/▼
//     of its own at the block's right edge. Every type row is a toggle target, because every one of
//     them has member rows to reveal — even a run of 1, whose member row carries the target the type
//     row does not.
//   - an OPEN type row lists its members beneath it in item 1's row shape, drawn one level deeper
//     (superMemberMarker) and by the very painter a plain group's members go through
//     (renderGroupMember), so a member opens onto its body inside a type row exactly as it does
//     inside a group — the sketch's 2nd step.
//   - the HEADER wears no state indicator at all: the umbrella's floor is its type rows, it never
//     folds to one line, and a click there closes every open child instead (targetUmbrella). It is
//     marked as a target only while something IS open, so a header with nothing to close keeps a
//     click's selection meaning rather than offering an affordance that does nothing.
//
// The count the header states is the number of CALLS, not of rows: a reader wants to know how much
// work is folded away, and the rows already say how it divides. Its star is the block's, live while
// any call in it is still open (blockState.live) — the running call being the last row's last
// member, by construction of a time-ordered walk (design call 2).
//
// Each run's views go through the promote-guard on their own (guardPromotions), in the frame their
// rows are actually drawn in: demotion changes what a member hides, and that is what its indicator
// and its click surface are answered from — the same reason the guard runs at the block's entrance
// rather than inside a row.
func renderSuperGroup(th theme, runs []toolRunView, width int, state blockState) blockPaint {
	calls := 0
	for _, r := range runs {
		calls += len(r.views)
	}
	label := th.toolLabel.Render(superGroupLabel) + " " +
		th.toolIndicator.Render("("+plural(calls, superCallNoun)+")")
	room := toolRowCells(th, width)

	var rows blockPaint
	open, at := false, 0 // at: the run head's offset from the umbrella's own head
	for i, r := range runs {
		views := guardPromotions(th, r.views, room, superMemberMarker(true))
		row := leaderRowIn(th, typeRowText(views), typeRowPaint(th, views),
			runAggregate(views), branchMarker(i == len(runs)-1), room, r.expanded, noRemainder)
		rows.addFor(at, []string{indicatorRow(th, row, width, stateIndicator(r.expanded))}, targetType)
		if r.expanded {
			open = true
			for k, tv := range views {
				lines, hides := renderGroupMember(th, tv, superMemberMarker(k == len(views)-1),
					superMemberGutter, width, room, r.memberExpanded(k))
				kind := targetNone
				if hides {
					kind = targetHeader
				}
				rows.addFor(at+k, lines, kind)
			}
		}
		at += len(r.views)
	}

	header := targetNone
	if open {
		header = targetUmbrella
	}
	var out blockPaint
	out.add(hangingWrap(th, th.toolHeader, state.star()+" ", label, width), header)
	out.join(rows)
	return out
}

// typeRowText is a type row's left content as it is MEASURED: the run's label, and the "(N)" beside
// it for a run of more than one. A run of 1 counts nothing, which is the rule the same-label group's
// header already answers to (groupCountFormat) — the number exists to say that a row stands for more
// than the single call it otherwise looks like.
func typeRowText(views []toolView) string {
	if len(views) == 0 {
		return ""
	}
	if len(views) < 2 {
		return views[0].Label
	}
	return views[0].Label + " " + fmt.Sprintf(groupCountFormat, len(views))
}

// typeRowPaint dresses whatever of [typeRowText] survived the row's clip: the label in the tool
// label's own bold gold — it is a tool name, and scanning the umbrella for those is what the reader
// opened it to do — and the count after it in the faint tone every group count wears. A clip that
// reached into the count leaves nothing to split on, and the survivor is painted as the label it
// began as.
func typeRowPaint(th theme, views []toolView) func(string) string {
	count := ""
	if len(views) >= 2 {
		count = " " + fmt.Sprintf(groupCountFormat, len(views))
	}
	return func(clipped string) string {
		if head, ok := strings.CutSuffix(clipped, count); ok && count != "" {
			return th.toolLabel.Render(head) + th.toolIndicator.Render(count)
		}
		return th.toolLabel.Render(clipped)
	}
}

// superRunViews is the umbrella's paint input: each of its runs as the views it folds plus the view
// state of the row and its members ([toolRunView]). The state is COPIED out of the entries, like
// memberFlags and for its reason — a painter is handed what it needs to draw and nothing it could
// write through (ADR 0011).
func superRunViews(entries []entry, g superGroup) []toolRunView {
	runs := make([]toolRunView, 0, len(g))
	for _, r := range g {
		span := entries[r.at : r.at+r.n]
		views := make([]toolView, len(span))
		for i := range span {
			views[i] = span[i].tool
		}
		runs = append(runs, toolRunView{
			views:    views,
			expanded: entries[r.at].typeExpanded,
			members:  memberFlags(span),
		})
	}
	return runs
}

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
// block's right edge and a member does not gain two columns of target by having nothing to reveal.
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
//
// marker and gutter are the FRAME the member is drawn in — the row's branch marker and the prefix
// its continuation rows hang under — rather than constants, because a member of a super-group's type
// row sits one level deeper than a member of a plain group and both frames have to reach the same
// painter (superMemberMarker, renderSuperGroup).
func renderGroupMember(th theme, tv toolView, marker, gutter string, width, room int, expanded bool) (lines []string, hides bool) {
	row := leaderRow(th, tv, marker, room, expanded, noRemainder)
	if hides = tv.Details.len() > 0; !hides {
		return []string{row}, false
	}
	if expanded {
		return renderExpandedMember(th, tv, marker, gutter, width, room), true
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
func renderExpandedMember(th theme, tv toolView, marker, gutter string, width, room int) []string {
	row := leaderRow(th, tv, marker, room, true, noRemainder)
	out := []string{indicatorRow(th, row, width, glyphExpanded)}
	for _, d := range tv.Details.all() {
		out = append(out, gutteredWrap(th, detailStyle(th, d.Kind, true), gutter, gutter, d.Text, room)...)
	}
	return append(out, seeLessRow(th, gutter, width))
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

// seeLessRow is the row that closes an OPEN body: the gutter its block continues its rows under —
// empty for a block whose body needs none — then the see-less marker flush against the block's right
// edge. It borrows the prompt block's WORDING and not its treatment — promptSeeLess is the one
// vocabulary (design call 7), while the style is the tool block's own marker tone, the same a
// "+N more lines" wears, because both are things apogee wrote onto a block rather than lines the
// tool produced.
//
// One row shape serves both callers — the open group member, under its │ gutter, and the ungrouped
// block, whose body hangs at the branch marker's indent and closes with the marker alone
// (seeLessFooter). A block that grew a footer of its own shape would be a second answer to "how does
// a body end", and the two would drift the first time either moved.
func seeLessRow(th theme, gutter string, width int) string {
	pad := strings.Repeat(" ",
		max(0, width-th.measure.Width(gutter)-th.measure.Width(promptSeeLess)))
	row := pad + th.toolMarker.Render(promptSeeLess)
	if gutter == "" {
		return row
	}
	return th.toolDetail.Render(gutter) + row
}

// seeLessFooter is the row an EXPANDED single block closes its body with — the canon spec's
// "Single tool expanded" sketch, where see less… trails the last body row flush against the right
// edge (docs/layout/tool-layout.md, "Fold states and interaction"). It is the extra collapse target
// the two fold states are given, and the only row a block paints that exists ONLY to be clicked —
// which is why it wears the same marker tone the "+N more lines" affordance does rather than any
// body tone.
//
// Two conditions, and both are about not writing an affordance that lies. A block that hides nothing
// is not toggleable at all (renderToolBlock's toggle), so a see-less there would offer a click that
// does nothing; and a block that painted no body row has nothing the footer could be closing — the
// sub-agent run whose whole reveal is its span, whose head's own report is empty. The caller adds
// what comes back under its OWN toggle kind, so the footer joins the block's click surface by the
// same act that paints it (blockPaint.add).
func seeLessFooter(th theme, body []string, width int, toggle targetKind) []string {
	if toggle == targetNone || len(body) == 0 {
		return nil
	}
	return []string{seeLessRow(th, "", width)}
}

// memberGutter is the continuation prefix an open member's rows carry where a hanging wrap would
// put blanks: the branch marker's own two-column indent, then the gutter glyph and its space, so
// the gutter stands exactly under the ┝ it continues. Being branchMarker's shape is what keeps an
// open member's text in the column its collapsed row used.
const memberGutter = "  " + glyphMemberGutter + " "

// The frame a super-group's MEMBER rows are drawn in, one level inside the type row they belong to
// (docs/layout/tool-layout.md, "Grouped tools expanded 1st step"): the type row's own gutter, then
// the member's branch glyph — "  │ ┝ " — with its body continuing under "  │ │ ". Both are built
// from memberGutter rather than restated, so a member of a type row sits exactly under the ┝ of the
// row it opened out of, and the two frames keep the one cell count that lets them share a painter
// (renderGroupMember).
const superMemberGutter = memberGutter + glyphMemberGutter + " "

func superMemberMarker(last bool) string {
	if last {
		return memberGutter + glyphBranchLast + " "
	}
	return memberGutter + glyphBranch + " "
}

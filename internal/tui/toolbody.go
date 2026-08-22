package tui

import (
	"strings"
)

// ----------------------------------------------------------------------------
// Tool bodies — one painter behind every frame a body is drawn in
// ----------------------------------------------------------------------------
//
// A tool body is the same thing wherever it is painted: the call's detail lines, laid out one under
// another inside whatever frame the path around them draws. Five paths draw one — the targetless
// shape's branch list open ([renderDetails]) and collapsed ([clipDetails]), an ungrouped call's body
// under its branch marker ([renderSubDetails]), an open group member's under its │ gutter
// ([renderExpandedMember]) and an open sub-agent member's under the same
// ([renderSubAgentMemberRows]) — and they differ in FRAMING alone: what leads a line's first row,
// what continues it, which tone the lines take, and how many rows one line may spend before the clip
// takes the rest.
//
// So the framing is stated as a value ([bodyFrame]) and the laying-out is written once
// ([bodyFrame.paint]). Five hand-written loops over the same three wrap primitives were five places
// a rule about bodies had to be remembered; one painter behind five specs is one.
//
// A path whose call may have recorded Edit regions asks [paintToolBody] instead, which puts ADR
// 0052's reading rule in front of that painter: the regions become two panes where they fit at the
// width the path holds ([splitBody]), and the stacked lines are what the body reads as otherwise.
// That wiring used to be spelled out at each of the three paths that can reach it, so a fourth path
// could arrive with a fourth answer; written once here, it cannot.
//
// The primitive chain underneath is unchanged and is not this module's business: [hangingWrap],
// [gutteredWrap] and [clipWrap] still lay ONE line out, [detailStyle] still says what colour it
// takes, and this painter sits one level above them, spending them per line in the frame's shape.

// bodyFrame is the physical frame one paint path draws a tool body in, stated as a value so the
// path can hand it to the painter instead of writing the lay-out loop itself.
//
// Every field is a difference the five frames actually have today, and each is preserved exactly as
// that frame spends it (the merge policy of docs/plans/"2026-08-19 - 04"): nothing here is a new
// choice, and no frame gained a capability by being written down.
type bodyFrame struct {
	// lead is the prefix a detail line's FIRST row carries. It is asked per line and told whether
	// the line closes the list, because the branch shapes spend a different glyph on their last
	// line (branchMarker's ┕) where the constant-prefix shapes hand back the same string every
	// time.
	lead func(last bool) string

	// guttered says continuation rows carry the frame's own prefix again ([gutteredWrap]) rather
	// than hanging under the lead in blanks ([hangingWrap]). The two paint differently and not only
	// differently-wide: a guttered frame's prefix is chrome painted in the detail tone beside the
	// text's own style, which is exactly what makes an open member's │ read as frame rather than as
	// part of the line beside it.
	guttered bool

	// expanded is the block STATE the lines take their tone from (detailStyle, detailTone). Only
	// the collapsed branch list paints on false; every other frame is an expanded paint outright,
	// because a collapsed block of those shapes paints no body line at all (collapsedBodyRows).
	expanded bool

	// rowCap is how many physical rows ONE detail line may spend before the clip takes the rest and
	// ends the kept row in " …" ([clipWrap]); 0 leaves the line uncapped. Only the collapsed branch
	// list caps, and a capped frame is a hanging one — [clipWrap] IS [hangingWrap] under a budget.
	rowCap int

	// closing paints whatever still rides after the SPLIT reading has replaced this frame's lines
	// with panes; nil where a frame's body has nothing riding after it. It exists for the targetless
	// shape alone, whose summary has no leader row to ride and closes the branch list instead
	// (branchDetails) — a fact that is as true of a body in two panes as of one in branch lines.
	closing func(th theme, tv toolView, width int) []string
}

// continuation is the prefix every row after a detail line's first carries — the frame's own prefix
// where it gutters, and the lead's width in blanks where it hangs — and so also the column the SPLIT
// reading hangs its panes under. Asking one function for both is what keeps a split body starting in
// the column its stacked twin would have.
func (f bodyFrame) continuation(th theme) string {
	lead := f.lead(true)
	if f.guttered {
		return lead
	}
	return strings.Repeat(" ", th.measure.Width(lead))
}

// paint is THE body painter: detail lines, a frame, a width, and the rows that come back. Each line
// is styled for its kind in the frame's state (detailStyle) and laid out by the one primitive the
// frame's shape asks for — clipped under a row budget, guttered, or hanging.
//
// A line carrying a chrome gutter of its own ([detailLine.Gutter] — the stacked diff reading's line
// number) has it appended to the frame's prefix rather than to its text, and every primitive here
// paints its prefix outside the band (renderHangingRow, gutteredWrap). That is what holds the
// numbers off the tint (ratified call 3 of docs/plans/"2026-08-19 - 05") without any wrap rail
// having to tell a number from the code beside it — and, because the prefix widens, a wrapped
// number's continuation rows hang under the text with the gutter blank.
//
// It REPORTS a clip for the same reason [clipWrap] does: whether a collapsed block hides anything is
// width-dependent once its lines can be cut, and the indicator, the click target and the paint all
// have to agree about it (blockHidesWhenCollapsed). An uncapped frame can never cut, and answers
// false.
func (f bodyFrame) paint(th theme, details []detailLine, width int) (rows []string, clipped bool) {
	cont := f.continuation(th)
	out := make([]string, 0, len(details))
	for i, d := range details {
		style := detailStyle(th, d.Kind, f.expanded)
		lead := f.lead(i == len(details)-1) + d.Gutter
		switch {
		case f.rowCap > 0:
			capped, cut := clipWrap(th, style, lead, d.Text, width, f.rowCap)
			out = append(out, capped...)
			clipped = clipped || cut
		case f.guttered:
			out = append(out, gutteredWrap(th, style, lead, cont+blankColumns(th, d.Gutter), d.Text, width)...)
		default:
			out = append(out, hangingWrap(th, style, lead, d.Text, width)...)
		}
	}
	return out, clipped
}

// blankColumns is s's own width in blanks. It stands a line's chrome gutter empty on the
// continuation rows of a wrapped line ([bodyFrame.paint]) so the text keeps one column down the
// whole line — the pad [splitCell.paint] puts under its panes' numbers, for the same reason: a
// second number on a continuation row would claim the wrapped line was two.
//
// The count is the width authority's (ADR 0030), not the string's length, so a gutter is padded by
// the columns it actually occupies.
func blankColumns(th theme, s string) string {
	return strings.Repeat(" ", th.measure.Width(s))
}

// paintToolBody is [bodyFrame.paint] with ADR 0052's reading rule in front of it: a call that
// recorded Edit regions is painted as two panes wherever the frame's own room allows the
// arrangement ([splitBody]), and as the stacked lines it was presented with otherwise.
//
// It is the ONE place a paint path chooses, and every path asks it with the width IT holds at the
// moment it paints — so the choice stays a property of this body at this width, kept nowhere, and a
// resize can flip the reading with no state to keep in step. The paths differ only in the frame they
// bring, which is why the frame is the parameter and the decision is not.
//
// A frame whose body can never take that reading — the collapsed branch list, a sub-agent member's
// own report — is painted through [bodyFrame.paint] directly rather than through here, and so has
// nothing to ask.
func paintToolBody(th theme, tv toolView, details []detailLine, frame bodyFrame, width int) []string {
	panes, split := splitBody(th, tv, frame.continuation(th), width)
	if !split {
		rows, _ := frame.paint(th, details, width)
		return rows
	}
	if frame.closing == nil {
		return panes
	}
	return append(panes, frame.closing(th, tv, width)...)
}

// splitBody is the SPLIT reading of an expanded call's body — the recorded Edit regions as two
// panes (splitDiffRows) laid out under prefix in width columns — and false when this body does not
// take that reading at all: no regions were recorded, so the block keeps whatever body it was
// presented with (ratified call 9, ADR 0052), or the panes would be too narrow to be worth the
// arrangement (splitDiffFits) and the stacked rows the same regions already built are the reading.
//
// The prefix is measured off the width before the panes are composed and painted in the detail
// tone, exactly as the detail-line painters spend it (bodyFrame.paint), so a split body starts in
// the column its stacked twin would have and the row still ends inside the width it was given.
//
// A body whose regions came from a diff spanning SEVERAL files is composed section by section, each
// under a muted row naming its file (ratified call 10) — the same header the stacked reading of it
// already carries, so the two readings of one printed diff say the same things in the same order.
// The header is truncated to the width rather than wrapped: it names a file, and a name broken over
// two rows would read as two of them. Whether the panes fit is asked ONCE over all the regions, so
// a body cannot paint half in panes and half stacked.
func splitBody(th theme, tv toolView, prefix string, width int) (rows []string, split bool) {
	inner := width - th.measure.Width(prefix)
	if !splitDiffFits(tv.Regions, inner) {
		return nil, false
	}
	var panes []string
	for _, section := range regionFileSections(tv.Regions, tv.RegionFiles) {
		if section.File != "" {
			panes = append(panes, th.toolDetail.Render(truncateToWidth(th, section.File, inner)))
		}
		panes = append(panes, splitDiffRows(th, section.Regions, inner)...)
	}
	if len(panes) == 0 {
		return nil, false
	}
	pad := th.toolDetail.Render(prefix)
	out := make([]string, len(panes))
	for i, row := range panes {
		out[i] = pad + row
	}
	return out, true
}

// expandedBranchFrame is the frame the EXPANDED targetless shape draws its branch list in: every
// line leads with its own ┝/┕ (branchMarker, the elbow on the last), continuation rows hang under
// that marker in blanks, and the lines take the open tone outright — the collapsed twin of this
// shape is a frame of its own ([collapsedBranchFrame]), never this one under a flag.
//
// Its list is CLOSED by the call's summary, which in this one shape has no leader row to ride
// (branchDetails). The stacked reading carries that line inside its own lines; the split reading has
// replaced those lines with panes, so the closer is re-laid beneath them, keeping its ┕ under the
// panes.
func expandedBranchFrame() bodyFrame {
	return bodyFrame{lead: branchMarker, expanded: true, closing: summaryBranchRows}
}

// summaryBranchRows lays a call's summary out as the one branch that closes a split body's list. It
// wears the ┕ elbow by construction — it is the only line in the list handed to the painter — which
// is the same glyph it takes as the last line of the stacked reading.
func summaryBranchRows(th theme, tv toolView, width int) []string {
	if tv.Summary.Text == "" {
		return nil
	}
	return renderDetails(th, []detailLine{tv.Summary.detailLine}, width)
}

// collapsedBranchFrame is [expandedBranchFrame] under the collapsed block's row budget and in the
// dim tone: the same ┝/┕ branches, each held to collapsedBranchRows rows with the clip taking the
// rest. It is what keeps the targetless shape inside the four-row budget the targeted one is held
// to — unclipped, one argument blob's first line soft-wrapped the block as tall as the terminal was
// narrow. It never splits: a collapsed block paints the reading it can fit on one row per line, and
// panes are not that.
func collapsedBranchFrame() bodyFrame {
	return bodyFrame{lead: branchMarker, rowCap: collapsedBranchRows}
}

// subDetailFrame is the frame an ungrouped TARGETED call's body is drawn in: no marker of its own,
// just the branch marker's width in blanks, so the body reads as that branch's content rather than
// as siblings of it. indent is the marker's measured width, which the caller already holds.
func subDetailFrame(indent int) bodyFrame {
	pad := strings.Repeat(" ", indent)
	return bodyFrame{lead: func(bool) string { return pad }, expanded: true}
}

// openMemberFrame is the frame an OPEN group member's body is drawn in: every row under the gutter
// its leader row continues into, painted as chrome beside the line's own style. gutter is the
// member's own frame rather than a constant, because a member of a super-group's type row sits one
// level deeper than a member of a plain group (memberGutter, superMemberGutter) and both reach this
// same painter.
func openMemberFrame(gutter string) bodyFrame {
	return bodyFrame{lead: func(bool) string { return gutter }, guttered: true, expanded: true}
}

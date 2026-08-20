package tui

import (
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// renderToolBranch renders one call of a tool block as its branch line (plus whatever hangs
// beneath it). Two shapes, and they are the whole grammar:
//
//   - a call WITH a target — the branch is the leader row every single block and every group
//     member takes (leaderRow): the branch marker, the target, a dotted leader, then the call's
//     Summary in an outcome slot flush against the row's right edge ("┕ main.go ⋯⋯⋯ 154 lines",
//     "┕ main.go ⋯⋯⋯ +2 −2"), the body it is hiding counted in that same slot while it is
//     collapsed ("┕ go test ./... ⋯⋯⋯ exit 0 · +3 more lines"). There is no target column to pad
//     to — the leader absorbs whatever the targets differ by, which is what puts a block of one
//     and a block of ten's outcomes in the same place. A call still in flight has no summary yet and lets the dots run to the
//     row's edge; the block repaints whole once the result folds in. Its Details, if any, are
//     the block's body and lay out beneath the branch at the branch marker's own width — not as
//     ┝/┕ branches of their own, because only calls are (a Terminal call's output, a diff body under its
//     diffstat) — painted whole when the block is expanded and not at all when it is collapsed.
//   - a call with NO target — the only shape with no target line: the header stands alone and
//     the detail lines are themselves the ┝/┕ branches, the summary last since it has no branch
//     line to ride (an unregistered tool's labelled arguments then its "error: …"
//     outcome, a stray result). Collapsed, that branch LIST is what the cap falls on — the
//     block has no body to cap instead — each surviving line clipped to a row of its own
//     (clipDetails). It counts what it cut nowhere: the count rides an outcome slot, this shape has
//     none, and the header's ▶ is what says there is more behind it.
//
// The shape follows from which halves of the outcome are filled and never from how many Details
// there are: a body of one line and a body of ten lay out the same way.
//
// The block's state reaches BOTH shapes. An expanded call lays out every line the entry retained,
// soft-wrapping whatever is overlong, and counts nothing — the see-less footer closes it
// instead (seeLessFooter), which is where the pointer of a reader who has just read to the end of a
// body already is. Where that call recorded Edit regions and the row is wide enough for them, the
// body lays out as two panes instead of as those lines (splitBody): the ARRANGEMENT is the width's
// to choose at every paint, and neither shape above changes by it. A COLLAPSED targeted call is the row budget's (layout.md, "Collapsed and expanded
// blocks"): its branch is the ONE leader row (leaderRow), no body line is painted at all, and the
// slot on that row counts the body WHOLE — the sketch's "+5 more lines" over a five-line output,
// riding the outcome it already carries (collapsedRemainder). So a collapsed block of that shape
// stands two rows tall whatever tool filled it and however long its target is, which is the point:
// a scrollback of tool calls reads as a list rather than as a wall. A
// collapsed targetless call caps its branch list instead, since there the lines ARE the branches —
// collapsedBodyCap of them, one clipped row each — and lands on the same budget.
//
// toggle is the block's own click surface, settled once by renderToolBlock and spent on every row
// a branch emits: the branch line, the body under it, the targetless shape's branch list. A click
// anywhere on a block that hides something flips it, which is the prompt block's rule read over the
// other collapsible shape in the transcript — a body a reader is looking at is the likeliest place
// for the pointer to be when they want it gone. Every row a branch emits carries that one meaning:
// the remainder no longer stands on a row of its own, so there is no line left whose click could
// only ever open.
func renderToolBranch(th theme, tv toolView, marker string, width int, expanded bool, toggle targetKind) blockPaint {
	if tv.Target == "" {
		if expanded {
			var out blockPaint
			rows := renderBranchList(th, tv, width)
			out.add(rows, toggle)
			out.add(seeLessFooter(th, rows, width, toggle), toggle)
			return out
		}
		shown, _, _ := collapsedCall(tv)
		var out blockPaint
		rows, _ := clipDetails(th, shown, width)
		out.add(rows, toggle)
		return out
	}
	indent := th.measure.Width(marker)
	var out blockPaint
	row := leaderRow(th, tv, marker, toolRowCells(th, width), expanded, collapsedRemainder(tv, expanded))
	if toggle != targetNone {
		row = indicatorRow(th, row, width, stateIndicator(expanded))
	}
	out.add([]string{row}, toggle)
	if expanded {
		body := renderSubDetails(th, tv, indent, width)
		out.add(body, toggle)
		out.add(seeLessFooter(th, body, width, toggle), toggle)
	}
	return out
}

// collapsedRemainder is the "+N more lines" a TARGETED call's leader row carries in its outcome
// slot: the count of the body a collapsed block is hiding, and nothing at all once the block is open
// — an expanded block hides no line, so there is nothing to count (renderSubDetails paints them
// all).
//
// It is the count alone and never a row of its own. The marker used to hang beneath the leader row,
// which cost every collapsed block a second row to say something the slot had the width for; folded
// into the slot it says the same thing in the row the block was going to spend anyway, and a
// collapsed lone call is exactly its header and its row (ISSUES.md, 2026-08-11).
//
// The wording is collapseAtCap's, the one place every collapsed shape counts its remainder, so the
// slot's tail and the prompt block's see-more stay two voices about one number (splitAtCap).
func collapsedRemainder(tv toolView, expanded bool) string {
	if expanded {
		return ""
	}
	_, remainder, truncated := collapsedDetails(tv.Details)
	if !truncated {
		return ""
	}
	return remainder.Text
}

// The COLLAPSED block's row budget — the house numbers behind "a collapsed block stands at most
// three rows tall": its header and, beneath it, the content rows its shape spends, whatever tool
// filled it and however long its target is (layout.md, "Collapsed and expanded blocks";
// docs/layout/tool-layout.md).
//
// A targeted call spends exactly ONE of them and no number here says so: the leader shape fills the
// width exactly and cuts the target to make it (leaderRow), and what the block is hiding is counted
// in that row's own outcome slot rather than on a row beneath it (collapsedRemainder). Nothing of
// the output is previewed: one preview line of a hundred said little and cost every block a row,
// while the count says the same thing in cells the row had spare.
//
// collapsedBodyCap is the TARGETLESS shape's cap — how many of its branch lines survive the
// collapse (collapsedCall), the block having no body to cap instead. It is the taller of the two
// shapes and the one the three-row budget is measured against, since there the branch lines ARE the
// content and there is no target line above them to read them against.
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
// "+N more lines" count of all of it (truncated says whether there is a body at all; the count is
// meaningless when there is not). The rows a collapsed block has to spend go to its
// target (its one leader row), so a body is a thing a click reveals rather than a thing the
// scrollback previews — which is why the shown slice it still returns is always empty, kept only so
// the two collapsed shapes answer in one signature.
//
// Truncation is a render-time act on facts the entry keeps whole (layout.md), so the count is
// composed on every repaint and never stored — which is what keeps it a statement about the paint
// rather than a line of the body, and lets the painter hand it to the leader row's slot instead of
// sniffing the finished lines for the wording (collapsedRemainder).
//
// The split is also the toggle-target rule's oracle for this shape: truncated is "the collapsed
// paint hides body", and since a targeted block's leader row is the same row open or closed, that
// alone is what makes its header clickable (blockHidesWhenCollapsed, through collapsedCall).
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
// It answers for the lines alone, and for a TARGETED call that is the whole answer: its branch is
// one leader row in both states (leaderRow), so an over-long target ends in " …" open or closed and
// a clipped target reveals nothing to expand (blockHidesWhenCollapsed). The width still reaches the
// TARGETLESS shape, whose surviving branch lines the row budget cuts — a fact about the width rather
// than about the entry, which lives with the clip that takes it (clipDetails).
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

// renderBranchList is what the EXPANDED targetless shape paints beneath its header — the branch
// list of [branchDetails], or the SPLIT reading of the body with that list's one non-body line
// still closing it.
//
// The shape is reachable by a diff-bodied call whose target resolves empty — a bare
// `git_diff_range` with neither base nor head is the common one (refRangeTarget) — and without
// this arm that call would keep the stacked reading at every width while the same diff with its
// refs named painted panes. Only the BODY chooses the reading: the summary has no leader row to
// ride in this shape and closes the branch list instead (branchDetails), which is as true of a
// body in two panes as of one in branch lines, so it keeps its ┕ under the panes.
//
// The panes hang at the branch marker's width in blanks rather than off ┝/┕ markers of their own:
// a marker per PHYSICAL row would mark a wrapped continuation as a branch, and the split reading's
// rows are not lines of the body but its arrangement.
func renderBranchList(th theme, tv toolView, width int) []string {
	return paintToolBody(th, tv, branchDetails(tv), expandedBranchFrame(), width)
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

// subAgentOpenMarker is branchMarker's answer for an EXPANDED delegation: the frame it opens takes
// the row's whole left edge, so the two blank columns a ┝/┕ hangs off become the corner and the arm
// carrying it across to the branch (docs/layout/tool-layout.md, "Grouped Sub-agents"). The "─" is
// written out rather than named because it is the corner's ARM and nothing else in the transcript
// draws one — the markdown table's rule (glyphTableRule) is a different element that happens to
// share the shape.
//
// It is exactly branchMarker's four cells, which is what lets a member open and close without its
// text moving sideways under the very click that opened it — the same promise the ▶ → ▼ swap keeps
// at the row's other edge (groupIndicatorCells).
const subAgentOpenMarker = glyphRailCorner + "─" + glyphRailTee + " "

// paintRowMarker dresses a leader row's leading marker. Every marker but one is the row's own chrome
// and takes its detail tone with the target beside it; the delegation frame's corner is the
// exception, because it is the top end of the RAIL running down the span below it and has to be read
// as the same line (design call 2 of docs/plans/"2026-08-11 - 01"): ┌ alone takes the rail's gold,
// while the arm and the branch it reaches — and the ┝/┕/│ of every other shape — stay in the tone
// their row is in.
//
// The split is made HERE, at the one place a marker is painted, rather than by handing pre-styled
// markers down: leaderRowIn measures the marker to lay the row out, and a measured string with
// escapes in it is a width waiting to be got wrong.
func paintRowMarker(th theme, marker string, expanded bool) string {
	if rest, ok := strings.CutPrefix(marker, glyphRailCorner); ok {
		return th.subRail.Render(glyphRailCorner) + detailTone(th, expanded).Render(rest)
	}
	return detailTone(th, expanded).Render(marker)
}

// renderSubDetails lays a call's body out beneath its branch line, indented to the branch marker's
// width and styled by kind, so it reads as that branch's content rather than as siblings of it. It
// is one of the five frames the shared body painter draws (subDetailFrame), and it asks that painter
// for the READING too: the two panes where the call recorded Edit regions this width has room for,
// and the detail lines otherwise (paintToolBody).
//
// It is the EXPANDED paint's alone — a collapsed block paints no body line at all
// (collapsedBodyRows) — so its frame carries the open tone outright rather than a parameter that
// could only ever be true (bodyFrame.expanded).
func renderSubDetails(th theme, tv toolView, indent, width int) []string {
	return paintToolBody(th, tv, tv.Details.all(), subDetailFrame(indent), width)
}

// toolCallRun returns the consecutive tool-call entries starting at entries[i] that fold into one
// grouped block, as their presentation views. WHICH entries those are is [sameLabelRun]'s answer —
// same sub-agent depth, same friendly Label, every member groupable — asked here for the views the
// painter needs, so the same-label group and the super-group that lists such runs as its rows
// (toolSuperGroup) cannot come to disagree about where a run ends. It returns nil when entries[i] is
// not a groupable tool call, and a one-view run when nothing follows it — the caller renders both as
// single blocks.
func toolCallRun(entries []entry, i int) []toolView {
	n := sameLabelRun(entries, i)
	if n == 0 {
		return nil
	}
	views := make([]toolView, 0, n)
	for j := i; j < i+n; j++ {
		views = append(views, entries[j].tool)
	}
	return views
}

// groupable reports whether a tool call can be shown as one member row of a grouped block: it
// needs a Target to lead that row, and it must not be marked solo by the presenter that
// built it (toolView.solo). Nothing else — a body no longer disqualifies a call, because a member
// row no longer has to hold everything the call has to say: it shows one leader row and keeps its
// body behind its own indicator (renderGroupMember, design call 3). So a batch of Terminal calls
// with output and a batch of edits with their diffs group exactly as a batch of reads always has,
// which is what a scrollback of ten same-label calls needed most.
//
// A call with NO target still keeps its own block: there the detail lines ARE the branches
// (renderToolBranch), so there is no leader row to lead. And solo is the
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
// under the row budget and in the dim tone — so its frame carries the open tone outright
// (expandedBranchFrame). It paints the lines it is HANDED and asks nothing about which reading the
// body takes: renderBranchList asks that once for the whole shape, and the one line this is spent
// on again beneath a split body is the summary rather than a body line (summaryBranchRows).
func renderDetails(th theme, details []detailLine, width int) []string {
	rows, _ := expandedBranchFrame().paint(th, details, width)
	return rows
}

// clipDetails is renderDetails under the collapsed block's row budget, and the same painter draws
// both (collapsedBranchFrame): every branch line gets collapsedBranchRows rows and the clip takes
// the rest (clipWrap, which ends a cut row in " …"), so a collapsed targetless block spends as many
// rows on its branch list as the cap left it lines. It is what keeps that shape inside the four-row
// budget the targeted one is held to — unclipped, one argument blob's first line soft-wrapped the
// block as tall as the terminal was narrow.
//
// It REPORTS the cut because whether a collapsed targetless block hides anything is width-dependent
// once its lines can be cut, and the indicator, the click target and the paint all have to agree
// about it (blockHidesWhenCollapsed). It is the only cut that is asked about: the targeted shape's
// row is one row in both states, so the target it clips hides nothing (leaderRow). The rule asks the
// clipper that paints the shape, so it cannot drift from what is on screen.
func clipDetails(th theme, details []detailLine, width int) (lines []string, clipped bool) {
	return collapsedBranchFrame().paint(th, details, width)
}

// detailStyle maps a detail kind and its block's STATE to a style. EVERY kind takes the plain tone
// of the state (detailTone); a diff kind adds the band its direction wears under that tone — the
// turquoise `diff-add-bg` or the red `diff-del-bg` (th.diffAdded / th.diffRemoved, backgrounds
// alone). The two channels answer two different questions and so cannot crowd each other: the tone
// says how loudly the block is speaking, which is a function of the state, and the band says which
// way the line went, which is not — the band is identical collapsed and expanded (view_diff's body
// is their producer — diffBody).
//
// The style is what the painters band a full row with: a background reaching the wrap rail or the
// pane edge (gutteredWrap/hangingWrap/clipWrap, splitCell.paint), which is why the diff kinds carry
// their colour as a background here rather than as a foreground on the glyphs.
func detailStyle(th theme, kind detailKind, expanded bool) lipgloss.Style {
	tone := detailTone(th, expanded)
	switch kind {
	case detailDiffAdded:
		return tone.Background(th.diffAdded.GetBackground())
	case detailDiffRemoved:
		return tone.Background(th.diffRemoved.GetBackground())
	default:
		return tone
	}
}

// detailTone is the plain-detail gray a tool block's text takes in each of its two states — the
// collapsed dim, or the step brighter an open block reads in (design call 9; the scheme's `muted`
// and `muted-bright` roles). It is the ONE place the state reaches the colour, so the target and
// the body of one block cannot come to disagree about how loudly they are speaking.
//
// It answers for a block's TEXT alone. The chrome — the ▶/▼ indicator, the see-less marker, an open
// member's │ gutter — keeps its own role in both states: those are apogee's marks on the
// block rather than what the block has to say, and brightening them with the content would make the
// affordances shout exactly where the content was meant to. The outcome slot is off this ramp
// entirely: it wears the marker role, which carries a step of its own for the open state
// (summaryStyle), and the `+N more lines` count it carries collapsed wears whatever the slot does
// (slotText).
func detailTone(th theme, expanded bool) lipgloss.Style {
	if expanded {
		return th.toolDetailBright
	}
	return th.toolDetail
}

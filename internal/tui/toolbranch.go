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
//     "┕ main.go ⋯⋯⋯ +2 −2"). There is no target column to pad to — the leader absorbs whatever
//     the targets differ by, which is what puts a block of one and a block of ten's outcomes in
//     the same place. A call still in flight has no summary yet and lets the dots run to the
//     row's edge; the block repaints whole once the result folds in. Its Details, if any, are
//     the block's body and lay out beneath the branch at the branch marker's own width — not as
//     ┝/┕ branches of their own, because only calls are (a Terminal call's output, a diff body under its
//     diffstat) — painted whole when the block is expanded and not at all when it is collapsed.
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
// soft-wrapping whatever is overlong, and grows no remainder marker — the see-less footer closes it
// instead (seeLessFooter), which is where the pointer of a reader who has just read to the end of a
// body already is. A COLLAPSED targeted call is the row budget's (layout.md, "Collapsed and expanded
// blocks"): its branch is the ONE leader row (leaderRow), no body line is painted at all, and the
// marker counts the body WHOLE — the sketch's "+5 more lines" over a five-line output. So a collapsed
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
			rows := renderDetails(th, branchDetails(tv), width)
			out.add(rows, toggle)
			out.add(seeLessFooter(th, rows, width, toggle), toggle)
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
		body := renderSubDetails(th, tv.Details.all(), indent, width)
		out.add(body, toggle)
		out.add(seeLessFooter(th, body, width, toggle), toggle)
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
// It REPORTS the cut because whether a collapsed targetless block hides anything is width-dependent
// once its lines can be cut, and the indicator, the click target and the paint all have to agree
// about it (blockHidesWhenCollapsed). It is the only cut that is asked about: the targeted shape's
// row is one row in both states, so the target it clips hides nothing (leaderRow). The rule asks the
// clipper that paints the shape, so it cannot drift from what is on screen.
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
// and `muted-bright` roles). It is the ONE place the state reaches the colour, so the target and
// the body of one block cannot come to disagree about how loudly they are speaking.
//
// It answers for a block's TEXT alone. The chrome — the ▶/▼ indicator, the `+N more lines` marker,
// an open member's │ gutter — keeps its own role in both states: those are apogee's marks on the
// block rather than what the block has to say, and brightening them with the content would make the
// affordances shout exactly where the content was meant to. The outcome slot is off this ramp
// entirely: it wears the marker role, which carries a step of its own for the open state
// (summaryStyle).
func detailTone(th theme, expanded bool) lipgloss.Style {
	if expanded {
		return th.toolDetailBright
	}
	return th.toolDetail
}

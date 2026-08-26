package tui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// The room the dotted leader flexes in (docs/layout/tool-layout.md, "Width and overflow"). The dot
// the run is built from is glyphLeaderDot, which lives with the other glyphs in theme.go.
//
// leaderGap is the blank held on each side of the run that carries the eye from a row's target to
// the outcome slot at its right edge, so the dots never touch the text they run between.
// leaderMinDots is the floor the run flexes down to before the LEFT target starts giving way — one
// dot, because a leader that vanished entirely would leave two unrelated words abutting and read as
// a single phrase.
const (
	leaderGap     = 1
	leaderMinDots = 1
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
// and the slot's remainder count are all answered from (blockHidesWhenCollapsed). A guard applied at
// the row would leave those three saying the call had nothing to reveal while the paint had just
// hidden a line.
//
// The answer depends on the WIDTH alone and never on the block's state, which is the leader row's
// standing promise read one level up: a row that promoted its line collapsed and demoted it open
// would move out from under the click that opened it.
//
// The slice is copied only where a view actually changed, so the ordinary block — nothing promoted,
// or everything promoted comfortably — reaches the painter as the very slice the entries handed
// over. The copy is shallow and that is enough: demoted rebuilds the body it changes rather than
// writing through the one the entry shares (toolView.demoted).
// marker is the frame the block's rows lead with — the closing branch marker for an ordinary block,
// the deeper nested one inside a super-group's type row (superMemberMarker) — because those cells
// come off the same row budget the guard is measuring and a member indented one level further has
// that much less target to protect.
func guardPromotions(th theme, views []toolView, room int, marker string) []toolView {
	out, copied := views, false
	for i, tv := range views {
		if !guardRefuses(th, tv, room, marker) {
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
// The caller passes the CLOSING marker of the block's frame, and either of that frame's two markers
// would do: both are one glyph in the same cell count (branchMarker, superMemberMarker) — a member's
// row keeps the same budget wherever in the block it sits, which is what lets one answer settle a
// block.
func guardRefuses(th theme, tv toolView, room int, marker string) bool {
	if !tv.promotable() || tv.Target == "" {
		return false
	}
	avail := room - th.measure.Width(marker)
	tail := leaderGap + th.measure.Width(tv.Summary.Text)
	return avail-tail-leaderGap-leaderMinDots < promoteMinTargetCells
}

// leaderRow paints one call's branch row whole — the shape every single block and every group
// member takes (docs/layout/tool-layout.md): the branch marker, the call's target, a dotted leader,
// and the outcome slot flush against the right of room.
//
// It returns a painted row rather than text-and-a-style because the row is no longer one voice: the
// target speaks in the block's state tone, the leader in its own damped `tool-leader` role, and the
// outcome in the `tool-marker` role a failed verdict overrides in red (summaryStyle). A caller that
// wrapped this afterwards would be wrapping a styled string; nothing needs to, because the row is
// one row by construction — that is what the overflow order below buys.
//
// The order room is spent in IS design call 4, and it is the whole of the arithmetic: the outcome
// slot is reserved first and always prints whole, the leader then flexes down to leaderMinDots, and
// only then is the target cut, ending in " …" (clipCells). A row too narrow for even that drops the
// target outright rather than the outcome — what happened is the half worth keeping, and a row with
// nothing left to give still says it.
//
// expanded reaches the TONES (detailTone, summaryStyle) and, through what the caller hands as
// remainder, the count in the slot. It never reaches the GEOMETRY: the row is one row in both
// states, which is what lets the same click that opened a member close it without the row moving
// out from under the pointer. What an open row loses is the count itself — it hides nothing to
// count — and the cells that frees go back to the target the collapse had cut.
//
// remainder is the collapsed paint's "+N more lines" — empty whenever the paint hides nothing, and
// on every row of a shape that never hides body of its own (a group member, a type row). It rides
// the outcome slot rather than a row beneath it (slotText), which is what makes a collapsed lone
// call ONE row of block plus its header.
func leaderRow(th theme, tv toolView, marker string, room int, expanded bool, remainder string) string {
	tone := detailTone(th, expanded)
	left, paint := expandTabs(tv.Target), func(s string) string { return tone.Render(s) }
	if tv.finished && left != "" {
		// The mark rides the row's LEFT text so the leader arithmetic measures it exactly as it
		// measures the name it follows — a ✓ appended after the clip would push the row a cell past
		// its width and fold it onto a second line. Its own colour is put back on after the clip,
		// which is why the painter re-splits rather than the caller pre-styling: leaderRowIn hands
		// back the SURVIVING text, and on a row too narrow to seat the mark the split simply does
		// not match and the name paints in one tone, whole (toolView.finished).
		left += " " + glyphDone
		paint = func(s string) string {
			if name, ok := strings.CutSuffix(s, " "+glyphDone); ok {
				return tone.Render(name) + " " + th.successMark.Render(glyphDone)
			}
			return tone.Render(s)
		}
	}
	return leaderRowIn(th, left, paint, tv.Summary, marker, room, expanded, remainder)
}

// leaderRowIn is the leader arithmetic itself, with the row's LEFT content handed in rather than
// read off a call: the plain text it is measured and clipped by, and the painter that dresses
// whatever survives that clip. It exists because a super-group's TYPE ROW is the same row about a
// whole run — "Terminal (3)" where a call would put its path — and a second copy of the overflow
// order would part company with this one the first time either moved (renderSuperGroup).
//
// paint receives the CLIPPED text and not the original, because a painted string cannot be cut
// without cutting its escapes: the row measures plain text, decides what fits, and only then hands
// the survivor to whoever knows how it should look.
//
// remainder is the collapsed paint's count of what the block is hiding, joined onto the summary in
// the slot (affordableSlot) instead of standing on a row of its own. Once it is in, it reaches the
// ARITHMETIC as part of the slot — measured, reserved and, at the last resort, clipped with it — and
// the styling not at all: the whole slot takes one style off the summary alone (summaryStyle), so a
// failed call's red still governs the row's right edge whatever the count says.
func leaderRowIn(th theme, left string, paint func(string) string, summary branchSummary,
	marker string, room int, expanded bool, remainder string) string {
	avail := max(1, room-th.measure.Width(marker))
	slot := affordableSlot(th, left, summary.Text, remainder, avail)
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
		target, _ = clipCells(th, left, budget)
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
		lead = paint(target) + strings.Repeat(" ", leaderGap)
		leadCells = th.measure.Width(target) + leaderGap
	}
	dots := max(leaderMinDots, avail-leadCells-tail)
	row := paintRowMarker(th, marker, expanded) + lead +
		th.toolLeader.Render(strings.Repeat(glyphLeaderDot, dots))
	if slot != "" {
		row += strings.Repeat(" ", leaderGap) + summaryStyle(th, summary, expanded).Render(slot)
	}
	return row
}

// slotSeparator is what the outcome slot joins its two halves with — the middle dot the typed stats
// already speak in ("exit 0 · 1.2s", `· recursive` on a target: qualifiedTarget) — so a slot that
// gained a remainder count reads as one phrase in one voice rather than as two marks that happened
// to land in the same column (design call 4 of docs/plans/"2026-08-11 - 00").
const slotSeparator = " · "

// affordableSlot is the outcome slot's text at THIS width: the summary, and the remainder count
// joined onto it while the row can seat both and still identify itself — promoteMinTargetCells of
// target, or the whole of a target shorter than that floor.
//
// The count is the row's FIRST concession, ahead of the dots and the target both, because it is the
// least of what the row has to say: the ▶ at its edge already announces that something is hidden,
// and the block opens onto the lines themselves. A row that gave up its path or its command to print
// a count of what it is not showing would be a row about nothing — the very shape the promote-guard
// refuses a one-line output (guardRefuses, design call 5), and refusing it here keeps that guard's
// promise on the demoted row, whose body is exactly what this count counts.
//
// The floor is measured against the arithmetic below, in the same terms: the slot and its gap
// reserved first, then the gap and the dot the leader may not go below.
func affordableSlot(th theme, left, summary, remainder string, avail int) string {
	if remainder == "" {
		return summary
	}
	joined := slotText(summary, remainder)
	floor := min(th.measure.Width(left), promoteMinTargetCells)
	if avail-leaderGap-th.measure.Width(joined)-leaderGap-leaderMinDots < floor {
		return summary
	}
	return joined
}

// noRemainder is what a row with nothing to count hands the leader row for its slot's tail: a group
// member and a super-group's type row, neither of which hides body of its own on the row it paints
// (renderGroupMember, renderSuperGroup), and any row of an OPEN block, which hides nothing at all.
// It is named rather than left a bare "" at those call sites, where an empty string says nothing
// about which of the row's several strings it is.
const noRemainder = ""

// slotText is the outcome slot's whole text: what the call came to, and — on a collapsed paint that
// hides body — the count of what it is hiding, as "12 lines · +5 more lines". The count rides the
// slot rather than a row beneath it, so a collapsed lone call spends its header and ONE row and a
// scrollback of them reads as a list (ISSUES.md, 2026-08-11).
//
// Either half alone is that half alone, with no separator to trail or to open on: a call still in
// flight has no summary yet and a block that hides nothing has no count, and both are ordinary.
func slotText(summary, remainder string) string {
	switch {
	case remainder == "":
		return summary
	case summary == "":
		return remainder
	}
	return summary + slotSeparator + remainder
}

// toolRowCells is the room a branch or member row lays itself out in: the block's width less the
// field the ▶/▼ is held in at its right edge (groupIndicatorCells). The field is reserved on every
// row, one wearing an indicator or not, so the outcome slots line up down the block's edge whatever
// each row has to reveal — and so a row does not move sideways the moment it gains something.
func toolRowCells(th theme, width int) int {
	return max(1, width-groupIndicatorCells(th))
}

// summaryStyle is the tone the outcome slot takes: the `tool-marker` role, one step up once the
// block is open, with a summary that says the call FAILED overriding both in red — design call 11
// makes that red the only failure marking, so no glyph and no header changes with it.
//
// It is the MARKER role and not the two-tone detail gray the rest of the row wears (design call 2 of
// docs/plans/"2026-08-11 - 00"). The slot is apogee's reading of what the call came to rather than a
// line the tool printed — "12 lines", "exit 0 · 1.2s", the quoted line a promotion lifted out of the
// body — and it is the very role the "+N more lines" count it now carries has always worn
// (slotText), never the tone of the output it is summarising. Under `dark` the old tone was the very
// hex the leader dots run in, which left the one part of the row that says what HAPPENED as quiet as
// the filler pointing at it.
//
// The verdict is the summary's OWN ([branchSummary.failed]) and never a reading of the joined slot
// or of the text in it: a remainder count appended to "denied" must not be able to talk the row out
// of its red, and a line the TOOL printed must not be able to talk a clean call into one (F-29).
//
// Every kind takes it, the promoted and quoted ones included: the slot's colour answers for the
// slot, and a summary that changed voice with the kind of thing it summarises would make the row's
// right edge a second, quieter place to read a body line from.
func summaryStyle(th theme, s branchSummary, expanded bool) lipgloss.Style {
	slot := th.toolMarker
	if expanded {
		slot = th.toolMarkerBright
	}
	if s.failed {
		return th.errorText
	}
	return slot
}

// failedSummary reads a WORDING for a verdict of failure (errorSummaryPrefix and the three bare
// verdicts beside it), plus the count a TYPE ROW aggregates its run's failures into ("3 errors",
// runAggregate).
//
// The text is read ONCE, at the seam that words it: namedSummary puts its own sentence to this
// vocabulary on the way in and the answer rides the summary from there ([branchSummary.failed]), so
// no painter and no counter asks the words again. The one other caller is subAgentSummary, which
// composes a reading of a run whose verdict lives on the HEAD's summary and carries that verdict
// onto the composed line rather than leaving it to be re-derived from it.
//
// The aggregate is worded by the house plural (plural, runAggregate) and read back here by
// countPhrase's strict split of that same "<n> <word>" shape, so the two cannot come to disagree,
// and only a count of one or more reads as a failure — "0 errors" is a clean run, and no aggregate
// says it anyway.
//
// interruptedSummary is read here for the same reason the other two are: it is a verdict about a
// call that produced no outcome, and the ONE place that answer has to reach is subAgentFinished — a
// delegation closed at replay must not wear the done ✓ its row would otherwise earn from being done.
func failedSummary(text string) bool {
	if strings.HasPrefix(text, errorSummaryPrefix) ||
		text == deniedSummary || text == cancelledSummary || text == interruptedSummary {
		return true
	}
	n, noun, ok := countPhrase(text)
	return ok && n > 0 && strings.TrimSuffix(noun, "s") == errorNoun
}

// clipCells fits text into ONE row of at most cells columns, ending it in clipTail when it had to
// cut, and reports the cut. It is clipWrap's arithmetic with no marker and no style — the same
// hangingPrefixes wrap and the same fitted tail — for the caller that has to keep composing after
// the clip instead of painting what comes back (leaderRow, which seats a clipped target and a
// clipped outcome slot in a row it goes on to assemble).
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
	groupIndicatorGap = 1
	groupCountFormat  = "(%d)"
)

// groupIndicatorCells is how many columns a member row gives up at its right edge to the indicator
// field: the gap plus the widest of the two glyphs it may show. Both are measured because the field
// must not change width when a member opens (▶ → ▼, item 5) — a member whose text re-wrapped on
// being expanded would move under the very click that expanded it.
func groupIndicatorCells(th theme) int {
	return groupIndicatorGap + max(th.measure.Width(glyphCollapsed), th.measure.Width(glyphExpanded))
}

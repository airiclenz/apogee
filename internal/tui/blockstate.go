package tui

// blockState is what a tool block's painter is told beyond the views themselves: which of the two
// paints to draw, and whether the collapsed one hides anything the views cannot account for.
//
// elides is the sub-agent run's case, and today its only one. A collapsed run's whole span — every
// inner block, every rail, every spacer among them — is left unpainted by [transcript.renderView]
// before the head block ever reaches the painter, so nothing among the views records that there is
// something behind the header. The toggle-target rule has to know anyway: layout.md makes a run
// with a span clickable however short its own report is. Like the mark itself it is
// state-INDEPENDENT — an expanded run sets it too, which is what leaves the header clickable so the
// same click closes it.
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
// marker replaces the branch marker the SINGLE shape's row would lead with — the ┌─┶ a lone
// delegation opened its frame with (design call 3 of docs/plans/"2026-08-11 - 01") until ADR 0063
// left a run its collapsed row and its view, since when no caller names one. Its ZERO VALUE is the
// ┝/┕ of the tree, so every block keeps the marker it always drew without saying so. It reaches the single shape alone: a group's markers are
// the LIST's — which row closes it is the list's own arithmetic — and a caller cannot hand one in.
type blockState struct {
	expanded bool
	elides   bool
	live     bool
	blink    bool
	glyph    string
	marker   string
}

// branchMarkerIn is the marker the single shape's n'th of total rows leads with: the frame the
// caller named, or the tree's own ┝/┕ where it named none ([blockState.marker]). It is a method so
// the override is read in ONE place — the painter asks for a marker and gets whichever one this
// block is framed in, rather than each row site remembering to check a field.
func (s blockState) branchMarkerIn(n, total int) string {
	if s.marker != "" {
		return s.marker
	}
	return branchMarker(n == total-1)
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

// stateIndicator is the glyph a TOGGLEABLE block wears — at its leader row's right edge, past the
// outcome slot (indicatorRow), or trailing the header's label in the targetless shape, which paints
// no leader row for it to sit at the edge of: ▼ for an expanded block, ▶ for a collapsed one
// (docs/layout/tool-layout.md, "Fold states and interaction"). It answers for the state alone —
// whether a block
// wears one at all is the toggle-target rule's, asked once in renderToolBlock — so the two questions
// stay one condition and one glyph apart.
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
func anyOpenCall(ins []paintInput) bool {
	for i := range ins {
		if ins[i].kind == entryToolCall && !ins[i].done {
			return true
		}
	}
	return false
}

// memberFlags is a grouped block's per-member view state, read off the run's own paint inputs in
// view order ([toolRunView.members]). It is a copy rather than the entries themselves because a
// painter is handed what it needs to draw and nothing it could write through: the flag is owned by
// the shared entries backing array and moved only by transcript.setExpanded (ADR 0011) — the very
// rule [paintInput] now states for every field at once.
func memberFlags(ins []paintInput) []bool {
	flags := make([]bool, len(ins))
	for i := range ins {
		flags[i] = ins[i].expanded
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
// A DELEGATION is the one shape whose hidden half is not among the lines at all: what its block
// opens onto is the prompt it carried, which the collapsed row never paints (subAgentHidesPrompt).
// That question is asked FIRST and on its own, because the body-counting rules below answer for a
// never-ran delegation by the promote-guard's leave — a refusal narrow enough to be demoted lands a
// body the count can see, the same refusal promoted at a wider terminal lands none — and an
// indicator that appears and disappears with the columns is one no reader can learn.
//
// It is the SINGLE block's rule: a group's header toggles nothing and asks nothing here, its members
// each wearing an indicator of their own under this same question asked of one call
// (renderGroupMember). The slice stays a slice because the question is about a block's views rather
// than about one of them, and item 5's per-member state is where that distinction is spent.
func blockHidesWhenCollapsed(th theme, views []toolView, width int) bool {
	for _, tv := range views {
		if subAgentHidesPrompt(tv) {
			return true
		}
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

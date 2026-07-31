package tui

import (
	"strings"
	"unicode"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
)

// ----------------------------------------------------------------------------
// Inline token accents — the prompt's tokens light up when they RESOLVE
// ----------------------------------------------------------------------------
//
// The mini-language is typed as ordinary prose, so nothing in the box says which words of a draft
// the machine will act on. This is that answer, and it is deliberately a narrow one: a "/token"
// takes the skill accent exactly when it names a catalog id, an "@token" takes the file accent
// exactly when its path is in the workspace listing, and everything else — prose, a path like
// /usr/bin, a mistyped id, a file that does not exist — stays plain. The styling therefore doubles
// as live validation: a typo visibly fails to light up while it is being typed, which is the
// cheapest possible correction loop and needs no note, no popup, and no key.
//
// It is a POST-RENDER pass over the textarea's own rendered block (accentTokens), composed inside
// [Model.inputView] before the drag-selection overlay so the selection strips and re-wins over any
// accent it covers (shadeCells, mouse.go). The widget draws the text; this only re-styles cells the
// widget already drew, which is why no copy of its content is needed here — only a mirror of where
// it WRAPPED it (wrapRowStarts), so a token that straddles a soft-wrap is painted on both rows.
//
// Nothing here may touch the disk: View runs on every frame. The "@" half asks the file cache what
// it already HOLDS (fileCache.holds) and never asks it to walk, so a cold or foreign-rooted cache
// renders the token plain and the accent appears once the "@" overlay's next lookup has warmed the
// listing.

// accentKind names which of the two token shapes an accent paints, and so which theme style it
// wears. It is not a property of the text — the same word is one kind or plain prose depending on
// whether it resolves — which is why the spans are re-derived per frame rather than stored.
type accentKind int

const (
	accentSkill accentKind = iota // a "/id" token naming a catalog skill
	accentFile                    // an "@path" token naming a file the workspace listing holds
)

// accentSpan is one resolving token as a byte range into the prompt value, with the kind that
// decides its colour.
type accentSpan struct {
	start, end int
	kind       accentKind
}

// inputCellSpan is one row-slice of a painted token: an ABSOLUTE visual row of the textarea's
// content (counted from the top of the value, so the caller subtracts ScrollYOffset to reach a
// screen row — the highlightInput posture) and the display-cell range [c0,c1) the token covers on
// it. A token confined to one row yields one of these; a token straddling a soft-wrap yields one
// per row it spans.
type inputCellSpan struct{ row, c0, c1 int }

// resolvingTokens locates the prompt tokens that resolve RIGHT NOW, in the order they will be
// painted. Both halves read the same predicates the rest of the package does — [Model.knownSkillID]
// for the catalog, [Model.knownWorkspaceFile] for the listing — so what lights up is exactly what
// the parse layer would recognise, and a token left plain is one that would travel as prose.
func (m Model) resolvingTokens() []accentSpan {
	value := m.input.Value()
	if value == "" {
		return nil
	}
	var spans []accentSpan
	for _, sp := range skillRefSpans(value, m.knownSkillID) {
		spans = append(spans, accentSpan{start: sp.start, end: sp.end, kind: accentSkill})
	}
	for _, sp := range fileRefSpans(value) {
		if m.knownWorkspaceFile(sp.name) {
			spans = append(spans, accentSpan{start: sp.start, end: sp.end, kind: accentFile})
		}
	}
	return spans
}

// knownWorkspaceFile reports whether path names a file the workspace listing already holds. It is
// the file half's resolve test, and it is deliberately the CACHE's answer rather than the disk's:
// the render path may not walk (fileCache.holds), so an unwarmed listing renders the token plain
// and self-heals on the next "@" lookup instead of stalling a frame on a filesystem walk.
func (m Model) knownWorkspaceFile(path string) bool {
	return m.files.holds(m.opts.Workspace, path)
}

// accentTokens overlays the resolving tokens' accents on the textarea's rendered block, working
// purely in visual-cell space like [Model.highlightInput]: each token's byte range becomes the
// row-slices the widget drew it on (inputCellSpans), ScrollYOffset maps those absolute rows onto
// the visible ones, and shadeCells re-renders exactly those cells under the token's style. Rows
// scrolled out of view are skipped, so a draft taller than the box costs only the spans it shows.
//
// Overlapping accents cannot occur (the two grammars claim disjoint tokens), and two accents on one
// row compose: shadeCells slices by display cell with ansi.Cut, which leaves the flanking parts —
// the first accent among them — exactly as they were.
func (m Model) accentTokens(view string) string {
	spans := m.resolvingTokens()
	if len(spans) == 0 {
		return view
	}
	value := m.input.Value()
	width := m.input.Width() // the widget's own text width: what it wrapped by, so what to mirror
	scroll := m.input.ScrollYOffset()
	lines := strings.Split(view, "\n")
	for _, sp := range spans {
		style := m.th.skillToken
		if sp.kind == accentFile {
			style = m.th.fileToken
		}
		from, to := runeOffsetOf(value, sp.start), runeOffsetOf(value, sp.end)
		for _, cs := range inputCellSpans(value, width, from, to) {
			r := cs.row - scroll
			if r < 0 || r >= len(lines) {
				continue // above or below the rows the box currently shows
			}
			w := lipgloss.Width(lines[r])
			c0 := clampInt(cs.c0, 0, w)
			c1 := clampInt(cs.c1, c0, w)
			if c1 <= c0 {
				continue
			}
			// The slice is in the painter's measure (shadeCells, width.go) because the cells
			// being re-styled are cells the terminal already painted. The COLUMNS reaching it
			// still come from the widget mirror below (runesWidth), which measures per rune —
			// the mirror's own oracle is the widget, and reconciling the two is the caret
			// mirrors' item, not this pass's.
			lines[r] = shadeCells(m.th.measure, lines[r], c0, c1, style)
		}
	}
	return strings.Join(lines, "\n")
}

// inputCellSpans maps a rune range of value onto the visual rows and display cells the textarea
// draws it at, at the widget's text width. It is [offsetToLineCol] carried the rest of the way: that
// one names the LOGICAL row and column, this one folds in the soft-wrap the widget applied to that
// logical line (wrapRowStarts) to reach the visual geometry the rendered block is addressed by.
//
// A token never crosses a newline — both grammars break on one (isInputSpace includes '\n', and a
// quoted @ref stops at it, scanRefToken) — so a range spanning two logical lines is not a token this
// pass drew and yields nothing rather than a guess. Within its line the range may still straddle any
// number of soft-wraps, and it yields one span per row it touches.
func inputCellSpans(value string, width, from, to int) []inputCellSpan {
	if to <= from || width < 1 {
		return nil
	}
	row, col0 := offsetToLineCol(value, from)
	endRow, col1 := offsetToLineCol(value, to)
	lines := strings.Split(value, "\n")
	if row != endRow || row >= len(lines) {
		return nil
	}
	base := 0
	for i := 0; i < row; i++ {
		base += len(wrapRowStarts([]rune(lines[i]), width))
	}
	runes := []rune(lines[row])
	starts := wrapRowStarts(runes, width)
	var out []inputCellSpan
	for k, s := range starts {
		e := len(runes)
		if k+1 < len(starts) {
			e = starts[k+1]
		}
		lo, hi := max(col0, s), min(col1, e)
		if lo >= hi {
			continue
		}
		c0 := runesWidth(runes[s:lo]) // lo < hi <= len(runes), so the row start is in range too
		out = append(out, inputCellSpan{row: base + k, c0: c0, c1: c0 + runesWidth(runes[lo:hi])})
	}
	return out
}

// wrapRowStarts reports the rune offset each visual (soft-wrapped) row of ONE logical line begins
// at, at the textarea's text width — so len(starts) is that line's row count and starts[k] anchors
// its k-th row. It mirrors the bubbles textarea's own `wrap`, rune for rune, because the accents are
// painted onto cells that widget already drew: a mirror that merely wrapped "correctly" would
// misplace them wherever the two disagreed. TestWrapRowStartsMirrorsTheWidget pins it against a real
// textarea's LineInfo at every column, so a change to the widget's wrap fails here rather than
// silently sliding the accents sideways.
//
// The widget's rule, in its own terms: text accumulates as WORD + trailing SPACES groups, and a
// group that would overflow the row opens a new one carrying the whole group (which is why a run of
// spaces can land alone on a row). A word too wide for any row is broken where it stands — and the
// widget compares its width against the row counting the word's last rune twice, an off-by-one
// mirrored here deliberately, since matching the split is the whole point. Finally a line whose
// content REACHES the width gains one trailing row, the seat the widget keeps for a caret past a
// full line (the same rule inputContentRows sizes the box by).
func wrapRowStarts(line []rune, width int) []int {
	if width < 1 {
		width = 1
	}
	starts := []int{0}
	rowWidth := 0              // display width of the runes already placed on the current row
	consumed := 0              // runes of line already placed on a row
	wordLen, wordWidth := 0, 0 // the pending word: a run of non-space runes
	spaces := 0                // the whitespace run trailing that word
	for _, r := range line {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			wordLen++
			wordWidth += runewidth.RuneWidth(r)
		}
		switch {
		case spaces > 0: // the group is finished: place it, on a new row if it does not fit
			if rowWidth+wordWidth+spaces > width {
				starts = append(starts, consumed)
				rowWidth = 0
			}
			rowWidth += wordWidth + spaces
			consumed += wordLen + spaces
			wordLen, wordWidth, spaces = 0, 0, 0
		case wordWidth+runewidth.RuneWidth(r) > width: // a word wider than a row: break it here
			if consumed > starts[len(starts)-1] { // the current row already holds something
				starts = append(starts, consumed)
				rowWidth = 0
			}
			rowWidth += wordWidth
			consumed += wordLen
			wordLen, wordWidth = 0, 0
		}
	}
	if rowWidth+wordWidth+spaces >= width {
		starts = append(starts, consumed) // the trailing row a width-filling line keeps for the caret
	}
	return starts
}

// runesWidth is the display width of a rune slice, accumulated per rune exactly as
// [cellToRuneOffset] inverts it — the two are the forward and backward halves of one cell↔rune
// mapping, so a token's start cell and its width are measured by the same ruler the caret is.
func runesWidth(rs []rune) int {
	w := 0
	for _, r := range rs {
		w += runewidth.RuneWidth(r)
	}
	return w
}

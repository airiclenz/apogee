package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
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
// row compose: shadeCells slices by display cell through the width authority, which leaves the
// flanking parts — the first accent among them — exactly as they were.
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
		for _, cs := range inputCellSpans(m.th.measure, value, width, from, to) {
			r := cs.row - scroll
			if r < 0 || r >= len(lines) {
				continue // above or below the rows the box currently shows
			}
			// The clamp, the columns and the cut are all the PAINTER's measure (width.go): the
			// cells being re-styled are cells the terminal already painted, so a bound or a
			// slice in any other measure names a different run of cells than the one on screen.
			// Only the ROW came from the widget's own wrap (inputCellSpans → wrapRowStarts), and
			// that split is the whole rule for a widget mirror: WHICH runes the widget put on a
			// row is the widget's answer, WHERE those runes then land is the painter's.
			w := m.th.measure.Width(lines[r])
			c0 := clampInt(cs.c0, 0, w)
			c1 := clampInt(cs.c1, c0, w)
			if c1 <= c0 {
				continue
			}
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
// The two halves answer to two different oracles, deliberately. The ROWS are the widget's: only it
// decides which runes it put on which line, so they are mirrored from its own wrap. The COLUMNS are
// the painter's — measure, the width authority (width.go) — because they address cells on a row the
// terminal has already drawn, which is what the caller then re-styles.
//
// A token never crosses a newline — both grammars break on one (isInputSpace includes '\n', and a
// quoted @ref stops at it, scanRefToken) — so a range spanning two logical lines is not a token this
// pass drew and yields nothing rather than a guess. Within its line the range may still straddle any
// number of soft-wraps, and it yields one span per row it touches.
func inputCellSpans(measure widthAuthority, value string, width, from, to int) []inputCellSpan {
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
		c0 := measure.Width(string(runes[s:lo])) // lo < hi <= len(runes), so the row start is in range too
		out = append(out, inputCellSpan{row: base + k, c0: c0, c1: c0 + measure.Width(string(runes[lo:hi]))})
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
// Its ORACLE is therefore the widget, never the width authority (width.go): the authority follows
// the painter, and the painter has no vote in where a third-party widget decided to break a line.
// So every measure here is runesWidth — uniseg, the widget's own — and it stays that way whatever
// the authority is on. The one exception is the last-rune term of the hard-break test below, where
// the widget itself reaches for go-runewidth; mirroring that means spelling it the same way.
//
// The widget's rule, in its own terms: text accumulates as WORD + trailing SPACES groups, and a
// group that would overflow the row opens a new one carrying the whole group (which is why a run of
// spaces can land alone on a row). A word too wide for any row is broken where it stands — and the
// widget compares its width against the row counting the word's last rune twice, an off-by-one
// mirrored here deliberately, since matching the split is the whole point. Finally a line whose
// content REACHES the width gains one trailing row, the seat the widget keeps for a caret past a
// full line.
//
// Its row COUNT — len(starts) — is what sizes the prompt box: inputContentRows (render.go) is the
// sum of this over the value's logical lines, which is the widget's own decomposition
// (totalVisualLines). So the box's height and the rows an accent lands on come off one ruler.
//
// The row and the pending word are re-measured from the line rather than accumulated per rune,
// which is not a detail: a grapheme cluster measures as a whole, so summing its runes one at a time
// would under-count exactly the sequences (an emoji carrying VARIATION SELECTOR-16) the widget
// counts as two cells. The two slices measured are the widget's own operands — the runes already on
// the row, and the pending word — so the mirror weighs the same text at the same moments it does.
//
// The line is sanitised before any of that runs (sanitizeInputLine), because the widget sanitises on
// the way IN: every write path passes its runes through one sanitizer, so a tab arrives as four
// spaces and a control rune or a utf8.RuneError arrives not at all. Measuring the raw rune instead
// weighs a tab as a single space-like column, and a rune the widget dropped as a column it never
// drew, and wraps such a line where the widget does not. The offsets returned are therefore offsets
// into the line AS THE WIDGET HOLDS IT — post-sanitising — which for every caller in the package is
// the line handed in, since what they hand over is the widget's own already-sanitised value
// (runesWidth, cellToRuneOffset in mouse.go).
func wrapRowStarts(line []rune, width int) []int {
	if width < 1 {
		width = 1
	}
	line = sanitizeInputLine(line)
	starts := []int{0}
	consumed := 0 // runes of line already placed on a row
	wordLen := 0  // the pending word: a run of non-space runes
	spaces := 0   // the whitespace run trailing that word
	for _, r := range line {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			wordLen++
		}
		word := line[consumed : consumed+wordLen] // the widget's `word`, r included
		switch {
		case spaces > 0: // the group is finished: place it, on a new row if it does not fit
			row := line[starts[len(starts)-1]:consumed] // the widget's `lines[row]`
			if runesWidth(row)+runesWidth(word)+spaces > width {
				starts = append(starts, consumed)
			}
			consumed += wordLen + spaces
			wordLen, spaces = 0, 0
		case runesWidth(word)+runewidth.RuneWidth(r) > width: // a word wider than a row: break it here
			if consumed > starts[len(starts)-1] { // the current row already holds something
				starts = append(starts, consumed)
			}
			consumed += wordLen
			wordLen = 0
		}
	}
	row, word := line[starts[len(starts)-1]:consumed], line[consumed:consumed+wordLen]
	if runesWidth(row)+runesWidth(word)+spaces >= width {
		starts = append(starts, consumed) // the trailing row a width-filling line keeps for the caret
	}
	return starts
}

// runesWidth is the display width of a rune run measured the way the textarea measures its own
// content — uniseg.StringWidth over the whole run, so a grapheme cluster counts once, as the cells
// the widget believes it drew. It is the forward half of the cell↔rune mapping [cellToRuneOffset]
// inverts, so a token's start cell and the caret's column are read off the same ruler, and it is
// that widget's ruler rather than the painter's: both are mirrors of its internal math.
//
// It measures no TABs and needs none of the tab arithmetic the transcript side carries: everything
// weighed here comes from the textarea's own value, which the widget sanitises tabs out of on the
// way in — see [cellToRuneOffset] (mouse.go) for why that holds on every write path — and a line
// reaching [wrapRowStarts] from anywhere else has been through the same sanitising first
// (sanitizeInputLine).
func runesWidth(rs []rune) int {
	return uniseg.StringWidth(string(rs))
}

// inputTabCells is how many spaces one TAB becomes on its way into the textarea: the widget
// sanitises every write with runeutil.NewSanitizer's defaults, and that sanitizer rewrites '\t' as
// four spaces flat — not to the next tab stop (bubbles/v2@v2.1.0/internal/runeutil/runeutil.go:26,
// textarea.san). It is deliberately its own constant rather than [tabCells] (wrap.go), which is the
// same number today for an unrelated reason: that one mirrors lipgloss's tab width because the
// PAINTER will apply it, this one mirrors the widget's sanitizer because the WIDGET applied it. A
// mirror answers to the widget alone (ADR 0030 §6), so if the two ever diverge each must follow its
// own oracle.
const inputTabCells = 4

// sanitizeInputLine rewrites line the way the textarea rewrote everything ever written into it, so a
// line is measured as the widget HOLDS it rather than as it was handed over. The widget's rule is
// runeutil.NewSanitizer's default (bubbles/v2@v2.1.0/internal/runeutil/runeutil.go:26-29, :56-95),
// and this is the whole of it per line: a utf8.RuneError is dropped, a TAB becomes [inputTabCells]
// spaces, every other control rune is dropped, and anything else is kept. Mirroring only the tab
// leaves the mirror one rune out of step with the widget for every rune it drops: the offsets
// returned index the value the widget HOLDS, so an accent past such a rune is seated on the wrong
// run of cells — and a utf8.RuneError, one cell wide to the ruler and absent from the widget, moves
// the wrap itself.
//
// '\r' and '\n' are the sanitizer's remaining case (each becomes one '\n') and are deliberately not
// handled here, because neither can reach a LINE: the widget sanitises BEFORE it splits its input
// into logical rows (bubbles/v2@v2.1.0/textarea/textarea.go:504, :519-529), so a '\r' has already
// become a row boundary rather than a rune inside a row, and the callers split the value on that
// boundary before they get here — [inputCellSpans] on '\n', which is all the widget's own value can
// carry, and inputContentRows (chromelayout.go) on either, since a value handed to it need not have
// come from the widget at all. That the widget's value is
// sanitised on every write path at all is argued once, from the caret's side, at [cellToRuneOffset]
// (mouse.go).
//
// A line the sanitizer would leave alone is returned as-is, unallocated: that is every line the
// package itself measures — the widget's value has already been through this — so the frame path
// pays nothing for a case only an outside caller can reach.
func sanitizeInputLine(line []rune) []rune {
	tabs, dropped := 0, 0
	for _, r := range line {
		switch {
		case r == '\t':
			tabs++
		case sanitizerDropsRune(r):
			dropped++
		}
	}
	if tabs == 0 && dropped == 0 {
		return line
	}
	out := make([]rune, 0, len(line)+tabs*(inputTabCells-1)-dropped)
	for _, r := range line {
		switch {
		case r == '\t':
			for i := 0; i < inputTabCells; i++ {
				out = append(out, ' ')
			}
		case sanitizerDropsRune(r):
			// Kept by neither the widget nor the mirror.
		default:
			out = append(out, r)
		}
	}
	return out
}

// sanitizerDropsRune reports whether the textarea's sanitizer drops r outright instead of keeping or
// rewriting it: utf8.RuneError, and every control rune it has no replacement for — which is all of
// them but '\t' (four spaces) and '\r'/'\n' (a row boundary, never a rune within a line — see
// [sanitizeInputLine]).
func sanitizerDropsRune(r rune) bool {
	return r == utf8.RuneError || (r != '\t' && unicode.IsControl(r))
}

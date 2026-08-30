package tui

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/charmbracelet/x/ansi"
)

// railedWidth floors a deeply-nested block's usable width at one column so the wrapper never
// divides by zero, even when the rail gutters consume more than the whole terminal width.
func TestRailedWidthFloors(t *testing.T) {
	if got := railedWidth(80, 0); got != 80 {
		t.Errorf("railedWidth(80, 0) = %d; want 80 (depth 0 takes no gutter)", got)
	}
	if got := railedWidth(3, 5); got != 1 {
		t.Errorf("railedWidth(3, 5) = %d; want 1 (floored, not negative)", got)
	}
}

// ----------------------------------------------------------------------------
// The absolute width cap (layout.md:159-160)
// ----------------------------------------------------------------------------

// wrapText holds the cap itself rather than trusting the wrap it delegates to. x/ansi's
// breakpoint branch (x/ansi@v0.11.7/wrap.go:406-419) has neither of the already-full-line checks
// its default branch has, so a run of the wrapper's own breakpoints kept growing a word onto a
// full line: ansi.Wrap("| --- | --- | --- |", 3, "") returned a five-cell first line, and the
// same input at limit 8 an eleven-cell one. That input is not exotic — a markdown delimiter row
// reaches the transcript verbatim whenever a table is too narrow to lay out, and any hyphenated
// word carries the same run in miniature.
//
// The cap is measured in the width authority's measure, since that is the measure the frame is
// painted in, so the whole table runs under both of them.
func TestWrapTextHoldsTheWidthCap(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in string }{
		{"delimiter row", "| --- | --- | --- |"},
		{"hyphen run", "----"},
		{"pipe row", "| alpha | beta | gamma |"},
		{"hyphenated prose", "a deeply nested sub-agent message that must wrap hard"},
		{"mixed pipe and hyphen runs", "|-|--|---|-- a-b-c |"},
		{"leading indent", "    indented -- text"},
		{"cjk", "日本語のテキスト"},
		{"vs16", "a " + vs16Warning + " b " + strings.Repeat(vs16Warning, 3) + " c"},
		{"cjk and vs16", "警告 " + vs16Warning + " 日本語-テキスト | " + vs16Warning},
		{"empty", ""},
	}

	for _, pm := range paintMethods {
		for _, c := range cases {
			for limit := 1; limit <= 8; limit++ {
				t.Run(pm.name+"/"+c.name+"/limit "+strconv.Itoa(limit), func(t *testing.T) {
					t.Parallel()
					th := newTheme(scheme.Default())
					th.measure = widthAuthority{method: pm.method}

					lines := wrapText(th, c.in, limit)
					if len(lines) == 0 {
						t.Fatalf("wrapText returned no lines at all")
					}
					for i, ln := range lines {
						w := th.measure.Width(ln)
						if w <= limit {
							continue
						}
						// The one thing a break cannot divide is a single grapheme wider than the
						// limit — a CJK glyph at limit 1 — and it gets a line to itself.
						if cluster, _ := ansi.FirstGraphemeCluster(ln, pm.method); cluster == ln {
							continue
						}
						t.Errorf("line %d %q is %d cells wide, over the %d cap: %q", i, ln, w, limit, lines)
					}
					// Capping moves where the lines break and nothing else: every non-space
					// character the wrap produced is still there, in order. (A break may swallow
					// the space it happens at, which is the wrapper's own behaviour.)
					capped := withoutSpaces(strings.Join(lines, ""))
					uncapped := withoutSpaces(strings.ReplaceAll(ansi.Wrap(c.in, limit, ""), "\n", ""))
					if capped != uncapped {
						t.Errorf("capping changed the content to %q, want the wrap's own %q", capped, uncapped)
					}
				})
			}
		}
	}
}

// withoutSpaces drops every space from s — the part of a wrapped line that must survive a
// re-break, since a break may consume the space it falls on.
func withoutSpaces(s string) string { return strings.ReplaceAll(s, " ", "") }

// ----------------------------------------------------------------------------
// The wrap is taken in the painter's measure (ADR 0030)
// ----------------------------------------------------------------------------

// Enforcing the cap in the authority's measure is only half of it: the BREAK has to be chosen in
// that measure too, or the wrapper is deciding where lines end by a ruler the terminal is not
// using. wrapText used to call the package-level ansi.Wrap, which is hard-wired to
// ansi.GraphemeWidth whatever the painter does — the last site ADR 0030 left unconverted. On the
// painter's DEFAULT WcWidth that reads a VARIATION SELECTOR-16 cluster as two cells where the
// terminal paints one, so the wrap broke a cell early on every line carrying one and the surface
// came out shorter than the width it was given.
//
// Under GraphemeWidth the authority's wrap IS ansi.Wrap, so the grapheme half of this table also
// pins that the conversion changed nothing where the two measures agree; only the WcWidth half
// fails on the pre-fix code.
func TestWrapTextBreaksInThePaintersMeasure(t *testing.T) {
	t.Parallel()

	warns := vs16Warning + " " + vs16Warning + " " + vs16Warning

	cases := []struct {
		name         string
		in           string
		limit        int
		wc, grapheme []string
	}{
		{
			// Five painted cells at limit 5 — one line the painter fills exactly. GraphemeWidth
			// calls the same string eight cells and breaks it.
			name:     "VS16 clusters that fit the painted limit",
			in:       warns,
			limit:    5,
			wc:       []string{warns},
			grapheme: []string{vs16Warning + " " + vs16Warning, vs16Warning},
		},
		{
			name:     "VS16 clusters that fit a narrow painted limit",
			in:       vs16Warning + " " + vs16Warning,
			limit:    3,
			wc:       []string{vs16Warning + " " + vs16Warning},
			grapheme: []string{vs16Warning, vs16Warning},
		},
		{
			// The wrap only moves where the measures disagree: plain prose breaks identically.
			name:     "ascii prose the measures agree about",
			in:       "the quick brown fox jumps",
			limit:    10,
			wc:       []string{"the quick", "brown fox", "jumps"},
			grapheme: []string{"the quick", "brown fox", "jumps"},
		},
		{
			// Wide glyphs are two cells to BOTH measures, so they break the same way as well.
			name:     "cjk the measures agree about",
			in:       "日本語のテキスト",
			limit:    6,
			wc:       []string{"日本語", "のテキ", "スト"},
			grapheme: []string{"日本語", "のテキ", "スト"},
		},
	}

	for _, pm := range paintMethods {
		for _, c := range cases {
			t.Run(pm.name+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				th := newTheme(scheme.Default())
				th.measure = widthAuthority{method: pm.method}

				want := c.wc
				if pm.method == ansi.GraphemeWidth {
					want = c.grapheme
				}
				if got := wrapText(th, c.in, c.limit); !reflect.DeepEqual(got, want) {
					t.Errorf("wrapText(%q, %d) = %q, want %q", c.in, c.limit, got, want)
				}
			})
		}
	}
}

// ----------------------------------------------------------------------------
// The hang a block cannot hold (hangingPrefixes)
// ----------------------------------------------------------------------------

// A marker is SHED WHOLE by a block that cannot seat it beside a column of text. At block width 1–2
// a two-column bullet used to be prepended to a wrap floored at one column, composing a three-cell
// line inside a two-cell block — layout.md's absolute width cap broken by the very glyph meant to
// decorate what it capped. The rule is the ladder a pane title already spends its width by: the
// mark that no longer fits is dropped, never squeezed, and the words keep the block.
//
// The sweep runs the whole narrow range on both painting measures, since the cap is enforced in the
// measure the frame is painted in (ADR 0030), and asserts the hang in BOTH directions — gone below
// the threshold, untouched at and above it — so the collapse cannot pass by quietly shedding the
// marker at ordinary widths too.
func TestHangingWrapCollapsesTheHangItCannotHold(t *testing.T) {
	t.Parallel()

	const marker = "• " // two columns, the shape a markdown list hangs under
	const text = "alpha beta gamma delta"

	for _, pm := range paintMethods {
		for width := 0; width <= 6; width++ {
			t.Run(pm.name+"/width "+strconv.Itoa(width), func(t *testing.T) {
				t.Parallel()
				th := newTheme(scheme.Default())
				th.measure = widthAuthority{method: pm.method}
				mw := th.measure.Width(marker)

				lines := hangingWrap(th, th.toolDetail, marker, text, width)
				if len(lines) == 0 {
					t.Fatalf("width %d: hangingWrap returned no lines at all", width)
				}
				for i, ln := range lines {
					if w := th.measure.Width(ln); w > max(width, 1) {
						t.Errorf("width %d: line %d %q is %d cells, over the %d cap",
							width, i, strip(ln), w, max(width, 1))
					}
				}
				switch hung := strings.HasPrefix(strip(lines[0]), marker); {
				case width < mw+1 && hung:
					t.Errorf("width %d holds no text column beside the marker, yet the hang survives: %q",
						width, mapStrip(lines))
				case width >= mw+1 && !hung:
					t.Errorf("width %d seats the marker and a text column, yet the hang was shed: %q",
						width, mapStrip(lines))
				}
			})
		}
	}
}

// ----------------------------------------------------------------------------
// The full-width band (renderToRail)
// ----------------------------------------------------------------------------

// A style that carries a BACKGROUND is filled out to its wrap rail, on the first row and on every
// wrapped continuation alike: that is how a diff line's tint reaches the block's edge under a short
// line's trailing space instead of stopping at the last glyph (ratified calls 2 and 6 of
// docs/plans/"2026-08-19 - 05"). The pad has to sit INSIDE the SGR run — a styled line closes with a
// reset, so spaces appended after it would show the terminal's own background through the very band
// they were added to fill — which is what the reset-at-the-end assertion below pins.
//
// The rails are asserted TOGETHER because they are one rule: every one of them bands the TEXT to the
// room its chrome prefix leaves and the two tile the block width between them, so the assertion is
// on the row's total either way — and a caller changing one rail alone is exactly the drift this
// holds shut.
func TestWrapRailsFillABandedStyleToTheRail(t *testing.T) {
	t.Parallel()

	const width = 24
	const marker = "┝ "
	const text = "alpha beta gamma delta epsilon" // longer than the rail: it wraps

	th := newTheme(scheme.Default())
	band := detailStyle(th, detailDiffAdded, true)

	rails := map[string][]string{
		"hangingWrap":  hangingWrap(th, band, marker, text, width),
		"gutteredWrap": gutteredWrap(th, band, marker, marker, text, width),
	}
	clipped, _ := clipWrap(th, band, marker, text, width, 1)
	rails["clipWrap"] = clipped

	for name, lines := range rails {
		if len(lines) == 0 {
			t.Fatalf("%s returned no lines", name)
		}
		for i, ln := range lines {
			if got := th.measure.Width(ln); got != width {
				t.Errorf("%s line %d is %d cells wide; want the full %d-cell rail (%q)",
					name, i, got, width, strip(ln))
			}
			if strings.HasSuffix(ln, " ") {
				t.Errorf("%s line %d ends in a bare space: the pad fell outside the band (%q)", name, i, ln)
			}
		}
	}
}

// The gutter beside a banded line stays CHROME (ratified call 3): gutteredWrap paints its marker and
// its continuation gutter in the detail tone and starts the band after them, so the band is the
// text's field and never the frame's. The row still totals the block width — the prefix and the band
// tile it exactly once between them, which is the arithmetic the rail assertion above depends on.
func TestGutteredWrapKeepsItsPrefixOutsideTheBand(t *testing.T) {
	t.Parallel()

	const width = 24
	const marker, gutter = "┝ ", "│ "

	th := newTheme(scheme.Default())
	band := detailStyle(th, detailDiffAdded, true)

	lines := gutteredWrap(th, band, marker, gutter, "alpha beta gamma delta epsilon", width)
	if len(lines) < 2 {
		t.Fatalf("gutteredWrap produced %d lines; want a wrapped body to check a continuation row", len(lines))
	}
	for i, prefix := range []string{marker, gutter} {
		if want := th.toolDetail.Render(prefix); !strings.HasPrefix(lines[i], want) {
			t.Errorf("line %d = %q; want it to open with the chrome prefix %q", i, lines[i], want)
		}
		if strings.HasPrefix(lines[i], band.Render(prefix)) {
			t.Errorf("line %d opens with the prefix inside the band; the gutter is chrome: %q", i, lines[i])
		}
	}
}

// The hanging prefix beside a banded line stays CHROME (ratified call 3): hangingWrap and its
// row-capped twin clipWrap paint the marker — and the blank indent under it on every continuation
// row — with the band's background CLEARED, then band the text alone out to the rail left of it, so
// the ┝/┕ branch glyph and the column beneath it read as the frame they are rather than as part of
// the change. It is the same division gutteredWrap draws above, which is what makes the band the
// text's field in every stacked and flat frame rather than in one of them.
func TestHangingRailsKeepTheirPrefixOutsideTheBand(t *testing.T) {
	t.Parallel()

	const width = 24
	const marker = "┝ "
	const indent = "  " // the blank hanging indent every continuation row leads with
	const text = "alpha beta gamma delta epsilon"

	th := newTheme(scheme.Default())
	band := detailStyle(th, detailDiffAdded, true)
	chrome := band.Background(lipgloss.NoColor{})

	lines := hangingWrap(th, band, marker, text, width)
	if len(lines) < 2 {
		t.Fatalf("hangingWrap produced %d lines; want a wrapped body to check a continuation row", len(lines))
	}
	clipped, _ := clipWrap(th, band, marker, text, width, 1)
	if len(clipped) != 1 {
		t.Fatalf("clipWrap spent %d rows on a one-row budget; want one", len(clipped))
	}

	for _, tc := range []struct {
		name   string
		row    string
		prefix string
	}{
		{name: "hangingWrap first row", row: lines[0], prefix: marker},
		{name: "hangingWrap continuation", row: lines[1], prefix: indent},
		{name: "clipWrap first row", row: clipped[0], prefix: marker},
	} {
		// The row is asserted WHOLE: the prefix in one unbanded run and everything after it — the
		// text and the pad that fills it to the rail — in one banded run, so a pad that leaked into
		// the chrome or a prefix that fell inside the band both read as a mismatch here.
		banded := strings.TrimPrefix(strip(tc.row), tc.prefix)
		if want := chrome.Render(tc.prefix) + band.Render(banded); tc.row != want {
			t.Errorf("%s = %q; want a chrome prefix beside the band %q", tc.name, tc.row, want)
		}
	}
}

// A style with NO background is untouched by the rule — it renders the very bytes it rendered before
// the band existed. That is what keeps every non-diff wrap in the transcript out of this change, and
// it is asserted on the bytes rather than on the stripped text because a pad inside a foreground-only
// style would be invisible to the eye and still wrong.
func TestWrapRailsLeaveAPlainStyleAlone(t *testing.T) {
	t.Parallel()

	const width = 24
	const marker = "┝ "

	th := newTheme(scheme.Default())
	prefixed := hangingPrefixes(th, marker, "alpha beta gamma delta epsilon", width)
	lines := hangingWrap(th, th.toolDetail, marker, "alpha beta gamma delta epsilon", width)

	for i, ln := range lines {
		if want := th.toolDetail.Render(prefixed[i]); ln != want {
			t.Errorf("plain line %d = %q; want the unpadded render %q", i, ln, want)
		}
	}
}

// The pad is counted in the WIDTH AUTHORITY's measure (ADR 0030), not in runes or bytes: a CJK glyph
// costs two cells, so a line of them takes half as many pad spaces as its rune count would suggest.
// Counting it any other way overshoots the rail and the viewport folds the banded row into two.
func TestWrapRailsPadWideGlyphsInTheAuthoritysMeasure(t *testing.T) {
	t.Parallel()

	const width = 20
	const text = "日本語" // three glyphs, six cells

	for _, pm := range paintMethods {
		t.Run(pm.name, func(t *testing.T) {
			t.Parallel()
			th := newTheme(scheme.Default())
			th.measure = widthAuthority{method: pm.method}

			lines := hangingWrap(th, detailStyle(th, detailDiffRemoved, true), "", text, width)
			if len(lines) != 1 {
				t.Fatalf("hangingWrap(%q) returned %d lines; want one", text, len(lines))
			}
			if got := th.measure.Width(lines[0]); got != width {
				t.Errorf("banded CJK line is %d cells; want the %d-cell rail", got, width)
			}
			if got, want := strip(lines[0]), text+strings.Repeat(" ", width-th.measure.Width(text)); got != want {
				t.Errorf("banded CJK line = %q; want %q (pad measured in cells, not runes)", got, want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The row-capped clip (clipWrap)
// ----------------------------------------------------------------------------

// clipWrap is hangingWrap with a row budget, and the budget is the only difference: text that fits
// inside it comes back as hangingWrap's own lines — same breaks, same hanging indent, same styling —
// and reports no clip. That is what lets a caller reach for it unconditionally instead of measuring
// the text first and branching, which is how the collapsed and expanded paints of one block would
// come to disagree about where a line breaks.
func TestClipWrapLeavesFittingTextAlone(t *testing.T) {
	t.Parallel()

	const width = 40
	marker := branchMarker(true)

	cases := []struct {
		name, text string
		maxRows    int
		rows       int // what the fixture wraps to — a budget it must stay under
	}{
		{name: "one row inside a two-row budget", text: "go build ./...", maxRows: 2, rows: 1},
		{name: "empty text", text: "", maxRows: 2, rows: 1},
		{name: "exactly the budget", text: strings.Repeat("ab ", 20), maxRows: 2, rows: 2},
		{name: "cjk", text: "日本語のテキスト", maxRows: 2, rows: 1},
		{name: "tab bearing", text: "a\tb\tc", maxRows: 1, rows: 1},
	}

	for _, pm := range paintMethods {
		for _, c := range cases {
			t.Run(pm.name+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				th := newTheme(scheme.Default())
				th.measure = widthAuthority{method: pm.method}

				want := hangingWrap(th, th.toolDetail, marker, c.text, width)
				if len(want) != c.rows {
					t.Fatalf("the fixture wraps to %d rows, not the %d the case assumes: %q",
						len(want), c.rows, mapStrip(want))
				}

				got, clipped := clipWrap(th, th.toolDetail, marker, c.text, width, c.maxRows)
				if clipped {
					t.Errorf("reported a clip on %d rows inside a %d-row budget", len(want), c.maxRows)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("clipped output %q differs from hangingWrap's %q", mapStrip(got), mapStrip(want))
				}
			})
		}
	}
}

// Text past the budget comes back as exactly the budget's rows, the last of them ending in the
// continuation tail, and every row inside the width it was given — layout.md's absolute cap, held
// in the measure the frame is painted in (ADR 0030). Fitting the tail is the whole point: appending
// it to a row the wrap had already filled would overrun the width and the viewport would fold the
// row in two, spending the very row the budget was saving.
//
// The rows above the last are hangingWrap's own, untouched, so a clip only ever cuts at its seam.
func TestClipWrapHoldsItsRowBudget(t *testing.T) {
	t.Parallel()

	longRun := "cd . && head -3 go.mod && echo \"---\" && wc -l $(find . -name '*.go' | grep -v dist) 2>/dev/null | tail -1"
	marker := branchMarker(true)
	indent := strings.Repeat(" ", len([]rune(marker))) // the marker is four one-cell runes

	cases := []struct{ name, text string }{
		{"long command", longRun + " && " + longRun},
		{"breakpoint run", strings.Repeat("a-b-c-d ", 40)},
		{"cjk", strings.Repeat("日本語のテキスト", 16)},
		{"tab bearing", strings.Repeat("col\tvalue\t", 20)},
		{"vs16", strings.Repeat(vs16Warning+" warning ", 24)},
	}

	for _, pm := range paintMethods {
		for _, c := range cases {
			for _, width := range []int{20, 40, 80} {
				for maxRows := 1; maxRows <= 2; maxRows++ {
					name := pm.name + "/" + c.name + "/width " + strconv.Itoa(width) + "/rows " + strconv.Itoa(maxRows)
					t.Run(name, func(t *testing.T) {
						t.Parallel()
						th := newTheme(scheme.Default())
						th.measure = widthAuthority{method: pm.method}

						full := hangingWrap(th, th.toolDetail, marker, c.text, width)
						if len(full) <= maxRows {
							t.Fatalf("the fixture wraps to %d rows and never reaches the %d-row budget", len(full), maxRows)
						}

						got, clipped := clipWrap(th, th.toolDetail, marker, c.text, width, maxRows)
						if !clipped {
							t.Errorf("reported no clip while dropping %d of %d rows", len(full)-maxRows, len(full))
						}
						if len(got) != maxRows {
							t.Fatalf("kept %d rows, want the budget's %d: %q", len(got), maxRows, mapStrip(got))
						}
						last := strip(got[maxRows-1])
						if !strings.HasSuffix(last, clipTail) {
							t.Errorf("the last kept row %q does not end in the continuation tail %q", last, clipTail)
						}
						for i, ln := range got {
							if w := th.measure.Width(ln); w > width {
								t.Errorf("row %d %q is %d cells, over the %d cap", i, strip(ln), w, width)
							}
							if strings.Contains(strip(ln), "\t") {
								t.Errorf("row %d still carries a tab for a style to rewrite: %q", i, strip(ln))
							}
							prefix := indent
							if i == 0 {
								prefix = marker
							}
							if !strings.HasPrefix(strip(ln), prefix) {
								t.Errorf("row %d %q does not hang under %q", i, strip(ln), prefix)
							}
						}
						// Only the seam row is re-cut; everything above it is the wrap's own.
						for i := 0; i < maxRows-1; i++ {
							if got[i] != full[i] {
								t.Errorf("row %d %q is not hangingWrap's own %q", i, strip(got[i]), strip(full[i]))
							}
						}
					})
				}
			}
		}
	}
}

// The degenerate widths, where the block is narrower than the mark it has to make. A clipped row
// says "…" or it says nothing at all, so the tail is the one thing the cap yields to — and a budget
// of no rows keeps nothing while still reporting the clip, rather than handing back a row the caller
// has no room to paint.
func TestClipWrapSurvivesNarrowWidths(t *testing.T) {
	t.Parallel()

	for _, pm := range paintMethods {
		t.Run(pm.name, func(t *testing.T) {
			t.Parallel()
			th := newTheme(scheme.Default())
			th.measure = widthAuthority{method: pm.method}
			floor := th.measure.Width(clipTail)

			for width := 0; width <= 6; width++ {
				got, clipped := clipWrap(th, th.toolDetail, branchMarker(true), "a long target that cannot fit", width, 1)
				if !clipped {
					t.Errorf("width %d: reported no clip", width)
				}
				if len(got) != 1 {
					t.Fatalf("width %d: kept %d rows, want 1: %q", width, len(got), mapStrip(got))
				}
				if w := th.measure.Width(got[0]); w > max(width, floor) {
					t.Errorf("width %d: row %q is %d cells, over the %d cap", width, strip(got[0]), w, max(width, floor))
				}

				// The clipTail allowance above is a DIFFERENT, intended floor, and fitting the tail
				// re-cuts the kept row on its way out — which would mask a marker overrun rather
				// than report one. With rows to spare nothing is re-cut, so this is where the
				// marker has to have been SHED by a block too narrow to hold it beside a text
				// column (hangCollapses) instead of prepended to a wrap floored at one column.
				rows, cut := clipWrap(th, th.toolDetail, branchMarker(true), "a long target that cannot fit", width, 99)
				if cut {
					t.Errorf("width %d: reported a clip inside a 99-row budget", width)
				}
				for i, ln := range rows {
					if w := th.measure.Width(ln); w > max(width, 1) {
						t.Errorf("width %d: unclipped row %d %q is %d cells, over the %d cap",
							width, i, strip(ln), w, max(width, 1))
					}
				}
			}

			if got, clipped := clipWrap(th, th.toolDetail, branchMarker(true), "anything", 40, 0); got != nil || !clipped {
				t.Errorf("a zero-row budget returned %q (clipped %v), want no rows and a reported clip", mapStrip(got), clipped)
			}
		})
	}
}

// wrapText is the one wrap in the package, so moving it moves every wrapped surface at once —
// transcript prose and table cells among them. Each is asserted through its own production entry
// point rather than through wrapText again, so a surface that grew a wrap of its own would show up
// here.
//
// The pop-up body is the third surface ISSUES.md named and it is deliberately NOT here: its wrap does
// move with the others, but the pane it lands in is composed by lipgloss — Style.Width pads, and
// past its width WRAPS, in GraphemeWidth whatever the painter is doing — so a VS16 line the
// authority calls short enough is folded back into two pane rows. That fold is the pane's, not the
// wrap's: it is reachable today through pop-up ROWS, which never touch wrapText at all, and it is
// the lipgloss pane's own deliberate behaviour rather than a residue tracked anywhere.
func TestWrappedSurfacesBreakInThePaintersMeasure(t *testing.T) {
	t.Parallel()

	// Five painted cells, eight grapheme cells: one line to the painter's default measure, two to
	// the other one, at a width of 5 on every surface.
	const width = 5
	warns := vs16Warning + " " + vs16Warning + " " + vs16Warning

	surfaces := []struct {
		name  string
		lines func(th theme) []string
	}{
		{
			name:  "transcript prose",
			lines: func(th theme) []string { return renderMarkdownBody(th, warns, width) },
		},
		{
			name:  "table cell",
			lines: func(th theme) []string { return wrapTableCell(th, warns, width) },
		},
	}

	for _, pm := range paintMethods {
		for _, s := range surfaces {
			t.Run(pm.name+"/"+s.name, func(t *testing.T) {
				t.Parallel()
				th := newTheme(scheme.Default())
				th.measure = widthAuthority{method: pm.method}

				want := 1
				if pm.method == ansi.GraphemeWidth {
					want = 2
				}
				lines := s.lines(th)
				if len(lines) != want {
					t.Fatalf("%s wrapped %q at width %d into %d lines, want %d: %q",
						s.name, warns, width, len(lines), want, lines)
				}
				// Whatever the break, no line may exceed the width it was given in the measure
				// the frame is painted in — layout.md's absolute cap.
				for i, ln := range lines {
					if w := th.measure.Width(strip(ln)); w > width {
						t.Errorf("%s line %d %q is %d cells, over the %d cap", s.name, i, strip(ln), w, width)
					}
				}
			})
		}
	}
}

// ----------------------------------------------------------------------------
// The sub-agent rail through the inter-block spacers
// ----------------------------------------------------------------------------

// The one separator row between two blocks is railed at the JOIN of their depths — the shallower
// of the two — and that single rule is the whole of the continuous rail: a spacer inside a run
// carries the gutter, a spacer that crosses a run boundary does not. A climb-out is no exception
// here: the ┊ replaces the spacer only where another grouped sub-agent follows the expanded one
// (railJoin, pinned by TestSubAgentCloserOnlyWhenAnotherGroupedMemberFollows), and none of these
// cases is a group. Each case pins the entire rendered scrollback, so a separator that gained or
// lost a rail shows as the row it is.
func TestRenderSpacerRailsAtTheJoinDepth(t *testing.T) {
	cases := []struct {
		name  string
		build func(tr *transcript)
		want  []string
	}{
		{
			// The narration between the two calls is what keeps them two BLOCKS: adjacent calls of
			// different labels fold under one umbrella otherwise (toolSuperGroup), and one block has
			// no spacer inside it to rail.
			name: "two different-label blocks inside one run",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", 1, 5, 1)
				tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "now the tests"})
				tr.apply(domain.ToolCallEvent{
					EventBase: domain.EventBase{Depth: 1},
					Call:      domain.ToolCall{ID: "c2", Tool: "terminal", Arguments: []byte(`{"command":"go test"}`)},
				})
			},
			want: []string{
				"│ ✦ Read",
				"│   ┕ a.go ⋯ 5 lines",
				"│", // both sides sit at depth 1: the rail runs straight through
				"│ ✦ now the tests",
				"│",
				"│ ✦ Terminal",
				"│   ┕ go test ⋯",
			},
		},
		{
			name: "a climb-out to the top level ends the rail",
			build: func(tr *transcript) {
				tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "child"})
				tr.apply(domain.MessageEvent{Text: "back to parent"})
			},
			want: []string{
				"│ ✦ child",
				"", // the join is the shallower of the two: the rail simply stops here
				"✦ back to parent",
			},
		},
		{
			name: "a 0 to 2 descent opens both rails at once",
			build: func(tr *transcript) {
				tr.apply(domain.MessageEvent{Text: "delegating"})
				tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 2}, Text: "deep"})
			},
			want: []string{
				"✦ delegating",
				"", // join 0: nothing announces the descent, so the spacer is the flat one
				"│ │ ✦ deep",
			},
		},
		{
			name: "a 2 to 1 climb-out keeps the outer rail",
			build: func(tr *transcript) {
				tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 2}, Text: "deep"})
				tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "shallower"})
			},
			want: []string{
				"│ │ ✦ deep",
				"│", // only the rail both sides reach survives the climb; no member follows, so no ┊
				"│ ✦ shallower",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)

			got, want := renderPlain(tr, 80), strings.Join(tc.want, "\n")

			if got != want {
				t.Errorf("spacer rail mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// The issue's core case: two sub-agent calls back to back are never visually connected. Under ADR
// 0063 they cannot be — each run is ONE row of the list they fold into, its span elided whole
// (layout.md), so there is no rail on screen for a second one to run into. What used to part them
// was the first frame's ┊ closer; now nothing stands between the rows because nothing hangs off
// them, and neither delegation's words reach the conversation at all.
func TestRenderConsecutiveSubAgentRunsAreNotConnected(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "s1", Tool: "sub_agent", Arguments: []byte(`{"task":"first"}`)}})
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s1"}, Text: "first child"})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "s2", Tool: "sub_agent", Arguments: []byte(`{"task":"second"}`)}})
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s2"}, Text: "second child"})

	want := strings.Join([]string{
		"✦ Sub-Agent (2)", // the two adjacent delegations are rows of one list (subAgentGroup)
		groupMemberLine("  ┝ first ⋯ 0 tool calls"),
		groupMemberLine("  ┕ second ⋯ 0 tool calls"),
	}, "\n")
	got := renderPlain(tr, 80)
	if got != want {
		t.Errorf("consecutive sub-agent runs mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	for _, absent := range []string{glyphSubRail, glyphRailClose, glyphRailCorner} {
		if strings.Contains(got, absent) {
			t.Errorf("two adjacent runs drew %q between them:\n%s", absent, got)
		}
	}
}

// A railed spacer is a real rail: it carries the subRail role's styling (an unstyled gutter would
// read as stray punctuation rather than as the frame continuing) and it ends on the glyph — the
// gutter's trailing space is trimmed, so the row never leaves a styled blank hanging off its right.
func TestRenderSpacerRailIsStyledAndUntrailed(t *testing.T) {
	th := newTheme(scheme.Default())
	tr := feed(
		domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "one"},
		domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "two"},
	)

	spacer := tr.renderLines(th, 80)[1] // the label → block separator inside the run

	if want := th.subRail.Render(glyphSubRail); spacer != want {
		t.Errorf("railed spacer = %q; want the subRail-styled gutter %q", spacer, want)
	}
	if plain := ansi.Strip(spacer); plain != strings.TrimRight(plain, " ") {
		t.Errorf("railed spacer %q carries trailing whitespace", plain)
	}
}

// The sub-agent frame — rail, corner and closer alike, all the subRail role — is painted in the
// scheme's `tool-header` role, the same gold toolLabel carries, so a nested run reads as one
// coloured frame rather than as dim chrome. The assertion compares against the palette's own render
// rather than a lipgloss byte-golden; the guard below it catches the opposite failure, a subRail
// role that paints nothing at all and would leave the rail unstyled.
func TestSubRailPaintedInToolHeaderGold(t *testing.T) {
	th := newTheme(scheme.Default())

	rail := th.subRail.Render(glyphSubRail)

	if want := lipgloss.NewStyle().Foreground(lipgloss.Color(scheme.Default().ToolHeader)).Render(glyphSubRail); rail != want {
		t.Errorf("rail = %q; want the `tool-header` role's gold %q the tool header carries", rail, want)
	}
	if rail == glyphSubRail {
		t.Fatal("the subRail role renders no escape sequence; the rail and label would be unstyled")
	}
}

// Design call 2 splits the delegation frame's header row between two voices, and the split is the
// whole point of it: ┌ and ┊ are the RAIL — its top end and its close — and wear the gold the │
// between them does, while the arm and the tee reaching across to the branch are that row's own
// chrome and stay in the tone every other ┝/┕ takes. A frame painted in one voice throughout would
// read as a box drawn around the run rather than as a rail hanging off it.
func TestSubAgentFrameSplitsRailGoldFromBranchTone(t *testing.T) {
	th := newTheme(scheme.Default())

	marker := paintRowMarker(th, subAgentOpenMarker, true)

	gold, branch := th.subRail.Render(glyphRailCorner), detailTone(th, true).Render("─"+glyphRailTee+" ")
	if want := gold + branch; marker != want {
		t.Errorf("┌─┶ marker = %q; want the corner in rail gold and the arm in the branch tone %q",
			marker, want)
	}
	if got, want := ansi.Strip(marker), subAgentOpenMarker; got != want {
		t.Errorf("painting the marker changed its cells to %q; want %q", got, want)
	}
	if plain := branchMarker(false); paintRowMarker(th, plain, true) != detailTone(th, true).Render(plain) {
		t.Error("an ordinary ┝ marker no longer paints in one tone; only the frame's corner is split off")
	}
}

// The done ✓ is the `success` role and nothing else — errorText's green counterpart, so the two
// verdicts a row can carry are read as one pair — and it lands on the row it belongs to, after the
// delegation's name and ahead of the leaders.
func TestSubAgentDoneMarkPaintedInTheSuccessRole(t *testing.T) {
	th := newTheme(scheme.Default())

	row := leaderRow(th, toolView{Target: "survey", finished: true}, branchMarker(true), 60, false, noRemainder)

	styled := th.successMark.Render(glyphDone)
	if styled == glyphDone {
		t.Fatal("the successMark role renders no escape sequence; the ✓ would be unstyled")
	}
	if want := lipgloss.NewStyle().Foreground(lipgloss.Color(scheme.Default().Success)).Render(glyphDone); styled != want {
		t.Errorf("✓ style = %q; want the scheme's `success` green %q", styled, want)
	}
	if !strings.Contains(row, styled) {
		t.Errorf("row %q does not carry the ✓ in its own role", row)
	}
	if got, want := ansi.Strip(row), "  ┕ survey ✓ "; !strings.HasPrefix(got, want) {
		t.Errorf("row text = %q; want it to open %q (the mark follows the name, ahead of the leaders)", got, want)
	}
	plain := leaderRow(th, toolView{Target: "survey"}, branchMarker(true), 60, false, noRemainder)
	if strings.Contains(ansi.Strip(plain), glyphDone) {
		t.Errorf("an unfinished delegation's row carries a ✓: %q", ansi.Strip(plain))
	}
}

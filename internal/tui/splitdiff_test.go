package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// sketchRegions is the change docs/layout/split-diff-layout.md draws both readings of: one
// replacement mid-file whose inserted line is too long for a pane, and a second region far enough
// down the file that the two panes' numbering has drifted apart. The stacked reading pins the same
// fixture (TestStackedDiffLinesRendersTheLayoutSketch), so the two arrangements of one body are
// tested against one change rather than against two convenient ones.
func sketchRegions() []domain.EditRegion {
	return []domain.EditRegion{
		{
			BeforeStart: 88, AfterStart: 88,
			Leading:  []string{"func paint(w int) error {", "  if w < minWidth {"},
			Removed:  []string{"    return errNarrow"},
			Inserted: []string{`    return fmt.Errorf("width %d under %d", w, minWidth)`},
			Trailing: []string{"  }"},
		},
		{
			BeforeStart: 204, AfterStart: 205,
			Leading:  []string{"  return nil"},
			Removed:  []string{"}"},
			Inserted: []string{"  }", ""},
		},
	}
}

// splitPlain is the painted rows with their styling stripped — what the terminal shows, which is
// what every layout assertion below is about.
func splitPlain(rows []string) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = strip(row)
	}
	return out
}

// The wide reading of the layout doc's own sketch, row for row: two panes under one number gutter,
// the removed line left and its replacement right on the SAME row, the replacement wrapping onto a
// continuation row that carries neither number nor marker, one ⋯ rule where the two regions do not
// meet, and each pane numbering its own file across it (before 204 against after 205).
//
// EITHER pane squares its filled rows to the pane's width now, which is what carries a band to the
// pane's edge (ratified call 2 of docs/plans/"2026-08-19 - 05") — so the trailing blanks below are
// the tint's own field on a changed row and an invisible pad on a context one, and the divider
// stands in the column it always did.
func TestSplitDiffRowsPaintsTheLayoutSketch(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	got := splitPlain(splitDiffRows(th, sketchRegions(), 100))

	want := []string{
		" 88   func paint(w int) error {                  │  88   func paint(w int) error {                 ",
		" 89     if w < minWidth {                        │  89     if w < minWidth {                       ",
		` 90 -     return errNarrow                       │  90 +     return fmt.Errorf("width %d under %d",`,
		"                                                 │       w, minWidth)                              ",
		" 91     }                                        │  91     }                                       ",
		strings.Repeat(glyphLeaderDot, 99),
		"204     return nil                               │ 205     return nil                              ",
		"205 - }                                          │ 206 +   }                                       ",
		"                                                 │ 207 +                                           ",
	}
	if len(got) != len(want) {
		t.Fatalf("painted %d rows, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d =\n%q\nwant\n%q", i, got[i], want[i])
		}
	}
}

// The width rule is the code column each pane is left with, and it flips at exactly the column the
// constant names (ratified call 5). The sketch's body shows a three-digit number, so its gutter is
// four cells and its marker two: 40 code columns per pane wants 95 columns of block, and 94 is one
// short.
func TestSplitDiffFitsFlipsAtTheBoundaryWidth(t *testing.T) {
	t.Parallel()

	regions := sketchRegions()
	if !splitDiffFits(regions, 95) {
		t.Errorf("splitDiffFits(95) = false, want true — 95 columns leave each pane exactly %d for code",
			splitPaneMinCols)
	}
	if splitDiffFits(regions, 94) {
		t.Errorf("splitDiffFits(94) = true, want false — one column short of %d per pane", splitPaneMinCols)
	}
	if got := splitCodeCells(95, splitNumberGutter(splitRowPlan(regions))); got != splitPaneMinCols {
		t.Errorf("code columns at the boundary = %d, want %d", got, splitPaneMinCols)
	}
	if splitDiffFits(nil, 200) {
		t.Error("splitDiffFits(nil) = true, want false — no regions is nothing to arrange")
	}
}

// A pure insertion has no before side at all, so the whole left stack of that region pads: the
// columns left of the divider are blank on every changed row, and the divider still stands where
// the context rows put it.
func TestSplitDiffRowsPadTheSideWithNoLines(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	regions := []domain.EditRegion{{
		BeforeStart: 12, AfterStart: 12,
		Leading:  []string{"before"},
		Inserted: []string{"added one", "added two"},
		Trailing: []string{"after"},
	}}

	rows := splitPlain(splitDiffRows(th, regions, 100))
	want := []string{
		"12   before                                      │ 12   before                                     ",
		"                                                 │ 13 + added one                                  ",
		"                                                 │ 14 + added two                                  ",
		"13   after                                       │ 15   after                                      ",
	}
	if len(rows) != len(want) {
		t.Fatalf("painted %d rows, want %d:\n%s", len(rows), len(want), strings.Join(rows, "\n"))
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d =\n%q\nwant\n%q", i, rows[i], want[i])
		}
	}
}

// A wrapped line is ONE line: its continuation rows carry no number and no marker, in either pane,
// so nothing on screen claims the wrap was a second change.
func TestSplitDiffContinuationRowsCarryNoNumberOrMarker(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	regions := []domain.EditRegion{{
		BeforeStart: 5, AfterStart: 5,
		Removed:  []string{strings.Repeat("old ", 30)},
		Inserted: []string{strings.Repeat("new ", 30)},
	}}

	rows := splitPlain(splitDiffRows(th, regions, 100))
	if len(rows) < 2 {
		t.Fatalf("painted %d rows, want the wrap to take several:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	gutter := splitNumberGutter(splitRowPlan(regions))
	for i, row := range rows[1:] {
		left, right, ok := strings.Cut(row, splitPaneDivider)
		if !ok {
			t.Fatalf("continuation row %d has no divider: %q", i+1, row)
		}
		// The left pane opens the row; the right pane opens one blank column past the divider.
		for name, pane := range map[string]string{"left": left, "right": strings.TrimPrefix(right, " ")} {
			head := th.measure.Truncate(pane, gutter+splitMarkerCells, "")
			if strings.TrimSpace(head) != "" {
				t.Errorf("%s pane of continuation row %d opens with %q, want a blank number and marker",
					name, i+1, head)
			}
		}
	}
}

// The panes stay row-aligned however the wrapping alternates between them: the divider stands in
// one column down the whole body, and no row overruns the width it was composed for (layout.md's
// absolute cap).
//
// The band changed nothing here, and that is the point of asserting it: a filled cell squares itself
// to the pane now, banded or plain, so every row still ends either at the divider — where the right
// pane has nothing — or at both panes and the rule between them, and the divider column is the one
// it always was.
func TestSplitDiffRowsStayAlignedWhenEitherSideWraps(t *testing.T) {
	t.Parallel()

	const width = 120
	th := newTheme(scheme.Default())
	regions := []domain.EditRegion{{
		BeforeStart: 1, AfterStart: 1,
		Removed:  []string{strings.Repeat("long removed ", 12), "short"},
		Inserted: []string{"short", strings.Repeat("long inserted ", 12)},
		Trailing: []string{"tail"},
	}}

	wantCol := splitPaneCells(width) + 1 // the pane, then the blank column before the divider
	for i, row := range splitPlain(splitDiffRows(th, regions, width)) {
		if w := th.measure.Width(row); w > width {
			t.Errorf("row %d is %d columns wide, over the %d it was composed for: %q", i, w, width, row)
		}
		idx := strings.Index(row, splitPaneDivider)
		if idx < 0 {
			continue // the ⋯ rule spans both panes and has no divider of its own
		}
		if col := th.measure.Width(row[:idx]); col != wantCol {
			t.Errorf("row %d puts the divider in column %d, want %d: %q", i, col, wantCol, row)
		}
		full := splitPaneCells(width)*2 + splitDividerCells
		if w := th.measure.Width(row); w != full && w != wantCol+1 {
			t.Errorf("row %d is %d columns, want the full %d — or %d where the right pane has nothing: %q",
				i, w, full, wantCol+1, row)
		}
	}
}

// The ⋯ rule is drawn between two regions that do NOT meet in the before file's numbering, and
// nowhere else — the very predicate the stacked reading elides by (regionsMeet), so the two
// readings of one body claim the same elisions. Regions that meet paint end to end, which is what
// makes the tiled neighbours read exactly as one merged region would have.
func TestSplitDiffRowsRuleOnlyWhereRegionsDoNotMeet(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	// The first region spans before-lines 10..14, so a neighbour starting at 15 meets it.
	meeting := []domain.EditRegion{
		{BeforeStart: 10, AfterStart: 10, Leading: []string{"a"}, Removed: []string{"b"}, Inserted: []string{"B"},
			Trailing: []string{"c", "d", "e"}},
		{BeforeStart: 15, AfterStart: 15, Leading: []string{"f", "g"}, Removed: []string{"h"}, Inserted: []string{"H"}},
	}
	for i, row := range splitPlain(splitDiffRows(th, meeting, 100)) {
		if strings.Contains(row, glyphLeaderDot) {
			t.Errorf("row %d draws a rule between regions that meet: %q", i, row)
		}
	}

	parted := []domain.EditRegion{meeting[0], meeting[1]}
	parted[1].BeforeStart, parted[1].AfterStart = 16, 16
	rows := splitPlain(splitDiffRows(th, parted, 100))
	// Five rows for the first region — one context, the removed and inserted pair on one row, three
	// trailing — so the rule stands on the sixth, between the regions rather than inside either.
	if len(rows) < 6 || !strings.Contains(rows[5], glyphLeaderDot) {
		t.Fatalf("rows =\n%s\nwant the rule between two regions that do not meet", strings.Join(rows, "\n"))
	}
	if got, want := th.measure.Width(rows[5]), splitPaneCells(100)*2+splitDividerCells; got != want {
		t.Errorf("the rule spans %d columns, want %d — both panes and the divider between them", got, want)
	}
}

// The marker rides its line's BAND while the number beside it stays chrome: the glyph sits inside
// the banded run rather than outside it, which is what keeps it the change's palette-proof signal —
// a mark that reads on a monochrome pipe and on a terminal that drops backgrounds alike (ADR 0052's
// 2026-08-19 amendment, which supersedes the "the marker travels with the TEXT's colour" rationale
// of ratified calls 6 and 7). The band it opens runs on to the pane's edge, which the two tests
// below pin on the pane painter itself.
func TestSplitDiffRowsColourTheMarkerWithItsLine(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no-colour profile: there is no styling to assert")
	}
	regions := []domain.EditRegion{{BeforeStart: 7, AfterStart: 7, Removed: []string{"gone"}, Inserted: []string{"here"}}}
	code := splitCodeCells(100, splitNumberGutter(splitRowPlan(regions)))

	row := splitDiffRows(th, regions, 100)[0]
	for _, banded := range []struct {
		pane, marker, text string
		kind               detailKind
	}{
		{pane: "removed", marker: stackedRemovedMarker, text: "gone", kind: detailDiffRemoved},
		{pane: "inserted", marker: stackedInsertedMarker, text: "here", kind: detailDiffAdded},
	} {
		want := detailStyle(th, banded.kind, true).
			Render(squareLine(th.measure, banded.marker+banded.text, splitMarkerCells+code))
		if !strings.Contains(row, want) {
			t.Errorf("the %s pane does not carry %q as one styled run: %q", banded.pane, want, row)
		}
	}
	if want := th.toolDetail.Render("7 "); !strings.Contains(row, want) {
		t.Errorf("the number gutter is not painted in the muted role: %q", row)
	}
}

// A short line's band runs to the pane's EDGE rather than to its last glyph (ratified call 2 of
// docs/plans/"2026-08-19 - 05"): the tint is where the change is said now, and a band that stopped
// at the text would say nothing under the trailing space — which on a pane of code is most of the
// row. The pad has to sit INSIDE the SGR run, because a styled row closes with a reset and spaces
// appended after it would show the terminal's own background through the very band they were added
// to fill; the number gutter stays OUTSIDE it, chrome (ratified call 3).
func TestSplitCellPaintBandsAShortLineToThePaneEdge(t *testing.T) {
	t.Parallel()

	const gutter, code = 4, 40
	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no-colour profile: there is no band to assert")
	}
	cell := splitCell{number: 90, marker: stackedRemovedMarker, kind: detailDiffRemoved, text: "gone"}

	rows := cell.paint(th, gutter, code)
	if len(rows) != 1 {
		t.Fatalf("paint spent %d rows on one short line, want one", len(rows))
	}
	if got, want := th.measure.Width(rows[0]), gutter+splitMarkerCells+code; got != want {
		t.Errorf("the row is %d columns, want the whole pane's %d: %q", got, want, strip(rows[0]))
	}
	if strings.HasSuffix(rows[0], " ") {
		t.Errorf("the row ends in a bare space: the pad fell outside the band: %q", rows[0])
	}
	number := th.toolDetail.Render(" 90 ") // three right-aligned digit columns, then the parting space
	if !strings.HasPrefix(rows[0], number) {
		t.Errorf("the row does not open with the chrome number %q: %q", number, rows[0])
	}
	want := detailStyle(th, detailDiffRemoved, true).
		Render(squareLine(th.measure, stackedRemovedMarker+"gone", splitMarkerCells+code))
	if got := strings.TrimPrefix(rows[0], number); got != want {
		t.Errorf("the banded run = %q, want the marker and its text filled to the pane edge %q", got, want)
	}
}

// A continuation row is banded from the SAME column its first row is — the marker's — while the
// gutter-width blanks standing in for the number stay chrome (ratified calls 2 and 3). The marker
// column is inside the band on both, which is what makes a wrapped line one unbroken block of tint
// instead of a band that steps right on every row after the first.
func TestSplitCellPaintKeepsContinuationGuttersChrome(t *testing.T) {
	t.Parallel()

	const gutter, code = 4, 10
	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no-colour profile: there is no band to assert")
	}
	cell := splitCell{number: 90, marker: stackedInsertedMarker, kind: detailDiffAdded,
		text: "alpha beta gamma delta"}

	rows := cell.paint(th, gutter, code)
	lines := wrapText(th, cell.text, code)
	if len(lines) < 2 {
		t.Fatalf("the fixture wrapped to %d lines, want a continuation row to check", len(lines))
	}
	if len(rows) != len(lines) {
		t.Fatalf("paint spent %d rows on %d wrapped lines, want one each", len(rows), len(lines))
	}
	band := detailStyle(th, detailDiffAdded, true)
	for i, line := range lines[1:] {
		want := strings.Repeat(" ", gutter) +
			band.Render(squareLine(th.measure, strings.Repeat(" ", splitMarkerCells)+line, splitMarkerCells+code))
		if rows[i+1] != want {
			t.Errorf("continuation row %d = %q, want bare gutter columns and a band from the marker column %q",
				i+1, rows[i+1], want)
		}
		if got, want := th.measure.Width(rows[i+1]), gutter+splitMarkerCells+code; got != want {
			t.Errorf("continuation row %d is %d columns, want the whole pane's %d: %q",
				i+1, got, want, strip(rows[i+1]))
		}
	}
}

// The two readings mark a change identically, so the split reading's marker column is exactly the
// stacked reading's — one width, measured through the width authority rather than assumed.
func TestSplitDiffMarkerColumnMatchesTheStackedReading(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	for _, marker := range []string{stackedRemovedMarker, stackedInsertedMarker, stackedContextMarker} {
		if got := th.measure.Width(marker); got != splitMarkerCells {
			t.Errorf("marker %q is %d columns, but the split panes budget %d", marker, got, splitMarkerCells)
		}
	}
}

// Nothing to arrange, and nowhere to arrange it: no regions paints no rows — which is what leaves a
// call that recorded none showing the argument-derived body it was presented with (ratified call
// 9) — and a width too narrow to seat one column of code paints none either, rather than composing
// a row wider than the block it is for.
func TestSplitDiffRowsPaintNothingWithoutRoomOrRegions(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	if got := splitDiffRows(th, nil, 200); got != nil {
		t.Errorf("splitDiffRows(nil) = %q, want no rows at all", got)
	}
	if got := splitDiffRows(th, sketchRegions(), 12); got != nil {
		t.Errorf("splitDiffRows at 12 columns = %q, want no rows — the code column would be under one cell", got)
	}
}

// TestGitDiffQuotedHeaderKeepsItsFileSections: git prints a section header in one of two shapes,
// and a name it had to escape arrives in the quoted one — `diff --git "a/…" "b/…"`, both names
// quoted the moment either needs it. The walk reads that shape too, so a path riding inside a
// quoted name keeps the body its Split/Stacked reading instead of dropping the whole diff to the
// plain uncoloured output, and the section is named by the path itself rather than its quoting.
func TestGitDiffQuotedHeaderKeepsItsFileSections(t *testing.T) {
	t.Parallel()

	tv := gitDiffCard(t, []string{
		`diff --git "a/my file.go" "b/my file.go"`,
		"index 1111111..2222222 100644",
		`--- "a/my file.go"`,
		`+++ "b/my file.go"`,
		"@@ -1,3 +1,3 @@",
		" one",
		"-two",
		"+TWO",
	})

	if got, want := tv.RegionFiles, []string{"my file.go"}; !slices.Equal(got, want) {
		t.Fatalf("region files = %v, want %v — the section is named by the unquoted path", got, want)
	}
	want := []detailLine{
		{Text: "my file.go"},
		{Text: "1   one"},
		{Kind: detailDiffRemoved, Text: "2 - two"},
		{Kind: detailDiffAdded, Text: "2 + TWO"},
	}
	if got, want := detailDump(tv.Details.all()), detailDump(want); got != want {
		t.Errorf("body:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// TestGitDiffQuotedHeaderDoesNotCostTheOtherFilesTheirSections: the walk is all-or-nothing
// (gitDiffFileSections), so before the quoted shape was read ONE escaped path took every other
// file's section down with it. A diff mixing the two spellings now keeps all of its sections.
func TestGitDiffQuotedHeaderDoesNotCostTheOtherFilesTheirSections(t *testing.T) {
	t.Parallel()

	tv := gitDiffCard(t, []string{
		"diff --git a/alpha.go b/alpha.go",
		"index 1111111..2222222 100644",
		"--- a/alpha.go",
		"+++ b/alpha.go",
		"@@ -1,2 +1,2 @@",
		" one",
		"-two",
		"+TWO",
		`diff --git "a/two words.go" "b/two words.go"`,
		"index 4444444..5555555 100644",
		"@@ -1,2 +1,2 @@",
		" alpha",
		"-beta",
		"+BETA",
	})

	if got, want := tv.RegionFiles, []string{"alpha.go", "two words.go"}; !slices.Equal(got, want) {
		t.Fatalf("region files = %v, want %v — one quoted header costs no file its section", got, want)
	}
	if got := len(tv.Regions); got != 2 {
		t.Errorf("regions = %d, want 2 — one per file section", got)
	}
}

// TestGitDiffFileHeaderPathReadsBothSpellings pins what the two shapes of a section header spell.
// git escapes a name with quote_c_style, writing a byte it cannot print as an octal escape — one
// per UTF-8 byte — so undoing that is what recovers the name every other reading of the file uses.
// A header in neither shape reads as no path at all, which is what stops the walk dead.
func TestGitDiffFileHeaderPathReadsBothSpellings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
		want string
	}{
		{name: "bare", line: "diff --git a/plain.go b/plain.go", want: "plain.go"},
		{name: "a space needs no quoting", line: "diff --git a/my file.go b/my file.go", want: "my file.go"},
		{name: "quoted with a space", line: `diff --git "a/my file.go" "b/my file.go"`, want: "my file.go"},
		{name: "an octal escape per non-ASCII byte", line: `diff --git "a/\303\274ni.go" "b/\303\274ni.go"`, want: "üni.go"},
		{name: "a letter escape", line: `diff --git "a/tab\tname.go" "b/tab\tname.go"`, want: "tab\tname.go"},
		{name: "the verbatim escapes", line: `diff --git "a/say \"hi\"\\.go" "b/say \"hi\"\\.go"`, want: `say "hi"\.go`},
		{name: "the b-side name is the one read", line: `diff --git "a/old\tname.go" "b/new\tname.go"`, want: "new\tname.go"},
		{name: "an unterminated quote", line: `diff --git "a/x.go" "b/x.go`},
		{name: "an escape git would not write", line: `diff --git "a/x\qy.go" "b/x\qy.go"`},
		{name: "a name bare of its side prefix", line: `diff --git "c/x.go" "d/x.go"`},
		{name: "only one side quoted", line: `diff --git "a/x.go" b/x.go`},
		{name: "nothing but the sides", line: `diff --git "a/" "b/"`},
		{name: "trailing junk after the pair", line: `diff --git "a/x.go" "b/x.go" and more`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := gitDiffFileHeaderPath(tc.line)
			if ok != (tc.want != "") {
				t.Fatalf("gitDiffFileHeaderPath(%q) ok = %v, want %v", tc.line, ok, tc.want != "")
			}
			if got != tc.want {
				t.Errorf("gitDiffFileHeaderPath(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

package tui

import (
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
// continuation row that carries neither number nor marker while the left pane pads to keep the
// divider in one column, one ⋯ rule where the two regions do not meet, and each pane numbering its
// own file across it (before 204 against after 205).
func TestSplitDiffRowsPaintsTheLayoutSketch(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	got := splitPlain(splitDiffRows(th, sketchRegions(), 100))

	want := []string{
		" 88   func paint(w int) error {                  │  88   func paint(w int) error {",
		" 89     if w < minWidth {                        │  89     if w < minWidth {",
		` 90 -     return errNarrow                       │  90 +     return fmt.Errorf("width %d under %d",`,
		"                                                 │       w, minWidth)",
		" 91     }                                        │  91     }",
		strings.Repeat(glyphLeaderDot, 99),
		"204     return nil                               │ 205     return nil",
		"205 - }                                          │ 206 +   }",
		"                                                 │ 207 + ",
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
		"12   before                                      │ 12   before",
		"                                                 │ 13 + added one",
		"                                                 │ 14 + added two",
		"13   after                                       │ 15   after",
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

// The marker travels with its line's COLOUR while the number beside it stays chrome: that is what
// keeps the glyph the change's palette-proof signal rather than a second thing the diff colour has
// to carry (ratified calls 6 and 7).
func TestSplitDiffRowsColourTheMarkerWithItsLine(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no-colour profile: there is no styling to assert")
	}
	regions := []domain.EditRegion{{BeforeStart: 7, AfterStart: 7, Removed: []string{"gone"}, Inserted: []string{"here"}}}

	row := splitDiffRows(th, regions, 100)[0]
	if want := th.diffRemoved.Render(stackedRemovedMarker + "gone"); !strings.Contains(row, want) {
		t.Errorf("the removed pane does not carry %q as one styled run: %q", want, row)
	}
	if want := th.diffAdded.Render(stackedInsertedMarker + "here"); !strings.Contains(row, want) {
		t.Errorf("the inserted pane does not carry %q as one styled run: %q", want, row)
	}
	if want := th.toolDetail.Render("7 "); !strings.Contains(row, want) {
		t.Errorf("the number gutter is not painted in the muted role: %q", row)
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

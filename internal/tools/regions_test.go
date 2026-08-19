package tools

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// wantRegion is one expected Edit region, spelled the way a reader checks it: the two start
// lines, then the four line runs.
type wantRegion struct {
	beforeStart int
	afterStart  int
	leading     []string
	removed     []string
	inserted    []string
	trailing    []string
}

// assertRegions fails t unless got matches want region for region. Line runs compare with
// slices.Equal, so an absent run may be spelled nil or empty in the expectation.
func assertRegions(t *testing.T, got []domain.EditRegion, want []wantRegion) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %d regions, want %d: %#v", len(got), len(want), got)
	}
	for i, region := range got {
		expected := want[i]
		if region.BeforeStart != expected.beforeStart || region.AfterStart != expected.afterStart {
			t.Errorf("region %d starts at before %d / after %d, want %d / %d",
				i, region.BeforeStart, region.AfterStart, expected.beforeStart, expected.afterStart)
		}
		for _, run := range []struct {
			name      string
			got, want []string
		}{
			{"Leading", region.Leading, expected.leading},
			{"Removed", region.Removed, expected.removed},
			{"Inserted", region.Inserted, expected.inserted},
			{"Trailing", region.Trailing, expected.trailing},
		} {
			if !slices.Equal(run.got, run.want) {
				t.Errorf("region %d %s = %q, want %q", i, run.name, run.got, run.want)
			}
		}
	}
}

// numberedLines returns count lines named "line 1".."line N" — a file long enough to place
// changes in without the head or the tail of it truncating their context.
func numberedLines(count int) []string {
	lines := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	return lines
}

// TestEditRegions_SingleChangeMidFile pins the ordinary case: one replaced line, three unchanged
// lines of context each side, and start lines that count the leading context in.
func TestEditRegions_SingleChangeMidFile(t *testing.T) {
	t.Parallel()

	oldLines := numberedLines(10)
	newLines := slices.Clone(oldLines)
	newLines[5] = "replaced line 6"

	got := editRegions(strings.Join(oldLines, "\n"), strings.Join(newLines, "\n"))

	assertRegions(t, got.Regions, []wantRegion{{
		beforeStart: 3,
		afterStart:  3,
		leading:     []string{"line 3", "line 4", "line 5"},
		removed:     []string{"line 6"},
		inserted:    []string{"replaced line 6"},
		trailing:    []string{"line 7", "line 8", "line 9"},
	}})
}

// TestEditRegions_AtFileHeadAndTail pins the two ends, where there are fewer than three unchanged
// lines to give: the head takes no leading context and its start lines stay at 1 rather than
// counting backwards past the first line, and the tail takes no trailing context.
func TestEditRegions_AtFileHeadAndTail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		changed int // 0-based index of the replaced line
		want    wantRegion
	}{
		{
			name:    "head",
			changed: 0,
			want: wantRegion{
				beforeStart: 1,
				afterStart:  1,
				removed:     []string{"line 1"},
				inserted:    []string{"replaced"},
				trailing:    []string{"line 2", "line 3", "line 4"},
			},
		},
		{
			name:    "tail",
			changed: 4,
			want: wantRegion{
				beforeStart: 2,
				afterStart:  2,
				leading:     []string{"line 2", "line 3", "line 4"},
				removed:     []string{"line 5"},
				inserted:    []string{"replaced"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			oldLines := numberedLines(5)
			newLines := slices.Clone(oldLines)
			newLines[test.changed] = "replaced"

			got := editRegions(strings.Join(oldLines, "\n"), strings.Join(newLines, "\n"))

			assertRegions(t, got.Regions, []wantRegion{test.want})
		})
	}
}

// regionBeforeEnd reports the last before-file line a region covers, context included — the
// number a renderer compares against the next region's BeforeStart to decide whether the two are
// adjacent in the file or have lines elided between them.
func regionBeforeEnd(region domain.EditRegion) int {
	return region.BeforeStart + len(region.Leading) + len(region.Removed) + len(region.Trailing) - 1
}

// TestEditRegions_NeighboursTileTheirContext pins how two nearby changes share the unchanged
// lines between them (owner decision, 2026-08-19): they stay SEPARATE regions whose context
// ranges TILE that run without overlap — the earlier takes up to three of the lines as its
// trailing context, the later takes what is left as its leading context. A gap of at most six
// lines is covered end to end, so the second region starts on the line after the first one ends
// and a renderer paints the two with no elision separator between them; the seventh line is left
// uncovered and the regions genuinely are apart.
func TestEditRegions_NeighboursTileTheirContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		gap      int
		adjacent bool
		want     []wantRegion
	}{
		{
			name:     "three lines apart, all of them trailing context",
			gap:      3,
			adjacent: true,
			want: []wantRegion{
				{
					beforeStart: 2,
					afterStart:  2,
					leading:     []string{"line 2", "line 3", "line 4"},
					removed:     []string{"line 5"},
					inserted:    []string{"replaced line 5"},
					trailing:    []string{"line 6", "line 7", "line 8"},
				},
				{
					beforeStart: 9,
					afterStart:  9,
					removed:     []string{"line 9"},
					inserted:    []string{"replaced line 9"},
					trailing:    []string{"line 10", "line 11", "line 12"},
				},
			},
		},
		{
			name:     "six lines apart, three each and no line left over",
			gap:      6,
			adjacent: true,
			want: []wantRegion{
				{
					beforeStart: 2,
					afterStart:  2,
					leading:     []string{"line 2", "line 3", "line 4"},
					removed:     []string{"line 5"},
					inserted:    []string{"replaced line 5"},
					trailing:    []string{"line 6", "line 7", "line 8"},
				},
				{
					beforeStart: 9,
					afterStart:  9,
					leading:     []string{"line 9", "line 10", "line 11"},
					removed:     []string{"line 12"},
					inserted:    []string{"replaced line 12"},
					trailing:    []string{"line 13", "line 14", "line 15"},
				},
			},
		},
		{
			name:     "seven lines apart, one line uncovered between them",
			gap:      7,
			adjacent: false,
			want: []wantRegion{
				{
					beforeStart: 2,
					afterStart:  2,
					leading:     []string{"line 2", "line 3", "line 4"},
					removed:     []string{"line 5"},
					inserted:    []string{"replaced line 5"},
					trailing:    []string{"line 6", "line 7", "line 8"},
				},
				{
					beforeStart: 10,
					afterStart:  10,
					leading:     []string{"line 10", "line 11", "line 12"},
					removed:     []string{"line 13"},
					inserted:    []string{"replaced line 13"},
					trailing:    []string{"line 14", "line 15", "line 16"},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			oldText, newText := twoChanges(test.gap)

			got := editRegions(oldText, newText)

			assertRegions(t, got.Regions, test.want)
			if len(got.Regions) != 2 {
				t.Fatalf("got %d regions, want 2", len(got.Regions))
			}
			adjacent := got.Regions[1].BeforeStart == regionBeforeEnd(got.Regions[0])+1
			if adjacent != test.adjacent {
				t.Errorf("region 0 ends at %d and region 1 starts at %d (adjacent=%t), want adjacent=%t",
					regionBeforeEnd(got.Regions[0]), got.Regions[1].BeforeStart, adjacent, test.adjacent)
			}
		})
	}
}

// twoChanges returns a twenty-line file and a copy of it with two lines replaced, gap unchanged
// lines apart — the fixture the tiling and stat-parity tests both place their changes with.
func twoChanges(gap int) (oldText, newText string) {
	const firstChange = 5 // 1-based line of the first change
	secondChange := firstChange + gap + 1

	oldLines := numberedLines(20)
	newLines := slices.Clone(oldLines)
	newLines[firstChange-1] = "replaced " + oldLines[firstChange-1]
	newLines[secondChange-1] = "replaced " + oldLines[secondChange-1]

	return strings.Join(oldLines, "\n"), strings.Join(newLines, "\n")
}

// TestEditRegions_StatMatchesTheLineDiff pins the count invariant the tiling rule exists to keep:
// Removed and Inserted carry changed lines only, never an unchanged line folded in to bridge a
// gap, so the summary's own Stat() agrees with the diffstat unifiedLineDiff counts from the very
// same operations. A nearby pair is the case that would drift first.
func TestEditRegions_StatMatchesTheLineDiff(t *testing.T) {
	t.Parallel()

	blockOld := strings.Join(numberedLines(12), "\n")
	blockNew := strings.Join(append(numberedLines(4), "one", "two"), "\n")

	tests := []struct {
		name             string
		oldText, newText string
	}{
		{name: "two changes six lines apart"},
		{name: "block replaced by a shorter one", oldText: blockOld, newText: blockNew},
		{name: "pure insertion", oldText: "alpha\nbravo", newText: "alpha\ninserted\nbravo"},
	}
	tests[0].oldText, tests[0].newText = twoChanges(6)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, want := unifiedLineDiff(test.oldText, test.newText)

			if got := editRegions(test.oldText, test.newText).Stat(); got != want {
				t.Errorf("Stat() = %+v, want the line diff's %+v", got, want)
			}
		})
	}
}

// TestEditRegions_PureInsertionAndDeletion pins the one-sided cases: a region records the side
// that changed and leaves the other empty, and the start lines still name where the region sits
// in each file, which for an insertion is a before file that never reaches the inserted lines.
func TestEditRegions_PureInsertionAndDeletion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		oldText, newText string
		want             wantRegion
	}{
		{
			name:    "pure insertion",
			oldText: "alpha\nbravo\ncharlie",
			newText: "alpha\ninserted\nbravo\ncharlie",
			want: wantRegion{
				beforeStart: 1,
				afterStart:  1,
				leading:     []string{"alpha"},
				inserted:    []string{"inserted"},
				trailing:    []string{"bravo", "charlie"},
			},
		},
		{
			name:    "pure deletion",
			oldText: "alpha\ndeleted\nbravo\ncharlie",
			newText: "alpha\nbravo\ncharlie",
			want: wantRegion{
				beforeStart: 1,
				afterStart:  1,
				leading:     []string{"alpha"},
				removed:     []string{"deleted"},
				trailing:    []string{"bravo", "charlie"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := editRegions(test.oldText, test.newText)

			assertRegions(t, got.Regions, []wantRegion{test.want})
		})
	}
}

// TestEditRegions_IdenticalInputsYieldNone pins the zero value for an edit that changed nothing:
// no regions, and therefore the zero diffstat the slot reads off them.
func TestEditRegions_IdenticalInputsYieldNone(t *testing.T) {
	t.Parallel()

	got := editRegions("alpha\nbravo\ncharlie", "alpha\nbravo\ncharlie")

	if len(got.Regions) != 0 {
		t.Errorf("Regions = %#v, want none", got.Regions)
	}
	if stat := got.Stat(); stat != (domain.DiffStat{}) {
		t.Errorf("Stat() = %+v, want the zero stat", stat)
	}
}

// TestEditRegions_OverBudgetPairYieldsNone pins the maxDiffTableCells guard: a pair whose LCS
// table would blow the budget comes back with no regions — the renderer's fallback — and nothing
// proportional to the refused table is allocated on the way. The allocation ceiling is the half
// that matters: without the guard the walk still returns regions, just after building the table
// the cap exists to refuse.
func TestEditRegions_OverBudgetPairYieldsNone(t *testing.T) {
	const (
		lines           = 6000 // 6000 x 6000 = 36e6 cells, past the 25e6 budget
		allocationLimit = 32 << 20
	)
	oldLines := make([]string, lines)
	newLines := make([]string, lines)
	for i := range oldLines {
		oldLines[i], newLines[i] = fmt.Sprintf("old %d", i), fmt.Sprintf("new %d", i)
	}
	oldText, newText := strings.Join(oldLines, "\n"), strings.Join(newLines, "\n")

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got := editRegions(oldText, newText)
	runtime.ReadMemStats(&after)

	if len(got.Regions) != 0 {
		t.Errorf("got %d regions, want none for an over-budget pair", len(got.Regions))
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > allocationLimit {
		t.Errorf("allocated %d bytes, want at most %d — the LCS table was built", allocated, allocationLimit)
	}
}

package probe

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// contextCostFixture is a fixed report shaped like a real one — four pieces in wire order, a
// five-figure menu, a total that is one estimate over the sum — so the golden pins the column
// layout on the widths that matter: a four-digit byte count beside a five-digit one, a
// three-digit token estimate beside a four-digit one.
func contextCostFixture() domain.ContextCost {
	return domain.ContextCost{
		Rows: []domain.ContextCostRow{
			{Name: "prompt", Bytes: 1043, Tokens: 261},
			{Name: "orientation", Bytes: 612, Tokens: 153},
			{Name: "context files", Bytes: 4201, Tokens: 1050},
			{Name: "tool menu", Bytes: 11512, Tokens: 2878},
		},
		Bytes:  17368,
		Tokens: 4342,
	}
}

// TestContextCostReportGolden pins the table's shape for the fixture — the header, the column
// alignment, the `~` prefix, the thousands separators — against testdata/contextcost.golden.
// `-update` (tuitest's package-wide flag) rewrites it.
func TestContextCostReportGolden(t *testing.T) {
	t.Parallel()

	report := ContextCost{Estimate: contextCostFixture(), Mode: "auto", CharsPerToken: 4.0}.Report()

	tuitest.GoldenText(t, filepath.Join("testdata", "contextcost.golden"), report)
}

// contextCostCalibratedFixture is the fixture a live reading re-renders: the same bytes through a
// calibrated ratio (3.7 chars/token here), so the tokens column moves while the bytes hold.
func contextCostCalibratedFixture() domain.ContextCost {
	return domain.ContextCost{
		Rows: []domain.ContextCostRow{
			{Name: "prompt", Bytes: 1043, Tokens: 282},
			{Name: "orientation", Bytes: 612, Tokens: 166},
			{Name: "context files", Bytes: 4201, Tokens: 1136},
			{Name: "tool menu", Bytes: 11512, Tokens: 3112},
		},
		Bytes:      17368,
		Tokens:     4695,
		Calibrated: true,
	}
}

// TestContextCostReportMeasuredGolden pins the two live shapes against their goldens: one
// `measured` column carrying a cached share (testdata/contextcost-measured.golden), and the
// `as configured` / `bypass` pair with the delta line (testdata/contextcost-delta.golden). The
// header reads `calibrated` on both.
func TestContextCostReportMeasuredGolden(t *testing.T) {
	t.Parallel()

	t.Run("one column", func(t *testing.T) {
		t.Parallel()

		report := ContextCost{
			Estimate:      contextCostCalibratedFixture(),
			Mode:          "auto",
			CharsPerToken: 3.7,
			Measured: []ContextCostMeasured{
				{Label: ContextCostColumnMeasured, PromptTokens: 4611, CachedPromptTokens: 4096},
			},
		}.Report()

		tuitest.GoldenText(t, filepath.Join("testdata", "contextcost-measured.golden"), report)
	})

	t.Run("two columns", func(t *testing.T) {
		t.Parallel()

		report := ContextCost{
			Estimate:      contextCostCalibratedFixture(),
			Mode:          "auto",
			CharsPerToken: 3.7,
			Armed:         1,
			Measured: []ContextCostMeasured{
				{Label: ContextCostColumnAsConfigured, PromptTokens: 4640},
				{Label: ContextCostColumnBypass, PromptTokens: 4611},
			},
		}.Report()

		tuitest.GoldenText(t, filepath.Join("testdata", "contextcost-delta.golden"), report)
	})
}

// TestContextCostReportMeasuredColumns asserts the live shape semantically: the column line and
// the total row put each measured count under its label, the header says calibrated, the cached
// share rides in brackets, the delta line is the two counts' difference, and the armed line — the
// estimate's pointer at --live — is not printed on a live report.
func TestContextCostReportMeasuredColumns(t *testing.T) {
	t.Parallel()

	report := ContextCost{
		Estimate:      contextCostCalibratedFixture(),
		Mode:          "plan",
		CharsPerToken: 3.7,
		Armed:         2,
		Measured: []ContextCostMeasured{
			{Label: ContextCostColumnAsConfigured, PromptTokens: 4640, CachedPromptTokens: 12},
			{Label: ContextCostColumnBypass, PromptTokens: 4611},
		},
	}.Report()
	lines := strings.Split(report, "\n")

	if !strings.Contains(lines[0], "(mode plan; ~3.7 chars/token, calibrated)") {
		t.Errorf("the header does not label the ratio as calibrated: %q", lines[0])
	}
	columns, total := lines[1], lines[len(lines)-2]
	for _, cell := range []struct{ label, value string }{
		{ContextCostColumnAsConfigured, "4640 (12 cached)"},
		{ContextCostColumnBypass, "4611"},
	} {
		at := strings.Index(columns, cell.label) + len(cell.label)
		if at < len(cell.label) {
			t.Fatalf("the column line does not name %q: %q", cell.label, columns)
		}
		if got := strings.Index(total, cell.value) + len(cell.value); got != at {
			t.Errorf("%q ends at column %d on the total row, its label ends at %d:\n%s\n%s",
				cell.value, got, at, columns, total)
		}
	}
	if last := lines[len(lines)-1]; last != "Reactions add 29 tokens at Turn 1" {
		t.Errorf("the delta line = %q, want the two counts' difference", last)
	}
	if strings.Contains(report, "armed") {
		t.Errorf("a live report must not print the estimate's armed line:\n%s", report)
	}
}

// TestContextCostReportRows asserts the rows semantically, so a re-recorded golden cannot pass a
// table whose columns drifted: every piece is a row, the total closes the table, and the two
// numeric columns line up on their right edges.
func TestContextCostReportRows(t *testing.T) {
	t.Parallel()

	report := ContextCost{Estimate: contextCostFixture(), Mode: "plan", CharsPerToken: 4.0}.Report()
	lines := strings.Split(report, "\n")

	if !strings.Contains(lines[0], "(mode plan; estimate, ~4.0 chars/token)") {
		t.Errorf("the header does not name the mode and the ratio: %q", lines[0])
	}
	wantRows := []string{
		"  prompt              1,043 B   ~261",
		"  orientation           612 B   ~153",
		"  context files       4,201 B  ~1050",
		"  tool menu          11,512 B  ~2878",
		"  total              17,368 B  ~4342",
	}
	if got := lines[1:]; len(got) != len(wantRows) {
		t.Fatalf("the table has %d rows, want %d:\n%s", len(got), len(wantRows), report)
	}
	for i, want := range wantRows {
		if lines[i+1] != want {
			t.Errorf("row %d:\n got %q\nwant %q", i+1, lines[i+1], want)
		}
	}
}

// TestContextCostReportArmedLine pins the trailing line: present, counted and correctly
// pluralised when the configuration arms an advise or shape Reaction, and absent otherwise.
func TestContextCostReportArmedLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		armed int
		want  string
	}{
		{"none", 0, ""},
		{"one", 1, "1 advise/shape Reaction armed — their directives are measured with --live"},
		{"two", 2, "2 advise/shape Reactions armed — their directives are measured with --live"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			report := ContextCost{Estimate: contextCostFixture(), Mode: "auto", CharsPerToken: 4.0, Armed: tc.armed}.Report()

			lines := strings.Split(report, "\n")
			last := lines[len(lines)-1]
			if tc.want == "" {
				if !strings.HasPrefix(last, "  total") {
					t.Errorf("with nothing armed the table must end on the total row, got %q", last)
				}
				return
			}
			if last != tc.want {
				t.Errorf("armed line:\n got %q\nwant %q", last, tc.want)
			}
		})
	}
}

// TestGroupThousands pins the separator spelling on the edges: no separator under four digits,
// one per three digits above, and a zero that stays a bare 0.
func TestGroupThousands(t *testing.T) {
	t.Parallel()

	cases := map[int]string{0: "0", 7: "7", 999: "999", 1000: "1,000", 17368: "17,368", 1234567: "1,234,567"}
	for n, want := range cases {
		if got := groupThousands(n); got != want {
			t.Errorf("groupThousands(%d) = %q, want %q", n, got, want)
		}
	}
}

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

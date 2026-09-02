package tools

import (
	"strings"
	"testing"
)

// closestRegion answers one of three shapes, in the order the report rules are tried: the
// whitespace-only difference named by line, the best-scoring window of trimmed lines, or — when not
// one line of the old text appears — the bare sentence the tools answered with before this existed.
func TestClosestRegion_ReportShapes(t *testing.T) {
	t.Parallel()

	goFile := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"

	cases := []struct {
		name    string
		content string
		old     string
		want    string
	}{
		{
			name:    "whitespace-only difference names the range and quotes it",
			content: goFile,
			old:     "func main() {\n  println(\"hi\")\n}",
			want: "old text not found in file — found at lines 3–5 with different whitespace " +
				"(leading/trailing spaces, tabs or blank lines); the file is unchanged:\n" +
				"  3 | func main() {\n" +
				"  4 | \tprintln(\"hi\")\n" +
				"  5 | }",
		},
		{
			name:    "a blank line inside the old text is dropped before matching",
			content: "alpha\nbeta\ngamma\n",
			old:     "alpha\n\nbeta",
			want: "old text not found in file — found at lines 1–2 with different whitespace " +
				"(leading/trailing spaces, tabs or blank lines); the file is unchanged:\n" +
				"  1 | alpha\n" +
				"  2 | beta",
		},
		{
			name:    "one line off scores as the closest window",
			content: "alpha\nbeta\ngamma\ndelta\n",
			old:     "beta\nGAMMA",
			want: "old text not found in file — closest match at lines 2–3 (1 of 2 lines match); " +
				"the file is unchanged:\n" +
				"  2 | beta\n" +
				"  3 | gamma",
		},
		{
			name:    "the earliest window wins a tie",
			content: "beta\nx\nbeta\ny\n",
			old:     "beta\nz",
			want: "old text not found in file — closest match at lines 1–2 (1 of 2 lines match); " +
				"the file is unchanged:\n" +
				"  1 | beta\n" +
				"  2 | x",
		},
		{
			name:    "a whitespace match found twice is ambiguous and falls through to the window",
			content: "alpha\n  alpha\n",
			old:     "alpha",
			want: "old text not found in file — closest match at lines 1–1 (1 of 1 lines match); " +
				"the file is unchanged:\n" +
				"  1 | alpha",
		},
		{
			name:    "nothing matches at all",
			content: goFile,
			old:     "nowhere near this file",
			want:    "old text not found in file",
		},
		{
			name:    "an all-blank old text points at no region",
			content: "alpha\n\n\nbeta\n",
			old:     "\n\n",
			want:    "old text not found in file",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := closestRegion(tc.content, tc.old)

			if got != tc.want {
				t.Errorf("closestRegion() =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// An excerpt longer than the cap is cut short and closed with the marker, so one failed edit can
// never quote the whole file back at the model.
func TestClosestRegion_ExcerptIsCapped(t *testing.T) {
	t.Parallel()
	var file strings.Builder
	var old strings.Builder
	for n := 1; n <= 20; n++ {
		file.WriteString("line ")
		file.WriteString(string(rune('a' + n - 1)))
		file.WriteString("\n")
		old.WriteString("  line ")
		old.WriteString(string(rune('a' + n - 1)))
		old.WriteString("  \n")
	}

	got := closestRegion(file.String(), strings.TrimSuffix(old.String(), "\n"))

	rows := strings.Split(got, "\n")
	if len(rows) != maxNearMatchExcerptLines+2 { // the header, the capped rows, the marker
		t.Fatalf("report has %d lines, want %d:\n%s", len(rows), maxNearMatchExcerptLines+2, got)
	}
	if rows[len(rows)-1] != nearMatchCutMarker {
		t.Errorf("last row = %q, want the cut marker %q", rows[len(rows)-1], nearMatchCutMarker)
	}
	if !strings.Contains(rows[0], "lines 1–20") {
		t.Errorf("header %q does not name the full range it cut short", rows[0])
	}
}

// A quoted file line is DATA inside the report's row grammar: a stray carriage return in the file
// must not forge a row of its own.
func TestClosestRegion_ExcerptEscapesRowBreaks(t *testing.T) {
	t.Parallel()

	got := closestRegion("alpha\nbeta\rforged row\n", "alpha\nBETA")

	if strings.Contains(got, "\r") {
		t.Errorf("report carries a raw carriage return: %q", got)
	}
	if !strings.Contains(got, `beta\rforged row`) {
		t.Errorf("report does not spell the escaped line: %q", got)
	}
}

// occurrenceLines names the line of every non-overlapping occurrence's first character, counting
// exactly as countOccurrences does so a "found N times" refusal and its lines cannot disagree.
func TestOccurrenceLines(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
		old     string
		want    []int
	}{
		{"none", "alpha\nbeta\n", "gamma", nil},
		{"twice on one line", "aaa bbb aaa\n", "aaa", []int{1, 1}},
		{"across lines", "x\nneedle\ny\nneedle\n", "needle", []int{2, 4}},
		{"non-overlapping", "aaaa\n", "aa", []int{1, 1}},
		{"empty old text", "alpha\n", "", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := occurrenceLines(tc.content, tc.old)

			if len(got) != len(tc.want) {
				t.Fatalf("occurrenceLines() = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("occurrenceLines() = %v, want %v", got, tc.want)
				}
			}
			if n := countOccurrences(tc.content, tc.old); n != len(got) {
				t.Errorf("countOccurrences() = %d but %d lines were named", n, len(got))
			}
		})
	}
}

// The clause a "found N times" refusal gains is empty when there is nothing to name, so the former
// wording survives to the byte.
func TestOccurrenceNote(t *testing.T) {
	t.Parallel()

	if got := occurrenceNote("x\nneedle\ny\nneedle\n", "needle"); got != " — at lines 2, 4" {
		t.Errorf("occurrenceNote() = %q", got)
	}
	if got := occurrenceNote("alpha\n", "gamma"); got != "" {
		t.Errorf("occurrenceNote() with no occurrence = %q, want the empty clause", got)
	}
}

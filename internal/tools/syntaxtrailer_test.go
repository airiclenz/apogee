package tools

import (
	"strings"
	"testing"
)

// The trailer is silent wherever it has nothing certain to say: a language the checker does not
// know, and content it finds well formed. Those two cases are what keeps every existing write
// result byte-identical to what it was before the trailer existed.
func TestSyntaxTrailerSaysNothingWhereThereIsNothingToSay(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		path    string
		content string
	}{
		{"unknown language", "notes.txt", "func main( {"},
		{"markdown", "README.md", "# heading ((("},
		{"valid go", "main.go", "package main\n\nfunc main() {}\n"},
		{"valid javascript", "app.js", "function f() { return 1; }\n"},
		{"empty content", "main.go", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := syntaxTrailer(tc.path, tc.content); got != "" {
				t.Errorf("syntaxTrailer = %q, want no trailer", got)
			}
		})
	}
}

// Go is checked by the real parser, so its header states the verdict plainly and each problem
// carries the line it fell on. The trailer opens on its own line so it never runs into the
// sentence it rides behind.
func TestSyntaxTrailerReportsGoProblemsWithTheirLines(t *testing.T) {
	t.Parallel()

	got := syntaxTrailer("broken.go", "package main\n\nfunc main() {}\n}\n")

	if !strings.HasPrefix(got, "\nsyntax check: ") {
		t.Fatalf("trailer = %q, want it to open a line with the plain header", got)
	}
	if !strings.Contains(got, "syntax check: 1 problem") {
		t.Errorf("trailer = %q, want the problem count", got)
	}
	if !strings.Contains(got, "\n  line 4: ") {
		t.Errorf("trailer = %q, want an indented located problem", got)
	}
}

// Every non-Go language is the bracket heuristic, which is known to false-positive (a JS/TS regex
// literal is the recorded case). The header must say so, so a model never reads a guess as a
// finding.
func TestSyntaxTrailerNamesTheHeuristicForNonGoLanguages(t *testing.T) {
	t.Parallel()

	got := syntaxTrailer("app.js", "function f() {\n  return 1;\n")

	if !strings.Contains(got, "syntax check (heuristic): ") {
		t.Errorf("trailer = %q, want the heuristic header for javascript", got)
	}
	if strings.Contains(got, "\nsyntax check: ") {
		t.Errorf("trailer = %q, want the plain header reserved for the real parser", got)
	}
}

// A wrecked file can cascade into dozens of parser errors; the trailer places the first ten and
// counts the rest, so one bad write cannot flood the Turn it reports into.
func TestSyntaxTrailerCapsTheProblemsItSpells(t *testing.T) {
	t.Parallel()

	got := syntaxTrailer("broken.go", "package main\n\n"+strings.Repeat("func (\n", 40))

	lines := strings.Split(strings.TrimPrefix(got, "\n"), "\n")
	if len(lines) != maxSyntaxTrailerErrors+2 { // header + capped problems + the "and N more" tail
		t.Fatalf("trailer = %q, want %d lines", got, maxSyntaxTrailerErrors+2)
	}
	if tail := lines[len(lines)-1]; !strings.HasPrefix(tail, "  … and ") || !strings.HasSuffix(tail, " more") {
		t.Errorf("last line = %q, want the elided remainder", tail)
	}
}

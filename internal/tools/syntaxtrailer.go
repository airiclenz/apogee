package tools

import (
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/syntaxcheck"
)

// maxSyntaxTrailerErrors is how many located problems a trailer spells before it stops counting
// them one by one. A write that breaks a file breaks it in one place or in a cascade; ten lines
// place the first cause and the tail is a number, so a wrecked file cannot flood the Turn it
// reports into.
const maxSyntaxTrailerErrors = 10

// syntaxTrailer is the structural feedback a write tool appends to its OWN success sentence: the
// in-process syntax verdict on the bytes it just wrote, read straight from the file the model
// asked for. It is never a refusal and never an error — the write landed as asked, and the trailer
// only tells the model what the file now says — so a caller folds it into the prose half of a
// success result and nothing else about that result changes.
//
// Empty for a path whose language the checker does not know and for content it finds valid, so a
// `.txt` write and a well-formed `.go` write read exactly as they did before this existed.
//
// Go carries the real parser's verdict and says so plainly; every other language is the
// bracket/string heuristic, which is known to false-positive (a JS/TS regex literal, for one), so
// its header names itself a heuristic rather than letting a model read a guess as a finding.
func syntaxTrailer(path, content string) string {
	result := syntaxcheck.Check(path, content)
	if result.Language == "" || result.Valid {
		return ""
	}

	header := "syntax check:"
	if result.Language != "go" {
		header = "syntax check (heuristic):"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n%s %d problem(s)", header, len(result.Errors))
	for i, e := range result.Errors {
		if i == maxSyntaxTrailerErrors {
			fmt.Fprintf(&b, "\n  … and %d more", len(result.Errors)-i)
			break
		}
		fmt.Fprintf(&b, "\n  line %d: %s", e.Line, e.Message)
	}
	return b.String()
}

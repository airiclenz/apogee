package tuitest

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Goldens are for RENDERING surfaces and nothing else (ADR 0062): a pane whose whole point is how
// it looks — column alignment, box drawing, the shape of a row. Everything else is asserted
// semantically, because a golden that pins behaviour fails on every unrelated wording change and
// is then updated without being read, which is worse than no test at all.

// updateGolden rewrites goldens instead of comparing against them: `go test ./cmd/apogee -update`.
// It is registered here, once, so every package that takes goldens gets the same flag spelled the
// same way.
var updateGolden = flag.Bool("update",
	false, "rewrite the golden frames under testdata/frames/ instead of comparing against them")

// goldenDir is where a package's goldens live, relative to the package directory — which is what
// `go test` makes the working directory.
var goldenDir = filepath.Join("testdata", "frames")

// Redaction replaces everything Pattern matches with With, before a frame is compared or written.
// A frame carries a temp home, today's date and an age in minutes; without redaction a golden
// churns on every run and stops meaning anything (plan item 4, A7).
type Redaction struct {
	Pattern *regexp.Regexp
	With    string

	// KeepWidth pads With back out to the columns the match itself occupied, instead of
	// substituting it as-is. It is what makes a redacted value that a surface pads a column out to
	// immune to its own width — see [RedactPadded], which is how one is built.
	KeepWidth bool
}

// Redact builds a [Redaction] from a pattern, panicking on a bad one the way regexp.MustCompile
// does — a redaction set is written once, beside the driver, and a broken one is a build-time
// mistake rather than a test outcome.
func Redact(pattern, with string) Redaction {
	return Redaction{Pattern: regexp.MustCompile(pattern), With: with}
}

// RedactPadded is [Redact] for a value a surface pads a column out to — the build version in the
// start-up card, where the value and the spaces behind it together fill a fixed number of columns
// up to a border. Such a value redacts its TEXT but not its WIDTH: a version one column longer
// than yesterday's eats one of those spaces, and every golden it appears in goes red on a diff
// that is nothing but a moved border (v0.18.9 → v0.18.10 cost three re-recorded frames). So the
// run of spaces behind the value is swallowed with it and the token is padded back out to the
// same width, which does not depend on the value's own.
//
// The token is never cut to fit: where the run it replaces is narrower than the token (a value at
// the end of a row, whose padding the frame already trimmed) the line grows by the token's excess
// — the same excess at every value width, so the golden is stable there too. What this does not
// suit is a value followed by a single separating space and then more text: the space is part of
// the run and would be swallowed. Use [Redact] for those.
//
// The pattern is wrapped in a non-capturing group before the run of spaces is appended, because
// Go's alternation binds looser than concatenation: an ungrouped `foo|bar` would compile as
// `foo` or `bar *` and silently drop the padding swallow from the first alternative, which is a
// width-unstable redaction that reports no error at all. Grouping adds no submatch, so a pattern's
// own capture-group numbering is untouched.
func RedactPadded(pattern, with string) Redaction {
	return Redaction{Pattern: regexp.MustCompile(`(?:` + pattern + `) *`), With: with, KeepWidth: true}
}

// ApplyRedactions runs every redaction over text, in order.
func ApplyRedactions(text string, redact ...Redaction) string {
	for _, r := range redact {
		if r.Pattern == nil {
			continue
		}
		if r.KeepWidth {
			text = r.Pattern.ReplaceAllStringFunc(text, func(match string) string {
				return padToWidth(r.With, ansi.StringWidth(match))
			})
			continue
		}
		text = r.Pattern.ReplaceAllString(text, r.With)
	}
	return text
}

// padToWidth is s with enough trailing spaces to fill width columns, and s untouched when it
// already fills them or more: a redacted token that got cut would hide what it stood for.
func padToWidth(s string, width int) string {
	if pad := width - ansi.StringWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// Golden compares a frame's plain text — redacted — against testdata/frames/<name>.txt in the
// calling package, and fails with a diff when they differ. With -update it rewrites the file
// instead, redactions and all: what is on disk is what a comparison will see.
func Golden(t testing.TB, name string, frame Frame, redact ...Redaction) {
	t.Helper()

	compareGolden(t, goldenDir, name, frame.String(), *updateGolden, redact)
}

// compareGolden is Golden's body with its two ambient inputs — the directory and the flag — passed
// in, so the golden machinery can be tested without a golden of its own.
func compareGolden(t testing.TB, dir, name, text string, update bool, redact []Redaction) {
	t.Helper()

	got := ApplyRedactions(text, redact...)
	path := filepath.Join(dir, name+".txt")
	if update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("updated %s", path)
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden frame %s: %v — run the package's tests with -update to record it", path, err)
		return
	}
	want := strings.TrimSuffix(string(raw), "\n")
	if got == want {
		return
	}
	t.Errorf("frame %q does not match %s:\n%s", name, path, unifiedDiff(want, got))
}

// unifiedDiff is a line diff of want against got, marked the way a patch is. It is a small
// longest-common-subsequence walk rather than a dependency: a frame is tens of lines, and a
// failure that prints two whole screens and lets the reader find the difference is not a failure
// anybody reads.
func unifiedDiff(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")
	// lcs[i][j] is the length of the longest common subsequence of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
				continue
			}
			lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
		}
	}
	var out strings.Builder
	out.WriteString("--- want\n+++ got\n")
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out.WriteString("  " + a[i] + "\n")
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out.WriteString("- " + a[i] + "\n")
			i++
		default:
			out.WriteString("+ " + b[j] + "\n")
			j++
		}
	}
	for ; i < len(a); i++ {
		out.WriteString("- " + a[i] + "\n")
	}
	for ; j < len(b); j++ {
		out.WriteString("+ " + b[j] + "\n")
	}
	return out.String()
}

package tuitest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestGoldenComparesTheRedactedFrame: a golden holds the frame AFTER redaction, so a run whose
// temp home differs from the recorded one still matches. Without this every golden churns on
// every run and stops being read (plan item 4, A7).
func TestGoldenComparesTheRedactedFrame(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "frames")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pane.txt"), []byte("workspace <ws>\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	home := t.TempDir()
	redact := []Redaction{Redact(regexp.QuoteMeta(home), "<ws>")}
	compareGolden(t, dir, "pane", "workspace "+home, false, redact)
}

// TestGoldenUpdateRecordsTheRedactedText: -update writes what a comparison will later read —
// redactions applied. A golden recorded raw would fail on the very next run.
func TestGoldenUpdateRecordsTheRedactedText(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "frames")
	home := t.TempDir()
	redact := []Redaction{Redact(regexp.QuoteMeta(home), "<ws>")}
	compareGolden(t, dir, "pane", "workspace "+home, true, redact)

	raw, err := os.ReadFile(filepath.Join(dir, "pane.txt"))
	if err != nil {
		t.Fatalf("the update did not write the golden: %v", err)
	}
	if got, want := string(raw), "workspace <ws>\n"; got != want {
		t.Errorf("recorded golden = %q, want %q", got, want)
	}
	// And what was recorded compares clean on the next run.
	compareGolden(t, dir, "pane", "workspace "+home, false, redact)
}

// TestUnifiedDiffMarksBothSides: a mismatch prints a diff, not two screens for the reader to
// compare by eye.
func TestUnifiedDiffMarksBothSides(t *testing.T) {
	t.Parallel()

	diff := unifiedDiff("one\ntwo\nthree", "one\nTWO\nthree")
	for _, want := range []string{"--- want", "+++ got", "  one", "- two", "+ TWO", "  three"} {
		if !strings.Contains(diff, want) {
			t.Errorf("the diff does not carry %q:\n%s", want, diff)
		}
	}
}

// TestApplyRedactionsRunsInOrder: redactions compose, and a nil pattern is skipped rather than
// panicking a test run at the worst moment.
func TestApplyRedactionsRunsInOrder(t *testing.T) {
	t.Parallel()

	got := ApplyRedactions("Session 2026-08-27 · 3 min ago",
		Redact(`Session \d{4}-\d{2}-\d{2}`, "Session <date>"),
		Redaction{},
		Redact(`\d+ min ago`, "<age>"))
	if want := "Session <date> · <age>"; got != want {
		t.Errorf("ApplyRedactions = %q, want %q", got, want)
	}
}

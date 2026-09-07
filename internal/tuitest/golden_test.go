package tuitest

import (
	"fmt"
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
	compareGolden(t, dir, "pane", ".txt", "workspace "+home, false, redact)
}

// TestGoldenUpdateRecordsTheRedactedText: -update writes what a comparison will later read —
// redactions applied. A golden recorded raw would fail on the very next run.
func TestGoldenUpdateRecordsTheRedactedText(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "frames")
	home := t.TempDir()
	redact := []Redaction{Redact(regexp.QuoteMeta(home), "<ws>")}
	compareGolden(t, dir, "pane", ".txt", "workspace "+home, true, redact)

	raw, err := os.ReadFile(filepath.Join(dir, "pane.txt"))
	if err != nil {
		t.Fatalf("the update did not write the golden: %v", err)
	}
	if got, want := string(raw), "workspace <ws>\n"; got != want {
		t.Errorf("recorded golden = %q, want %q", got, want)
	}
	// And what was recorded compares clean on the next run.
	compareGolden(t, dir, "pane", ".txt", "workspace "+home, false, redact)
}

// TestGoldenTextRoundTrips: a golden that is neither under testdata/frames/ nor a `.txt` file
// records and compares through the same machinery. The Event lines contract's goldens are
// `testdata/eventlines/*.jsonl` (ADR 0075 §14), and before GoldenText the directory and the
// extension were hard-coded — a caller could reach neither.
func TestGoldenTextRoundTrips(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "eventlines")
	session := "s-20260907-120000"
	line := `{"event":"run_finished","session":"` + session + `"}`
	redact := []Redaction{Redact(regexp.QuoteMeta(session), "<session>")}

	compareGolden(t, dir, "run", ".jsonl", line, true, redact)

	raw, err := os.ReadFile(filepath.Join(dir, "run.jsonl"))
	if err != nil {
		t.Fatalf("the update did not write the golden: %v", err)
	}
	if got, want := string(raw), `{"event":"run_finished","session":"<session>"}`+"\n"; got != want {
		t.Errorf("recorded golden = %q, want %q", got, want)
	}
	// And a second run, with a session id of its own, compares clean against what was recorded.
	other := "s-20260908-093000"
	compareGolden(t, dir, "run", ".jsonl", `{"event":"run_finished","session":"`+other+`"}`, false,
		[]Redaction{Redact(regexp.QuoteMeta(other), "<session>")})
}

// setUpdateGolden swaps the package-level -update flag for one test and puts it back afterwards.
// GoldenText reads that flag rather than taking it as an argument — which is why the tests below
// cannot run in parallel — and pinning it here also makes them independent of a run that was itself
// invoked with -update, where an unpinned comparison would silently rewrite its fixture instead.
func setUpdateGolden(t *testing.T, update bool) {
	t.Helper()

	prev := *updateGolden
	t.Cleanup(func() { *updateGolden = prev })
	*updateGolden = update
}

// errorRecorder stands in for the *testing.T a real caller hands [GoldenText], so a MISMATCH can be
// asserted: a real T would fail the very test that is pinning the failure.
type errorRecorder struct {
	// The embedded TB is nil on purpose. GoldenText's comparison path calls only Helper, Errorf and
	// Fatalf; a call to any other one should panic here rather than quietly do nothing.
	testing.TB

	msg string
}

// Helper is a no-op: there is no real test frame to attribute failures to.
func (r *errorRecorder) Helper() {}

// Errorf records the message instead of failing.
func (r *errorRecorder) Errorf(format string, args ...any) {
	r.msg = fmt.Sprintf(format, args...)
}

// Fatalf records the message and returns, which testing.T would not: compareGolden returns of its
// own accord right after the one Fatalf a comparison can reach, so nothing runs on past it here
// either — and a golden the split sent to the wrong path is then a readable assertion failure
// rather than a nil-TB panic that takes the rest of the package's run with it.
func (r *errorRecorder) Fatalf(format string, args ...any) {
	r.msg = fmt.Sprintf(format, args...)
}

// TestGoldenTextReadsTheNamedDirectoryAndExtension: the golden [GoldenText] compares against is the
// file the caller named — its own directory, its own extension — and not testdata/frames/<name>.txt.
// Those two are ambient inside compareGolden, so a GoldenText that dropped either would still find
// a golden in every package that also takes frames, and the split it does itself is exercised by
// nothing that drives compareGolden directly (apogee-70d).
func TestGoldenTextReadsTheNamedDirectoryAndExtension(t *testing.T) {
	setUpdateGolden(t, false)

	dir := filepath.Join(t.TempDir(), "eventlines")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "run.jsonl")
	if err := os.WriteFile(path, []byte(`{"event":"run_finished","session":"<session>"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	session := "s-20260907-120000"
	GoldenText(t, path, `{"event":"run_finished","session":"`+session+`"}`,
		Redact(regexp.QuoteMeta(session), "<session>"))
}

// TestGoldenTextFailsWithADiffNamingTheGolden: a mismatch reports the path the caller named and the
// diff, and it names the golden by the stem with the extension taken off — a name that still carried
// its `.jsonl` would read as a file beside the one the message points at.
func TestGoldenTextFailsWithADiffNamingTheGolden(t *testing.T) {
	setUpdateGolden(t, false)

	dir := filepath.Join(t.TempDir(), "eventlines")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "run.jsonl")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rec := &errorRecorder{}
	GoldenText(rec, path, "one\nTWO")

	for _, want := range []string{path, `"run"`, "- two", "+ TWO"} {
		if !strings.Contains(rec.msg, want) {
			t.Errorf("the mismatch message does not carry %q:\n%s", want, rec.msg)
		}
	}
}

// TestGoldenTextUpdateWritesTheNamedPath: -update records to the same path a comparison then reads,
// creating the caller's directory on the way — and writes NOTHING beside it, which is what catches
// a split that kept the frame extension or left the stem's own one on the end.
func TestGoldenTextUpdateWritesTheNamedPath(t *testing.T) {
	setUpdateGolden(t, true)

	dir := filepath.Join(t.TempDir(), "eventlines")
	path := filepath.Join(dir, "run.jsonl")
	session := "s-20260907-120000"
	redact := []Redaction{Redact(regexp.QuoteMeta(session), "<session>")}
	line := `{"event":"run_finished","session":"` + session + `"}`

	GoldenText(t, path, line, redact...)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the update did not write the golden at the path GoldenText was handed: %v", err)
	}
	if got, want := string(raw), `{"event":"run_finished","session":"<session>"}`+"\n"; got != want {
		t.Errorf("recorded golden = %q, want %q", got, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != "run.jsonl" {
		t.Errorf("the update left %v in the golden's directory, want only run.jsonl", names)
	}

	// And what was recorded compares clean through GoldenText itself, with the flag back down.
	*updateGolden = false
	GoldenText(t, path, line, redact...)
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

// TestRedactPaddedHoldsTheColumnAtEveryValueWidth: a value the surface pads out to a border takes
// the padding with it when it is redacted, so the border stays where it was however wide the value
// is. Redacting the text alone moves the border by the value's own drift and reds every golden the
// surface appears in on a diff that says nothing (v0.18.9 → v0.18.10 cost three frames).
func TestRedactPaddedHoldsTheColumnAtEveryValueWidth(t *testing.T) {
	t.Parallel()

	row := func(value string) string {
		return "version  " + value + strings.Repeat(" ", 12-len(value)) + "│"
	}
	short := ApplyRedactions(row("v0.18.9"), RedactPadded(regexp.QuoteMeta("v0.18.9"), "<version>"))
	grown := ApplyRedactions(row("v0.18.10"), RedactPadded(regexp.QuoteMeta("v0.18.10"), "<version>"))
	if want := "version  <version>   │"; short != want {
		t.Errorf("the short version redacted to %q, want %q", short, want)
	}
	if short != grown {
		t.Errorf("the column moved when the value grew: %q vs %q", short, grown)
	}
}

// TestRedactPaddedNeverCutsTheToken: where the run it replaces is narrower than the token itself —
// a value at the end of a row, whose padding the frame already trimmed — the token stays whole. A
// cut token would read as a value rather than as a redaction, which is the one thing a redaction
// must never do.
func TestRedactPaddedNeverCutsTheToken(t *testing.T) {
	t.Parallel()

	got := ApplyRedactions("apogee v0.18.10", RedactPadded(regexp.QuoteMeta("v0.18.10"), "<version>"))
	if want := "apogee <version>"; got != want {
		t.Errorf("ApplyRedactions = %q, want %q", got, want)
	}
}

// TestRedactPaddedGroupsThePattern: an alternation the caller writes binds as ONE alternative set
// against the padding, not as `first` or `second <padding>`. Go's alternation is looser than
// concatenation, so an ungrouped pattern would swallow the padding behind the last alternative
// only — every other one redacts its text but not its width, silently, with no error to notice.
func TestRedactPaddedGroupsThePattern(t *testing.T) {
	t.Parallel()

	row := func(value string) string {
		return "model  " + value + strings.Repeat(" ", 10-len(value)) + "│"
	}
	first := ApplyRedactions(row("alpha"), RedactPadded(`alpha|beta`, "<model>"))
	second := ApplyRedactions(row("beta"), RedactPadded(`alpha|beta`, "<model>"))
	if want := "model  <model>   │"; first != want {
		t.Errorf("the first alternative redacted to %q, want %q", first, want)
	}
	if first != second {
		t.Errorf("the column moved between alternatives: %q vs %q", first, second)
	}
}

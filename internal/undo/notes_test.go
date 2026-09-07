package undo

import (
	"strings"
	"testing"
)

// scriptedStep is the preview the listing tests render: one restore, one delete and one skip, so
// every classification a row can carry is exercised by a single case.
func scriptedStep() Step {
	return Step{
		Ordinal:    3,
		Generation: 7,
		Changes: []Change{
			{Path: "/w/a.go", Action: ActionRestore},
			{Path: "/w/new.go", Action: ActionDelete},
			{Path: "/w/b.go", Action: ActionSkip, Reason: "edited since"},
		},
	}
}

func TestPreviewLinesDiscloseEveryRecordedPath(t *testing.T) {
	t.Parallel()

	got := PreviewLines(scriptedStep())

	// The listing IS the authorization surface, so every path appears with what would happen to
	// it — and the actions line up, because a column a human can scan is what makes it readable.
	want := []string{
		"exchange 3:",
		"  restore /w/a.go",
		"  delete  /w/new.go",
		"  skip    /w/b.go — edited since",
	}
	assertLines(t, got, want)
}

// The lines name no command: the verb and the line that applies the step are the Driver's, which
// is what lets `/undo`, `/redo` and `apogee undo` share one listing.
func TestPreviewLinesNameNoCommand(t *testing.T) {
	t.Parallel()

	for _, line := range PreviewLines(scriptedStep()) {
		if strings.Contains(line, "/undo") || strings.Contains(line, "/redo") || strings.Contains(line, "apogee") {
			t.Errorf("the listing names a Driver's verb: %q", line)
		}
	}
}

func TestReportLinesCountTheOutcomeAndNameEverySkip(t *testing.T) {
	t.Parallel()

	got := ReportLines(Report{
		Ordinal:  2,
		Restored: []string{"/w/a.go", "/w/c.go"},
		Deleted:  []string{"/w/new.go"},
		Skipped: []Skipped{
			{Path: "/w/b.go", Reason: "edited since"},
			{Path: "/w/d.go", Reason: "permission denied"},
		},
	})

	want := []string{
		"exchange 2: 2 restored, 1 removed, 2 skipped",
		"  skip    /w/b.go — edited since",
		"  skip    /w/d.go — permission denied",
	}
	assertLines(t, got, want)
}

func TestNothingLinesNameTheReasonWhenThereIsOne(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		reason string
		want   string
	}{
		{
			name: "snapshots in force",
			want: "nothing to undo — no agent file writes are recorded for this session",
		},
		{
			name:   "the store could not be had",
			reason: "git not found",
			want:   "nothing to undo — no agent file writes are recorded for this session (git not found)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := NothingLines(c.reason)

			// A thinner journal must not read as a broken one: the reason is what separates them.
			assertLines(t, got, []string{c.want})
		})
	}
}

// assertLines compares a rendered listing against the lines it must be, exactly and in order.
func assertLines(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("lines =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

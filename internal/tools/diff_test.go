package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func TestViewDiff_ReportsChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "line one\nline two\nline three\n")

	result, err := NewViewDiff(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "newContent": "line one\nline TWO\nline three\n"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	// The middle line changed: it must show as a removal and an addition, with the
	// unchanged lines as context.
	if !strings.Contains(result.Content, "- line two") {
		t.Errorf("diff missing removal of the old line:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "+ line TWO") {
		t.Errorf("diff missing addition of the new line:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "  line one") {
		t.Errorf("diff missing the unchanged context line:\n%s", result.Content)
	}
}

func TestViewDiff_NoChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "same\ncontent\n")

	result, err := NewViewDiff(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "newContent": "same\ncontent\n"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "No changes") {
		t.Errorf("identical content should report no changes, got %q", result.Content)
	}
}

// TestViewDiff_ReportsDiffStat pins the structured half of view_diff's outcome. The stat is
// counted from the diff OPERATIONS, so the test also asserts the equality that makes it
// trustworthy: it matches what counting leading "+"/"-" over the rendered text yields, which
// is what a reader had to do before the summary existed.
func TestViewDiff_ReportsDiffStat(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "one\ntwo\nthree\n")
	tool := NewViewDiff(root)

	cases := []struct {
		name        string
		newContent  string
		wantContent string
		wantSummary domain.DiffStat
	}{
		{
			name:        "line replaced",
			newContent:  "one\nTWO\nthree\n",
			wantContent: "  one\n- two\n+ TWO\n  three\n  ",
			wantSummary: domain.DiffStat{Added: 1, Removed: 1},
		},
		{
			name:        "pure addition",
			newContent:  "one\ntwo\nthree\nfour\n",
			wantContent: "  one\n  two\n  three\n+ four\n  ",
			wantSummary: domain.DiffStat{Added: 1},
		},
		{
			name:        "pure deletion",
			newContent:  "one\nthree\n",
			wantContent: "  one\n- two\n  three\n  ",
			wantSummary: domain.DiffStat{Removed: 1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(),
				callWith(t, "c1", map[string]any{"path": "f.txt", "newContent": tc.newContent}))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.Content != tc.wantContent {
				t.Errorf("Content = %q, want %q", result.Content, tc.wantContent)
			}
			stat, ok := result.Summary.(domain.DiffStat)
			if !ok {
				t.Fatalf("Summary = %#v, want a domain.DiffStat", result.Summary)
			}
			if stat != tc.wantSummary {
				t.Errorf("Summary = %+v, want %+v", stat, tc.wantSummary)
			}
			if added, removed := countDiffTags(result.Content); added != stat.Added || removed != stat.Removed {
				t.Errorf("stat %+v disagrees with the rendered text (+%d -%d)", stat, added, removed)
			}
		})
	}
}

// countDiffTags counts the added and removed lines of a rendered diff the way a reader of
// the text has to — by their leading "+"/"-".
func countDiffTags(rendered string) (added, removed int) {
	for _, line := range strings.Split(rendered, "\n") {
		switch {
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

// TestViewDiff_NoChangesCarriesNoSummary: identical content is not a diff, so the sentinel
// result carries no summary at all and a host renders its sentence as prose.
func TestViewDiff_NoChangesCarriesNoSummary(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "same\ncontent\n")

	result, err := NewViewDiff(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "newContent": "same\ncontent\n"}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.Content != "No changes detected" {
		t.Fatalf("Content = %q, want the no-changes sentinel", result.Content)
	}
	if result.Summary != nil {
		t.Errorf("Summary = %#v, want nil for the no-changes sentinel", result.Summary)
	}
}

// TestViewDiff_Deterministic proves the diff output is stable across repeated calls — the
// LCS table fully determines the ordering (no map iteration, no time-dependence).
func TestViewDiff_Deterministic(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "a\nb\nc\nd\ne\n")
	tool := NewViewDiff(root)
	call := callWith(t, "c1", map[string]any{"path": "f.txt", "newContent": "a\nX\nc\nd\nY\n"})

	first, err := tool.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, err := tool.Execute(context.Background(), call)
		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		if again.Content != first.Content {
			t.Fatalf("diff is not deterministic:\nfirst:\n%s\nlater:\n%s", first.Content, again.Content)
		}
	}
}

func TestUnifiedLineDiff_PureInsertionAndDeletion(t *testing.T) {
	t.Parallel()

	// Pure addition.
	if got, _ := unifiedLineDiff("a\nb", "a\nb\nc"); !strings.Contains(got, "+ c") {
		t.Errorf("addition diff = %q, want a + c line", got)
	}
	// Pure deletion.
	if got, _ := unifiedLineDiff("a\nb\nc", "a\nc"); !strings.Contains(got, "- b") {
		t.Errorf("deletion diff = %q, want a - b line", got)
	}
	// Identical → empty.
	if got, _ := unifiedLineDiff("same", "same"); got != "" {
		t.Errorf("identical diff = %q, want empty", got)
	}
}

// TestViewDiff_RefusesOversizeFile: the old side is read through the same bounded one-handle
// read the read tools use, so a file past maxFileReadBytes is refused with that read's message
// and never reaches the diff. The file is sparse (a Truncate with nothing written), so the test
// costs no 10 MiB write. It fails against the pre-change code, which read the whole file with a
// plain os.ReadFile and no size check at all.
func TestViewDiff_RefusesOversizeFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	f, err := os.Create(filepath.Join(root, "big.bin"))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := f.Truncate(maxFileReadBytes + 1); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := NewViewDiff(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "big.bin", "newContent": "x"}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want true: a file past the read cap was diffed")
	}
	want := fmt.Sprintf("file too large: %d bytes (max %d)", int64(maxFileReadBytes)+1, int64(maxFileReadBytes))
	if result.Content != want {
		t.Errorf("Content = %q, want %q", result.Content, want)
	}
}

// TestViewDiff_RefusesOversizeNewContent: model-authored content is held to maxFileContentBytes,
// the ceiling the write tools apply — and the refusal lands before the file is even read, so an
// oversized proposal costs nothing. Fails against the pre-change code, which had no such check.
func TestViewDiff_RefusesOversizeNewContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTempFile(t, root, "f.txt", "small\n")

	result, err := NewViewDiff(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "newContent": strings.Repeat("x", maxFileContentBytes+1)}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want true: oversized newContent was diffed")
	}
	want := fmt.Sprintf("newContent too large: %d bytes (max %d)", maxFileContentBytes+1, maxFileContentBytes)
	if result.Content != want {
		t.Errorf("Content = %q, want %q", result.Content, want)
	}
}

// TestViewDiff_DegradesOverBudgetToDiffstat: a pair whose LCS table would exceed
// maxDiffTableCells comes back as a diffstat with a sentence saying the rendering was withheld —
// and, the point of the cap, WITHOUT allocating the table. 6000 x 6000 lines is the audit's
// measured case: the table alone is ~288 MiB, so the allocation ceiling asserted here (32 MiB)
// cannot be met by any implementation that builds it. Deliberately not parallel — it reads
// process-wide allocation counters.
func TestViewDiff_DegradesOverBudgetToDiffstat(t *testing.T) {
	const (
		lines       = 6000 // 6000 x 6000 = 36e6 cells, past the 25e6 budget
		shared      = 10   // identical lines at each end, so the head/tail trim has something to find
		allocCeling = 32 << 20
	)
	oldLines := make([]string, lines)
	newLines := make([]string, lines)
	for i := range oldLines {
		if i < shared || i >= lines-shared {
			oldLines[i], newLines[i] = fmt.Sprintf("same %d", i), fmt.Sprintf("same %d", i)
			continue
		}
		oldLines[i], newLines[i] = fmt.Sprintf("old %d", i), fmt.Sprintf("new %d", i)
	}

	root := t.TempDir()
	writeTempFile(t, root, "big.txt", strings.Join(oldLines, "\n"))
	call := callWith(t, "c1", map[string]any{"path": "big.txt", "newContent": strings.Join(newLines, "\n")})

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	result, err := NewViewDiff(root).Execute(context.Background(), call)
	runtime.ReadMemStats(&after)

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("IsError = true, want a degraded success result: %q", result.Content)
	}
	if !strings.Contains(result.Content, "Diff too large to render") {
		t.Errorf("Content = %q, want the over-budget sentence", result.Content)
	}
	if strings.Contains(result.Content, "\n") {
		t.Errorf("the degraded result rendered diff lines, want the diffstat sentence alone:\n%s", result.Content)
	}
	want := domain.DiffStat{Added: lines - 2*shared, Removed: lines - 2*shared}
	if stat, ok := result.Summary.(domain.DiffStat); !ok || stat != want {
		t.Errorf("Summary = %#v, want %+v", result.Summary, want)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > allocCeling {
		t.Errorf("allocated %d bytes, want at most %d — the LCS table was built", allocated, allocCeling)
	}
}

// TestOverBudgetDiff_Counts pins the degraded stat's arithmetic on inputs small enough to read,
// including the case the clamp exists for: a pure insertion, where the shared head and the shared
// tail overlap and a line would otherwise be counted twice.
func TestOverBudgetDiff_Counts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		old, new string
		want     domain.DiffStat
	}{
		{"middle line replaced", "a\nX\nb", "a\nY\nb", domain.DiffStat{Added: 1, Removed: 1}},
		{"pure insertion overlaps head and tail", "a\nb", "a\nx\nb", domain.DiffStat{Added: 1}},
		{"pure deletion overlaps head and tail", "a\nx\nb", "a\nb", domain.DiffStat{Removed: 1}},
		{"shared head only", "a\nb\nc", "a\nX\nY", domain.DiffStat{Added: 2, Removed: 2}},
		{"shared tail only", "b\nc\na", "X\nY\na", domain.DiffStat{Added: 2, Removed: 2}},
		{"nothing shared", "a", "b", domain.DiffStat{Added: 1, Removed: 1}},
		{"trailing newline counts its empty line", "a\n", "b\n", domain.DiffStat{Added: 1, Removed: 1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, stat := overBudgetDiff(tc.old, tc.new, lineCount(tc.old), lineCount(tc.new))

			if stat != tc.want {
				t.Errorf("stat = %+v, want %+v", stat, tc.want)
			}
		})
	}
}

// TestViewDiff_RefusesEscapingSymlink pins the STATIC half of the fence at view_diff's boundary:
// a workspace component that is a symlink pointing OUTSIDE the workspace is refused with the
// uniform ErrPathEscape message, and nothing from outside reaches the diff.
//
// This one is a boundary pin, not new behaviour — the former resolveInRoot pre-pass refused this
// case too. What the change actually gained is pinned by the racing twin below, which the old
// resolveInRoot + os.ReadFile pair followed.
func TestViewDiff_RefusesEscapingSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_rsa"), []byte(outsideMarker), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "ssh")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	result, err := NewViewDiff(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "ssh/id_rsa", "newContent": ""}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want true: the diff read a symlink out of the workspace (content: %q)", result.Content)
	}
	if !strings.Contains(result.Content, ErrPathEscape.Error()) {
		t.Errorf("content %q does not carry the ErrPathEscape message %q", result.Content, ErrPathEscape.Error())
	}
	if strings.Contains(result.Content, outsideMarker) {
		t.Errorf("content leaked the file outside the workspace: %q", result.Content)
	}
}

// TestViewDiff_RefusesComponentSwappedMidRead is the behaviour view_diff gained by reading through
// the fence rather than around it: a workspace component swapped to an outside-pointing symlink
// while the call is in flight no longer redirects the read. The old resolveInRoot + os.ReadFile
// pair validated the path and then re-walked it, so a swap landing in that window was followed and
// the outside file came back as the diff's removed lines. This test fails against the pre-change
// code (the shared harness the read tools use, with view_diff's own empty-newContent call shape).
func TestViewDiff_RefusesComponentSwappedMidRead(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	escapes := escapesUnderComponentSwap(t, NewViewDiff(root), root, 2000)

	if escapes != 0 {
		t.Errorf("%d of 2000 diffs returned the file outside the workspace, want 0", escapes)
	}
}

func TestViewDiff_ToolErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tool := NewViewDiff(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContain string
	}{
		{"missing path", map[string]any{"newContent": "x"}, "path is required"},
		{"file not found", map[string]any{"path": "nope.txt", "newContent": "x"}, "file not found"},
		{"path escape", map[string]any{"path": "../escape.txt", "newContent": "x"}, "outside the workspace"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(), callWith(t, "c1", tc.args))
			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("IsError = false, want true (content: %q)", result.Content)
			}
			if !strings.Contains(result.Content, tc.wantContain) {
				t.Errorf("content %q does not contain %q", result.Content, tc.wantContain)
			}
		})
	}
}

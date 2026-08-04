package recall

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// workspaceA and workspaceB are two abstract absolute workspace paths. Nothing on disk needs
// to exist at them: the store only digests and records the string.
const (
	workspaceA = "/home/dev/project-a"
	workspaceB = "/home/dev/project-b"
)

// appendAll appends every text in order, failing the test on the first error.
func appendAll(t *testing.T, s *Store, workspace string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if err := s.Append(workspace, text); err != nil {
			t.Fatalf("Append(%q, %q): %v", workspace, text, err)
		}
	}
}

// loadOK loads a workspace's entries, failing the test on error.
func loadOK(t *testing.T, s *Store, workspace string) []string {
	t.Helper()
	got, err := s.Load(workspace)
	if err != nil {
		t.Fatalf("Load(%q): %v", workspace, err)
	}
	return got
}

// wantEntries asserts the loaded entries equal want, in order.
func wantEntries(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("loaded %d entries, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// seedFile writes count records for ws straight into its file, bypassing Append. Compaction
// tests need a file already at the threshold without paying for thousands of appends.
func seedFile(t *testing.T, s *Store, ws string, count int) {
	t.Helper()
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	var buf strings.Builder
	for i := range count {
		line, err := json.Marshal(record{
			Workspace: ws,
			At:        time.Now().UTC().Format(time.RFC3339),
			Text:      fmt.Sprintf("seed %d", i),
		})
		if err != nil {
			t.Fatalf("marshal seed record: %v", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(s.path(ws), []byte(buf.String()), filePerm); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
}

// countLines reports the number of newline-terminated lines in a file.
func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if len(data) == 0 {
		return 0
	}
	return len(strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"))
}

func TestAppendLoadRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("preserves order", func(t *testing.T) {
		t.Parallel()
		s := New(t.TempDir())
		appendAll(t, s, workspaceA, "first", "second", "third")
		wantEntries(t, loadOK(t, s, workspaceA), []string{"first", "second", "third"})
	})

	t.Run("preserves multi-line text on one disk line", func(t *testing.T) {
		t.Parallel()
		s := New(t.TempDir())
		multi := "write a test\n\nthen run make check"
		appendAll(t, s, workspaceA, multi, "after")
		wantEntries(t, loadOK(t, s, workspaceA), []string{multi, "after"})
		if lines := countLines(t, s.path(workspaceA)); lines != 2 {
			t.Errorf("file has %d lines, want 2 — a multi-line prompt must stay one record", lines)
		}
	})

	t.Run("a trailing separator names the same workspace", func(t *testing.T) {
		t.Parallel()
		s := New(t.TempDir())
		appendAll(t, s, workspaceA+"/", "slashed")
		wantEntries(t, loadOK(t, s, workspaceA), []string{"slashed"})
	})
}

func TestAppendDedupsOnlyConsecutiveEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		send []string
		want []string
	}{
		{name: "adjacent duplicates collapse", send: []string{"a", "a"}, want: []string{"a"}},
		{name: "a repeat run collapses to one", send: []string{"a", "a", "a"}, want: []string{"a"}},
		{name: "a separated repeat survives", send: []string{"a", "b", "a"}, want: []string{"a", "b", "a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := New(t.TempDir())
			appendAll(t, s, workspaceA, tc.send...)
			wantEntries(t, loadOK(t, s, workspaceA), tc.want)
		})
	}
}

func TestAppendCompactsPastLimit(t *testing.T) {
	t.Parallel()

	t.Run("compacts to the newest entries once past the threshold", func(t *testing.T) {
		t.Parallel()
		s := New(t.TempDir())
		seedFile(t, s, workspaceA, compactAt)
		appendAll(t, s, workspaceA, "newest")

		path := s.path(workspaceA)
		if lines := countLines(t, path); lines != maxEntries {
			t.Fatalf("file has %d lines after compaction, want %d", lines, maxEntries)
		}
		got := loadOK(t, s, workspaceA)
		if len(got) != maxEntries {
			t.Fatalf("loaded %d entries, want %d", len(got), maxEntries)
		}
		if got[len(got)-1] != "newest" {
			t.Errorf("newest entry = %q, want %q", got[len(got)-1], "newest")
		}
		// The kept window is the newest maxEntries of (compactAt seeds + the appended one),
		// so the oldest survivor is seed number compactAt-maxEntries+1.
		wantOldest := fmt.Sprintf("seed %d", compactAt-maxEntries+1)
		if got[0] != wantOldest {
			t.Errorf("oldest surviving entry = %q, want %q", got[0], wantOldest)
		}
	})

	t.Run("leaves a file at the threshold alone", func(t *testing.T) {
		t.Parallel()
		s := New(t.TempDir())
		seedFile(t, s, workspaceA, compactAt-1)
		appendAll(t, s, workspaceA, "at the line")

		if lines := countLines(t, s.path(workspaceA)); lines != compactAt {
			t.Errorf("file has %d lines, want %d — compaction must not fire at the threshold", lines, compactAt)
		}
		if got := loadOK(t, s, workspaceA); len(got) != maxEntries {
			t.Errorf("Load returned %d entries, want the newest %d", len(got), maxEntries)
		}
	})
}

func TestLoadFiltersForeignWorkspaceRecords(t *testing.T) {
	t.Parallel()

	s := New(t.TempDir())
	appendAll(t, s, workspaceA, "mine")

	// A digest collision would land another workspace's records in this file; they must not
	// surface as this workspace's recall.
	stranger, err := encodeRecord(record{Workspace: "/elsewhere", At: "2026-08-04T00:00:00Z", Text: "theirs"})
	if err != nil {
		t.Fatalf("encodeRecord: %v", err)
	}
	if err := appendLine(s.path(workspaceA), stranger); err != nil {
		t.Fatalf("appendLine: %v", err)
	}

	wantEntries(t, loadOK(t, s, workspaceA), []string{"mine"})

	// Dedup answers for this workspace's view too: the stranger's line must not hide behind
	// the newest entry of the workspace being appended to.
	appendAll(t, s, workspaceA, "mine")
	wantEntries(t, loadOK(t, s, workspaceA), []string{"mine"})
}

func TestLoadSkipsMalformedLines(t *testing.T) {
	t.Parallel()

	s := New(t.TempDir())
	appendAll(t, s, workspaceA, "before")
	path := s.path(workspaceA)
	if err := appendLine(path, []byte("{not json\n\n")); err != nil {
		t.Fatalf("appendLine: %v", err)
	}
	appendAll(t, s, workspaceA, "after")

	wantEntries(t, loadOK(t, s, workspaceA), []string{"before", "after"})
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	t.Parallel()

	s := New(filepath.Join(t.TempDir(), "never-created"))
	got, err := s.Load(workspaceA)
	if err != nil {
		t.Fatalf("Load on a missing file: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("loaded %q, want no entries", got)
	}
	if _, err := os.Stat(s.dir); !os.IsNotExist(err) {
		t.Errorf("Load created the store directory; it must stay lazy (stat err = %v)", err)
	}
}

func TestTwoWorkspacesTwoFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := New(dir)
	appendAll(t, s, workspaceA, "a-one", "a-two")
	appendAll(t, s, workspaceB, "b-one")

	wantEntries(t, loadOK(t, s, workspaceA), []string{"a-one", "a-two"})
	wantEntries(t, loadOK(t, s, workspaceB), []string{"b-one"})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("store dir holds %d files, want one per workspace", len(entries))
	}
	if s.path(workspaceA) == s.path(workspaceB) {
		t.Error("both workspaces resolved to the same file")
	}
}

func TestAppendKeepsRecallPrivate(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are not meaningful on windows")
	}
	s := New(filepath.Join(t.TempDir(), "prompts"))
	appendAll(t, s, workspaceA, "private")

	dirInfo, err := os.Stat(s.dir)
	if err != nil {
		t.Fatalf("stat store dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != dirPerm {
		t.Errorf("store dir mode = %o, want %o", got, dirPerm)
	}
	fileInfo, err := os.Stat(s.path(workspaceA))
	if err != nil {
		t.Fatalf("stat recall file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != filePerm {
		t.Errorf("recall file mode = %o, want %o", got, filePerm)
	}
}

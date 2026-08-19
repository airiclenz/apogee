package tools

// The write funnel's undo capture (ADR 0051). Every content-writing verb reaches the
// filesystem through safeWriteFile, so these tests ask that ONE seam the three questions the
// journal's usefulness rests on: does each verb leave exactly one record for the path it
// touched, does that record hold the bytes that were there before (a revert puts them back),
// and does a write that never landed leave the journal untouched.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/undo"
)

// runJournalledWrite executes one write tool under a context carrying journal, failing the
// test unless the call succeeded — a refused write journals nothing by design, so a case that
// meant to record something must not silently pass on a tool error.
func runJournalledWrite(t *testing.T, journal *undo.Journal, tool domain.Tool, args map[string]any) {
	t.Helper()

	result, err := tool.Execute(undo.WithJournal(context.Background(), journal), callWith(t, "c1", args))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
}

// readOrAbsent returns the file's content, or ok=false when it does not exist.
func readOrAbsent(t *testing.T, path string) (string, bool) {
	t.Helper()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data), true
}

// TestWriteFunnelJournalsEveryContentVerb walks the four tools that write content through
// safeWriteFile. Each records ONE change for its path; the previewed action being restore or
// delete (never skip) is the post-hash agreeing with what is on disk; and the revert putting
// the file back exactly as it was is the pre-image being the real one.
func TestWriteFunnelJournalsEveryContentVerb(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		file       string
		before     string // empty means the file does not exist before the write
		exists     bool
		newTool    func(root string) domain.Tool
		args       func(file string) map[string]any
		wantAction undo.Action
		wantAfter  string
	}{
		{
			name:    "write_file creating a file",
			file:    "made.txt",
			newTool: func(root string) domain.Tool { return NewWriteFile(root) },
			args: func(file string) map[string]any {
				return map[string]any{"path": file, "content": "fresh"}
			},
			wantAction: undo.ActionDelete,
			wantAfter:  "fresh",
		},
		{
			name:    "write_file overwriting a file",
			file:    "kept.txt",
			before:  "original",
			exists:  true,
			newTool: func(root string) domain.Tool { return NewWriteFile(root) },
			args: func(file string) map[string]any {
				return map[string]any{"path": file, "content": "replaced"}
			},
			wantAction: undo.ActionRestore,
			wantAfter:  "replaced",
		},
		{
			name:    "edit_existing_file",
			file:    "edited.txt",
			before:  "original",
			exists:  true,
			newTool: func(root string) domain.Tool { return NewEditExistingFile(root) },
			args: func(file string) map[string]any {
				return map[string]any{"path": file, "content": "edited"}
			},
			wantAction: undo.ActionRestore,
			wantAfter:  "edited",
		},
		{
			name:    "single_find_and_replace",
			file:    "single.txt",
			before:  "keep OLD keep",
			exists:  true,
			newTool: func(root string) domain.Tool { return NewSingleFindReplace(root) },
			args: func(file string) map[string]any {
				return map[string]any{"path": file, "oldText": "OLD", "newText": "NEW"}
			},
			wantAction: undo.ActionRestore,
			wantAfter:  "keep NEW keep",
		},
		{
			name:    "multi_find_and_replace",
			file:    "multi.txt",
			before:  "A and B",
			exists:  true,
			newTool: func(root string) domain.Tool { return NewMultiFindReplace(root) },
			args: func(file string) map[string]any {
				return map[string]any{"path": file, "replacements": []any{
					map[string]any{"oldText": "A", "newText": "X"},
					map[string]any{"oldText": "B", "newText": "Y"},
				}}
			},
			wantAction: undo.ActionRestore,
			wantAfter:  "X and Y",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := tempRoot(t)
			path := filepath.Join(root, tc.file)
			if tc.exists {
				writeFixtureFile(t, path, tc.before)
			}

			journal := undo.New()
			runJournalledWrite(t, journal, tc.newTool(root), tc.args(tc.file))

			if got, _ := readOrAbsent(t, path); got != tc.wantAfter {
				t.Fatalf("file after the write = %q, want %q", got, tc.wantAfter)
			}

			step, ok := journal.Preview()
			if !ok {
				t.Fatal("the write left no undo step")
			}
			if len(step.Changes) != 1 {
				t.Fatalf("journal holds %d changes, want exactly 1: %+v", len(step.Changes), step.Changes)
			}
			change := step.Changes[0]
			if change.Path != path {
				t.Errorf("recorded path = %q, want %q", change.Path, path)
			}
			if change.Action != tc.wantAction {
				t.Fatalf("previewed action = %v (%s), want %v", change.Action, change.Reason, tc.wantAction)
			}

			report, err := journal.Revert()
			if err != nil {
				t.Fatalf("Revert: %v", err)
			}
			if len(report.Skipped) != 0 {
				t.Fatalf("revert skipped %+v, want nothing skipped", report.Skipped)
			}

			got, exists := readOrAbsent(t, path)
			if exists != tc.exists {
				t.Fatalf("file exists after undo = %v, want %v", exists, tc.exists)
			}
			if got != tc.before {
				t.Errorf("file after undo = %q, want the pre-image %q", got, tc.before)
			}
		})
	}
}

// TestWriteFunnelKeepsOneRecordPerPath pins the merge the journal performs on the funnel's
// behalf: two writes to the same file inside one group are one undo step back to the state
// before the FIRST of them, not two steps that peel off one write each.
func TestWriteFunnelKeepsOneRecordPerPath(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	path := filepath.Join(root, "twice.txt")
	writeFixtureFile(t, path, "original")

	journal := undo.New()
	tool := NewWriteFile(root)
	runJournalledWrite(t, journal, tool, map[string]any{"path": "twice.txt", "content": "first"})
	runJournalledWrite(t, journal, tool, map[string]any{"path": "twice.txt", "content": "second"})

	step, ok := journal.Preview()
	if !ok {
		t.Fatal("the writes left no undo step")
	}
	if len(step.Changes) != 1 {
		t.Fatalf("journal holds %d changes, want exactly 1: %+v", len(step.Changes), step.Changes)
	}
	if step.Changes[0].Action != undo.ActionRestore {
		t.Fatalf("previewed action = %v (%s), want restore", step.Changes[0].Action, step.Changes[0].Reason)
	}

	if _, err := journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if got, _ := readOrAbsent(t, path); got != "original" {
		t.Errorf("file after undo = %q, want the pre-image of the FIRST write %q", got, "original")
	}
}

// TestWriteFunnelJournalsNothingWhenTheWriteFails is the other half of the ordering contract:
// the record is committed only after the mutation succeeded, so a refused write leaves the
// journal exactly as empty as it found it — generation included, since a stale generation
// would invalidate a preview no write had actually invalidated.
func TestWriteFunnelJournalsNothingWhenTheWriteFails(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	journal := undo.New()

	result, err := NewWriteFile(root).Execute(undo.WithJournal(context.Background(), journal),
		callWith(t, "c1", map[string]any{"path": "../escaped.txt", "content": "nope"}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("the escaping write was allowed: %q", result.Content)
	}

	if step, ok := journal.Preview(); ok {
		t.Fatalf("a refused write left an undo step: %+v", step.Changes)
	}
	if got := journal.Generation(); got != 0 {
		t.Errorf("generation = %d after a refused write, want 0", got)
	}
}

// TestWriteFunnelWritesWithoutAJournal is the floor: with nothing recording, the funnel
// behaves exactly as it did before the journal existed.
func TestWriteFunnelWritesWithoutAJournal(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	if undo.FromContext(context.Background()) != nil {
		t.Fatal("a bare context reports a journal")
	}

	result, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "plain.txt", "content": "written"}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if got, _ := readOrAbsent(t, filepath.Join(root, "plain.txt")); got != "written" {
		t.Errorf("file content = %q, want %q", got, "written")
	}
}

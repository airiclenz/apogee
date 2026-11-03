package gitstatus

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// porcelainZ joins records the way `git status -z` emits them: each one NUL-terminated.
func porcelainZ(records ...string) string {
	return strings.Join(records, "\x00") + "\x00"
}

// countsOnly is s with the entry lists dropped — the part of a Snapshot Counter fills, so a
// Parse result and a Counter result compare field for field.
func countsOnly(s Snapshot) Snapshot {
	s.StagedEntries, s.UnstagedEntries, s.UntrackedEntries = nil, nil, nil
	return s
}

// count streams out through a Counter chunk bytes at a time and returns what it folded.
func count(t *testing.T, out string, chunk int) Snapshot {
	t.Helper()
	var c Counter
	for start := 0; start < len(out); start += chunk {
		end := min(start+chunk, len(out))
		n, err := c.Write([]byte(out[start:end]))
		if err != nil || n != end-start {
			t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, end-start)
		}
	}
	return c.Snapshot()
}

func TestParse_ReadsBranchAndCounts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		out  string
		want Snapshot
	}{
		{
			name: "clean tree with an upstream in step",
			out: porcelainZ("# branch.oid abc123", "# branch.head main",
				"# branch.upstream origin/main", "# branch.ab +0 -0"),
			want: Snapshot{Branch: "main", Upstream: "origin/main", HasAheadBehind: true},
		},
		{
			name: "a file staged and edited again is one dirty path",
			out: porcelainZ("# branch.head main",
				"1 MM N... 100644 100644 100644 aaaa bbbb pkg/my file.go"),
			want: Snapshot{
				Branch: "main", Dirty: 1, Staged: 1, Unstaged: 1,
				StagedEntries:   []string{"M  pkg/my file.go"},
				UnstagedEntries: []string{"M  pkg/my file.go"},
			},
		},
		{
			name: "a rename counts once and carries its origin",
			out: porcelainZ("# branch.head main",
				"2 R. N... 100644 100644 100644 aaaa bbbb R100 new.go", "old.go"),
			want: Snapshot{
				Branch: "main", Dirty: 1, Staged: 1,
				StagedEntries: []string{"R  new.go (from old.go)"},
			},
		},
		{
			name: "an unmerged path counts once, as unstaged",
			out: porcelainZ("# branch.head main",
				"u UU N... 100644 100644 100644 100644 aaaa bbbb cccc conflict.go"),
			want: Snapshot{
				Branch: "main", Dirty: 1, Unstaged: 1,
				UnstagedEntries: []string{"U  conflict.go (unmerged)"},
			},
		},
		{
			name: "each untracked file in a new directory counts",
			out:  porcelainZ("# branch.head main", "? new/a.txt", "? new/b.txt", "? new/c d.txt"),
			want: Snapshot{
				Branch: "main", Dirty: 3, Untracked: 3,
				UntrackedEntries: []string{"new/a.txt", "new/b.txt", "new/c d.txt"},
			},
		},
		{
			name: "a detached head names no branch",
			out:  porcelainZ("# branch.oid abc123", "# branch.head (detached)"),
			want: Snapshot{Detached: true},
		},
		{
			name: "no upstream states no divergence",
			out:  porcelainZ("# branch.head feature", "? x"),
			want: Snapshot{Branch: "feature", Dirty: 1, Untracked: 1, UntrackedEntries: []string{"x"}},
		},
		{
			name: "ahead only",
			out:  porcelainZ("# branch.head main", "# branch.upstream origin/main", "# branch.ab +4 -0"),
			want: Snapshot{Branch: "main", Upstream: "origin/main", HasAheadBehind: true, Ahead: 4},
		},
		{
			name: "a malformed ab header leaves the divergence unstated",
			out:  porcelainZ("# branch.head main", "# branch.upstream origin/main", "# branch.ab nonsense"),
			want: Snapshot{Branch: "main", Upstream: "origin/main"},
		},
		{
			name: "an unrecognised or truncated record is skipped",
			out:  porcelainZ("warning: something git said", "# branch.head main", "1 M. N...", "? kept.txt"),
			want: Snapshot{Branch: "main", Dirty: 1, Untracked: 1, UntrackedEntries: []string{"kept.txt"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Parse(tc.out)

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Parse = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestCounter_MatchesParse proves the two forms share one fold: on every input, split at every
// chunk size from one byte up, the Counter's counts and branch fields equal Parse's, and it
// keeps no entry lists.
func TestCounter_MatchesParse(t *testing.T) {
	t.Parallel()

	out := porcelainZ("# branch.oid abc123", "# branch.head main",
		"# branch.upstream origin/main", "# branch.ab +2 -3",
		"1 MM N... 100644 100644 100644 aaaa bbbb both.go",
		"1 .M N... 100644 100644 100644 aaaa bbbb edited.go",
		"2 R. N... 100644 100644 100644 aaaa bbbb R100 new.go", "? looks-untracked-but-is-an-origin",
		"u UU N... 100644 100644 100644 100644 aaaa bbbb cccc conflict.go",
		"? a.txt", "? b.txt", "noise")
	want := countsOnly(Parse(out))
	if want.Dirty != 6 {
		t.Fatalf("Parse Dirty = %d, want 6 — the fixture has drifted", want.Dirty)
	}

	for chunk := 1; chunk <= len(out); chunk++ {
		got := count(t, out, chunk)

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("chunk %d: Counter = %+v, want Parse's %+v", chunk, got, want)
		}
	}
}

// TestCounter_UnterminatedTailIsReadNotConsumed proves Snapshot reads a tail with no closing
// NUL as Parse would, and that a Write completing that record later folds it exactly once.
func TestCounter_UnterminatedTailIsReadNotConsumed(t *testing.T) {
	t.Parallel()

	var c Counter
	if _, err := c.Write([]byte("# branch.head main\x00? half")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	early := c.Snapshot()
	if _, err := c.Write([]byte("way.txt\x00")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	late := c.Snapshot()

	if want := countsOnly(Parse("# branch.head main\x00? half")); !reflect.DeepEqual(early, want) {
		t.Errorf("early Snapshot = %+v, want Parse's %+v", early, want)
	}
	if late.Untracked != 1 || late.Dirty != 1 {
		t.Errorf("late Snapshot = %+v, want the completed record counted once", late)
	}
}

// TestCounter_CountsALargeStreamExactly feeds more than 256 KiB of status in small writes that
// split records across their boundaries, and checks every count is exact — no cap, no loss.
func TestCounter_CountsALargeStreamExactly(t *testing.T) {
	t.Parallel()

	const (
		modified  = 2000
		untracked = 2000
		renamed   = 500
		chunk     = 37 // prime, so record and write boundaries keep drifting apart
		minStream = 256 << 10
	)
	records := []string{"# branch.head main", "# branch.upstream origin/main", "# branch.ab +7 -1"}
	for i := range modified {
		records = append(records, fmt.Sprintf("1 .M N... 100644 100644 100644 aaaa bbbb dir/modified-file-%05d.go", i))
	}
	for i := range renamed {
		records = append(records,
			fmt.Sprintf("2 R. N... 100644 100644 100644 aaaa bbbb R100 dir/renamed-to-%05d.go", i),
			fmt.Sprintf("dir/renamed-from-%05d.go", i))
	}
	for i := range untracked {
		records = append(records, fmt.Sprintf("? new/untracked-file-with-a-long-name-%05d.txt", i))
	}
	out := porcelainZ(records...)
	if len(out) <= minStream {
		t.Fatalf("stream is %d bytes, want more than %d", len(out), minStream)
	}

	got := count(t, out, chunk)

	want := Snapshot{
		Branch: "main", Upstream: "origin/main", HasAheadBehind: true, Ahead: 7, Behind: 1,
		Dirty: modified + renamed + untracked, Staged: renamed, Unstaged: modified, Untracked: untracked,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Counter = %+v, want %+v", got, want)
	}
	if parsed := countsOnly(Parse(out)); !reflect.DeepEqual(parsed, want) {
		t.Errorf("Parse = %+v, want %+v", parsed, want)
	}
}

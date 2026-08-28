package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func TestGrep_Execute_FindsMatchesWithLocation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	result, err := NewGrep(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "^package "}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "src/a.go:1:package a") {
		t.Errorf("missing expected match for src/a.go: %q", result.Content)
	}
	if !strings.Contains(result.Content, "b.go:1:package b") {
		t.Errorf("missing expected match for nested b.go: %q", result.Content)
	}
}

func TestGrep_Execute_ExcludesNoiseDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	result, err := NewGrep(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "noise"}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result.Content, "No matches found") {
		t.Errorf("node_modules match leaked through exclusion: %q", result.Content)
	}
}

func TestGrep_Execute_IncludeGlobNarrows(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	result, err := NewGrep(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "func", "include": "*.go"}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result.Content, "a.go") {
		t.Errorf("include glob excluded a matching .go file: %q", result.Content)
	}
}

func TestGrep_Execute_InvalidRegexFallsBackToLiteral(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	// "Alpha(" is not a valid regex (unclosed group); it must be matched literally
	// against "func Alpha() {}".
	result, err := NewGrep(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "Alpha("}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result.Content, "a.go") {
		t.Errorf("literal fallback failed to match: %q", result.Content)
	}
}

func TestGrep_Execute_SearchesSingleFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	result, err := NewGrep(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "Beta", "path": "src/inner/b.go"}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result.Content, "b.go:2:") || !strings.Contains(result.Content, "Beta") {
		t.Errorf("single-file search missing match: %q", result.Content)
	}
}

func TestGrep_Execute_ReportsMatchCount(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)
	tool := NewGrep(root, nil)

	cases := []struct {
		name        string
		args        map[string]any
		wantContent string
		wantSummary domain.MatchedLines
	}{
		{
			name:        "matches",
			args:        map[string]any{"pattern": "^package "},
			wantContent: "[2 total matches, showing 1-2]\nsrc/a.go:1:package a\nsrc/inner/b.go:1:package b",
			wantSummary: domain.MatchedLines{Total: 2},
		},
		{
			// The sentinel path carries a summary too, so a host reads a zero rather
			// than testing this sentence for a "No matches" prefix.
			name:        "no matches",
			args:        map[string]any{"pattern": "zzz-nothing-matches-this"},
			wantContent: "No matches found",
			wantSummary: domain.MatchedLines{Total: 0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(), callWith(t, "c1", tc.args))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.Content != tc.wantContent {
				t.Errorf("Content = %q, want %q", result.Content, tc.wantContent)
			}
			matched, ok := result.Summary.(domain.MatchedLines)
			if !ok {
				t.Fatalf("Summary = %#v, want a domain.MatchedLines", result.Summary)
			}
			if matched != tc.wantSummary {
				t.Errorf("Summary = %+v, want %+v", matched, tc.wantSummary)
			}
		})
	}
}

// TestGrep_Execute_SearchesSubdirectory covers the walk that does NOT start at the
// workspace root: the fence is anchored at the workspace (so a link out of the SUBTREE but
// still inside the workspace keeps matching), while the reported location stays relative to
// the searched directory, as before. It is the deterministic cover for the walk-relative →
// workspace-relative lift the fenced open needs.
func TestGrep_Execute_SearchesSubdirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_rsa"), []byte(grepOutsideMarker), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "id_rsa"), filepath.Join(root, "src", "notes.go")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	// ../top.txt leaves the searched subtree but stays inside the workspace: still searched.
	if err := os.Symlink(filepath.Join("..", "top.txt"), filepath.Join(root, "src", "up.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	result, err := NewGrep(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "a|TOKEN", "path": "src"}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "a.go:1:package a") {
		t.Errorf("location is not relative to the searched directory: %q", result.Content)
	}
	if !strings.Contains(result.Content, "inner/b.go:2:func Beta() {}") {
		t.Errorf("nested location is not relative to the searched directory: %q", result.Content)
	}
	if !strings.Contains(result.Content, "up.txt:1:alpha") {
		t.Errorf("a symlink out of the subtree but inside the workspace stopped matching: %q", result.Content)
	}
	if strings.Contains(result.Content, grepOutsideMarker) {
		t.Errorf("the subdirectory walk read a file outside the workspace: %q", result.Content)
	}
}

// grepOutsideMarker is the body of the file OUTSIDE the workspace — the audit probe's own
// payload. A grep result carrying it read through the fence.
const grepOutsideMarker = "SUPERSECRET_TOKEN=abc123"

// TestGrep_RefusesEscapingSymlink pins the workspace fence on the file grep actually READS,
// not just on the path argument it was given. git stores symlinks verbatim, so a clone can
// plant `notes.txt -> ~/.ssh/id_rsa`; the walk yields it as an ordinary non-directory entry,
// and grep is ReadOnly() — it runs unapproved in every mode, Plan included. Before the fence
// reached the walk, an ordinary whole-workspace grep returned the outside file's matching
// lines as tool content (the audit's probe read SUPERSECRET_TOKEN through exactly this
// shape), so the "directory walk" case fails against the pre-change code.
//
// The "single file" case is a boundary pin rather than new behaviour: the top-level path
// argument was already fenced by resolveInRoot, and it now ALSO opens through the pinned
// root — this fails if either half is ever dropped.
func TestGrep_RefusesEscapingSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_rsa"), []byte(grepOutsideMarker), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "inside.txt"), []byte("WORKSPACE_TOKEN=ok"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "id_rsa"), filepath.Join(root, "notes.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	// A RELATIVE in-workspace symlink must still be searched: the fence narrows what leaves
	// the workspace, never what stays inside it.
	if err := os.Symlink("inside.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	tool := NewGrep(root, nil)

	t.Run("directory walk", func(t *testing.T) {
		t.Parallel()

		result, err := tool.Execute(context.Background(),
			callWith(t, "c1", map[string]any{"pattern": "TOKEN="}))

		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		if result.IsError {
			t.Fatalf("unexpected tool error: %q", result.Content)
		}
		if strings.Contains(result.Content, grepOutsideMarker) {
			t.Errorf("the walk read a file outside the workspace: %q", result.Content)
		}
		if strings.Contains(result.Content, "notes.txt") {
			t.Errorf("the escaping symlink was reported as a match source: %q", result.Content)
		}
		if !strings.Contains(result.Content, "inside.txt:1:WORKSPACE_TOKEN=ok") {
			t.Errorf("the ordinary in-workspace file stopped matching: %q", result.Content)
		}
		if !strings.Contains(result.Content, "link.txt:1:WORKSPACE_TOKEN=ok") {
			t.Errorf("a relative in-workspace symlink stopped matching: %q", result.Content)
		}
		if matched, ok := result.Summary.(domain.MatchedLines); !ok || matched.Total != 2 {
			t.Errorf("Summary = %#v, want domain.MatchedLines{Total: 2}", result.Summary)
		}
	})

	t.Run("single file", func(t *testing.T) {
		t.Parallel()

		result, err := tool.Execute(context.Background(),
			callWith(t, "c1", map[string]any{"pattern": "TOKEN=", "path": "notes.txt"}))

		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		if !result.IsError {
			t.Fatalf("IsError = false, want true: the search followed a symlink out of the workspace (content: %q)", result.Content)
		}
		if !strings.Contains(result.Content, "outside the workspace") {
			t.Errorf("content %q does not carry the path-escape message", result.Content)
		}
		if strings.Contains(result.Content, grepOutsideMarker) {
			t.Errorf("content leaked the file outside the workspace: %q", result.Content)
		}
	})
}

// TestGrep_Execute_SearchesUnderAnExtraReadRoot pins the mount half of the read-only roots
// seam for grep: an ABSOLUTE search path under a configured extra root is searched — the whole
// walk pinned to that root, so its subdirectories are reached and its matches are reported by
// names measured from it — while a workspace-relative search is untouched by the mount and a
// path under no root is still refused with the one uniform escape message.
func TestGrep_Execute_SearchesUnderAnExtraReadRoot(t *testing.T) {
	t.Parallel()

	root, extra, outside := t.TempDir(), t.TempDir(), t.TempDir()
	seedTree(t, root)
	seedTree(t, extra)
	if err := os.WriteFile(filepath.Join(outside, "id_rsa"), []byte(grepOutsideMarker), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tool := NewGrep(root, func() []string { return []string{extra} })

	cases := []struct {
		name    string
		path    string
		want    []string // substrings the result content must carry
		wantErr bool
	}{
		{"extra root itself", extra, []string{"src/a.go:1:package a", "src/inner/b.go:1:package b"}, false},
		{"subdir of the extra root", filepath.Join(extra, "src"), []string{"a.go:1:package a"}, false},
		{"one file under the extra root", filepath.Join(extra, "src", "a.go"), []string{":1:package a"}, false},
		{"workspace relative unchanged", "src", []string{"a.go:1:package a"}, false},
		{"under no root", outside, []string{"outside the workspace"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(),
				callWith(t, "c1", map[string]any{"pattern": "^package ", "path": tc.path}))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.IsError != tc.wantErr {
				t.Fatalf("IsError = %v, want %v (content: %q)", result.IsError, tc.wantErr, result.Content)
			}
			for _, want := range tc.want {
				if !strings.Contains(result.Content, want) {
					t.Errorf("content %q does not contain %q", result.Content, want)
				}
			}
		})
	}
}

// TestGrep_Execute_RefusesSymlinkEscapingAnExtraReadRoot pins that mounting a directory for
// reading mounts THAT directory and nothing it points at: a link inside the extra root aimed
// outside it is refused by the extra root's own fence, exactly as a link out of the workspace
// is refused by the workspace's. A read-only root is a root, not a doorway.
func TestGrep_Execute_RefusesSymlinkEscapingAnExtraReadRoot(t *testing.T) {
	t.Parallel()

	root, extra, outside := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_rsa"), []byte(grepOutsideMarker), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extra, "inside.txt"), []byte("SKILL_TOKEN=ok"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "id_rsa"), filepath.Join(extra, "notes.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	result, err := NewGrep(root, func() []string { return []string{extra} }).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "TOKEN=", "path": extra}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if strings.Contains(result.Content, grepOutsideMarker) {
		t.Errorf("the walk read a file outside the extra root: %q", result.Content)
	}
	if !strings.Contains(result.Content, "inside.txt:1:SKILL_TOKEN=ok") {
		t.Errorf("the ordinary file inside the extra root stopped matching: %q", result.Content)
	}
}

// TestGrep_SearchesAnExtraRootBySymlinkSpelling pins that a search whose path runs through a
// symlink to a mounted read-only root is served, and that its matches are named from the REAL
// root — the spelling a dotfiles-managed ~/.apogee/skills hands the model, which read_file
// refused while grep read it (audit 2026-08-28 F-13). The pin is here so a future change to
// readScope.resolve cannot regress grep the way readBounded regressed.
func TestGrep_SearchesAnExtraRootBySymlinkSpelling(t *testing.T) {
	t.Parallel()

	workspace, extra, link := symlinkedExtraReadRoot(t)
	tool := NewGrep(workspace, func() []string { return []string{extra} })

	content := spelledLikeReal(t, tool,
		map[string]any{"pattern": "^name: ", "path": link},
		map[string]any{"pattern": "^name: ", "path": extra})

	if !strings.Contains(content, "skill/SKILL.md:1:"+skillFixtureLine) {
		t.Errorf("matches %q do not name the hit at its real location skill/SKILL.md", content)
	}
}

// writeGrepLines writes name under root with one newline-terminated line per element.
func writeGrepLines(t *testing.T, root, name string, lines ...string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(root, name), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("setup write %s: %v", name, err)
	}
}

// TestGrep_Execute_ContextLines pins the whole rendered body, because context output is a
// FORMAT contract the model reads: match lines keep the "file:line:text" colon, context lines
// take the "file:line-text" dash, merged regions never repeat a line, and "--" appears only
// on a real gap.
func TestGrep_Execute_ContextLines(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		lines []string
		args  map[string]any
		want  string
	}{
		{
			name:  "context before and after a mid-file match",
			lines: []string{"L1", "L2", "L3", "NEEDLE a", "L5", "L6", "L7"},
			args:  map[string]any{"pattern": "NEEDLE", "context_lines": 2},
			want: "[1 total matches, showing 1-1]\n" +
				"f.txt:2-L2\nf.txt:3-L3\nf.txt:4:NEEDLE a\nf.txt:5-L5\nf.txt:6-L6",
		},
		{
			// Line 1 has nothing before it and line 3 is EOF: neither may invent a line.
			name:  "matches at line 1 and at EOF stay in range",
			lines: []string{"NEEDLE a", "L2", "NEEDLE b"},
			args:  map[string]any{"pattern": "NEEDLE", "context_lines": 2},
			want: "[2 total matches, showing 1-2]\n" +
				"f.txt:1:NEEDLE a\nf.txt:2-L2\nf.txt:3:NEEDLE b",
		},
		{
			name:  "overlapping context regions merge without duplicates",
			lines: []string{"L1", "NEEDLE a", "L3", "NEEDLE b", "L5", "L6"},
			args:  map[string]any{"pattern": "NEEDLE", "context_lines": 2},
			want: "[2 total matches, showing 1-2]\n" +
				"f.txt:1-L1\nf.txt:2:NEEDLE a\nf.txt:3-L3\nf.txt:4:NEEDLE b\nf.txt:5-L5\nf.txt:6-L6",
		},
		{
			// [1,3] and [4,6] do not overlap but they touch: a "--" there would claim a gap
			// that does not exist.
			name:  "touching context regions merge with no separator",
			lines: []string{"L1", "NEEDLE a", "L3", "L4", "NEEDLE b", "L6"},
			args:  map[string]any{"pattern": "NEEDLE", "context_lines": 1},
			want: "[2 total matches, showing 1-2]\n" +
				"f.txt:1-L1\nf.txt:2:NEEDLE a\nf.txt:3-L3\nf.txt:4-L4\nf.txt:5:NEEDLE b\nf.txt:6-L6",
		},
		{
			name: "distant groups are separated by --",
			lines: []string{"L1", "NEEDLE a", "L3", "L4", "L5", "L6",
				"L7", "L8", "L9", "L10", "NEEDLE b", "L12"},
			args: map[string]any{"pattern": "NEEDLE", "context_lines": 1},
			want: "[2 total matches, showing 1-2]\n" +
				"f.txt:1-L1\nf.txt:2:NEEDLE a\nf.txt:3-L3\n--\n" +
				"f.txt:10-L10\nf.txt:11:NEEDLE b\nf.txt:12-L12",
		},
		{
			name:  "zero context renders the historical bare form",
			lines: []string{"L1", "NEEDLE a", "L3"},
			args:  map[string]any{"pattern": "NEEDLE", "context_lines": 0},
			want:  "[1 total matches, showing 1-1]\nf.txt:2:NEEDLE a",
		},
		{
			name:  "negative context is clamped to none",
			lines: []string{"L1", "NEEDLE a", "L3"},
			args:  map[string]any{"pattern": "NEEDLE", "context_lines": -5},
			want:  "[1 total matches, showing 1-1]\nf.txt:2:NEEDLE a",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writeGrepLines(t, root, "f.txt", tc.lines...)

			result, err := NewGrep(root, nil).Execute(context.Background(), callWith(t, "c1", tc.args))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.IsError {
				t.Fatalf("unexpected tool error: %q", result.Content)
			}
			if result.Content != tc.want {
				t.Errorf("Content =\n%s\nwant\n%s", result.Content, tc.want)
			}
		})
	}
}

// TestGrep_Execute_ContextLinesPaginateByMatches pins the ride-along rule: max_results and
// offset select MATCHES, and the selected match's context is added on top rather than eating
// into the page.
func TestGrep_Execute_ContextLinesPaginateByMatches(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeGrepLines(t, root, "f.txt",
		"L1", "NEEDLE a", "L3", "L4", "NEEDLE b", "L6", "L7", "NEEDLE c", "L9")

	result, err := NewGrep(root, nil).Execute(context.Background(), callWith(t, "c1", map[string]any{
		"pattern": "NEEDLE", "context_lines": 1, "max_results": 1, "offset": 1,
	}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	want := "[3 total matches, showing 2-2]\nf.txt:4-L4\nf.txt:5:NEEDLE b\nf.txt:6-L6"
	if result.Content != want {
		t.Errorf("Content =\n%s\nwant\n%s", result.Content, want)
	}
	if matched, ok := result.Summary.(domain.MatchedLines); !ok || matched.Total != 3 {
		t.Errorf("Summary = %#v, want domain.MatchedLines{Total: 3}", result.Summary)
	}
}

// TestGrep_Execute_ContextLinesClampedToMaximum covers the silent narrowing: an over-wide
// request is trimmed to maxGrepContextLines instead of failing the search.
func TestGrep_Execute_ContextLinesClampedToMaximum(t *testing.T) {
	t.Parallel()

	lines := make([]string, 0, 30)
	for n := 1; n <= 30; n++ {
		if n == 15 {
			lines = append(lines, "NEEDLE")
			continue
		}
		lines = append(lines, fmt.Sprintf("L%d", n))
	}
	root := t.TempDir()
	writeGrepLines(t, root, "f.txt", lines...)

	result, err := NewGrep(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "NEEDLE", "context_lines": 99}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	body := strings.Split(result.Content, "\n")[1:]
	if len(body) != 2*maxGrepContextLines+1 {
		t.Fatalf("rendered %d body lines, want %d: %q", len(body), 2*maxGrepContextLines+1, result.Content)
	}
	if body[0] != "f.txt:5-L5" || body[len(body)-1] != "f.txt:25-L25" {
		t.Errorf("context window = %q..%q, want f.txt:5-L5..f.txt:25-L25", body[0], body[len(body)-1])
	}
}

// TestGrep_Execute_ContextLinesGroupPerFile covers the grouping the merge relies on: each
// file's context is gathered from that file, under the display name the search path implies.
func TestGrep_Execute_ContextLinesGroupPerFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	writeGrepLines(t, root, "src/a.txt", "a1", "NEEDLE a", "a3")
	writeGrepLines(t, root, "src/b.txt", "b1", "NEEDLE b", "b3")

	result, err := NewGrep(root, nil).Execute(context.Background(), callWith(t, "c1", map[string]any{
		"pattern": "NEEDLE", "path": "src", "context_lines": 1,
	}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	want := "[2 total matches, showing 1-2]\n" +
		"a.txt:1-a1\na.txt:2:NEEDLE a\na.txt:3-a3\n" +
		"b.txt:1-b1\nb.txt:2:NEEDLE b\nb.txt:3-b3"
	if result.Content != want {
		t.Errorf("Content =\n%s\nwant\n%s", result.Content, want)
	}
}

// TestGrep_Execute_ContextLinesAbsentMatchesDefault pins the byte-identical guarantee: an
// absent context_lines, an explicit 0, and a negative value all render today's output.
func TestGrep_Execute_ContextLinesAbsentMatchesDefault(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)
	tool := NewGrep(root, nil)

	baseline, err := tool.Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "^package "}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if baseline.Content != "[2 total matches, showing 1-2]\nsrc/a.go:1:package a\nsrc/inner/b.go:1:package b" {
		t.Fatalf("baseline output changed: %q", baseline.Content)
	}

	for _, value := range []int{0, -1} {
		result, err := tool.Execute(context.Background(),
			callWith(t, "c1", map[string]any{"pattern": "^package ", "context_lines": value}))
		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		if result.Content != baseline.Content {
			t.Errorf("context_lines %d changed the default output:\n%s\nwant\n%s", value, result.Content, baseline.Content)
		}
	}
}

func TestGrep_Execute_ToolErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)
	tool := NewGrep(root, nil)

	cases := []struct {
		name        string
		args        map[string]any
		wantContain string
	}{
		{"missing pattern", map[string]any{}, "pattern is required"},
		{"path escape", map[string]any{"pattern": "x", "path": "../"}, "outside the workspace"},
		{"missing path", map[string]any{"pattern": "x", "path": "absent"}, "path not found"},
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

// TestGrep_Execute_NewlineInAFilenameCannotForgeARow pins that a matched file whose NAME
// carries a line break stays ONE row in both render paths — the bare "path:line:text" form
// and the context form of renderFileGroup — with the break spelled out in the path. The
// context case seeds a line on either side of the match, so the "path:line-text" branch
// that prints surrounding lines is exercised too, not just the matched row.
func TestGrep_Execute_NewlineInAFilenameCannotForgeARow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		content  string
		args     map[string]any
		wantRows []string
	}{
		{
			name:     "plain rows",
			content:  "needle\n",
			args:     map[string]any{"pattern": "needle"},
			wantRows: []string{":1:needle"},
		},
		{
			name:     "context rows",
			content:  "before\nneedle\nafter\n",
			args:     map[string]any{"pattern": "needle", "context_lines": 1},
			wantRows: []string{":1-before", ":2:needle", ":3-after"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			seedForgingFile(t, root, tc.content)

			result, err := NewGrep(root, nil).Execute(context.Background(), callWith(t, "c1", tc.args))

			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			if result.IsError {
				t.Fatalf("unexpected tool error: %q", result.Content)
			}
			lines := strings.Split(result.Content, "\n")
			if len(lines) != len(tc.wantRows)+1 {
				t.Fatalf("got %d lines, want header + %d rows: %q", len(lines), len(tc.wantRows), result.Content)
			}
			for i, want := range tc.wantRows {
				row := lines[i+1]
				if !strings.Contains(row, forgingRowSpelling) {
					t.Errorf("row %q does not carry the escaped path spelling %q", row, forgingRowSpelling)
				}
				if !strings.HasSuffix(row, want) {
					t.Errorf("row %q does not end in %q", row, want)
				}
			}
		})
	}
}

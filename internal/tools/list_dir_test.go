package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// seedTree creates a small directory tree under root for the list_dir and grep tests.
func seedTree(t *testing.T, root string) {
	t.Helper()
	dirs := []string{"src", "src/inner", "node_modules", ".hidden"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("setup mkdir %s: %v", d, err)
		}
	}
	files := map[string]string{
		"top.txt":           "alpha",
		"src/a.go":          "package a\nfunc Alpha() {}",
		"src/inner/b.go":    "package b\nfunc Beta() {}",
		"node_modules/x.js": "noise",
		".hidden/secret":    "hidden",
		".dotfile":          "dot",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatalf("setup write %s: %v", name, err)
		}
	}
}

func TestListDir_Execute_ListsTopLevel(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	result, err := NewListDir(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "."}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "top.txt") || !strings.Contains(result.Content, "src/") {
		t.Errorf("listing missing expected entries: %q", result.Content)
	}
	if strings.Contains(result.Content, "node_modules") || strings.Contains(result.Content, ".dotfile") || strings.Contains(result.Content, ".hidden") {
		t.Errorf("listing leaked excluded/hidden entries: %q", result.Content)
	}
}

func TestListDir_Execute_RecursesWhenAsked(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	result, err := NewListDir(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "src", "recursive": true}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(result.Content, "a.go") || !strings.Contains(result.Content, "b.go") {
		t.Errorf("recursive listing missing nested entries: %q", result.Content)
	}
}

func TestListDir_Execute_NonRecursiveStopsAtTop(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)

	result, err := NewListDir(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "src"}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if strings.Contains(result.Content, "b.go") {
		t.Errorf("non-recursive listing leaked nested entry: %q", result.Content)
	}
}

func TestListDir_Execute_ReportsEntryCounts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedTree(t, root)
	tool := NewListDir(root, nil)

	cases := []struct {
		name        string
		args        map[string]any
		wantContent string
		wantSummary domain.ListedEntries
	}{
		{
			name:        "no pagination",
			args:        map[string]any{"path": "."},
			wantContent: "[2 entries total]\nsrc/\ntop.txt",
			wantSummary: domain.ListedEntries{Total: 2, Skipped: 0},
		},
		{
			name:        "offset skips entries",
			args:        map[string]any{"path": ".", "offset": 1},
			wantContent: "[2 entries total, skipped first 1]\ntop.txt",
			wantSummary: domain.ListedEntries{Total: 2, Skipped: 1},
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
			listed, ok := result.Summary.(domain.ListedEntries)
			if !ok {
				t.Fatalf("Summary = %#v, want a domain.ListedEntries", result.Summary)
			}
			if listed != tc.wantSummary {
				t.Errorf("Summary = %+v, want %+v", listed, tc.wantSummary)
			}
		})
	}
}

// TestListDir_RefusesEscapingSymlink pins the workspace fence on every directory the walk
// OPENS, not just on the path argument it was given: a symlinked directory pointing out of
// the workspace is never listed through, whether the model names it directly or the
// recursion reaches it. list_dir is ReadOnly(), so it runs unapproved in every mode, and an
// inventory of `~/.ssh` is reconnaissance even when no file is read.
//
// The named-directly case is a boundary pin (resolveInRoot already refuses it, and the walk
// now ALSO opens through the pinned root); the recursive case additionally pins that
// descent happens by workspace-relative NAME through the fence — a link is never a
// directory the walk descends into.
func TestListDir_RefusesEscapingSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "ssh"), 0o755); err != nil {
		t.Fatalf("setup outside dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "ssh", "id_rsa"), []byte("KEY"), 0o600); err != nil {
		t.Fatalf("setup outside file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "inner"), 0o755); err != nil {
		t.Fatalf("setup workspace dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "inner", "a.go"), []byte("package a"), 0o644); err != nil {
		t.Fatalf("setup workspace file: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "ssh"), filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	tool := NewListDir(root, nil)

	t.Run("named directly", func(t *testing.T) {
		t.Parallel()

		result, err := tool.Execute(context.Background(),
			callWith(t, "c1", map[string]any{"path": "escape"}))

		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		if !result.IsError {
			t.Fatalf("IsError = false, want true: a directory outside the workspace was listed (%q)", result.Content)
		}
		if !strings.Contains(result.Content, "outside the workspace") {
			t.Errorf("content %q does not carry the path-escape message", result.Content)
		}
		if strings.Contains(result.Content, "id_rsa") {
			t.Errorf("content leaked the directory outside the workspace: %q", result.Content)
		}
	})

	t.Run("recursive walk", func(t *testing.T) {
		t.Parallel()

		result, err := tool.Execute(context.Background(),
			callWith(t, "c1", map[string]any{"path": ".", "recursive": true}))

		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		if result.IsError {
			t.Fatalf("unexpected tool error: %q", result.Content)
		}
		if strings.Contains(result.Content, "id_rsa") {
			t.Errorf("the walk listed through a symlink out of the workspace: %q", result.Content)
		}
		if strings.Contains(result.Content, "escape/") {
			t.Errorf("the escaping symlink was descended into as a directory: %q", result.Content)
		}
		if !strings.Contains(result.Content, "inner/") || !strings.Contains(result.Content, "a.go") {
			t.Errorf("the ordinary in-workspace subtree stopped being listed: %q", result.Content)
		}
	})
}

// TestListDir_Execute_ListsUnderAnExtraReadRoot pins that a listing may START in a configured
// read-only root and recurse inside it: the whole walk is pinned to the root the path was
// accepted under, so a subdirectory of an extra root lists exactly as a workspace one does.
// Workspace-relative listings are unchanged, and a directory under no root is still refused
// with the uniform escape message.
func TestListDir_Execute_ListsUnderAnExtraReadRoot(t *testing.T) {
	t.Parallel()

	root, extra, outside := t.TempDir(), t.TempDir(), t.TempDir()
	seedTree(t, root)
	seedTree(t, extra)

	tool := NewListDir(root, func() []string { return []string{extra} })

	cases := []struct {
		name      string
		path      string
		recursive bool
		want      []string // substrings the listing must carry
		wantErr   bool
	}{
		{"extra root itself", extra, false, []string{"top.txt", "src/"}, false},
		{"subdir of the extra root", filepath.Join(extra, "src"), true, []string{"a.go", "b.go"}, false},
		{"workspace relative unchanged", "src", true, []string{"a.go", "b.go"}, false},
		{"under no root", outside, false, []string{"outside the workspace"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(),
				callWith(t, "c1", map[string]any{"path": tc.path, "recursive": tc.recursive}))

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

// skillFixtureLine is the one line the SKILL.md in symlinkedExtraReadRoot's fixture holds:
// grep matches it, and every tool that reports the file names the line's file.
const skillFixtureLine = "name: code-audit"

// symlinkedExtraReadRoot builds the fixture the three WALKING read tools share for the
// symlink-SPELLING case: a workspace, a real extra root holding skill/SKILL.md — real because
// matchRoot only mounts a root that is its own real path — and a symlink to that root from
// somewhere else. It is the shape a dotfiles-managed ~/.apogee/skills hands the model: the
// mount is real, and only the path the model SPELLS runs through a link (audit 2026-08-28
// F-13). extra is the root to MOUNT, link the spelling to search by.
func symlinkedExtraReadRoot(t *testing.T) (workspace, extra, link string) {
	t.Helper()

	workspace, extra = tempRoot(t), tempRoot(t)
	writeFixtureFile(t, filepath.Join(extra, "skill", "SKILL.md"), skillFixtureLine)

	link = filepath.Join(tempRoot(t), "lib")
	if err := os.Symlink(extra, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	return workspace, extra, link
}

// spelledLikeReal runs tool twice — once with the path argument spelled through the symlink,
// once with the mounted root's own name — and returns the spelled run's content, failing
// unless that run succeeded AND answered byte-for-byte what the real spelling answers. The
// equality is the whole point: these tools resolve their path (readScope.resolve), so the
// names they report are measured from the REAL root, and a symlink spelling can neither be
// refused nor leak the link's own name into the answer. Each caller then asserts the names.
func spelledLikeReal(t *testing.T, tool domain.Tool, spelledArgs, realArgs map[string]any) string {
	t.Helper()

	spelled, err := tool.Execute(context.Background(), callWith(t, "c1", spelledArgs))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if spelled.IsError {
		t.Fatalf("the symlink spelling was refused: %q", spelled.Content)
	}

	resolved, err := tool.Execute(context.Background(), callWith(t, "c2", realArgs))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if spelled.Content != resolved.Content {
		t.Fatalf("the symlink spelling answered %q, want the real spelling's answer %q",
			spelled.Content, resolved.Content)
	}
	return spelled.Content
}

// TestListDir_ListsAnExtraRootBySymlinkSpelling pins that a listing whose path runs through a
// symlink to a mounted read-only root is served, and served under the root's REAL names.
// read_file took the other road on the same path and refused it (audit 2026-08-28 F-13); this
// pins the spelling so a future change to readScope.resolve cannot regress list_dir the way
// readBounded regressed.
func TestListDir_ListsAnExtraRootBySymlinkSpelling(t *testing.T) {
	t.Parallel()

	workspace, extra, link := symlinkedExtraReadRoot(t)
	tool := NewListDir(workspace, func() []string { return []string{extra} })

	content := spelledLikeReal(t, tool,
		map[string]any{"path": link, "recursive": true},
		map[string]any{"path": extra, "recursive": true})

	if !strings.Contains(content, "skill/") || !strings.Contains(content, "SKILL.md") {
		t.Errorf("listing %q does not name skill/SKILL.md at its real location", content)
	}
}

func TestListDir_Execute_ToolErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	tool := NewListDir(root, nil)

	cases := []struct {
		name        string
		args        map[string]any
		wantContain string
	}{
		{"not a directory", map[string]any{"path": "f.txt"}, "not a directory"},
		{"missing directory", map[string]any{"path": "absent"}, "directory not found"},
		{"path escape", map[string]any{"path": "../"}, "outside the workspace"},
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

// TestListDir_DangerousActionClassification mirrors delete_file's classification pin from
// the read side: list_dir declares itself read-only, so the write-shaped floor (ADR 0012,
// Rule.WritesOnly) does not fire on a listing that names a guarded WRITE target. The
// load-bearing row is the home skill library — ~/.apogee/skills is a sanctioned extra
// read root and every skill run begins by listing its own skill directory, which the
// ~/.apogee rule hard-refused before the class existed. The command-shaped floor is NOT
// exempted (TestCommandShapedRulesIgnoreTheToolClass in internal/security), so this pins
// only the path-shaped half.
func TestListDir_DangerousActionClassification(t *testing.T) {
	t.Parallel()

	guard := security.DefaultDangerousActionGuard()
	tool := NewListDir(t.TempDir(), nil)

	for _, tc := range []struct {
		name, path string
	}{
		{"the home skill library", "/root/.apogee/skills/security-audit"},
		{"a macOS home skill library", "/Users/alice/.apogee/skills/code-audit"},
		{"the git control plane, read side", ".git/hooks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The call names itself with the tool's OWN name and its OWN argument key, and
			// the guard sees the REAL tool, so the class it judges is the class dispatch
			// hands it.
			call := callWith(t, "c1", map[string]any{"path": tc.path})
			call.Tool = listDirSpec.name

			if d := guard.Inspect(call, tool, nil); d.Triggered() {
				t.Errorf("guard triggered rule %q (tier %v) on a read-only listing of %q, want no trigger",
					d.RuleID, d.Tier, tc.path)
			}
		})
	}
}

// TestListDir_Execute_NewlineInAFilenameCannotForgeARow pins that an entry name carrying a
// line break stays ONE row of the listing: the result is the header plus a single entry row,
// and that row spells the break out rather than pasting it into the grammar.
func TestListDir_Execute_NewlineInAFilenameCannotForgeARow(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedForgingFile(t, root, "x")

	result, err := NewListDir(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "."}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	lines := strings.Split(result.Content, "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want header + 1 row: %q", len(lines), result.Content)
	}
	if !strings.Contains(lines[1], forgingRowSpelling) {
		t.Errorf("row %q does not carry the escaped entry spelling %q", lines[1], forgingRowSpelling)
	}
}

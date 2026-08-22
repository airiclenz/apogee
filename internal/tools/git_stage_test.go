package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stagedNote is the wording a caller injects; the helper must return it verbatim on a stage and
// never invent one of its own.
const stagedNote = " (rename staged in git)"

// gitStatusPorcelain returns `git status --porcelain` for root, which is how these tests read
// what actually landed in the index (the helper's own return value only says what it believes).
func gitStatusPorcelain(t *testing.T, root string) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git on PATH; skipping the live git-staging run")
	}
	cmd := exec.Command(gitPath, "status", "--porcelain")
	cmd.Dir = root
	cmd.Env = safeGitEnv("")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	return string(out)
}

// renameOnDisk performs the filesystem half a tool would have completed before it stages.
func renameOnDisk(t *testing.T, root, from, to string) {
	t.Helper()
	if err := os.Rename(filepath.Join(root, from), filepath.Join(root, to)); err != nil {
		t.Fatalf("rename %s -> %s: %v", from, to, err)
	}
}

// TestStageGitPaths_StagesTrackedRename is the whole point of the helper: a tracked file renamed
// on disk comes back staged as a rename, and the caller's note is returned for its result text.
func TestStageGitPaths_StagesTrackedRename(t *testing.T) {
	root := gitRepo(t)
	renameOnDisk(t, root, "README.md", "GUIDE.md")

	note := stageGitPaths(context.Background(), root, stagedNote, "README.md", "GUIDE.md")

	if note != stagedNote {
		t.Fatalf("note = %q, want %q", note, stagedNote)
	}
	status := gitStatusPorcelain(t, root)
	if !strings.Contains(status, "R  README.md -> GUIDE.md") {
		t.Fatalf("status does not show the staged rename:\n%s", status)
	}
	if strings.Contains(status, "??") {
		t.Fatalf("the destination was left untracked:\n%s", status)
	}
}

// TestStageGitPaths_UntrackedSourceIsLeftAlone pins the trackedness probe: staging an untracked
// file would add content the operator never committed, so the helper must do nothing at all —
// silently, because the file operation itself succeeded.
func TestStageGitPaths_UntrackedSourceIsLeftAlone(t *testing.T) {
	root := gitRepo(t)
	if err := writeFileForTest(root, "scratch.txt", "draft\n"); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	renameOnDisk(t, root, "scratch.txt", "notes.txt")

	if note := stageGitPaths(context.Background(), root, stagedNote, "scratch.txt", "notes.txt"); note != "" {
		t.Fatalf("note = %q, want empty for an untracked source", note)
	}
	status := gitStatusPorcelain(t, root)
	if !strings.Contains(status, "?? notes.txt") {
		t.Fatalf("the untracked file should still be untracked:\n%s", status)
	}
	if strings.Contains(status, "A  ") {
		t.Fatalf("nothing may be staged for an untracked source:\n%s", status)
	}
}

// TestStageGitPaths_OutsideARepository covers the workspace that is no git worktree: the same
// index probe that catches an untracked source catches "not a repository" too, and both skip.
func TestStageGitPaths_OutsideARepository(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	renameOnDisk(t, root, "a.txt", "b.txt")

	if note := stageGitPaths(context.Background(), root, stagedNote, "a.txt", "b.txt"); note != "" {
		t.Fatalf("note = %q, want empty outside a repository", note)
	}
}

// TestStageGitPaths_GitAbsent pins the graceful-absence half of §3a: with no git on PATH the
// helper returns nothing rather than reporting a failure the operator cannot act on.
func TestStageGitPaths_GitAbsent(t *testing.T) {
	withFakeGit(t, false, "")

	root := t.TempDir()
	if note := stageGitPaths(context.Background(), root, stagedNote, "a.txt", "b.txt"); note != "" {
		t.Fatalf("note = %q, want empty when git is absent", note)
	}
}

// TestStageGitPaths_NoPaths guards the degenerate call: no pathspec means `git add -A --` over
// nothing, which git reads as the whole tree — so the helper must return before it runs.
func TestStageGitPaths_NoPaths(t *testing.T) {
	root := gitRepo(t)
	if err := writeFileForTest(root, "stray.txt", "x\n"); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if note := stageGitPaths(context.Background(), root, stagedNote); note != "" {
		t.Fatalf("note = %q, want empty with no paths", note)
	}
	if status := gitStatusPorcelain(t, root); !strings.Contains(status, "?? stray.txt") {
		t.Fatalf("an empty call staged something:\n%s", status)
	}
}

// TestStageGitPaths_GlobMetacharacterFilename pins the :(literal) pathspec magic. Without it
// `file[1].txt` is a character class matching file1.txt — a name that does not exist — so the
// stage would silently miss the file it was handed.
func TestStageGitPaths_GlobMetacharacterFilename(t *testing.T) {
	root := gitRepo(t)
	if err := writeFileForTest(root, "file[1].txt", "bracketed\n"); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	// `git add` globs its pathspec too, so the fixture commits the whole tree rather than
	// naming the bracketed file.
	runInRepo(t, root, "add", "-A", ".")
	runInRepo(t, root, "commit", "-m", "bracketed file")
	renameOnDisk(t, root, "file[1].txt", "file[2].txt")

	note := stageGitPaths(context.Background(), root, stagedNote, "file[1].txt", "file[2].txt")

	if note != stagedNote {
		t.Fatalf("note = %q, want %q", note, stagedNote)
	}
	status := gitStatusPorcelain(t, root)
	if !strings.Contains(status, "R  file[1].txt -> file[2].txt") {
		t.Fatalf("the bracketed rename was not staged:\n%s", status)
	}
}

// TestStageGitPaths_SkippedNoteKeepsOneLine pins the failure wording the callers append: git's
// first line names the problem, and the tail — advice for a human at a terminal — is dropped.
func TestStageGitPaths_SkippedNoteKeepsOneLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		reason string
		want   string
	}{
		{"first line only", "fatal: pathspec did not match\nhint: try something else\n", " (git staging skipped: fatal: pathspec did not match)"},
		{"empty reason", "  \n", " (git staging skipped: git add failed)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := stagingSkipped(tc.reason); got != tc.want {
				t.Fatalf("stagingSkipped(%q) = %q, want %q", tc.reason, got, tc.want)
			}
		})
	}
}

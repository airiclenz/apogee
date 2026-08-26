package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
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

// ----------------------------------------------------------------------------
// The staging child and the confinement box
// ----------------------------------------------------------------------------

// stagingConfiner records the git SUBCOMMAND of every cmd handed to Confine, and can refuse to
// establish the box for one named subcommand. The selective refusal is what makes the two failure
// paths separable: the staging `add` reports a note, while the trackedness probe ahead of it (and
// the command-config probe behind that) is one of the silent skips.
type stagingConfiner struct {
	// failFor is the subcommand whose Confine reports ErrConfinementUnavailable ("" = all succeed).
	failFor string

	mu   sync.Mutex
	seen []string
}

func (c *stagingConfiner) Capabilities() domain.ConfinementCaps {
	return domain.ConfinementCaps{FSWrite: true}
}

// Confine leaves cmd untouched, so a "confined" run still executes the real git in these
// hermetic tests (the dev host has no landlock — contract §6). What is asserted is that the box
// was ASKED for, which is the whole of the handoff this helper is responsible for.
func (c *stagingConfiner) Confine(_ context.Context, _ domain.ConfinementBox, cmd *exec.Cmd) error {
	sub := gitSubcommandOf(cmd.Args)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, sub)
	if c.failFor != "" && sub == c.failFor {
		return fmt.Errorf("%w: fake", domain.ErrConfinementUnavailable)
	}
	return nil
}

func (c *stagingConfiner) subcommands() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.seen)
}

// gitSubcommandOf picks the subcommand out of a git argv, stepping over the `-c <name=value>`
// hardening pairs gitRunSpec puts ahead of it.
func gitSubcommandOf(argv []string) string {
	for i := 1; i < len(argv); i++ {
		if argv[i] == "-c" {
			i++
			continue
		}
		return argv[i]
	}
	return ""
}

// confinedStagingCtx is the context a workspace-scoped writer's call carries in Auto with
// confine-to-workspace: the Confinement handle the Run verdict installed (internal/agent's
// confineChildren), which is what runGit reads to fence the staging child.
func confinedStagingCtx(c domain.Confiner, root string) context.Context {
	return domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: c,
		Box:      domain.ConfinementBox{WorkspaceRoot: root},
	})
}

// TestStageGitPaths_ConfinesTheGitChild is the staging half of F-03: with the handle on ctx every
// git child this helper spawns is handed to the Confiner before it runs, so a move_file in Auto
// fences its index update exactly as a git_status call would be fenced.
func TestStageGitPaths_ConfinesTheGitChild(t *testing.T) {
	root := gitRepo(t)
	renameOnDisk(t, root, "README.md", "GUIDE.md")
	conf := &stagingConfiner{}

	note := stageGitPaths(confinedStagingCtx(conf, root), root, stagedNote, "README.md", "GUIDE.md")

	if note != stagedNote {
		t.Fatalf("note = %q, want %q", note, stagedNote)
	}
	seen := conf.subcommands()
	for _, want := range []string{"ls-files", "add"} {
		if !slices.Contains(seen, want) {
			t.Errorf("git %s was not confined; Confine saw %v", want, seen)
		}
	}
	if status := gitStatusPorcelain(t, root); !strings.Contains(status, "R  README.md -> GUIDE.md") {
		t.Fatalf("the confined run did not stage the rename:\n%s", status)
	}
}

// TestStageGitPaths_UnconfinableChildIsANote pins the best-effort floor when the box cannot be
// established: the file operation has already happened, so neither branch may fail the call or
// signal the D4 demote. A staging `add` that could not be confined is reported as the skipped
// note the model reads; a probe that could not be confined joins the silent skips, exactly as an
// absent git or an untracked source does.
func TestStageGitPaths_UnconfinableChildIsANote(t *testing.T) {
	t.Run("the staging add reports the note", func(t *testing.T) {
		root := gitRepo(t)
		renameOnDisk(t, root, "README.md", "GUIDE.md")
		conf := &stagingConfiner{failFor: "add"}

		note := stageGitPaths(confinedStagingCtx(conf, root), root, stagedNote, "README.md", "GUIDE.md")

		if !strings.HasPrefix(note, " (git staging skipped:") {
			t.Fatalf("note = %q, want a staging-skipped note", note)
		}
		if strings.Contains(gitStatusPorcelain(t, root), "R  ") {
			t.Error("nothing may be staged when the add was never confined")
		}
	})

	t.Run("the trackedness probe skips silently", func(t *testing.T) {
		root := gitRepo(t)
		renameOnDisk(t, root, "README.md", "GUIDE.md")
		conf := &stagingConfiner{failFor: "ls-files"}

		if note := stageGitPaths(confinedStagingCtx(conf, root), root, stagedNote, "README.md", "GUIDE.md"); note != "" {
			t.Fatalf("note = %q, want empty: an unconfinable probe is a silent skip", note)
		}
	})
}

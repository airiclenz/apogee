package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// withFakeGit swaps lookGit for the duration of a test (restored on cleanup), so the
// graceful-degradation and confine paths are exercisable without depending on the host.
func withFakeGit(t *testing.T, found bool, path string) {
	t.Helper()
	orig := lookGit
	lookGit = func() (string, bool) { return path, found }
	t.Cleanup(func() { lookGit = orig })
}

// gitRepo creates an initialized git repository in a fresh temp dir with a committed
// file on a known branch, skipping the test when git is unavailable (the tool's
// graceful contract — the live behaviour is only assertable where git exists).
func gitRepo(t *testing.T) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git on PATH; skipping the live git-tool run")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitPath, args...)
		cmd.Dir = root
		// A deterministic identity + main branch so the tests do not depend on host config.
		cmd.Env = append(safeGitEnv(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := writeFileForTest(root, "README.md", "hello\n"); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	run("add", "README.md")
	run("commit", "-m", "initial")
	return root
}

func writeFileForTest(root, name, content string) error {
	abs, err := resolveInRoot(name, root)
	if err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0o644)
}

func branchCall(id, args string) domain.ToolCall {
	return domain.ToolCall{ID: id, Tool: "git_branch", Arguments: []byte(args)}
}

func commitCall(id, args string) domain.ToolCall {
	return domain.ToolCall{ID: id, Tool: "git_commit", Arguments: []byte(args)}
}

func diffCall(id, args string) domain.ToolCall {
	return domain.ToolCall{ID: id, Tool: "git_diff_range", Arguments: []byte(args)}
}

// ----------------------------------------------------------------------------
// Markers
// ----------------------------------------------------------------------------

func TestGit_Markers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	br := NewGitBranch(root)
	if br.Name() != "git_branch" {
		t.Errorf("branch Name() = %q", br.Name())
	}
	if br.ReadOnly() {
		t.Error("git_branch must be write-capable (ReadOnly()==false)")
	}
	if !domain.IsSubprocessTool(br) {
		t.Error("git_branch must be a SubprocessTool")
	}
	if IsWorkspaceScopedWriter(br) {
		t.Error("git_branch must NOT carry the workspaceScopedWriter marker (it is OS-confined)")
	}

	co := NewGitCommit(root)
	if co.ReadOnly() {
		t.Error("git_commit must be write-capable (ReadOnly()==false)")
	}
	if !domain.IsSubprocessTool(co) {
		t.Error("git_commit must be a SubprocessTool")
	}

	dr := NewGitDiffRange(root)
	if !domain.IsReadOnly(dr) {
		t.Error("git_diff_range must be ReadOnly (a diff is harmless inspection)")
	}
	if !domain.IsSubprocessTool(dr) {
		t.Error("git_diff_range must still be a SubprocessTool")
	}
}

// ----------------------------------------------------------------------------
// Graceful degradation when git is absent (§3a)
// ----------------------------------------------------------------------------

func TestGit_GracefulWhenAbsent(t *testing.T) {
	withFakeGit(t, false, "")
	root := t.TempDir()

	cases := []struct {
		name string
		exec func() (domain.ToolResult, error)
	}{
		{"branch", func() (domain.ToolResult, error) {
			return NewGitBranch(root).Execute(context.Background(), branchCall("c1", `{"action":"list"}`))
		}},
		{"commit", func() (domain.ToolResult, error) {
			return NewGitCommit(root).Execute(context.Background(), commitCall("c1", `{"message":"x"}`))
		}},
		{"diff", func() (domain.ToolResult, error) {
			return NewGitDiffRange(root).Execute(context.Background(), diffCall("c1", `{"base":"a","head":"b"}`))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := tc.exec()
			if err != nil {
				t.Fatalf("Execute err = %v, want nil (absence must degrade, not crash)", err)
			}
			if !res.IsError || !strings.Contains(res.Content, "git not available") {
				t.Errorf("result = %q, want a clear 'git not available' result", res.Content)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Argument validation (no git needed — rejected before the subprocess)
// ----------------------------------------------------------------------------

func TestGitBranch_InvalidAction(t *testing.T) {
	t.Parallel()
	res, err := NewGitBranch(t.TempDir()).Execute(context.Background(), branchCall("c1", `{"action":"rebase"}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "action must be one of") {
		t.Errorf("result = %q, want an invalid-action error", res.Content)
	}
}

func TestGitBranch_NameRequired(t *testing.T) {
	t.Parallel()
	res, err := NewGitBranch(t.TempDir()).Execute(context.Background(), branchCall("c1", `{"action":"create"}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "name is required") {
		t.Errorf("result = %q, want a name-required error", res.Content)
	}
}

func TestGitBranch_ProtectedDeleteBlocked(t *testing.T) {
	// Not parallel: withFakeGit swaps the package-level lookGit var.
	// No git needed: the protected-branch check rejects before the subprocess.
	withFakeGit(t, true, "/usr/bin/git")
	for _, name := range []string{"main", "Master", "develop", "development"} {
		res, err := NewGitBranch(t.TempDir()).Execute(context.Background(),
			branchCall("c1", fmt.Sprintf(`{"action":"delete","name":%q}`, name)))
		if err != nil {
			t.Fatalf("Execute err = %v", err)
		}
		if !res.IsError || !strings.Contains(res.Content, "protected branch") {
			t.Errorf("deleting %q: result = %q, want a protected-branch refusal", name, res.Content)
		}
	}
}

// TestGitBranch_RejectsOptionLikeArgs proves the SEC-06 leading-"-" guard: a model-supplied
// branch name or start-point that git would read as an option flag is refused before the
// subprocess runs (the git tools use argv arrays, so this is the remaining injection class).
func TestGitBranch_RejectsOptionLikeArgs(t *testing.T) {
	// Not parallel: withFakeGit swaps the package-level lookGit var. The guard rejects before
	// the subprocess, but a present git keeps the test honest that the guard — not a missing
	// git — is what blocks.
	withFakeGit(t, true, "/usr/bin/git")

	cases := []struct {
		name    string
		args    string
		wantMsg string
	}{
		{"create name -D", `{"action":"create","name":"-D"}`, "branch name may not begin with '-'"},
		{"switch option name", `{"action":"switch","name":"--orphan"}`, "branch name may not begin with '-'"},
		{"delete option name", `{"action":"delete","name":"-rf"}`, "branch name may not begin with '-'"},
		{"create option start_point", `{"action":"create","name":"feature","start_point":"--upload-pack=evil"}`, "start_point may not begin with '-'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := NewGitBranch(t.TempDir()).Execute(context.Background(), branchCall("c1", tc.args))
			if err != nil {
				t.Fatalf("Execute err = %v", err)
			}
			if !res.IsError || !strings.Contains(res.Content, tc.wantMsg) {
				t.Errorf("args %s: result = %q, want %q", tc.args, res.Content, tc.wantMsg)
			}
		})
	}
}

func TestGitCommit_MessageRequired(t *testing.T) {
	t.Parallel()
	res, err := NewGitCommit(t.TempDir()).Execute(context.Background(), commitCall("c1", `{"message":"   "}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "message is required") {
		t.Errorf("result = %q, want a message-required error", res.Content)
	}
}

func TestGitDiffRange_RefValidation(t *testing.T) {
	t.Parallel()
	dr := NewGitDiffRange(t.TempDir())

	res, err := dr.Execute(context.Background(), diffCall("c1", `{"base":"","head":"x"}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "base ref is required") {
		t.Errorf("empty base: result = %q", res.Content)
	}

	// An injection-shaped ref (a space + an option) must be rejected by the ref class.
	res, err = dr.Execute(context.Background(), diffCall("c2", `{"base":"main","head":"x; rm -rf /"}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "invalid head ref") {
		t.Errorf("malformed head: result = %q, want an invalid-ref error", res.Content)
	}

	// SEC-06: a leading-"-" ref passes the validRef character class (which permits "-") but
	// git would read it as an option even after the "..." join — it must be rejected.
	res, err = dr.Execute(context.Background(), diffCall("c3", `{"base":"--output=/tmp/evil","head":"main"}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "invalid base ref") {
		t.Errorf("leading-dash base: result = %q, want an invalid-ref error", res.Content)
	}
}

func TestGitDiffRange_PathEscapeRejected(t *testing.T) {
	// Not parallel: withFakeGit swaps the package-level lookGit var.
	withFakeGit(t, true, "/usr/bin/git")
	dr := NewGitDiffRange(t.TempDir())
	res, err := dr.Execute(context.Background(),
		diffCall("c1", `{"base":"main","head":"dev","paths":["../../etc/passwd"]}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError {
		t.Errorf("a path escaping the root must be rejected; result = %q", res.Content)
	}
}

// ----------------------------------------------------------------------------
// Live behaviour against a real temp repo (skips when git is absent)
// ----------------------------------------------------------------------------

func TestGitBranch_CreateSwitchListDelete(t *testing.T) {
	root := gitRepo(t)
	br := NewGitBranch(root)

	// create
	res, err := br.Execute(context.Background(), branchCall("c1", `{"action":"create","name":"feature"}`))
	if err != nil {
		t.Fatalf("create err = %v", err)
	}
	if res.IsError {
		t.Fatalf("create errored: %q", res.Content)
	}

	// list shows the new branch
	res, err = br.Execute(context.Background(), branchCall("c2", `{"action":"list"}`))
	if err != nil {
		t.Fatalf("list err = %v", err)
	}
	if res.IsError || !strings.Contains(res.Content, "feature") {
		t.Errorf("list = %q, want it to contain 'feature'", res.Content)
	}

	// switch back to main, then delete the merged-off feature (safe -d).
	if _, err := br.Execute(context.Background(), branchCall("c3", `{"action":"switch","name":"main"}`)); err != nil {
		t.Fatalf("switch err = %v", err)
	}
	res, err = br.Execute(context.Background(), branchCall("c4", `{"action":"delete","name":"feature"}`))
	if err != nil {
		t.Fatalf("delete err = %v", err)
	}
	if res.IsError {
		t.Errorf("delete of a no-new-commits branch should succeed; got %q", res.Content)
	}
}

func TestGitCommit_StagesAndCommits(t *testing.T) {
	root := gitRepo(t)
	if err := writeFileForTest(root, "new.txt", "added\n"); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	co := NewGitCommit(root)
	res, err := co.Execute(context.Background(), commitCall("c1", `{"message":"add new.txt","files":["new.txt"]}`))
	if err != nil {
		t.Fatalf("commit err = %v", err)
	}
	if res.IsError {
		t.Fatalf("commit errored: %q", res.Content)
	}
	// The one-line summary carries the message.
	if !strings.Contains(res.Content, "add new.txt") {
		t.Errorf("commit summary = %q, want it to mention the message", res.Content)
	}
}

func TestGitCommit_PathEscapeRejected(t *testing.T) {
	root := gitRepo(t)
	co := NewGitCommit(root)
	res, err := co.Execute(context.Background(),
		commitCall("c1", `{"message":"x","files":["../../etc/passwd"]}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError {
		t.Errorf("staging a file outside the root must be rejected; result = %q", res.Content)
	}
}

func TestGitDiffRange_ShowsDiff(t *testing.T) {
	root := gitRepo(t)
	gitPath, _ := exec.LookPath("git")
	env := append(safeGitEnv(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	runIn := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitPath, args...)
		cmd.Dir = root
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Branch off main, add a commit on the branch, so main...feature has a diff.
	runIn("checkout", "-b", "feature")
	if err := writeFileForTest(root, "feature.txt", "feature work\n"); err != nil {
		t.Fatalf("write feature file: %v", err)
	}
	runIn("add", "feature.txt")
	runIn("commit", "-m", "feature commit")

	dr := NewGitDiffRange(root)
	res, err := dr.Execute(context.Background(), diffCall("c1", `{"base":"main","head":"feature","name_only":true}`))
	if err != nil {
		t.Fatalf("diff err = %v", err)
	}
	if res.IsError || !strings.Contains(res.Content, "feature.txt") {
		t.Errorf("diff = %q, want it to name feature.txt", res.Content)
	}
}

// ----------------------------------------------------------------------------
// Confinement handoff + the "confine if you can, gate if you can't" net
// ----------------------------------------------------------------------------

func TestGitBranch_RunsUnderConfine(t *testing.T) {
	root := gitRepo(t)
	br := NewGitBranch(root)
	conf := &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}}
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: conf,
		Box:      domain.ConfinementBox{WorkspaceRoot: root},
	})

	res, err := br.Execute(ctx, branchCall("c1", `{"action":"list"}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if conf.confineCount() != 1 {
		t.Errorf("Confine called %d times, want 1 (the tool must confine the cmd it builds)", conf.confineCount())
	}
	if res.IsError {
		t.Errorf("confined list errored: %q", res.Content)
	}
}

func TestGitCommit_ConfinementUnavailablePropagates(t *testing.T) {
	// Not parallel: withFakeGit swaps the package-level lookGit var.
	withFakeGit(t, true, "/usr/bin/git")
	co := NewGitCommit(t.TempDir())
	conf := &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}, unavailable: true}
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: conf,
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})

	_, err := co.Execute(ctx, commitCall("c1", `{"message":"should not run"}`))
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Fatalf("Execute err = %v, want ErrConfinementUnavailable (must not run unconfined)", err)
	}
}

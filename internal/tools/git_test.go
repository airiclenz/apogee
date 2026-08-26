package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
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
		cmd.Env = append(safeGitEnv(""),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	// The env vars above cover only this helper's own git calls; the tools under
	// test run git with the scrubbed host env (safeGitEnv), which carries no
	// identity on CI runners — a repo-local identity keeps their commits working.
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
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

func statusCall(id string) domain.ToolCall {
	return domain.ToolCall{ID: id, Tool: "git_status"}
}

func logCall(id, args string) domain.ToolCall {
	return domain.ToolCall{ID: id, Tool: "git_log", Arguments: []byte(args)}
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

	// git_diff_range carries BOTH: the read-only declaration (what keeps it in Plan mode's
	// menu) and the subprocess marker (what classifies the call). The marker outranks the
	// declaration — confinement-execution-contract §4, amended 2026-07-26 — so dropping
	// either one moves the call's class; the agent-side pin is TestClassifyTool.
	dr := NewGitDiffRange(root)
	if !domain.IsReadOnly(dr) {
		t.Error("git_diff_range must be ReadOnly (a diff is harmless inspection)")
	}
	if !domain.IsSubprocessTool(dr) {
		t.Error("git_diff_range must still be a SubprocessTool (it launches the system git)")
	}

	// git_status carries the same pair as git_diff_range: an honest read-only declaration and
	// the subprocess marker that outranks it.
	st := NewGitStatus(root)
	if st.Name() != "git_status" {
		t.Errorf("status Name() = %q", st.Name())
	}
	if !domain.IsReadOnly(st) {
		t.Error("git_status must be ReadOnly (reporting the tree changes nothing)")
	}
	if !domain.IsSubprocessTool(st) {
		t.Error("git_status must be a SubprocessTool (it launches the system git)")
	}
	if IsWorkspaceScopedWriter(st) {
		t.Error("git_status must NOT carry the workspaceScopedWriter marker (it is OS-confined)")
	}

	// git_log carries the same pair for the same reason: reading history writes nothing, but
	// the subprocess marker is what classifies the call.
	lg := NewGitLog(root)
	if lg.Name() != "git_log" {
		t.Errorf("log Name() = %q", lg.Name())
	}
	if !domain.IsReadOnly(lg) {
		t.Error("git_log must be ReadOnly (reading history changes nothing)")
	}
	if !domain.IsSubprocessTool(lg) {
		t.Error("git_log must be a SubprocessTool (it launches the system git)")
	}
	if IsWorkspaceScopedWriter(lg) {
		t.Error("git_log must NOT carry the workspaceScopedWriter marker (it is OS-confined)")
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
		{"status", func() (domain.ToolResult, error) {
			return NewGitStatus(root).Execute(context.Background(), statusCall("c1"))
		}},
		{"log", func() (domain.ToolResult, error) {
			return NewGitLog(root).Execute(context.Background(), logCall("c1", `{}`))
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

// TestBuildBranchArgs_TerminatesRefPosition pins the exact argv for the two checkout forms:
// both end in "--", so git can never read a non-ref, path-shaped name (or start-point) as a
// pathspec and revert tracked files. Dropping the terminator reopens that class.
func TestBuildBranchArgs_TerminatesRefPosition(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args gitBranchArgs
		want []string
	}{
		{
			name: "switch",
			args: gitBranchArgs{Action: "switch", Name: "docs"},
			want: []string{"checkout", "docs", "--"},
		},
		{
			name: "create without start point",
			args: gitBranchArgs{Action: "create", Name: "feature"},
			want: []string{"checkout", "-b", "feature", "--"},
		},
		{
			name: "create with start point",
			args: gitBranchArgs{Action: "create", Name: "feature", StartPoint: "docs"},
			want: []string{"checkout", "-b", "feature", "docs", "--"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, errMsg := buildBranchArgs(tc.args)
			if errMsg != "" {
				t.Fatalf("buildBranchArgs(%+v) rejected: %s", tc.args, errMsg)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("argv = %q, want %q", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("argv = %q, want %q", got, tc.want)
				}
			}
		})
	}
}

// TestGitBranch_SwitchToPathShapedNameKeepsEdits is the behavioural half: switching to a
// branch that does not exist but names a tracked directory must fail, not silently restore
// that directory from the index. Without the "--" terminator git reports "Updated 1 path from
// the index" with exit 0, the tool reports success, and the uncommitted edit is gone with no
// undo — the user was never told.
func TestGitBranch_SwitchToPathShapedNameKeepsEdits(t *testing.T) {
	root := gitRepo(t)
	gitPath, _ := exec.LookPath("git")
	runIn := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitPath, args...)
		cmd.Dir = root
		cmd.Env = append(safeGitEnv(""),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// A committed docs/notes.md, so "docs" is a tracked path but not a ref.
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := writeFileForTest(root, "docs/notes.md", "committed\n"); err != nil {
		t.Fatalf("seed docs/notes.md: %v", err)
	}
	runIn("add", "docs/notes.md")
	runIn("commit", "-m", "add notes")

	const edit = "uncommitted work that must survive\n"
	if err := writeFileForTest(root, "docs/notes.md", edit); err != nil {
		t.Fatalf("edit docs/notes.md: %v", err)
	}

	res, err := NewGitBranch(root).Execute(context.Background(), branchCall("c1", `{"action":"switch","name":"docs"}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError {
		t.Errorf("switching to non-existent branch 'docs' reported success (%q); it must fail, not check out the path", res.Content)
	}

	got, err := os.ReadFile(filepath.Join(root, "docs", "notes.md"))
	if err != nil {
		t.Fatalf("read back docs/notes.md: %v", err)
	}
	if string(got) != edit {
		t.Errorf("docs/notes.md = %q, want the uncommitted edit %q to survive byte-identical", got, edit)
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
	env := append(safeGitEnv(""),
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
	if conf.confineCount() != 3 {
		t.Errorf("Confine called %d times, want 3 (the two filter-driver config probes and the branch list — every git subprocess the tool builds is confined)", conf.confineCount())
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

// ----------------------------------------------------------------------------
// git_status — porcelain v2 parsing, the caps, and the live tree
// ----------------------------------------------------------------------------

// porcelainZ joins records the way `git status --porcelain=v2 -z` emits them: every record,
// headers included, terminated by a NUL.
func porcelainZ(records ...string) string {
	var b strings.Builder
	for _, record := range records {
		b.WriteString(record)
		b.WriteString("\x00")
	}
	return b.String()
}

// TestGitStatus_ParsesPorcelainRecords covers the record shapes against the renderer, which is
// where a mis-split path or a mis-filed XY code becomes visible to the model.
func TestGitStatus_ParsesPorcelainRecords(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		out  string
		want []string
		deny []string
	}{
		{
			name: "branch with upstream divergence",
			out: porcelainZ(
				"# branch.oid abc123",
				"# branch.head main",
				"# branch.upstream origin/main",
				"# branch.ab +2 -1",
			),
			want: []string{"On branch main", "Upstream origin/main: ahead 2, behind 1", "Working tree clean"},
		},
		{
			name: "detached head",
			out:  porcelainZ("# branch.oid abc123", "# branch.head (detached)"),
			want: []string{"HEAD detached"},
			deny: []string{"On branch"},
		},
		{
			name: "no upstream states no divergence",
			out:  porcelainZ("# branch.head feature"),
			want: []string{"On branch feature"},
			deny: []string{"Upstream", "ahead"},
		},
		{
			name: "a malformed ab header leaves the divergence unstated",
			out:  porcelainZ("# branch.head main", "# branch.upstream origin/main", "# branch.ab nonsense"),
			want: []string{"Upstream origin/main"},
			deny: []string{"ahead"},
		},
		{
			name: "a path changed on both sides is listed on both",
			out: porcelainZ("# branch.head main",
				"1 MM N... 100644 100644 100644 aaaa bbbb pkg/my file.go"),
			want: []string{"Staged (1):", "M  pkg/my file.go", "Unstaged (1):"},
		},
		{
			name: "a rename carries the original path",
			out: porcelainZ("# branch.head main",
				"2 R. N... 100644 100644 100644 aaaa bbbb R100 new.go", "old.go"),
			want: []string{"Staged (1):", "R  new.go (from old.go)"},
			deny: []string{"Unstaged"},
		},
		{
			name: "an unmerged path is listed once, as unstaged",
			out: porcelainZ("# branch.head main",
				"u UU N... 100644 100644 100644 100644 aaaa bbbb cccc conflict.go"),
			want: []string{"Unstaged (1):", "U  conflict.go (unmerged)"},
			deny: []string{"Staged"},
		},
		{
			name: "untracked names survive their spaces",
			out:  porcelainZ("# branch.head main", "? notes and more.txt"),
			want: []string{"Untracked (1):", "notes and more.txt"},
		},
		{
			name: "an unrecognised record is skipped, not fatal",
			out:  porcelainZ("warning: something git said", "# branch.head main", "? kept.txt"),
			want: []string{"On branch main", "Untracked (1):", "kept.txt"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := renderGitStatus(parseGitStatus(tc.out))
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("status = %q, want it to contain %q", got, want)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(got, deny) {
					t.Errorf("status = %q, want it NOT to contain %q", got, deny)
				}
			}
		})
	}
}

// TestGitStatus_CapsEachList proves the bound: a section longer than the cap shows the cap's
// worth of paths, states the FULL count in its header, and names how many it withheld — so a
// repository mid-refactor cannot flood the model's context.
func TestGitStatus_CapsEachList(t *testing.T) {
	t.Parallel()

	const extra = 5
	records := []string{"# branch.head main"}
	for i := 0; i < maxGitStatusPaths+extra; i++ {
		records = append(records, fmt.Sprintf("? file%d.txt", i))
	}
	got := renderGitStatus(parseGitStatus(porcelainZ(records...)))

	if want := fmt.Sprintf("Untracked (%d):", maxGitStatusPaths+extra); !strings.Contains(got, want) {
		t.Errorf("status = %q, want the header %q with the full count", got, want)
	}
	if n := strings.Count(got, "\n  file"); n != maxGitStatusPaths {
		t.Errorf("listed %d paths, want the cap of %d", n, maxGitStatusPaths)
	}
	if want := fmt.Sprintf("[...%d more]", extra); !strings.Contains(got, want) {
		t.Errorf("status = %q, want the truncation note %q", got, want)
	}
}

func TestGitStatus_CleanTree(t *testing.T) {
	root := gitRepo(t)

	res, err := NewGitStatus(root).Execute(context.Background(), statusCall("c1"))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if res.IsError {
		t.Fatalf("status errored: %q", res.Content)
	}
	if !strings.Contains(res.Content, "On branch main") || !strings.Contains(res.Content, "Working tree clean") {
		t.Errorf("status = %q, want the branch line and a clean tree", res.Content)
	}
}

func TestGitStatus_ReportsStagedUnstagedAndUntracked(t *testing.T) {
	root := gitRepo(t)
	gitPath, _ := exec.LookPath("git")
	runIn := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitPath, args...)
		cmd.Dir = root
		cmd.Env = safeGitEnv("")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	if err := writeFileForTest(root, "staged.txt", "new and staged\n"); err != nil {
		t.Fatalf("write staged.txt: %v", err)
	}
	runIn("add", "staged.txt")
	if err := writeFileForTest(root, "README.md", "edited, not staged\n"); err != nil {
		t.Fatalf("edit README.md: %v", err)
	}
	if err := writeFileForTest(root, "untracked.txt", "never added\n"); err != nil {
		t.Fatalf("write untracked.txt: %v", err)
	}

	res, err := NewGitStatus(root).Execute(context.Background(), statusCall("c1"))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if res.IsError {
		t.Fatalf("status errored: %q", res.Content)
	}
	for _, want := range []string{
		"Staged (1):", "A  staged.txt",
		"Unstaged (1):", "M  README.md",
		"Untracked (1):", "untracked.txt",
	} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("status = %q, want it to contain %q", res.Content, want)
		}
	}
}

func TestGitStatus_DetachedHead(t *testing.T) {
	root := gitRepo(t)
	gitPath, _ := exec.LookPath("git")
	cmd := exec.Command(gitPath, "checkout", "--detach")
	cmd.Dir = root
	cmd.Env = safeGitEnv("")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout --detach: %v\n%s", err, out)
	}

	res, err := NewGitStatus(root).Execute(context.Background(), statusCall("c1"))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if res.IsError {
		t.Fatalf("status errored: %q", res.Content)
	}
	if !strings.Contains(res.Content, "HEAD detached") {
		t.Errorf("status = %q, want it to report a detached HEAD", res.Content)
	}
}

// TestGitStatus_NotARepoMatchesOtherGitTools pins the error SHAPE: outside a repository
// git_status fails the way the other git tools do — an IsError result carrying git's own
// message, never a Go error and never a success claiming a clean tree.
func TestGitStatus_NotARepoMatchesOtherGitTools(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH; skipping the live git-tool run")
	}
	root := t.TempDir()

	status, err := NewGitStatus(root).Execute(context.Background(), statusCall("c1"))
	if err != nil {
		t.Fatalf("status Execute err = %v, want the failure as a result", err)
	}
	diff, err := NewGitDiffRange(root).Execute(context.Background(), diffCall("c2", `{"base":"HEAD","head":"HEAD"}`))
	if err != nil {
		t.Fatalf("diff Execute err = %v, want the failure as a result", err)
	}

	// The shape both tools share: a failed git is an IsError RESULT carrying git's own words —
	// never a Go error, and never a success claiming a clean tree. (The words differ per
	// subcommand; git_status gets the plain "not a git repository", which is the point of
	// surfacing git's message rather than a wording of our own.)
	if !status.IsError || !diff.IsError {
		t.Fatalf("outside a repo both tools must be IsError; status=%v diff=%v", status.IsError, diff.IsError)
	}
	if strings.TrimSpace(diff.Content) == "" {
		t.Error("git_diff_range outside a repo must still carry git's message")
	}
	if !strings.Contains(status.Content, "not a git repository") {
		t.Errorf("status = %q, want git's own not-a-repository message", status.Content)
	}
}

// ----------------------------------------------------------------------------
// git_log (2026-08-10)
// ----------------------------------------------------------------------------

// gitLogLine is the exact three-field shape one git_log line must have: short hash, an
// iso-strict (space-free) timestamp, then the subject. Pinning it here is what keeps the
// --format/--date pair from drifting into something a model cannot split positionally.
// The zone is the one part that is NOT byte-stable across git versions: a UTC offset is
// spelled "Z" by newer git and "+00:00" by older, both RFC 3339 for the same instant, so
// the alternation is the drift the shape tolerates — and the only one.
var gitLogLine = regexp.MustCompile(`^[0-9a-f]{7,40} \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}([+-]\d{2}:\d{2}|Z) .+$`)

// commitInRepo adds a one-file commit to an existing test repo, so a log test has real
// history to page through.
func commitInRepo(t *testing.T, root, name, subject string) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git on PATH; skipping the live git-tool run")
	}
	if err := writeFileForTest(root, name, subject+"\n"); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	for _, args := range [][]string{{"add", name}, {"commit", "-m", subject}} {
		cmd := exec.Command(gitPath, args...)
		cmd.Dir = root
		cmd.Env = safeGitEnv("")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestGitLog_ClampsMaxCount(t *testing.T) {
	t.Parallel()

	// A non-positive count is JSON-absent (or nonsense) and takes the default; anything above
	// the ceiling is pinned to it; a sane request passes through untouched.
	for _, tc := range []struct{ in, want int }{
		{0, defaultGitLogCount},
		{-7, defaultGitLogCount},
		{1, 1},
		{20, 20},
		{maxGitLogCount, maxGitLogCount},
		{maxGitLogCount + 1, maxGitLogCount},
		{100000, maxGitLogCount},
	} {
		if got := clampGitLogCount(tc.in); got != tc.want {
			t.Errorf("clampGitLogCount(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestGitLog_RefValidation proves the ref guard rejects before the subprocess: the same
// conservative character class git_diff_range uses, plus the explicit leading-"-" rejection
// that stops a ref being read as an option flag (SEC-06).
func TestGitLog_RefValidation(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{
		"main; rm -rf /",
		"main$(whoami)",
		"--output=/tmp/pwned",
		"-n",
		"a b",
	} {
		res, err := NewGitLog(t.TempDir()).Execute(context.Background(), logCall("c1", fmt.Sprintf(`{"ref":%q}`, ref)))
		if err != nil {
			t.Fatalf("Execute(%q) err = %v, want the rejection as a result", ref, err)
		}
		if !res.IsError || !strings.Contains(res.Content, "invalid ref") {
			t.Errorf("ref %q = %q, want an invalid-ref result", ref, res.Content)
		}
	}
}

func TestGitLog_DefaultsToHEADNewestFirst(t *testing.T) {
	root := gitRepo(t)
	commitInRepo(t, root, "second.txt", "second subject")
	commitInRepo(t, root, "third.txt", "third subject")

	res, err := NewGitLog(root).Execute(context.Background(), logCall("c1", `{}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if res.IsError {
		t.Fatalf("log errored: %q", res.Content)
	}

	lines := strings.Split(strings.TrimSpace(res.Content), "\n")
	if len(lines) != 3 {
		t.Fatalf("log = %q, want 3 commit lines", res.Content)
	}
	// Newest first, and every line carries the pinned hash/date/subject shape.
	for i, want := range []string{"third subject", "second subject", "initial"} {
		if !strings.HasSuffix(lines[i], want) {
			t.Errorf("line %d = %q, want it to end with %q (newest first)", i, lines[i], want)
		}
		if !gitLogLine.MatchString(lines[i]) {
			t.Errorf("line %d = %q, want the short-hash / iso-strict-date / subject shape", i, lines[i])
		}
	}
}

func TestGitLog_MaxCountLimitsCommits(t *testing.T) {
	root := gitRepo(t)
	commitInRepo(t, root, "second.txt", "second subject")
	commitInRepo(t, root, "third.txt", "third subject")

	res, err := NewGitLog(root).Execute(context.Background(), logCall("c1", `{"max_count":2}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if res.IsError {
		t.Fatalf("log errored: %q", res.Content)
	}
	lines := strings.Split(strings.TrimSpace(res.Content), "\n")
	if len(lines) != 2 {
		t.Fatalf("log = %q, want exactly 2 commit lines", res.Content)
	}
	if !strings.HasSuffix(lines[0], "third subject") {
		t.Errorf("line 0 = %q, want the newest commit", lines[0])
	}

	// An over-ceiling request is clamped, not refused: the call still succeeds and simply
	// returns everything the (shorter) history holds.
	over, err := NewGitLog(root).Execute(context.Background(), logCall("c2", `{"max_count":100000}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if over.IsError {
		t.Fatalf("over-ceiling log errored: %q", over.Content)
	}
	if got := len(strings.Split(strings.TrimSpace(over.Content), "\n")); got != 3 {
		t.Errorf("over-ceiling log returned %d lines, want the whole 3-commit history", got)
	}
}

func TestGitLog_ExplicitRef(t *testing.T) {
	root := gitRepo(t)
	commitInRepo(t, root, "second.txt", "second subject")

	// An older commit named explicitly logs only the history up to it, proving the ref is
	// honoured rather than silently replaced by HEAD.
	res, err := NewGitLog(root).Execute(context.Background(), logCall("c1", `{"ref":"HEAD~1"}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if res.IsError {
		t.Fatalf("log errored: %q", res.Content)
	}
	if strings.Contains(res.Content, "second subject") {
		t.Errorf("log of HEAD~1 = %q, must not include the commit above it", res.Content)
	}
	if !strings.Contains(res.Content, "initial") {
		t.Errorf("log of HEAD~1 = %q, want the initial commit", res.Content)
	}

	// A branch name is the everyday form and must work the same way.
	byBranch, err := NewGitLog(root).Execute(context.Background(), logCall("c2", `{"ref":"main"}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if byBranch.IsError || !strings.Contains(byBranch.Content, "second subject") {
		t.Errorf("log of main = %q, want the full branch history", byBranch.Content)
	}
}

// TestGitLog_UnknownRefMatchesOtherGitTools pins the error SHAPE: a ref that passes the
// character class but does not exist fails the way the other git tools do — an IsError result
// carrying git's own message, never a Go error and never an empty success.
func TestGitLog_UnknownRefMatchesOtherGitTools(t *testing.T) {
	root := gitRepo(t)

	res, err := NewGitLog(root).Execute(context.Background(), logCall("c1", `{"ref":"no-such-branch"}`))
	if err != nil {
		t.Fatalf("Execute err = %v, want the failure as a result", err)
	}
	diff, err := NewGitDiffRange(root).Execute(context.Background(), diffCall("c2", `{"base":"no-such-branch","head":"HEAD"}`))
	if err != nil {
		t.Fatalf("diff Execute err = %v, want the failure as a result", err)
	}
	if !res.IsError || !diff.IsError {
		t.Fatalf("an unknown ref must be IsError for both tools; log=%v diff=%v", res.IsError, diff.IsError)
	}
	if strings.TrimSpace(res.Content) == "" {
		t.Error("git_log on an unknown ref must carry git's own message")
	}
}

// TestGitLog_PathShapedRefIsNotAPathspecLog is the behavioural half of the "--" terminator:
// `git log <name>` where <name> is a tracked PATH rather than a ref is a pathspec log — it
// answers "which commits touched this file" with exit 0. Dropping the terminator would turn a
// model's typo'd branch name into a plausible, WRONG history reported as success.
func TestGitLog_PathShapedRefIsNotAPathspecLog(t *testing.T) {
	root := gitRepo(t)

	res, err := NewGitLog(root).Execute(context.Background(), logCall("c1", `{"ref":"README.md"}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError {
		t.Errorf("log of the tracked path %q = %q, want a loud failure, not a pathspec log", "README.md", res.Content)
	}
}

// TestGitLog_EmptyRepo covers a freshly initialised repository: HEAD resolves to nothing, so
// git fails and the tool surfaces git's own words rather than claiming an empty history.
func TestGitLog_EmptyRepo(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git on PATH; skipping the live git-tool run")
	}
	root := t.TempDir()
	cmd := exec.Command(gitPath, "init", "-b", "main")
	cmd.Dir = root
	cmd.Env = safeGitEnv("")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	res, err := NewGitLog(root).Execute(context.Background(), logCall("c1", `{}`))
	if err != nil {
		t.Fatalf("Execute err = %v, want the failure as a result", err)
	}
	if !res.IsError {
		t.Fatalf("log of an empty repo = %q, want an IsError result", res.Content)
	}
	if strings.TrimSpace(res.Content) == "" {
		t.Error("git_log on an empty repo must carry git's own message")
	}
}

// ----------------------------------------------------------------------------
// Repo-supplied hooks and diff drivers
// ----------------------------------------------------------------------------

// posixScriptHost skips a test that installs a POSIX shell script (a git hook or a diff
// driver) on a platform where the fixture's assumptions do not hold. The behaviour being
// pinned is git's, not the shell's, and it is the same behaviour on every platform.
func posixScriptHost(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hook/driver fixtures are POSIX shell scripts; the git flags they pin are platform-independent")
	}
}

// writeMarkerScript installs an executable POSIX script at scriptPath whose observable effects
// are creating markerPath and running tail. It uses shell builtins only, so it does not depend
// on what survives the git tools' scrubbed, workspace-scoped PATH.
func writeMarkerScript(t *testing.T, scriptPath, markerPath, tail string) {
	t.Helper()
	script := "#!/bin/sh\n: > \"" + markerPath + "\"\n" + tail + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script %s: %v", scriptPath, err)
	}
}

// installRepoHook writes a .git/hooks/<name> hook into an existing repository — the shape an
// attacker-authored checkout arrives in (a tarball or mirror carrying its own .git, or a single
// in-workspace write). It returns the marker path the hook creates if it ever runs.
func installRepoHook(t *testing.T, root, name, tail string) (marker string) {
	t.Helper()
	marker = filepath.Join(t.TempDir(), name+".ran")
	hookDir := filepath.Join(root, ".git", "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("hooks dir: %v", err)
	}
	writeMarkerScript(t, filepath.Join(hookDir, name), marker, tail)
	return marker
}

// requireNotRan fails when the marker file exists, i.e. the hook or driver executed.
func requireNotRan(t *testing.T, marker, what string) {
	t.Helper()
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("%s executed; a repo-supplied program must never run for a git tool", what)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat marker: %v", err)
	}
}

// runInRepo runs a raw git command in root with the tools' own scrubbed environment plus a
// deterministic identity, for tests that need to arrange repository state directly.
func runInRepo(t *testing.T, root string, args ...string) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git on PATH; skipping the live git-tool run")
	}
	cmd := exec.Command(gitPath, args...)
	cmd.Dir = root
	cmd.Env = append(safeGitEnv(""),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestGitCommit_DoesNotRunRepoSuppliedHook pins the committing path against an
// attacker-authored pre-commit hook. The hook exits 1, so a run would be visible twice over:
// the marker file appears AND the commit is vetoed. Neither may happen.
func TestGitCommit_DoesNotRunRepoSuppliedHook(t *testing.T) {
	posixScriptHost(t)
	root := gitRepo(t)
	marker := installRepoHook(t, root, "pre-commit", "exit 1")

	if err := writeFileForTest(root, "new.txt", "added\n"); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	res, err := NewGitCommit(root).Execute(context.Background(),
		commitCall("c1", `{"message":"add new.txt","files":["new.txt"]}`))
	if err != nil {
		t.Fatalf("commit err = %v", err)
	}
	if res.IsError {
		t.Fatalf("commit was vetoed by the repo's hook: %q", res.Content)
	}
	requireNotRan(t, marker, "the repository's pre-commit hook")
}

// TestGitBranch_DoesNotRunRepoSuppliedHook pins the emptied core.hooksPath specifically: a
// branch switch has no --no-verify to pass, so post-checkout is stopped by that global option
// alone. It is also the hook class --no-verify would not cover on the commit path either.
func TestGitBranch_DoesNotRunRepoSuppliedHook(t *testing.T) {
	posixScriptHost(t)
	root := gitRepo(t)
	marker := installRepoHook(t, root, "post-checkout", "exit 0")

	res, err := NewGitBranch(root).Execute(context.Background(),
		branchCall("c1", `{"action":"create","name":"feature"}`))
	if err != nil {
		t.Fatalf("branch err = %v", err)
	}
	if res.IsError {
		t.Fatalf("branch create errored: %q", res.Content)
	}
	requireNotRan(t, marker, "the repository's post-checkout hook")
}

// TestGitDiffRange_DoesNotRunRepoSuppliedDiffDriver pins the read path against the two driver
// kinds a repository can select in .gitattributes and configure in its own config: a textconv
// filter and an external diff command. Both are programs git would run during what the operator
// approved as an inspection, and both must be refused — while the diff still reports the real
// stored bytes rather than the driver's rendering.
func TestGitDiffRange_DoesNotRunRepoSuppliedDiffDriver(t *testing.T) {
	posixScriptHost(t)

	for _, tc := range []struct {
		name      string
		configKey string
	}{
		{name: "textconv filter", configKey: "diff.hostile.textconv"},
		{name: "external diff command", configKey: "diff.hostile.command"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := gitRepo(t)
			marker := filepath.Join(t.TempDir(), "driver.ran")
			driver := filepath.Join(t.TempDir(), "driver.sh")
			writeMarkerScript(t, driver, marker, `echo "rendered by the repository"`)

			if err := writeFileForTest(root, ".gitattributes", "*.data diff=hostile\n"); err != nil {
				t.Fatalf("write .gitattributes: %v", err)
			}
			if err := writeFileForTest(root, "a.data", "before\n"); err != nil {
				t.Fatalf("write data file: %v", err)
			}
			runInRepo(t, root, "config", tc.configKey, driver)
			runInRepo(t, root, "add", ".gitattributes", "a.data")
			runInRepo(t, root, "commit", "-m", "seed the driver")
			runInRepo(t, root, "checkout", "-b", "feature")
			if err := writeFileForTest(root, "a.data", "after\n"); err != nil {
				t.Fatalf("rewrite data file: %v", err)
			}
			runInRepo(t, root, "add", "a.data")
			runInRepo(t, root, "commit", "-m", "change the data file")

			res, err := NewGitDiffRange(root).Execute(context.Background(),
				diffCall("c1", `{"base":"main","head":"feature"}`))
			if err != nil {
				t.Fatalf("diff err = %v", err)
			}
			if res.IsError {
				t.Fatalf("diff errored: %q", res.Content)
			}
			requireNotRan(t, marker, "the repository's "+tc.name)
			if !strings.Contains(res.Content, "after") {
				t.Errorf("diff = %q, want the stored bytes rather than a driver's rendering", res.Content)
			}
		})
	}
}

// TestGit_RefusesRepoLocalFilterDriver pins the SP-2 refusal at the runGit choke point: a
// repository delivered with its own .git/config naming a filter command gets no git call at
// all, whichever tool asked — the write paths (add/commit run clean) and the read paths (a
// plain log or status runs it too) alike. Each of the three driver hooks is a command git
// would execute, so each must trigger, and the refusal must NAME the key it found.
func TestGit_RefusesRepoLocalFilterDriver(t *testing.T) {
	tools := []struct {
		name string
		run  func(root string) (domain.ToolResult, error)
	}{
		{"git_commit", func(root string) (domain.ToolResult, error) {
			return NewGitCommit(root).Execute(context.Background(), commitCall("c1", `{"message":"refused"}`))
		}},
		{"git_branch", func(root string) (domain.ToolResult, error) {
			return NewGitBranch(root).Execute(context.Background(), branchCall("c1", `{"action":"list"}`))
		}},
		{"git_log", func(root string) (domain.ToolResult, error) {
			return NewGitLog(root).Execute(context.Background(), logCall("c1", `{}`))
		}},
	}

	for _, key := range []string{"filter.hostile.clean", "filter.hostile.smudge", "filter.hostile.process"} {
		t.Run(key, func(t *testing.T) {
			root := gitRepo(t)
			runInRepo(t, root, "config", "--local", key, "cat")

			for _, tool := range tools {
				t.Run(tool.name, func(t *testing.T) {
					res, err := tool.run(root)
					if err != nil {
						t.Fatalf("%s err = %v", tool.name, err)
					}
					if !res.IsError {
						t.Fatalf("%s succeeded on a repo configuring %s: %q", tool.name, key, res.Content)
					}
					if !strings.Contains(res.Content, key) {
						t.Errorf("%s refusal = %q, want it to name %s", tool.name, res.Content, key)
					}
				})
			}
		})
	}
}

// TestGitCommit_RefusesRepoSuppliedFilterDriver is the same refusal against the live exploit
// rather than the config key: a .gitattributes selecting a clean filter whose command is a
// marker script, which staging would otherwise execute as the operator.
func TestGitCommit_RefusesRepoSuppliedFilterDriver(t *testing.T) {
	posixScriptHost(t)
	root := gitRepo(t)
	marker := filepath.Join(t.TempDir(), "filter.ran")
	driver := filepath.Join(t.TempDir(), "filter.sh")
	writeMarkerScript(t, driver, marker, "cat")

	if err := writeFileForTest(root, ".gitattributes", "*.data filter=hostile\n"); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}
	if err := writeFileForTest(root, "a.data", "before\n"); err != nil {
		t.Fatalf("write data file: %v", err)
	}
	runInRepo(t, root, "config", "--local", "filter.hostile.clean", driver)

	res, err := NewGitCommit(root).Execute(context.Background(),
		commitCall("c1", `{"message":"stage the data file","files":["a.data",".gitattributes"]}`))
	if err != nil {
		t.Fatalf("commit err = %v", err)
	}
	if !res.IsError {
		t.Fatalf("commit ran with a repo-supplied clean filter configured: %q", res.Content)
	}
	if !strings.Contains(res.Content, "filter.hostile.clean") {
		t.Errorf("refusal = %q, want it to name filter.hostile.clean", res.Content)
	}
	requireNotRan(t, marker, "the repository's clean filter")
}

// TestGit_FilterRefusalStaysRepoLocal pins the other half of the rule: the probe costs a clean
// repository nothing, and a driver in the OPERATOR's own global config — the config the threat
// model trusts, on the same boundary HOME sits on in safeEnvKeys — never refuses.
func TestGit_FilterRefusalStaysRepoLocal(t *testing.T) {
	for _, tc := range []struct {
		name       string
		globalOnly bool
	}{
		{name: "no filter driver configured"},
		{name: "driver in the operator's global config", globalOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gitPath, err := exec.LookPath("git")
			if err != nil {
				t.Skip("no git on PATH; skipping the live git-tool run")
			}
			if tc.globalOnly {
				home := t.TempDir()
				t.Setenv("HOME", home)
				global := "[filter \"hostile\"]\n\tclean = cat\n"
				if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(global), 0o644); err != nil {
					t.Fatalf("write global config: %v", err)
				}
			}
			root := gitRepo(t)
			if tc.globalOnly {
				// Guard against a vacuous pass: without git reading the injected HOME there is
				// no global driver to leave alone.
				seen, err := runGitUnchecked(context.Background(), gitPath, root, gitTimeout,
					"config", "--global", "--name-only", "--get-regexp", gitFilterConfigName.String())
				if err != nil || seen.exitCode != 0 {
					t.Skip("this git did not read the injected HOME config; nothing to assert")
				}
			}

			res, err := NewGitStatus(root).Execute(context.Background(), statusCall("c1"))
			if err != nil {
				t.Fatalf("status err = %v", err)
			}
			if res.IsError {
				t.Fatalf("status refused: %q", res.Content)
			}
		})
	}
}

// TestRunGit_AppliesHardeningToEveryInvocation pins the two hardening measures runGit applies
// to ALL git calls, without depending on the host's git: a fake program records the argv it was
// launched with and whether GIT_CONFIG_NOSYSTEM reached its environment.
func TestRunGit_AppliesHardeningToEveryInvocation(t *testing.T) {
	posixScriptHost(t)

	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	fakeGit := filepath.Join(dir, "fake-git")
	script := "#!/bin/sh\n{ echo \"argv: $*\"; echo \"nosystem: ${GIT_CONFIG_NOSYSTEM-unset}\"; } > \"" + record + "\"\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}

	if _, err := runGit(context.Background(), fakeGit, t.TempDir(), gitTimeout, "status"); err != nil {
		t.Fatalf("runGit err = %v", err)
	}
	out, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	got := string(out)
	// The global options must precede the subcommand — git only accepts -c there.
	if !strings.Contains(got, "argv: -c core.hooksPath= status") {
		t.Errorf("argv = %q, want the hooks-path option ahead of the subcommand", got)
	}
	if !strings.Contains(got, "nosystem: 1") {
		t.Errorf("env = %q, want GIT_CONFIG_NOSYSTEM=1", got)
	}
}

// TestRunGit_MemoisesTheFilterDriverProbePerRoot pins the probe's cost model: the repo-local
// filter-driver probe runs once per repository root — not once per git call — and one root's
// cached answer never stands in for another's. A fake git logs every invocation it receives, so
// what is counted is the real subprocess count rather than a proxy for it.
func TestRunGit_MemoisesTheFilterDriverProbePerRoot(t *testing.T) {
	posixScriptHost(t)

	dir := t.TempDir()
	invocations := filepath.Join(dir, "invocations")
	fakeGit := filepath.Join(dir, "fake-git")
	script := "#!/bin/sh\necho \"$*\" >> \"" + invocations + "\"\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	firstRoot, secondRoot := t.TempDir(), t.TempDir()
	runIn := func(root string) {
		t.Helper()
		if _, err := runGit(context.Background(), fakeGit, root, gitTimeout, "status"); err != nil {
			t.Fatalf("runGit err = %v", err)
		}
	}

	runIn(firstRoot)
	runIn(firstRoot)
	runIn(secondRoot)

	logged, err := os.ReadFile(invocations)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	probes, calls := 0, 0
	for _, line := range strings.Split(strings.TrimSpace(string(logged)), "\n") {
		switch {
		case strings.Contains(line, "--get-regexp"):
			probes++
		case strings.Contains(line, "status"):
			calls++
		}
	}

	wantProbes := len(gitFilterConfigScopes) * 2
	if probes != wantProbes {
		t.Errorf("probe invocations = %d, want %d (one per config scope per root; the repeat call on the first root must reuse the memoised answer)", probes, wantProbes)
	}
	if calls != 3 {
		t.Errorf("git calls = %d, want 3 (memoising the probe must not swallow a real invocation)", calls)
	}
}

// ----------------------------------------------------------------------------
// RunGitQuery — the engine's own read-side git
// ----------------------------------------------------------------------------

// TestRunGitQuery_ReturnsStdoutAloneAndAppliesHardening pins the funnel entry the tracked-file
// mutation floor takes: the caller gets the child's STDOUT as data — the diagnostics it printed
// stay out of the payload — and the invocation carries the same hardening every git tool's does.
// A fake git outside the workspace records what it was launched with, so the assertion is about
// the real argv and environment rather than a proxy for them.
func TestRunGitQuery_ReturnsStdoutAloneAndAppliesHardening(t *testing.T) {
	posixScriptHost(t)

	fakeDir := t.TempDir()
	record := filepath.Join(fakeDir, "record")
	fakeGit := filepath.Join(fakeDir, "fake-git")
	script := "#!/bin/sh\n" +
		"{ echo \"argv: $*\"; echo \"nosystem: ${GIT_CONFIG_NOSYSTEM-unset}\"; echo \"apikey: ${APOGEE_API_KEY-unset}\"; } >> \"" + record + "\"\n" +
		"echo diagnostic >&2\n" +
		"echo PAYLOAD\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	withFakeGit(t, true, fakeGit)
	t.Setenv("APOGEE_API_KEY", "shhh-secret")

	out, err := RunGitQuery(context.Background(), t.TempDir(), gitTimeout, "status", "--porcelain")
	if err != nil {
		t.Fatalf("RunGitQuery err = %v", err)
	}

	if strings.TrimSpace(out) != "PAYLOAD" {
		t.Errorf("stdout = %q, want the child's stdout alone (no stderr diagnostic)", out)
	}
	logged, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	got := string(logged)
	if !strings.Contains(got, "argv: -c core.hooksPath= status --porcelain") {
		t.Errorf("argv = %q, want the hardening option ahead of the subcommand", got)
	}
	if !strings.Contains(got, "nosystem: 1") {
		t.Errorf("env = %q, want GIT_CONFIG_NOSYSTEM=1", got)
	}
	if strings.Contains(got, "shhh-secret") || !strings.Contains(got, "apikey: unset") {
		t.Errorf("env = %q, want the allowlist to have dropped APOGEE_API_KEY", got)
	}
}

// TestRunGitQuery_RefusesAPlantedGit pins the exec fence on the engine's own git: a git that
// resolves INSIDE the workspace is bytes the model may have written, and the funnel refuses to
// run it — with the fence's sentinel intact, so a caller can tell a refusal from an absence.
func TestRunGitQuery_RefusesAPlantedGit(t *testing.T) {
	root := t.TempDir()
	planted := plantExecutable(t, root, "node_modules/.bin/git")
	withFakeGit(t, true, planted)

	_, err := RunGitQuery(context.Background(), root, gitTimeout, "status", "--porcelain")

	if !errors.Is(err, security.ErrExecFromWritablePath) {
		t.Fatalf("err = %v, want the exec-fence refusal", err)
	}
	if !strings.Contains(err.Error(), "node_modules") {
		t.Errorf("err = %q, want the refusal to name the resolved path", err)
	}
}

// TestRunGitQuery_NonZeroExitIsAnError pins the deliberate flattening: the engine's git has one
// caller, which treats every failure as "skip this check", so a clean non-zero exit is an error
// here rather than the captured outcome a TOOL would show the model.
func TestRunGitQuery_NonZeroExitIsAnError(t *testing.T) {
	posixScriptHost(t)

	fakeDir := t.TempDir()
	fakeGit := filepath.Join(fakeDir, "fake-git")
	if err := os.WriteFile(fakeGit, []byte("#!/bin/sh\necho 'fatal: not a git repository' >&2\nexit 128\n"), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	withFakeGit(t, true, fakeGit)

	_, err := RunGitQuery(context.Background(), t.TempDir(), gitTimeout, "rev-parse", "--is-inside-work-tree")

	if err == nil {
		t.Fatal("RunGitQuery err = nil, want a non-zero exit reported as an error")
	}
	if !strings.Contains(err.Error(), "exit 128") {
		t.Errorf("err = %q, want the exit status named", err)
	}
}

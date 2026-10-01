package agent

// Tests for the tracked-file mutation floor (treesnapshot.go): the always-on
// structural warning appended to subprocess tool results when a call changes
// workspace files under a git repository. Driven through executeTool — the seam
// the floor lives on — with a fake SubprocessTool whose Execute mutates the
// temp workspace, so the assertions cover the wiring, not just the diff helper.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/gitexec"
	"github.com/airiclenz/apogee/internal/subprocess"
)

// mutatingSubprocessTool is a fake SubprocessTool whose Execute runs an arbitrary
// mutation against the test workspace and returns a canned result — the stand-in
// for terminal/python-exec on the dispatch seam.
type mutatingSubprocessTool struct {
	subprocess bool
	run        func() error
	result     domain.ToolResult
}

func (m mutatingSubprocessTool) Name() string            { return "fake_subprocess" }
func (m mutatingSubprocessTool) Description() string     { return "test subprocess stand-in" }
func (m mutatingSubprocessTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (m mutatingSubprocessTool) Subprocess() bool        { return m.subprocess }

func (m mutatingSubprocessTool) Execute(context.Context, domain.ToolCall) (domain.ToolResult, error) {
	if m.run != nil {
		if err := m.run(); err != nil {
			return domain.ToolResult{}, err
		}
	}
	res := m.result
	res.CallID = "call-1"
	return res, nil
}

// requireGit skips the test when no git binary is on PATH — the floor itself
// degrades to inactive in that case, so there is nothing to assert.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available on PATH")
	}
}

// mustGit runs one git command in dir, failing the test on error.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newGitWorkspace creates a temp git repo holding one committed tracked file and
// returns the root plus the tracked file's path.
func newGitWorkspace(t *testing.T) (root, trackedPath string) {
	t.Helper()
	root = t.TempDir()
	mustGit(t, root, "init", "-q")
	trackedPath = filepath.Join(root, "tracked.txt")
	if err := os.WriteFile(trackedPath, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, root, "add", "tracked.txt")
	mustGit(t, root, "-c", "user.email=test@test", "-c", "user.name=test", "commit", "-q", "-m", "seed")
	return root, trackedPath
}

// newWorkspaceAgent constructs an Agent whose workspace root is dir.
func newWorkspaceAgent(t *testing.T, dir string) *Agent {
	t.Helper()
	cfg := baseConfig(&recordingSink{})
	cfg.WorkspaceDir = dir
	a, err := newAgent(cfg, echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a
}

// executeFake routes one fake-subprocess call through executeTool and returns the result.
func executeFake(t *testing.T, a *Agent, tool mutatingSubprocessTool) domain.ToolResult {
	t.Helper()
	call := domain.ToolCall{ID: "call-1", Tool: tool.Name()}
	result, outcome := a.executeTool(context.Background(), 0, tool, call, nil)
	if outcome != dispatchDone {
		t.Fatalf("outcome = %v, want dispatchDone", outcome)
	}
	return result
}

// ---------------------------------------------------------------------------

func TestTreeSnapshot_TrackedFileOverwriteGetsWarning(t *testing.T) {
	t.Parallel()
	requireGit(t)
	root, tracked := newGitWorkspace(t)
	a := newWorkspaceAgent(t, root)

	result := executeFake(t, a, mutatingSubprocessTool{
		subprocess: true,
		run:        func() error { return os.WriteFile(tracked, []byte("clobbered\n"), 0o644) },
		result:     domain.ToolResult{Content: "ok"},
	})

	want := "[warning: this command changed workspace files: tracked.txt]"
	if !strings.Contains(result.Content, want) {
		t.Fatalf("result content %q missing %q", result.Content, want)
	}
}

func TestTreeSnapshot_NewUntrackedFileGetsWarning(t *testing.T) {
	t.Parallel()
	requireGit(t)
	root, _ := newGitWorkspace(t)
	a := newWorkspaceAgent(t, root)

	result := executeFake(t, a, mutatingSubprocessTool{
		subprocess: true,
		run: func() error {
			return os.WriteFile(filepath.Join(root, "appeared.txt"), []byte("x\n"), 0o644)
		},
		result: domain.ToolResult{Content: "ok"},
	})

	want := "[warning: this command changed workspace files: appeared.txt]"
	if !strings.Contains(result.Content, want) {
		t.Fatalf("result content %q missing %q", result.Content, want)
	}
}

func TestTreeSnapshot_ErrorResultStillGetsWarning(t *testing.T) {
	t.Parallel()
	requireGit(t)
	root, tracked := newGitWorkspace(t)
	a := newWorkspaceAgent(t, root)

	result := executeFake(t, a, mutatingSubprocessTool{
		subprocess: true,
		run:        func() error { return os.WriteFile(tracked, []byte("half-written\n"), 0o644) },
		result:     domain.ToolResult{Content: "command failed\n[exit code 1]", IsError: true},
	})

	if !result.IsError {
		t.Fatal("fake result lost its IsError flag")
	}
	want := "[warning: this command changed workspace files: tracked.txt]"
	if !strings.Contains(result.Content, want) {
		t.Fatalf("error result content %q missing %q", result.Content, want)
	}
}

func TestTreeSnapshot_NoopCallGetsNoWarning(t *testing.T) {
	t.Parallel()
	requireGit(t)
	root, _ := newGitWorkspace(t)
	a := newWorkspaceAgent(t, root)

	result := executeFake(t, a, mutatingSubprocessTool{
		subprocess: true,
		result:     domain.ToolResult{Content: "ok"},
	})

	if strings.Contains(result.Content, "[warning:") {
		t.Fatalf("no-op call gained a warning: %q", result.Content)
	}
}

func TestTreeSnapshot_NonGitWorkspaceRunsWithoutSnapshotOrWarning(t *testing.T) {
	t.Parallel()
	requireGit(t)
	root := t.TempDir() // deliberately NOT a git repo
	a := newWorkspaceAgent(t, root)

	result := executeFake(t, a, mutatingSubprocessTool{
		subprocess: true,
		run: func() error {
			return os.WriteFile(filepath.Join(root, "written.txt"), []byte("x\n"), 0o644)
		},
		result: domain.ToolResult{Content: "ok"},
	})

	if strings.Contains(result.Content, "[warning:") {
		t.Fatalf("non-git workspace gained a warning: %q", result.Content)
	}
	if a.tree.active(context.Background()) {
		t.Fatal("snapshotter reports active in a non-git workspace")
	}
}

func TestTreeSnapshot_NonSubprocessToolMutationGetsNoWarning(t *testing.T) {
	t.Parallel()
	requireGit(t)
	root, tracked := newGitWorkspace(t)
	a := newWorkspaceAgent(t, root)

	// Subprocess() reports false — the marker's degraded-build shape — so the floor
	// must not fire: it watches the OS-subprocess surface only.
	result := executeFake(t, a, mutatingSubprocessTool{
		subprocess: false,
		run:        func() error { return os.WriteFile(tracked, []byte("changed\n"), 0o644) },
		result:     domain.ToolResult{Content: "ok"},
	})

	if strings.Contains(result.Content, "[warning:") {
		t.Fatalf("non-subprocess tool gained a warning: %q", result.Content)
	}
}

func TestTreeSnapshot_CapListsTenPathsAndTail(t *testing.T) {
	t.Parallel()
	requireGit(t)
	root, _ := newGitWorkspace(t)
	a := newWorkspaceAgent(t, root)

	const total = 13
	result := executeFake(t, a, mutatingSubprocessTool{
		subprocess: true,
		run: func() error {
			for i := 0; i < total; i++ {
				name := filepath.Join(root, fmt.Sprintf("new-%02d.txt", i))
				if err := os.WriteFile(name, []byte("x\n"), 0o644); err != nil {
					return err
				}
			}
			return nil
		},
		result: domain.ToolResult{Content: "ok"},
	})

	var listed []string
	for i := 0; i < treeMutationWarningCap; i++ {
		listed = append(listed, fmt.Sprintf("new-%02d.txt", i))
	}
	want := "[warning: this command changed workspace files: " +
		strings.Join(listed, ", ") +
		fmt.Sprintf(" … and %d more]", total-treeMutationWarningCap)
	if !strings.Contains(result.Content, want) {
		t.Fatalf("result content %q missing capped warning %q", result.Content, want)
	}
}

func TestTreeSnapshot_DiffHelpers(t *testing.T) {
	t.Parallel()

	t.Run("rename line yields the destination path", func(t *testing.T) {
		t.Parallel()
		paths := porcelainDiffPaths("", "R  old.txt -> new.txt\n")
		if len(paths) != 1 || paths[0] != "new.txt" {
			t.Fatalf("paths = %v, want [new.txt]", paths)
		}
	})

	t.Run("status deepening reports the path once", func(t *testing.T) {
		t.Parallel()
		paths := porcelainDiffPaths(" M a.txt\n", "MM a.txt\n")
		if len(paths) != 1 || paths[0] != "a.txt" {
			t.Fatalf("paths = %v, want [a.txt]", paths)
		}
	})

	t.Run("identical snapshots yield nothing", func(t *testing.T) {
		t.Parallel()
		if paths := porcelainDiffPaths(" M a.txt\n", " M a.txt\n"); len(paths) != 0 {
			t.Fatalf("paths = %v, want empty", paths)
		}
	})
}

// ---------------------------------------------------------------------------
// The floor's git goes through the tools funnel (F-05)
// ---------------------------------------------------------------------------

// specLog is the record of every git spec a fake Host was handed, in launch order.
type specLog struct {
	mu    sync.Mutex
	specs []subprocess.SubprocessSpec
}

func (l *specLog) add(spec subprocess.SubprocessSpec) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.specs = append(l.specs, spec)
}

// all returns a copy of the specs recorded so far.
func (l *specLog) all() []subprocess.SubprocessSpec {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.specs)
}

// gitCommand returns the git invocation spec carries with the binary and the hardening `-c`
// options stripped — "status --porcelain" — so a fake Spawn can answer by subcommand.
func gitCommand(spec subprocess.SubprocessSpec) string {
	args := spec.Argv[1:]
	for len(args) >= 2 && args[0] == "-c" {
		args = args[2:]
	}
	return strings.Join(args, " ")
}

// scriptedGit returns a Host whose Look answers gitPath and whose Spawn answers every command
// from answer, recording each spec it was handed. The command-config probe is answered as a
// clean repository — its rev-parse names no file, every scope listing passes — so answer sees
// only the commands themselves. Nothing is executed.
func scriptedGit(gitPath string, answer func(command string) subprocess.SubprocessResult) (gitexec.Host, *specLog) {
	log := &specLog{}
	host := gitexec.Host{
		Look: func(string) (string, error) { return gitPath, nil },
		Spawn: func(_ context.Context, spec subprocess.SubprocessSpec) (subprocess.SubprocessResult, error) {
			log.add(spec)
			command := gitCommand(spec)
			switch {
			case strings.HasPrefix(command, "rev-parse --git-path config "):
				return subprocess.SubprocessResult{}, nil
			case strings.HasPrefix(command, "config ") && strings.HasSuffix(command, " --show-origin --list -z"):
				return subprocess.SubprocessResult{ExitCode: 1}, nil
			}
			return answer(command), nil
		},
	}
	return host, log
}

// withEngineGit returns the setup hook that points the Agent's own git — the mutation floor's
// and the commit-secrets pre-check's — at host.
func withEngineGit(host gitexec.Host) func(*Agent) {
	return func(a *Agent) {
		a.gitHost = host
		a.tree = newTreeSnapshotter(a.cfg.WorkspaceDir, host)
	}
}

// TestTreeSnapshot_GitRunsThroughTheFunnel pins F-05's fix at the floor's own seam: the git the
// floor spawns around every subprocess call carries the tools funnel's hardening (the
// core.hooksPath=/core.fsmonitor=false options, GIT_CONFIG_NOSYSTEM) and the allowlisted environment, so apogee's own
// API key never reaches the most frequently spawned program the agent runs.
func TestTreeSnapshot_GitRunsThroughTheFunnel(t *testing.T) {
	// No t.Parallel: APOGEE_API_KEY is process-wide.
	root := t.TempDir()
	t.Setenv("APOGEE_API_KEY", "shhh-secret")
	host, log := scriptedGit(filepath.Join(t.TempDir(), "git"), func(command string) subprocess.SubprocessResult {
		if command == "rev-parse --is-inside-work-tree" {
			return subprocess.SubprocessResult{Stdout: "true\n"}
		}
		return subprocess.SubprocessResult{}
	})
	a := newWorkspaceAgent(t, root)
	withEngineGit(host)(a)

	executeFake(t, a, mutatingSubprocessTool{
		subprocess: true,
		result:     domain.ToolResult{Content: "ok"},
	})

	var argvs []string
	var envs [][]string
	for _, spec := range log.all() {
		argvs = append(argvs, strings.Join(spec.Argv[1:], " "))
		envs = append(envs, spec.Env)
	}
	if len(argvs) == 0 {
		t.Fatal("the floor spawned no git at all")
	}
	if !slices.Contains(argvs, "-c core.hooksPath= -c core.fsmonitor=false rev-parse --is-inside-work-tree") {
		t.Errorf("argvs = %q, want the probe hardened", argvs)
	}
	if !slices.Contains(argvs, "-c core.hooksPath= -c core.fsmonitor=false status --porcelain") {
		t.Errorf("argvs = %q, want the snapshots hardened", argvs)
	}
	for i, env := range envs {
		if !slices.Contains(env, "GIT_CONFIG_NOSYSTEM=1") {
			t.Errorf("run %q env lacks GIT_CONFIG_NOSYSTEM=1", argvs[i])
		}
		for _, kv := range env {
			if strings.HasPrefix(kv, "APOGEE_API_KEY=") || strings.Contains(kv, "shhh-secret") {
				t.Errorf("run %q env carries %q, want the allowlist to have dropped APOGEE_API_KEY", argvs[i], kv)
			}
		}
	}
}

// TestTreeSnapshot_PlantedGitTurnsTheFloorOff pins the fence half: a git resolving INSIDE the
// workspace is bytes the model may have written, so the funnel refuses it — and the floor's
// contract turns that refusal into a silent skip rather than a failed tool call.
func TestTreeSnapshot_PlantedGitTurnsTheFloorOff(t *testing.T) {
	t.Parallel()
	requireGit(t)
	root, tracked := newGitWorkspace(t)
	planted := filepath.Join(root, "node_modules", ".bin", "git")
	if err := os.MkdirAll(filepath.Dir(planted), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(planted, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write planted git: %v", err)
	}
	host, log := scriptedGit(planted, func(string) subprocess.SubprocessResult {
		return subprocess.SubprocessResult{Stdout: "true\n"}
	})
	a := newWorkspaceAgent(t, root)
	withEngineGit(host)(a)

	result := executeFake(t, a, mutatingSubprocessTool{
		subprocess: true,
		run:        func() error { return os.WriteFile(tracked, []byte("changed\n"), 0o644) },
		result:     domain.ToolResult{Content: "ok"},
	})

	if result.IsError || !strings.Contains(result.Content, "ok") {
		t.Errorf("a refused git broke the tool call: %+v", result)
	}
	if strings.Contains(result.Content, "[warning:") {
		t.Errorf("a refused git still produced a warning: %q", result.Content)
	}
	if specs := log.all(); len(specs) != 0 {
		t.Errorf("the planted git was launched %d times; the exec fence must refuse it before the spawn", len(specs))
	}
}

// TestTreeSnapshot_ConfinedCallSnapshotsOutsideTheBox pins where the floor's git runs: apogee's
// own bookkeeping is not the model's command, so a Confine call installs the box for the TOOL
// and never for the snapshots around it. A confined snapshot would pay the re-exec wrapper on
// every call and turn a backend failure into the D4 demote signal.
func TestTreeSnapshot_ConfinedCallSnapshotsOutsideTheBox(t *testing.T) {
	t.Parallel()
	requireGit(t)
	root, _ := newGitWorkspace(t)
	conf := &fakeConfiner{caps: capsBoth()}
	cfg := baseConfig(&recordingSink{})
	cfg.WorkspaceDir = root
	cfg.Confiner = conf
	a, err := newAgent(cfg, echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	tool := &subprocTool{name: "fake_subprocess"}

	call := domain.ToolCall{ID: "call-1", Tool: tool.Name()}
	box := domain.ConfinementBox{WorkspaceRoot: root}
	if _, outcome := a.executeTool(context.Background(), 0, tool, call, &box); outcome != dispatchDone {
		t.Fatalf("outcome = %v, want dispatchDone", outcome)
	}

	if !tool.confinedOK() {
		t.Error("the tool never saw the Confinement handle; the box must reach the model's command")
	}
	if got := conf.confineCount(); got != 1 {
		t.Errorf("Confine called %d times, want 1 (the tool alone) — the floor's git must run outside the box", got)
	}
}

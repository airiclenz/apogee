package main

// The footer's git segment, driven end to end: the binary's own host (gitstatus_host.go) wired
// into a real TUI run over a real clone, read off the footer the way a human reads it.
//
// The claim is the whole chain, not any one link of it: the composition root hands the renderer
// the host, the status read finds the clone level with its upstream, and a commit pushed to that
// upstream from somewhere else reaches the behind count through the background fetch alone — no
// keystroke, no Turn. The cadences are shortened per session (launchTUIInGitPolling) so the run
// does not sit out the 5 s and 5 min defaults.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/testtime"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// gitStatusBranch is the branch every repository in these tests is pinned to (`init -b`), so the
// footer's branch word is the test's choice rather than the machine's init.defaultBranch.
const gitStatusBranch = "main"

// gitStatusEvery is the cadence both git chains run on in these tests: short enough that a fetch
// and the read it asks for land within a test's patience, long enough that a loaded box is not
// spawning git back to back.
var gitStatusEvery = testtime.Fixed(250 * time.Millisecond)

// TestE2EGitStatusFollowsTheUpstream drives the footer's git segment over a clone of a bare
// remote: the clone opens level with its upstream, and a commit a second clone pushes shows as one
// behind once the background fetch has seen it.
func TestE2EGitStatusFollowsTheUpstream(t *testing.T) {
	t.Parallel()

	ws, pusher := gitStatusClones(t)
	stub := stubllm.New(t, loadScript(t, "livestate"))
	// liveStateSize's hundred and forty columns: at a hundred the git segment is the second to give
	// way, and a temp path long enough would take its room.
	drv := tuitest.NewDriver(t, liveStateSize)
	launchTUIInGitPolling(t, drv, stub, ws, gitStatusEvery)
	waitIdle(drv)

	// The whole level run is seated before anything moves, so the behind count read later is the
	// fetch's doing and not a segment that was simply slow to appear.
	level := gitStatusBranch + " 0Δ 0↓ 0↑"
	drv.WaitFor(func() bool { return strings.Contains(drv.Frame().String(), level) },
		tuitest.Awaiting("the footer's level git segment "+level))
	if got := footerRow(t, drv.Frame()); !strings.Contains(got, level) {
		t.Fatalf("the level git segment %q is on the frame but not on the footer row %q", level, got)
	}

	gitCommitFile(t, pusher, "b.txt", "upstream\n", "an upstream commit")
	gitOutput(t, pusher, "push", "-q", "origin", gitStatusBranch)

	behind := gitStatusBranch + " 0Δ 1↓ 0↑"
	testtime.Eventually(t, testtime.Budget(time.Minute), func() bool {
		return strings.Contains(drv.Frame().String(), behind)
	}, "the footer to read "+behind+" after the push")
	if got := footerRow(t, drv.Frame()); !strings.Contains(got, behind) {
		t.Fatalf("the behind count %q is on the frame but not on the footer row %q", behind, got)
	}
}

// gitStatusClones builds a bare remote holding one commit and two clones of it: the workspace the
// run opens in, and the clone a test pushes from. They sit under one short temp directory rather
// than t.TempDir(), whose path carries the test's name: the footer's workdir cell is the path, and
// a long one would crowd out the very segment under test. Both clones track the remote's branch,
// so the workspace has an upstream from its first read.
func gitStatusClones(t *testing.T) (ws, pusher string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH; the footer's git segment has nothing to read")
	}
	made, err := os.MkdirTemp("", "ag")
	if err != nil {
		t.Fatalf("make the git fixtures' directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(made) })
	// Symlink-resolved, for e2eWorkspace's reason: the run announces the path it resolved to.
	root, err := filepath.EvalSymlinks(made)
	if err != nil {
		t.Fatalf("resolve the git fixtures' directory: %v", err)
	}

	remote := filepath.Join(root, "r.git")
	gitOutput(t, root, "init", "-q", "--bare", "-b", gitStatusBranch, remote)

	pusher = filepath.Join(root, "p")
	gitOutput(t, root, "init", "-q", "-b", gitStatusBranch, pusher)
	gitCommitFile(t, pusher, "a.txt", "hello\n", "the first commit")
	gitOutput(t, pusher, "remote", "add", "origin", remote)
	gitOutput(t, pusher, "push", "-q", "-u", "origin", gitStatusBranch)

	ws = filepath.Join(root, "w")
	gitOutput(t, root, "clone", "-q", remote, ws)
	return ws, pusher
}

// gitCommitFile writes name into repo and commits it. The identity is passed on the command line,
// as secretsRepoWorkspace sets it in the repository, so the commit never depends on the machine's
// global git config; signing is switched off for the same reason.
func gitCommitFile(t *testing.T, repo, name, content, message string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
	gitOutput(t, repo, "add", name)
	gitOutput(t, repo, "-c", "user.name=apogee e2e", "-c", "user.email=e2e@apogee.test",
		"-c", "commit.gpgsign=false", "commit", "-q", "-m", message)
}

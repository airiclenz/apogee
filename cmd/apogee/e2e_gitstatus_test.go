package main

// The footer's git segment, driven end to end: the binary's own host (gitstatus_host.go) wired
// into a real TUI run over a real clone, read off the footer the way a human reads it.
//
// The claim is the whole chain, not any one link of it: the composition root hands the renderer
// the host, the status read finds the clone level with its upstream, and a commit pushed to that
// upstream from somewhere else reaches the behind count through the background fetch alone — no
// keystroke, no Turn. The cadences are shortened per session (launchTUIInGitPolling) so the run
// does not sit out the 5 s and 5 min defaults.
//
// The segment is also a control (ADR 0108): the same clones carry the git actions a click on it
// opens — a pull and a push taken through the picker — and a repository the git tools refuse,
// which shows no segment and whose /git refuses.

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
	if got := footerWorkspaceRow(t, drv.Frame()); !strings.Contains(got, level) {
		t.Fatalf("the level git segment %q is on the frame but not on the footer's workspace row %q", level, got)
	}

	gitCommitFile(t, pusher, "b.txt", "upstream\n", "an upstream commit")
	gitOutput(t, pusher, "push", "-q", "origin", gitStatusBranch)

	behind := gitStatusBranch + " 0Δ 1↓ 0↑"
	testtime.Eventually(t, testtime.Budget(time.Minute), func() bool {
		return strings.Contains(drv.Frame().String(), behind)
	}, "the footer to read "+behind+" after the push")
	if got := footerWorkspaceRow(t, drv.Frame()); !strings.Contains(got, behind) {
		t.Fatalf("the behind count %q is on the frame but not on the footer's workspace row %q", behind, got)
	}
}

// TestE2EGitActionsPullAndPushFromTheFooter drives the footer's git actions over the same clones:
// a click on the git segment opens the git actions picker, pull fast-forwards the commit a second
// clone pushed, and push sends a commit made in the workspace — each landing as a transcript note,
// with the segment's counts read again so the footer shows what the action did.
//
// Pull is clicked before the local commit is made: `--ff-only` refuses a branch that has diverged,
// so the order is the claim's, not the test's convenience. Both chains poll every gitStatusEvery,
// so a background fetch is often in flight when an action is taken; the action waits for it rather
// than racing it for the remote-tracking ref.
func TestE2EGitActionsPullAndPushFromTheFooter(t *testing.T) {
	t.Parallel()

	ws, pusher := gitStatusClones(t)
	stub := stubllm.New(t, loadScript(t, "livestate"))
	drv := tuitest.NewDriver(t, liveStateSize)
	launchTUIInGitPolling(t, drv, stub, ws, gitStatusEvery)
	waitIdle(drv)
	awaitGitSegment(t, drv, gitStatusBranch+" 0Δ 0↓ 0↑")

	gitCommitFile(t, pusher, "b.txt", "upstream\n", "an upstream commit")
	gitOutput(t, pusher, "push", "-q", "origin", gitStatusBranch)
	behind := gitStatusBranch + " 0Δ 1↓ 0↑"
	awaitGitSegment(t, drv, behind)

	runGitActionFromTheFooter(t, drv, behind, gitPullGloss)
	drv.WaitText(gitPullNote)
	awaitGitSegment(t, drv, gitStatusBranch+" 0Δ 0↓ 0↑")
	if _, err := os.Stat(filepath.Join(ws, "b.txt")); err != nil {
		t.Fatalf("the pull reported %q but the upstream's file is not in the workspace: %v", gitPullNote, err)
	}

	gitCommitFile(t, ws, "c.txt", "local\n", "a local commit")
	ahead := gitStatusBranch + " 0Δ 0↓ 1↑"
	awaitGitSegment(t, drv, ahead)

	runGitActionFromTheFooter(t, drv, ahead, gitPushGloss)
	drv.WaitText(gitPushNote)
	awaitGitSegment(t, drv, gitStatusBranch+" 0Δ 0↓ 0↑")
	head := strings.TrimSpace(gitOutput(t, ws, "rev-parse", "HEAD"))
	remote := strings.Fields(gitOutput(t, pusher, "ls-remote", "origin", "refs/heads/"+gitStatusBranch))
	if len(remote) == 0 || remote[0] != head {
		t.Fatalf("the push reported %q but the remote's %s is %v, want the workspace's HEAD %s",
			gitPushNote, gitStatusBranch, remote, head)
	}
}

// TestE2EGitActionsRefusedRepository opens the run in a repository the git tools refuse — its own
// config names an ssh command to run — and finds no git segment on the footer and /git answering
// with its refusal rather than a picker.
//
// The absence is watched over a window of many status cadences rather than asserted once: a
// segment that is merely slow to appear would pass a single look. The sibling tests show the same
// clones seating the segment within that cadence when nothing is refused.
func TestE2EGitActionsRefusedRepository(t *testing.T) {
	t.Parallel()

	ws, _ := gitStatusClones(t)
	gitOutput(t, ws, "config", "core.sshCommand", "ssh -v")
	stub := stubllm.New(t, loadScript(t, "livestate"))
	drv := tuitest.NewDriver(t, liveStateSize)
	launchTUIInGitPolling(t, drv, stub, ws, gitStatusEvery)
	waitIdle(drv)

	submit(drv, "/git")
	drv.WaitText(gitRefusedNote)
	if frameHas(drv.Frame(), gitActionsPickerTitle) {
		t.Fatalf("/git in a refused repository opened the git actions picker:\n%s", drv.Frame())
	}

	for watched := testtime.Expiry(gitRefusedWatch); !watched(); {
		if row := footerWorkspaceRow(t, drv.Frame()); strings.Contains(row, glyphGitDirty) {
			t.Fatalf("a refused repository shows a git segment on the footer's workspace row %q", row)
		}
		time.Sleep(testtime.Fixed(50 * time.Millisecond))
	}
}

// The git actions' own words (internal/tui/gitactions.go and cmd/apogee/gitactions_host.go): the
// picker's title over these clones, the glosses that filter it down to one row, and the notes the
// actions and the refusal land as.
const (
	gitActionsPickerTitle = "git actions — " + gitStatusBranch
	gitPullGloss          = "fast-forward"
	gitPushGloss          = "never forced"
	gitPullNote           = "git pull: fast-forwarded 1 commit"
	gitPushNote           = "git push: pushed " + gitStatusBranch + " to origin/" + gitStatusBranch
	gitRefusedNote        = "no git here — git actions need a workspace in a git repository"
	// glyphGitDirty is the changed-paths mark every git segment carries, a detached HEAD's
	// included — the one mark no other footer segment paints.
	glyphGitDirty = "Δ"
)

// gitRefusedWatch is how long a refused repository is watched for a git segment: eight status
// cadences, each of which would have seated one in a repository the tools accept.
var gitRefusedWatch = testtime.Fixed(8 * gitStatusEvery)

// awaitGitSegment waits for the footer to read segment and checks it stands on the footer's
// workspace row rather than anywhere else on the frame.
func awaitGitSegment(t *testing.T, drv *tuitest.Driver, segment string) {
	t.Helper()

	drv.WaitFor(func() bool { return strings.Contains(drv.Frame().String(), segment) },
		tuitest.Awaiting("the footer's git segment "+segment))
	if got := footerWorkspaceRow(t, drv.Frame()); !strings.Contains(got, segment) {
		t.Fatalf("the git segment %q is on the frame but not on the footer's workspace row %q", segment, got)
	}
}

// runGitActionFromTheFooter clicks the footer's git segment, reading segment, and takes the one
// action whose row the filter text leaves in the picker — the gloss words a single row says.
func runGitActionFromTheFooter(t *testing.T, drv *tuitest.Driver, segment, filter string) {
	t.Helper()

	f := drv.Frame()
	x, y, ok := f.Find(segment)
	if !ok || y != footerRowIndex(f, footerWorkspace) {
		t.Fatalf("no git segment %q on the footer's workspace row to click:\n%s", segment, f)
	}
	click(drv, x, y)
	drv.WaitText(gitActionsPickerTitle)
	drv.Type(filter)
	drv.WaitText(gitActionsPickerTitle + filterTitleJoin + filter)
	drv.Press(tuitest.Enter)
	drv.WaitGone(gitActionsPickerTitle)
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

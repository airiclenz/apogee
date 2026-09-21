package main

// The commit-secrets guard, end to end (ADR 0080): internal/agent's pre-check shadow-stages what a
// git_commit would stage over the workspace's REAL repository and hands internal/security's
// `commit-secrets` rule the staged diff, rather than the findings internal/agent's own tests hand
// the guard. A driven run through the composition root is the only place the real git_commit,
// the shadow index, the scanner and the approval pane all meet.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The two prompts testdata/stubllm/guard-secrets.yaml answers, the wrap-ups it closes each
// exchange with, and the finding as the pane paints it and the model is told it.
const (
	secretsCommitPrompt = "Commit the env file with the git tool."
	cleanCommitPrompt   = "Commit the notes file with the git tool."

	secretsDeniedWrapUp = "The secret stayed out of history."
	cleanCommitWrapUp   = "The commit is in."

	// secretsFix is the rule's Hint verbatim as security.SecretsHint renders it for this
	// fixture, on the pane's Fix row: the path finding first, then the content finding, in the
	// order item 6 pinned. A test that paraphrased it would pass over the day somebody rewrote
	// half of it.
	secretsFix = "Fix: staged secret material: .env (secret-bearing file name), " +
		".env (AWS access key id) — unstage it, or approve to commit anyway"

	// secretsEnvLine is what the fixture's `.env` holds: one AWS access key id, the shape the
	// scanner's content pattern matches on. It is a made-up id, not a credential.
	secretsEnvLine = "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n"
)

// TestE2EGuardSecretsForcesALookAtTheCommit drives a git_commit that would stage an untracked
// `.env` holding an AWS access key id, under --mode auto — where an ordinary git_commit runs with
// nobody asked. The pre-check forces the call onto the approval pane with the finding on the Fix
// row and no session row (a forced look is remembered nowhere); a deny answers the model with the
// same finding, and the real index is untouched: `.env` is still untracked afterwards, because the
// shadow staging never wrote to it.
func TestE2EGuardSecretsForcesALookAtTheCommit(t *testing.T) {
	deps, _ := installFenceableConfiner(t)
	ws := secretsRepoWorkspace(t, ".env", secretsEnvLine)
	stub := stubllm.New(t, loadScript(t, "guard-secrets"))
	drv := tuitest.NewDriver(t, tuitest.Size{W: 140, H: 30})
	sess := launchTUIInWith(t, drv, stub, ws, "", deps, "--mode", "auto")

	submit(drv, secretsCommitPrompt)
	pane := awaitForcedPane(drv)
	if flat := paneText(pane); !strings.Contains(flat, forcedReason) {
		t.Errorf("the secrets pane does not read %q:\n%s", forcedReason, pane)
	}
	if flat := paneText(pane); !strings.Contains(flat, flatten(secretsFix)) {
		t.Errorf("the secrets pane does not carry the finding %q:\n%s", secretsFix, pane)
	}
	if _, _, ok := pane.Find(approvalMarker); ok {
		t.Errorf("the secrets pane offers %q, a row the engine would not honour:\n%s", approvalMarker, pane)
	}

	// Deny it, and the finding reaches the MODEL, appended to the denial. The claim is about what
	// the model was TOLD, so it is made against the request the stub received rather than the
	// transcript, where the sentence sits clipped inside a collapsed block.
	decideForced(drv, "d")
	want := "tool call denied by approver — " + strings.TrimPrefix(secretsFix, "Fix: ")
	drv.WaitFor(func() bool { return stubSawMessage(stub, want) },
		tuitest.Awaiting("the denial, with its finding, to reach the model"))
	drv.WaitText(secretsDeniedWrapUp)

	if status := gitOutput(t, ws, "status", "--porcelain"); !strings.Contains(status, "?? .env") {
		t.Errorf("after the denied commit `.env` is no longer untracked; the pre-check touched the real index:\n%s", status)
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EGuardSecretsCleanCommitNeedsNoLook is the control: the same call over an untracked
// `notes.txt` holding a line of prose raises no pane at all under --mode auto, and the commit
// lands. It runs under installFenceableConfiner so the only thing that could have raised a pane
// is the secrets pre-check — on a host that cannot fence, Auto would gate the git child for
// `subprocess execution`, a reason that has nothing to do with secrets.
func TestE2EGuardSecretsCleanCommitNeedsNoLook(t *testing.T) {
	deps, _ := installFenceableConfiner(t)
	ws := secretsRepoWorkspace(t, "notes.txt", "a line of prose\n")
	stub := stubllm.New(t, loadScript(t, "guard-secrets"))
	drv := tuitest.NewDriver(t, tuitest.Size{W: 140, H: 30})
	sess := launchTUIInWith(t, drv, stub, ws, "", deps, "--mode", "auto")

	// The wrap-up arriving is the claim: a pane would have held the call — and the whole run —
	// until somebody answered it, and nobody here does.
	submit(drv, cleanCommitPrompt)
	drv.WaitText(cleanCommitWrapUp)
	if _, _, ok := drv.Frame().Find(forcedMarker); ok {
		t.Errorf("the clean commit raised a forced pane:\n%s", drv.Frame())
	}
	if stubSawMessage(stub, "tool call denied by approver") {
		t.Errorf("the clean commit reached the model as a denial; want it to land")
	}
	if committed := gitOutput(t, ws, "show", "--name-only", "--format="); !strings.Contains(committed, "notes.txt") {
		t.Errorf("HEAD does not carry notes.txt; the clean commit never landed:\n%s", committed)
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// secretsRepoWorkspace is e2eWorkspace `git init`-ed, with a repo-local identity so the commit
// needs no global config, and one untracked file — name holding content — for the model to stage.
// It is a real repository, unlike controlPlaneWorkspace's hand-seeded `.git`, because the
// pre-check's shadow staging diffs it. Skipped when no git is on PATH, as internal/agent does.
func secretsRepoWorkspace(t *testing.T, name, content string) string {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH; the commit and its pre-check cannot spawn")
	}
	ws := e2eWorkspace(t)
	gitOutput(t, ws, "init", "-q")
	gitOutput(t, ws, "config", "user.email", "e2e@apogee.test")
	gitOutput(t, ws, "config", "user.name", "apogee e2e")
	if err := os.WriteFile(filepath.Join(ws, name), []byte(content), 0o600); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
	return ws
}

// gitOutput runs one git command in ws and returns its combined output, failing the test on a
// non-zero exit.
func gitOutput(t *testing.T, ws string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = ws
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

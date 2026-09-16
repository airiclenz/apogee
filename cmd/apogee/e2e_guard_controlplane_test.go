package main

// The shell write view, end to end (apogee-t74): internal/security's `write-git-control-plane` rule
// reads the command line the registry's REAL Terminal declares (domain.ShellCommandTool), rather
// than the stub tool internal/security's own tests hand it. A driven run through the composition
// root is the only place that declaration and the rule meet.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The two prompts testdata/stubllm/guard-controlplane.yaml answers, the wrap-ups it closes each
// exchange with, and the rule's reason as the model is told it.
const (
	controlPlaneReadPrompt  = "List the git hooks dir with the terminal tool."
	controlPlaneWritePrompt = "Overwrite the git config with the terminal tool."

	controlPlaneReadWrapUp  = "That is what the hooks dir had to say."
	controlPlaneWriteWrapUp = "The control plane stayed shut."

	// controlPlaneReason is the rule's Reason verbatim from internal/security's rule set, prefixed
	// the way internal/agent hands a hard refusal to the model. A test that paraphrased it would
	// pass over the day somebody rewrote half of it.
	controlPlaneReason = "refused by the dangerous-action guard: " +
		"write or delete under a repository's git control plane (.git/hooks, .git/config)"

	// seededGitConfig is what the workspace's `.git/config` holds before either command runs — and
	// after, if the write view did its job.
	seededGitConfig = "[core]\n\trepositoryformatversion = 0\n"
)

// TestE2EGuardControlPlane drives a read and a write under the workspace's git control plane
// through the registry's real `terminal` tool. The read (`ls -la .git/hooks`) names the control
// plane in its text and meets only the ordinary subprocess gate: the shell write view sees a read
// leader and no redirect, so the rule stays at TierNone. The write (`echo hooked > .git/config`)
// is hard-refused before it spawns, the model is told the rule's reason, and the file is untouched.
func TestE2EGuardControlPlane(t *testing.T) {
	ws := controlPlaneWorkspace(t)
	stub := stubllm.New(t, loadScript(t, "guard-controlplane"))
	drv := tuitest.NewDriver(t, tuitest.Size{W: 100, H: 30})
	sess := launchTUIIn(t, drv, stub, ws, "")

	// The READ: an ordinary gate, naming the mode's own cause — not the rule's. The pane is waited
	// for unconditionally: the script's wrap-up fires on a refusal too, so a conditional approve
	// would assert nothing about which of the two the call met.
	submit(drv, controlPlaneReadPrompt)
	pane := awaitApprovalPane(drv)
	if flat := flatten(pane.String()); !strings.Contains(flat, controlReason) {
		t.Errorf("the read's pane does not read %q:\n%s", controlReason, pane)
	}
	if stubSawMessage(stub, controlPlaneReason) {
		t.Errorf("the read %q reached the model as a control-plane refusal; want the rule silent", "ls -la .git/hooks")
	}
	decide(drv, "a")
	drv.WaitText(controlPlaneReadWrapUp)

	// The WRITE: no pane — the call is refused before dispatch — and the refusal carries the
	// rule's reason to the model. The claim is about what the model was TOLD, so it is made against
	// the request the stub received rather than the transcript, where the result sits in a
	// collapsed block.
	before := readGitConfig(t, ws)
	submit(drv, controlPlaneWritePrompt)
	drv.WaitText(controlPlaneWriteWrapUp)
	if !stubSawMessage(stub, controlPlaneReason) {
		t.Errorf("the write's tool result on the wire does not carry %q", controlPlaneReason)
	}
	if after := readGitConfig(t, ws); !bytes.Equal(before, after) {
		t.Errorf(".git/config changed across the refused write:\nbefore: %q\nafter:  %q", before, after)
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// controlPlaneWorkspace is e2eWorkspace with a git control plane in it: the seeded `.git/config`
// the write aims at and the `.git/hooks/` dir the read lists. It is seeded by hand rather than
// `git init`-ed so the test needs no git binary, and it deliberately writes no HEAD — nothing here
// asks git anything, and a tree git would refuse to call a repository is still a control plane
// to the rule, which matches the path's text.
func controlPlaneWorkspace(t *testing.T) string {
	t.Helper()

	ws := e2eWorkspace(t)
	if err := os.MkdirAll(filepath.Join(ws, ".git", "hooks"), 0o700); err != nil {
		t.Fatalf("seed the git hooks dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".git", "config"), []byte(seededGitConfig), 0o600); err != nil {
		t.Fatalf("seed the git config: %v", err)
	}
	return ws
}

// readGitConfig returns the workspace's `.git/config` bytes.
func readGitConfig(t *testing.T, ws string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(ws, ".git", "config"))
	if err != nil {
		t.Fatalf("read .git/config: %v", err)
	}
	return data
}

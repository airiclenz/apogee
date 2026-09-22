package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

// The commit-secrets pre-check is judged through the dispatch it tightens: a real repository
// under a real git_commit tool, driven through one Turn, with the approval prompt read through
// gateApprover — the finding is only observable on what the human would have read.

const (
	// pemSecret is the one added line the scanner classes as a private key.
	pemSecret = "-----BEGIN RSA PRIVATE KEY-----\n"
	// privateKeyName is a basename the path globs flag on its own.
	privateKeyName = "id_rsa"
	// secretGoFile is a source file whose staged content carries pemSecret.
	secretGoFile = "keys.go"
)

// newSecretsRepo is newGitWorkspace with a repo-local identity: the git_commit tool's git
// carries only gitexec.SafeEnv, so the identity mustGit sets per `-c` never reaches it and the
// tool's own commit would fail without one in the repository's config.
func newSecretsRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)
	root, _ := newGitWorkspace(t)
	mustGit(t, root, "config", "user.email", "test@test")
	mustGit(t, root, "config", "user.name", "test")
	return root
}

// stageSecrets writes and stages privateKeyName plus secretGoFile carrying pemSecret.
func stageSecrets(t *testing.T, root string) {
	t.Helper()
	writeRepoFile(t, root, privateKeyName, pemSecret)
	writeRepoFile(t, root, secretGoFile, "package keys\n\nconst pem = `"+pemSecret+"`\n")
	mustGit(t, root, "add", privateKeyName, secretGoFile)
}

func writeRepoFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// secretsConfig is an Auto configuration over root holding the real git_commit tool and the
// given approver, so the tightened verdict reaches the prompt and, on allow, the real commit.
func secretsConfig(sink *recordingSink, root string, approver domain.Approver) domain.Config {
	cfg := configWithTools(sink, tools.NewGitCommit(root))
	cfg.WorkspaceDir = root
	cfg.Mode = domain.ModeAuto
	cfg.Confiner = eligibleConfiner{}
	cfg.Approver = approver
	return cfg
}

// gitOut runs one host git command in dir and returns its trimmed output.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// countShadowGit swaps the shadow funnel for one that counts its invocations and restores it
// when the test ends. The tests that use it must not run in parallel with each other.
func countShadowGit(t *testing.T) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	orig := shadowGitQuery
	shadowGitQuery = func(ctx context.Context, gitPath, dir string, env []string, timeout time.Duration, args ...string) (string, error) {
		calls.Add(1)
		return orig(ctx, gitPath, dir, env, timeout, args...)
	}
	t.Cleanup(func() { shadowGitQuery = orig })
	return &calls
}

// realIndexBytes reads the repository's index file, or nil when there is none.
func realIndexBytes(t *testing.T, root string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".git", "index"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestGuardrails_CommitSecretsForcesApprovalEvenInAuto proves a git_commit whose staged tree
// carries secret material forces exactly one approval look in Auto, worded by the scanner: the
// prompt's Reason is the pinned Tier-2 reason and its Remedy the scanner's Hint. An allow lands
// the commit through the tool — the shadow left the real index exactly as it was — and a deny
// answers the model with the finding and leaves no blob of the secret in the real object store.
func TestGuardrails_CommitSecretsForcesApprovalEvenInAuto(t *testing.T) {
	wantRemedy := security.SecretsHint([]security.SecretFinding{
		{Path: privateKeyName, Class: "secret-bearing file name"},
		{Path: privateKeyName, Class: "private key"},
		{Path: secretGoFile, Class: "private key"},
	})

	t.Run("approve lands the commit", func(t *testing.T) {
		root := newSecretsRepo(t)
		stageSecrets(t, root)
		sink := &recordingSink{}
		approver := &gateApprover{decision: domain.ApprovalAllow}

		driveToolCall(t, secretsConfig(sink, root, approver), sink, "c1", "git_commit", `{"message":"add keys"}`)

		if len(approver.requests) != 1 {
			t.Fatalf("approver consulted %d times in Auto; staged secrets must force exactly one look", len(approver.requests))
		}
		req := approver.requests[0]
		if req.Reason != forceApprovalReason {
			t.Errorf("Reason = %q, want %q", req.Reason, forceApprovalReason)
		}
		if req.Remedy != wantRemedy {
			t.Errorf("Remedy = %q, want %q", req.Remedy, wantRemedy)
		}
		if got := gitOut(t, root, "log", "-1", "--format=%s"); got != "add keys" {
			t.Errorf("HEAD subject = %q, want the approved commit to have landed", got)
		}
		if res, ok := lastToolResult(sink.events); !ok || res.IsError {
			t.Errorf("tool result = %+v (ok=%v), want a successful commit", res, ok)
		}
		// The commit consumed exactly what the fixture staged: the shadow's staging touched
		// only its copy, so nothing is left in the real index afterwards.
		if got := gitOut(t, root, "diff", "--cached", "--name-only"); got != "" {
			t.Errorf("staged after commit = %q, want nothing", got)
		}
	})

	t.Run("deny answers the model with the finding and writes no blob", func(t *testing.T) {
		root := newSecretsRepo(t)
		// Written but NOT staged: the call names the files, so the only `git add` that could
		// write the secret's blob before the prompt is the shadow's.
		writeRepoFile(t, root, privateKeyName, pemSecret)
		writeRepoFile(t, root, secretGoFile, "package keys\n\nconst pem = `"+pemSecret+"`\n")
		// hash-object without -w computes the id and writes nothing.
		secretBlob := gitOut(t, root, "hash-object", privateKeyName)
		secretBlobPath := filepath.Join(root, ".git", "objects", secretBlob[:2], secretBlob[2:])
		if _, err := os.Stat(secretBlobPath); !os.IsNotExist(err) {
			t.Fatalf("fixture: secret blob %s already in the object store (stat err = %v)", secretBlob, err)
		}
		sink := &recordingSink{}
		approver := &gateApprover{decision: domain.ApprovalDeny}

		driveToolCall(t, secretsConfig(sink, root, approver), sink, "c1", "git_commit",
			`{"message":"add keys","files":["`+privateKeyName+`","`+secretGoFile+`"]}`)

		if len(approver.requests) != 1 {
			t.Fatalf("approver consulted %d times, want 1", len(approver.requests))
		}
		res, ok := lastToolResult(sink.events)
		if !ok || !res.IsError {
			t.Fatalf("tool result = %+v (ok=%v), want the denial", res, ok)
		}
		if want := "tool call denied by approver — " + wantRemedy; res.Content != want {
			t.Errorf("denial = %q, want %q", res.Content, want)
		}
		if _, err := os.Stat(secretBlobPath); !os.IsNotExist(err) {
			t.Errorf("secret blob %s reached the real object store (stat err = %v)", secretBlob, err)
		}
		if got := gitOut(t, root, "diff", "--cached", "--name-only"); got != "" {
			t.Errorf("real index staged %q after a denied call, want nothing", got)
		}
	})
}

// TestCommitSecretsCleanTreeStaysSilent proves the pre-check tightens nothing when the staged
// diff is clean: a plain file commits without a look in Auto, and a staged DELETION of a
// `.key` file — the one shape `--name-only` would otherwise list — never forces one.
func TestCommitSecretsCleanTreeStaysSilent(t *testing.T) {
	t.Run("plain file", func(t *testing.T) {
		root := newSecretsRepo(t)
		writeRepoFile(t, root, "notes.txt", "nothing secret\n")
		mustGit(t, root, "add", "notes.txt")
		sink := &recordingSink{}
		approver := &gateApprover{decision: domain.ApprovalDeny}

		driveToolCall(t, secretsConfig(sink, root, approver), sink, "c1", "git_commit", `{"message":"notes"}`)

		if len(approver.requests) != 0 {
			t.Fatalf("approver consulted %d times for a clean tree, want 0", len(approver.requests))
		}
		if got := gitOut(t, root, "log", "-1", "--format=%s"); got != "notes" {
			t.Errorf("HEAD subject = %q, want the commit to have run free", got)
		}
	})

	t.Run("staged deletion of a key file", func(t *testing.T) {
		root := newSecretsRepo(t)
		writeRepoFile(t, root, "server.key", pemSecret)
		mustGit(t, root, "add", "server.key")
		mustGit(t, root, "commit", "-q", "-m", "seed key")
		mustGit(t, root, "rm", "-q", "--cached", "server.key")
		sink := &recordingSink{}
		approver := &gateApprover{decision: domain.ApprovalDeny}

		driveToolCall(t, secretsConfig(sink, root, approver), sink, "c1", "git_commit", `{"message":"drop key"}`)

		if len(approver.requests) != 0 {
			t.Fatalf("approver consulted %d times for a staged deletion, want 0", len(approver.requests))
		}
		if got := gitOut(t, root, "log", "-1", "--format=%s"); got != "drop key" {
			t.Errorf("HEAD subject = %q, want the deletion committed without a look", got)
		}
	})
}

// TestCommitSecretsGitFailureSkips proves the two edges of the silent-skip contract: a root
// that is no repository skips the check (the tool reports the failure, nothing is forced), and
// a fresh repository with NO index file is not a failure — the shadow points GIT_INDEX_FILE at
// a path that does not exist yet, so the diff runs and the finding forces the look.
func TestCommitSecretsGitFailureSkips(t *testing.T) {
	t.Run("non-repo root proceeds", func(t *testing.T) {
		requireGit(t)
		root := t.TempDir()
		writeRepoFile(t, root, privateKeyName, pemSecret)
		sink := &recordingSink{}
		approver := &gateApprover{decision: domain.ApprovalDeny}

		driveToolCall(t, secretsConfig(sink, root, approver), sink, "c1", "git_commit",
			`{"message":"m","files":["`+privateKeyName+`"]}`)

		if len(approver.requests) != 0 {
			t.Errorf("approver consulted %d times outside a repository, want 0 (silent skip)", len(approver.requests))
		}
		if res, ok := lastToolResult(sink.events); !ok || !res.IsError {
			t.Errorf("tool result = %+v (ok=%v), want the tool's own git failure", res, ok)
		}
	})

	t.Run("fresh repo without an index file still diffs", func(t *testing.T) {
		requireGit(t)
		root := t.TempDir()
		mustGit(t, root, "init", "-q")
		mustGit(t, root, "config", "user.email", "test@test")
		mustGit(t, root, "config", "user.name", "test")
		writeRepoFile(t, root, privateKeyName, pemSecret)
		if realIndexBytes(t, root) != nil {
			t.Fatal("fixture: a fresh repository must have no index file")
		}
		sink := &recordingSink{}
		approver := &gateApprover{decision: domain.ApprovalDeny}

		driveToolCall(t, secretsConfig(sink, root, approver), sink, "c1", "git_commit",
			`{"message":"m","files":["`+privateKeyName+`"]}`)

		if len(approver.requests) != 1 {
			t.Fatalf("approver consulted %d times, want 1: the shadow diff must run against a missing index", len(approver.requests))
		}
		if !strings.Contains(approver.requests[0].Remedy, privateKeyName) {
			t.Errorf("Remedy = %q, want it to name %s", approver.requests[0].Remedy, privateKeyName)
		}
		if realIndexBytes(t, root) != nil {
			t.Error("the shadow staging created the real index file")
		}
	})
}

// writeSleepingGit installs an executable POSIX git at dir/git that answers nothing and sleeps
// well past any budget a test sets, so every shadow run the pre-check makes is one the check's
// own context has to cut short. It returns dir.
func writeSleepingGit(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the sleeping-git fixture is a POSIX shell script; the budget it pins is platform-independent")
	}
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatalf("write sleeping git: %v", err)
	}
	return dir
}

// lowerCommitSecretsTimeout shrinks the pre-check's budget for one test and restores it after,
// so a case can spend the whole ceiling in milliseconds. Tests using it must not run in parallel.
func lowerCommitSecretsTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	orig := commitSecretsTimeout
	commitSecretsTimeout = d
	t.Cleanup(func() { commitSecretsTimeout = orig })
}

// TestCommitSecretsIncompleteScanForcesApproval proves the degraded check fails loud: a scan
// that resolved a repository and then could not finish — the budget spent on a git that never
// answers, or a git run that failed mid-scan — forces exactly one approval look in Auto, worded
// as an unchecked commit rather than as a finding, and a denied look answers the model with the
// same words. This is the outcome ADR 0080 decision 6's silent skip used to hide.
func TestCommitSecretsIncompleteScanForcesApproval(t *testing.T) {
	// No t.Parallel in either case: PATH, the shadow funnel and the budget are process-wide.
	t.Run("the budget cuts the scan short", func(t *testing.T) {
		root := newSecretsRepo(t)
		stageSecrets(t, root)
		slow := writeSleepingGit(t, t.TempDir())
		t.Setenv("PATH", slow+string(os.PathListSeparator)+os.Getenv("PATH"))
		lowerCommitSecretsTimeout(t, 250*time.Millisecond)
		sink := &recordingSink{}
		approver := &gateApprover{decision: domain.ApprovalDeny}

		driveToolCall(t, secretsConfig(sink, root, approver), sink, "c1", "git_commit", `{"message":"add keys"}`)

		requireIncompleteScanLook(t, approver, sink)
	})

	t.Run("a git failure after the repository resolved", func(t *testing.T) {
		root := newSecretsRepo(t)
		stageSecrets(t, root)
		orig := shadowGitQuery
		// rev-parse resolves the repository; the diff that would read the staged bytes dies.
		shadowGitQuery = func(ctx context.Context, gitPath, dir string, env []string, timeout time.Duration, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "diff" {
				return "", errors.New("git diff: exit 128: fatal: unable to read the index")
			}
			return orig(ctx, gitPath, dir, env, timeout, args...)
		}
		t.Cleanup(func() { shadowGitQuery = orig })
		sink := &recordingSink{}
		approver := &gateApprover{decision: domain.ApprovalDeny}

		driveToolCall(t, secretsConfig(sink, root, approver), sink, "c1", "git_commit", `{"message":"add keys"}`)

		requireIncompleteScanLook(t, approver, sink)
	})
}

// requireIncompleteScanLook asserts the one forced look an incomplete scan warrants: the Tier-2
// reason on the prompt, the incomplete-scan wording as its Fix row, and the denial answering the
// model with that same wording.
func requireIncompleteScanLook(t *testing.T, approver *gateApprover, sink *recordingSink) {
	t.Helper()
	if len(approver.requests) != 1 {
		t.Fatalf("approver consulted %d times, want the one look a scan that could not finish forces", len(approver.requests))
	}
	req := approver.requests[0]
	if req.Reason != forceApprovalReason {
		t.Errorf("Reason = %q, want %q", req.Reason, forceApprovalReason)
	}
	if req.Remedy != incompleteScanHint {
		t.Errorf("Remedy = %q, want the incomplete-scan wording %q", req.Remedy, incompleteScanHint)
	}
	res, ok := lastToolResult(sink.events)
	if !ok || !res.IsError {
		t.Fatalf("tool result = %+v (ok=%v), want the denial", res, ok)
	}
	if want := "tool call denied by approver — " + incompleteScanHint; res.Content != want {
		t.Errorf("denial = %q, want %q", res.Content, want)
	}
}

// TestCommitSecretsSkipsInPlanMode proves Plan mode spawns no git child for the pre-check:
// resolve() refuses the write leaf and applyOverlays ignores the overlay, so the shadow would
// answer a question nobody asks.
func TestCommitSecretsSkipsInPlanMode(t *testing.T) {
	root := newSecretsRepo(t)
	stageSecrets(t, root)
	calls := countShadowGit(t)
	sink := &recordingSink{}
	approver := &gateApprover{decision: domain.ApprovalAllow}
	cfg := secretsConfig(sink, root, approver)
	cfg.Mode = domain.ModePlan

	driveToolCall(t, cfg, sink, "c1", "git_commit", `{"message":"add keys"}`)

	if n := calls.Load(); n != 0 {
		t.Errorf("shadow git spawned %d times in Plan mode, want 0", n)
	}
	if len(approver.requests) != 0 {
		t.Errorf("approver consulted %d times in Plan mode, want 0", len(approver.requests))
	}
	if res, ok := lastToolResult(sink.events); !ok || !res.IsError {
		t.Errorf("tool result = %+v (ok=%v), want Plan mode's refusal", res, ok)
	}
}

// TestCommitSecretsHonoursStricterTextVerdict proves the guard is tighten-only: when the text
// guard already forced the look from the call's own arguments — a `files` entry under
// apogee's control plane fires the write-apogee-control-plane rule (the message is a payload
// key no rule reads) — that verdict stands as worded, its own Hint on the prompt rather than
// the scanner's, and the shadow never runs, even with secret material staged.
func TestCommitSecretsHonoursStricterTextVerdict(t *testing.T) {
	root := newSecretsRepo(t)
	stageSecrets(t, root)
	calls := countShadowGit(t)
	sink := &recordingSink{}
	approver := &gateApprover{decision: domain.ApprovalDeny}

	driveToolCall(t, secretsConfig(sink, root, approver), sink, "c1", "git_commit",
		`{"message":"m","files":["~/.apogee/config.yaml"]}`)

	if n := calls.Load(); n != 0 {
		t.Errorf("shadow git spawned %d times under a stricter text verdict, want 0", n)
	}
	if len(approver.requests) != 1 {
		t.Fatalf("approver consulted %d times, want the text guard's one forced look", len(approver.requests))
	}
	req := approver.requests[0]
	if req.Reason != forceApprovalReason {
		t.Errorf("Reason = %q, want %q", req.Reason, forceApprovalReason)
	}
	if strings.Contains(req.Remedy, "staged secret material") || !strings.Contains(req.Remedy, "~/.apogee") {
		t.Errorf("Remedy = %q, want the control-plane rule's own Hint, not the scanner's", req.Remedy)
	}
}

package gitexec_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/gitexec"
	"github.com/airiclenz/apogee/internal/realpath"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/subprocess"
	"github.com/airiclenz/apogee/internal/testtime"
)

// testTimeout bounds every git invocation these tests make; the runner's own ceilings are not
// what is under test here.
var testTimeout = testtime.Budget(15 * time.Second)

// posixScriptHost skips a test whose fixture is a POSIX shell script. The flags and environment
// those fixtures pin are platform-independent; only the way the fake git is written is not.
func posixScriptHost(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake-git fixtures are POSIX shell scripts; the behaviour they pin is platform-independent")
	}
}

// writeFakeGit installs an executable POSIX script at dir/fake-git and returns its path.
func writeFakeGit(t *testing.T, dir, script string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-git")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	return path
}

// fakeGitHost returns the real OS Host with its lookup answering path, so the resolution paths
// are exercisable without depending on the host's git. It fakes the LOOK alone — the fence
// security.ResolveProgram applies to what the look answers is the real one, which is what makes
// the planted-git refusal a genuine assertion — and the Host is a value the test owns, so no
// other test's resolution sees it.
func fakeGitHost(path string) gitexec.Host {
	host := gitexec.OS()
	host.Look = func(string) (string, error) { return path, nil }
	return host
}

// realGit returns the host's git, skipping the test when there is none — the live behaviour is
// only assertable where git exists (§3a: git is a convenience dependency).
func realGit(t *testing.T) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git on PATH; skipping the live git run")
	}
	return gitPath
}

// runRealGit drives the host's git directly to build a fixture repository. It never goes through
// the package under test, so a fixture cannot be shaped by the very behaviour being asserted.
func runRealGit(t *testing.T, gitPath, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(gitPath, args...)
	cmd.Dir = dir
	cmd.Env = append(gitexec.SafeEnv(""),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// ----------------------------------------------------------------------------
// The hardening every invocation carries
// ----------------------------------------------------------------------------

// TestCapture_AppliesHardeningToEveryInvocation pins the hardening measures the runner applies
// to ALL git calls, without depending on the host's git: a fake program records the argv it was
// launched with and whether GIT_CONFIG_NOSYSTEM reached its environment.
func TestCapture_AppliesHardeningToEveryInvocation(t *testing.T) {
	posixScriptHost(t)

	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	fakeGit := writeFakeGit(t, dir,
		"#!/bin/sh\n{ echo \"argv: $*\"; echo \"nosystem: ${GIT_CONFIG_NOSYSTEM-unset}\"; } > \""+record+"\"\n")

	if _, err := gitexec.OS().Capture(context.Background(), fakeGit, t.TempDir(), testTimeout, "status"); err != nil {
		t.Fatalf("Capture err = %v", err)
	}
	out, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	got := string(out)
	// The global options must precede the subcommand — git only accepts -c there.
	if !strings.Contains(got, "argv: -c core.hooksPath= -c core.fsmonitor=false status") {
		t.Errorf("argv = %q, want the hooks-path option ahead of the subcommand", got)
	}
	if !strings.Contains(got, "nosystem: 1") {
		t.Errorf("env = %q, want GIT_CONFIG_NOSYSTEM=1", got)
	}
}

// TestCapture_ServesTheCacheWhileTheConfigHolds pins the probe's cost model: the repo-local
// command-config probe runs once per repository while the files that decided it hold — not once
// per git call — and one root's cached answer never stands in for another's. A fake git logs
// every invocation it receives, so what is counted is the real subprocess count rather than a
// proxy for it; it prints nothing, so the probe names no file and the memo holds on no prints.
func TestCapture_ServesTheCacheWhileTheConfigHolds(t *testing.T) {
	posixScriptHost(t)

	dir := t.TempDir()
	invocations := filepath.Join(dir, "invocations")
	fakeGit := writeFakeGit(t, dir, "#!/bin/sh\necho \"$*\" >> \""+invocations+"\"\n")

	firstRoot, secondRoot := t.TempDir(), t.TempDir()
	runIn := func(root string) {
		t.Helper()
		if _, err := gitexec.OS().Capture(context.Background(), fakeGit, root, testTimeout, "status"); err != nil {
			t.Fatalf("Capture err = %v", err)
		}
	}

	runIn(firstRoot)
	runIn(firstRoot)
	runIn(secondRoot)

	logged, err := os.ReadFile(invocations)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	listings, paths, calls := 0, 0, 0
	for _, line := range strings.Split(strings.TrimSpace(string(logged)), "\n") {
		switch {
		case strings.Contains(line, "--show-origin"):
			listings++
		case strings.Contains(line, "rev-parse"):
			paths++
		case strings.Contains(line, "status"):
			calls++
		}
	}

	wantListings := len(gitexec.FilterConfigScopes) * 2
	if listings != wantListings {
		t.Errorf("config listings = %d, want %d (one per config scope per root; the repeat call on the first root must reuse the memoised answer)", listings, wantListings)
	}
	if paths != 2 {
		t.Errorf("rev-parse invocations = %d, want 2 (one per root)", paths)
	}
	if calls != 3 {
		t.Errorf("git calls = %d, want 3 (memoising the probe must not swallow a real invocation)", calls)
	}
}

// appendToConfig appends text to the repository's .git/config without a git call — the write a
// confined terminal's opaque program would make, which no argv of ours sees.
func appendToConfig(t *testing.T, root, text string) {
	t.Helper()
	path := filepath.Join(root, ".git", "config")
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read .git/config: %v", err)
	}
	if err := os.WriteFile(path, append(current, text...), 0o644); err != nil {
		t.Fatalf("write .git/config: %v", err)
	}
}

// captureStatus runs one status through Capture on root and returns the captured outcome.
func captureStatus(t *testing.T, gitPath, root string) subprocess.SubprocessResult {
	t.Helper()
	res, err := gitexec.OS().Capture(context.Background(), gitPath, root, testTimeout, "status", "--porcelain")
	if err != nil {
		t.Fatalf("Capture err = %v", err)
	}
	return res
}

// TestCapture_ReprobesWhenTheConfigChanges pins that the memo is keyed on the config's identity,
// not the process: a command-valued key written into .git/config AFTER the first probe — with no
// git call, as an opaque program under a confined terminal would write it — refuses the next call,
// and removing it lets the call through again.
func TestCapture_ReprobesWhenTheConfigChanges(t *testing.T) {
	gitPath := realGit(t)
	root := t.TempDir()
	runRealGit(t, gitPath, root, "init", "-b", "main")
	if res := captureStatus(t, gitPath, root); res.ExitCode != 0 {
		t.Fatalf("Capture refused a clean repository: %q", res.CombinedOutput)
	}

	const hostile = "[filter \"x\"]\n\tclean = true\n"
	appendToConfig(t, root, hostile)
	refused := captureStatus(t, gitPath, root)

	if refused.ExitCode == 0 || !strings.Contains(refused.CombinedOutput, "filter.x.clean") {
		t.Fatalf("Capture after the config write = %q, want the refusal naming filter.x.clean", refused.CombinedOutput)
	}
	path := filepath.Join(root, ".git", "config")
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read .git/config: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSuffix(string(current), hostile)), 0o644); err != nil {
		t.Fatalf("restore .git/config: %v", err)
	}
	if res := captureStatus(t, gitPath, root); res.ExitCode != 0 {
		t.Errorf("Capture after the key was removed = %q, want the call to run again", res.CombinedOutput)
	}
}

// TestCapture_ReprobesAfterAMidSessionInit pins that a probe which reached no repository is never
// memoised: it has no file to fingerprint, so a stored empty answer would hold for the process
// lifetime, and a `git init` plus a command-valued key made after it would let the next call run
// the driver.
func TestCapture_ReprobesAfterAMidSessionInit(t *testing.T) {
	gitPath := realGit(t)
	root := t.TempDir()
	if err := exec.Command(gitPath, "-C", root, "rev-parse", "--git-dir").Run(); err == nil {
		t.Skip("the temp directory sits inside a repository; the no-repository case cannot be staged")
	}
	if res := captureStatus(t, gitPath, root); res.ExitCode == 0 {
		t.Fatalf("status outside any repository exited 0: %q", res.CombinedOutput)
	}

	runRealGit(t, gitPath, root, "init", "-b", "main")
	appendToConfig(t, root, "[filter \"x\"]\n\tclean = true\n")
	res := captureStatus(t, gitPath, root)

	if want := gitexec.CommandConfigRefusal([]string{"filter.x.clean"}); res.CombinedOutput != want {
		t.Errorf("Capture after git init = %q, want the refusal %q", res.CombinedOutput, want)
	}
}

// TestCommandConfigRefusal_NamesTheSshSigningKeyCommand pins that the probe, run through a real
// git's --get-regexp, refuses a repository whose own config names gpg.ssh.defaultKeyCommand: git
// runs that program to find a signing key whenever an ssh-format signed commit has no
// user.signingKey, so a planted commit.gpgsign=true would run it on the next rebase.
func TestCommandConfigRefusal_NamesTheSshSigningKeyCommand(t *testing.T) {
	gitPath := realGit(t)
	root := t.TempDir()
	runRealGit(t, gitPath, root, "init", "-b", "main")
	appendToConfig(t, root, "[gpg \"ssh\"]\n\tdefaultKeyCommand = ./x\n")

	res := captureStatus(t, gitPath, root)

	if want := gitexec.CommandConfigRefusal([]string{"gpg.ssh.defaultkeycommand"}); res.CombinedOutput != want {
		t.Errorf("Capture = %q, want the refusal %q", res.CombinedOutput, want)
	}
}

// TestCommandConfigRefusal_NamesTheRemoteHelper pins the remote-helper route: git runs
// git-remote-<vcs> for a remote whose vcs is set, and resolves a slashed name against the working
// directory, so a repository naming one is refused by that key.
func TestCommandConfigRefusal_NamesTheRemoteHelper(t *testing.T) {
	gitPath := realGit(t)
	root := t.TempDir()
	runRealGit(t, gitPath, root, "init", "-b", "main")
	appendToConfig(t, root, "[remote \"origin\"]\n\tvcs = a/b\n")

	res := captureStatus(t, gitPath, root)

	if want := gitexec.CommandConfigRefusal([]string{"remote.origin.vcs"}); res.CombinedOutput != want {
		t.Errorf("Capture = %q, want the refusal %q", res.CombinedOutput, want)
	}
}

// TestCapture_ReprobesWhenAnAbsentIncludeAppears pins two things at once: the probe follows
// includes at all (a scoped `git config` read follows none unless asked), and a missing include
// — which git skips silently — is watched from the start, so the file the model creates later
// refuses the next call.
func TestCapture_ReprobesWhenAnAbsentIncludeAppears(t *testing.T) {
	gitPath := realGit(t)
	root := t.TempDir()
	runRealGit(t, gitPath, root, "init", "-b", "main")
	appendToConfig(t, root, "[include]\n\tpath = ../extra\n")
	if res := captureStatus(t, gitPath, root); res.ExitCode != 0 {
		t.Fatalf("Capture refused a repository whose include is absent: %q", res.CombinedOutput)
	}

	if err := os.WriteFile(filepath.Join(root, "extra"), []byte("[filter \"x\"]\n\tclean = true\n"), 0o644); err != nil {
		t.Fatalf("write the include: %v", err)
	}
	res := captureStatus(t, gitPath, root)

	if res.ExitCode == 0 || !strings.Contains(res.CombinedOutput, "filter.x.clean") {
		t.Errorf("Capture after the include appeared = %q, want the refusal naming filter.x.clean", res.CombinedOutput)
	}
}

// TestCapture_ReprobesWhenTheBranchChanges pins HEAD's place in the fingerprinted set: an
// includeIf.onbranch include is listed only while HEAD matches, so a branch switch that touches
// no config file at all still changes the answer.
func TestCapture_ReprobesWhenTheBranchChanges(t *testing.T) {
	gitPath := realGit(t)
	root := t.TempDir()
	runRealGit(t, gitPath, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "extra"), []byte("[filter \"x\"]\n\tclean = true\n"), 0o644); err != nil {
		t.Fatalf("write the include: %v", err)
	}
	appendToConfig(t, root, "[includeIf \"onbranch:feat\"]\n\tpath = ../extra\n")
	if res := captureStatus(t, gitPath, root); res.ExitCode != 0 {
		t.Fatalf("Capture refused on a branch the include does not apply to: %q", res.CombinedOutput)
	}

	runRealGit(t, gitPath, root, "checkout", "-b", "feat")
	res := captureStatus(t, gitPath, root)

	if res.ExitCode == 0 || !strings.Contains(res.CombinedOutput, "filter.x.clean") {
		t.Errorf("Capture on the matching branch = %q, want the refusal naming filter.x.clean", res.CombinedOutput)
	}
}

// ----------------------------------------------------------------------------
// Run, RunTo — the engine's own read-side git
// ----------------------------------------------------------------------------

// TestRun_RefusesAPlantedGit pins the exec fence on the engine's own git: a git that resolves
// INSIDE the workspace is bytes the model may have written, and the funnel refuses to run it —
// with the fence's sentinel intact, so a caller can tell a refusal from an absence.
func TestRun_RefusesAPlantedGit(t *testing.T) {
	root := t.TempDir()
	planted := filepath.Join(root, "node_modules", ".bin", "git")
	if err := os.MkdirAll(filepath.Dir(planted), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(planted, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write planted git: %v", err)
	}
	host := fakeGitHost(planted)

	_, err := host.Run(context.Background(), root, nil, testTimeout, "status", "--porcelain")

	if !errors.Is(err, security.ErrExecFromWritablePath) {
		t.Fatalf("err = %v, want the exec-fence refusal", err)
	}
	if !strings.Contains(err.Error(), "node_modules") {
		t.Errorf("err = %q, want the refusal to name the resolved path", err)
	}
}

// TestRun_ReturnsStdoutAloneAndAppendsTheCallersEnv pins the two halves of the query entry: the
// caller gets the child's STDOUT as data — the diagnostics it printed stay out of the payload —
// and the environment it named is appended to the hardened allowlist rather than replacing it,
// so a GIT_DIR redirect to apogee's own object store cannot drop GIT_CONFIG_NOSYSTEM or hand the
// child a credential variable the allowlist scrubbed.
func TestRun_ReturnsStdoutAloneAndAppendsTheCallersEnv(t *testing.T) {
	posixScriptHost(t)

	fakeDir := t.TempDir()
	record := filepath.Join(fakeDir, "record")
	fakeGit := writeFakeGit(t, fakeDir, "#!/bin/sh\n"+
		"{ echo \"argv: $*\"; echo \"nosystem: ${GIT_CONFIG_NOSYSTEM-unset}\"; "+
		"echo \"gitdir: ${GIT_DIR-unset}\"; echo \"apikey: ${APOGEE_API_KEY-unset}\"; } >> \""+record+"\"\n"+
		"echo diagnostic >&2\n"+
		"echo PAYLOAD\n")
	host := fakeGitHost(fakeGit)
	t.Setenv("APOGEE_API_KEY", "shhh-secret")

	store := t.TempDir()
	out, err := host.Run(context.Background(), t.TempDir(), []string{"GIT_DIR=" + store}, testTimeout, "status", "--porcelain")
	if err != nil {
		t.Fatalf("Run err = %v", err)
	}

	if strings.TrimSpace(out) != "PAYLOAD" {
		t.Errorf("stdout = %q, want the child's stdout alone (no stderr diagnostic)", out)
	}
	logged, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	got := string(logged)
	if !strings.Contains(got, "argv: -c core.hooksPath= -c core.fsmonitor=false status --porcelain") {
		t.Errorf("argv = %q, want the hardening option ahead of the subcommand", got)
	}
	if !strings.Contains(got, "nosystem: 1") {
		t.Errorf("env = %q, want GIT_CONFIG_NOSYSTEM=1 to survive the caller's env", got)
	}
	if !strings.Contains(got, "gitdir: "+store) {
		t.Errorf("env = %q, want the caller's GIT_DIR appended", got)
	}
	if strings.Contains(got, "shhh-secret") || !strings.Contains(got, "apikey: unset") {
		t.Errorf("env = %q, want the allowlist to have dropped APOGEE_API_KEY", got)
	}
	// The probe must have run under the caller's environment too — otherwise a redirected run
	// would be judged by the config of whatever repository happens to sit in dir.
	if !strings.Contains(got, "argv: -c core.hooksPath= -c core.fsmonitor=false config --local") {
		t.Errorf("record = %q, want the command-config probe among the invocations", got)
	}
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if strings.HasPrefix(line, "gitdir: ") && line != "gitdir: "+store {
			t.Errorf("an invocation ran without the caller's GIT_DIR: %q", line)
		}
	}
}

// TestRun_NonZeroExitIsAnError pins the deliberate flattening: the engine's git has callers that
// treat every failure as "skip this check", so a clean non-zero exit is an error here rather than
// the captured outcome a TOOL would show the model.
func TestRun_NonZeroExitIsAnError(t *testing.T) {
	posixScriptHost(t)

	fakeGit := writeFakeGit(t, t.TempDir(), "#!/bin/sh\necho 'fatal: not a git repository' >&2\nexit 128\n")
	host := fakeGitHost(fakeGit)

	_, err := host.Run(context.Background(), t.TempDir(), nil, testTimeout, "rev-parse", "--is-inside-work-tree")

	if err == nil {
		t.Fatal("Run err = nil, want a non-zero exit reported as an error")
	}
	if !strings.Contains(err.Error(), "exit 128") {
		t.Errorf("err = %q, want the exit status named", err)
	}
}

// TestRunDiagnosed_ReturnsTheWarningsOfAZeroExit pins why the diagnosed sibling exists: a command
// that reports a partial outcome as a warning and exits zero — `add` over a directory it cannot
// open — hands its caller that warning, kept apart from stdout, where Run would have dropped it.
func TestRunDiagnosed_ReturnsTheWarningsOfAZeroExit(t *testing.T) {
	posixScriptHost(t)

	fakeGit := writeFakeGit(t, t.TempDir(), "#!/bin/sh\n"+
		"case \"$*\" in *config*|*rev-parse*) exit 1 ;; esac\n"+
		"echo \"warning: could not open directory 'dir/': Permission denied\" >&2\n"+
		"echo PAYLOAD\n")
	host := fakeGitHost(fakeGit)

	stdout, stderr, err := host.RunDiagnosed(context.Background(), t.TempDir(), nil, testTimeout, "add", "-A")
	if err != nil {
		t.Fatalf("RunDiagnosed err = %v", err)
	}
	if strings.TrimSpace(stdout) != "PAYLOAD" {
		t.Errorf("stdout = %q, want the child's stdout alone", stdout)
	}
	if strings.TrimSpace(stderr) != "warning: could not open directory 'dir/': Permission denied" {
		t.Errorf("stderr = %q, want the warning the zero exit carried", stderr)
	}
}

// TestRunTo_StreamsPayloadUntruncated pins why the streaming sibling exists: a caller whose
// payload IS the child's output — a blob read out of an object database — must not have it cut at
// the capped path's ceiling, where the truncation would silently corrupt the bytes it writes.
func TestRunTo_StreamsPayloadUntruncated(t *testing.T) {
	posixScriptHost(t)

	const chunk = 64
	const repeats = 8192 // 512 KiB — twice the capped path's ceiling
	fakeGit := writeFakeGit(t, t.TempDir(), "#!/bin/sh\n"+
		"case \"$*\" in *config*|*rev-parse*) exit 1 ;; esac\n"+
		"line=0123456789012345678901234567890123456789012345678901234567890123\n"+
		"n=0\nwhile [ $n -lt 8192 ]; do printf '%s' \"$line\"; n=$((n+1)); done\n")
	host := fakeGitHost(fakeGit)

	var payload bytes.Buffer
	if err := host.RunTo(context.Background(), t.TempDir(), nil, testTimeout, &payload, "cat-file", "blob", "deadbeef"); err != nil {
		t.Fatalf("RunTo err = %v", err)
	}

	if payload.Len() <= subprocess.MaxSubprocessOutputBytes {
		t.Fatalf("payload = %d bytes, want more than the capped path's %d-byte ceiling (the fixture must exceed it for this to assert anything)",
			payload.Len(), subprocess.MaxSubprocessOutputBytes)
	}
	if payload.Len() != chunk*repeats {
		t.Errorf("payload = %d bytes, want %d untruncated", payload.Len(), chunk*repeats)
	}
	if strings.Contains(payload.String(), "output truncated") {
		t.Error("payload carries the capped path's truncation marker; the stream must be uncapped")
	}
}

// ----------------------------------------------------------------------------
// The repo-local command-config refusal
// ----------------------------------------------------------------------------

// TestCommandConfigRefusal_JudgesTheRepositoryTheRunActuallyReaches pins the regression a shared
// runner makes possible: the probe must run under the CALLER's environment, so a workspace whose
// own .git/config names a credential helper refuses the git TOOLS while a run redirected by
// GIT_DIR into a store of apogee's own — a different repository, with a config the workspace
// bytes never touched — is judged by that store's config instead.
func TestCommandConfigRefusal_JudgesTheRepositoryTheRunActuallyReaches(t *testing.T) {
	gitPath := realGit(t)

	root := t.TempDir()
	runRealGit(t, gitPath, root, "init", "-b", "main")
	runRealGit(t, gitPath, root, "config", "--local", "credential.helper", "!/bin/sh -c 'echo hostile'")

	store := filepath.Join(t.TempDir(), "objects")
	runRealGit(t, gitPath, filepath.Dir(store), "init", "--bare", store)

	res, err := gitexec.OS().Capture(context.Background(), gitPath, root, testTimeout, "status", "--porcelain")
	if err != nil {
		t.Fatalf("Capture err = %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("Capture succeeded on a repository configuring a credential helper: %q", res.CombinedOutput)
	}
	if !strings.Contains(res.CombinedOutput, "credential.helper") {
		t.Errorf("refusal = %q, want it to name the key it found", res.CombinedOutput)
	}

	out, err := gitexec.OS().Run(context.Background(), root, []string{
		"GIT_DIR=" + store,
		"GIT_WORK_TREE=" + root,
		"GIT_INDEX_FILE=" + filepath.Join(store, "apogee-index"),
	}, testTimeout, "rev-parse", "--git-dir")
	if err != nil {
		t.Fatalf("Run under GIT_DIR err = %v, want the store's own (clean) config to decide", err)
	}
	if strings.TrimSpace(out) != store {
		t.Errorf("git-dir = %q, want the redirected store %q", strings.TrimSpace(out), store)
	}
}

// ----------------------------------------------------------------------------
// Host — the funnel over a value, scriptable without a git
// ----------------------------------------------------------------------------

// gitAnswer scripts one fake git run: given the argv the launcher was handed, flattened to one
// space-joined string, it returns the outcome the run reports.
type gitAnswer func(argv string) subprocess.SubprocessResult

// cleanRepository answers the command-config probe as a repository with nothing to refuse — the
// rev-parse reaches it and names no file, and every scope listing passes with a non-zero exit —
// and every other invocation with command's answer.
func cleanRepository(command gitAnswer) gitAnswer {
	return func(argv string) subprocess.SubprocessResult {
		switch {
		case strings.Contains(argv, " rev-parse --git-path "):
			return subprocess.SubprocessResult{}
		case strings.Contains(argv, " --show-origin --list -z"):
			return subprocess.SubprocessResult{ExitCode: 1}
		}
		return command(argv)
	}
}

// fakeHost returns a Host whose Look answers gitPath and whose launchers answer from answer,
// with the specs every launch was handed. No real git runs: the spawned argv is never executed.
func fakeHost(gitPath string, answer gitAnswer) (gitexec.Host, *[]subprocess.SubprocessSpec) {
	spawned := &[]subprocess.SubprocessSpec{}
	host := gitexec.Host{
		Look: func(string) (string, error) { return gitPath, nil },
		Spawn: func(_ context.Context, spec subprocess.SubprocessSpec) (subprocess.SubprocessResult, error) {
			*spawned = append(*spawned, spec)
			return answer(strings.Join(spec.Argv, " ")), nil
		},
		SpawnTo: func(_ context.Context, spec subprocess.SubprocessSpec, stdout io.Writer) (subprocess.SubprocessResult, error) {
			*spawned = append(*spawned, spec)
			res := answer(strings.Join(spec.Argv, " "))
			_, err := io.WriteString(stdout, res.Stdout)
			res.Stdout = ""
			return res, err
		},
	}
	return host, spawned
}

// outsideGit returns an absolute git path outside every root a test runs in, so the exec fence
// lets it through. Nothing is written there: a fake launcher never executes it.
func outsideGit(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "git")
}

// TestHost_RunReportsAnAbsentGitWithoutSpawning pins the graceful degradation through a fake
// Look: a lookup that finds no git yields UnavailableMessage, and nothing is launched.
func TestHost_RunReportsAnAbsentGitWithoutSpawning(t *testing.T) {
	t.Parallel()
	host, spawned := fakeHost("", cleanRepository(func(string) subprocess.SubprocessResult {
		return subprocess.SubprocessResult{}
	}))
	host.Look = func(string) (string, error) { return "", exec.ErrNotFound }

	_, err := host.Run(context.Background(), t.TempDir(), nil, testTimeout, "status")

	if err == nil || err.Error() != gitexec.UnavailableMessage || len(*spawned) != 0 {
		t.Errorf("Run err = %v after %d launches, want %q and none", err, len(*spawned), gitexec.UnavailableMessage)
	}
}

// TestHost_RunRefusesAPlantedGit pins that a fake Look replaces the lookup, never the fence: the
// git it answers from inside the workspace is refused with the fence's sentinel and never run.
func TestHost_RunRefusesAPlantedGit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	planted := filepath.Join(root, "bin", "git")
	if err := os.MkdirAll(filepath.Dir(planted), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(planted, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write planted git: %v", err)
	}
	host, spawned := fakeHost(planted, cleanRepository(func(string) subprocess.SubprocessResult {
		return subprocess.SubprocessResult{}
	}))

	_, err := host.Run(context.Background(), root, nil, testTimeout, "status")

	if !errors.Is(err, security.ErrExecFromWritablePath) || len(*spawned) != 0 {
		t.Errorf("Run err = %v after %d launches, want the exec-fence refusal and none", err, len(*spawned))
	}
}

// TestHost_RunReportsANonZeroExit pins that a scripted exit status reaches Run's flattened error
// exactly as a real git's would.
func TestHost_RunReportsANonZeroExit(t *testing.T) {
	t.Parallel()
	host, _ := fakeHost(outsideGit(t), cleanRepository(func(string) subprocess.SubprocessResult {
		return subprocess.SubprocessResult{ExitCode: 128, CombinedOutput: "fatal: not a git repository"}
	}))

	_, err := host.Run(context.Background(), t.TempDir(), nil, testTimeout, "rev-parse", "--is-inside-work-tree")

	if err == nil || !strings.Contains(err.Error(), "exit 128") {
		t.Errorf("Run err = %v, want the exit status named", err)
	}
}

// TestHost_DataEntryPointsReportATruncatedStdoutAsIncomplete pins that a stdout which overran the
// output cap is never handed back as success: Run, Query and RunDiagnosed each fail with an error
// matching ErrIncomplete that names the cap, return no stdout, and RunDiagnosed still returns the
// run's stderr beside it.
func TestHost_DataEntryPointsReportATruncatedStdoutAsIncomplete(t *testing.T) {
	t.Parallel()
	const warning = "warning: something git said"
	cases := []struct {
		name       string
		call       func(h gitexec.Host, gitPath, root string) (stdout, stderr string, err error)
		wantStderr string
	}{
		{name: "Run", call: func(h gitexec.Host, _, root string) (string, string, error) {
			out, err := h.Run(context.Background(), root, nil, testTimeout, "diff")
			return out, "", err
		}},
		{name: "Query", call: func(h gitexec.Host, gitPath, root string) (string, string, error) {
			out, err := h.Query(context.Background(), gitPath, root, nil, testTimeout, "diff")
			return out, "", err
		}},
		{name: "RunDiagnosed", call: func(h gitexec.Host, _, root string) (string, string, error) {
			return h.RunDiagnosed(context.Background(), root, nil, testTimeout, "diff")
		}, wantStderr: warning},
	}
	for _, tc := range cases {
		name := tc.name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gitPath := outsideGit(t)
			host, _ := fakeHost(gitPath, cleanRepository(func(string) subprocess.SubprocessResult {
				return subprocess.SubprocessResult{Truncated: true, Stdout: "diff --git a/x b/x\n+partial", CombinedOutput: warning}
			}))

			out, stderr, err := tc.call(host, gitPath, t.TempDir())

			if !errors.Is(err, gitexec.ErrIncomplete) {
				t.Fatalf("%s err = %v, want a failure matching ErrIncomplete", name, err)
			}
			if want := fmt.Sprintf("git diff: output exceeded %d bytes", subprocess.MaxSubprocessOutputBytes); err.Error() != want {
				t.Errorf("%s err = %q, want %q", name, err, want)
			}
			if out != "" {
				t.Errorf("%s stdout = %q, want nothing from a truncated run", name, out)
			}
			if stderr != tc.wantStderr {
				t.Errorf("%s stderr = %q, want %q", name, stderr, tc.wantStderr)
			}
		})
	}
}

// TestHost_RunReportsANonZeroExitOverATruncatedStdout pins the order of the two failures: a run
// that exited non-zero is git's own answer whatever it printed, so a cut stdout beside it still
// reports the exit status as the plain error, never as an incomplete one.
func TestHost_RunReportsANonZeroExitOverATruncatedStdout(t *testing.T) {
	t.Parallel()
	host, _ := fakeHost(outsideGit(t), cleanRepository(func(string) subprocess.SubprocessResult {
		return subprocess.SubprocessResult{ExitCode: 1, Truncated: true, CombinedOutput: "fatal: bad"}
	}))

	_, err := host.Run(context.Background(), t.TempDir(), nil, testTimeout, "diff")

	if err == nil || errors.Is(err, gitexec.ErrIncomplete) || !strings.Contains(err.Error(), "exit 1") {
		t.Errorf("Run err = %v, want the plain exit-status error", err)
	}
}

// TestHost_RunNamesTheSubcommandPastLeadingOptions pins that every failure message names the
// subcommand, not a global option before it: each outcome is driven with the snapshot store's own
// staging spelling, whose leading `-c core.excludesFile=` once put `-c` in the message.
func TestHost_RunNamesTheSubcommandPastLeadingOptions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		result     subprocess.SubprocessResult
		wantPrefix string
	}{
		{name: "TimedOut", result: subprocess.SubprocessResult{TimedOut: true, ExitCode: -1}, wantPrefix: "git add: timed out after"},
		{name: "DrainWedged", result: subprocess.SubprocessResult{DrainWedged: true, ExitCode: -1}, wantPrefix: "git add: output drain wedged"},
		{name: "ExitCode", result: subprocess.SubprocessResult{ExitCode: 128, CombinedOutput: "fatal: bad"}, wantPrefix: "git add: exit 128"},
		{name: "Truncated", result: subprocess.SubprocessResult{Truncated: true}, wantPrefix: "git add: output exceeded"},
	}
	for _, tc := range cases {
		name, result, wantPrefix := tc.name, tc.result, tc.wantPrefix
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			host, _ := fakeHost(outsideGit(t), cleanRepository(func(string) subprocess.SubprocessResult {
				return result
			}))

			_, err := host.Run(context.Background(), t.TempDir(), nil, testTimeout,
				"-c", "core.excludesFile=", "add", "-A", "--ignore-errors")

			if err == nil || !strings.HasPrefix(err.Error(), wantPrefix) {
				t.Errorf("Run err = %v, want a message beginning %q", err, wantPrefix)
			}
		})
	}
}

// TestHost_CaptureRefusesARepositoryWhoseConfigNamesAProgram pins that the probe spawns through
// the Host too: a scripted --local listing carrying a filter driver refuses the call, and the
// command itself is never launched.
func TestHost_CaptureRefusesARepositoryWhoseConfigNamesAProgram(t *testing.T) {
	t.Parallel()
	gitPath := outsideGit(t)
	host, spawned := fakeHost(gitPath, func(argv string) subprocess.SubprocessResult {
		if strings.Contains(argv, " config --local ") {
			return subprocess.SubprocessResult{Stdout: "file:.git/config\x00filter.x.clean\ntrue\x00"}
		}
		return subprocess.SubprocessResult{}
	})

	res, err := host.Capture(context.Background(), gitPath, t.TempDir(), testTimeout, "status")
	if err != nil {
		t.Fatalf("Capture err = %v", err)
	}

	for _, spec := range *spawned {
		if strings.HasSuffix(strings.Join(spec.Argv, " "), " status") {
			t.Errorf("the refused command was launched: %q", spec.Argv)
		}
	}
	if want := gitexec.CommandConfigRefusal([]string{"filter.x.clean"}); res.CombinedOutput != want {
		t.Errorf("Capture output = %q, want the refusal %q", res.CombinedOutput, want)
	}
}

// TestHost_SpawnReceivesTheHardenedSpec pins that a fake launcher replaces the process, not the
// hardening: the spec it is handed carries the global options ahead of the subcommand and
// GIT_CONFIG_NOSYSTEM after the scoped allowlist.
func TestHost_SpawnReceivesTheHardenedSpec(t *testing.T) {
	t.Parallel()
	host, spawned := fakeHost(outsideGit(t), cleanRepository(func(string) subprocess.SubprocessResult {
		return subprocess.SubprocessResult{Stdout: "PAYLOAD"}
	}))

	if _, err := host.Run(context.Background(), t.TempDir(), nil, testTimeout, "status"); err != nil {
		t.Fatalf("Run err = %v", err)
	}

	last := (*spawned)[len(*spawned)-1]
	if got := strings.Join(last.Argv[1:], " "); got != "-c core.hooksPath= -c core.fsmonitor=false status" {
		t.Errorf("argv = %q, want the hardening options ahead of the subcommand", got)
	}
	if !slices.Contains(last.Env, "GIT_CONFIG_NOSYSTEM=1") {
		t.Errorf("env = %q, want GIT_CONFIG_NOSYSTEM=1", last.Env)
	}
}

// TestHost_RunToStreamsThroughSpawnTo pins that the streaming entry takes the Host's streaming
// launcher, with the payload written to the caller's writer rather than returned.
func TestHost_RunToStreamsThroughSpawnTo(t *testing.T) {
	t.Parallel()
	host, _ := fakeHost(outsideGit(t), cleanRepository(func(string) subprocess.SubprocessResult {
		return subprocess.SubprocessResult{Stdout: "PAYLOAD"}
	}))
	host.Spawn = func(_ context.Context, spec subprocess.SubprocessSpec) (subprocess.SubprocessResult, error) {
		if strings.Contains(strings.Join(spec.Argv, " "), "cat-file") {
			t.Errorf("RunTo launched its command through Spawn: %q", spec.Argv)
		}
		return cleanRepository(func(string) subprocess.SubprocessResult {
			return subprocess.SubprocessResult{}
		})(strings.Join(spec.Argv, " ")), nil
	}

	var payload bytes.Buffer
	if err := host.RunTo(context.Background(), t.TempDir(), nil, testTimeout, &payload, "cat-file", "blob", "deadbeef"); err != nil {
		t.Fatalf("RunTo err = %v", err)
	}

	if payload.String() != "PAYLOAD" {
		t.Errorf("payload = %q, want the SpawnTo launcher's stream", payload.String())
	}
}

// TestHost_ZeroValueIsTheOS pins that every nil field falls back to the real OS: the zero Host,
// and OS(), scope the environment exactly as the package-level SafeEnv does.
func TestHost_ZeroValueIsTheOS(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	want := gitexec.SafeEnv(root)

	for name, host := range map[string]gitexec.Host{"Host{}": {}, "OS()": gitexec.OS()} {
		if got := host.SafeEnv(root); !slices.Equal(got, want) {
			t.Errorf("%s.SafeEnv(root) = %q, want %q", name, got, want)
		}
	}
}

// TestHost_RefusesAnIncompleteCommandConfigProbe pins that the probe fails closed: a probe call
// that timed out, wedged its drain or overran the output cap refuses both Capture (a failed
// outcome) and Run (an error) with a sentence naming the incomplete call, and launches no
// command. The next call, with git answering cleanly, probes afresh and passes — the incomplete
// answer was never memoised either way.
func TestHost_RefusesAnIncompleteCommandConfigProbe(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		call   string
		broken subprocess.SubprocessResult
		step   string
	}{
		{
			name:   "listing timed out",
			call:   " config --local ",
			broken: subprocess.SubprocessResult{TimedOut: true, ExitCode: -1, Stdout: "file:.git/config\x00user.name\nx\x00"},
			step:   "git config --local timed out",
		},
		{
			name:   "listing drain wedged",
			call:   " config --local ",
			broken: subprocess.SubprocessResult{DrainWedged: true, ExitCode: -1},
			step:   "git config --local: output drain wedged",
		},
		{
			name:   "listing truncated",
			call:   " config --worktree ",
			broken: subprocess.SubprocessResult{Truncated: true, Stdout: "file:.git/config\x00user.name\nx\x00file:.git/con"},
			step:   "git config --worktree printed more than",
		},
		{
			name:   "rev-parse timed out with no output",
			call:   " rev-parse --git-path ",
			broken: subprocess.SubprocessResult{TimedOut: true, ExitCode: -1},
			step:   "git rev-parse --git-path timed out",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gitPath, root := outsideGit(t), t.TempDir()
			isBroken := true
			host, spawned := fakeHost(gitPath, cleanRepository(func(string) subprocess.SubprocessResult {
				return subprocess.SubprocessResult{Stdout: "PAYLOAD"}
			}))
			clean := host.Spawn
			host.Spawn = func(ctx context.Context, spec subprocess.SubprocessSpec) (subprocess.SubprocessResult, error) {
				if isBroken && strings.Contains(strings.Join(spec.Argv, " "), tc.call) {
					*spawned = append(*spawned, spec)
					return tc.broken, nil
				}
				return clean(ctx, spec)
			}

			captured, captureErr := host.Capture(context.Background(), gitPath, root, testTimeout, "status")
			_, runErr := host.Run(context.Background(), root, nil, testTimeout, "status")
			isBroken = false
			before := len(*spawned)
			passed, passErr := host.Capture(context.Background(), gitPath, root, testTimeout, "status")

			if captureErr != nil || captured.ExitCode != 1 || !isIncompleteRefusal(captured.CombinedOutput, tc.step) {
				t.Errorf("Capture = %q (exit %d, err %v), want the incomplete-probe refusal naming %q",
					captured.CombinedOutput, captured.ExitCode, captureErr, tc.step)
			}
			if runErr == nil || !isIncompleteRefusal(runErr.Error(), tc.step) {
				t.Errorf("Run err = %v, want the incomplete-probe refusal naming %q", runErr, tc.step)
			}
			for _, spec := range (*spawned)[:before] {
				if strings.HasSuffix(strings.Join(spec.Argv, " "), " status") {
					t.Errorf("the refused command was launched: %q", spec.Argv)
				}
			}
			if passErr != nil || passed.ExitCode != 0 || passed.Stdout != "PAYLOAD" {
				t.Errorf("Capture after a clean probe = %+v (err %v), want the command's own outcome", passed, passErr)
			}
			if !launchedProbe(*spawned, before) {
				t.Error("the clean call launched no probe — an incomplete answer was memoised")
			}
		})
	}
}

// TestDataPathRefusalIsTyped pins what the data path's error says about a refusal: a repo-local
// command-config key and a merge driver each fail Run with a RefusedError whose Sentence — and
// whose Error() — is the refusal sentence, while a failed resolution is never one: a planted git
// still matches the exec fence's sentinel and an absent git is still UnavailableMessage.
func TestDataPathRefusalIsTyped(t *testing.T) {
	t.Parallel()
	nothing := func(string) subprocess.SubprocessResult { return subprocess.SubprocessResult{} }

	refusals := []struct {
		name    string
		listing string
		args    []string
		want    string
	}{
		{
			name:    "command-config key",
			listing: "file:.git/config\x00filter.x.clean\ntrue\x00",
			args:    []string{"status"},
			want:    gitexec.CommandConfigRefusal([]string{"filter.x.clean"}),
		},
		{
			name:    "merge driver",
			listing: "file:.git/config\x00" + mergeDriverKey + "\nx %O %A %B\x00",
			args:    []string{"rebase", "side"},
			want:    gitexec.MergeDriverRefusal("rebase", []string{mergeDriverKey}),
		},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host, _ := fakeHost(outsideGit(t), mergeDriverListing(tc.listing))

			_, err := host.Run(context.Background(), t.TempDir(), nil, testTimeout, tc.args...)

			var refused gitexec.RefusedError
			if !errors.As(err, &refused) {
				t.Fatalf("Run err = %v (%T), want a RefusedError", err, err)
			}
			if refused.Sentence != tc.want || err.Error() != tc.want {
				t.Errorf("Sentence = %q, Error() = %q, want both %q", refused.Sentence, err.Error(), tc.want)
			}
			if errors.Is(err, gitexec.ErrIncomplete) {
				t.Errorf("Run err = %v matches ErrIncomplete, want a standing refusal", err)
			}
		})
	}

	t.Run("planted git", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		planted := filepath.Join(root, "bin", "git")
		if err := os.MkdirAll(filepath.Dir(planted), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(planted, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write planted git: %v", err)
		}
		host, _ := fakeHost(planted, cleanRepository(nothing))

		_, err := host.Run(context.Background(), root, nil, testTimeout, "status")

		var refused gitexec.RefusedError
		if !errors.Is(err, security.ErrExecFromWritablePath) || errors.As(err, &refused) {
			t.Errorf("Run err = %v (%T), want the exec-fence refusal and no RefusedError", err, err)
		}
	})

	t.Run("absent git", func(t *testing.T) {
		t.Parallel()
		host, _ := fakeHost("", cleanRepository(nothing))
		host.Look = func(string) (string, error) { return "", exec.ErrNotFound }

		_, err := host.Run(context.Background(), t.TempDir(), nil, testTimeout, "status")

		var refused gitexec.RefusedError
		if err == nil || err.Error() != gitexec.UnavailableMessage || errors.As(err, &refused) {
			t.Errorf("Run err = %v (%T), want %q and no RefusedError", err, err, gitexec.UnavailableMessage)
		}
	})
}

// TestIncompleteIsNotARefusal pins the other side of the typed refusal: a command-config probe
// that did not complete fails Run with an error matching ErrIncomplete and never a RefusedError,
// so a caller that latches on a refusal asks again after an unfinished check.
func TestIncompleteIsNotARefusal(t *testing.T) {
	t.Parallel()
	host, _ := fakeHost(outsideGit(t), cleanRepository(func(string) subprocess.SubprocessResult {
		return subprocess.SubprocessResult{}
	}))
	clean := host.Spawn
	host.Spawn = func(ctx context.Context, spec subprocess.SubprocessSpec) (subprocess.SubprocessResult, error) {
		if strings.Contains(strings.Join(spec.Argv, " "), " config --local ") {
			return subprocess.SubprocessResult{TimedOut: true, ExitCode: -1}, nil
		}
		return clean(ctx, spec)
	}

	_, err := host.Run(context.Background(), t.TempDir(), nil, testTimeout, "status")

	var refused gitexec.RefusedError
	if !errors.Is(err, gitexec.ErrIncomplete) || errors.As(err, &refused) {
		t.Errorf("Run err = %v (%T), want a failure matching ErrIncomplete and no RefusedError", err, err)
	}
}

// isIncompleteRefusal reports whether text is the incomplete-probe refusal and names step.
func isIncompleteRefusal(text, step string) bool {
	return strings.HasPrefix(text, "git refused:") && strings.Contains(text, "did not complete") &&
		strings.Contains(text, step)
}

// launchedProbe reports whether any spec launched from index from on is the probe's rev-parse.
func launchedProbe(spawned []subprocess.SubprocessSpec, from int) bool {
	for _, spec := range spawned[from:] {
		if strings.Contains(strings.Join(spec.Argv, " "), " rev-parse --git-path ") {
			return true
		}
	}
	return false
}

// TestHost_CapturePassesAWorktreeListingThatExits128 pins the pass case the fail-closed probe
// keeps: a --worktree listing git finishes with exit 128 (the scope does not apply) is a clean
// answer, not an incomplete one, so the command runs.
func TestHost_CapturePassesAWorktreeListingThatExits128(t *testing.T) {
	t.Parallel()
	gitPath := outsideGit(t)
	host, _ := fakeHost(gitPath, func(argv string) subprocess.SubprocessResult {
		switch {
		case strings.Contains(argv, " config --worktree "):
			return subprocess.SubprocessResult{ExitCode: 128, CombinedOutput: "fatal: --worktree cannot be used"}
		case strings.Contains(argv, " rev-parse --git-path "), strings.Contains(argv, " config --local "):
			return subprocess.SubprocessResult{}
		}
		return subprocess.SubprocessResult{CombinedOutput: "PAYLOAD"}
	})

	res, err := host.Capture(context.Background(), gitPath, t.TempDir(), testTimeout, "status")

	if err != nil || res.ExitCode != 0 || res.CombinedOutput != "PAYLOAD" {
		t.Errorf("Capture = %+v (err %v), want the command's own outcome", res, err)
	}
}

// ----------------------------------------------------------------------------
// CaptureEnv and CaptureEnvTo — Capture with a caller's env and spawn options
// ----------------------------------------------------------------------------

// TestHost_CaptureEnvAppendsTheEnvToTheProbeAndTheChild pins that the caller's env reaches every
// launch a CaptureEnv makes — each probe call as much as the command — and comes last, after
// GIT_CONFIG_NOSYSTEM, so it adds to the hardened environment and removes nothing from it.
func TestHost_CaptureEnvAppendsTheEnvToTheProbeAndTheChild(t *testing.T) {
	t.Parallel()
	gitPath := outsideGit(t)
	host, spawned := fakeHost(gitPath, cleanRepository(func(string) subprocess.SubprocessResult {
		return subprocess.SubprocessResult{CombinedOutput: "PAYLOAD"}
	}))
	env := []string{"GIT_TERMINAL_PROMPT=0", "GIT_DIR=/elsewhere"}

	res, err := host.CaptureEnv(context.Background(), gitPath, t.TempDir(), env, gitexec.CaptureOptions{}, testTimeout, "status")
	if err != nil || res.CombinedOutput != "PAYLOAD" {
		t.Fatalf("CaptureEnv = %q (err %v), want the command's own outcome", res.CombinedOutput, err)
	}

	if len(*spawned) < 2 {
		t.Fatalf("%d launches, want the probe's and the command's", len(*spawned))
	}
	for _, spec := range *spawned {
		if got := spec.Env[len(spec.Env)-len(env):]; !slices.Equal(got, env) {
			t.Errorf("launch %q ends its env with %q, want the caller's %q last", spec.Argv, got, env)
		}
		if !slices.Contains(spec.Env, "GIT_CONFIG_NOSYSTEM=1") {
			t.Errorf("launch %q env = %q, want GIT_CONFIG_NOSYSTEM=1 kept", spec.Argv, spec.Env)
		}
	}
	if last := (*spawned)[len(*spawned)-1]; !strings.HasSuffix(strings.Join(last.Argv, " "), " status") {
		t.Errorf("last launch = %q, want the command after its probe", last.Argv)
	}
}

// TestHost_CaptureIsCaptureEnvWithNothingAdded pins that Capture's launch is unchanged by the
// new entry point: its command spec equals CaptureEnv's with a nil env and zero options —
// argv, env, directory, timeout and the terminal it keeps — and so does its outcome.
func TestHost_CaptureIsCaptureEnvWithNothingAdded(t *testing.T) {
	t.Parallel()
	gitPath, root := outsideGit(t), t.TempDir()
	host, spawned := fakeHost(gitPath, cleanRepository(func(string) subprocess.SubprocessResult {
		return subprocess.SubprocessResult{CombinedOutput: "PAYLOAD", ExitCode: 3}
	}))

	viaCapture, captureErr := host.Capture(context.Background(), gitPath, root, testTimeout, "status", "--porcelain")
	captureSpec := (*spawned)[len(*spawned)-1]
	viaEnv, envErr := host.CaptureEnv(context.Background(), gitPath, root, nil, gitexec.CaptureOptions{}, testTimeout, "status", "--porcelain")
	envSpec := (*spawned)[len(*spawned)-1]

	if captureErr != nil || envErr != nil || !reflect.DeepEqual(viaCapture, viaEnv) {
		t.Errorf("Capture = %+v (err %v), CaptureEnv = %+v (err %v), want the same outcome",
			viaCapture, captureErr, viaEnv, envErr)
	}
	if !reflect.DeepEqual(captureSpec, envSpec) {
		t.Errorf("Capture launched %+v, CaptureEnv(nil, zero) launched %+v, want the same spec", captureSpec, envSpec)
	}
	if got := strings.Join(captureSpec.Argv[1:], " "); got != "-c core.hooksPath= -c core.fsmonitor=false status --porcelain" {
		t.Errorf("Capture argv = %q, want the hardening options ahead of the subcommand and nothing else", got)
	}
	if captureSpec.NoTerminal {
		t.Error("Capture launched with NoTerminal set, want the terminal left as it was")
	}
}

// TestHost_CaptureEnvCarriesNoTerminalOntoTheSpec pins that CaptureOptions.NoTerminal reaches the
// spec the launcher is handed, on the captured and the streamed entry point alike.
func TestHost_CaptureEnvCarriesNoTerminalOntoTheSpec(t *testing.T) {
	t.Parallel()
	gitPath := outsideGit(t)
	host, spawned := fakeHost(gitPath, cleanRepository(func(string) subprocess.SubprocessResult {
		return subprocess.SubprocessResult{}
	}))
	opts := gitexec.CaptureOptions{NoTerminal: true}

	if _, err := host.CaptureEnv(context.Background(), gitPath, t.TempDir(), nil, opts, testTimeout, "fetch"); err != nil {
		t.Fatalf("CaptureEnv err = %v", err)
	}
	captured := (*spawned)[len(*spawned)-1]
	if _, err := host.CaptureEnvTo(context.Background(), gitPath, t.TempDir(), nil, opts, testTimeout, io.Discard, "status"); err != nil {
		t.Fatalf("CaptureEnvTo err = %v", err)
	}
	streamed := (*spawned)[len(*spawned)-1]

	for name, spec := range map[string]subprocess.SubprocessSpec{"CaptureEnv": captured, "CaptureEnvTo": streamed} {
		if !spec.NoTerminal {
			t.Errorf("%s launched %q with NoTerminal=false, want true", name, spec.Argv)
		}
	}
}

// TestCaptureEnvTo_StreamsPastTheCapUntruncated pins that the streamed entry point hands the
// whole payload over — twice the capped path's ceiling, byte for byte — with Truncated never set.
func TestCaptureEnvTo_StreamsPastTheCapUntruncated(t *testing.T) {
	posixScriptHost(t)

	const chunk = 64
	const repeats = 8192 // 512 KiB — twice the capped path's ceiling
	fakeGit := writeFakeGit(t, t.TempDir(), "#!/bin/sh\n"+
		"case \"$*\" in *config*|*rev-parse*) exit 1 ;; esac\n"+
		"line=0123456789012345678901234567890123456789012345678901234567890123\n"+
		"n=0\nwhile [ $n -lt 8192 ]; do printf '%s' \"$line\"; n=$((n+1)); done\n")
	host := fakeGitHost(fakeGit)

	var payload bytes.Buffer
	res, err := host.CaptureEnvTo(context.Background(), fakeGit, t.TempDir(), []string{"GIT_TERMINAL_PROMPT=0"},
		gitexec.CaptureOptions{NoTerminal: true}, testTimeout, &payload, "status", "--porcelain=v2", "-z")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("CaptureEnvTo = exit %d, output %q (err %v), want a clean run", res.ExitCode, res.CombinedOutput, err)
	}

	if payload.Len() <= subprocess.MaxSubprocessOutputBytes {
		t.Fatalf("payload = %d bytes, want more than the capped path's %d-byte ceiling (the fixture must exceed it for this to assert anything)",
			payload.Len(), subprocess.MaxSubprocessOutputBytes)
	}
	if payload.Len() != chunk*repeats || res.Truncated {
		t.Errorf("payload = %d bytes (Truncated %v), want %d untruncated", payload.Len(), res.Truncated, chunk*repeats)
	}
}

// TestHost_CaptureEnvToProbesUnderTheSameEnv pins that the streamed entry point's probe runs
// under the caller's env, as CaptureEnv's does, and that the payload leaves through SpawnTo.
func TestHost_CaptureEnvToProbesUnderTheSameEnv(t *testing.T) {
	t.Parallel()
	gitPath := outsideGit(t)
	host, spawned := fakeHost(gitPath, cleanRepository(func(string) subprocess.SubprocessResult {
		return subprocess.SubprocessResult{Stdout: "PAYLOAD"}
	}))
	env := []string{"GIT_DIR=/elsewhere"}

	var payload bytes.Buffer
	if _, err := host.CaptureEnvTo(context.Background(), gitPath, t.TempDir(), env, gitexec.CaptureOptions{}, testTimeout, &payload, "status"); err != nil {
		t.Fatalf("CaptureEnvTo err = %v", err)
	}

	if payload.String() != "PAYLOAD" {
		t.Errorf("payload = %q, want the SpawnTo launcher's stream", payload.String())
	}
	if !launchedProbe(*spawned, 0) {
		t.Fatal("CaptureEnvTo launched no probe")
	}
	for _, spec := range *spawned {
		if !slices.Contains(spec.Env, env[0]) {
			t.Errorf("launch %q env = %q, want the caller's %q", spec.Argv, spec.Env, env[0])
		}
	}
}

// TestHost_CaptureEnvToWritesNothingInARefusedRepository pins the refusal on the streamed entry
// point: a repository whose own config names a program gets no command launch, nothing on the
// caller's writer, and the refusal as the failed outcome.
func TestHost_CaptureEnvToWritesNothingInARefusedRepository(t *testing.T) {
	t.Parallel()
	gitPath := outsideGit(t)
	host, spawned := fakeHost(gitPath, func(argv string) subprocess.SubprocessResult {
		if strings.Contains(argv, " config --local ") {
			return subprocess.SubprocessResult{Stdout: "file:.git/config\x00core.sshcommand\nssh -i key\x00"}
		}
		return subprocess.SubprocessResult{Stdout: "PAYLOAD"}
	})

	var payload bytes.Buffer
	res, err := host.CaptureEnvTo(context.Background(), gitPath, t.TempDir(), nil, gitexec.CaptureOptions{NoTerminal: true}, testTimeout, &payload, "status")
	if err != nil {
		t.Fatalf("CaptureEnvTo err = %v", err)
	}

	if payload.Len() != 0 {
		t.Errorf("payload = %q, want nothing written for a refused repository", payload.String())
	}
	for _, spec := range *spawned {
		if strings.HasSuffix(strings.Join(spec.Argv, " "), " status") {
			t.Errorf("the refused command was launched: %q", spec.Argv)
		}
	}
	if want := gitexec.CommandConfigRefusal([]string{"core.sshcommand"}); res.ExitCode != 1 || res.CombinedOutput != want {
		t.Errorf("CaptureEnvTo = exit %d, output %q; want exit 1 and the refusal %q", res.ExitCode, res.CombinedOutput, want)
	}
}

// TestCaptureEnv_JudgesTheRepositoryTheEnvReaches is Run's regression pin for the captured entry
// point, with a real git: a workspace whose own config names a credential helper is refused,
// while a CaptureEnv redirected by GIT_DIR into a clean store of apogee's own is judged by that
// store's config and runs.
func TestCaptureEnv_JudgesTheRepositoryTheEnvReaches(t *testing.T) {
	gitPath := realGit(t)

	root := t.TempDir()
	runRealGit(t, gitPath, root, "init", "-b", "main")
	runRealGit(t, gitPath, root, "config", "--local", "credential.helper", "!/bin/sh -c 'echo hostile'")
	store := filepath.Join(t.TempDir(), "objects")
	runRealGit(t, gitPath, filepath.Dir(store), "init", "--bare", store)

	refused, err := gitexec.OS().CaptureEnv(context.Background(), gitPath, root, nil, gitexec.CaptureOptions{}, testTimeout, "rev-parse", "--git-dir")
	if err != nil || refused.ExitCode == 0 || !strings.Contains(refused.CombinedOutput, "credential.helper") {
		t.Errorf("CaptureEnv in the workspace = exit %d, %q (err %v); want the refusal naming credential.helper",
			refused.ExitCode, refused.CombinedOutput, err)
	}

	env := []string{"GIT_DIR=" + store, "GIT_WORK_TREE=" + root}
	res, err := gitexec.OS().CaptureEnv(context.Background(), gitPath, root, env, gitexec.CaptureOptions{NoTerminal: true}, testTimeout, "rev-parse", "--git-dir")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("CaptureEnv under GIT_DIR = exit %d, %q (err %v); want the store's own clean config to decide",
			res.ExitCode, res.CombinedOutput, err)
	}
	if got := strings.TrimSpace(res.CombinedOutput); got != store {
		t.Errorf("git-dir = %q, want the redirected store %q", got, store)
	}
}

// ----------------------------------------------------------------------------
// Repo-local merge drivers: neutralised on every call, refused only for a merge
// ----------------------------------------------------------------------------

// mergeDriverKey is the planted driver's key as git's own listing spells it: section and variable
// lowercased, the mixed-case subsection kept — the spelling the override must repeat exactly.
const mergeDriverKey = "merge.Capcom-Log.driver"

// mergeDriverRepository builds a real repository whose history holds a merge commit that a
// remerge-diff can only re-create by running the merge driver: a committed .gitattributes routes
// f through Capcom-Log, both branches edit f's one line, and the sides were joined with -s ours,
// so re-merging them is a genuine content merge of f. Only after that is the driver planted in
// .git/config — a script that creates the returned marker if it ever runs — so a call missing its
// override fails the test rather than the fixture. It also returns a wrapper git that records
// every argv it is launched with in the returned record file and then runs the host's git.
func mergeDriverRepository(t *testing.T) (root, wrapper, record, marker string) {
	t.Helper()
	posixScriptHost(t)
	gitPath := realGit(t)
	root = t.TempDir()
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	runRealGit(t, gitPath, root, "init", "-b", "main")
	write(".gitattributes", "f merge=Capcom-Log\n")
	write("f", "base\n")
	runRealGit(t, gitPath, root, "add", ".")
	runRealGit(t, gitPath, root, "commit", "-m", "base")
	runRealGit(t, gitPath, root, "checkout", "-b", "side")
	write("f", "side\n")
	runRealGit(t, gitPath, root, "commit", "-am", "side")
	runRealGit(t, gitPath, root, "checkout", "main")
	write("f", "main\n")
	runRealGit(t, gitPath, root, "commit", "-am", "main")
	runRealGit(t, gitPath, root, "merge", "-s", "ours", "--no-edit", "side")

	scripts := t.TempDir()
	marker = filepath.Join(scripts, "driver.ran")
	driver := filepath.Join(scripts, "driver.sh")
	if err := os.WriteFile(driver, []byte("#!/bin/sh\n: > \""+marker+"\"\n"), 0o755); err != nil {
		t.Fatalf("write driver: %v", err)
	}
	appendToConfig(t, root, "[merge \"Capcom-Log\"]\n\tdriver = "+driver+" %O %A %B\n")

	record = filepath.Join(scripts, "argv")
	wrapper = writeFakeGit(t, scripts, "#!/bin/sh\necho \"$*\" >> \""+record+"\"\nexec \""+gitPath+"\" \"$@\"\n")
	return root, wrapper, record, marker
}

// requireDriverNeverRan fails the test when the planted merge driver has executed.
func requireDriverNeverRan(t *testing.T, marker string) {
	t.Helper()
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("the repository's merge driver executed")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat marker: %v", err)
	}
}

// TestMergeDriver_FixtureRunsTheDriverWithoutTheOverride pins that the fixture is live: the
// host's git, run bare, executes the planted driver on a remerge-diff — so the neutralisation
// tests below fail if their override goes missing.
func TestMergeDriver_FixtureRunsTheDriverWithoutTheOverride(t *testing.T) {
	root, _, _, marker := mergeDriverRepository(t)
	runRealGit(t, realGit(t), root, "show", "--remerge-diff", "HEAD")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("a bare remerge-diff did not run the planted driver (%v); the fixture asserts nothing", err)
	}
}

// TestMergeDriver_ReadVerbsRunWithTheDriverNeutralised pins the neutralisation half: in a
// repository whose own config names a merge driver and nothing else, every read — the two
// remerge-diff forms among them, which DO run a merge driver — succeeds, the driver never runs,
// and every launched argv carries the override with the subsection spelled as git lists it.
func TestMergeDriver_ReadVerbsRunWithTheDriverNeutralised(t *testing.T) {
	root, wrapper, record, marker := mergeDriverRepository(t)

	for _, args := range [][]string{
		{"status"},
		{"log", "-p"},
		{"show", "--remerge-diff", "HEAD"},
		{"log", "--diff-merges=remerge", "-p"},
	} {
		res, err := gitexec.OS().Capture(context.Background(), wrapper, root, testTimeout, args...)
		if err != nil {
			t.Fatalf("Capture %v err = %v", args, err)
		}
		if res.ExitCode != 0 {
			t.Errorf("Capture %v exit %d: %q", args, res.ExitCode, res.CombinedOutput)
		}
		requireDriverNeverRan(t, marker)
	}
	if _, err := gitexec.OS().Query(context.Background(), wrapper, root, nil, testTimeout, "show", "--remerge-diff", "HEAD"); err != nil {
		t.Errorf("Query remerge-diff err = %v", err)
	}
	requireDriverNeverRan(t, marker)

	logged, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read argv record: %v", err)
	}
	override := "-c " + mergeDriverKey + "= "
	commands := 0
	for _, line := range strings.Split(strings.TrimSpace(string(logged)), "\n") {
		if strings.Contains(line, " rev-parse --git-path ") || strings.Contains(line, " --show-origin --list -z") {
			continue
		}
		commands++
		if !strings.Contains(line, override) {
			t.Errorf("argv %q lacks the override %q", line, override)
		}
	}
	if commands != 5 {
		t.Errorf("recorded %d commands, want 5", commands)
	}
}

// TestMergeDriver_RefusesOnlyTheMergeCapableArgv pins the refusing half against the real git: an
// argv whose job is a content merge is refused with MergeDriverRefusal naming the key and never
// launched, while a fast-forward-only merge and the unwind flags run (git answers them itself).
// The driver never runs either way.
func TestMergeDriver_RefusesOnlyTheMergeCapableArgv(t *testing.T) {
	root, wrapper, _, marker := mergeDriverRepository(t)
	keys := []string{mergeDriverKey}

	for _, args := range [][]string{
		{"rebase", "side"},
		{"merge", "side"},
		{"merge-tree", "main", "side"},
		{"stash", "pop"},
		{"-c", "x.y=z", "rebase", "side"},
	} {
		res, err := gitexec.OS().Capture(context.Background(), wrapper, root, testTimeout, args...)
		if err != nil {
			t.Fatalf("Capture %v err = %v", args, err)
		}
		if want := gitexec.MergeDriverRefusal(subcommandOf(args), keys); res.CombinedOutput != want {
			t.Errorf("Capture %v = %q, want the refusal %q", args, res.CombinedOutput, want)
		}
		requireDriverNeverRan(t, marker)
	}
	if _, err := gitexec.OS().Query(context.Background(), wrapper, root, nil, testTimeout, "rebase", "side"); err == nil ||
		err.Error() != gitexec.MergeDriverRefusal("rebase", keys) {
		t.Errorf("Query rebase err = %v, want the merge-driver refusal", err)
	}

	for _, args := range [][]string{
		{"merge", "--ff-only", "side"},
		{"rebase", "--abort"},
		{"rebase", "--quit"},
	} {
		res, err := gitexec.OS().Capture(context.Background(), wrapper, root, testTimeout, args...)
		if err != nil {
			t.Fatalf("Capture %v err = %v", args, err)
		}
		if gitexec.IsCommandConfigRefusal(res.CombinedOutput) {
			t.Errorf("Capture %v was refused: %q", args, res.CombinedOutput)
		}
		requireDriverNeverRan(t, marker)
	}
}

// subcommandOf names the subcommand of a test argv, past a leading -c pair.
func subcommandOf(args []string) string {
	if args[0] == "-c" {
		return args[2]
	}
	return args[0]
}

// mergeDriverListing answers the probe as a repository whose --local config carries listing —
// one `file:<origin>\0<key>\n<value>\0` record per key — and every other invocation with an
// empty success.
func mergeDriverListing(listing string) gitAnswer {
	return func(argv string) subprocess.SubprocessResult {
		if strings.Contains(argv, " config --local ") {
			return subprocess.SubprocessResult{Stdout: listing}
		}
		return subprocess.SubprocessResult{}
	}
}

// TestMergeDriver_JudgesEachArgvByWhetherItCanMerge pins the merge-capable verb list row by row
// through a fake launcher: a refused argv is never launched and gets MergeDriverRefusal naming
// its verb; an admitted one is launched with the override ahead of its subcommand.
func TestMergeDriver_JudgesEachArgvByWhetherItCanMerge(t *testing.T) {
	t.Parallel()
	listing := "file:.git/config\x00merge.Capcom-Log.driver\nx %O %A %B\x00"
	for _, tc := range []struct {
		args    []string
		refused bool
	}{
		{[]string{"rebase", "@{u}"}, true},
		{[]string{"rebase", "--continue"}, true},
		{[]string{"cherry-pick", "abc"}, true},
		{[]string{"revert", "abc"}, true},
		{[]string{"am", "patch"}, true},
		{[]string{"merge-tree", "a", "b"}, true},
		{[]string{"merge", "side"}, true},
		{[]string{"pull"}, true},
		{[]string{"stash", "apply"}, true},
		{[]string{"stash", "pop", "--index"}, true},
		{[]string{"stash", "-q", "branch", "b"}, true},
		{[]string{"checkout", "-m", "main"}, true},
		{[]string{"switch", "--merge", "main"}, true},
		{[]string{"apply", "-3", "p"}, true},
		{[]string{"apply", "--3way", "p"}, true},
		{[]string{"-c", "x.y=z", "rebase"}, true},
		{[]string{"merge", "--ff-only", "side"}, false},
		{[]string{"pull", "--ff-only"}, false},
		{[]string{"rebase", "--abort"}, false},
		{[]string{"cherry-pick", "--quit"}, false},
		{[]string{"revert", "--abort"}, false},
		{[]string{"am", "--abort"}, false},
		{[]string{"merge", "--abort"}, false},
		{[]string{"stash", "list"}, false},
		{[]string{"stash"}, false},
		{[]string{"checkout", "main"}, false},
		{[]string{"checkout", "main", "--", "-m"}, false},
		{[]string{"apply", "p"}, false},
		{[]string{"status"}, false},
		{[]string{"log", "--remerge-diff"}, false},
		{[]string{"diff"}, false},
	} {
		host, spawned := fakeHost(outsideGit(t), mergeDriverListing(listing))
		res, err := host.Capture(context.Background(), outsideGit(t), t.TempDir(), testTimeout, tc.args...)
		if err != nil {
			t.Fatalf("Capture %v err = %v", tc.args, err)
		}
		last := strings.Join((*spawned)[len(*spawned)-1].Argv[1:], " ")
		if tc.refused {
			if want := gitexec.MergeDriverRefusal(subcommandOf(tc.args), []string{mergeDriverKey}); res.CombinedOutput != want {
				t.Errorf("Capture %v = %q, want the refusal %q", tc.args, res.CombinedOutput, want)
			}
			if strings.HasSuffix(last, strings.Join(tc.args, " ")) {
				t.Errorf("the refused argv %v was launched: %q", tc.args, last)
			}
			continue
		}
		want := "-c core.hooksPath= -c core.fsmonitor=false -c " + mergeDriverKey + "= " + strings.Join(tc.args, " ")
		if res.ExitCode != 0 || last != want {
			t.Errorf("Capture %v launched %q (exit %d, %q), want %q", tc.args, last, res.ExitCode, res.CombinedOutput, want)
		}
	}
}

// TestMergeDriver_OtherKeysStillRefuseTheRepository pins the boundary of the neutralisation: a
// driver whose subsection holds `=` cannot be emptied by `-c` (git splits at the first `=`, so the
// override would name another key and leave the driver live) and stays in the refusing class, and
// a driver beside any other command-valued key refuses the whole repository exactly as before,
// naming both keys. Neither run launches the command.
func TestMergeDriver_OtherKeysStillRefuseTheRepository(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		listing string
		names   []string
	}{
		{"file:.git/config\x00merge.a=b.driver\nx\x00", []string{"merge.a=b.driver"}},
		{
			"file:.git/config\x00merge.Capcom-Log.driver\nx\x00file:.git/config\x00filter.x.clean\ntrue\x00",
			[]string{mergeDriverKey, "filter.x.clean"},
		},
	} {
		host, spawned := fakeHost(outsideGit(t), mergeDriverListing(tc.listing))
		res, err := host.Capture(context.Background(), outsideGit(t), t.TempDir(), testTimeout, "status")
		if err != nil {
			t.Fatalf("Capture err = %v", err)
		}
		if want := gitexec.CommandConfigRefusal(tc.names); res.CombinedOutput != want {
			t.Errorf("Capture = %q, want the refusal %q", res.CombinedOutput, want)
		}
		for _, spec := range *spawned {
			if strings.HasSuffix(strings.Join(spec.Argv, " "), " status") {
				t.Errorf("the refused command was launched: %q", spec.Argv)
			}
		}
	}
}

// TestIsCommandConfigRefusal_TellsAStandingRefusalFromEverythingElse pins the predicate a caller
// recognises a refused repository by: true for both standing refusals, false for a probe that
// could not finish and for anything git itself might print.
func TestIsCommandConfigRefusal_TellsAStandingRefusalFromEverythingElse(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		gitexec.CommandConfigRefusal([]string{"core.sshcommand"}):                               true,
		gitexec.CommandConfigRefusal([]string{"a", "b", "c", "d", "e", "f"}):                    true,
		gitexec.MergeDriverRefusal("rebase", []string{mergeDriverKey}):                          true,
		gitexec.MergeDriverRefusal("cherry-pick", []string{"merge.a.driver", "merge.b.driver"}): true,
		gitexec.CommandConfigIncompleteRefusal("git config --local timed out after 15s"):        false,
		"fatal: not a git repository (or any of the parent directories): .git":                  false,
		"git refused: something else entirely":                                                  false,
		"":                                                                                      false,
	}
	for text, want := range cases {
		if got := gitexec.IsCommandConfigRefusal(text); got != want {
			t.Errorf("IsCommandConfigRefusal(%q) = %v, want %v", text, got, want)
		}
	}
}

// TestStateKinds pins the one value State reports for a root: a work tree with its top level, no
// repository, refused, incomplete or unknown — with Err set for exactly the kinds that are not an
// answer about the repository's shape.
func TestStateKinds(t *testing.T) {
	t.Parallel()
	refusedNames := []string{"filter.x.clean"}

	// repository builds a real repository and returns its root as git spells it.
	repository := func(t *testing.T, gitPath string) string {
		t.Helper()
		root, err := realpath.Resolve(t.TempDir())
		if err != nil {
			t.Fatalf("resolve the temp dir: %v", err)
		}
		runRealGit(t, gitPath, root, "init", "-b", "main")
		return root
	}
	// live is a fixture on the host's git; scripted is one whose git never runs.
	type fixture struct {
		host     gitexec.Host
		gitPath  string
		root     string
		topLevel string
		ctx      context.Context
	}
	live := func(root, topLevel string, gitPath string) fixture {
		return fixture{host: gitexec.OS(), gitPath: gitPath, root: root, topLevel: topLevel, ctx: context.Background()}
	}
	scripted := func(t *testing.T, answer gitAnswer) fixture {
		t.Helper()
		gitPath := outsideGit(t)
		host, _ := fakeHost(gitPath, answer)
		return fixture{host: host, gitPath: gitPath, root: t.TempDir(), ctx: context.Background()}
	}

	cases := []struct {
		name    string
		fixture func(t *testing.T) fixture
		want    gitexec.RepoStateKind
		check   func(t *testing.T, state gitexec.RepoState)
	}{
		{
			name: "repository root",
			fixture: func(t *testing.T) fixture {
				gitPath := realGit(t)
				root := repository(t, gitPath)
				return live(root, root, gitPath)
			},
			want: gitexec.WorkTree,
		},
		{
			name: "subdirectory of a repository",
			fixture: func(t *testing.T) fixture {
				gitPath := realGit(t)
				root := repository(t, gitPath)
				sub := filepath.Join(root, "a", "b")
				if err := os.MkdirAll(sub, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				return live(sub, root, gitPath)
			},
			want: gitexec.WorkTree,
		},
		{
			name: "plain directory",
			fixture: func(t *testing.T) fixture {
				return live(t.TempDir(), "", realGit(t))
			},
			want: gitexec.NoRepository,
		},
		{
			name: "bare repository",
			fixture: func(t *testing.T) fixture {
				gitPath := realGit(t)
				root := t.TempDir()
				runRealGit(t, gitPath, root, "init", "--bare", ".")
				return live(root, "", gitPath)
			},
			want: gitexec.NoRepository,
		},
		{
			name: "exit 0 outside a foreign work tree",
			fixture: func(t *testing.T) fixture {
				return scripted(t, cleanRepository(func(string) subprocess.SubprocessResult {
					return subprocess.SubprocessResult{Stdout: "false\n/elsewhere/tree\n"}
				}))
			},
			want: gitexec.NoRepository,
		},
		{
			name: "refused command-config key",
			fixture: func(t *testing.T) fixture {
				gitPath := realGit(t)
				root := repository(t, gitPath)
				appendToConfig(t, root, "[filter \"x\"]\n\tclean = true\n")
				return live(root, "", gitPath)
			},
			want: gitexec.Refused,
			check: func(t *testing.T, state gitexec.RepoState) {
				var refused gitexec.RefusedError
				want := gitexec.CommandConfigRefusal(refusedNames)
				if state.Refusal != want || !errors.As(state.Err, &refused) || refused.Sentence != want {
					t.Errorf("Refusal = %q, Err = %v (%T), want %q carried by a RefusedError", state.Refusal, state.Err, state.Err, want)
				}
			},
		},
		{
			name: "incomplete command-config probe",
			fixture: func(t *testing.T) fixture {
				return scripted(t, func(argv string) subprocess.SubprocessResult {
					if strings.Contains(argv, " config --local ") {
						return subprocess.SubprocessResult{TimedOut: true, ExitCode: -1}
					}
					return cleanRepository(func(string) subprocess.SubprocessResult {
						return subprocess.SubprocessResult{Stdout: "true\n/never/read\n"}
					})(argv)
				})
			},
			want: gitexec.Incomplete,
			check: func(t *testing.T, state gitexec.RepoState) {
				if !errors.Is(state.Err, gitexec.ErrIncomplete) || !isIncompleteRefusal(state.Err.Error(), "git config --local timed out") {
					t.Errorf("Err = %v, want the incomplete-probe sentence matching ErrIncomplete", state.Err)
				}
			},
		},
		{
			name: "cancelled context",
			fixture: func(t *testing.T) fixture {
				gitPath := realGit(t)
				root := repository(t, gitPath)
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				f := live(root, "", gitPath)
				f.ctx = ctx
				return f
			},
			want: gitexec.Unknown,
			check: func(t *testing.T, state gitexec.RepoState) {
				if !errors.Is(state.Err, context.Canceled) {
					t.Errorf("Err = %v, want the context's cancellation", state.Err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := tc.fixture(t)

			state := f.host.State(f.ctx, f.gitPath, f.root, nil, testTimeout)

			if state.Kind != tc.want {
				t.Fatalf("Kind = %v (Err %v), want %v", state.Kind, state.Err, tc.want)
			}
			if state.TopLevel != f.topLevel {
				t.Errorf("TopLevel = %q, want %q", state.TopLevel, f.topLevel)
			}
			wantsErr := tc.want != gitexec.WorkTree && tc.want != gitexec.NoRepository
			if (state.Err != nil) != wantsErr {
				t.Errorf("Err = %v for %v, want set = %t", state.Err, tc.want, wantsErr)
			}
			if tc.want != gitexec.Refused && state.Refusal != "" {
				t.Errorf("Refusal = %q for %v, want none", state.Refusal, tc.want)
			}
			if tc.check != nil {
				tc.check(t, state)
			}
		})
	}
}

// TestStateAt_ReportsAnUnresolvableGitAsUnavailable pins the resolving entry point: a git that
// cannot be resolved is Unavailable with the Resolve failure in Err and nothing launched, and a
// git that resolves is State's own answer.
func TestStateAt_ReportsAnUnresolvableGitAsUnavailable(t *testing.T) {
	t.Parallel()
	workTree := cleanRepository(func(string) subprocess.SubprocessResult {
		return subprocess.SubprocessResult{Stdout: "true\n/some/tree\n"}
	})

	t.Run("absent git", func(t *testing.T) {
		t.Parallel()
		host, spawned := fakeHost("", workTree)
		host.Look = func(string) (string, error) { return "", exec.ErrNotFound }

		state := host.StateAt(context.Background(), t.TempDir(), nil, testTimeout)

		if state.Kind != gitexec.Unavailable || state.Err == nil || state.Err.Error() != gitexec.UnavailableMessage || len(*spawned) != 0 {
			t.Errorf("StateAt = %+v after %d launches, want Unavailable with %q and none", state, len(*spawned), gitexec.UnavailableMessage)
		}
	})

	t.Run("fenced git", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		host, spawned := fakeHost(filepath.Join(root, "git"), workTree)

		state := host.StateAt(context.Background(), root, nil, testTimeout)

		if state.Kind != gitexec.Unavailable || !errors.Is(state.Err, security.ErrExecFromWritablePath) || len(*spawned) != 0 {
			t.Errorf("StateAt = %+v after %d launches, want Unavailable with the exec-fence refusal and none", state, len(*spawned))
		}
	})

	t.Run("resolved git", func(t *testing.T) {
		t.Parallel()
		host, _ := fakeHost(outsideGit(t), workTree)

		state := host.StateAt(context.Background(), t.TempDir(), nil, testTimeout)

		want := gitexec.RepoState{Kind: gitexec.WorkTree, TopLevel: filepath.FromSlash("/some/tree")}
		if state != want {
			t.Errorf("StateAt = %+v, want %+v", state, want)
		}
	})
}

// TestRepoState_ZeroValueIsUnknown pins that a RepoState nobody filled in is no answer, and that
// every kind names itself for test output.
func TestRepoState_ZeroValueIsUnknown(t *testing.T) {
	t.Parallel()
	var zero gitexec.RepoState
	if zero.Kind != gitexec.Unknown {
		t.Errorf("zero RepoState kind = %v, want Unknown", zero.Kind)
	}
	names := map[gitexec.RepoStateKind]string{
		gitexec.Unknown: "Unknown", gitexec.WorkTree: "WorkTree", gitexec.NoRepository: "NoRepository",
		gitexec.Refused: "Refused", gitexec.Incomplete: "Incomplete", gitexec.Unavailable: "Unavailable",
	}
	for kind, want := range names {
		if got := kind.String(); got != want {
			t.Errorf("RepoStateKind(%d).String() = %q, want %q", int(kind), got, want)
		}
	}
}

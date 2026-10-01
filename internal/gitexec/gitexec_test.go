package gitexec_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/gitexec"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/subprocess"
)

// testTimeout bounds every git invocation these tests make; the runner's own ceilings are not
// what is under test here.
const testTimeout = 15 * time.Second

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

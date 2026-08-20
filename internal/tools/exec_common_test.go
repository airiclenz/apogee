package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestRunSubprocessNilConfinerFailsClosed pins the §2.2 posture on the one handle shape the
// confine guard used to wave through: a Confinement installed with no Confiner behind it. That
// is broken wiring, not permission to run free — it must surface as ErrConfinementUnavailable,
// which dispatch turns into the truthful demote to Approval, and the command must never run.
func TestRunSubprocessNilConfinerFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the guard it pins is platform-independent")
	}
	t.Parallel()

	// The canary is a file the command would create: its absence is the proof that nothing
	// ran, which an error alone cannot give.
	canary := filepath.Join(t.TempDir(), "ran")
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: nil,
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})

	_, err := runSubprocess(ctx, subprocessSpec{
		argv: []string{"/bin/sh", "-c", fmt.Sprintf("touch %s", strconv.Quote(canary))},
	})
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Fatalf("runSubprocess err = %v, want ErrConfinementUnavailable (a handle with no Confiner must fail closed)", err)
	}
	if _, statErr := os.Stat(canary); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("stat %s = %v, want not-exist — the command must not have run unconfined", canary, statErr)
	}
}

// TestRunSubprocessReapsTheProcessGroupOnACleanExit pins the half of the §2.4 teardown that
// cmd.Cancel cannot reach: a command that BACKGROUNDS something and then exits normally. Nothing
// is cancelled here, so before the post-Wait reap the descendant simply outlived the call — a
// persistence primitive the tool still reported as a success. The one-shot contract (ADR 0008)
// says the tree goes when the call does.
func TestRunSubprocessReapsTheProcessGroupOnACleanExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process groups; the Windows job-object twin lives in terminal_windows_test.go")
	}
	t.Parallel()

	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	// The shell backgrounds a long sleep, records ITS pid, and exits — no `wait`, so the run
	// completes cleanly in milliseconds. The redirection keeps the sleep off the captured
	// pipes, so Wait returns as soon as the shell does: this test is about the reap, not the
	// drain (TestRunSubprocessReportsAWedgedDrain owns that).
	script := fmt.Sprintf(`sleep 300 >/dev/null 2>&1 & echo $! > %s`, strconv.Quote(pidFile))

	res, err := runSubprocess(context.Background(), subprocessSpec{argv: []string{"/bin/sh", "-c", script}})
	if err != nil {
		t.Fatalf("runSubprocess err = %v, want nil", err)
	}
	if res.exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0 — the shell itself exited cleanly (output %q)", res.exitCode, res.combinedOutput)
	}

	pid := waitForPIDFile(t, pidFile)
	// Whatever the assertion finds, the sleep must not leak into the machine.
	t.Cleanup(func() { killPID(pid) })

	if pidAlive(pid, 3*time.Second) {
		t.Errorf("backgrounded PID %d survived a CLEAN exit; the process group was reaped only on cancellation", pid)
	}
}

// TestRunSubprocessReportsAWedgedDrain pins the second half of the same finding: when something
// the command left running still holds the output pipe, exec cuts the drain off at
// processWaitDelay and returns exec.ErrWaitDelay — which is not an *exec.ExitError, so the exit
// code falls through to the leader's own status. The leader exited 0, so the call used to render
// as a green tick with a silently truncated tail.
func TestRunSubprocessReportsAWedgedDrain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell; the exit-code mapping it pins is platform-independent")
	}
	// processWaitDelay is a package var, so this test cannot run in parallel; shrinking it is
	// what keeps a five-second drain out of the suite.
	prev := processWaitDelay
	processWaitDelay = 250 * time.Millisecond
	t.Cleanup(func() { processWaitDelay = prev })

	// The sleep INHERITS the captured pipes and outlives the shell, so the output copy cannot
	// finish: Wait blocks until the delay expires. The sleep is short enough that a failed
	// reap cannot leave a process around for long.
	res, err := runSubprocess(context.Background(), subprocessSpec{argv: []string{"/bin/sh", "-c", `sleep 10 &`}})
	if err != nil {
		t.Fatalf("runSubprocess err = %v, want nil (a wedged drain is a result, not a Go error)", err)
	}
	if !res.drainWedged {
		t.Fatalf("drainWedged = false, want true — the pipe was still held when the delay expired (exit code %d)", res.exitCode)
	}
	if res.exitCode == 0 {
		t.Errorf("exitCode = 0 for a run whose descendants held the pipe and were killed; the operator would read that as a clean success")
	}
}

// TestSubprocessEnvDropsTheConfiguredSecretNames pins the caller-named half of the credential
// scrub: a variable the host named — in whatever case the operator's config spells it — never
// reaches the child, while the rest of the operator's environment still does. It is the mechanism
// that keeps an `api-key-env:` credential (ADR 0047) out of a subprocess the model steers.
func TestSubprocessEnvDropsTheConfiguredSecretNames(t *testing.T) {
	// Not parallel: t.Setenv.
	t.Setenv("APOGEE_TEST_PROVIDER_KEY", "sk-configured-value")
	t.Setenv("APOGEE_TEST_ENDPOINT", "http://192.0.2.1:1111")

	// The configured spelling differs in case from the exported one: the two are one variable on
	// Windows, and dropping both spellings is the safe direction everywhere else.
	env := subprocessEnv([]string{"apogee_test_provider_key", "  ", ""})

	if value, ok := envValue(env, "APOGEE_TEST_PROVIDER_KEY"); ok {
		t.Errorf("APOGEE_TEST_PROVIDER_KEY = %q reached the child environment, want it dropped", value)
	}
	for _, entry := range env {
		if strings.Contains(entry, "sk-configured-value") {
			t.Errorf("the configured key survived under another name: %q", entry)
		}
	}
	if value, _ := envValue(env, "APOGEE_TEST_ENDPOINT"); value != "http://192.0.2.1:1111" {
		t.Errorf("APOGEE_TEST_ENDPOINT = %q, want it inherited (only the NAMED variables are dropped)", value)
	}
	if _, ok := envValue(env, "PATH"); !ok {
		t.Error("PATH did not survive: the tools run in the operator's environment, not an allowlist")
	}
}

// TestSubprocessEnvDropsApogeeCredentialsWithNoConfiguredNames pins the floor the extensible scrub
// must not move: with nothing configured, the environment is exactly what it was before a host
// could name anything — apogee's own key gone, everything else inherited.
func TestSubprocessEnvDropsApogeeCredentialsWithNoConfiguredNames(t *testing.T) {
	// Not parallel: t.Setenv.
	t.Setenv("APOGEE_API_KEY", "sk-secret-value")
	t.Setenv("APOGEE_TEST_ENDPOINT", "http://192.0.2.1:1111")

	for _, configured := range [][]string{nil, {}} {
		env := subprocessEnv(configured)
		if value, ok := envValue(env, "APOGEE_API_KEY"); ok {
			t.Errorf("configured=%v: APOGEE_API_KEY = %q reached the child, want apogee's own key always dropped", configured, value)
		}
		if value, _ := envValue(env, "APOGEE_TEST_ENDPOINT"); value != "http://192.0.2.1:1111" {
			t.Errorf("configured=%v: APOGEE_TEST_ENDPOINT = %q, want it inherited", configured, value)
		}
	}
}

// TestSubprocessEnvAppendsExtrasAfterTheScrub pins that the extras a tool adds are not themselves
// filtered by the configured names — they are apogee's own additions (PYTHONSAFEPATH and friends),
// appended last so they win over an inherited spelling, and the scrub is a subtraction from the
// INHERITED half alone.
func TestSubprocessEnvAppendsExtrasAfterTheScrub(t *testing.T) {
	// Not parallel: t.Setenv.
	t.Setenv("APOGEE_TEST_PROVIDER_KEY", "sk-configured-value")

	env := subprocessEnv([]string{"APOGEE_TEST_PROVIDER_KEY"}, "PYTHONSAFEPATH=1")
	if value, ok := envValue(env, "PYTHONSAFEPATH"); !ok || value != "1" {
		t.Errorf("PYTHONSAFEPATH = %q (present=%v), want the tool's own extra appended", value, ok)
	}
	if got := env[len(env)-1]; got != "PYTHONSAFEPATH=1" {
		t.Errorf("last entry = %q, want the extra last so it wins over an inherited spelling", got)
	}
}

// TestRunHookSubprocessScrubsApogeeCredentials pins the reason the exported door exists at all: a
// hook that spawns must not hand apogee's own key to the child. The check is made from INSIDE the
// process — what the child can actually read — rather than off the spec, and the control variable
// proves the scrub is a subtraction rather than an empty environment.
func TestRunHookSubprocessScrubsApogeeCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the scrub it pins is platform-independent")
	}
	// Not parallel: t.Setenv.
	t.Setenv("APOGEE_API_KEY", "sk-secret-value")
	t.Setenv("APOGEE_TEST_ENDPOINT", "http://192.0.2.1:1111")

	out, err := RunHookSubprocess(
		context.Background(),
		[]string{"/bin/sh", "-c", `printf 'key=[%s] endpoint=[%s]' "$APOGEE_API_KEY" "$APOGEE_TEST_ENDPOINT"`},
		"",
		30*time.Second,
		"",
	)
	if err != nil {
		t.Fatalf("RunHookSubprocess: %v", err)
	}
	if want := "key=[] endpoint=[http://192.0.2.1:1111]"; out != want {
		t.Errorf("child saw %q, want %q — apogee's key must not reach a hook's subprocess", out, want)
	}
}

// TestRunHookSubprocessReturnsStdoutAloneAndFeedsStdin pins the payload contract the execution
// tools do NOT have: the caller reads the child's stdout as data, so a diagnostic must never be
// spliced into it. Interleaving here would put the warning line into the file autofix writes back.
func TestRunHookSubprocessReturnsStdoutAloneAndFeedsStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the stream split it pins is platform-independent")
	}
	t.Parallel()

	out, err := RunHookSubprocess(
		context.Background(),
		[]string{"/bin/sh", "-c", `echo "warning: noisy formatter" >&2; cat`},
		"",
		30*time.Second,
		"payload\n",
	)
	if err != nil {
		t.Fatalf("RunHookSubprocess: %v", err)
	}
	if out != "payload\n" {
		t.Errorf("stdout = %q, want the stdin payload alone — stderr must not be interleaved into it", out)
	}
}

// TestRunHookSubprocessFailsOnANonZeroExit pins that a caller reading stdout as data gets ONE
// failure signal: a clean non-zero exit is a normal result to an execution tool (the model reads
// the code) but is "no usable output" here, and the diagnostics say which command complained.
func TestRunHookSubprocessFailsOnANonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the exit handling it pins is platform-independent")
	}
	t.Parallel()

	out, err := RunHookSubprocess(
		context.Background(),
		[]string{"/bin/sh", "-c", `echo "cannot parse input" >&2; exit 3`},
		"",
		30*time.Second,
		"",
	)
	if err == nil {
		t.Fatalf("RunHookSubprocess err = nil, want a non-zero exit reported as an error (out = %q)", out)
	}
	if out != "" {
		t.Errorf("stdout = %q, want it empty on a failed run", out)
	}
	if !strings.Contains(err.Error(), "cannot parse input") {
		t.Errorf("err = %v, want the command's diagnostics quoted", err)
	}
}

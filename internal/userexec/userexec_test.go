package userexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/security"
)

// requireShell skips a test that scripts its command with `sh`. Every assertion in this file is
// about what apogee does with a child process, not about the child itself, so a host with no POSIX
// shell simply has nothing here to prove.
func requireShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("these tests script the command with sh; Windows has no POSIX shell to script it with")
	}
}

// shell is the argv that runs one script under sh.
func shell(script string) []string {
	return []string{"sh", "-c", script}
}

// TestRunReportsTheExitStatusAndTheTailOfWhatTheCommandSaid is the shape every caller reads: a
// status, and the complaint folded onto one line — facts, never a sentence.
func TestRunReportsTheExitStatusAndTheTailOfWhatTheCommandSaid(t *testing.T) {
	requireShell(t)
	t.Parallel()

	result, err := Run(context.Background(), shell(`echo "no such"; echo "  recipient" >&2; exit 3`), Options{WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("Run on a command that exited 3 returned an error: %v", err)
	}
	if result.ExitCode != 3 || result.TimedOut {
		t.Errorf("result = %+v, want exit 3 and no timeout", result)
	}
	if result.StderrTail != "recipient" {
		t.Errorf("StderrTail = %q, want the folded complaint", result.StderrTail)
	}
	if result.Stdout != "" || result.StdoutTruncated {
		t.Errorf("stdout was kept without WantStdout: %+v", result)
	}
}

// TestRunKeepsStdoutOnlyWhenWantedAndUpToTheCap is the two-sided stdout contract: discarded by
// default, and when kept, bounded with the overflow reported rather than silently cut.
func TestRunKeepsStdoutOnlyWhenWantedAndUpToTheCap(t *testing.T) {
	requireShell(t)
	t.Parallel()

	for _, tc := range []struct {
		name          string
		script        string
		opts          Options
		wantStdout    string
		wantTruncated bool
	}{
		{name: "not wanted", script: "echo secret", opts: Options{}},
		{name: "wanted and within the cap", script: "printf sk-key", opts: Options{WantStdout: true, StdoutCap: 64}, wantStdout: "sk-key"},
		{name: "wanted and past the cap", script: "printf 0123456789", opts: Options{WantStdout: true, StdoutCap: 4}, wantStdout: "0123", wantTruncated: true},
		{name: "wanted with a zero cap", script: "printf x", opts: Options{WantStdout: true}, wantTruncated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := Run(context.Background(), shell(tc.script), tc.opts)
			if err != nil || result.ExitCode != 0 {
				t.Fatalf("Run = (%+v, %v), want a clean exit", result, err)
			}
			if result.Stdout != tc.wantStdout || result.StdoutTruncated != tc.wantTruncated {
				t.Errorf("stdout = (%q, truncated %t), want (%q, %t)", result.Stdout, result.StdoutTruncated, tc.wantStdout, tc.wantTruncated)
			}
		})
	}
}

// TestRunFeedsStdinAndAppendsTheEnvironmentLast is the child's two inputs: the reader it is handed,
// and an environment where the caller's variables beat inherited ones of the same name.
func TestRunFeedsStdinAndAppendsTheEnvironmentLast(t *testing.T) {
	requireShell(t)
	t.Setenv("APOGEE_USEREXEC_TEST", "inherited")

	result, err := Run(context.Background(), shell(`cat; printf " %s" "$APOGEE_USEREXEC_TEST"`), Options{
		Stdin:      strings.NewReader("payload"),
		WantStdout: true,
		StdoutCap:  64,
		Env:        []string{"APOGEE_USEREXEC_TEST=appended"},
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("Run = (%+v, %v), want a clean exit", result, err)
	}
	if result.Stdout != "payload appended" {
		t.Errorf("stdout = %q, want the stdin echoed and the appended variable winning", result.Stdout)
	}
}

// TestRunReportsTheDeadlineRatherThanWaitingOnASleep is the bound that keeps one wedged command
// from holding its caller. A command that IS the sleep dies with the deadline and nothing else is
// owed. A wrapper-shaped one — a shell that spawned the sleep — leaves a grandchild holding the
// stderr pipe it inherited, and the copy behind that pipe would block forever; WaitGrace is what
// bounds it, so that case must finish soon after the grace and never later. Both the caller's own
// context deadline and Options.Timeout are the deadline.
func TestRunReportsTheDeadlineRatherThanWaitingOnASleep(t *testing.T) {
	requireShell(t)
	t.Parallel()

	const deadline = 200 * time.Millisecond
	for _, tc := range []struct {
		name   string
		script string
		bound  func(context.Context) (context.Context, context.CancelFunc, Options)
		within time.Duration
	}{
		{
			name:   "the command itself, under the context's deadline",
			script: "exec sleep 5",
			bound: func(ctx context.Context) (context.Context, context.CancelFunc, Options) {
				ctx, cancel := context.WithTimeout(ctx, deadline)
				return ctx, cancel, Options{}
			},
			within: time.Second,
		},
		{
			name:   "a wrapper's grandchild, under Options.Timeout",
			script: "sleep 5",
			bound: func(ctx context.Context) (context.Context, context.CancelFunc, Options) {
				return ctx, func() {}, Options{Timeout: deadline}
			},
			within: WaitGrace + time.Second,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel, opts := tc.bound(context.Background())
			defer cancel()

			started := time.Now()
			result, err := Run(ctx, shell(tc.script), opts)
			elapsed := time.Since(started)

			if err != nil {
				t.Fatalf("Run past the deadline returned an error rather than a timed-out result: %v", err)
			}
			if !result.TimedOut {
				t.Errorf("result = %+v, want TimedOut", result)
			}
			if elapsed > tc.within {
				t.Errorf("Run took %s to give up on a %s deadline; the bound here is %s", elapsed, deadline, tc.within)
			}
		})
	}
}

// TestRunCapsStderrWithoutKillingTheCommand is the SIGPIPE guard: a command printing far past
// MaxStderr still exits with ITS status, and what survives is the folded tail, not a signal.
func TestRunCapsStderrWithoutKillingTheCommand(t *testing.T) {
	requireShell(t)
	t.Parallel()

	result, err := Run(context.Background(), shell(`i=0; while [ $i -lt 600 ]; do echo "0123456789abcdefghij" >&2; i=$((i+1)); done; exit 7`), Options{})
	if err != nil {
		t.Fatalf("Run on a chatty command returned an error: %v", err)
	}
	if result.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want the command's own 7 — a capped stderr must not turn into SIGPIPE", result.ExitCode)
	}
	if !strings.HasSuffix(result.StderrTail, "…") {
		t.Errorf("StderrTail = %q, want the truncated tail to end in an ellipsis", result.StderrTail)
	}
	if runes := []rune(result.StderrTail); len(runes) > StderrTailRunes+1 {
		t.Errorf("tail is %d runes long; it is capped at %d", len(runes), StderrTailRunes)
	}
}

// TestRunRefusesAProgramInsideTheWorkspace is the exec fence: the model can write files in the
// workspace, so running one of them would turn a file write into arbitrary code execution on the
// user's machine. An empty root fences nothing, for the Drivers that have no workspace to name.
func TestRunRefusesAProgramInsideTheWorkspace(t *testing.T) {
	requireShell(t)
	t.Parallel()

	workspace := t.TempDir()
	marker := filepath.Join(workspace, "it-ran")
	program := filepath.Join(workspace, "hook.sh")
	if err := os.WriteFile(program, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatalf("write the program: %v", err)
	}

	_, err := Run(context.Background(), []string{program}, Options{WorkspaceRoot: workspace})
	if !errors.Is(err, security.ErrExecFromWritablePath) {
		t.Fatalf("Run of a program inside the workspace = %v, want a refusal wrapping ErrExecFromWritablePath", err)
	}
	if !strings.Contains(err.Error(), "refusing to run") {
		t.Errorf("error = %q, want a refusal naming the program", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("the refused program ran anyway: %v", statErr)
	}

	result, err := Run(context.Background(), []string{program}, Options{})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("Run with no root = (%+v, %v), want the program to run — an empty fence refuses nothing", result, err)
	}
}

// TestRunRefusesWhatItCannotRunBeforeAnythingRuns is the other two sentences a command never
// reaching a status earns: an empty argv, and a program that is not on PATH.
func TestRunRefusesWhatItCannotRunBeforeAnythingRuns(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		argv []string
		want string
	}{
		{name: "no argv", argv: nil, want: "no command to run"},
		{name: "a blank program", argv: []string{"  "}, want: "no command to run"},
		{name: "not on PATH", argv: []string{"apogee-no-such-user-command"}, want: `"apogee-no-such-user-command" is not on this machine's PATH`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Run(context.Background(), tc.argv, Options{})
			if err == nil || err.Error() != tc.want {
				t.Errorf("Run = %v, want %q", err, tc.want)
			}
		})
	}
}

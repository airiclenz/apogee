package reactions

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// requireShell skips a test that scripts its Hook with `sh`. Every assertion in this file is about
// what apogee does with a child process, not about the child itself, so a host with no POSIX shell
// simply has nothing here to prove.
func requireShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("these tests script the Hook with sh; Windows has no POSIX shell to script it with")
	}
}

// shellHook builds a command Hook that runs one shell script under a generous timeout.
func shellHook(name, script string) Hook {
	return Hook{
		Name:    name,
		Events:  []Event{TurnFinished},
		Command: []string{"sh", "-c", script},
		Timeout: 10 * time.Second,
	}
}

// TestCommandExecutorFeedsThePayloadOnStdinAndTheHookFactsInTheEnvironment is the round trip the
// command half exists for: the script gets the whole JSON document on stdin, byte for byte, and
// the APOGEE_HOOK_* convenience facts in its environment — which is the contract a user's script
// is written against.
func TestCommandExecutorFeedsThePayloadOnStdinAndTheHookFactsInTheEnvironment(t *testing.T) {
	requireShell(t)

	dir := t.TempDir()
	stdinFile := filepath.Join(dir, "stdin.json")
	envFile := filepath.Join(dir, "env.txt")
	t.Setenv("APOGEE_HOOK_TEST_STDIN_OUT", stdinFile)
	t.Setenv("APOGEE_HOOK_TEST_ENV_OUT", envFile)

	payload := Payload{
		Event:     FileChanged,
		Hook:      "notify",
		Time:      "2026-09-06T12:00:00Z",
		Workspace: "/work/space",
		Tool:      "write_file",
		Path:      "/work/space/main.go",
		Schedule:  &ScheduleRef{ID: "sched-1", Name: "docs sweep"},
	}
	hook := shellHook("notify", `cat > "$APOGEE_HOOK_TEST_STDIN_OUT"; env > "$APOGEE_HOOK_TEST_ENV_OUT"`)

	// DefaultExecutor rather than commandExecutor directly, so the dispatch on `command:` is
	// exercised by the same test that proves what the command receives.
	if err := DefaultExecutor(dir).Run(context.Background(), hook, payload); err != nil {
		t.Fatalf("Run: %v", err)
	}

	wantBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal the payload: %v", err)
	}
	gotBody := readFile(t, stdinFile)
	if gotBody != string(wantBody) {
		t.Errorf("stdin =\n%s\nwant\n%s", gotBody, wantBody)
	}

	environment := readFile(t, envFile)
	for _, want := range []string{
		EnvEvent + "=file-changed",
		EnvName + "=notify",
		EnvWorkspace + "=/work/space",
		EnvPath + "=/work/space/main.go",
		EnvScheduleID + "=sched-1",
		EnvScheduleName + "=docs sweep",
	} {
		if !strings.Contains(environment, want) {
			t.Errorf("the command's environment is missing %q; it held:\n%s", want, environment)
		}
	}
}

// TestCommandExecutorReportsTheExitStatusAndWhatTheCommandSaid proves the failure line carries
// both halves of what a user needs to fix a broken Hook: the status it died with, and the
// complaint it printed on the way.
func TestCommandExecutorReportsTheExitStatusAndWhatTheCommandSaid(t *testing.T) {
	requireShell(t)

	hook := shellHook("notify", `echo "no such recipient" >&2; exit 3`)

	err := commandExecutor{workspaceRoot: t.TempDir()}.Run(context.Background(), hook, Payload{Event: TurnFinished})
	if err == nil {
		t.Fatal("Run on a command that exited 3 returned no error")
	}
	if got := err.Error(); got != "exit 3: no such recipient" {
		t.Errorf("error = %q, want %q", got, "exit 3: no such recipient")
	}
}

// TestCommandExecutorReportsTheDeadlineRatherThanWaitingOnASleep is the bound that keeps one
// wedged script from holding a Hook's worker — and, at shutdown, the whole grace period.
//
// The two cases are the two shapes a hung Hook takes. A command that IS the sleep dies with the
// deadline and nothing else is owed. A wrapper-shaped one — a shell that spawned the sleep — leaves
// a grandchild holding the stderr pipe it inherited, and the copy behind that pipe would block
// forever; waitGrace is what bounds it, so this case must finish soon after the grace and never
// later (keyresolve.go and keystore/run.go carry the same guard for the same reason).
func TestCommandExecutorReportsTheDeadlineRatherThanWaitingOnASleep(t *testing.T) {
	requireShell(t)

	for _, tc := range []struct {
		name   string
		script string
		within time.Duration
	}{
		{name: "the command itself", script: "exec sleep 5", within: time.Second},
		{name: "a wrapper's grandchild", script: "sleep 5", within: waitGrace + time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hook := shellHook("notify", tc.script)
			hook.Timeout = 200 * time.Millisecond

			ctx, cancel := context.WithTimeout(context.Background(), hook.Timeout)
			defer cancel()

			started := time.Now()
			err := commandExecutor{workspaceRoot: t.TempDir()}.Run(ctx, hook, Payload{Event: TurnFinished})
			elapsed := time.Since(started)

			if err == nil {
				t.Fatal("Run past the deadline returned no error")
			}
			if !strings.Contains(err.Error(), "timed out after 200ms") {
				t.Errorf("error = %q, want it to name the deadline", err)
			}
			if elapsed > tc.within {
				t.Errorf("Run took %s to give up on a 200ms deadline; the bound here is %s", elapsed, tc.within)
			}
		})
	}
}

// TestCommandExecutorRefusesAProgramInsideTheWorkspace is the exec fence: the model can write
// files in the workspace, so a Hook that ran one of them would turn a file write into arbitrary
// code execution on the user's machine.
func TestCommandExecutorRefusesAProgramInsideTheWorkspace(t *testing.T) {
	workspace := t.TempDir()
	marker := filepath.Join(workspace, "it-ran")
	program := filepath.Join(workspace, "hook.sh")
	script := "#!/bin/sh\ntouch " + marker + "\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("write the program: %v", err)
	}

	hook := Hook{
		Name:    "planted",
		Events:  []Event{TurnFinished},
		Command: []string{program},
		Timeout: 10 * time.Second,
	}

	err := commandExecutor{workspaceRoot: workspace}.Run(context.Background(), hook, Payload{Event: TurnFinished})
	if err == nil {
		t.Fatal("Run of a program inside the workspace returned no error")
	}
	if !strings.Contains(err.Error(), "refusing to run") {
		t.Errorf("error = %q, want a refusal naming the program", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("the refused program ran anyway: %v", statErr)
	}
}

// TestCommandExecutorCutsAnOverlongComplaintDownToATail keeps a chatty script from pushing
// apogee's own words off the one row the failure line is read on.
func TestCommandExecutorCutsAnOverlongComplaintDownToATail(t *testing.T) {
	requireShell(t)

	hook := shellHook("notify", `i=0; while [ $i -lt 600 ]; do echo "0123456789abcdefghij" >&2; i=$((i+1)); done; exit 1`)

	err := commandExecutor{workspaceRoot: t.TempDir()}.Run(context.Background(), hook, Payload{Event: TurnFinished})
	if err == nil {
		t.Fatal("Run on a command that exited 1 returned no error")
	}
	message := err.Error()
	if !strings.HasPrefix(message, "exit 1: ") {
		t.Errorf("error = %q, want it to open with the exit status", message)
	}
	if !strings.HasSuffix(message, "…") {
		t.Errorf("error = %q, want the truncated tail to end in an ellipsis", message)
	}
	if runes := []rune(message); len(runes) > maxErrorStderr+len("exit 1: ")+1 {
		t.Errorf("error is %d runes long; the tail is capped at %d", len(runes), maxErrorStderr)
	}
}

// readFile reads a file the scripted Hook wrote.
func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

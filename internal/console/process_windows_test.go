//go:build windows

package console

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
)

// windowsProcessTestTimeout bounds every wait in this file: generous, because a loaded CI host can
// take seconds to start cmd.exe inside a pseudoconsole, and finite, so a wedged Console fails the
// test instead of hanging the suite.
const windowsProcessTestTimeout = 30 * time.Second

// TestProcessRunsCommandsTypedIntoTheConsoleOnWindows drives an interactive cmd.exe the way a
// model does: type a line, read what it printed — with the pseudoconsole's VT painting stripped.
func TestProcessRunsCommandsTypedIntoTheConsoleOnWindows(t *testing.T) {
	t.Parallel()
	process := startWindowsProcess(t, cmdSpec("cmd /q"))

	// %OS% expands only when cmd.exe runs the line, so the echo of the typed text cannot satisfy
	// the wait on its own.
	if _, err := process.Write([]byte("echo apogee-%OS%\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	output := readWindowsUntil(t, process, "apogee-Windows_NT")

	if strings.ContainsRune(output, '\x1b') {
		t.Errorf("output still carries escape sequences: %q", output)
	}
}

// TestProcessRecordsTheExitCodeOnWindows pins the exit code a Console reports once its command has
// ended on its own.
func TestProcessRecordsTheExitCodeOnWindows(t *testing.T) {
	t.Parallel()
	process := startWindowsProcess(t, cmdSpec("cmd /c exit 3"))

	awaitWindows(t, func() bool { return !process.Alive() }, "the process to exit")

	if code := process.ExitCode(); code != 3 {
		t.Errorf("ExitCode() = %d, want 3", code)
	}
}

// TestProcessCloseIsBoundedOnWindows closes a Console whose interactive cmd.exe would run forever:
// Close must release the pseudoconsole, end the process and return well inside closeJoinTimeout,
// with the exit recorded by the time it does.
func TestProcessCloseIsBoundedOnWindows(t *testing.T) {
	t.Parallel()
	process := startWindowsProcess(t, cmdSpec("cmd /q"))

	started := time.Now()
	if err := process.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	elapsed := time.Since(started)

	if elapsed >= closeJoinTimeout {
		t.Errorf("Close took %v, want well inside %v", elapsed, closeJoinTimeout)
	}
	if process.Alive() {
		t.Error("Alive() = true after Close")
	}
	if err := process.Close(); err != nil {
		t.Errorf("second Close: %v, want the first call's nil", err)
	}
}

// TestProcessStartRefusesAConfinedSpecOnWindows pins the fail-closed refusal: a confined Console
// cannot be honoured on Windows, so Start wraps ErrConfinementUnavailable before Prepare runs and
// before anything starts.
func TestProcessStartRefusesAConfinedSpecOnWindows(t *testing.T) {
	t.Parallel()
	prepared := false
	spec := cmdSpec("cmd /q")
	spec.Confined = true
	spec.Prepare = func(*exec.Cmd) error { prepared = true; return nil }

	process, err := Start(spec)

	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Fatalf("Start(confined) = %v, want ErrConfinementUnavailable", err)
	}
	if process != nil {
		t.Error("Start returned a Process for a refused spec")
	}
	if prepared {
		t.Error("Prepare ran for a confined spec Windows refuses")
	}
}

// TestProcessStartRefusesARestrictedTokenOnWindows covers the backstop: a Prepare hook that puts a
// token on the command is refused the same fail-closed way, since the launcher cannot honour it.
func TestProcessStartRefusesARestrictedTokenOnWindows(t *testing.T) {
	t.Parallel()
	spec := cmdSpec("cmd /q")
	spec.Prepare = func(cmd *exec.Cmd) error {
		cmd.SysProcAttr.Token = syscall.Token(1)
		return nil
	}

	if _, err := Start(spec); !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Fatalf("Start(token) = %v, want ErrConfinementUnavailable", err)
	}
}

// TestProcessPrepareSeesTheRawCommandLineOnWindows pins what the Prepare hook is handed: the
// command with its directory, environment and the caller's verbatim command line already set —
// the line the launcher then runs.
func TestProcessPrepareSeesTheRawCommandLineOnWindows(t *testing.T) {
	t.Parallel()
	errStop := errors.New("stop before launching")
	dir := t.TempDir()
	spec := cmdSpec(`cmd /c echo "quoted"`)
	spec.Dir = dir
	spec.Env = []string{"APOGEE_CONSOLE_TEST=1"}
	var seen *exec.Cmd
	spec.Prepare = func(cmd *exec.Cmd) error {
		seen = cmd
		return errStop
	}

	if _, err := Start(spec); !errors.Is(err, errStop) {
		t.Fatalf("Start = %v, want Prepare's own error", err)
	}

	if seen.SysProcAttr == nil || seen.SysProcAttr.CmdLine != spec.CommandLine {
		t.Errorf("Prepare saw command line %+v, want %q", seen.SysProcAttr, spec.CommandLine)
	}
	if seen.Dir != dir {
		t.Errorf("Prepare saw Dir %q, want %q", seen.Dir, dir)
	}
	if len(seen.Env) != 1 || seen.Env[0] != spec.Env[0] {
		t.Errorf("Prepare saw Env %q, want %q", seen.Env, spec.Env)
	}
}

// cmdSpec returns a Spec that runs commandLine through cmd.exe, verbatim, the way console_open
// hands one over.
func cmdSpec(commandLine string) Spec {
	return Spec{Argv: strings.Fields(commandLine), CommandLine: commandLine}
}

// startWindowsProcess starts spec, closing it when the test ends. It skips on a host without a
// pseudoconsole and fails on any other launch error.
func startWindowsProcess(t *testing.T, spec Spec) *Process {
	t.Helper()
	process, err := Start(spec)
	if errors.Is(err, platform.ErrPseudoConsoleUnavailable) {
		t.Skipf("no pseudoconsole on this host: %v", err)
	}
	if err != nil {
		t.Fatalf("Start(%q): %v", spec.CommandLine, err)
	}
	t.Cleanup(func() { _ = process.Close() })
	return process
}

// readWindowsUntil accumulates a Process's output until it carries want, and returns everything
// read. It fails the test rather than returning short.
func readWindowsUntil(t *testing.T, process *Process, want string) string {
	t.Helper()
	var seen strings.Builder
	deadline := time.Now().Add(windowsProcessTestTimeout)
	for time.Now().Before(deadline) {
		output, _ := process.Read(200 * time.Millisecond)
		seen.WriteString(output)
		if strings.Contains(seen.String(), want) {
			return seen.String()
		}
	}
	t.Fatalf("waited %v for %q; read %q", windowsProcessTestTimeout, want, seen.String())
	return ""
}

// awaitWindows polls condition until it holds, failing the test with what it was waiting for.
func awaitWindows(t *testing.T, condition func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(windowsProcessTestTimeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waited %v for %s", windowsProcessTestTimeout, what)
}

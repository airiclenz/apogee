//go:build windows

package platform

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/airiclenz/apogee/internal/testtime"
)

// The Windows twin of TestTeardownDoesNotReachASetsidEscapee (teardown_unix_test.go). POSIX has a
// documented escape from its teardown container — a descendant that calls setsid leaves the
// process group — and that test pins it. Windows claims to have none: the Job Object
// NewProcessTeardown builds does not permit breakaway, so a contained process that asks
// CreateProcess for CREATE_BREAKAWAY_FROM_JOB is refused with ERROR_ACCESS_DENIED
// (teardown_windows.go). This test shows the claim on the production os/exec path. The contained
// process is this test binary re-run with TestBreakawayHelper selected alone.

const (
	// breakawayHelperEnv makes the re-run binary the contained process. Unset, as in every
	// ordinary run, TestBreakawayHelper does nothing.
	breakawayHelperEnv = "APOGEE_BREAKAWAY_HELPER"

	// breakawayGo is the line the parent writes once the helper is contained. os/exec leaves a
	// window between CreateProcess and the job assignment, so the helper attempts nothing before
	// it reads this line.
	breakawayGo = "go"

	// breakawayResultPrefix starts the one line the helper reports its attempt on, followed by the
	// Win32 error code CreateProcess returned (ERROR_SUCCESS when the breakaway was allowed).
	breakawayResultPrefix = "breakaway: "
)

// breakawayTestTimeout bounds the whole exchange with the helper: a budget, since a loaded box
// starts a race-built test binary slowly. It is the run's context deadline, so when it expires
// cmd.Cancel tears the helper down, which ends every read from it and the final Wait.
var breakawayTestTimeout = testtime.Budget(60 * time.Second)

// TestBreakawayHelper is the contained half of TestTeardownJobRefusesBreakaway, run in a child
// process. It waits for the parent's go line, tries to start a process outside its job, and
// reports the Win32 error code the attempt returned.
func TestBreakawayHelper(t *testing.T) {
	if os.Getenv(breakawayHelperEnv) == "" {
		return
	}

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(line) != breakawayGo {
		t.Fatalf("helper: read %q (err %v) from stdin, want the %q line", line, err, breakawayGo)
	}
	code, err := tryBreakaway()
	if err != nil {
		t.Fatalf("helper: %v", err)
	}
	fmt.Printf("%s%d\n", breakawayResultPrefix, code)
}

// tryBreakaway asks CreateProcess to start cmd.exe outside the caller's job and returns the Win32
// error code it answered with. The process is created suspended, so if the breakaway is allowed it
// is terminated before it runs an instruction, and ERROR_SUCCESS is returned.
func tryBreakaway() (uint32, error) {
	commandLine, err := windows.UTF16PtrFromString("cmd.exe /c exit 0")
	if err != nil {
		return 0, fmt.Errorf("encode the command line: %w", err)
	}
	startup := windows.StartupInfo{}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	var process windows.ProcessInformation
	err = windows.CreateProcess(nil, commandLine, nil, nil, false,
		windows.CREATE_BREAKAWAY_FROM_JOB|windows.CREATE_SUSPENDED, nil, nil, &startup, &process)
	if err != nil {
		var errno syscall.Errno
		if !errors.As(err, &errno) {
			return 0, fmt.Errorf("CreateProcess returned a non-Win32 error: %w", err)
		}
		return uint32(errno), nil
	}
	_ = windows.TerminateProcess(process.Process, killedExitCode)
	_ = windows.CloseHandle(process.Thread)
	_ = windows.CloseHandle(process.Process)
	return uint32(windows.ERROR_SUCCESS), nil
}

// TestTeardownJobRefusesBreakaway pins the Windows side of the §2.4 teardown contract: a process
// contained through the production os/exec path (NewProcessTeardown, then Start and Contain as
// RunWithTeardown makes them) cannot start a descendant outside the job, so cancel and Reap reach
// every process the run spawned. A breakaway that succeeds, or fails for any reason other than the
// job's refusal, fails the test.
func TestTeardownJobRefusesBreakaway(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), breakawayTestTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, self, "-test.run=^TestBreakawayHelper$")
	cmd.Env = append(os.Environ(), breakawayHelperEnv+"=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("helper stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("helper stdout: %v", err)
	}

	td := NewProcessTeardown(cmd)
	if err := cmd.Start(); err != nil {
		td.Release()
		t.Fatalf("start the helper: %v", err)
	}
	td.Contain(cmd)
	// The teardown's own exit path, on every way out of this test: closing stdin ends a helper
	// still waiting for its go line, the context deadline bounds the Wait, and Reap and Release
	// end the job exactly as RunWithTeardown and its caller do.
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
		td.Reap(cmd)
		td.Release()
		if t.Failed() && stderr.Len() > 0 {
			t.Logf("helper stderr:\n%s", &stderr)
		}
	}()

	// Contain swallows every error, and a runner whose test process already sits in a job that
	// denies breakaway would refuse the helper's attempt too: without this check a teardown that
	// never assigned the helper would still pass.
	job, ok := td.(*jobTeardown)
	if !ok {
		t.Fatalf("NewProcessTeardown returned %T, want *jobTeardown", td)
	}
	job.mu.Lock()
	held := job.job != windows.InvalidHandle && job.assigned
	job.mu.Unlock()
	if !held {
		t.Fatal("the helper never joined the teardown job, so a refused breakaway would prove nothing about it")
	}

	if _, err := fmt.Fprintln(stdin, breakawayGo); err != nil {
		t.Fatalf("send the go line: %v", err)
	}
	code := readBreakawayResult(t, stdout)
	switch code {
	case uint32(windows.ERROR_ACCESS_DENIED):
	case uint32(windows.ERROR_SUCCESS):
		t.Fatal("CreateProcess with CREATE_BREAKAWAY_FROM_JOB succeeded inside the teardown job: a descendant can leave the job, so a cancel or Reap no longer reaches the whole tree")
	default:
		t.Fatalf("CreateProcess with CREATE_BREAKAWAY_FROM_JOB inside the teardown job = error %d (%v), want ERROR_ACCESS_DENIED (%d)",
			code, syscall.Errno(code), uint32(windows.ERROR_ACCESS_DENIED))
	}
}

// readBreakawayResult reads the helper's stdout up to its result line and returns the error code
// on it, failing the test with everything the helper printed when it exits without one. The read
// ends when the helper does, which breakawayTestTimeout's deadline guarantees.
func readBreakawayResult(t *testing.T, stdout io.Reader) uint32 {
	t.Helper()

	var seen []string
	lines := bufio.NewScanner(stdout)
	for lines.Scan() {
		rest, found := strings.CutPrefix(lines.Text(), breakawayResultPrefix)
		if !found {
			seen = append(seen, lines.Text())
			continue
		}
		code, err := strconv.ParseUint(rest, 10, 32)
		if err != nil {
			t.Fatalf("the helper's result line %q does not end in an error code: %v", lines.Text(), err)
		}
		return uint32(code)
	}
	t.Fatalf("the helper exited without reporting a result (read error %v), stdout:\n%s",
		lines.Err(), strings.Join(seen, "\n"))
	return 0
}

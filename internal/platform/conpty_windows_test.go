//go:build windows

package platform

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The pseudoconsole tests' bounds: generous, because a loaded CI host can take seconds to start
// cmd.exe, and finite, because a pseudoconsole that misbehaves must not hang the suite.
const (
	conptyTestTimeout = 30 * time.Second
	conptyTestPoll    = 20 * time.Millisecond
	conptyTestCols    = 80
	conptyTestRows    = 25
)

// TestStartPseudoConsoleRunsTheCommand starts a real cmd.exe inside a pseudoconsole and reads back
// what it printed and how it exited.
func TestStartPseudoConsoleRunsTheCommand(t *testing.T) {
	t.Parallel()

	const marker = "apogee-conpty-marker"
	pty := startTestPseudoConsole(t, "cmd /c echo "+marker)
	output := drainPseudoConsole(pty)

	code := waitWithin(t, pty)
	if err := pty.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got := output.wait(t)

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(got, marker) {
		t.Errorf("the pseudoconsole rendered %q, want it to contain %q", got, marker)
	}
}

// TestPseudoConsoleReleaseKeepsTheFinalFrame is the pseudoconsole-level regression guard for a
// short command's last frame: Release leaves the output open, the reader drains conhost's final
// flush to the end of output, and only then does CloseOutput give the read end back.
func TestPseudoConsoleReleaseKeepsTheFinalFrame(t *testing.T) {
	t.Parallel()

	const runs = 50
	for i := range runs {
		marker := fmt.Sprintf("apogee-conpty-final-frame-%d", i)
		pty := startTestPseudoConsole(t, "cmd /c echo "+marker)
		t.Cleanup(func() { _ = pty.Close() })
		output := drainPseudoConsole(pty)

		waitWithin(t, pty)
		if err := pty.Release(); err != nil {
			t.Fatalf("run %d: Release: %v", i, err)
		}
		got := output.wait(t)
		if err := pty.CloseOutput(); err != nil {
			t.Fatalf("run %d: CloseOutput: %v", i, err)
		}

		if !strings.Contains(got, marker) {
			t.Fatalf("run %d: the pseudoconsole rendered %q, want it to contain %q", i, got, marker)
		}
	}
}

// TestPseudoConsoleCloseClosesTheOutput pins that a Close-only caller is left holding no read
// handle: Close releases the output pipe along with everything else.
func TestPseudoConsoleCloseClosesTheOutput(t *testing.T) {
	t.Parallel()

	pty := startTestPseudoConsole(t, "cmd /c exit")
	output := drainPseudoConsole(pty)
	waitWithin(t, pty)

	if err := pty.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	output.wait(t)

	if _, err := pty.out.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Errorf("reading the output after Close = %v, want os.ErrClosed", err)
	}
}

// TestPseudoConsoleFailedLaunchClosesTheOutput pins that a launch that fails gives back both pipe
// ends: with no reader, there is no final frame to keep the output open for.
func TestPseudoConsoleFailedLaunchClosesTheOutput(t *testing.T) {
	t.Parallel()

	pty, err := newPseudoConsole(conptyTestCols, conptyTestRows)
	if errors.Is(err, ErrPseudoConsoleUnavailable) {
		t.Skipf("no pseudoconsole on this host: %v", err)
	}
	if err != nil {
		t.Fatalf("newPseudoConsole: %v", err)
	}
	if err := pty.launch(PseudoConsoleSpec{
		Path:        `C:\apogee-no-such-dir\missing.exe`,
		CommandLine: "missing",
		Cols:        conptyTestCols,
		Rows:        conptyTestRows,
	}); err == nil {
		t.Fatal("launch succeeded, want an error")
	}

	pty.discard()

	if _, err := pty.out.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Errorf("reading the output after a failed launch = %v, want os.ErrClosed", err)
	}
	if _, err := pty.in.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("writing the input after a failed launch = %v, want os.ErrClosed", err)
	}
}

// TestPseudoConsoleKillReapsTheTree proves the suspended start holds the whole tree: a descendant
// cmd.exe spawns is in the job, and Kill ends it along with the leader.
func TestPseudoConsoleKillReapsTheTree(t *testing.T) {
	t.Parallel()

	pty := startTestPseudoConsole(t, "cmd /c ping -n 60 127.0.0.1 >nul")
	t.Cleanup(func() { _ = pty.Close() })
	drainPseudoConsole(pty)

	// cmd.exe plus the ping it spawned: two live processes, both inside the job.
	pollJob(t, pty, func(active uint32) bool { return active >= 2 }, "the descendant to join the job")

	pty.Kill()

	if code := waitWithin(t, pty); code != killedExitCode {
		t.Errorf("exit code after Kill = %d, want %d", code, killedExitCode)
	}
	pollJob(t, pty, func(active uint32) bool { return active == 0 }, "the job to empty")
}

// TestStartPseudoConsoleRefusesABadSpec covers the launches that must fail before anything runs.
func TestStartPseudoConsoleRefusesABadSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec PseudoConsoleSpec
	}{
		{"zero size", PseudoConsoleSpec{CommandLine: "cmd /c exit", Cols: 0, Rows: conptyTestRows}},
		{"oversized", PseudoConsoleSpec{CommandLine: "cmd /c exit", Cols: 1 << 16, Rows: conptyTestRows}},
		{"empty command line", PseudoConsoleSpec{Cols: conptyTestCols, Rows: conptyTestRows}},
		{"missing executable", PseudoConsoleSpec{
			Path:        `C:\apogee-no-such-dir\missing.exe`,
			CommandLine: "missing",
			Cols:        conptyTestCols,
			Rows:        conptyTestRows,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pty, err := StartPseudoConsole(tt.spec)

			if err == nil {
				_ = pty.Close()
				t.Fatal("StartPseudoConsole succeeded, want an error")
			}
			if errors.Is(err, ErrPseudoConsoleUnavailable) {
				t.Skipf("no pseudoconsole on this host: %v", err)
			}
		})
	}
}

// startTestPseudoConsole starts commandLine under cmd.exe's resolved path, skipping on a host
// that will not create a pseudoconsole.
func startTestPseudoConsole(t *testing.T, commandLine string) *PseudoConsole {
	t.Helper()

	cmdPath, err := exec.LookPath("cmd")
	if err != nil {
		t.Fatalf("locate cmd.exe: %v", err)
	}
	pty, err := StartPseudoConsole(PseudoConsoleSpec{
		Path:        cmdPath,
		CommandLine: commandLine,
		Cols:        conptyTestCols,
		Rows:        conptyTestRows,
	})
	if errors.Is(err, ErrPseudoConsoleUnavailable) {
		t.Skipf("no pseudoconsole on this host: %v", err)
	}
	if err != nil {
		t.Fatalf("StartPseudoConsole: %v", err)
	}
	return pty
}

// pseudoConsoleOutput is what a drain goroutine collected, readable once the drain has ended.
type pseudoConsoleOutput struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	done chan struct{}
}

// Write appends to the collected output.
func (o *pseudoConsoleOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.Write(p)
}

// wait returns the collected output once the drain has seen the end of the pseudoconsole's
// output, failing the test if it never does.
func (o *pseudoConsoleOutput) wait(t *testing.T) string {
	t.Helper()

	select {
	case <-o.done:
	case <-time.After(conptyTestTimeout):
		t.Fatal("the pseudoconsole's output never ended after Close")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

// drainPseudoConsole reads pty continuously — a pseudoconsole whose output pipe fills stalls the
// child — until its output ends.
func drainPseudoConsole(pty *PseudoConsole) *pseudoConsoleOutput {
	output := &pseudoConsoleOutput{done: make(chan struct{})}
	go func() {
		defer close(output.done)
		_, _ = io.Copy(output, pty)
	}()
	return output
}

// waitWithin waits for pty's leader to exit and returns its exit code, failing the test if it
// overruns.
func waitWithin(t *testing.T, pty *PseudoConsole) int {
	t.Helper()

	type exit struct {
		code int
		err  error
	}
	exited := make(chan exit, 1)
	go func() {
		code, err := pty.Wait()
		exited <- exit{code, err}
	}()
	select {
	case got := <-exited:
		if got.err != nil {
			t.Fatalf("Wait: %v", got.err)
		}
		return got.code
	case <-time.After(conptyTestTimeout):
		t.Fatal("the pseudoconsole's process did not exit in time")
		return -1
	}
}

// jobAccounting is JOBOBJECT_BASIC_ACCOUNTING_INFORMATION, which x/sys does not declare.
type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

// pollJob waits until the number of live processes in pty's job satisfies ready, failing the test
// with what it was waiting for if it never does.
func pollJob(t *testing.T, pty *PseudoConsole, ready func(active uint32) bool, what string) {
	t.Helper()

	if pty.job.job == windows.InvalidHandle || !pty.job.assigned {
		t.Fatal("the process never joined a job")
	}
	deadline := time.Now().Add(conptyTestTimeout)
	for {
		var info jobAccounting
		err := windows.QueryInformationJobObject(pty.job.job, windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
		if err != nil {
			t.Fatalf("query the job: %v", err)
		}
		if ready(info.ActiveProcesses) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s (%d processes active)", what, info.ActiveProcesses)
		}
		time.Sleep(conptyTestPoll)
	}
}

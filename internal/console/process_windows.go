//go:build windows

package console

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
)

// terminal is the Windows half of a Process: the pseudoconsole (ConPTY) the command runs in, which
// also holds the kill-on-close Job Object the command and every descendant live in.
type terminal struct {
	console pseudoConsole
}

// pseudoConsole is what a Process needs of its platform.PseudoConsole. It is an interface so a
// test can stand in for the real one and watch the order the teardown runs in.
type pseudoConsole interface {
	io.ReadWriter
	Wait() (int, error)
	Kill()
	// Release ends the tree and the pseudoconsole but leaves the output open, so the reader can
	// drain the final frame to the end of output.
	Release() error
	// CloseOutput gives the output's read end back, ending a read still in flight.
	CloseOutput() error
}

// Start runs spec's command inside a pseudoconsole and returns the live Process.
//
// A confined spec runs under the restricted token its Prepare hook put on the command (the
// Windows Confiner's only touch on it): the launcher creates the child under that token, inside
// the same job. A confined spec whose Prepare set no token is refused before any process starts
// with an error wrapping domain.ErrConfinementUnavailable — the caller's fail-closed path (a
// demotion to Approval) is what that sentinel exists for — so a command reported confined never
// runs unfenced.
//
// The process is created suspended inside a kill-on-close job and only then resumed
// (platform.StartPseudoConsole), so Kill and Close reach everything it spawns. A host without a
// pseudoconsole yields an error wrapping platform.ErrPseudoConsoleUnavailable.
func Start(spec Spec) (*Process, error) {
	if len(spec.Argv) == 0 {
		return nil, errors.New("console: no command to run")
	}
	launch, err := prepareLaunch(spec)
	if err != nil {
		return nil, err
	}
	console, err := platform.StartPseudoConsole(launch)
	if err != nil {
		return nil, err
	}

	process := newProcess()
	process.console = console
	go process.collectOutput(console, process.outputSink(spec.Confined))
	go process.reap()
	return process, nil
}

// prepareLaunch assembles the command as an *exec.Cmd — the shape the caller's Prepare hook is
// written against on every platform — and reads the pseudoconsole launch back out of it: the
// resolved program, the verbatim command line, the directory, the environment and the token,
// whatever Prepare left them as. The command is never started through os/exec. A confined spec
// whose Prepare left no token fails closed with domain.ErrConfinementUnavailable.
func prepareLaunch(spec Spec) (platform.PseudoConsoleSpec, error) {
	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	if spec.CommandLine != "" {
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: spec.CommandLine}
	}

	if spec.Prepare != nil {
		if err := spec.Prepare(cmd); err != nil {
			return platform.PseudoConsoleSpec{}, err
		}
	}
	if cmd.Err != nil {
		return platform.PseudoConsoleSpec{}, cmd.Err
	}
	token := launchToken(cmd)
	if spec.Confined && token == 0 {
		return platform.PseudoConsoleSpec{}, fmt.Errorf(
			"%w: the console was to be confined but no restricted token was prepared", domain.ErrConfinementUnavailable)
	}

	return platform.PseudoConsoleSpec{
		Path:        cmd.Path,
		CommandLine: launchCommandLine(cmd),
		Dir:         cmd.Dir,
		Env:         cmd.Env,
		Cols:        windowCols,
		Rows:        windowRows,
		Token:       token,
	}, nil
}

// launchToken returns the primary token Prepare put on cmd, or zero when it set none and the
// child is to run under this process's own. The token stays borrowed: the Confiner that minted it
// owns the handle, and the launcher holds its own duplicate for the length of the launch.
func launchToken(cmd *exec.Cmd) windows.Token {
	if cmd.SysProcAttr == nil {
		return 0
	}
	return windows.Token(cmd.SysProcAttr.Token)
}

// launchCommandLine returns the command line cmd is launched with: the raw one when the caller set
// it (SysProcAttr.CmdLine, used verbatim exactly as os/exec would), otherwise the argv joined the
// way os/exec joins it.
func launchCommandLine(cmd *exec.Cmd) string {
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.CmdLine != "" {
		return cmd.SysProcAttr.CmdLine
	}
	return windows.ComposeCommandLine(cmd.Args)
}

// Write sends input to the pseudoconsole, where the process reads it as keyboard input.
func (p *Process) Write(input []byte) (int, error) {
	return p.console.Write(input)
}

// Kill terminates the process and every descendant its job holds, and returns without waiting for
// the exit to be recorded. It is idempotent and safe from any goroutine.
func (p *Process) Kill() { p.console.Kill() }

// Close kills the process tree, releases the pseudoconsole and its job, and waits for the process
// to be reaped and its output drained (see shutdown for what holds afterwards). The pseudoconsole
// is released BEFORE the joins: its output never ends while it is open, so releasing it is what
// lets the reader finish. The output pipe is closed only AFTER them — closing it under a read in
// flight would cut off the final frame — and closing it then still ends a reader the bounded join
// gave up on. It is idempotent: a Console is closed by whoever gets there first, its owner or the
// engine.
func (p *Process) Close() error {
	err := p.shutdown(p.console.Release, releaseBeforeJoin)
	return errors.Join(err, p.console.CloseOutput())
}

// reap waits for the process, records how it ended, and then releases the pseudoconsole.
//
// The release is the Windows form of the §2.4 teardown-on-every-exit: it terminates whatever the
// command left running in its job, and — because a pseudoconsole's output does not end when its
// process does — it is what lets the reader drain the final frame and close the ring, so a read
// waiting on an exited Console returns instead of sitting out its window. The output pipe is
// closed only once the reader has reached the end of output (or closeJoinTimeout has passed):
// conhost flushes a short command's last frame as the pseudoconsole closes, and closing the pipe
// at once would cancel the read carrying it. Neither error is lost: both steps are idempotent and
// hand the same error to Close.
func (p *Process) reap() {
	defer close(p.reaped)
	// An exit that could not be observed reports -1 — the code of a process whose end is
	// unknown — and that is all a Console has to say about it.
	code, _ := p.console.Wait()
	p.recordExit(code)
	_ = p.console.Release()
	joinBefore(p.readerDone, time.Now().Add(closeJoinTimeout))
	_ = p.console.CloseOutput()
}

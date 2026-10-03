//go:build windows

package console

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
)

// terminal is the Windows half of a Process: the pseudoconsole (ConPTY) the command runs in, which
// also holds the kill-on-close Job Object the command and every descendant live in.
type terminal struct {
	console *platform.PseudoConsole
}

// Start runs spec's command inside a pseudoconsole and returns the live Process.
//
// A confined spec is refused before anything else happens — before Prepare runs, before any
// process starts — with an error wrapping domain.ErrConfinementUnavailable: nothing on Windows can
// confine a Console yet, and the caller's fail-closed path (a demotion to Approval) is what that
// sentinel exists for. A Prepare hook that puts a restricted token on the command is refused the
// same way, as a backstop: the pseudoconsole launcher has no way to honour one.
//
// The process is created suspended inside a kill-on-close job and only then resumed
// (platform.StartPseudoConsole), so Kill and Close reach everything it spawns. A host without a
// pseudoconsole yields an error wrapping platform.ErrPseudoConsoleUnavailable.
func Start(spec Spec) (*Process, error) {
	if len(spec.Argv) == 0 {
		return nil, errors.New("console: no command to run")
	}
	if spec.Confined {
		return nil, fmt.Errorf("%w: a console cannot be confined on Windows", domain.ErrConfinementUnavailable)
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
// resolved program, the verbatim command line, the directory and the environment, whatever
// Prepare left them as. The command is never started through os/exec.
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
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Token != 0 {
		return platform.PseudoConsoleSpec{}, fmt.Errorf(
			"%w: a console cannot run under a restricted token", domain.ErrConfinementUnavailable)
	}

	return platform.PseudoConsoleSpec{
		Path:        cmd.Path,
		CommandLine: launchCommandLine(cmd),
		Dir:         cmd.Dir,
		Env:         cmd.Env,
		Cols:        windowCols,
		Rows:        windowRows,
	}, nil
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
// lets the reader finish. It is idempotent: a Console is closed by whoever gets there first, its
// owner or the engine.
func (p *Process) Close() error {
	return p.shutdown(p.console.Close, releaseBeforeJoin)
}

// reap waits for the process, records how it ended, and then releases the pseudoconsole.
//
// The release is the Windows form of the §2.4 teardown-on-every-exit: it terminates whatever the
// command left running in its job, and — because a pseudoconsole's output does not end when its
// process does — it is what lets the reader drain the final frame and close the ring, so a read
// waiting on an exited Console returns instead of sitting out its window. Its error is not lost:
// the release is idempotent and hands the same error to Close.
func (p *Process) reap() {
	defer close(p.reaped)
	// An exit that could not be observed reports -1 — the code of a process whose end is
	// unknown — and that is all a Console has to say about it.
	code, _ := p.console.Wait()
	p.recordExit(code)
	_ = p.console.Close()
}

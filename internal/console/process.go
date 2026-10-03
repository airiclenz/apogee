//go:build !windows

package console

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/airiclenz/apogee/internal/platform"
)

// waitDelay bounds how long Wait blocks draining the pseudo-terminal after the process has been
// killed, so a descendant holding the tty open cannot wedge a Console's teardown forever. It
// matches the one-shot subprocess path's delay (internal/tools' §2.4 teardown).
const waitDelay = 5 * time.Second

// terminal is the POSIX half of a Process: the command, the pseudo-terminal's master side, and the
// teardown that kills the command's process group.
type terminal struct {
	cmd    *exec.Cmd
	master *os.File
	// cancel stops the process by cancelling the command's own context, whose cmd.Cancel —
	// wired by td — signals the whole process group.
	cancel context.CancelFunc
	// td is platform's §2.4 process teardown: it owns cmd.Cancel and the clean-exit group kill
	// reap performs, so a Console tears its tree down through the same tested contract as the
	// one-shot subprocess path.
	td platform.ProcessTeardown
}

// Start runs spec's command under a pseudo-terminal and returns the live Process.
//
// The command gets a context of its own — never a per-call one — because a Console outlives the
// tool call that opened it: cancelling that context is what Kill does, and the cmd.Cancel that
// platform.NewProcessTeardown wires turns it into a SIGKILL of the whole process group so nothing
// the command spawned is left behind (confinement execution contract §2.4).
//
// The process is started as a SESSION leader with the terminal as its controlling tty, which is
// what makes job control, line editing and Ctrl-C work inside it. A session leader cannot also be
// placed in a caller-chosen process group, so whatever Setpgid the teardown or the Prepare hook
// asked for is dropped here — at no cost to teardown, because after setsid the process's group
// id equals its pid and a kill aimed at the negative pid still reaches the whole group.
func Start(spec Spec) (*Process, error) {
	if len(spec.Argv) == 0 {
		return nil, errors.New("console: no command to run")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	td := platform.NewProcessTeardown(cmd)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.WaitDelay = waitDelay

	if spec.Prepare != nil {
		if err := spec.Prepare(cmd); err != nil {
			cancel()
			return nil, err
		}
	}

	process := newProcess()
	process.cmd = cmd
	process.cancel = cancel
	process.td = td
	collect := process.outputSink(spec.Confined)

	// pty.StartWithAttrs assigns this copy to cmd.SysProcAttr before Start, so clearing Setpgid
	// here discards the one the teardown set and the child never runs setsid-then-setpgid. The
	// teardown's kill is unaffected: a session leader's PGID == its PID, which is exactly the
	// group the negative-PID kill is aimed at.
	attrs := syscall.SysProcAttr{}
	if cmd.SysProcAttr != nil {
		attrs = *cmd.SysProcAttr
	}
	attrs.Setpgid = false
	attrs.Setsid = true
	attrs.Setctty = true
	master, err := pty.StartWithAttrs(cmd, &pty.Winsize{Rows: windowRows, Cols: windowCols}, &attrs)
	if err != nil {
		cancel()
		return nil, err
	}
	process.master = master

	go process.collectOutput(master, collect)
	go process.reap()
	return process, nil
}

// Write sends input to the terminal, where the process reads it as keyboard input.
func (p *Process) Write(input []byte) (int, error) {
	return p.master.Write(input)
}

// Kill stops the process and everything it spawned, and returns without waiting for the exit to
// be recorded. It is idempotent and safe to call from the reader goroutine, which is what the
// denial watch does.
func (p *Process) Kill() { p.cancel() }

// Close kills the process, waits for it to be reaped and its output drained, and releases the
// pseudo-terminal (see shutdown for what holds afterwards). The master is released only after the
// reader has drained it: the kill closes every writer's end, which is what ends the reader. It is
// idempotent: a Console is closed by whoever gets there first, its owner or the engine.
func (p *Process) Close() error {
	return p.shutdown(p.releaseMaster, releaseAfterJoin)
}

// releaseMaster closes the pseudo-terminal's master side; a master already closed is not an error.
func (p *Process) releaseMaster() error {
	if err := p.master.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	return nil
}

// reap waits for the process, records how it ended, and kills the process group once more.
//
// The second kill is the §2.4 teardown-on-every-exit amendment: a command that exits on its own
// after backgrounding a child leaves that child holding the terminal, so the clean-exit path
// needs the same group kill the cancel path gets — the teardown's Reap, aimed at the reaped
// leader's negative pid, which is safe precisely because the group still has a member (the
// kernel cannot recycle a process group id while one remains). A descendant that left the group
// with a setsid or setpgid of its own is outside that reach — the same accepted residual the
// one-shot subprocess path documents at platform.NewProcessTeardown. Release then drops whatever
// the containment held, a no-op on POSIX.
func (p *Process) reap() {
	defer close(p.reaped)
	_ = p.cmd.Wait()
	code := -1
	if state := p.cmd.ProcessState; state != nil {
		code = state.ExitCode()
	}
	p.recordExit(code)
	p.td.Reap(p.cmd)
	p.td.Release()
}

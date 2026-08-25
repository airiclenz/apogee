//go:build !windows

package console

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/airiclenz/apogee/internal/platform"
)

// ErrUnsupported is returned by Start on a platform with no pseudo-terminal backend. It is
// declared in both halves of the build-tag pair so callers can compare against it everywhere;
// on POSIX nothing ever returns it.
var ErrUnsupported = errors.New("console is not supported on Windows yet")

// windowRows and windowCols are the pseudo-terminal's size. A program that lays its output out
// for a terminal (a REPL banner, a test runner's progress line, a dev server's table) needs a
// window to lay it out in, and a fixed one keeps a Console's output reproducible across hosts —
// nothing here follows the human's real terminal, which the model is not looking at anyway.
const (
	windowRows = 40
	windowCols = 160
)

// waitDelay bounds how long Wait blocks draining the pseudo-terminal after the process has been
// killed, so a descendant holding the tty open cannot wedge a Console's teardown forever. It
// matches the one-shot subprocess path's delay (internal/tools' §2.4 teardown).
const waitDelay = 5 * time.Second

// closeJoinTimeout bounds how long Close waits for the reader goroutine to finish after the kill.
// The kill closes every writer's end of the tty, so the reader normally returns within
// microseconds; the bound exists so a wedged fd cannot make closing a Console hang.
const closeJoinTimeout = 5 * time.Second

// Spec describes one console process to start. It is deliberately free of ids, owners and tool
// vocabulary: this file's whole subject is a process behind a pseudo-terminal, and the registry
// above it owns everything else.
type Spec struct {
	// Argv is the program and its arguments — already shell-wrapped by the caller when the
	// command is a shell line.
	Argv []string
	// Dir is the working directory, already resolved and fenced by the caller.
	Dir string
	// Env is the child's complete environment; nil inherits this process's.
	Env []string
	// Confined reports that the caller fenced the command, which is what puts the
	// kill-on-denial watch on the output path. It describes the command's treatment, not a
	// request: this package never confines anything itself.
	Confined bool
	// Prepare is the caller's hook on the assembled *exec.Cmd — confinement, refusals,
	// anything that must touch the command before it starts. It runs after Dir and Env are
	// set and before the pseudo-terminal is opened; an error from it aborts the start. nil
	// means no preparation.
	Prepare func(*exec.Cmd) error
}

// Process is one live console: a command running under a pseudo-terminal, its output collected
// into a ring buffer by a reader goroutine, its input written to the terminal's master side.
//
// Everything a caller can do to it is safe from any goroutine. The two goroutines Start leaves
// behind — the reader draining the terminal and the waiter reaping the process — end on their own
// when the process does; Close ends them early.
type Process struct {
	cmd    *exec.Cmd
	master *os.File
	ring   *ring
	// cancel stops the process by cancelling the command's own context, whose cmd.Cancel
	// signals the whole process group.
	cancel context.CancelFunc
	// denial is the kill-on-denial watch, non-nil only for a confined Console.
	denial *platform.DenialKillWriter
	// readerDone closes when the reader goroutine has drained the terminal for the last time,
	// and reaped when the waiter goroutine has recorded how the process ended.
	readerDone chan struct{}
	reaped     chan struct{}

	mu       sync.Mutex
	exited   bool
	exitCode int

	closeOnce sync.Once
	closeErr  error
}

// Start runs spec's command under a pseudo-terminal and returns the live Process.
//
// The command gets a context of its own — never a per-call one — because a Console outlives the
// tool call that opened it: cancelling that context is what Kill does, and cmd.Cancel turns it
// into a SIGKILL of the whole process group so nothing the command spawned is left behind
// (confinement execution contract §2.4).
//
// The process is started as a SESSION leader with the terminal as its controlling tty, which is
// what makes job control, line editing and Ctrl-C work inside it. A session leader cannot also be
// placed in a caller-chosen process group, so whatever Setpgid the Prepare hook asked for is
// dropped here — at no cost to teardown, because after setsid the process's group id equals its
// pid and a kill aimed at the negative pid still reaches the whole group.
func Start(spec Spec) (*Process, error) {
	if len(spec.Argv) == 0 {
		return nil, errors.New("console: no command to run")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Cancel = func() error {
		killProcessGroup(cmd)
		return nil
	}
	cmd.WaitDelay = waitDelay

	if spec.Prepare != nil {
		if err := spec.Prepare(cmd); err != nil {
			cancel()
			return nil, err
		}
	}

	process := &Process{
		cmd:        cmd,
		ring:       newRing(ringCapacity),
		cancel:     cancel,
		readerDone: make(chan struct{}),
		reaped:     make(chan struct{}),
		exitCode:   -1,
	}
	var collect io.Writer = process.ring
	if spec.Confined {
		// A confined command that prints an OS denial is stopped where it was denied instead
		// of running on against a half-done workspace (ADR 0056 §2). The watch forwards every
		// byte to the ring first, so the model still reads the denial that killed it.
		process.denial = platform.NewDenialKillWriter(process.ring, process.Kill)
		collect = process.denial
	}

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

	go process.collectOutput(collect)
	go process.reap()
	return process, nil
}

// Read returns the output produced since the previous Read, with terminal control sequences
// stripped, together with how many bytes the ring dropped over the same span. With wait <= 0 it
// reports what is buffered now; with wait > 0 it returns as soon as new output arrives, the
// window passes, or the process's output ends.
func (p *Process) Read(wait time.Duration) (string, int) {
	unread, dropped := p.ring.Read(wait)
	return stripEscapes(string(unread)), dropped
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
// pseudo-terminal. What the ring still holds stays readable afterwards, so a caller can take the
// tail after closing — and by the time Close returns, Alive and ExitCode report the final answer.
// Both joins are bounded so a wedged descendant cannot make closing a Console hang. It is
// idempotent: a Console is closed by whoever gets there first, its owner or the engine.
func (p *Process) Close() error {
	p.Kill()
	p.closeOnce.Do(func() {
		deadline := time.Now().Add(closeJoinTimeout)
		joinBefore(p.readerDone, deadline)
		joinBefore(p.reaped, deadline)
		if err := p.master.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			p.closeErr = err
		}
		p.ring.close()
	})
	return p.closeErr
}

// joinBefore waits for done to close, giving up at deadline.
func joinBefore(done <-chan struct{}, deadline time.Time) {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

// Alive reports whether the process is still running.
func (p *Process) Alive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.exited
}

// ExitCode returns the code the process exited with, or -1 while it is still running and for a
// process a signal killed — Alive tells the two apart.
func (p *Process) ExitCode() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitCode
}

// DenialStopped reports that the kill-on-denial watch stopped this Console: the command was
// confined and its output carried an OS denial signature. It is always false for an unconfined
// Console, which has no watch.
func (p *Process) DenialStopped() bool {
	return p.denial != nil && p.denial.Detected()
}

// collectOutput drains the terminal into collect — the ring, or the denial watch in front of it —
// until the process and everything holding the terminal open are gone. The read error at the end
// is the ordinary way a pseudo-terminal reports that (EIO on Linux, EOF elsewhere), so there is
// nothing to report; the ring closes to release anyone waiting on output that will not come.
func (p *Process) collectOutput(collect io.Writer) {
	defer close(p.readerDone)
	_, _ = io.Copy(collect, p.master)
	p.ring.close()
}

// reap waits for the process, records how it ended, and kills the process group once more.
//
// The second kill is the §2.4 teardown-on-every-exit amendment: a command that exits on its own
// after backgrounding a child leaves that child holding the terminal, so the clean-exit path
// needs the same group kill the cancel path gets. Aiming it at the reaped leader's negative pid
// is safe precisely because the group still has a member — the kernel cannot recycle a process
// group id while one remains.
func (p *Process) reap() {
	defer close(p.reaped)
	_ = p.cmd.Wait()
	code := -1
	if state := p.cmd.ProcessState; state != nil {
		code = state.ExitCode()
	}
	p.mu.Lock()
	p.exited = true
	p.exitCode = code
	p.mu.Unlock()
	killProcessGroup(p.cmd)
}

// killProcessGroup SIGKILLs the process's whole group. The process is a session leader, so its
// group id equals its pid and the negative-pid kill reaches every descendant that has not
// deliberately left the group with a setsid or setpgid of its own — the same accepted residual
// the one-shot subprocess path documents. An already-gone process or empty group is the ordinary
// case and answers with an error worth ignoring.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}

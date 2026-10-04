package console

import (
	"context"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/platform"
)

// windowRows and windowCols are the terminal's size. A program that lays its output out for a
// terminal (a REPL banner, a test runner's progress line, a dev server's table) needs a window to
// lay it out in, and a fixed one keeps a Console's output reproducible across hosts — nothing here
// follows the human's real terminal, which the model is not looking at anyway.
const (
	windowRows = 40
	windowCols = 160
)

// closeJoinTimeout bounds how long Close waits for the reader and the reaper to finish after the
// kill. Ending the process (and, on Windows, releasing the pseudoconsole) ends the terminal's
// output, so the reader normally returns within microseconds; the bound exists so a wedged handle
// cannot make closing a Console hang.
const closeJoinTimeout = 5 * time.Second

// Spec describes one console process to start. It is deliberately free of ids, owners and tool
// vocabulary: this package's process files are about a process behind a terminal, and the
// registry above them owns everything else.
type Spec struct {
	// Argv is the program and its arguments — already shell-wrapped by the caller when the
	// command is a shell line.
	Argv []string
	// CommandLine is the verbatim process command line Argv must be launched with, or "" when
	// the platform's own argv joining is faithful (POSIX, where it is ignored). On Windows it is
	// handed to the pseudoconsole launcher exactly as given — never re-joined from Argv —
	// because cmd.exe does not read the escapes an argv join adds (platform.Host.CommandLine
	// builds it).
	CommandLine string
	// Dir is the working directory, already resolved and fenced by the caller.
	Dir string
	// Env is the child's complete environment; nil inherits this process's.
	Env []string
	// Confined reports that the caller fenced the command, which is what puts the
	// kill-on-denial watch on the output path. It describes the command's treatment, not a
	// request: this package never confines anything itself. On Windows, where a Console cannot
	// be confined yet, Start refuses a confined spec with domain.ErrConfinementUnavailable.
	Confined bool
	// Prepare is the caller's hook on the assembled *exec.Cmd — confinement, refusals,
	// anything that must touch the command before it starts. It runs after Dir, Env and the
	// raw command line are set and before the terminal is opened; an error from it aborts the
	// start. nil means no preparation.
	Prepare func(*exec.Cmd) error
}

// Process is one live console: a command running under a terminal — a pseudo-terminal on POSIX, a
// pseudoconsole on Windows — its output collected into a ring buffer by a reader goroutine, its
// input written to the terminal.
//
// Everything a caller can do to it is safe from any goroutine. The two goroutines Start leaves
// behind — the reader draining the terminal and the waiter reaping the process — end on their own
// when the process does; Close ends them early.
type Process struct {
	// terminal is the platform's half: the terminal and the process behind it, and how each is
	// written to, killed and released (process.go on POSIX, process_windows.go on Windows).
	terminal

	ring *ring
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

// newProcess returns a Process with its ring and join channels ready and no terminal yet: the
// platform's Start fills the terminal in once the process is running.
func newProcess() *Process {
	return &Process{
		ring:       newRing(ringCapacity),
		readerDone: make(chan struct{}),
		reaped:     make(chan struct{}),
		exitCode:   -1,
	}
}

// outputSink returns where the reader goroutine copies the terminal's output: the ring, or — for a
// confined command — the kill-on-denial watch in front of it.
//
// A confined command that prints an OS denial is stopped where it was denied instead of running on
// against a half-done workspace (ADR 0056 §2). A terminal has one stream, so the watch reads stdout
// and stderr alike here — the line-anchored signature, not the pipe path's stderr-only wiring, is
// what keeps a quoted denial from killing it. The watch forwards every byte to the ring first, so
// the model still reads the denial that killed it.
func (p *Process) outputSink(confined bool) io.Writer {
	if !confined {
		return p.ring
	}
	p.denial = platform.NewDenialKillWriter(p.ring, p.Kill)
	return p.denial
}

// Read returns the output produced since the previous Read, with terminal control sequences
// stripped, together with how many bytes the ring dropped over the same span. It is ReadContext
// with a context that is never cancelled.
func (p *Process) Read(wait time.Duration) (string, int) {
	return p.ReadContext(context.Background(), wait)
}

// ReadContext returns the output produced since the previous read, with terminal control
// sequences stripped, together with how many bytes the ring dropped over the same span. With
// wait <= 0 it reports what is buffered now; with wait > 0 it returns as soon as new output
// arrives, the window passes, the process's output ends, or ctx is cancelled — the cancel
// returning nothing and leaving whatever is buffered for the next read.
func (p *Process) ReadContext(ctx context.Context, wait time.Duration) (string, int) {
	unread, dropped := p.ring.ReadContext(ctx, wait)
	return stripEscapes(string(unread)), dropped
}

// Alive reports whether the process is still running.
func (p *Process) Alive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.exited
}

// ExitCode returns the code the process exited with, or -1 while it is still running and for a
// POSIX process a signal killed — Alive tells the two apart. On Windows a killed tree reports the
// code its job was terminated with.
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

// collectOutput drains source — the terminal — into collect, the ring or the denial watch in front
// of it, until the terminal reports the end of its output. The read error at the end is the
// ordinary way a terminal reports that (EIO from a Linux pseudo-terminal, EOF elsewhere, a closed
// pipe when a pseudoconsole's output is closed under a read the teardown stopped waiting for), so
// there is nothing to report; the ring closes to release anyone waiting on output that will not
// come.
func (p *Process) collectOutput(source io.Reader, collect io.Writer) {
	defer close(p.readerDone)
	_, _ = io.Copy(collect, source)
	p.ring.close()
}

// recordExit records how the process ended, which is what Alive and ExitCode report from then on.
func (p *Process) recordExit(code int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.exited = true
	p.exitCode = code
}

// releaseOrder says when Close frees the terminal relative to joining the reader and the reaper.
type releaseOrder int

const (
	// releaseAfterJoin frees the terminal once the reader has drained it: a POSIX
	// pseudo-terminal reports the end of its output by itself once the kill has closed every
	// writer's end, and releasing the master first would cut that last drain short.
	releaseAfterJoin releaseOrder = iota
	// releaseBeforeJoin frees the terminal first: a pseudoconsole's output never ends while it
	// is open, so the reader can only finish once it is released.
	releaseBeforeJoin
)

// shutdown is Close's one implementation: it kills the process, then — exactly once — frees the
// terminal through release in the platform's order, waits for the reader and the reaper, and
// closes the ring. What the ring still holds stays readable afterwards, so a caller can take the
// tail after closing — and by the time it returns, Alive and ExitCode report the final answer.
// Both joins share one closeJoinTimeout deadline so a wedged descendant cannot make closing a
// Console hang. Every call returns the first call's release error.
func (p *Process) shutdown(release func() error, order releaseOrder) error {
	p.Kill()
	p.closeOnce.Do(func() {
		if order == releaseBeforeJoin {
			p.closeErr = release()
		}
		deadline := time.Now().Add(closeJoinTimeout)
		joinBefore(p.readerDone, deadline)
		joinBefore(p.reaped, deadline)
		if order == releaseAfterJoin {
			p.closeErr = release()
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

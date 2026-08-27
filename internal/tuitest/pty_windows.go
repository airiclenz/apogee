//go:build windows

package tuitest

import (
	"testing"
	"time"
)

// PTYDriver on Windows is the type and nothing else: it exists so that a test file which drives
// the shipped binary through a pseudo-terminal COMPILES here, and skips rather than fails.
//
// The black-box driver is deliberately unix-only. It is built on a unix pty pair — a controlling
// terminal, a session leader, SIGWINCH, SIGKILL on a process group, and a termios read-back — and
// a ConPTY stand-in for those would be a different mechanism asserting different things while
// wearing the same test's name. What Windows keeps is the in-process [Driver], which has no
// platform gate: the frames, the keys, the waits and the goldens are all still checked here.
type PTYDriver struct{ t testing.TB }

// ptySkip is what every entry point says on its way out.
const ptySkip = "tuitest: the PTY driver needs a unix pseudo-terminal; not available on Windows"

// NewPTYDriver skips the calling test. Nothing is started, and the returned driver is only there
// so the call site type-checks — t.Skip ends the test before it can be used.
func NewPTYDriver(t testing.TB, _ string, _ []string, _ []string, _ Size) *PTYDriver {
	t.Helper()
	t.Skip(ptySkip)
	return &PTYDriver{t: t}
}

// Frame skips.
func (d *PTYDriver) Frame() Frame { d.t.Skip(ptySkip); return Frame{} }

// Screen skips.
func (d *PTYDriver) Screen() *Screen { d.t.Skip(ptySkip); return nil }

// Pid skips.
func (d *PTYDriver) Pid() int { d.t.Skip(ptySkip); return 0 }

// Exited skips.
func (d *PTYDriver) Exited() <-chan int { d.t.Skip(ptySkip); return nil }

// Bytes skips.
func (d *PTYDriver) Bytes() []byte { d.t.Skip(ptySkip); return nil }

// TTYState skips.
func (d *PTYDriver) TTYState() (echo, canonical bool) { d.t.Skip(ptySkip); return false, false }

// Type skips.
func (d *PTYDriver) Type(string) { d.t.Skip(ptySkip) }

// Press skips.
func (d *PTYDriver) Press(Key) { d.t.Skip(ptySkip) }

// Resize skips.
func (d *PTYDriver) Resize(int, int) { d.t.Skip(ptySkip) }

// WaitFor skips.
func (d *PTYDriver) WaitFor(func() bool, ...Option) { d.t.Skip(ptySkip) }

// WaitText skips.
func (d *PTYDriver) WaitText(string) { d.t.Skip(ptySkip) }

// WaitGone skips.
func (d *PTYDriver) WaitGone(string) { d.t.Skip(ptySkip) }

// WaitQuiet skips.
func (d *PTYDriver) WaitQuiet(time.Duration) { d.t.Skip(ptySkip) }

// Quit skips.
func (d *PTYDriver) Quit() int { d.t.Skip(ptySkip); return 0 }

// Kill skips.
func (d *PTYDriver) Kill() { d.t.Skip(ptySkip) }

// Close is the one method that does not skip: it is registered as a cleanup by the unix
// constructor, and a cleanup that skipped would turn every Windows test's teardown into a skip.
func (d *PTYDriver) Close() {}

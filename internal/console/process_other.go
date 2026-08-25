//go:build windows

package console

import (
	"errors"
	"os/exec"
	"time"
)

// ErrUnsupported is what Start returns on Windows, whose pseudo-terminal facility (ConPTY) this
// package does not implement yet. It is declared in both halves of the build-tag pair so callers
// can compare against it everywhere.
//
// Windows keeps the whole exported surface rather than dropping out of the build, because the
// tools above it are registered on every platform: a roster that enabled a console tool would
// otherwise trip the unknown-tool notice instead of telling the user what is actually missing.
var ErrUnsupported = errors.New("console is not supported on Windows yet")

// Spec describes one console process to start. See the POSIX build for what each field means;
// on Windows nothing reads them, because no process is ever started.
type Spec struct {
	Argv     []string
	Dir      string
	Env      []string
	Confined bool
	Prepare  func(*exec.Cmd) error
}

// Process is the Windows stand-in for a live console. Start never returns one, so its methods
// exist only to keep the package's surface identical on both platforms.
type Process struct{}

// Start reports that this platform has no pseudo-terminal backend.
func Start(Spec) (*Process, error) { return nil, ErrUnsupported }

// Read returns no output and no dropped bytes.
func (p *Process) Read(time.Duration) (string, int) { return "", 0 }

// Write reports that no process is listening.
func (p *Process) Write([]byte) (int, error) { return 0, ErrUnsupported }

// Kill does nothing; there is no process to stop.
func (p *Process) Kill() {}

// Close does nothing; there is no terminal to release.
func (p *Process) Close() error { return nil }

// Alive reports false: a Process on this platform is never running.
func (p *Process) Alive() bool { return false }

// ExitCode returns -1, the code of a process that never ran.
func (p *Process) ExitCode() int { return -1 }

// DenialStopped reports false: with no process there is no denial watch.
func (p *Process) DenialStopped() bool { return false }

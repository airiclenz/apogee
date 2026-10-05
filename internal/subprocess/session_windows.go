//go:build windows

package subprocess

import "os/exec"

// startConfinedSession is a no-op on Windows: the controlling terminal it detaches a POSIX
// child from is a session property, and Windows has neither sessions nor TIOCSTI. The run's
// container there is the Job Object the teardown owns (session_unix.go holds the POSIX rule).
func startConfinedSession(_ *exec.Cmd) {}

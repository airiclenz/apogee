//go:build windows

package main

import "errors"

// errNoPTY is what every recording entry point answers on Windows. The recorder is built on a unix
// pty pair — a session leader with a controlling terminal, a process-group kill — exactly as
// tuitest's PTYDriver is, and a ConPTY stand-in would be a different recorder under the same name.
// Everything that reads a take (check, render) still builds and runs here.
var errNoPTY = errors.New("recording needs a unix pseudo-terminal; not available on Windows")

// TermOptions is the terminal a take is recorded in; see the unix build for the fields' meaning.
type TermOptions struct {
	Cols, Rows     int
	FPS            int
	Env            []string
	Dir            string
	FromFirstPaint bool
}

// Terminal is the type and nothing else on Windows: [StartTerminal] never returns one.
type Terminal struct{}

// StartTerminal refuses: there is no pty to start a program under.
func StartTerminal(string, []string, TermOptions) (*Terminal, error) { return nil, errNoPTY }

// Send refuses.
func (*Terminal) Send([]byte) error { return errNoPTY }

// Event does nothing.
func (*Terminal) Event(string, string) {}

// Current is the empty snapshot.
func (*Terminal) Current() Snapshot { return Snapshot{} }

// Painted is already closed: there is no paint to wait for.
func (*Terminal) Painted() <-chan struct{} {
	painted := make(chan struct{})
	close(painted)
	return painted
}

// Done is already closed: there is no program to wait for.
func (*Terminal) Done() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

// ExitCode is -1: nothing ran.
func (*Terminal) ExitCode() int { return -1 }

// Close returns no take.
func (*Terminal) Close() *Take { return nil }

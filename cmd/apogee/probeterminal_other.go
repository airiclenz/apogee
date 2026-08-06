//go:build !windows

package main

// prepareTerminalOutput has nothing to do off Windows: a POSIX terminal interprets the sequences
// the probe writes without being asked to, and the end-of-line wrapping the last-column section
// measures is the terminal's own — there is no console-mode flag between the program and it.
//
// This twin exists so every one of the six release targets compiles the probe; see
// probeterminal_windows.go for the mode a Windows console has to be put into first.
func prepareTerminalOutput(uintptr) (func(), error) { return func() {}, nil }

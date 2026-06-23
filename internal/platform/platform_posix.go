//go:build !windows

package platform

// posixHost is the POSIX implementation of Host (Linux, macOS, and the other
// Unix targets). It is the real Phase-0 backend; the Windows counterpart is a
// stub (plan §P0.5).
type posixHost struct{}

// Command wraps line in `sh -c`, the POSIX shell invocation.
func (posixHost) Command(line string) []string { return []string{"sh", "-c", line} }

// ExecExt is empty on POSIX — executables carry no filename extension.
func (posixHost) ExecExt() string { return "" }

// Current returns the platform Host for this (non-Windows) build target.
func Current() Host { return posixHost{} }

var _ Host = posixHost{}

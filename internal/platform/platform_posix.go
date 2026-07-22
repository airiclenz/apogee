//go:build !windows

package platform

// Current returns the platform Host for this (non-Windows) build target: the POSIX rule
// set — `sh -c`, no executable suffix, exact-case slash-separated paths, and an argv that
// execve takes verbatim, so no raw command line is needed (host.go carries the rules and
// their reasoning).
func Current() Host { return posixRules() }

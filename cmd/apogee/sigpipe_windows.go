//go:build windows

package main

// ignoreSIGPIPE is the no-op twin of the Unix disarm: Windows has no SIGPIPE at all. A write to a
// pipe whose reader is gone fails with ERROR_BROKEN_PIPE, which the runtime hands straight back to
// the caller as an error, so the Event stream already gets on this OS what the Unix build has to
// ask for (sigpipe_unix.go).
func ignoreSIGPIPE() {}

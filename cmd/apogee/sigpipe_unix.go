//go:build !windows

package main

import (
	"os/signal"
	"syscall"
)

// ignoreSIGPIPE disarms the signal a closed stdout raises, so a broken pipe reaches the Event
// stream as the write ERROR it is rather than as a dead process.
//
// The Go runtime's own rule is the reason this exists: a write to file descriptor 1 or 2 that
// gets EPIPE re-raises SIGPIPE with the default disposition, which kills the process without a
// word (os/signal, "SIGPIPE"). That is right for a program whose stdout IS its output — `apogee
// headless --format text` piped into `head` should die quietly at the closed pipe. It is exactly
// wrong for the Event stream: a run that has already edited files would be half-killed because a
// consumer stopped reading, with no `run_finished`, no record saved and no exit code worth
// reading (ADR 0075 decision 9). Ignored, the same write returns EPIPE to the Writer, which
// reports it once on stderr and goes silent while the run finishes on its own terms.
//
// It is process-wide and therefore called ONLY under `--format json`, where the stream's
// blocking-write contract asks for it; every other invocation of this binary keeps the default
// disposition it has always had.
func ignoreSIGPIPE() { signal.Ignore(syscall.SIGPIPE) }

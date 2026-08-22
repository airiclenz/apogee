package platform

import (
	"io"
	"strings"
	"sync/atomic"
)

// confinementDenialSignatures are the OS-denial spellings the confined-run watch keys on:
// strerror(EPERM) — what seatbelt denials print on macOS — and strerror(EACCES) — what
// landlock denials print on Linux (its filesystem refusals are EACCES, not EPERM) — each
// in libc's capitalisation and the lower-cased spelling Go's syscall.Errno prints, plus
// the bare errno names toolchains emit. They are POSIX spellings by design — the Windows
// token backend's denials print "Access is denied.", which is deliberately not matched:
// the Windows terminal path has no fail-fast floor either (see FailFastPreamble).
// Best-effort by design — strerror text is locale-dependent, and a missed match costs
// nothing (the model still sees the non-zero exit), while a false match is confined to
// confined runs and surfaces loudly as a labeled error.
var confinementDenialSignatures = []string{
	"Operation not permitted",
	"operation not permitted",
	"EPERM",
	"Permission denied",
	"permission denied",
	"EACCES",
}

// denialSignatureOverlap is how many trailing bytes a DenialKillWriter carries between
// writes: one byte short of the longest signature, so a signature split across two pipe
// chunks still matches on the next write.
var denialSignatureOverlap = func() int {
	longest := 0
	for _, signature := range confinementDenialSignatures {
		if len(signature) > longest {
			longest = len(signature)
		}
	}
	return longest - 1
}()

// LooksLikeConfinementDenial reports whether a confined command's output carries one of
// the OS-denial signatures — the heuristic half of confinement-denial handling; the
// structural half (the run was confined) is the caller's to check. It is the same match
// DenialKillWriter applies live, exported so the execution tools label a finished result
// with the identical judgement.
func LooksLikeConfinementDenial(output string) bool {
	for _, signature := range confinementDenialSignatures {
		if strings.Contains(output, signature) {
			return true
		}
	}
	return false
}

// DenialKillWriter is the live kill-on-denial watch a CONFINED subprocess run is wired
// through (fix A of the 2026-08-22 workspace-clobber incident): an io.Writer placed on the
// child's output path that forwards every byte to next and scans the stream for an
// OS-denial signature. The first match calls kill exactly once — the caller hands in
// whatever stops the run, e.g. the CommandContext cancel whose cmd.Cancel kills the whole
// process group — so a script whose command was denied by the confinement fence is stopped
// there instead of running its remaining lines against a half-done state. That job cannot
// be left to `set -e`: POSIX exempts every command of an AND-OR list but the last, so a
// denied `mkdir d && cd d` chain does NOT abort the script, and the unguarded lines after
// it run with the cwd unchanged — the incident's exact clobber.
//
// The watch is best-effort in both directions and its caller must treat it so: the kill
// races the shell's next command (the denial's stderr reaches the parent through a pipe),
// and a confined command that legitimately prints a signature is killed too — that false
// positive surfaces loudly as a labeled error the model can react to, and it is confined
// to confined runs, which an output-quoting command has little business being. Scanning
// spans write boundaries (a signature split across two pipe chunks still matches).
//
// Write is single-writer by contract (os/exec drives one copier per writer; a caller
// wiring stdout and stderr to the SAME DenialKillWriter gets exec's single interleaved
// copier). Detected is safe to read from another goroutine after the run.
type DenialKillWriter struct {
	next     io.Writer
	kill     func()
	tail     []byte
	detected atomic.Bool
}

// NewDenialKillWriter returns a watch forwarding to next that calls kill exactly once
// when the stream first carries an OS-denial signature.
func NewDenialKillWriter(next io.Writer, kill func()) *DenialKillWriter {
	return &DenialKillWriter{next: next, kill: kill}
}

// Write forwards p to the underlying writer, then scans it (joined with the carried tail
// of the previous write) for a denial signature; the first match triggers the kill. The
// forward happens first so the output the caller captures is complete even for the write
// that kills the run.
func (w *DenialKillWriter) Write(p []byte) (int, error) {
	written, err := w.next.Write(p)
	if !w.detected.Load() {
		w.scan(p)
	}
	return written, err
}

// Detected reports whether the watch matched a denial signature (and so issued its kill).
func (w *DenialKillWriter) Detected() bool { return w.detected.Load() }

// scan matches the carried tail plus p against the signatures, fires the kill on the
// first match, and otherwise keeps the new overlap tail for the next write.
func (w *DenialKillWriter) scan(p []byte) {
	window := append(w.tail, p...)
	if LooksLikeConfinementDenial(string(window)) {
		w.detected.Store(true)
		w.tail = nil
		w.kill()
		return
	}
	if len(window) > denialSignatureOverlap {
		window = window[len(window)-denialSignatureOverlap:]
	}
	// Copy rather than alias: window may share p's backing array, which belongs to the
	// caller after Write returns. copy has memmove semantics, so the self-overlap when
	// window still aliases w.tail is safe.
	w.tail = append(w.tail[:0], window...)
}

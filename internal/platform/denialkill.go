package platform

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"sync/atomic"
)

// denialLinePattern is the line-anchored OS-denial signature the confined-run watch and
// the terminal's result label share (amended 2026-09-16, ADR 0056 D2): a LINE — its
// trailing " \t\r" trimmed — ends in strerror(EPERM) ("Operation not permitted", what
// seatbelt denials print on macOS) or strerror(EACCES) ("Permission denied", what landlock
// denials print on Linux — its filesystem refusals are EACCES, not EPERM), in libc's
// capitalisation or the lower-cased spelling Go's syscall.Errno prints, followed by one of
// the tails the common toolchains append: nothing; Java's `)`; Rust's ` (os error N)`;
// rsync's ` (N)`; Python's `: '<path>'` / `: "<path>"` (anything to the end of the line, so
// its two-path `'/a' -> '/b'` counts); Perl's ` at <file> line N.`. The anchoring is the
// point: a line that merely CONTAINS the phrase mid-sentence (`permission denied for user x`,
// a quoted log line, a commit message) is not a denial. A signature-free denial (curl's
// exit 23, useradd) is a documented miss. The spellings are POSIX by design — the Windows
// token backend's denials print "Access is denied.", which is deliberately not matched: the
// Windows terminal path has no fail-fast floor either (see FailFastPreamble). Best-effort
// by design — strerror text is locale-dependent, and a missed match costs nothing (the
// model still sees the non-zero exit).
var denialLinePattern = regexp.MustCompile(
	`(?:[Pp]ermission denied|[Oo]peration not permitted)` +
		`(?:\)|\s\(os error \d+\)|\s\(\d+\)|: ['"].*|\sat \S+ line \d+\.?)?$`,
)

// denialErrnoPattern is the second half of the signature: the bare errno names toolchains
// emit (Node's `Error: EACCES: permission denied, open '/x'`, Ruby's `Errno::EACCES`, a
// `write failed: EPERM` at the line's end), bounded on both sides by a non-alphanumeric byte
// or the line's start or end, so an identifier that merely embeds the letters does not match.
var denialErrnoPattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9])(?:EACCES|EPERM)(?:[^A-Za-z0-9]|$)`)

// denialLineTrim is what a line sheds before the anchored match: the CR a PTY appends and
// trailing blanks, so `mkdir: Permission denied\r\n` off a Console matches like its pipe twin.
const denialLineTrim = " \t\r"

// denialLineCarry caps the unfinished line a DenialKillWriter carries between writes: a
// line split across two pipe chunks — inside the phrase or inside a long allowed tail — must
// still match on the next write, but a newline-free flood must not grow the carry without
// bound; only the last denialLineCarry bytes of such a line are kept.
const denialLineCarry = 4096

// looksLikeDenialLine reports whether one line — already trimmed of denialLineTrim — carries
// the anchored signature or a bounded errno name.
func looksLikeDenialLine(line string) bool {
	return denialLinePattern.MatchString(line) || denialErrnoPattern.MatchString(line)
}

// LooksLikeConfinementDenial reports whether a confined command's output carries an
// OS-denial signature on one of its LINES — the heuristic half of confinement-denial
// handling; the structural half (the run was confined) is the caller's to check. The final
// line counts whether or not a newline ends it. It is the same match DenialKillWriter
// applies live, exported so the execution tools label a finished result with the identical
// judgement.
func LooksLikeConfinementDenial(output string) bool {
	for len(output) > 0 {
		line, rest, _ := strings.Cut(output, "\n")
		if looksLikeDenialLine(strings.TrimRight(line, denialLineTrim)) {
			return true
		}
		output = rest
	}
	return false
}

// DenialKillWriter is the live kill-on-denial watch a CONFINED subprocess run is wired
// through (fix A of the 2026-08-22 workspace-clobber incident): an io.Writer placed on the
// child's STDERR path that forwards every byte to next and scans the stream, line by line,
// for an OS-denial signature. The first match calls kill exactly once — the caller hands in
// whatever stops the run, e.g. the CommandContext cancel whose cmd.Cancel kills the whole
// process group — so a script whose command was denied by the confinement fence is stopped
// there instead of running its remaining lines against a half-done state. That job cannot
// be left to `set -e`: POSIX exempts every command of an AND-OR list but the last, so a
// denied `mkdir d && cd d` chain does NOT abort the script, and the unguarded lines after
// it run with the cwd unchanged — the incident's exact clobber.
//
// The match is line-anchored (denialLinePattern): a line whose tail IS a denial, not a line
// that merely contains the phrase. Since 2026-09-16 the pipe path watches stderr alone —
// stdout is a command's data, and the incident this closed (session-mining fc413fb5) was a
// confined `cat` of a log to STDOUT whose lines ended in a real Go denial
// (`open /dev/ptmx: permission denied`), killed as if the cat itself had been denied. The
// PTY console keeps its single stream (a terminal has no second one) under the same anchored
// rule. The watch is still best-effort in both directions and its caller must treat it so:
// the kill races the shell's next command (the denial's stderr reaches the parent through a
// pipe), and a confined command whose stderr line legitimately ends in a signature is killed
// too — that false positive surfaces loudly as a labeled error the model can react to.
// Scanning spans write boundaries: the unfinished line is carried to the next write (capped
// at denialLineCarry), so a line split across two pipe chunks still matches, and the final
// newline-less line is judged on every write.
//
// Write is single-writer by contract (os/exec drives one copier per writer). Detected is
// safe to read from another goroutine after the run.
type DenialKillWriter struct {
	next     io.Writer
	kill     func()
	carry    []byte
	detected atomic.Bool
}

// NewDenialKillWriter returns a watch forwarding to next that calls kill exactly once
// when the stream first carries an OS-denial signature.
func NewDenialKillWriter(next io.Writer, kill func()) *DenialKillWriter {
	return &DenialKillWriter{next: next, kill: kill}
}

// Write forwards p to the underlying writer, then scans it (joined with the carried
// unfinished line of the previous write) for a denial signature; the first match triggers
// the kill. The forward happens first so the output the caller captures is complete even
// for the write that kills the run.
func (w *DenialKillWriter) Write(p []byte) (int, error) {
	written, err := w.next.Write(p)
	if !w.detected.Load() {
		w.scan(p)
	}
	return written, err
}

// Detected reports whether the watch matched a denial signature (and so issued its kill).
func (w *DenialKillWriter) Detected() bool { return w.detected.Load() }

// scan matches the carried unfinished line plus p, line by line — the newline-less tail
// included — fires the kill on the first match, and otherwise keeps that tail (capped at
// denialLineCarry) as the carry for the next write.
func (w *DenialKillWriter) scan(p []byte) {
	window := append(w.carry, p...)
	if LooksLikeConfinementDenial(string(window)) {
		w.detected.Store(true)
		w.carry = nil
		w.kill()
		return
	}
	unfinished := window
	if at := bytes.LastIndexByte(window, '\n'); at >= 0 {
		unfinished = window[at+1:]
	}
	if len(unfinished) > denialLineCarry {
		unfinished = unfinished[len(unfinished)-denialLineCarry:]
	}
	// Copy rather than alias: unfinished may share p's backing array, which belongs to the
	// caller after Write returns. copy has memmove semantics, so the self-overlap when
	// unfinished still aliases w.carry is safe.
	w.carry = append(w.carry[:0], unfinished...)
}

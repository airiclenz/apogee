package tui

import (
	"fmt"
	"io"
	"os"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// ----------------------------------------------------------------------------
// The diagnostic seam — --tui-trace and --tui-diag
// ----------------------------------------------------------------------------
//
// Both are hidden flags, both default off, and both cost nothing when unset: no wrapper is
// installed, no file is opened, and every observation point below is a nil check. They are
// portable — the same seam on every OS, not a Windows one — because a rendering bug is argued
// from the same two artifacts everywhere: the exact bytes the renderer wrote, and what the
// terminal told the program about itself.
//
// The reason they exist in the repo at all is that the alternative is a PATCHED renderer. The
// 2026-08 Windows ghosting investigation had to build apogee against a locally patched
// bubbletea to see either artifact, which is both unshippable and a measurement of a program
// nobody runs. These two seams make the same evidence available from a stock build.

// tracedOutput is the terminal bubbletea paints into when --tui-trace names a file: os.Stdout in
// every respect the renderer can observe, with each write ALSO appended to the trace as one
// quoted Go string per line. That format is deliberate — the escape sequences a renderer emits
// are unprintable, and a quoted string is both readable and mechanically re-parsable, so a trace
// can be replayed through a virtual terminal rather than only eyeballed.
//
// What it holds is the RENDERER's stream and only that. The three screen-control sequences apogee
// sends on its own behalf (claimAltScreen, and its pairing exit) go straight to os.Stdout before
// the program exists and after it has stopped, so they are outside the traced window by
// construction — which is the honest boundary, since they are not what any frame is painted with.
//
// The whole design is one constraint, and it is the thing most easily got wrong here. bubbletea
// decides whether its output IS a terminal by type-asserting it to term.File (tty_windows.go:36,
// tty_unix.go:30), and colorprofile.Detect makes the same assertion on the same value
// (tea.go:1083). A plain io.Writer wrapper satisfies neither: raw mode, VT processing, size
// detection, the cursor-movement optimizations and the colour profile would all silently change,
// so the trace would faithfully record a DIFFERENT program from the one being debugged.
// Satisfying term.File in full — and answering Fd() with the real terminal's descriptor — is
// what keeps a traced run identical to an untraced one. [TestTracedOutputIsATermFile] pins it.
type tracedOutput struct {
	// term is the real terminal, os.Stdout. Every method except Write is ITS behaviour,
	// unchanged and undelegated-to-anything-else, which is the point of the type.
	term *os.File

	// mu serialises the pair of writes below, so a write from bubbletea's exec seam (exec.go:112
	// hands the same output to a suspended child) cannot interleave a trace line with the
	// renderer's. It is held by the pointer this type is always used through — nothing in the
	// value-copied Model holds a tracedOutput (ADR 0011; doc.go).
	mu sync.Mutex

	// trace is the trace file. Writes to it are UNBUFFERED on purpose: the frame worth having is
	// the last one before a crash, and a buffered tail is exactly the part a crash eats.
	trace io.WriteCloser
}

// tracedOutput must be a term.File or the seam measures the wrong program — see the type's doc.
var _ term.File = (*tracedOutput)(nil)

// newTracedOutput returns out wrapped so every write to it is also appended to the file at path,
// which is created if absent and appended to if present (two runs of the same session leave two
// traces in one file rather than one silently destroying the other).
func newTracedOutput(out *os.File, path string) (*tracedOutput, error) {
	// 0o600: a trace holds everything painted on the screen — prompts, model output, file
	// contents — so it is the owner's to read and nobody else's.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &tracedOutput{term: out, trace: f}, nil
}

// Write sends p to the terminal and then records it in the trace, in that order: the terminal is
// what the program is for, and the trace is a record of what was actually sent to it. The
// terminal's result is the one returned — a trace that cannot be written is a lost diagnostic,
// never a broken session, so its error is dropped rather than surfaced into the render loop.
func (o *tracedOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n, err := o.term.Write(p)
	_, _ = fmt.Fprintf(o.trace, "%q\n", p)
	return n, err
}

// Read delegates to the terminal. bubbletea reads input from stdin rather than from its output,
// so nothing calls this; it exists because term.File is an io.ReadWriteCloser and the assertion
// this type is built to satisfy is the whole interface or none of it.
func (o *tracedOutput) Read(p []byte) (int, error) { return o.term.Read(p) }

// Close closes the trace file and deliberately leaves the terminal open. Nothing that hands a
// process its stdout hands it the right to close it, and bubbletea never closes its output
// either — so the only thing this owns is the file it opened.
func (o *tracedOutput) Close() error { return o.trace.Close() }

// Fd answers with the REAL terminal's descriptor. Every capability probe in the stack — raw
// mode, console mode, size, is-this-a-tty — goes through it, so answering with anything else is
// the same mistake as not being a term.File at all.
func (o *tracedOutput) Fd() uintptr { return o.term.Fd() }

// The diag log's keys. They are constants because two places write the width method — start-up
// and the mode report that moves it — and a log whose key drifted between the two is a log
// nobody can grep.
const (
	diagWidthMethod  = "width-method"
	diagSize         = "size"
	diagColorProfile = "color-profile"
	diagUnset        = "(unset)" // an environment variable that is not set, spelled unambiguously
)

// diagEnvKeys are the environment variables that decide how the renderer talks to this terminal,
// in the order the log lists them. TERM is first because it is the one that matters most and the
// one most likely to be empty: an unset TERM gives ultraviolet's painter ZERO capabilities
// (xtermCaps returns noCaps for it), which is the normal state of a Windows shell and was
// invisible before this log existed. The other three name the emulator, which is what a bug
// report has to say.
var diagEnvKeys = []string{"TERM", "COLORTERM", "WT_SESSION", "TERM_PROGRAM"}

// diagLog is the --tui-diag seam: a small text log of the terminal facts a rendering bug is
// argued from, recorded at start-up and then again whenever one of them CHANGES. Change
// suppression is what keeps it small enough to paste into an issue — a size that is re-reported
// every frame, or a mode the terminal answers twice with the same value, says nothing the first
// line did not.
//
// It is held by POINTER in the value-copied Model (doc.go's no-copy invariant): it owns a mutex
// and an open file, and a nil *diagLog is the off state. Every method is nil-safe, so the
// observation points read as one unconditional line rather than a branch each.
type diagLog struct {
	mu   sync.Mutex
	w    io.WriteCloser
	last map[string]string // key → the value last written, the change-suppression memory
}

// newDiagLog opens the log at path, created if absent and appended to if present — the trace
// file's posture, for the same reason.
func newDiagLog(path string) (*diagLog, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &diagLog{w: f, last: map[string]string{}}, nil
}

// record writes "key: value" when value differs from what this key last carried, and nothing at
// all when it does not. A log that cannot be written has nowhere to report that, so the error is
// dropped: the seam is a diagnostic and must never be able to fail a session.
func (d *diagLog) record(key, value string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if prev, seen := d.last[key]; seen && prev == value {
		return
	}
	d.last[key] = value
	_, _ = fmt.Fprintf(d.w, "%s: %s\n", key, value)
}

// start records the facts that are already known before the program begins: the four environment
// variables that decide how the renderer talks to this terminal, and the width method the painter
// (and with it [widthAuthority]) starts on. lookupEnv is injected so a test needs no process
// environment; Run passes os.Getenv.
func (d *diagLog) start(lookupEnv func(string) string, method ansi.Method) {
	if d == nil {
		return
	}
	for _, key := range diagEnvKeys {
		value := lookupEnv(key)
		if value == "" {
			value = diagUnset
		}
		d.record(key, value)
	}
	d.record(diagWidthMethod, widthMethodName(method))
}

// observe folds one message from the running program into the log. It is called at the TOP of
// [Model.Update] and consumes nothing: every message it recognises still reaches the switch
// below it, so switching the log on cannot change what the program does.
//
// The three it recognises are the three the program learns about its terminal from — and the
// mode report is the important one. It carries the terminal's answer to bubbletea's start-up
// DECRQM queries, mode 2027 (Unicode core) among them, which is the answer that decides whether
// the painter measures in grapheme clusters or in wcwidth. bubbletea acts on it and then hands
// it on to Update (tea.go:786-798 does not `continue`), so this is an observation point that
// already exists rather than one this seam had to create.
func (d *diagLog) observe(msg tea.Msg) {
	if d == nil {
		return
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.record(diagSize, fmt.Sprintf("%dx%d", msg.Width, msg.Height))
	case tea.ColorProfileMsg:
		d.record(diagColorProfile, msg.Profile.String())
	case tea.ModeReportMsg:
		if msg.Mode == nil {
			return
		}
		d.record(fmt.Sprintf("mode %d", msg.Mode.Mode()),
			fmt.Sprintf("%d (%s)", msg.Value, modeSettingName(msg.Value)))
	}
}

// Close closes the log file.
func (d *diagLog) Close() error {
	if d == nil {
		return nil
	}
	return d.w.Close()
}

// widthMethodName spells an ansi.Method for the log. The two names are the painter's own
// vocabulary rather than the constants' Go spelling, so a log line reads the same way the
// upstream discussion of this bug does.
func widthMethodName(method ansi.Method) string {
	switch method {
	case ansi.WcWidth:
		return "wcwidth"
	case ansi.GraphemeWidth:
		return "grapheme"
	default:
		return fmt.Sprintf("unknown(%d)", int(method))
	}
}

// modeSettingName spells a DECRQM answer. The numeric value is logged beside it because that is
// what upstream issues and the VT specification quote; the word is there so the line is readable
// without the table.
func modeSettingName(setting ansi.ModeSetting) string {
	switch setting {
	case ansi.ModeNotRecognized:
		return "not recognized"
	case ansi.ModeSet:
		return "set"
	case ansi.ModeReset:
		return "reset"
	case ansi.ModePermanentlySet:
		return "permanently set"
	case ansi.ModePermanentlyReset:
		return "permanently reset"
	default:
		return "unknown"
	}
}

package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/sanitize"
)

// probeTerminalStreams is the seam onto the process's own terminal. Production never reassigns
// it; a test does, to prove the refusal below without needing a pseudoterminal — which is also
// exactly how CI hits this command.
var probeTerminalStreams = func() (in, out *os.File) { return os.Stdin, os.Stdout }

// errProbeTerminalNeedsATerminal is the refusal when either stream is redirected. Unlike the host
// report, this command cannot degrade to a partial answer: every line it prints is the terminal's
// answer to a sequence it wrote, so with nothing on the other end there is no measurement to make
// and a report full of "no answer" rows would be worse than a refusal — it reads like a finding.
var errProbeTerminalNeedsATerminal = errors.New(
	"apogee probe terminal: stdin and stdout must both be a terminal — this command measures the " +
		"terminal by writing sequences to it and reading its replies back, so with either stream " +
		"redirected there is nothing to measure (`apogee probe` is the host report, and it needs neither)")

// errProbeTerminalMismatch is the non-zero exit a MISMATCH earns. The report is already on stdout
// when this is returned: the finding is the product, and the exit status is only what lets a
// script — or an implementation plan's acceptance line — check it without reading the table.
var errProbeTerminalMismatch = errors.New(
	"apogee probe terminal: at least one section reports MISMATCH (the report above says which)")

// probeTerminalCommand builds `apogee probe terminal` — the third half-that-is-not-a-half of
// `probe`: where `probe host` reports what this MACHINE will let apogee do and `probe model`
// reports what the MODEL will do, this one reports what the TERMINAL actually does, by doing it
// and reading the answer back.
//
// It exists because the 2026-08 Windows rendering investigation spent a session on four
// hypotheses that were all statements about a terminal nobody had measured. Every one of them was
// checkable in about a second with DSR-CPR; none of them was checkable from inside a running TUI.
//
// Its cost profile is the host report's, not the battery's: no agent runs, no model is called and
// nothing is written to disk. What it is NOT is read-only about the screen — it paints on the
// alternate screen for the duration and restores the primary one afterwards.
func probeTerminalCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "terminal",
		Short: "Measure what this terminal actually does, and check it against what the renderer assumes",
		Long: "apogee probe terminal measures the terminal instead of trusting it. It writes\n" +
			"sequences and reads the cursor back with DSR-CPR (CSI 6n), so every line it prints\n" +
			"is an observation: how the terminal negotiates modes 2026 and 2027, how far each\n" +
			"glyph apogee paints actually advances the cursor, where its tab stops really are,\n" +
			"what it does when the final column of a row is written, and which cursor and erase\n" +
			"sequences it supports against the set TERM grants the renderer.\n\n" +
			"No agent runs, no model is called and nothing is written to disk. The measurements\n" +
			"are painted on the alternate screen and the primary screen is restored afterwards.\n\n" +
			"It exits non-zero when any section reports MISMATCH, so it can be checked by a\n" +
			"script as well as read.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, out := probeTerminalStreams()
			if !term.IsTerminal(in.Fd()) || !term.IsTerminal(out.Fd()) {
				return errProbeTerminalNeedsATerminal
			}

			report, err := runTerminalProbe(in, out)
			if err != nil {
				return err
			}
			// The report is this command's PRODUCT, so it goes to real stdout — the same split
			// the host and model halves make, and for the same reason: `apogee probe terminal
			// >term.txt` must leave the table in the file.
			//
			// Escape-stripped on the way out (internal/sanitize owns the set and the reasons),
			// like both siblings: every row is built from what the TERMINAL wrote back when the
			// probe asked, so printing it raw would hand the measured terminal a second chance
			// to act on its own answers instead of being described by them.
			fmt.Fprintln(cmd.OutOrStdout(), sanitize.StripEscapes(report.Report()))
			if report.Mismatch() {
				return errProbeTerminalMismatch
			}
			return nil
		},
	}
}

// runTerminalProbe puts the terminal into the state a bubbletea session runs it in, takes the
// measurements, and puts it back. The state matters as much as the measurements: raw mode is what
// stops the shell's line discipline from eating the terminal's replies, and on Windows the output
// handle's console mode is what decides how the last column of a row behaves — so a probe that
// skipped either would measure a terminal apogee never talks to.
func runTerminalProbe(in, out *os.File) (probe.Terminal, error) {
	previousInput, err := term.MakeRaw(in.Fd())
	if err != nil {
		return probe.Terminal{}, fmt.Errorf("apogee probe terminal: could not put the terminal in raw mode: %w", err)
	}
	defer func() { _ = term.Restore(in.Fd(), previousInput) }()

	restoreOutput, err := prepareTerminalOutput(out.Fd())
	if err != nil {
		return probe.Terminal{}, fmt.Errorf("apogee probe terminal: could not prepare the terminal for VT output: %w", err)
	}
	defer restoreOutput()

	width, height, err := term.GetSize(out.Fd())
	if err != nil {
		return probe.Terminal{}, fmt.Errorf("apogee probe terminal: could not read the terminal size: %w", err)
	}

	replies := newReplyPump(in)
	return probe.GatherTerminal(probe.TerminalInputs{
		Out:    out,
		Read:   replies.read,
		Width:  width,
		Height: height,
		Fd:     out.Fd(),
		// The resolved TERM, which is the single input to ultraviolet's xtermCaps and therefore
		// to everything the renderer will and will not emit. On Windows it is normally empty,
		// which is the whole point of printing the capability section beside it.
		TERM: os.Getenv("TERM"),
	}), nil
}

// replyPump reads the terminal's answers off stdin on a goroutine, because a console handle
// cannot be read with a deadline: os.File.SetReadDeadline works only on pollable descriptors and
// a Windows console is not one. Moving the blocking read off the measuring goroutine is what lets
// a silent terminal be reported as silent instead of hanging the command.
type replyPump struct {
	chunks <-chan []byte
}

// newReplyPump starts the pump. Its goroutine outlives a probe that gave up waiting — a read
// already blocked in the kernel cannot be cancelled — which is why this is a short-lived
// command's helper and not a general-purpose reader: the process exits moments later and takes
// the goroutine with it.
func newReplyPump(in *os.File) *replyPump {
	chunks := make(chan []byte, 16)
	go func() {
		defer close(chunks)
		buf := make([]byte, 256)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				select {
				case chunks <- chunk:
				default: // nobody is listening any more; the measurement has moved on
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return &replyPump{chunks: chunks}
}

// read returns whatever the terminal has said, waiting up to timeout for the first byte. It
// returns nil when nothing arrived in time or the terminal's input side closed.
func (p *replyPump) read(timeout time.Duration) []byte {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case chunk, ok := <-p.chunks:
		if !ok {
			return nil
		}
		return chunk
	case <-timer.C:
		return nil
	}
}

//go:build windows

package tui

import (
	"os"

	uv "github.com/charmbracelet/ultraviolet"
)

// ----------------------------------------------------------------------------
// The painter's environment — apogee names the terminal it is talking to
// ----------------------------------------------------------------------------
//
// Windows shells do not follow the Unix TERM convention, so TERM is EMPTY — measured, not
// assumed — in conhost, in Windows Terminal and in VS Code's integrated terminal alike. The
// painter reads exactly that variable to decide what the terminal can do: ultraviolet's
// xtermCaps switches on the first "-"-separated field of TERM, "" matches no case, and the
// renderer is handed noCaps — no VPA, no HPA, no CHA, no ECH, no ICH, no REP, no SD/SU
// (ultraviolet/terminal_renderer.go:170-171 and 1590). A renderer with no CHA and no HPA has no
// absolute column addressing at all: it moves the cursor by counting relative moves against its
// own model of the screen, and when that model is wrong it has nothing left to re-anchor with.
//
// That is what turns a single column error into permanent corruption. One bad advance poisons
// the renderer's belief about a line, the diff renderer then reads those cells as already
// correct, and nothing repaints them until a full redraw — the "only a terminal resize fixes it"
// symptom of the 2026-08 Windows ghosting investigation. The measurements taken there
// (docs/plans/2026-08-06 - 04, item 4) confirmed noCaps in exactly that role: the AMPLIFIER, not
// the trigger. The identical noCaps stream paints correctly in conhost, so this file does not by
// itself fix the ghost — it removes the mechanism that makes any error unrecoverable, and it is
// worth landing on its own merits, because a renderer that believes a modern Windows terminal
// cannot address a column is simply wrong about the machine it is painting.
//
// The correction is to tell bubbletea what terminal it is talking to through the environment IT
// reads: tea.WithEnvironment feeds the same slice to newCursedRenderer and to
// colorprofile.Detect (bubbletea/tea.go:1063 and :1083). The PROCESS environment is never
// touched. The injected slice belongs to the program alone, so nothing apogee spawns — a tool, a
// $EDITOR, a suspended shell — inherits a TERM this machine does not actually have.

const (
	// injectedTerm is the terminal type apogee names itself to the painter as, and it is the
	// configuration macOS already runs on rather than a new one: xtermCaps("xterm-256color")
	// yields allCaps &^ capHPA &^ capCHT &^ capREP — VPA, CHA, CBT, ECH, ICH, SD and SU — which
	// is the proven-good capability set the platform nobody reports ghosting on paints with
	// every day.
	injectedTerm = "xterm-256color"

	// injectedColorTerm is the anti-regression half, and it is not optional. colorprofile.Detect
	// reads the very same slice, and today — with TERM empty — it resolves through the Windows
	// API path to TrueColor on any Windows new enough to have VT at all. Naming "xterm-256color"
	// and stopping there would resolve to ANSI256 wherever WT_SESSION is absent (conhost, VS
	// Code), visibly flattening the palette and the spinner's Oklch gradient. COLORTERM=truecolor
	// restores colorprofile's early TrueColor return (colorprofile/env.go's colorTerm branch), so
	// naming the terminal costs no colour. TestInjectedEnvironmentResolvesTheSameColorProfile is
	// the pin.
	injectedColorTerm = "truecolor"
)

// programEnviron reports the environment [Run] starts the Bubble Tea program with, or nil to
// leave bubbletea on its own default (os.Environ(), tea.go:622) and the program options exactly
// as they have always been.
//
// It injects only on a real terminal. With stdout redirected there is no painter to configure —
// the renderer is writing to a file, where cursor optimizations and colour profiles are noise —
// and the environment bubbletea reports back to the model (EnvMsg) should be the machine's own
// rather than one apogee invented for a screen that is not there.
func programEnviron() []string {
	if !stdoutIsTerminal() {
		return nil
	}
	return injectTerminalEnv(os.Environ())
}

// injectTerminalEnv returns environ with the two terminal-describing variables apogee is willing
// to supply added to it, or nil when it has nothing to add.
//
// The rule is deliberately narrow. A TERM the environment already names is never overridden — a
// WSL, Cygwin or MSYS shell sets a real one, and so does a user who configured it, and either
// knows more about this terminal than a hardcoded default does — and when TERM stands, COLORTERM
// is left alone with it, because the two describe one terminal and apogee has no business
// upgrading half of somebody else's description. COLORTERM is added only when it is absent, for
// the same reason.
//
// "Unset" means unset OR empty, and the lookup is uv.Environ's rather than os.LookupEnv's on
// purpose: TERM= names no terminal type, xtermCaps("") is precisely the noCaps case this file
// exists to remove, and reading it the way the painter will read it is what keeps the decision
// and its consequence in agreement.
//
// It never writes through to environ: the result is a fresh slice, so the caller's environment —
// os.Environ()'s copy in production, a test's literal in tests — is observably unchanged.
func injectTerminalEnv(environ []string) []string {
	env := uv.Environ(environ)
	if env.Getenv("TERM") != "" {
		return nil
	}
	// Appended rather than prepended because every consumer resolves a duplicate key to the LAST
	// occurrence (uv.Environ.LookupEnv scans backwards; colorprofile's environ map takes the last
	// write), so an appended value is the one that would win even if the slice already carried the
	// key some other way.
	injected := make([]string, len(environ), len(environ)+2)
	copy(injected, environ)
	injected = append(injected, "TERM="+injectedTerm)
	if env.Getenv("COLORTERM") == "" {
		injected = append(injected, "COLORTERM="+injectedColorTerm)
	}
	return injected
}

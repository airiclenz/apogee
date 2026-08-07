//go:build windows

package tui

import (
	"os"

	"golang.org/x/sys/windows"
)

// prepareAltScreenConsole sets DISABLE_NEWLINE_AUTO_RETURN on stdout's console buffer BEFORE
// [claimAltScreen] switches to the alternate one, and it is the fix for the Windows ghosting bug.
//
// The mechanism, measured end to end (plan findings 39-45). A Windows console buffer that does not
// carry DISABLE_NEWLINE_AUTO_RETURN translates every bare LF an application writes into CR LF. The
// renderer depends on the untranslated meaning: ultraviolet emits `\r\n` when it wants the next row
// at column 1 and a bare `\n` when it wants the next row at the SAME column, and it then addresses
// cells relative to that preserved column. Translate the LF and every such row is painted 1-based
// instead of column-relative, so the cells the renderer believes it just overwrote are never
// touched — they stay on screen as fragments of an earlier frame. That is the ghost.
//
// bubbletea does set the flag (tty_windows.go:48-52), so on its own this would never bite. What
// makes it bite is the ORDER. [claimAltScreen] runs in [Run] before the program starts, and the
// mode word is per screen buffer: a buffer that is already the alternate one when bubbletea calls
// SetConsoleMode gets the flag on the buffer nobody is writing to any more, and the live alternate
// buffer keeps the default, translating. Setting it here — on the primary buffer, before the
// switch — makes the alternate buffer inherit it, and bubbletea's own later call then lands on a
// buffer that already agrees.
//
// Why this rather than dropping the early claim: the claim is load-bearing on macOS (see
// [claimAltScreen] — the scroll bar Terminal.app leaves lit for the whole run), so the ordering has
// to stay and the flag has to move ahead of it. This is also why the bug is Windows Terminal's and
// VS Code's and not conhost's: in a classic console window VT processing is off when [Run] writes
// the switch, so the switch is not honoured there and the alternate buffer is only entered later,
// by the renderer, after the mode is already set. Under a pseudoconsole VT processing is on from
// the start, the early switch IS honoured, and the window closes on the wrong buffer.
//
// It is deliberately additive — the existing mode word is preserved and one flag is OR'd onto it,
// exactly as bubbletea does, so nothing this does not name is changed. A failure is not an error
// worth failing [Run] over: the flag is an optimisation of correctness on one platform, and a
// terminal that refuses it leaves apogee exactly where it was before this function existed.
//
// Why it returns a restore, and why the restore cannot be left to bubbletea. bubbletea saves the
// output console mode in initInput (tty_windows.go:36-41) and puts it back at shutdown
// (tty.go:47-51) — but it samples the mode AFTER this function has already changed it, so what it
// restores is this function's mode, not the shell's. Left uncorrected, every Windows TUI session
// would hand the shell back a console with newline-auto-return disabled, and the next program to
// write a bare LF — any Go binary, apogee's own multi-line CLI output included — would staircase
// down the screen. The closure is the missing half: it is the only place that still holds the mode
// word as it was before apogee touched anything. It is never nil, so the caller can defer it
// unconditionally, and it is a no-op on every path where nothing was changed.
func prepareAltScreenConsole(f *os.File) func() {
	h := windows.Handle(f.Fd())
	var previous uint32
	if err := windows.GetConsoleMode(h, &previous); err != nil {
		return func() {}
	}
	if err := windows.SetConsoleMode(h,
		previous|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN); err != nil {
		return func() {}
	}
	return func() { _ = windows.SetConsoleMode(h, previous) }
}

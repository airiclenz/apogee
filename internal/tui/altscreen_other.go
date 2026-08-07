//go:build !windows

package tui

import "os"

// prepareAltScreenConsole is the no-op this rule collapses to off Windows.
//
// There is no console mode word anywhere else, and no layer that rewrites a bare LF on the way to
// the terminal: a Unix tty in raw mode delivers `\n` as the renderer wrote it, which is why the
// ghost this guards against has only ever been reported on Windows. See altscreen_windows.go for
// the mechanism and the measurement.
//
// The returned restore is the same never-nil contract the Windows twin keeps — nothing was changed
// here, so there is nothing to put back, but [Run] defers it unconditionally and a nil would panic.
func prepareAltScreenConsole(_ *os.File) func() { return func() {} }

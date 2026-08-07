//go:build !windows

package tui

// programDeclinesSyncOutput is the no-op this rule collapses to off Windows.
//
// The asymmetry is not caution, it is the measurement. Synchronized output is declined on Windows
// because ConPTY forwards the window EMPTY and re-serializes the frame outside it, so the atomicity
// is never delivered and the cursor-hide flicker mitigation bubbletea trades away for it is lost
// for nothing (syncoutput.go, finding 34). No such re-serializing layer stands between apogee and
// the terminal anywhere else; there the mode is honoured, the frame really is presented atomically,
// and declining it would buy the tearing BSU/ESU exists to prevent.
func programDeclinesSyncOutput() bool { return false }

//go:build !windows

package probe

// The screen read-back is a Windows console capability and has no portable counterpart: no VT
// sequence reports what is IN a cell, and the terminals apogee runs on elsewhere keep their buffer
// to themselves. The two measurements that need it — did a hard tab erase what it moved over, did
// ECH/ICH change the row — therefore report "unverified" here rather than a guess, which is the
// same posture the rest of the probe takes towards anything it could not observe.
//
// This twin exists so every one of the six release targets compiles the terminal probe; see
// terminal_windows.go for the real thing.

// consoleRow reports that this OS offers no screen read-back.
func consoleRow(uintptr, int, int) (string, bool) { return "", false }

// consoleCursor reports that this OS offers no console cursor to read beside the VT answer.
func consoleCursor(uintptr) (CursorPos, bool) { return CursorPos{}, false }

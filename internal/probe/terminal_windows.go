//go:build windows

package probe

import (
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The screen read-back, Windows only.
//
// There is no portable VT sequence that reports what is IN a cell — DSR-CPR answers where the
// cursor is and nothing else — so two of the probe's questions cannot be answered by writing
// escape sequences at all: did a hard tab erase the cells it moved over, and did ECH/ICH really
// change the row. Windows answers both, because a console (including the headless one ConPTY runs
// behind Windows Terminal and VS Code) keeps a real screen buffer the process can read.
//
// That is exactly where the questions were asked, so the read-back lives here and the rest of the
// world reports "unverified" (terminal_other.go) rather than guessing. What it reads is the
// console's own buffer: under ConPTY that is headless conhost's model of the screen, which is the
// upstream side of the re-serialization the 2026-08 ghosting investigation ended on — a useful
// thing to know when the buffer and the visible screen disagree.

// procReadConsoleOutputCharacterW reads a run of characters straight out of the active screen
// buffer. golang.org/x/sys/windows does not wrap it, and neither does charmbracelet/x/windows, so
// it is resolved here rather than adding a dependency for one call.
var procReadConsoleOutputCharacterW = windows.NewLazySystemDLL("kernel32.dll").
	NewProc("ReadConsoleOutputCharacterW")

// consoleRow reads width cells of the terminal's row-th visible row (1-based, window-relative)
// and returns them as text. It reports false when the handle is not a console screen buffer or
// the read fails — a diagnostic that cannot be taken is reported as not taken, never as a result.
func consoleRow(fd uintptr, row, width int) (string, bool) {
	if row < 1 || width < 1 {
		return "", false
	}
	handle := windows.Handle(fd)
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(handle, &info); err != nil {
		return "", false
	}

	// ReadConsoleOutputCharacter addresses the BUFFER, which scrolls under the window; the probe
	// counts rows from the top of the visible window, so the origin has to be added back.
	origin := windows.Coord{X: info.Window.Left, Y: info.Window.Top + int16(row-1)}
	cells := make([]uint16, width)
	var read uint32
	rc, _, _ := procReadConsoleOutputCharacterW.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&cells[0])),
		uintptr(uint32(width)),
		uintptr(*(*uint32)(unsafe.Pointer(&origin))),
		uintptr(unsafe.Pointer(&read)),
	)
	if rc == 0 {
		return "", false
	}

	// A cell that was never written reads as NUL. It occupies a column exactly as a space does, so
	// it is rendered as one — otherwise every read-back would compare unequal for a reason that has
	// nothing to do with what is being measured.
	cells = cells[:read]
	for i, c := range cells {
		if c == 0 {
			cells[i] = ' '
		}
	}
	return string(utf16.Decode(cells)), true
}

// consoleCursor reports the cursor position the console API sees, 1-based and window-relative so
// it is directly comparable with the DSR-CPR answer the report prints beside it.
func consoleCursor(fd uintptr) (CursorPos, bool) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(fd), &info); err != nil {
		return CursorPos{}, false
	}
	return CursorPos{
		Row: int(info.CursorPosition.Y-info.Window.Top) + 1,
		Col: int(info.CursorPosition.X-info.Window.Left) + 1,
	}, true
}

package tuitest

import "fmt"

// Key is one keystroke, spelled the only way a terminal can spell one: as the bytes it sends. A
// driver types bytes into a program's input, and what the program makes of them is the key parser's
// business — the same parser the shipped binary uses, reading the same bytes a real xterm would
// have written. Nothing here constructs a tea.KeyPressMsg, because a Msg built in a test proves the
// Model handles a Msg, not that the key reaches it.
//
// The sequences are the xterm ones bubbletea's v2 parser decodes (the legacy table in
// ultraviolet's key_table.go). Each is pinned by TestKeysDecodeAsIntended, which feeds it through a
// real program and reads back the Msg — so a parser change breaks the pin rather than a test that
// happens to use the key.
type Key string

// The keys apogee's own surfaces bind. Arrows and page keys come in pairs because a driver that can
// walk a list one way but not back cannot assert that walking off the end is safe.
const (
	// Enter is CR, not LF: a terminal in raw mode sends \r for the Return key, and \n is a
	// different key event entirely.
	Enter Key = "\r"
	// Esc is a lone escape. The reader resolves it after its 50 ms escape timeout, since until
	// then the byte could still be the start of a sequence — the one key a driver waits on.
	Esc       Key = "\x1b"
	Tab       Key = "\t"
	ShiftTab  Key = "\x1b[Z"
	Space     Key = " "
	Backspace Key = "\x7f"

	Up     Key = "\x1b[A"
	Down   Key = "\x1b[B"
	Right  Key = "\x1b[C"
	Left   Key = "\x1b[D"
	PgUp   Key = "\x1b[5~"
	PgDown Key = "\x1b[6~"

	// AltUp and AltDown are the CSI parameterised form (modifier 3 = Alt) rather than the
	// ESC-prefixed one; both decode to the same event and this is what a modern terminal sends.
	AltUp   Key = "\x1b[1;3A"
	AltDown Key = "\x1b[1;3B"

	CtrlC Key = "\x03"

	// F1–F4 are SS3 sequences and F5 upwards are CSI ~ sequences — the historical split every
	// terminal still carries.
	F1  Key = "\x1bOP"
	F2  Key = "\x1bOQ"
	F3  Key = "\x1bOR"
	F4  Key = "\x1bOS"
	F5  Key = "\x1b[15~"
	F6  Key = "\x1b[17~"
	F7  Key = "\x1b[18~"
	F8  Key = "\x1b[19~"
	F9  Key = "\x1b[20~"
	F10 Key = "\x1b[21~"
	F11 Key = "\x1b[23~"
	F12 Key = "\x1b[24~"
)

// Click and Release are the two halves of one left-button click at a cell, spelled the way a terminal
// in SGR mouse mode (1006) spells one: `ESC [ < 0 ; col ; row M` for the press and the same report
// ending in `m` for the release. x and y are the ZERO-based cell coordinates a [Frame] answers in
// ([Frame.Find], [Frame.Cell]); the report itself is 1-based, so the conversion happens here and a
// caller never does the arithmetic that puts the pointer one row off what it read.
//
// They are [Key] values like every other key because they are the same thing: bytes fed through the
// program's own parser. Nothing here builds a tea.MouseClickMsg — a Msg built in a test proves the
// Model handles a Msg, not that the pointer reaches it — and both are pinned by
// TestKeysDecodeAsIntended alongside the keys.
//
// The press and the release are separate because a terminal sends them separately: apogee's own
// release handler ends a drag and copies a selection (mouse.go), so a driver that pressed without
// releasing would leave the program mid-gesture.
func Click(x, y int) Key { return sgrMouse(x, y, 'M') }

// Release ends the click Click began, at the same cell.
func Release(x, y int) Key { return sgrMouse(x, y, 'm') }

// sgrMouse is the one place the report's shape is written: button 0 (left), the 1-based column and
// row, and the final byte that tells a press from a release.
func sgrMouse(x, y int, final rune) Key {
	return Key(fmt.Sprintf("\x1b[<0;%d;%d%c", x+1, y+1, final))
}

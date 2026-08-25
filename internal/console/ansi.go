package console

import "strings"

// escape is the byte every terminal control sequence starts with (ASCII ESC, 0x1b).
const escape = 0x1b

// bell terminates an OSC sequence in the older, and still most common, spelling; the other
// is ST — ESC \ — which stripEscapes matches as well.
const bell = 0x07

// stripEscapes removes the terminal control sequences a program running under a pseudo-terminal
// emits — CSI (ESC [ … final byte) and OSC (ESC ] … BEL or ST) — from s, so the text handed to a
// model is what a human would SEE on the screen rather than the wire bytes that paint it. Colour,
// cursor movement and window-title sequences all fall in those two families; a lone ESC that
// starts neither is dropped on its own.
//
// An incomplete sequence — one whose final byte has not arrived, because the ring handed back a
// chunk that ends mid-escape — costs only its ESC: the remaining bytes are emitted as text. That
// is the conservative direction. The alternative (swallow to the end of the chunk) would eat real
// output whenever a program prints a bare ESC, and this package holds no cross-read escape state
// by design — a Console's Read is a chunk boundary, not a terminal emulator.
func stripEscapes(s string) string {
	if !strings.ContainsRune(s, escape) {
		return s
	}
	var stripped strings.Builder
	stripped.Grow(len(s))
	for index := 0; index < len(s); {
		if s[index] != escape {
			stripped.WriteByte(s[index])
			index++
			continue
		}
		index += escapeSequenceLength(s[index:])
	}
	return stripped.String()
}

// escapeSequenceLength reports how many bytes of s — which starts at an ESC — belong to a control
// sequence stripEscapes drops. It returns 1 for a lone or incomplete ESC, so the caller always
// makes progress.
func escapeSequenceLength(s string) int {
	if len(s) < 2 {
		return 1
	}
	switch s[1] {
	case '[':
		return csiLength(s)
	case ']':
		return oscLength(s)
	default:
		return 1
	}
}

// csiLength measures a CSI sequence: ESC [ , then parameter bytes (0x30–0x3f), then intermediate
// bytes (0x20–0x2f), then one final byte (0x40–0x7e) which ends it. Anything else means the
// sequence is truncated or malformed and only the ESC is consumed.
func csiLength(s string) int {
	index := 2
	for index < len(s) && s[index] >= 0x30 && s[index] <= 0x3f {
		index++
	}
	for index < len(s) && s[index] >= 0x20 && s[index] <= 0x2f {
		index++
	}
	if index < len(s) && s[index] >= 0x40 && s[index] <= 0x7e {
		return index + 1
	}
	return 1
}

// oscLength measures an OSC sequence: ESC ] , then a payload, then either BEL or ST (ESC \).
// A payload with no terminator in s is truncated and only the ESC is consumed.
func oscLength(s string) int {
	for index := 2; index < len(s); index++ {
		if s[index] == bell {
			return index + 1
		}
		if s[index] == escape && index+1 < len(s) && s[index+1] == '\\' {
			return index + 2
		}
	}
	return 1
}

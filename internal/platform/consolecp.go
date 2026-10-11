package platform

import (
	"bytes"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// codepageUTF8 is the Windows codepage identifier for UTF-8 — the value ConsoleOutputCodepage
// reports off Windows, and one DecodeConsoleOutput never decodes through.
const codepageUTF8 uint32 = 65001

// consoleCodepages maps a Windows codepage identifier to the golang.org/x/text encoding that
// reads it. It holds exactly the codepages x/text provides; a console set to any other page
// (857, 861, 864 and 869 among the OEM ones) has no decoder here and its output passes through.
var consoleCodepages = map[uint32]encoding.Encoding{
	437:  charmap.CodePage437,
	850:  charmap.CodePage850,
	852:  charmap.CodePage852,
	855:  charmap.CodePage855,
	858:  charmap.CodePage858,
	860:  charmap.CodePage860,
	862:  charmap.CodePage862,
	863:  charmap.CodePage863,
	865:  charmap.CodePage865,
	866:  charmap.CodePage866,
	874:  charmap.Windows874,
	932:  japanese.ShiftJIS,
	936:  simplifiedchinese.GBK,
	949:  korean.EUCKR,
	950:  traditionalchinese.Big5,
	1250: charmap.Windows1250,
	1251: charmap.Windows1251,
	1252: charmap.Windows1252,
	1253: charmap.Windows1253,
	1254: charmap.Windows1254,
	1255: charmap.Windows1255,
	1256: charmap.Windows1256,
	1257: charmap.Windows1257,
	1258: charmap.Windows1258,
}

// DecodeConsoleOutput returns b as a string, with each line that is not valid UTF-8 decoded from
// the console codepage. It is pure and compiled on every target, so the decode is testable on any
// host; the codepage comes from ConsoleOutputCodepage at the call site.
//
// The judgement is per line, not per buffer: one console stream can interleave a tool that writes
// UTF-8 with one that writes the OEM codepage, and decoding a UTF-8 line through a codepage would
// turn its multi-byte runes into mojibake of their own. A line is the run of bytes up to and
// including a '\n'. A valid line passes untouched; an invalid one is decoded whole.
//
// A partial UTF-8 rune at the very end of b — the cut a byte cap leaves — is set aside when the
// last line is judged, so the cap alone never flips a UTF-8 line into a codepage decode; it stays
// in the output as it was. Only the last line gets that allowance: anywhere else an incomplete
// rune is a real encoding error.
//
// A codepage with no table entry (65001 included) returns b unchanged, as does a line the decoder
// itself refuses — showing the raw bytes is the best this function can do with them.
func DecodeConsoleOutput(b []byte, codepage uint32) string {
	decoding, ok := consoleCodepages[codepage]
	if !ok || utf8.Valid(b) {
		return string(b)
	}

	var out bytes.Buffer
	out.Grow(len(b))
	for rest := b; len(rest) > 0; {
		line := rest
		if newline := bytes.IndexByte(rest, '\n'); newline >= 0 {
			line = rest[:newline+1]
		}
		rest = rest[len(line):]
		isLast := len(rest) == 0
		out.WriteString(decodeConsoleLine(line, decoding, isLast))
	}
	return out.String()
}

// decodeConsoleLine returns line untouched when it is valid UTF-8 — after setting aside a partial
// trailing rune when isLast — and otherwise decoded through decoding, falling back to the raw
// bytes when the decoder refuses the line.
func decodeConsoleLine(line []byte, decoding encoding.Encoding, isLast bool) string {
	judged := line
	if isLast {
		judged = line[:len(line)-partialRuneSuffix(line)]
	}
	if utf8.Valid(judged) {
		return string(line)
	}

	decoded, err := decoding.NewDecoder().Bytes(line)
	if err != nil {
		return string(line)
	}
	return string(decoded)
}

// partialRuneSuffix returns how many trailing bytes of b are the start of a UTF-8 rune cut short —
// a lead byte followed by fewer continuation bytes than it announces — and 0 when b ends on a
// complete rune or on a byte that cannot begin one.
func partialRuneSuffix(b []byte) int {
	for size := 1; size < utf8.UTFMax && size <= len(b); size++ {
		tail := b[len(b)-size:]
		if utf8.RuneStart(tail[0]) {
			if utf8.FullRune(tail) {
				return 0
			}
			return size
		}
	}
	return 0
}

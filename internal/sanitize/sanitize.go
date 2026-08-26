package sanitize

import "strings"

// StripEscapes removes the C0 control characters — and DEL, their ASCII sibling — from untrusted
// text, keeping the two that ordinary prose is written with: the newline and the tab. A control
// character in a model- or repo-supplied string is an instruction to the terminal rather than a
// character in the text: ESC opens an ANSI sequence — an OSC 52 clipboard write (\x1b]52;...), a
// CSI cursor/screen game, an OSC 8 hyperlink a cell buffer may deliberately honour — BEL rings the
// bell and closes an OSC 52 payload, CR rewinds the line so what follows overwrites what the reader
// already saw, and NUL or DEL takes string length while occupying no display cell, which is the
// same lie to the column math an unstripped ESC tells. Dropping the whole class neutralises all of
// them regardless of how a streamed chunk split a sequence, where dropping ESC alone left the rest
// to arrive intact. Styling a renderer adds afterwards is applied to already-stripped text, so its
// own escapes are unaffected.
//
// The newline and the tab survive because the callers of this form are wrapped BODIES, where they
// are the structure rather than a hazard: a streamed reply, a canonical message, a tool result's
// content, a printed answer. Dropping or folding them there would run paragraphs together and
// flatten a command's output into one line. Text that must stay on one row calls
// [StripEscapesToLine] instead.
//
// The bidi formatting characters go too ([BidiControl]), for the same reason CR does: a
// right-to-left override inside a tool argument reorders the glyphs of a decision row without
// touching a byte the executor sees, so the operator reads one command and approves another.
//
// It is idempotent — every seam may strip twice — and allocation-free on text with nothing to
// rewrite.
func StripEscapes(s string) string {
	// strings.Map returns s itself, unallocated, when it rewrites nothing: the overwhelmingly common
	// case of text carrying no control character at all. An invalid UTF-8 byte is the one thing it
	// rewrites unasked — normalising it to U+FFFD, and paying for the copy — which is benign at a
	// terminal seam, where a lone 0x80 had no display of its own to lose.
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		return dropControl(r)
	}, s)
}

// StripEscapesToLine is [StripEscapes] for text that must stay on ONE line — a label printed beside
// the thing it belongs to — where the two controls a body keeps would forge a second line or a
// false column. They fold to a space rather than surviving, one rune for one rune so a later clip
// counts what the row will hold; every other character goes exactly where [StripEscapes] sends it.
func StripEscapesToLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		return dropControl(r)
	}, s)
}

// StripEscapesAll escape-strips every string in xs with [StripEscapes], returning a new slice (nil
// for nil), so a batch of untrusted labels — an approval request's choices — is sanitized in one
// call.
func StripEscapesAll(xs []string) []string {
	if xs == nil {
		return nil
	}
	out := make([]string, len(xs))
	for i, s := range xs {
		out[i] = StripEscapes(s)
	}
	return out
}

// BidiControl reports whether r is one of the Unicode bidirectional formatting characters — the
// embeddings and overrides U+202A–U+202E, the isolates U+2066–U+2069, and the two marks U+200E and
// U+200F. Every one of them reorders the glyphs around it while leaving the underlying bytes alone,
// which at a display seam means the row can say something other than what it holds.
//
// Deliberately the bidi set and NOT the whole of unicode.Cf, which the INGESTION and STORAGE seams
// drop wholesale (neuterInert in internal/tools, SanitizeContent in internal/library). The
// asymmetry is intended, not an inconsistency to repair later: Cf also holds U+200D ZWJ, which is
// load-bearing inside an emoji sequence, and U+00AD soft hyphen — dropping those where untrusted
// bytes ARRIVE costs nothing, but dropping them at a DISPLAY seam would mangle the user's own prose
// on its way to the screen.
//
// This set is spelled once, here. A copy of it anywhere in the module is a bug: four copies is what
// this package replaced, and the copy that had drifted was the one nobody was reading.
func BidiControl(r rune) bool {
	return r == '\u200e' || r == '\u200f' || // LRM, RLM
		(r >= '\u202a' && r <= '\u202e') || // LRE, RLE, PDF, LRO, RLO
		(r >= '\u2066' && r <= '\u2069') // LRI, RLI, FSI, PDI
}

// dropControl maps a C0 control character — DEL and the bidi formatting characters with it — to
// strings.Map's "drop this rune", and passes every other rune through untouched. Text carrying none
// of them maps to itself, which strings.Map returns without allocating.
func dropControl(r rune) rune {
	if r < 0x20 || r == 0x7f || BidiControl(r) {
		return -1
	}
	return r
}

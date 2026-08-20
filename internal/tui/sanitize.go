package tui

import "strings"

// ----------------------------------------------------------------------------
// The escape-stripping seam — what untrusted text is allowed to carry to a cell
// ----------------------------------------------------------------------------
//
// This file holds the package's security seam: the one rewrite every untrusted string passes
// through before it reaches a display surface, spelled ONCE (ADR 0043) and called from every
// producer-facing entry point in the package. Its rule and the reason the frame needs one are
// stated in doc.go's second invariant — untrusted text is escape-stripped at the SEAM it enters
// the view through, never at each producer — and [stripEscapes]' own comment below says which
// characters go, which two stay, and why.
//
// It lives apart from the transcript it grew up in because it is not the transcript's: the
// scrollback is only one of its callers, beside the tool card, the pop-up rows, the settings
// values and the codec that re-strips a session file on the way back in. Nothing here knows a
// transcript, an entry or a Model — it is pure string work over runes, and that is the whole of
// its contract.

// stripEscapes removes the C0 control characters — and DEL, their ASCII sibling — from untrusted
// text, keeping the two that ordinary prose is written with: the newline and the tab. A control
// character in a model- or repo-supplied string is an instruction to the terminal rather than a
// character in the text, and each of them reaches something this package guards: ESC opens an ANSI
// sequence — an OSC 52 clipboard write (\x1b]52;...), a CSI cursor/screen game, the OSC 8 opener the
// pane's cellbuf deliberately honours — BEL rings the bell and closes an OSC 52 payload, CR rewinds
// the line so what follows overwrites what the reader already saw, and NUL or DEL takes string
// length while occupying no display cell, which is the same lie to the column math an unstripped ESC
// tells. Dropping the whole class neutralises all of them regardless of how a streamed chunk split a
// sequence, where dropping ESC alone left the rest to arrive intact. The styling the renderer adds
// afterwards is applied by lipgloss to already-stripped text, so its own escapes are unaffected.
//
// The newline and the tab survive because THIS package's biggest callers are wrapped bodies, where
// they are the structure rather than a hazard: the streamed buffer (appendToken), the canonical
// message (commitAssistant), a tool result's content (addToolResult, which then splits it into
// detail lines), and everything a session file re-strips on the way back in (transcriptcodec.go).
// Dropping or folding them there would run paragraphs together and flatten a command's output into
// one line. The one-line cells that also strip — a popup row, a settings value — legitimately carry
// neither, and the character that actually rewinds a line beside them, CR, goes either way.
//
// The bidi formatting characters go too (bidiControl), for the same reason CR does: this is the
// DECISION surface. A right-to-left override inside a tool argument reorders the glyphs of the
// approval row without touching a byte the executor sees, so the operator reads one command and
// approves another — the same "read one thing, run another" family as the field flattening beside
// it, and invisible to it, because folding "\n" does nothing to U+202E.
func stripEscapes(s string) string {
	// strings.Map returns s itself, unallocated, when it rewrites nothing: the overwhelmingly common
	// case of text carrying no control character at all. An invalid UTF-8 byte is the one thing it
	// rewrites unasked — normalising it to U+FFFD, and paying for the copy — which is benign at a
	// terminal seam, where a lone 0x80 had no display of its own to lose.
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f || bidiControl(r) {
			return -1 // strings.Map's "drop this rune"
		}
		return r
	}, s)
}

// bidiControl reports whether r is one of the Unicode bidirectional formatting characters — the
// embeddings and overrides U+202A–U+202E, the isolates U+2066–U+2069, and the two marks U+200E and
// U+200F. Every one of them reorders the glyphs around it while leaving the underlying bytes alone,
// which at a display seam means the row can say something other than what it holds.
//
// Deliberately the bidi set and NOT the whole of unicode.Cf, which the INGESTION and STORAGE seams
// drop wholesale (neuterInert in internal/tools, sanitize in internal/library). The asymmetry is
// intended, not an inconsistency to repair later: Cf also holds U+200D ZWJ, which is load-bearing
// inside an emoji sequence, and U+00AD soft hyphen — dropping those where untrusted bytes ARRIVE
// costs nothing, but dropping them here would mangle the user's own prose on its way to the screen.
// The same set is stripped at the two other seams that kept it, internal/title.strippableControl and
// internal/session's id validator; a fourth copy anywhere means one of them has drifted.
func bidiControl(r rune) bool {
	return r == '\u200e' || r == '\u200f' || // LRM, RLM
		(r >= '\u202a' && r <= '\u202e') || // LRE, RLE, PDF, LRO, RLO
		(r >= '\u2066' && r <= '\u2069') // LRI, RLI, FSI, PDI
}

// stripEscapesAll escape-strips every string in xs, returning a new slice (nil for nil), so a
// batch of untrusted labels (an approval request's choices) is sanitized in one call.
func stripEscapesAll(xs []string) []string {
	if xs == nil {
		return nil
	}
	out := make([]string, len(xs))
	for i, s := range xs {
		out[i] = stripEscapes(s)
	}
	return out
}

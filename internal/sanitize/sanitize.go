package sanitize

import "strings"

// StripEscapes removes the ANSI escape sequences and the control characters from untrusted text,
// keeping the two that ordinary prose is written with: the newline and the tab. A control
// character in a model- or repo-supplied string is an instruction to the terminal rather than a
// character in the text: ESC opens an ANSI sequence — an OSC 52 clipboard write (\x1b]52;...), a
// CSI cursor/screen game, an OSC 8 hyperlink a cell buffer may deliberately honour — BEL rings the
// bell and closes an OSC 52 payload, CR rewinds the line so what follows overwrites what the reader
// already saw, and NUL or DEL takes string length while occupying no display cell, which is the
// same lie to the column math an unstripped ESC tells.
//
// An escape sequence goes WHOLE, not just its ESC: a CSI (ESC `[`) through its final byte in `@`–`~`,
// an OSC (ESC `]`) through the BEL or the ESC `\` that terminates it, and any other ESC together
// with the one character that follows it — unless that follower is a newline or a tab, which the
// form keeps or folds and the strip therefore leaves to it, or another ESC, which opens a sequence
// of its own; then the ESC goes alone. A sequence nothing terminates is swallowed to the end of
// its LINE, and the newline survives: a half-escape rendered literally is exactly what the strip
// exists to prevent, while the line after it is text the reader is owed. This supersedes the
// ESC-only drop that `docs/plans/archived/2026-08-26 - 02 - untrusted-text-approval-integrity-plan.md`
// NOTES settled on — only the ESC dropped, the sequence's tail left as visible residue, which
// neutralised a sequence however a streamed chunk split it but painted `[31m` beside a label. The
// tail of a sequence a chunk boundary split still arrives with no ESC ahead of it, so it is still
// inert; it is merely no longer the designed output for a sequence that arrived whole.
//
// Beside the sequences, the whole control class goes: C0 (except the two the form keeps), DEL, the
// C1 range U+0080–U+009F — the 8-bit spellings of CSI and OSC among them, dropped as characters
// rather than parsed as introducers — and the bidi formatting characters ([BidiControl]), for the
// same reason CR does: a right-to-left override inside a tool argument reorders the glyphs of a
// decision row without touching a byte the executor sees, so the operator reads one command and
// approves another. Styling a renderer adds afterwards is applied to already-stripped text, so its
// own escapes are unaffected.
//
// The newline and the tab survive because the callers of this form are wrapped BODIES, where they
// are the structure rather than a hazard: a streamed reply, a canonical message, a tool result's
// content, a printed answer. Dropping or folding them there would run paragraphs together and
// flatten a command's output into one line. Text that must stay on one row calls
// [StripEscapesToLine] instead.
//
// It is idempotent — every seam may strip twice — and allocation-free on text with nothing to
// rewrite.
func StripEscapes(s string) string {
	return strip(s, keepBreaks)
}

// StripEscapesToLine is [StripEscapes] for text that must stay on ONE line — a label printed beside
// the thing it belongs to — where the two controls a body keeps would forge a second line or a
// false column. They fold to a space rather than surviving, one rune for one rune so a later clip
// counts what the row will hold; every other character goes exactly where [StripEscapes] sends it,
// and an unterminated sequence still ends at the newline, which then folds like any other.
func StripEscapesToLine(s string) string {
	return strip(s, foldBreaks)
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
// Deliberately the bidi set and NOT the whole of unicode.Cf, which the INGESTION seam drops
// wholesale (neuterInert in internal/tools, the only wholesale dropper). The
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

// The bytes the sequence parser reads by name.
const (
	escape          = 0x1b // ESC, the 7-bit introducer of every sequence the strip parses
	bell            = 0x07 // BEL, one of the two terminators of an OSC
	csiFinalFirst   = '@'  // a CSI ends at its first byte in this range —
	csiFinalLast    = '~'  // — the parameter and intermediate bytes all sit below it
	stringTerminate = '\\' // ESC \ is the other OSC terminator (ST)
)

// The two forms, named at their call sites: whether the newline and the tab — the two controls
// prose is written with, and the ONE thing the exported forms differ in — go out as they came in
// or fold to a space each.
const (
	keepBreaks = false
	foldBreaks = true
)

// isBreak reports whether r is one of the two controls a form decides about.
func isBreak(r rune) bool { return r == '\n' || r == '\t' }

// strip is the one strip both forms are: the escape sequences parsed and dropped whole, the control
// class dropped, the breaks kept or folded as the form says. Text with nothing to rewrite is
// returned as it is, unallocated — the overwhelmingly common case, and why the seam is cheap. An
// invalid UTF-8 byte in text that IS rewritten comes out as U+FFFD, the rune the decoder hands the
// loop; text with nothing to rewrite keeps its bytes, which is benign at a terminal seam either
// way, where a lone 0x80 had no display of its own to lose.
func strip(s string, fold bool) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return isDropped(r) || (fold && isBreak(r)) }) {
		return s
	}
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == escape:
			i += escapeRunes(runes[i:]) - 1
		case isBreak(r) && fold:
			b.WriteByte(' ')
		case isDropped(r):
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isDropped reports whether r is a character the strip drops on sight, the two breaks aside: the
// C0 controls, DEL, the C1 controls and the bidi formatting characters. ESC is in the set — the
// fast path needs it there — but the loop parses it as a sequence before this predicate is asked.
func isDropped(r rune) bool {
	if isBreak(r) {
		return false
	}
	return r < 0x20 || (r >= 0x7f && r <= 0x9f) || BidiControl(r)
}

// escapeRunes reports how many runes the escape sequence beginning at runes[0] (an ESC) spans,
// and never counts past a newline: a CSI runs to its final byte, an OSC to a BEL or a string
// terminator, and anything else is the ESC plus its follower — except a follower the form keeps or
// folds (a newline or a tab) or one that opens a sequence of its own (another ESC), which the ESC
// leaves behind. An unterminated sequence swallows the rest of its line.
func escapeRunes(runes []rune) int {
	if len(runes) < 2 || isBreak(runes[1]) || runes[1] == escape {
		return 1
	}
	switch runes[1] {
	case '[':
		for i := 2; i < len(runes) && runes[i] != '\n'; i++ {
			if runes[i] >= csiFinalFirst && runes[i] <= csiFinalLast {
				return i + 1
			}
		}
	case ']':
		for i := 2; i < len(runes) && runes[i] != '\n'; i++ {
			if runes[i] == bell {
				return i + 1
			}
			if runes[i] == escape && i+1 < len(runes) && runes[i+1] == stringTerminate {
				return i + 2
			}
		}
	default:
		return 2
	}
	return lineRunes(runes)
}

// lineRunes reports how many runes runes holds ahead of its first newline — the whole slice when
// it has none — which is what an unterminated sequence swallows.
func lineRunes(runes []rune) int {
	for i, r := range runes {
		if r == '\n' {
			return i
		}
	}
	return len(runes)
}

// ClampRunes cuts s to at most n runes, on a rune boundary so the result is always valid UTF-8.
// It returns s itself when it already fits, trims nothing and appends no ellipsis: a caller that
// wants the cut marked (the "…" a ledger row or a delegation label carries) decides that on the
// result, by comparing it with what went in. It is the one clamp for text that must fit a fixed
// budget — a gate's reason, a skill's summary, a delegation's cause — where the budget is a rune
// count and nothing else.
func ClampRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// FirstLine reduces s to the one form every single-line display can paint: the text before the
// first newline, trimmed of surrounding whitespace. A string that holds nothing else comes back
// empty, which is the ABSENT signal a label's callers read — nothing here decides what to do about
// that, only what the line is.
//
// It takes s as it stands. Where a render seam demands untrusted model text be escape-stripped
// first, the CALLER strips it and hands the result in ([StripEscapesToLine] at the headless seam,
// the view's own strip in the TUI): the strip belongs to the seam that paints, the first-line rule
// belongs here, and keeping them apart is what lets one rule serve seams with different strips.
func FirstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

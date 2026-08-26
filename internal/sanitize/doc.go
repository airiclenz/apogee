// Package sanitize holds the one rewrite untrusted text passes through before it reaches a
// terminal — the control-character and bidi-formatting strip — spelled ONCE for the whole
// module (ADR 0043).
//
// It exists because the strip was spelled four times: internal/tui's display seam, internal/title's
// title cleanup, internal/session's id validator, and the headless CLI's answer printer. Three of
// them carried a near-identical copy of the bidi set with three different comments, and the fourth
// — the CLI's — had drifted: it dropped the C0 controls and DEL but not the bidi characters, so the
// same hostile model id printed clean in the pane and reordered on stdout. Keeping four copies in
// step by hand is the failure mode; one package with one set is parity by construction.
//
// The package is stdlib-only (strings) and depends on nothing else in Apogee, which is what lets
// both halves of the binary — the TUI and the CLI — reach one helper, exactly as internal/format
// does for the numbers they both spell. Everything here is a pure function over runes: nothing in
// it knows a Model, a session, a terminal or a config.
//
// Three forms of one strip, differing only in what they do with the two controls prose is written
// with:
//
//   - [StripEscapes] keeps the newline and the tab, because its callers are wrapped BODIES — a
//     streamed answer, a canonical message, a tool result — where they are the structure rather
//     than a hazard.
//   - [StripEscapesToLine] folds each of them to one space, for text that must stay on ONE row —
//     a label printed beside something else — where a surviving newline forges a second line and a
//     surviving tab paints a column the writer never measured.
//   - [StripEscapesAll] is [StripEscapes] over a slice, so a batch of untrusted labels is
//     sanitized in one call.
//
// [BidiControl] is the set the three of them drop beside the control characters, exported because
// two callers need the predicate rather than the rewrite: internal/title folds it into a wider
// "strippable" test, and internal/session REFUSES an id that carries one rather than stripping it.
//
// The seam rule is unchanged by this package existing: untrusted text is stripped at the SEAM it
// enters a display through, never at each producer (internal/tui/doc.go states it for the frame).
// This package owns the spelling of the strip, not the decision of where it runs.
package sanitize

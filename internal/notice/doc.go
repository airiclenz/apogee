// Package notice composes the SENTENCES a Driver shows a user about what a session loaded and why
// it will not send, so the TUI, the headless CLI and the daemon narrate one event with one wording
// instead of three. It is the leaf between internal/domain — whose reports are values, not prose
// (see domain/contextfile.go, which reserves formatting for the host) — and internal/format, which
// spells the numbers those sentences carry.
//
// A composer here returns text and nothing else: no terminal escapes are stripped, no stream is
// chosen, no note is recorded. Each Driver owns that half, because each strips, routes and
// records differently — the TUI adds an ephemeral transcript note, headless writes stderr, the
// daemon logs the anomalies alone. That split is what the Anomaly flag exists for.
//
// Three families today, here for the same reason:
//
//   - ContextFileNotices (contextfiles.go) words what a workspace's context files did — what
//     loaded and how big it was, which file was present but unreadable, and whether the standing
//     system content has outgrown its Budget share. It yields ContextNotice values rather than
//     bare strings, because one Driver shows every line and another keeps only the ones that
//     report something wrong.
//
//   - ServerOffline (serveroffline.go) words the refusal all three Drivers reach when the startup
//     beat never answered. It was spelled out three times before it lived here, bound only by
//     comments asking whoever edited one copy to go and visit the other two.
//
//   - WindowUnknown (window.go) is the honesty line for a binding whose context window nobody
//     could name — the Budget and auto-compaction bind against the window, so with none known
//     they silently do nothing. A const rather than a function: it carries no value at all, and
//     the sentence a session shows and the one an unattended run says are the same sentence.
//
// Sentences are built by concatenation rather than fmt.Sprintf: these are short fixed shapes
// carrying one or two values, and reading the literal in the source is how a reviewer checks the
// wording a user actually sees.
//
// The package depends on internal/domain and internal/format, and on nothing else in Apogee.
package notice

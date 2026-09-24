// Package recall persists Apogee's prompt recall: the per-workspace list of inputs the
// operator has sent, which the prompt box walks with Up/Down (CONTEXT "prompt recall" — the
// session browser keeps the word "history", this does not).
//
// One JSONL file per workspace lives under an injected directory (the composition root passes
// <config-home>/prompts); the Store NEVER reaches for an ambient ~/.apogee itself (ADR 0001),
// and it writes nothing into the project tree. A Store is bound to one workspace at New, so it
// serves the TUI's recall seam directly — AppendPrompt and LoadPrompts name no workspace — with
// no adapter between them; this package does not import the TUI. Each line is a self-describing record — the
// workspace it belongs to, a UTC stamp, and the prompt text — so a filename digest collision
// costs a filtered line, not a wrong recall, and a multi-line prompt still occupies exactly
// one line on disk.
//
// Invariants:
//   - AppendPrompt is consecutive-dedup'd: re-sending the workspace's newest prompt writes nothing,
//     so a repeated send never pushes the prompt before it out of reach.
//   - The file is self-trimming: once it grows past compactAt lines, AppendPrompt rewrites it down
//     to the newest maxEntries records through a temp file and a rename.
//   - LoadPrompts is total: a missing file loads empty, malformed lines are skipped, and foreign
//     records are filtered — a damaged file degrades recall, it never fails a session.
//   - Directory 0700, files 0600: recorded prompts are the operator's own words.
//   - The Store is process-local. Its mutex serializes goroutines sharing one Store but
//     makes no cross-process locking claims in v1 (internal/library's posture): two apogee
//     processes — or two Stores bound to one workspace — appending to one file may interleave.
package recall

---
title: A debug Printf in the PDF dictionary reader writes hostile bytes to stdout
status: open
type: task
priority: 1
labels: [security]
plan: 2026-11-27 - 00 - pdf-parser-hardening-plan.md
created: 2026-11-27T21:27:15Z
---

`internal/doctext/pdfparse/lex.go:550`: `buffer.readDict` runs an unconditional
`fmt.Printf("DEBUG: %T(%v)\n. Skip dict", tok, tok)` when a dictionary key is not a name; every
other debug print in the package is behind `DebugOn`.

Any slightly corrupt PDF writes to process stdout: it corrupts the Bubble Tea frame, breaks the
machine-readable channel of `apogee headless` and the daemon, and breaks the wire-silent engine
invariant (ADR 0031). `%v` of a string token prints attacker bytes raw, so a hostile PDF can inject
terminal escape sequences (OSC 52 clipboard write, title change, cursor moves).

Fix: delete the line (the following `errorf` already carries the text), or gate it on `DebugOn`
and quote with `%q`. List it in the `pdfparse/doc.go` local patches.

Source: `docs/reviews/code-audit-2026-11-27.md` (Medium, independently verified).

## Acceptance

- A PDF with a non-name dictionary key writes nothing to stdout; a test captures stdout to pin it.

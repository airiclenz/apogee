---
title: Context files (AGENTS.md, CLAUDE.md) are read unbounded
status: open
type: task
priority: 2
labels: [security]
plan: 2026-11-28 - 00 - audit-bounds-guards-deadlines-plan.md
created: 2026-11-27T21:27:15Z
---

`internal/agent/contextfiles.go:100-107`: `readContextFile` does `io.ReadAll` with no size bound
and no regular-file check on files a repo controls. The sibling `readFileRef` (`loop.go:1919-1935`)
already caps at 10 MiB. The in-code comment calls the read deliberately unbounded, but a malicious
repo is in the threat model.

A multi-hundred-MB AGENTS.md exhausts memory at session start, `/clear` and resume (several copies
are made); a FIFO at that name blocks session start forever.

Fix: check for a regular file and read through a cap, as `readFileRef` does; refuse or truncate with
a visible notice above the cap. Related: `unbounded-reads-unlinted`.

Source: `docs/reviews/code-audit-2026-11-27.md` (Medium, independently verified).

## Acceptance

- A context file that is a FIFO or over the cap does not block or exhaust memory, and the user sees a notice; tests pin both.

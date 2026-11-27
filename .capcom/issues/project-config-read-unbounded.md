---
title: The Project config is read with no size cap or regular-file check
status: open
type: task
priority: 2
labels: [security]
created: 2026-11-27T21:27:15Z
---

`internal/config/layer.go:385`: `readProjectLayer` reads the repo-controlled
`<root>/.apogee/config.yaml` with an unbounded `os.ReadFile` after only `os.Stat` and
`projectRealPath` — before any Adoption prompt, and again on every live re-read and daemon Firing.

A cloned repo can make the file a symlink to `/dev/zero`, a blocking device or a multi-GB file,
exhausting memory (a watchdog reset on a Pi-class box) or hanging startup. Denial of service only.

Fix: `os.Lstat` and refuse anything not a regular file (one notice, layer skipped, as
`projectwrite.go:154` does for symlinks); read through a size cap (e.g. `io.LimitReader(f, 1<<20)`)
and refuse when exceeded. Related: `unbounded-reads-unlinted`.

Source: `docs/reviews/code-audit-2026-11-27.md` (High lowered to Medium, independently verified).

## Acceptance

- A Project config that is a FIFO, device, or over the cap is skipped with one notice and never read in full; tests pin each case.

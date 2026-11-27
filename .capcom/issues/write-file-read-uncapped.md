---
title: write_file loads an existing file with no size cap
status: open
type: task
priority: 2
created: 2026-11-27T21:27:15Z
---

`internal/tools/write_file.go:118`, `internal/tools/write_target.go:148-149`: `write_file` reads the
existing file with uncapped `target.read()`, once for diff regions and again for the undo
pre-image. Sibling edit tools use `readCapped`.

Overwriting a multi-GB file on an 8 GB box loads it into memory up to twice.

Fix: bound the regions pre-read with `readCapped` (fall back to `okInsertedRegion`); cap or skip the
journal pre-image above a size limit with an explicit "not undoable" note.

Source: `docs/reviews/code-audit-2026-11-27.md` (Medium, independently verified).

## Acceptance

- Overwriting a file over the cap reads at most the cap, and the result says the write is not undoable; a test pins it.

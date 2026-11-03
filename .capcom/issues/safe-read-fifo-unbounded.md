---
title: "`SafeReadFile` and the copy source block on a planted FIFO and read unbounded files"
status: done
type: bug
priority: 2
labels: [security]
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
closed: 2026-11-03T16:07:40Z
reason: Resolved by plan '2026-11-03 - 00 - code-audit-fixes-plan' (archived in c3346711)
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 7 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `SafeReadFile` (internal/security/safeio.go:259, also :270) opens with a blocking read, no regular-file gate and no size bound; `openCopySource` (:337) opens blocking before checking `IsRegular()`. `SafeOpen` was hardened with `O_NONBLOCK` for this case in the 2026-09-20 audit. Callers on model-chosen paths: tools/write_target.go:121, tools/diagnostics.go:191, undo/journal.go:770, `copy_file`.

## Why

`mkfifo x` then a write, edit, diagnostics or `copy_file` on `x` wedges the Turn; a multi-GB planted file exhausts RAM on a small box.

## Acceptance

- `SafeReadFile` and the copy source open non-blocking and refuse non-regular files with `ErrNotRegular`; a FIFO test returns promptly.
- Tool-facing reads (edit, find-replace, diagnostics) go through a capped variant returning `ErrTooLarge`.

---
title: Windows label journal keeps an in-memory entry after a failed flush, so a retry labels with no entry on disk
status: done
type: bug
priority: 2
labels: [windows, residue]
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
closed: 2026-11-03T16:07:40Z
reason: Resolved by plan '2026-11-03 - 00 - code-audit-fixes-plan' (archived in c3346711)
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 17 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `Journal.record` (internal/platform/winlabel/session.go:128) assigns `j.rec.Entries` before `flush()` and does not roll back on failure. The next `LabelTree` finds the entry, returns `changed=false` without flushing, and sets the Low label.

## Why

After a transient journal write failure, a crash leaves Low labels (and a destroyed foreign label) with no record for Recover or Residue, breaking ADR 0020 §2 journal-before-label.

## Acceptance

- A flush failure restores the previous entries (or a dirty flag forces a re-flush before any label write); a test injects a flush failure and checks the retry flushes before labelling.

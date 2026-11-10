---
title: Snapshot git calls share a fixed 60 s deadline that a large workspace on a slow box exceeds
status: open
type: bug
priority: 2
labels: [slow-box]
created: 2026-11-10T19:35:13Z
---

Source: docs/reviews/code-audit-2026-11-10.md ("Fixed 60 s deadline on every snapshot git call"). Verified 2026-11-10 at a5c4f2db. The failure cost is worse than reported, the outcome milder.

**Where:** `internal/snapshot/store.go:25` (`snapshotTimeout = 60 * time.Second`), used by `stageAndWrite` (`:479`, `:482`), `settleWarnings` (`:306`), `dropFromIndex` (`:332`), `Content` (`:559`) and `stream` (`:615`). Caller: `internal/agent/dispatch.go:1837` (`journal.MarkPre` before `runTool`).

**What:** every git call the undo snapshot store makes has the same fixed 60 s ceiling. The slow one is the whole-tree `add -A` of a session's first capture, which hashes every non-ignored file into the store; on a large workspace on SD/USB storage that exceeds 60 s.

**Why it matters:**
- A timed-out capture does not fail fast: `Capture` treats the add's deadline error as a failed add and retries via `captureFresh` — a second full `add -A` into an empty index (another 60 s) plus a whole-tree `ls-files --others`.
- That runs synchronously before the model's first write-capable tool call, so the call stalls ~2 min or more. `MarkPre` leaves `pre` empty on failure, so every later write-capable call in the Exchange pays it again.
- The timeout SIGKILLs git (process-group teardown), leaving `index.lock` in the store; later adds into the persistent index then fail and fall to the full-rehash `captureFresh` every time (inferred from code, not reproduced).
- The error is surfaced (ErrorEvent `undo`) and undo falls back to funnel-only coverage (ADR 0051), so nothing breaks outright — but a slow box ends up with no snapshot undo and slow write calls.

**Suggested fix:** bound a wedge, not the amount of work: a no-progress backstop with a much larger ceiling for `add -A`/`write-tree`, or a budget scaled with the workspace; keep a short bound only on cheap reads (`ls-tree`, `cat-file`). Do not chain `captureFresh` after a deadline, and do not retry a capture that failed for the same reason on every write call in an Exchange. Clear a stale `index.lock` in the store's private directory before an add, or stage into a fresh index after a killed add.

## Acceptance

- A capture whose add exceeds the old 60 s (simulated through a test knob or slow-git fake) completes, or fails once without a second full captureFresh pass.
- After a capture is killed mid-add, the next capture in the same session succeeds against the persistent index (no stale index.lock failure).
- A failed pre-image capture is not retried on every later write-capable call in the same Exchange.
- A wedged git (one that never exits) is still ended by a backstop.

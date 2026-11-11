---
title: A failed heartbeat rebind is never retried until the upstream changes again
status: done
type: bug
priority: 3
labels: [tui]
plan: 2026-11-11 - 00 - pdf-parser-fork-and-defects-plan
created: 2026-11-10T19:36:31Z
closed: 2026-11-11T10:11:35Z
reason: "Resolved by item 8: a failed rebind is retried on a later beat, refusal note still once per target (plan '2026-11-11 - 00 - pdf-parser-fork-and-defects-plan', archived at 87846d40)"
---

Source: docs/reviews/code-audit-2026-11-10.md ("A failed rebind is never retried, leaving the session on a stale model"). Verified PARTLY 2026-11-10 at a5c4f2db.

The mechanism is real; the non-retry is currently pinned by a test, and the report's claim that the `lastRebindFailed` dedupe "is reachable only on a flip-flop" is wrong (it also fires on the same target with a new window).

**Where:** `internal/tui/heartbeat.go` `observeBinding` (:321-361) and `applyRebind` (:377-394).

**What:** `observeBinding` records the beat's model and window as the last observation (:330) before `applyRebind` calls `ServerHost.Rebind`. When the seam errors, the binding stays put (intended), but the observation keeps the new values, so the next beat on the same upstream sees "nothing changed" and the rebind is never tried again. Only a further upstream change, a `/model` pick or a `/settings` edit re-drives it. `TestRebindFailureNotedOnce` (heartbeat_test.go:1189) pins that an identical beat must not re-run the seam.

**Why it matters:** most rebind errors are config errors that fail identically on retry. The exceptions are transient ones — the per-model system-prompt file read in `resolveModelBindings` (`os.ReadFile`) — or a cause the user fixes outside apogee (an edited prompt file). After either, the session stays on the old model, or stays unbound when the failure was the first-contact bind (`oldModel == ""`, sends refused), until the user acts. The heartbeat exists to stop exactly this drift.

**Suggested fix:** on a seam error, restore the pre-capture observed values (or keep a separate "last bound attempt" marker) so a later beat re-detects and retries. Keep `lastRebindFailed` as the once-per-target note guard; consider a backoff so a permanently bad config is not re-resolved every 10 s beat. Update `TestRebindFailureNotedOnce` to the new contract.

## Acceptance

- A rebind that fails once and succeeds on a later identical beat ends bound to the new model, pinned by a test.
- A permanently failing rebind still writes its refusal note exactly once per target.
- The first-contact (unbound) failure case recovers on a later beat without user action.

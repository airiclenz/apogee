---
title: "One context-fill value for every run (architecture review 2026-11-02 #9)"
status: done
type: task
priority: 3
plan: 2026-11-23 - 06 - architecture-review-cleanups-plan.md
created: 2026-11-02T20:02:19Z
closed: 2026-11-24T12:15:31Z
reason: Resolved by the architecture-review-cleanups plan (docs/plans/archived/2026-11-23 - 06 - architecture-review-cleanups-plan.md, archived at 2da7d4506)
---

Found by docs/reviews/archived/architecture-review-2026-11-02.html, verified 2026-11-02 at 00d5bf9f. Verified PARTLY: the top level ignoring the reading's window and sub-agent ~ estimates missing from run views are largely deliberate (rebind rule; plan 2026-11-01-00 out of scope). Revisit once plan '2026-11-02 - 06 - watch-run-projection-plan.md' lands; recommended rule if pursued: keep today's window authority, show child ~ on the run-view gauge only.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.

## Log

- 2026-11-21 Airic Lenz: Triage 2026-11-21 at 7869cca7 (read-only sub-agent check, not re-verified): PARTIAL — viewedFill (46699077) unified gauge and side panel; child ~ estimates still missing (fold.go:295 depth-0 only)

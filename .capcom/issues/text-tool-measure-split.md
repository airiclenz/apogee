---
title: "Text-tool profiles: ContextEstimate counts the instruction block, the guard and bound count the menu JSON"
status: done
type: task
priority: 3
plan: 2026-11-03 - 01 - open-bugs-plan
created: 2026-11-02T20:02:20Z
closed: 2026-11-03T17:58:59Z
reason: "Resolved by item 5 of plan '2026-11-03 - 01 - open-bugs-plan' (archived in b3ffc752): every request measure counts the sent tool surface (instruction block on text-tool profiles) via one helper."
---

Found by docs/reviews/architecture-review-2026-11-02.html, verified 2026-11-02 at 00d5bf9f. On a text-tool profile the request measures disagree on what the tool menu weighs. Left out of plan '2026-11-02 - 05 - request-room-and-delegate-bounds-plan.md' by the verifier's recommendation (change nothing now).

## Acceptance

- Every request measure counts the same tool-menu bytes on a text-tool profile, or the split is documented as deliberate.

---
title: "ADR 0035 addendum of 2026-09-18: the small umbrella's fold is view state and writes nothing"
---

Every `✦ Tools (N calls)` umbrella folds from its header now, and size alone says whose state the
fold is. An umbrella is **large** by its type-row count against `ui.tools-fold-over` and nothing
else — not whether the Turn is running, not whether every call has returned — so the 2026-09-17
addendum's condition that the umbrella had settled is gone, and a large umbrella inside a sub-agent's
run view, opened while the parent Turn runs, folds and writes `ui.tools-open` exactly as one at the
prompt does. A **small** umbrella folds on a flag of its own head entry: **view state**, in-session
only, never written to `config.yaml`, never serialized into the session record, so it starts every
session open. It stays outside this record's set on purpose — there is no key for it to write, and
decision 2's fence admits a gesture only when it lands on a registry-declared key the user can find in
`/settings`; a small fold has none, and the set stays at four. The 2026-09-17 sentence that a small
umbrella, or one with a call in flight, has no fold and that its header click closes its open
children is superseded: child state survives beneath a fold and is there again on reopen. A small
umbrella that grows large follows `ui.tools-open` from then on, its own flag ignored while it is
large. Plan `docs/plans/2026-09-18 - 01` carries the ratified calls.

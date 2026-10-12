---
title: ADR 0036 context
---

ADR 0028 gave apogee
a `/server` picker and, with it, a **second** description of what an upstream server is. Its
decision 5 added the file-only `servers:` list — one entry carrying `name`, `endpoint`, optional
`api-key`, optional `model` — *beside* the top-level `endpoint:` / `api-key:` / `host-alias:` /
`model:` quadruple that had described the startup server since the first release. The two say the
same four things in two shapes: one block per server in the list, four sibling scalars for the one
the session starts on. The seam is visible in the code that has to bridge it — `serverEntry`'s doc
comment explains that `APOGEE_API_KEY` "is a single value and it belongs to the STARTUP server (the
top-level `endpoint:`)", and `upstreamChoices` synthesizes a row for that startup server on every
launch, carrying a duplicate-name edge (the synthesized alias colliding with a configured `name`)
that exists only because the quadruple is not already a list entry.

ADR 0028 also declined, explicitly *for now and not on principle*, "a `--save` form for `/server`,
and a `server:` startup key naming an entry": session-scoped was the smaller, reversible claim and
matched `/confine off`-without-`--save`. What that leaves is a picker whose most ordinary use —
move to the box you actually work on — is forgotten at quit, and whose only durable form is hand
surgery on `endpoint:`, the very fact the list beside it already states.

ADR 0035 then built the
machinery that makes the schema change mechanical rather than delicate: a declarative key registry
with a bijection guard against `fileConfig`'s yaml tags (a schema key without a registry row is a
test failure), and a comment-preserving splice writer that persists one named key per deliberate
edit, verified by re-parse and written atomically. `ISSUES.md` carried the request to retire the
quadruple and named its open design calls — which entry a session starts on, the override story,
the empty-list first run, migration — and said it needed grilling and its own record. A 2026-08-05
grill (two rounds of owner questions) settled all four. This record is that ratification; it is
implemented by `docs/plans/2026-08-05 - 05 - servers-single-definition-plan.md`.

The constraints it lands inside:
ADR 0024 — the engine
is never constructed without an endpoint (`errMissingEndpoint`), and the heartbeat, not config, is
what discovers a model;
ADR 0023 §8 and
`seedConfig`'s pinned never-overwrite invariant — the config file is a document the user owns;
ADR 0035's authorized write set and its explicit rejection of boot-time auto-sync;
ADR 0029 — Launch
profiles are the launcher's, and never ride in `servers:`;
ADR 0031
— every Driver stays viable, so nothing safety- or startup-critical may exist only as an
interactive TUI flow;
ADR 0011 — the renderer is thin
and its Model is copied by value.

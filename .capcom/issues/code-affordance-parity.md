---
title: apogee-code feature parity — user-facing affordances not yet ported
status: parked
type: feature
priority: 3
aliases: [apogee-09k]
created: 2026-09-03T18:40:19Z
---

**Status:** parked 2026-06-25; most of the surface has since shipped (`CHANGELOG.md` and the
archived plans hold that record). Additive TUI/UX layers on top of the agent core, which is
already at parity. Scope is
*user-facing* parity with the apogee-code VS Code extension (`airic-lenz.apogee-code` v0.2.58)
only — the by-design Phase-4 items were tracked separately.

**Verification note (the source-of-truth correction):** apogee-code's `Apogee-Code-TDD.md`
claims it has *no slash commands, only `@file`*. **That doc is stale.** When porting, treat the
shipped webview (`~/.vscode/extensions/airic-lenz.apogee-code-0.2.58/media/chat.js`, array `Ws`)
as the behavioral oracle, not the TDD. On send the webview posts `{text, skillIds, fileRefs}`.

**Remaining:**

- **Server / model switching** — **every switch SHIPPED (2026-07-28 the two user-facing ones,
  2026-07-29 the local-server half, 2026-08-12 the profile half); the request-side remainder
  below was demoted from `[P1]` on 2026-08-14 (owner call).** The shipped bodies have
  left this file for their authoritative records
  ([ADR 0028](docs/adr/0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md)
  for `/model` + `/server`,
  [ADR 0029](docs/adr/0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md)
  for the launcher,
  [ADR 0044](docs/adr/0044-model-profiles-are-per-model-and-mostly-shipped.md) for the profile).
  **Remaining:**
  - **Sampling params on the Model profile — deferred, demand-driven (owner call 2026-08-14):
    covered launch-side for launcher-managed servers.** apogee deliberately sends no sampling
    params ([ADR 0046](docs/adr/0046-the-engine-bounds-every-reply-with-an-output-cap.md)
    leaves temperature et al. unset), so the server's own defaults win — and for a
    llama-launcher-managed server those defaults ARE the Launch profile's flags (`--temp`,
    `--top-p`, …), which is the owner's whole workflow. Request-side knobs would matter only for
    an endpoint the launcher did not start (a remote OpenAI-compatible server, LM Studio, a cloud
    API) or for changing sampling mid-session without a model reload; neither need has appeared.
    Pick up only on a real ask; the grill it then needs must keep the request-side "Model profile"
    and launch-side "Launch profile" namespaces from colliding in the UX. Its one prerequisite is
    already discharged: the field-wise `SetSampling` merge landed 2026-08-13, so a profile that
    sets only temperature no longer nils the engine's stamped cap.
    [ADR 0025](docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md) §6 still
    defers the user-after-tool wire risk to this layer by name.

**Related (parked below):** per-tool approval overrides (`toolApprovalOverrides`:
automatic/ask-first/excluded) — apogee-code surfaces this in config; apogee has the internal
disposition table but no user-facing override. See *Configurable tool × mode security matrix*.

**Standing rules the shipped work left behind:** the `model-profiles:` shape table grows one
entry per *sighting*, never per guess ([ADR 0044](docs/adr/0044-model-profiles-are-per-model-and-mostly-shipped.md)).
What has shipped out of this entry since it was parked is recorded in `CHANGELOG.md` and the
archived plans and ADRs it names — only the open remainder above lives here.

---
title: ADR 0001 consequences
---

- The loop must expose a **public** Go package (it cannot be buried in `internal/`).
- The bench measures the *library build* of the loop, not the literal shipped binary;
  fidelity is protected by a `go.mod` version pin.
- "Embeddable, steppable, no process-global state, copyable conversation state" becomes a
  Phase-0 design constraint on the loop — get it wrong and neither forking nor stepping
  is possible later.
- **Bench isolation by default**: because state roots are Config-injected, the bench
  points the Library/sessions at ephemeral directories, so sim runs never read or write the
  developer's production Library — runs stay reproducible and the production Library is
  never flooded by a narrow sim distribution. A deliberate opt-in "reduced-weight bleed" of
  sim observations into production may be added later *if* it proves worthwhile; it is not
  built by default.
- The public API is **not bench-only**: it is a deliberate **third-party embedding
  surface** — other applications can run an Apogee agent in-process. The TUI, the
  optional `apogee headless` CLI, and the bench are all consumers of this one package.
  This raises the bar on API stability and versioning (semver, a guarded public surface)
  and makes the `internal/` vs public boundary a product decision, not an afterthought.
- **Co-development & versioning.** apogee-sim uses a `go.mod replace` → a local apogee path
  during active development (the bench measures the working tree); a pinned version/commit is
  used only for archived A/B evidence. The public API is **v0.x with no stability promise
  through Phase 3, and `v1.0.0` is cut at the end of Phase 3**, once every consumer (TUI,
  bench, optional `headless`) has exercised it. Events and Hook points stay **additively
  extensible** (a new variant is a minor bump). Seed types the bench needs (e.g.
  `OrderingConstraints`) **move into apogee** and the bench imports them — never the reverse.

  > **Amendment 2026-06-25 (P3.16 — `v1.0.0` cut).** Phase 3 is complete and **`v1.0.0` is
  > tagged**. The "v0.x, no stability promise" clause above is now spent: **semver begins.** The
  > frozen v1 public surface is the root `apogee` package (`Agent`/`New`/`Resume`, `Config` and
  > its host delegates `EventSink`/`Approver`/`Asker`/`ExternalEffects`, the four-rung `Mode`
  > ladder, the `Tool`/`ToolRegistry` extension point and its `ReadOnlyTool`/`ExternalEffectTool`
  > markers, the `Event` variants, and the hook points); tools are an open extension point behind
  > the registry (ADR 0002), not root types. Events and hook points remain **additively
  > extensible** — a new Event variant or hook point is a **minor** bump, not a break. Phase-3
  > public-surface additions reviewed at the freeze (§3 D7 of the Phase-3 detail plan): the
  > `Asker` host delegate (struct-typed for additive growth) and the `ModeAllowEdits` constant.
  > The changelog is tracked from this release in [`CHANGELOG.md`](../../../../CHANGELOG.md).

  > (Amended 2026-09-30: that v1.x series was an accidental pre-production run. The project
  > restarted at v0.x on 2026-07-23, the v1 tags were deleted, and `go.mod` carries
  > `retract [v1.0.0, v1.8.0]` so `@latest` resolves to the current 0.x release. Versioning is
  > deliberately 0.x while pre-production — `AGENTS.md`, "Distribution", and `VERSION`.)

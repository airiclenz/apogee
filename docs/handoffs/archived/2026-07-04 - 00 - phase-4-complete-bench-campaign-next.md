# Handoff — Phase 4 complete, `v1.2.0` rolled up — the bench campaign is next

**Date:** 2026-07-04. **Status: Phase 4 is DONE.** Every catalogued Mechanism is registered,
config-gated, self-regulating, and provably drivable from an external module. The accumulated
CHANGELOG entries are rolled into a **`[1.2.0]`** section (additive minor — sanity-checked against
the `v1.1.0..HEAD` diff: the public facade only gains symbols). **No tag, no push — cutting
`v1.2.0` stays owner-run.** This doc is the pickup point for the work that Phase 4 deliberately
left out of scope: the **bench A/B campaign**, which is apogee-sim work in the apogee-sim repo.

## Where things stand
- `main` carries Phase-4 items 1–16 (see the ✅ notes in `docs/plans/phase-4-detail-plan.md`).
- Working tree builds; the bench-readiness regression (`benchreadiness_test.go`, item 15) is green.
- The catalogue (`docs/design/mechanism-catalogue.md`) is **ratified and its ledger closed**: every
  ported Mechanism carries a shipping item and a **`pending`** bench validation; every
  DROP/FOLD/SPLIT/DEFER row carries its verdict (mirrored in `TODO.md`'s deliberate-trail section).
- **Pinned port source:** apogee-sim @ `d22086701ff9ba8e5565f9587945d6d97434b646`. apogee-sim is
  read-only reference; no apogee code imports it, and Phase 4 changed nothing inside it (ADR 0001).

## What the bench (apogee-sim) must now build and run
The plan ends with apogee *benchable*, not *benched*. Every catalogued Mechanism ships **default-off
(D1)**; its default flips ON only after its own bench A/B passes (ADR 0009 gate). Those flips are
**one-line follow-ups in apogee, gated on bench wins** — not part of any apogee plan.

1. **Import apogee as a library and drive the loop in-process.** The bench is the ADR 0001 consumer:
   construct `apogee.New`/`Resume`, `Step` to quiescent boundaries, `Snapshot`/fork counterfactuals,
   enable Mechanisms via `Config`, and register experimental hooks at the five hook points. The
   in-repo executable contract to mirror is **`benchreadiness_test.go`** — if the bench harness and
   that test diverge, the test is the ground truth of "how apogee is driven".
2. **The two-arm aggregate A/B (the hard-constraint gate).** Run a **mechanisms-on** arm against a
   **Bypass** arm (the same code path, Mechanisms off / structure on) over the task suite, and prove
   the distributional **non-inferiority** gate of **ADR 0009** — apogee-with-Mechanisms is never
   worse than Bypass. Bypass is the honest floor: exempt off-ramps still fire, Budget + Compaction
   still run.
3. **Per-mechanism leave-one-out A/Bs.** For each Mechanism, an on-vs-off arm to attribute its
   marginal effect. A Mechanism that wins its leave-one-out (and clears the aggregate gate) earns its
   **default-ON flip** in apogee. Exempt-from-suppression ≠ exempt-from-validation — the off-ramps'
   leave-one-out stays pending like everyone else's.
4. **The longitudinal Library experiment.** The Library's payoff is cross-session, so it needs its own
   protocol: prove it **improves over sessions AND never drops below baseline** on the same model +
   fingerprint, across a run series into one `LibraryDir`. Confidence gates injection (a
   low-confidence metadata-label identity observes but does not inject; only a weights-hash identity
   injects today — the behavioral-probe/medium tier is Phase 5).
5. **Feed wins back as default flips.** Each passing Mechanism → a one-line default change in apogee +
   a ledger update (`pending` → the winning evidence). No flip ships un-vetted.

## Known limitation the bench must decide on (surfaced by item 15, not fixed here)
**An external module cannot enable apogee's *catalogued* Mechanisms today.** The `mechanisms:` enable
path lives in `cmd/apogee`, and `mechanisms.Build` + the constructor table are `internal/` — there is
no *public, library-level* API to turn a catalogued Mechanism on by ID. `benchreadiness_test.go`
stands in for that enable path in-repo only because it is allowed to import `internal/mechanisms`; an
out-of-tree bench cannot. The bench has two viable paths, and **choosing between them is the first
bench-side decision**:
- **(a)** drive apogee through its own **experimental hooks** (the bench's native instrument — always
  public) plus the `cmd/apogee` config for whole-binary arms; or
- **(b)** motivate a **new apogee plan item** that adds a public library-level enable surface (e.g. a
  `Config`-level enabled-ID set, or an exported builder) if the bench needs to arm catalogued
  Mechanisms in-process without spawning the CLI.
This is a **known limitation, not a defect** — Phase 4's scope was "benchable via the config + the
experimental-hook surface", which item 15 proves. Record the chosen path when the bench harness lands.

## Explicitly NOT done here (owner / Phase 5 / bench)
- **Cutting `v1.2.0`** (tag + push) — owner-run.
- **Per-mechanism default flips to ON** — one-line follow-ups gated on the bench wins above.
- **The behavioral-probe fingerprint / `apogee probe`** (medium confidence tier) — Phase 5; the seam
  ships in item 13.
- **Any change inside apogee-sim, any apogee import of apogee-sim, any `sim`/`bench` subcommand** —
  ADR 0001 (rejected options stay rejected).

## Pointers (don't re-read into context unless you need them)
- Catalogue + closed ledger: `docs/design/mechanism-catalogue.md`
- Plan + ✅ result/NOTES trail: `docs/plans/phase-4-detail-plan.md`
- The A/B decision rule (the gate): `docs/adr/0009-the-ab-decision-rule.md`
- Embeddable-library / bench contract: `docs/adr/0001-*.md`; Bypass floor: `docs/adr/0006-*.md`
- The executable bench contract to mirror: `benchreadiness_test.go`
- Review-fixes ground truth (R1–R5): `docs/plans/archived/phase-4-review-fixes-plan.md`
- Release notes: `CHANGELOG.md` (`[1.2.0]`); deferral trail: `TODO.md`

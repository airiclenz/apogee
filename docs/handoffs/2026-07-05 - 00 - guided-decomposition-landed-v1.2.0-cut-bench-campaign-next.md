# Handoff — Guided decomposition landed, `v1.2.0` cut — the bench campaign is next

**Date:** 2026-07-05. **Status: `guided_decomposition` (ADR 0014) is DONE; `v1.2.0` is tagged
and pushed.** This doc **supersedes** the phase-4 handoff
(`archived/2026-07-04 - 00 - phase-4-complete-bench-campaign-next.md`): everything that
handoff left open is folded in below, updated where the guided-decomposition work sharpened
it. The pickup is unchanged in kind — the **bench A/B campaign**, which is apogee-sim work in
the apogee-sim repo.

## Where things stand

- **`v1.2.0` is cut**: annotated tag at `aa5d268` (the phase-4 CHANGELOG-rollup commit, per
  the `v1.1.0` precedent), pushed to `origin`. The tag target was already on `origin`, so the
  remote is self-consistent.
- `main` additionally carries **guided decomposition, items 1–6 complete** — plan + ✅ trail:
  `docs/plans/archived/guided-decomposition-plan.md`; ratified design + dated Realisation
  note: `docs/adr/0014-…md`; code: `internal/mechanisms/guided_decomposition.go`. Working
  tree clean; gates green (item 5 is the loop-level fan-out acceptance incl. `-race`).
- `guided_decomposition` ships **default-off (D1)** with `Requires: [tool_result_cap]`
  (enable-time registry gate, `ErrMissingRequirement`) and `IncompatibleWith: [decompose]`.
  Its bench validation is **pending**, like every catalogued Mechanism.
- CHANGELOG: `[1.2.0]` is released; **`[Unreleased]` holds the guided-decomposition entries**
  ("Post-`v1.2.0`", additive minor) — the next cut is a `v1.3.0`, owner's call on timing.
- `origin/main` is current through the guided-decomposition work (`5631457`); only the
  commit carrying this handoff may still need a push.

## What the bench (apogee-sim) must build and run — carried forward, still not done

Nothing bench-side has started. The five-step campaign from the phase-4 handoff stands:

1. **Import apogee as a library and drive the loop in-process** (the ADR 0001 consumer).
   The executable ground truth of "how apogee is driven" is `benchreadiness_test.go`.
2. **Two-arm aggregate A/B**: mechanisms-on vs **Bypass** (ADR 0006 floor), proving the
   distributional **non-inferiority** gate of ADR 0009.
3. **Per-mechanism leave-one-out A/Bs** to attribute marginal effect. **For
   `guided_decomposition` this is leave-the-STACK-out** — it is benched as
   `guided_decomposition + tool_result_cap` (the `Requires` relation; it cannot arm alone).
4. **The longitudinal Library experiment** (improves over sessions AND never below baseline,
   same model + fingerprint, one `LibraryDir` across a run series).
5. **Feed wins back as one-line default-ON flips** in apogee + ledger updates
   (`pending` → the winning evidence). No flip ships un-vetted.

## First bench-side decision — carried forward, now sharpened

An external module still cannot enable apogee's *catalogued* Mechanisms by ID (the enable
path lives in `cmd/apogee`; `mechanisms.Build` and the constructor table are `internal/`).
The two paths from the phase-4 handoff remain: **(a)** drive apogee through the public
experimental hooks + `cmd/apogee` config for whole-binary arms, or **(b)** motivate a new
apogee plan item adding a public library-level enable surface.

**New since 2026-07-04:** whichever path is chosen must express **multi-ID stack arming** —
enabling `guided_decomposition` without `tool_result_cap` refuses to boot
(`ErrMissingRequirement`), so a single-ID enable affordance is no longer sufficient. If (b)
is chosen, grill the surface into the domain model first (ADR 0014 is the precedent for how
a Mechanism-adjacent surface gets ratified before code). Record the chosen path when the
bench harness lands.

## Owner-run residuals (apogee-side, not bench work)

- **Cut `v1.3.0`** when desired — `[Unreleased]` already isolates the guided-decomposition
  content; the cut is a rollup commit + annotated tag, per the `v1.1.0`/`v1.2.0` precedent.
- **Linux landlock live-enforcement proof** — still open in CHANGELOG → "Known post-release
  verification" (the macOS seatbelt arm is confirmed ✅; the landlock arm needs a
  `CONFIG_SECURITY_LANDLOCK`-enabled kernel).

## Explicitly NOT next (parked, evidence- or grill-gated)

- **Depth-1 relaxation** of the guided-decomposition gate (ADR 0014 §5 — additive, evidence-gated).
- **Mid-Exchange auto-compaction** (parked in `TODO.md` 2026-07-05 — its own future grill).
- **Constant tuning** (subtask bounds, gate thresholds) — bench evidence, not code-review taste.
- **Fan-out TUI affordances** (queue/chip progress display) — additive, unplanned.
- Any change inside apogee-sim history, any apogee import of apogee-sim, any `sim`/`bench`
  subcommand — ADR 0001 (rejected options stay rejected).

## Pointers (don't re-read into context unless you need them)

- Guided decomposition: `docs/adr/0014-…md` (+ Realisation note) · CONTEXT.md entries
  (Guided decomposition, Mechanism descriptor) · plan trail
  `docs/plans/archived/guided-decomposition-plan.md`
- Bench contract: ADR 0001; Bypass floor: ADR 0006; the A/B gate: ADR 0009; executable
  drive contract: `benchreadiness_test.go`
- Catalogue + closed ledger: `docs/design/mechanism-catalogue.md`; deferral trail: `TODO.md`
- Release notes: `CHANGELOG.md` (`[1.2.0]` released · `[Unreleased]` = guided decomposition)
- Predecessor handoff: `archived/2026-07-04 - 00 - phase-4-complete-bench-campaign-next.md`

## Suggested skills for the next session

- **`/grill-with-docs`** — if the bench needs path (b), grill the public enable surface into
  the domain model before writing any plan or code.
- **`/implement-plan`** — if a new apogee-side plan comes out of that grill.
- **`/code-review`** (or `/code-review ultra`) — before merging any new public-surface work;
  the v1 API is under semver, so a breaking change needs a deliberate call.
- **`/verify`** — on a landlock-enabled Linux box, to close the live-enforcement residual.
- **`/handoff`** — at the end of the bench-harness session, superseding this doc.

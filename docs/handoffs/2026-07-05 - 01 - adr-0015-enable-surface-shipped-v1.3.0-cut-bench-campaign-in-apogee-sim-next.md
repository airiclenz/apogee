# Handoff — ADR 0015 enable surface shipped, `v1.3.0` cut — the bench campaign (apogee-sim) is next

**Date:** 2026-07-05. **Status: the public Mechanism enable surface (ADR 0015) is DONE;
`v1.3.0` is tagged and pushed.** This doc **supersedes**
`archived/2026-07-05 - 00 - guided-decomposition-landed-v1.2.0-cut-bench-campaign-next.md`:
its "first bench-side decision" is **closed** — path (b) was chosen, grilled, ratified, and
implemented. The pickup is the **bench A/B campaign**, which is apogee-sim work — worked
**from this directory**: apogee-sim is the sibling repo at `../apogee-sim`
(`/Users/ericesins/Repos/Airic/apogee-sim`).

## Where things stand (apogee)

- **`v1.3.0` is cut**: annotated tag at `1f6d3aa` (the CHANGELOG-rollup commit, per the
  `v1.1.0`/`v1.2.0` precedent), pushed; `main` == `origin/main`; tree clean; gates green
  (`go build`/`go test ./...`, benchreadiness under `-race`).
- **The enable surface shipped** (ADR 0015, all 6 plan items): `Config.EnableMechanisms
  []MechanismID` (New/Resume build catalogued Mechanisms internally, merged into the
  experimental-hook registry), `apogee.CataloguedMechanisms()` + descriptor/Capability/
  SuppressionPolicy exports, matchable `ErrMissingRequirement`/`ErrUnknownMechanism`,
  wire.go collapsed to a YAML→ID-list producer (YAML surface unchanged), and
  `benchreadiness_test.go` now a TRUE external-surface consumer (no `internal/*` mechanism
  imports). Design + realisation note: `docs/adr/0015-…md`; plan trail:
  `docs/plans/archived/mechanism-enable-surface-plan.md`.
- CHANGELOG: `[1.3.0]` is released; there is **no `[Unreleased]` section** — the next change
  re-creates it.
- Every catalogued Mechanism remains **default-off (D1)**; flips are ADR 0009
  evidence-gated — that evidence is what the campaign produces.

## Where things stand (apogee-sim, `../apogee-sim`)

- `go.mod` carries `replace github.com/airiclenz/apogee => ../apogee`, so the bench sees the
  enable surface immediately; **`v1.3.0` is the citable surface for evidence** ("benched at
  v1.3.0").
- **`internal/coreagent` (P1.7) already drives apogee in-process through the public API** —
  `Run(ctx, RunConfig{Endpoint, Model, WorkspaceDir, Task, MaxTurns})` steps to Exchange
  completion, records every `apogee.Event`, reads the workspace back; `ScoreFileEdit` judges
  the outcome. Proven hermetically (scripted `httptest` model) under `-race`. It arms an
  **empty registry today** — growing arm construction (`EnableMechanisms` + `Bypass`) is the
  campaign's first code.
- **Pending P1.7 residual (quick win, do first):** the live-model eval — point
  `RunConfig.Endpoint` at the live server (`http://192.168.64.1:1111`) and run the same code
  path against a real model. Pending because the build container does not route to the
  server; **this host does**. See apogee-sim `CLAUDE.md` (coreagent entry).
- Caveats when reading apogee-sim docs: its `internal/bench` is the **retired proxy-era**
  A/B (an `X-Apogee-Bypass` header — not applicable in-process), and parts of its
  CLAUDE.md/CONTEXT still speak proxy-era vocabulary; the pivot is recorded in its
  `docs/handoffs/2026-06-22 - 02 - STRATEGIC-PIVOT-…md`. apogee-sim keeps its **own
  glossary** (Sim, Baseline, Intervention are bench terms, not Apogee terms — apogee
  CONTEXT.md "The bench").

## The campaign (five steps; step 1 essentially done)

1. ~~Import apogee as a library and drive the loop in-process~~ — **done** (coreagent),
   minus the live-eval residual above. The executable drive contract is apogee's
   `benchreadiness_test.go`, now written entirely against the public surface.
2. **Two-arm aggregate A/B**: full default-ON candidate set vs **Bypass** (ADR 0006 floor),
   proving the ADR 0009 non-inferiority gate. Needs: arm construction on coreagent, a task
   corpus, paired scoring, and the stats machinery (task-blocked Wilcoxon; noise-floor δ
   from A/A calibration; BH FDR for the separate superiority selection).
3. **Per-mechanism leave-one-out A/Bs** — arm sets are now computable from
   `apogee.CataloguedMechanisms()` (`Requires` traversal; `guided_decomposition` is benched
   as a stack with `tool_result_cap`, leave-the-STACK-out). A compiling example lives in
   apogee's root `example_test.go`.
4. **The longitudinal Library experiment** (improves over sessions AND never below baseline;
   same model + fingerprint, one `LibraryDir` across a run series).
5. **Feed wins back as one-line default-ON flips** in apogee + catalogue-ledger updates
   (`docs/design/mechanism-catalogue.md`). No flip ships un-vetted.

## First bench-side decisions (design, then plan — in apogee-sim)

The harness design has real choices worth a short grill against apogee-sim's own docs
before writing its plan: where the harness lives (grow `coreagent` vs a new package —
the old `internal/bench` is proxy-era, likely not the home), the task corpus (the
`sim-prompts/` inventory and `internal/sim` quality scorers exist — reuse vs new
file-edit corpus), the run/persistence format for evidence, and the stats implementation.
apogee-sim's `docs/plans/` is empty — the campaign plan will be its first.

## Owner-run residuals (apogee-side, unchanged)

- **Linux landlock live-enforcement proof** — still open in CHANGELOG → "Known post-release
  verification" (macOS seatbelt arm confirmed ✅; needs a `CONFIG_SECURITY_LANDLOCK` kernel).

## Explicitly NOT next (parked, evidence- or grill-gated)

- **Default-ON flips without ADR 0009 evidence** — the campaign produces the evidence first.
- **A public Mechanism-authorship SPI** or a `cmd/apogee` headless subcommand — both
  rejected with path (a) (ADR 0015 Considered options; ADR 0002 stands).
- **Depth-1 relaxation** (ADR 0014 §5), **mid-Exchange auto-compaction** (TODO.md
  2026-07-05), **constant tuning**, **fan-out TUI affordances** — all carried forward
  unchanged from the predecessor handoff.
- Any apogee import of apogee-sim, any `sim`/`bench` subcommand in apogee — ADR 0001.

## Pointers (don't re-read into context unless you need them)

- Enable surface: `docs/adr/0015-…md` (+ Realisation note) · plan trail
  `docs/plans/archived/mechanism-enable-surface-plan.md` · public examples in
  `example_test.go` · drive contract `benchreadiness_test.go`
- Bench contract: ADR 0001; Bypass floor: ADR 0006; the A/B gate: ADR 0009; the
  guided-decomposition stack: ADR 0014
- Catalogue + ledger: `docs/design/mechanism-catalogue.md`; deferral trail: `TODO.md`
- apogee-sim ground truth: `../apogee-sim/CLAUDE.md` (coreagent, P1.7) ·
  `../apogee-sim/internal/coreagent/` · pivot handoff
  `../apogee-sim/docs/handoffs/2026-06-22 - 02 - …md`
- Release notes: `CHANGELOG.md` (`[1.3.0]` released, no `[Unreleased]` yet)
- Predecessor handoff:
  `archived/2026-07-05 - 00 - guided-decomposition-landed-v1.2.0-cut-bench-campaign-next.md`

## Suggested skills for the next session

- **`manage-llm-server`** — first, to confirm what's running at the live endpoint before the
  coreagent live eval.
- **`/grill-with-docs`** — the bench-harness design (arms, corpus, persistence, stats),
  grilled against **apogee-sim's** CONTEXT/CLAUDE/handoffs; ADR 0015 was this session's
  precedent for grill-before-plan.
- **`/implement-plan`** — for the campaign plan that comes out of that grill (it will live in
  `../apogee-sim/docs/plans/`).
- **`/code-review`** — before merging the harness; apogee-sim has its own conventions.
- **`/handoff`** — at the end of the bench-harness session, superseding this doc.

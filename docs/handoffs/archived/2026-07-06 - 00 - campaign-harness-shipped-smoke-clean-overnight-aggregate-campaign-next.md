# Handoff — Campaign harness shipped + smoke-verified; the overnight aggregate campaign is next

**Date:** 2026-07-06. **Status: the Campaign instrument (apogee-sim) is DONE and
live-smoke-verified; the harness plan is COMPLETE and archived.** This doc **supersedes**
`archived/2026-07-05 - 01 - adr-0015-enable-surface-shipped-v1.3.0-cut-bench-campaign-in-apogee-sim-next.md`:
its pickup — "the bench A/B campaign is apogee-sim work" — is built. What remains is
**operator work**: run the first overnight aggregate campaign and read its evidence.
Work from this directory; apogee-sim is the sibling repo at `../apogee-sim`.

## Where things stand (apogee — unchanged)

- `v1.3.0` tagged at `1f6d3aa`; `main` == `origin/main`; tree clean. The enable surface
  (ADR 0015) is the campaign's drive contract; nothing apogee-side changed this session.
- Every catalogued Mechanism remains **default-off (D1)**; flips are ADR 0009
  evidence-gated — the campaign now exists to produce exactly that evidence.
- Owner residual (unchanged): Linux landlock live-enforcement proof (CHANGELOG "Known
  post-release verification").

## Where things stand (apogee-sim)

- **The Campaign instrument shipped in full** — all 10 items of
  `docs/plans/archived/campaign-harness-plan.md` (status COMPLETE): corpus loader,
  `CandidateArm()`/`LeaveOneOutArms()` over the public catalogue, single-run executor,
  evidence-bundle store with model-fingerprint refusal, resumable sequential scheduler,
  exact Wilcoxon (Pratt) + split-half δ + BH FDR, analyze/report, and the
  `apogee-sim campaign run / analyze <id> / list` CLI. Design: apogee-sim ADR 0012;
  protocol: the plan's ratified D1–D10 table; full record: apogee-sim `CHANGELOG.md` top
  entry. `main` is **ahead of origin** (unpushed) — push when convenient.
- **The item-10 live smoke ran clean (2026-07-06):** campaign
  `gemma-4-e4b-it-qat-20260706-smoke` — 4/4 runs (2 tasks × 2 arms × 1 rep), 0
  `infra_failed`, ~15 min. `fix-off-by-one` A/A (gate pass both arms);
  `implement-lru-cache` C/C (gate fail both arms). Bundle valid at
  `~/.apogee-sim/campaigns/gemma-4-e4b-it-qat-20260706-smoke/`.
- **Known wrinkle, deliberate:** the smoke's verdict prints `inferior`. Structural at
  reps=1, NOT a harness bug — δ=0 (split-half needs ≥2 reps) and N=2 pairs can never
  reach α=0.05, so the "inconclusive ≠ pass" posture correctly refuses. The label
  conflates "no evidence" with "proven worse"; a distinct zero-evidence wording is a
  flagged improvement for the step-3 plan (see the plan's item-10 close-out NOTES). At
  reps=5 over the full corpus this artifact disappears.
- **Server state at handoff:** `llama-launcher` has `gemma-4-e4b-it-qat` loaded on
  `127.0.0.1:1111` (llama.cpp). Verify with `llama-launcher status --json` before
  launching anything — the campaign REFUSES to resume if the model fingerprint changed
  (deliberate, no override hatch), so don't switch profiles mid-campaign.

## The pickup: the first overnight aggregate campaign (operator work)

From `../apogee-sim`:

```
apogee-sim campaign run --model gemma-4-e4b-it-qat --endpoint http://127.0.0.1:1111 --reps 5
```

- Full corpus = 14 eligible tasks × 2 arms × 5 reps = **140 runs, est. 7–12 h** (smoke
  averaged ~3.7 min/run → ~8.7 h). Strictly sequential, SIGINT-safe; resume with
  `--id <campaign-id>`; `--reps` tops up incrementally; `analyze <id>` is safe
  mid-campaign (reports partial-N honestly).
- Then `apogee-sim campaign analyze <id>` → `report.md` is the first real **ADR 0009
  aggregate non-inferiority verdict** (candidate `compatibleBaseStack()` vs Bypass
  floor), with a genuine split-half δ this time.
- Read the verdict against ADR 0009's disposition rule before touching any default:
  non-inferior ≠ flip; superiority selection is a separate, BH-FDR-controlled claim.

## After that (each step gets its own session)

1. **Second campaign on `qwythos-9b`** (profile exists; coreagent live-eval confirmed
   2026-07-05).
2. **Step 3 — per-mechanism leave-one-out**: `LeaveOneOutArms()` + BH FDR are already
   shipped; the execution protocol gets its own grill + plan (fold in the zero-evidence
   report-wording fix flagged above).
3. **Step 4 — longitudinal Library experiment**; **step 5 — feed wins back** as
   one-line default-ON flips in apogee + catalogue-ledger updates
   (`docs/design/mechanism-catalogue.md`). No flip ships un-vetted.

## Housekeeping done this session (2026-07-06)

- apogee-sim: plan close-out committed; completed plan moved to
  `docs/plans/archived/` (references in CHANGELOG.md + ADR 0012 repointed); **16 of 17
  pre-pivot handoffs archived** to `docs/handoffs/archived/`. The STRATEGIC-PIVOT
  handoff (2026-06-22 - 02) stays active deliberately — it's cited as "Source of
  direction" by this repo's merge plan and anchors apogee-sim's handoff lineage.
- Three borderline items were judged mooted-by-pivot when archiving (resurrect from
  `archived/` if that call was wrong): the `reconstructIssues` single-error nit
  (2026-06-10 - 01), the stepwise-lab fs-confinement follow-up (2026-06-10 - 02;
  confinement now lives in apogee, ADR 0012), and the never-implemented
  `sim-results-dir-config` plan (2026-06-12 - 00; `$APOGEE_SIM_HOME` covers the live
  instruments).
- apogee: `docs/plans/implementation-plan-apogee-merge.md` deliberately NOT archived —
  Phase 5 and Phase 4's "backed by an A/B" half are still open; it remains the master
  roadmap.

## Explicitly NOT next (parked, evidence- or grill-gated — carried forward)

- **Default-ON flips without ADR 0009 evidence** — the overnight campaign produces the
  evidence first.
- A public Mechanism-authorship SPI / `cmd/apogee` headless subcommand (rejected, ADR
  0015); depth-1 relaxation (ADR 0014 §5); mid-Exchange auto-compaction (TODO.md);
  constant tuning; fan-out TUI affordances.
- Any apogee import of apogee-sim, any `sim`/`bench` subcommand in apogee — ADR 0001.

## Pointers (don't re-read into context unless needed)

- Campaign instrument: `../apogee-sim/internal/campaign/` · apogee-sim ADR 0012 · plan
  trail `../apogee-sim/docs/plans/archived/campaign-harness-plan.md` (D1–D10 + all
  NOTES) · CLI in `../apogee-sim/cmd/apogee-sim/main.go`
- Smoke evidence bundle: `~/.apogee-sim/campaigns/gemma-4-e4b-it-qat-20260706-smoke/`
- The A/B gate: apogee ADR 0009 · Bypass floor: ADR 0006 · enable surface: ADR 0015 ·
  bench contract: ADR 0001
- Catalogue + ledger: `docs/design/mechanism-catalogue.md`
- Predecessor handoff: `archived/2026-07-05 - 01 - adr-0015-enable-surface-shipped-v1.3.0-cut-bench-campaign-in-apogee-sim-next.md`
- Pivot lineage: `../apogee-sim/docs/handoffs/2026-06-22 - 02 - STRATEGIC-PIVOT-…md`

## Suggested skills for the next session

- **`manage-llm-server`** — first: confirm `gemma-4-e4b-it-qat` is still the loaded
  profile before `campaign run` (fingerprint refusal makes a silent model swap a hard
  stop, which is what you want — but check first anyway).
- **`/grill-with-docs`** — when the aggregate evidence is in and step 3 (leave-one-out)
  is reached: grill its execution protocol against apogee-sim's docs before writing its
  plan (precedent: ADR 0012 / ADR 0015 grill-before-plan).
- **`/handoff`** — at the end of the campaign-evidence session, superseding this doc.

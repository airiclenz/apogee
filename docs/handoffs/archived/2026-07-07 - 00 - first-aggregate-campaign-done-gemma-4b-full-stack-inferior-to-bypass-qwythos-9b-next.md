# Handoff — First aggregate campaign is DONE; the full stack is *inferior* to Bypass on gemma-4b; qwythos-9b campaign is next

**Date:** 2026-07-07. **Status: the first overnight aggregate campaign ran to completion
(140/140, 0 `infra_failed`) and the ADR 0009 aggregate non-inferiority gate FAILED on
`gemma-4-e4b-it-qat` — the candidate `compatibleBaseStack()` is measurably *worse* than the
Bypass floor on this model.** This is real reps=5 evidence with a calibrated split-half δ, **not**
the reps=1 "inferior" artifact the smoke produced. This doc **supersedes**
`2026-07-06 - 00 - campaign-harness-shipped-smoke-clean-overnight-aggregate-campaign-next.md`:
its pickup — "run the first overnight aggregate campaign and read its evidence" — is DONE.
Work from this directory; apogee-sim is the sibling repo at `../apogee-sim`.

## The result (don't re-derive — read the bundle)

- **Campaign `gemma-4-e4b-it-qat-20260706`**: 14 tasks × 2 arms × 5 reps = 140 runs, **complete,
  0 `infra_failed`**, nothing excluded. Ran ~10 h wall across resumes.
- **Verdict: gate FAILS.** One-sided Wilcoxon signed-rank (Pratt) on per-task diffs
  (candidate − bypass) shifted by **δ = 0.4048** (split-half A/A null, 95th pct):
  **W⁺ = 66.0, p = 0.2087** (α = 0.05, N = 14 paired tasks) → candidate is **not** non-inferior.
  Point estimate points the *wrong* way: candidate mean grade **2.400** vs bypass **2.757**
  (≈ −0.36/task).
- **Every secondary favors Bypass** (consistent direction, not a noisy near-miss): gate-pass
  33 vs **45**/70 · compile 70% vs **87%** · tests 147/53 vs **188/22** · lint-clean 8 vs **21**/70.
- Full tables, per-task diffs, protocol: **`report.md` + `analysis.json` in the bundle** —
  `~/.apogee-sim/campaigns/gemma-4-e4b-it-qat-20260706/` (do not paste them back into context;
  read the file).

## Disposition per ADR 0009 — nothing flipped, nothing should have

Read against `docs/adr/0009-the-ab-decision-rule.md` (§Disposition + §Aggregate composition):

- The **aggregate Bypass non-inferiority test is the shipped guarantee** and it **failed**, which
  is the *"gate fails → cannot ship"* row at the system level. So **no default-ON flip** — the
  opposite of flip evidence. All catalogued Mechanisms remain **default-OFF (D1)** in apogee,
  exactly where they were. **Nothing in apogee changed this session and nothing should.**
- Superiority/FDR selection never opens — it is closed/hierarchical behind passing the gate, and
  we did not pass.
- **Scope caveats (load-bearing, don't overread):** δ is calibrated *per (suite × model ×
  temperature)*, so this condemns the full-ON set **on one small 4B model**, not the stack
  universally. And the aggregate cannot say *which* mechanism(s) drag it down — a net-negative
  full set is exactly the case leave-one-out attribution exists to dissect.

## The pickup: second campaign on `qwythos-9b` (operator work, own session)

The real question this result raises: does a **more capable** model benefit from the stack where
the 4B does not? `qwythos-9b` profile exists (`~/LL-Models/qwythos-9b`; coreagent live-eval
confirmed 2026-07-05). From `../apogee-sim`, after loading that profile on `llama-launcher`:

```
apogee-sim campaign run --model qwythos-9b --endpoint http://127.0.0.1:1111 --reps 5
```

then `apogee-sim campaign analyze <id>` → `report.md`. Same disposition discipline: non-inferior
≠ flip; superiority is a separate BH-FDR-controlled claim.

## Operational lessons from this run (READ before launching the next campaign)

- **A ~10 h campaign must run in a terminal YOU own, not as a harness background task.** Twice
  this session the campaign was launched via the harness `run_in_background` mechanism and twice it
  was **reaped by remote-control mode** (SIGINT-safe, so it shut down cleanly and lost nothing —
  46, then 53 runs preserved). The harness also (correctly) **blocks a detached daemon** as
  "unauthorized persistence." The working recipe: run it yourself with
  `nohup caffeinate -i <apogee-sim campaign run …> >> ~/campaign-run.log 2>&1 &` in a terminal you
  leave open. `caffeinate -i` stops idle-sleep; `nohup` survives terminal close.
- **Resume is free and idempotent:** `campaign run --id <id> …` re-checks the model fingerprint,
  skips completed runs (`runs.jsonl` is the ledger), and `--reps` tops up. It **hard-refuses if the
  loaded model fingerprint changed** (no override) — so **don't switch the `llama-launcher` profile
  mid-campaign**. Verify with `llama-launcher status --json` before every launch/resume.
- `analyze` is a short foreground command (seconds), unaffected by the reaping — safe to run from
  inside a Claude session anytime, even mid-campaign (reports partial-N honestly).

## After that (each step its own session)

1. **`qwythos-9b` campaign** (above) — the per-model comparison that tells us if this is a
   small-model effect or the stack being wrong generally.
2. **Step 3 — per-mechanism leave-one-out**: `LeaveOneOutArms()` + BH FDR are already shipped
   (apogee-sim). Greedy backward elimination to find whether a *subset* clears the floor, or which
   mechanism(s) are net-harmful on 4B. **Grill its execution protocol against apogee-sim's docs
   before writing its plan** (precedent: ADR 0012 / ADR 0015 grill-before-plan). Fold in the
   zero-evidence report-wording fix flagged in the harness plan's item-10 close-out — **note:** that
   fix targets the *reps=1* case; this reps=5 "inferior" label was correct, so it's a lower priority
   than the qwythos-9b evidence.
3. **Step 4 — longitudinal Library experiment**; **step 5 — feed wins back** as one-line
   default-ON flips in apogee + `docs/design/mechanism-catalogue.md` ledger updates. No flip ships
   un-vetted — and on current evidence there is **no win to feed back yet**.

## Repo / tree state at handoff

- **apogee:** no commits, no working changes this session; defaults untouched. (`main` shows ahead
  of `origin/main` from a stale local ref — pre-existing, not from this session.)
- **apogee-sim:** `main` remains **ahead of origin (unpushed)** — the campaign harness; push when
  convenient. Working tree clean; the locally-built `apogee-sim` binary is gitignored. I ran
  `make build` to produce it — rebuild with `make build` in `../apogee-sim` if it's gone.
- **Evidence bundle** (outside both repos, not version-controlled):
  `~/.apogee-sim/campaigns/gemma-4-e4b-it-qat-20260706/` — keep it; it's the ADR 0009 record for
  this model.

## Explicitly NOT next (parked — carried forward)

- **Any default-ON flip** — the aggregate evidence on 4b says *don't*, and no other model has been
  measured yet.
- **Deleting/rejecting mechanisms off this one aggregate failure** — the aggregate can't attribute
  blame; that's leave-one-out's job, and it's model-specific evidence.
- A public Mechanism-authorship SPI / `cmd/apogee` headless subcommand (rejected, ADR 0015);
  depth-1 relaxation (ADR 0014 §5); mid-Exchange auto-compaction (TODO.md); constant tuning.
- Any apogee import of apogee-sim, any `sim`/`bench` subcommand in apogee — ADR 0001.

## Pointers (don't re-read into context unless needed)

- **This campaign's evidence:** `~/.apogee-sim/campaigns/gemma-4-e4b-it-qat-20260706/report.md`
  (+ `analysis.json`, `runs.jsonl`, `traces/`)
- The A/B decision rule: **`docs/adr/0009-the-ab-decision-rule.md`** · Bypass floor: ADR 0006 ·
  enable surface: ADR 0015 · bench contract: ADR 0001
- Campaign instrument: `../apogee-sim/internal/campaign/` · apogee-sim ADR 0012 · plan trail
  `../apogee-sim/docs/plans/archived/campaign-harness-plan.md` (D1–D10 + NOTES) · CLI in
  `../apogee-sim/cmd/apogee-sim/main.go`
- Catalogue + ledger: `docs/design/mechanism-catalogue.md`
- Master roadmap (still open, NOT archived): `docs/plans/implementation-plan-apogee-merge.md`
  (Phase 5 + Phase 4's "backed by an A/B" half)
- Predecessor handoff (now superseded): `2026-07-06 - 00 - campaign-harness-shipped-…-next.md`

## Suggested skills for the next session

- **`manage-llm-server`** — first: load the `qwythos-9b` profile and confirm it's the running
  model *before* `campaign run` (the fingerprint refusal makes a silent swap a hard stop).
- **`/grill-with-docs`** — when step 3 (leave-one-out) is reached: grill its execution protocol
  against apogee-sim's docs before writing its plan.
- **`archive-handoffs`** — the superseded `2026-07-06 - 00` handoff can be archived to
  `docs/handoffs/archived/` (this doc replaces it).
- **`/handoff`** — at the end of the qwythos-9b evidence session, superseding this doc.

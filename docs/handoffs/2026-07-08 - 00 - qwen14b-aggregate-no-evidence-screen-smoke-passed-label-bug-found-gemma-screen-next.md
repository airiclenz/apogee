# Handoff — qwen25-coder-14b aggregate DONE (no-evidence, zero variance); Screen smoke PASSED (item 7 done); descriptive-label bug found; gemma Screen next

**Date:** 2026-07-08. **Status: two campaigns completed cleanly on `qwen25-coder-14b`
(the 140-run aggregate and the 34-run Screen smoke — plan item 7 is DONE, all acceptance
criteria met); the analyzer wording bug the smoke found is now FIXED (commit `48cd48f`, step 1).**
This doc **supersedes** `2026-07-07 - 01 - screen-shipped-qwythos-abandoned-…`: its
pickup items 1 (smoke) and 2 (second-model aggregate) are DONE — the aggregate ran on
the **14B**, not the 7B (operator's call; the 7B never ran and doesn't need to).
Work from this directory; apogee-sim is the sibling repo at `../apogee-sim`.

## The goal (decision frame for everything below)

**Curate a sensible mechanism set for apogee: keep what helps where it can, remove or
gate what hurts.** Every next step is ranked by how directly it serves that. The
evidence so far:

| Model | Bundle | Verdict | Meaning for the goal |
|---|---|---|---|
| gemma-4-e4b-it-qat (4B) | `gemma-4-e4b-it-qat-20260706` | **inferior** | The full stack HURTS a small model. *Which* mechanism carries the harm is unknown → that's the gemma Screen's job. |
| qwen25-coder-14b | `qwen25-coder-14b-20260707` | **no-evidence** | Zero effect on graded outcomes for a capable coder (see findings). Grades can't discriminate here — but wall-time can (see finding 3). |
| qwythos-9b | `qwythos-9b-20260707` (16/140) | abandoned | Model unfit (think-block death spirals); record only. |

Working hypothesis the evidence now supports: **the stack is outcome-neutral on capable
models and harmful on weak ones** — so "gets out of the way" is the binding constraint,
and the gemma Screen (attribution of the harm) is the critical path.

## Findings this session (2026-07-07 evening; don't re-derive — bundles + reports exist)

1. **qwen25-coder-14b aggregate: `no-evidence`, and it's real, not a rig bug.**
   140/140 in 3 h 30 m, 0 infra_failed. Every one of the 14 tasks produced the *same
   letter grade in all 10 of its runs* (both arms, all reps) → zero non-zero paired
   diffs → Wilcoxon min attainable p = 1.0. Byte-identical secondary aggregates between
   arms (mean 1.786, gate 15/70, compile 71%, tests 95/65, lint 10/70). **Wiring was
   verified before believing it**: manifest arms differ only by `Bypass: false/true`
   (correct bypass-floor design), and candidate-vs-bypass trace files differ for every
   pair hashed — different transcripts, same outcome buckets. Report:
   `~/.apogee-sim/campaigns/qwen25-coder-14b-20260707/report.md`.
2. **The 14B is outcome-deterministic on this corpus despite temp 0.7** — confirmed
   twice (the aggregate, then the smoke: all 17 arms identical grades per task). The
   letter-grade instrument has **zero within-task variance on capable models**, so it
   cannot measure "helps" there. Any future "does it help strong models?" claim needs a
   finer instrument or harder corpus (ranked step 6).
3. **Efficiency signal, unmined:** many candidate-arm runs finished in 6–25 s vs
   minutes for bypass, with identical grades (visible in `~/campaign-run.log` walls;
   traces preserved). Plausibly `cached_content_intercept`. If it holds up, this is the
   "helps where it can" evidence for capable models — same outcome, less time/compute.
   The analyzer's secondary table does not currently surface wall time.
4. **Screen smoke (plan item 7) PASSED all acceptance criteria** on bundle
   `qwen25-coder-14b-20260707-smoke` (34/34 in ~1 h 10 m, 0 infra_failed): manifest
   `protocol: leave-one-out` / 17 arms / `bh_q: 0.05`; off-ramps
   (`empty_response_recovery`, `tool_use_enforcer`) correctly never left out; 15
   attribution rows all `no-evidence`; overall verdict `no-evidence`, "Convicted: none
   (no-evidence)… underpowered, not exonerating"; resume with `--id` skipped 34/34 in
   0 s; `campaign list` shows the bundle. Note: min attainable p was 1.0, not the
   plan's predicted 0.25 — because diffs were all-zero (finding 2), not a bug.
5. **BUG (FIXED — commit `48cd48f`, step 1): the Screen report's "Descriptive vs Bypass"
   column over-claimed.** It printed `inferior (p=1.0000)` on every row — p=1.0 on
   all-zero diffs is *no evidence*, not inferiority. Cause: `report.go` rendered a two-way
   label (`non-inferior` else `inferior`) from `DescriptiveNonInferior`, which never got
   item 5's no-evidence relabeling that the gate verdict and Convicted column have.
   Rendering-only — recorded runs/analysis unaffected. **Fix:** added
   `MechanismAttribution.DescriptiveNoEvidence` (classified via the same `gateNoEvidence`
   preconditions the gate uses; `deltaComputable` threaded into `computeScreen`) + a
   three-way label, so the column now reads `no-evidence (p=…)` when underpowered/δ-
   uncomputable. Verdict-wording tests extended (smoke-shape fixture now pins the
   descriptive no-evidence + guards the `| inferior (p=` mislabel; new evidence-shaped
   fixture pins genuine inferior/non-inferior floors are not swallowed). Both fail against
   the old renderer, pass now. Smoke bundle re-analyzed — all 15 rows now `no-evidence`.
   Binary rebuilt → `48cd48f`.

## Ranked next steps (in order; why each)

1. ✅ **DONE (commit `48cd48f`) — Fix the descriptive-label bug, pin the wording, commit,
   `make build`.** Added a descriptive no-evidence state (mirrors the `gateNoEvidence`
   logic) so the column reads `no-evidence (p=…)` when underpowered; extended the
   verdict-wording tests; re-analyzed the smoke bundle; rebuilt the binary → `48cd48f`.
   See finding 5 for details. *Why it was first:* the gemma Screen report would otherwise
   print 15 misleading "inferior" labels right where the harm-attribution will be read —
   the whole ADR 0009 discipline is about not over-claiming. Fixing BEFORE launch also
   avoided the "never kill-and-resume onto a freshly rebuilt binary" trap.
   **→ Next actionable: step 2 (launch the gemma Screen).**
2. **Reload gemma and launch the gemma Screen (the critical path — start it the same
   session, it's ~85 h wall).** `llama-launcher load gemma-4-e4b-it-qat`, verify with
   `llama-launcher status --json` (fingerprint hard-refusal protects against a silent
   mismatch — `qwen25-coder-14b` is what's loaded as of this handoff), then detached
   from `../apogee-sim`:
   `( cd ../apogee-sim && nohup caffeinate -i ./apogee-sim campaign run --arms leave-one-out --model gemma-4-e4b-it-qat --endpoint http://127.0.0.1:1111 --reps 5 >> ~/campaign-run.log 2>&1 & )`
   — 1,190 runs ≈ 4 overnights, resumable free via `--id`. *Why:* gemma is the model
   where the stack demonstrably hurts; the Screen is the instrument that names the
   guilty mechanism(s). Nothing else advances "get out of the way" until it runs.
3. **While the Screen runs: write the L9 ledger entries for the two COMPLETED
   campaigns** in `docs/design/mechanism-catalogue.md` (this repo), keyed by campaign ID
   + model: gemma aggregate → inferior; qwen25-coder-14b aggregate → no-evidence with
   the zero-variance caveat and the efficiency observation. *Why now:* L9 permits
   entries from completed campaigns only — both qualify; capturing them before the
   Screen's flood of data keeps the ledger honest. `analyze`/`list` are safe
   mid-campaign; ledger edits don't touch the sim.
4. **While the Screen runs: mine the 14B efficiency signal** (finding 3). Extract
   per-arm wall/turn stats from the two qwen bundles' `runs.jsonl` + traces; check
   whether the speedup attributes to `cached_content_intercept`; decide whether to add
   wall time to the analyzer's secondary table. *Why:* it's the only "helps" evidence
   available for capable models given zero grade variance, and it's sitting in already
   -recorded data — no new runs needed.
5. **Read the Screen per discipline, then run the Confirmation campaign.** Convicted
   set + replication readout first — if the in-bundle candidate-vs-Bypass direction does
   NOT reproduce the aggregate failure, stop and investigate the rig before trusting
   attributions (ADR 0013 §4). Then `campaign run --enable-mechanisms <base minus
   convicted> --model gemma-4-e4b-it-qat --reps 5` (~10 h) for the full NI gate. *Why:*
   this pair converts "the stack hurts gemma" into "mechanisms X, Y hurt gemma; removing
   them restores the floor" — the actionable form of "get out of the way".
6. **Grill the instrument-sensitivity question before acting on it** (finding 2): the
   grade instrument can't discriminate on capable models. Options to grill: finer
   scoring (test-level partial credit), harder tasks, making the 3 ineligible prompts
   eligible (they lack `expected_files`/`quality_gate` and are skipped in every
   campaign). *Why ranked here:* it blocks only future capable-model claims, not the
   gemma work; it's a design decision that deserves the grill-first treatment per
   project practice.
7. **Housekeeping:** push apogee-sim `main` (was 21 commits ahead; step 1 adds more —
   optional `/code-review` first); archive the superseded `2026-07-07 - 01` handoff.

## Operational lessons (carried forward + NEW from this session)

- **NEW — detached launches from the harness WORK**: double-fork with sandbox disabled,
  `( cd ../apogee-sim && nohup caffeinate -i … >> ~/campaign-run.log 2>&1 & )` →
  process reparents to launchd (PPID 1) and survives the session. Used twice
  successfully on 2026-07-07 (aggregate + smoke). The 2026-07-06 "twice reaped" lesson
  applies to harness *background tasks*, not to this pattern.
- **NEW — harness background-Bash watchers DO get reaped** (twice again this session);
  the campaigns were unaffected. The **Monitor tool** (poll-until-exit loop) worked for
  completion notification.
- **Verify the loaded model before every launch/resume**: `llama-launcher status
  --json`. Don't switch profiles mid-campaign. Don't kill-and-resume onto a freshly
  rebuilt binary.
- `pkill -INT -f "apogee-sim campaign run"` is the clean kill (SIGINT-safe scheduler,
  prints the resume command).
- Rebuild `../apogee-sim` (`make build`) after any source change; the binary is
  gitignored and goes stale silently. Built at `48cd48f` as of this update (step 1).

## Repo / tree state at handoff

- **apogee:** no code changes. This handoff and the `2026-07-07 - 01` doc are
  untracked; older handoffs moved to `archived/` (also untracked/deleted in status).
- **apogee-sim:** working tree clean at `48cd48f` (step 1's label-bug fix landed on
  `main`); binary rebuilt and stamped `48cd48f`. `main` remains **unpushed** (was 21+
  ahead of origin; step 1 added one more). Push is step 7 (optional `/code-review` first).
- **Bundles** (`~/.apogee-sim/campaigns/`): `gemma-4-e4b-it-qat-20260706` (inferior —
  the harm record), `qwen25-coder-14b-20260707` (no-evidence), `qwen25-coder-14b-20260707-smoke`
  (item-7 acceptance record — `report.md`/`analysis.json` re-analyzed post-fix under
  `48cd48f`; descriptive column now reads `no-evidence`), `qwythos-9b-20260707` (16/140, abandoned-model record),
  `gemma-…-smoke` (old harness smoke). All have `report.md`/`analysis.json` except the
  qwythos partial.
- **LLM server:** `qwen25-coder-14b` loaded on `127.0.0.1:1111` — the gemma Screen
  needs a profile reload first.

## Explicitly NOT next (parked — carried forward)

- **Any default-ON flip / mechanism deletion / apogee code change from current
  evidence** — L9: ledger entries only; curation decisions wait for Screen +
  Confirmation.
- A qwen25-coder-**7b** aggregate (the 14B answered the capable-model question);
  resuming qwythos-9b (abandoned).
- Family-swap arms (ADR 0012 §2); off-ramp firing-subpopulation instrument (ADR 0013
  §3); iterated greedy elimination (ADR 0013 §2); second-round Screen decisions before
  the Confirmation result; mechanism-authorship SPI (ADR 0015); depth-1 relaxation
  (ADR 0014 §5); mid-Exchange auto-compaction (TODO.md); apogee↔apogee-sim imports
  (ADR 0001).

## Pointers (don't re-read into context unless needed)

- Screen protocol: `../apogee-sim/docs/adr/0013-the-leave-one-out-screen-protocol.md` ·
  plan (L-table + operator sequence §"After this plan"):
  `../apogee-sim/docs/plans/leave-one-out-campaign-plan.md` · terms: `../apogee-sim/CONTEXT.md`
- Decision rule: `docs/adr/0009-the-ab-decision-rule.md` · bypass floor: ADR 0006 ·
  enable surface: ADR 0015
- Bug site: `../apogee-sim/internal/campaign/report.go:165-168` (label),
  `../apogee-sim/internal/campaign/analyze.go:211` (`DescriptiveNonInferior` — needs a
  no-evidence sibling; mirror `gateNoEvidence`, `analyze.go:423`)
- Catalogue + ledger target: `docs/design/mechanism-catalogue.md` · master roadmap:
  `docs/plans/implementation-plan-apogee-merge.md`
- Superseded handoff: `2026-07-07 - 01 - screen-shipped-qwythos-abandoned-…`

## Suggested skills for the next session

- **`manage-llm-server`** — before the Screen: reload `gemma-4-e4b-it-qat`, verify via
  `status --json`; the fingerprint check will hard-refuse a mismatch but check first
  anyway.
- **`coding-standards`** — for the step-1 label fix + wording test in apogee-sim.
- **`archive-handoffs`** — move the superseded `2026-07-07 - 01` doc to `archived/`.
- **`/code-review`** — optional, on apogee-sim's unpushed commits before pushing.
- **`grill-with-docs`** — for step 6 (instrument sensitivity) when its turn comes.
- **`/handoff`** — at session end, superseding this doc.

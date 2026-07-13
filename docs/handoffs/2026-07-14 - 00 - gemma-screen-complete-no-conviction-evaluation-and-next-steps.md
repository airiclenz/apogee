# Handoff — gemma Screen COMPLETE, verdict **no-conviction** (0 convicted, "underpowered for diffuse harm"); next session: read the Screen per discipline, write its L9 entry, decide the next move

**Date:** 2026-07-14. **Status: the leave-one-out Screen `gemma-4-e4b-it-qat-20260708`
finished clean overnight — 1190/1190 runs, 0 infra_failed, 133 h 24 m wall. Headline:
no single mechanism convicted.** This doc **supersedes** `2026-07-08 - 00 -
qwen14b-aggregate-…` (archived): all four of its actionable steps are DONE (label-bug
fix `48cd48f`; Screen launched + completed; L9 entries for both aggregates; 14B
efficiency signal mined — finding REVERSED, see below). Work from this directory;
apogee-sim is the sibling repo at `../apogee-sim`.

**The next session's job (per operator): thoroughly evaluate the Screen results and
plan the next step(s), starting with a documentation review.** This doc deliberately
records the Screen's *facts* without interpreting them — the reading is the work.

## The goal (decision frame — unchanged)

**Curate a sensible mechanism set for apogee: keep what helps where it can, remove or
gate what hurts.** Evidence state after this session:

| Campaign | Model | Verdict | Standing |
|---|---|---|---|
| `gemma-4-e4b-it-qat-20260706` (aggregate) | gemma (4B) | **inferior** | The stack hurts gemma. Valid — real multi-turn engagement (3–50 turns/run). |
| `gemma-4-e4b-it-qat-20260708` (**Screen**) | gemma (4B) | **no-conviction** | NEW — completed 2026-07-14 00:38 CEST. To be read next session. |
| `qwen25-coder-14b-20260707` (aggregate) | qwen 14B | no-evidence | **Silent on capable models** — step-4 mining (2026-07-08) found zero tool executions in all 174 qwen runs; grades measured the seeded workspace. See the catalogue ledger amendment. |
| `qwythos-9b-20260707` (16/140) | qwythos 9B | abandoned | Record only. |

The 2026-07-08 working hypothesis is now one-legged: only "the stack harms weak
models" retains evidence; the capable-model leg is unsupported (not refuted — unmeasured).

## The Screen's recorded facts (do NOT re-derive; report + analysis.json exist)

Bundle: `~/.apogee-sim/campaigns/gemma-4-e4b-it-qat-20260708/` (`report.md`,
`analysis.json`, `runs.jsonl` 1190 lines, 1190 traces). Protocol as planned:
`leave-one-out`, 17 arms (15 without-X + candidate + bypass), 14 tasks × 5 reps,
BH-FDR q = 0.05, δ = 0.4643 (split-half AA null), Wilcoxon exact (Pratt). Off-ramps
(`tool_use_enforcer`, `empty_response_recovery`) were never left out, by design.

Headlines (from `report.md`, recorded here for orientation only):

- **Convicted: none.** Report's own wording: "no single Mechanism convicted —
  underpowered for diffuse harm." All 15 attribution rows p ≥ 0.11 pre-BH.
- **The harm replicated in-bundle** (candidate-vs-bypass per-task means): worst
  `propagate-lookup-dense` −2.4, `propagate-lookup-errors` −2.4, `logger-api-refactor`
  −0.8, `cli-with-subcommands` −0.6, `propagate-lookup-rename` −0.6; five tasks ≤ +0.4
  the other way. Formal replication readout per ADR 0013 §4 is next session's first
  check before trusting anything else.
- **One standout row (descriptive, NOT a conviction):** `truncate_history` — the only
  mechanism whose removal trends positive (mean diff +0.429, p = 0.1104) and the only
  arm descriptively **non-inferior to Bypass (p = 0.0006)**; every other without-X arm
  reads `inferior`. Consistent with the rep-0 anecdote (on `propagate-lookup-errors`,
  `without-truncate_history` scored A/pass while sibling arms sat at D/fail).
- Campaign ops: single uninterrupted process (no resume needed), pace settled at
  ~6.7 min/run exactly matching the July-6 aggregate calibration.

## What else happened this session (2026-07-08 → 07-14; all committed)

1. **Step-1 label-bug fix** (`48cd48f`, apogee-sim): Screen's "Descriptive vs Bypass"
   column now reads `no-evidence` when underpowered. Verified: the completed Screen's
   descriptive column renders real values (one non-inferior, rest inferior).
2. **L9 ledger entries written + committed** (`19f677c`, apogee): new "Bench campaign
   evidence (L9)" subsection in `docs/design/mechanism-catalogue.md` for both completed
   aggregates.
3. **Step-4 mining REVERSED the 14B efficiency signal** (`488b8eb`, apogee): all 174
   qwen runs were single round-trips with **zero tool executions** — qwen emitted
   fenced-JSON pseudo-tool-calls that never parsed; the loop treats a no-tool-call
   response as exchange-complete at turn 1; `tool_use_enforcer` is unreachable there
   (needs ≥2 prior assistant messages, sim `internal/proxy/tooluse_enforcer.go`).
   Wall gap = response length (`tool_use_directive`/`decompose` shaping), NOT
   `cached_content_intercept` (0 fires — refuted by construction). Full record: the
   catalogue ledger's 2026-07-08 amendment.

## Next session — suggested order

1. **Documentation review first** (operator asked for this explicitly):
   - `../apogee-sim/docs/adr/0013-the-leave-one-out-screen-protocol.md` — esp. §4
     (replication readout discipline) and §2 (iterated greedy elimination, parked).
   - `docs/adr/0009-the-ab-decision-rule.md` (apogee) — the no-over-claiming rule.
   - `../apogee-sim/docs/plans/leave-one-out-campaign-plan.md` — the L-table (L9:
     ledger-only disposition **for every outcome including no-conviction**) and the
     "After this plan" operator sequence.
   - `docs/design/mechanism-catalogue.md` — Ledger + "Bench campaign evidence"
     subsection incl. both 2026-07-08 amendments (context for what's already recorded).
   - ADR 0006 (bypass floor), `../apogee-sim/CONTEXT.md` (terms), ADR 0012 §2 /
     ADR 0013 §2–3 (parked alternatives that may now be relevant).
2. **Read the Screen per discipline:** replication readout first (ADR 0013 §4 — if the
   in-bundle candidate-vs-Bypass direction did NOT reproduce the aggregate failure,
   stop and investigate the rig; the per-task diffs above suggest it did, but run the
   formal check). Then the attribution table, then the descriptive column — in that
   order, resisting the `truncate_history` teaser until the protocol earns it.
3. **Write the Screen's L9 ledger entry** (completed campaign → qualifies; keyed by
   campaign ID + model) in `docs/design/mechanism-catalogue.md`.
4. **Decide the next move — this is a design decision; grill it, don't default it.**
   The no-conviction branches the plan anticipated (all currently parked, pointers
   above): a Confirmation-style probe of the standout (e.g. `--enable-mechanisms <base
   minus truncate_history>`, ~10 h, full NI gate — note ADR 0013 §4's caution that with
   no convicted set this is exploratory, not confirmatory); iterated greedy elimination
   (ADR 0013 §2); a higher-powered second Screen; or accepting "diffuse harm" and
   grilling what that means for curation. L9 still forbids apogee changes either way.
5. **Deferred rig work (now safe — campaign done, rebuilds allowed):** the analyzer
   engagement guard (turns / tool-exec counts / wall time in the secondary table +
   zero-engagement alarm — motivated by the qwen post-mortem) and the qwen tool-call
   protocol fix (chat-template/parser investigation) if a capable-model campaign is
   ever wanted. Both in apogee-sim; `make build` after any change.

## Operational state at handoff

- **LLM server:** `gemma-4-e4b-it-qat` still loaded on `127.0.0.1:1111` (uptime ~5.6
  days). Campaign is DONE — **switching profiles is safe again.**
- **apogee:** clean at `488b8eb` except one **untracked, operator-created**
  `docs/kill-and-resume.md` (stop/resume crib notes — not authored this session; leave
  to the operator). This handoff is new; the 2026-07-08 handoff moved to `archived/`.
- **apogee-sim:** clean at `48cd48f`, **1 commit unpushed** (the label fix; the
  operator pushed the earlier backlog). Push is trivial housekeeping — optional
  `/code-review` first.
- **Binary:** `../apogee-sim/apogee-sim` built at `48cd48f` — matches source; the
  whole Screen ran on it.
- **Bundles** (`~/.apogee-sim/campaigns/`): `gemma-4-e4b-it-qat-20260708` (the Screen —
  NEW), `gemma-4-e4b-it-qat-20260706` (aggregate, inferior), `qwen25-coder-14b-20260707`
  (+ `-smoke`) (no-evidence; capable-model-invalid per step 4), `qwythos-9b-20260707`
  (partial), `gemma-…-smoke` (old harness smoke).

## Operational lessons (carried forward; all verified again this session)

- Detached double-fork launch survives sessions: `( cd ../apogee-sim && nohup
  caffeinate -i ./apogee-sim campaign run … >> ~/campaign-run.log 2>&1 & )` —
  reparents to launchd. Harness *background Bash* still gets reaped; the **Monitor
  tool** worked for 3-hourly progress + completion notification (used for this
  campaign's final night).
- `pkill -INT -f "apogee-sim campaign run"` = clean kill (prints resume command);
  resume with `campaign run --model <m> --id <campaign-id>` (idempotent, skips
  recorded runs). Verify the loaded model (`llama-launcher status --json`) before any
  launch/resume; never resume onto a freshly rebuilt binary.
- Campaign ETA lore: early log ETAs are task-major-biased (first estimate was 3× off);
  the cumulative mean converges after ~1 rep — gemma runs ~6.7 min/run on this rig.

## Explicitly NOT next (parked — carried forward)

- **Any default-ON flip / mechanism deletion / apogee code change from current
  evidence** — L9: ledger entries only, for every outcome including no-conviction.
- A qwen25-coder-7b aggregate; resuming qwythos-9b; re-running the 14B before the
  tool-call protocol fix exists.
- Family-swap arms (ADR 0012 §2); off-ramp firing-subpopulation instrument (ADR 0013
  §3); mechanism-authorship SPI (ADR 0015); depth-1 relaxation (ADR 0014 §5);
  mid-Exchange auto-compaction (TODO.md); apogee↔apogee-sim imports (ADR 0001).
  (Iterated greedy elimination, ADR 0013 §2, moves from "parked" to "candidate next
  move" — see step 4 above.)

## Pointers (don't re-read into context unless needed)

- Screen bundle + report: `~/.apogee-sim/campaigns/gemma-4-e4b-it-qat-20260708/`
- Screen protocol: `../apogee-sim/docs/adr/0013-the-leave-one-out-screen-protocol.md` ·
  plan + L-table: `../apogee-sim/docs/plans/leave-one-out-campaign-plan.md`
- Decision rule: `docs/adr/0009-the-ab-decision-rule.md` · bypass floor: ADR 0006 ·
  terms: `../apogee-sim/CONTEXT.md`
- Ledger (+ 2026-07-08 amendments): `docs/design/mechanism-catalogue.md` §"Bench
  campaign evidence" · master roadmap: `docs/plans/implementation-plan-apogee-merge.md`
- qwen post-mortem details: catalogue ledger amendment + this repo's commit `488b8eb`
- Superseded handoff (full session narrative): `archived/2026-07-08 - 00 -
  qwen14b-aggregate-no-evidence-screen-smoke-passed-label-bug-found-gemma-screen-next.md`

## Suggested skills for the next session

- **`grill-with-docs`** — the centerpiece: stress-test the no-conviction reading and
  the choice of next campaign against ADR 0009/0013 and the L-table before running
  anything.
- **`manage-llm-server`** — before any new campaign: load/verify the right profile
  (gemma is still loaded; switching is safe now).
- **`coding-standards`** — if the analyzer engagement-guard / wall-time work happens.
- **`/code-review`** + **`pr-lifecycle`** — optional, for apogee-sim's unpushed commit.
- **`/handoff`** — at session end, superseding this doc.

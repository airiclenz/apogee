# Handoff — Screen evaluated (L9 entry committed), next move grilled & ratified: exploratory **Probe** (base minus `truncate_history`); Probe launched then killed at 0/140 (operator had no 12 h window) — **next session: resume it, one command**

**Date:** 2026-07-14 (same-day supersession of `archived/2026-07-14 - 00 - gemma-screen-
complete-no-conviction-evaluation-and-next-steps.md` — all five of its steps are done or
decided). Work from this directory; apogee-sim is the sibling repo at `../apogee-sim`.

## First action of the next session — resume the Probe

```
llama-launcher status --json   # gemma-4-e4b-it-qat must be the active profile; load it if not
( cd ../apogee-sim && nohup caffeinate -i ./apogee-sim campaign run \
    --model gemma-4-e4b-it-qat \
    --id gemma-4-e4b-it-qat-20260714-minus-truncate-history \
    >> ~/campaign-run.log 2>&1 & )
```

- The bundle + manifest are **already pre-registered and verified** (2 arms × 16
  mechanisms — the Screen's `without-truncate_history` set verbatim, off-ramps in, only
  the Bypass bit differs; empty protocol field = aggregate; reps 5, α 0.05).
  **0/140 runs recorded** — the 09:38 launch was cleanly SIGINT-killed at 09:45 before
  the first run finished, so resume simply runs all 140. Kill/resume again is free and
  idempotent (`pkill -INT -f "apogee-sim campaign run"` prints the resume command).
- **Expected duration ≈ 12.7 h uninterrupted** — measured, not guessed: in the Screen,
  the `without-truncate_history` arm averaged 5.82 min/run and Bypass 5.05 min/run;
  70 + 70 runs at those paces = 12.67 h. Plan a night, not an evening.
- **Do NOT rebuild apogee-sim before the Probe completes** (never resume onto a rebuilt
  binary). The binary was built at `48cd48f`; the only commit since (`777af3f`) is
  docs-only, so binary still matches source. The deferred rig work (below) waits.

## What this session decided (grilled per the 00-handoff's step 4; operator-ratified)

1. **Next move = exploratory Probe**: one aggregate-Protocol campaign, base minus
   `truncate_history` vs Bypass — chosen over iterated greedy elimination (~85 h/round,
   unchanged cost objection), a higher-powered second Screen (needs corpus authoring
   first; same 14 tasks would replicate underpoweredness), and accepting diffuse harm
   now (stalls curation with no gate-level evidence).
2. **"Probe" is now a canonical term** in `../apogee-sim/CONTEXT.md` (commit `777af3f`):
   aggregate-Protocol Campaign over a custom enable set testing a data-suggested
   hypothesis; statistically rigorous but **exploratory by construction** — a pass
   licenses a ledger claim, never a curation action. It is NOT a Confirmation campaign
   (that term requires a convicted set).
3. **Sequencing: Probe before rig work.** The analyzer engagement guard and the qwen
   tool-call fix stay deferred until the Probe completes (no rebuilds mid-campaign;
   gemma engagement is not in doubt — 1,930 tool calls in the 20260706 aggregate).
4. **Pre-registered reading rules for the Probe** (recorded here — nowhere else):
   - Read order: fresh split-half δ → NI gate → closed superiority. Before believing
     grades, spot-check engagement in `runs.jsonl` (`turns`, `tool_runs` fields) — the
     engagement guard isn't built yet; this is the qwen post-mortem lesson applied.
   - **Pass (NI):** L9 ledger entry only — "base minus `truncate_history` non-inferior
     to Bypass on gemma; localization of the aggregate harm to `truncate_history`
     supported — exploratory." No curation action, no pre-committed follow-up.
   - **Also superior:** same entry, superiority noted — strengthens but does not change
     the disposition (L9 forbids apogee changes regardless).
   - **Fail:** the diffuse-harm reading gains weight; the branch decision re-opens
     (iterated greedy / bigger corpus / accept diffuse harm) — grill again, don't default.

## What this session already completed (all committed)

- **Screen evaluated per discipline** (replication readout first — it passed; then
  attribution — convicted none; then descriptive — `truncate_history` sole standout).
  Full record: the new L9 entry `gemma-4-e4b-it-qat-20260708` in
  `docs/design/mechanism-catalogue.md` (apogee commit `33e5511`), incl. a dated
  amendment to the working-hypothesis paragraph (Screen→Confirmation path closed).
- **CONTEXT.md "Probe" term** (apogee-sim commit `777af3f`).
- Docs read per the 00-handoff's step 1 (ADR 0013, ADR 0009, plan L-table, ledger,
  ADR 0006, CONTEXT.md, ADR 0012) — pointers unchanged, see the archived 00-handoff.

## Operational state at handoff

- **LLM server:** `gemma-4-e4b-it-qat` loaded fresh this morning (the overnight server
  had been stopped — the 00-handoff's "still loaded" was stale). No campaign running, so
  switching/unloading is safe; the Probe resume needs gemma back first.
- **apogee:** clean at `33e5511` + this handoff (00-handoff moved to `archived/`).
  Untracked operator file `docs/kill-and-resume.md` — leave to the operator.
- **apogee-sim:** clean at `777af3f`, **2 commits unpushed** (`48cd48f` label fix,
  `777af3f` Probe term). Push = optional housekeeping, `/code-review` first if desired.
- **Bundles:** `gemma-4-e4b-it-qat-20260714-minus-truncate-history` (the Probe, 0/140,
  manifest verified) is new; the rest as listed in the archived 00-handoff.

## Operational lessons (new this session + key carry-forwards)

- **NEW — shared `~/campaign-run.log` bites progress greps:** all campaigns append to
  one log. Any progress/monitor grep must be scoped to the region **after the campaign's
  own `Created campaign <id>` line** (`grep -an "Created campaign $CID" | tail -1` →
  `tail -n +N`); an unscoped `[n/140]` grep matched the old qwen campaign and produced a
  phantom "140/140 complete" event. Completion detection by unique campaign ID
  (`Campaign <id>:`) is safe unscoped.
- Monitor tool pattern that works (poll loop, 60 s, persistent): completion line by ID,
  `pgrep -f "apogee-sim campaign run"` death check, 3-hourly scoped progress tick.
- Detached double-fork launch (`( cd ../apogee-sim && nohup caffeinate -i ./apogee-sim
  campaign run … >> ~/campaign-run.log 2>&1 & )`) survives session end; harness
  background Bash does not.
- Campaign ETAs: derive per-arm from prior bundles' `runs.jsonl` `wall_millis` (this is
  how 12.7 h was computed) — early in-log ETAs are task-major-biased.

## Explicitly NOT next (carried forward unchanged)

- Any apogee default flip / mechanism deletion / code change from current evidence (L9:
  ledger only — including for the Probe, whatever its outcome).
- Rig work before the Probe completes: analyzer engagement guard, qwen tool-call
  protocol fix (both queued right after).
- All parked items listed in the archived 00-handoff (family-swap arms, off-ramp SPI,
  depth-1 relaxation, mid-Exchange auto-compaction, apogee↔apogee-sim imports;
  iterated-greedy stays "candidate next move only if the Probe fails").

## Suggested skills

- **`manage-llm-server`** — verify/load `gemma-4-e4b-it-qat` before the resume; also to
  free the machine afterwards if wanted.
- **`handoff`** — at session end, superseding this doc (record the Probe verdict + its
  L9 entry).
- **`grill-with-docs`** — only if the Probe **fails** its gate (the branch decision
  re-opens) or when curation is finally on the table.
- **`coding-standards`** — for the engagement-guard / qwen-fix rig work after the Probe.
- **`/code-review`** + **`pr-lifecycle`** — optional, for apogee-sim's two unpushed
  commits.

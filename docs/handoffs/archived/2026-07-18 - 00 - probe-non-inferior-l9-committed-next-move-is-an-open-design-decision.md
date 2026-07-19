# Handoff — Probe completed **non-inferior** (140/140, engagement verified), L9 entry committed; next campaign is an **open design decision** (grill it), rig work now unblocked

**Date:** 2026-07-18 (supersedes `archived/2026-07-14 - 01 - screen-evaluated-probe-
ratified-killed-at-0-of-140-resume-it-first.md` — its one action, resuming the Probe, is
done and evaluated). Work from this directory; apogee-sim is the sibling repo at
`../apogee-sim`.

## Where things stand

- **The Probe passed.** `gemma-4-e4b-it-qat-20260714-minus-truncate-history` completed
  2026-07-18 (140/140, 0 infra_failed, 13 h 54 m wall): candidate (base minus
  `truncate_history`) **non-inferior** to Bypass within fresh δ = 0.4643 (W+ = 102.0,
  p = 0.0003, N = 14); **not superior**; engagement spot-checked by hand in `runs.jsonl`
  (zero runs without tool executions in either arm). The full evidence, read in the
  pre-registered order (engagement → fresh δ → NI gate → closed superiority), lives in
  the L9 entry in `docs/design/mechanism-catalogue.md` (apogee commit `b152293`) — read
  that entry, not this doc, for the numbers and the disposition.
- **Disposition applied verbatim:** localization of the aggregate harm to
  `truncate_history` is supported — **exploratory**. Ledger claim only; no curation
  action, no pre-committed follow-up. The catalogue's working-hypothesis paragraph
  carries a dated 2026-07-18 amendment.
- **Nothing is queued by pre-registration.** The next campaign is an open design
  decision. A Confirmation campaign remains unavailable (no convicted set exists — the
  Screen convicted none; a Probe pass cannot create one).

## Next moves (operator picks; none is pre-committed)

1. **Grill the next campaign design** (`grill-with-docs`): what, if anything, turns the
   exploratory localization claim into evidence that licenses curation. The L9
   constraint stands until a confirmatory design says otherwise.
2. **Rig work — now unblocked** (campaign done ⇒ rebuilds allowed again), per the qwen
   entry's amendment consequence (c) in the catalogue:
   - Analyzer **engagement guard**: turns / tool-exec counts / wall in the secondary
     table + flag zero-engagement campaigns instead of reading them as no-evidence.
   - **qwen tool-call protocol fix**: chat-template/parser investigation of the
     fenced-JSON pseudo-tool-calls.
3. **Optional housekeeping:** apogee-sim has **2 unpushed commits** (`48cd48f` label
   fix, `777af3f` Probe term). `/code-review` first if desired, then push.

## Operational state at handoff

- **LLM server:** `gemma-4-e4b-it-qat` still loaded (llama.cpp via llama-launcher,
  `127.0.0.1:1111`). No campaign running — switching/unloading is safe.
- **NEW — `~/.apogee-sim/config.yaml` now exists**, setting `upstream.url:
  http://127.0.0.1:1111`. Campaign commands no longer need `--endpoint`. Backstory: with
  no config file, apogee-sim defaulted to Ollama's `localhost:11434`, which is why the
  07-14 handoff's verbatim resume command failed on 07-17; the resume ran with an
  explicit `--endpoint` (safe under D10 — the fingerprint pins model identity, not URL,
  and the re-check passed). If the server address ever changes, update this file.
- **apogee:** clean at `b152293`. Untracked operator file `docs/kill-and-resume.md` —
  leave to the operator.
- **apogee-sim:** clean at `777af3f`, 2 unpushed (above). Binary still the Jul 8 build;
  rebuilds permitted now.
- **Bundles:** the Probe bundle is complete (`manifest.json`, `runs.jsonl`, 142 traces,
  `report.md`, `analysis.json`); the rest as listed in the archived 07-14 handoffs.

## Operational lessons (new this session + key carry-forwards)

- **NEW — endpoint drift:** a missing `~/.apogee-sim/config.yaml` silently falls back to
  `localhost:11434`. Fixed by creating the config (above); verified end-to-end by
  running `campaign run` without `--endpoint` from a corpus-less directory (fails at
  corpus load ⇒ fingerprint step passed ⇒ config took effect, nothing created on disk).
- **NEW — completion detection by campaign ID is NOT safe unscoped after a kill**
  (corrects the 07-14 lesson): a killed session leaves `Campaign <id>: partial …` in the
  shared `~/campaign-run.log`, which a naive completion grep matches. Scope every grep —
  completion included — past the latest `Resuming campaign <id>` (or `Created campaign
  <id>`) anchor line.
- **Monitor pattern that worked** (60 s persistent poll): scoped terminal-line grep by
  ID; pgrep death check with a bracket pattern (`campaign ru[n]`) so the monitor's own
  command line doesn't match; one first-run-recorded event (~6 min, confirms recording
  works); 45-min stall warning; 3-hourly progress tick. Ended itself cleanly on the
  campaign's terminal line.
- In-log ETAs are task-major-biased (observed again: 4 h 55 m at run 1 vs 13 h 54 m
  actual) — derive ETAs per-arm from prior bundles' `wall_millis`.
- Detached double-fork launch (`( cd ../apogee-sim && nohup caffeinate -i ./apogee-sim
  campaign run … >> ~/campaign-run.log 2>&1 & )`) survives session end; harness
  background Bash does not.

## Explicitly NOT next (carried forward, updated)

- Any apogee default flip / mechanism deletion / code change from current evidence —
  L9: the Probe's pass licenses its ledger claim **only**.
- A Confirmation campaign (no convicted set exists).
- **Iterated greedy elimination:** was "candidate next move only if the Probe fails" —
  the Probe passed, so it stays parked unless the grill revives it.
- All parked items from the archived 07-14 00-handoff (family-swap arms, off-ramp SPI,
  depth-1 relaxation, mid-Exchange auto-compaction, apogee↔apogee-sim imports).

## Suggested skills

- **`grill-with-docs`** — the next-campaign design decision (next move 1); curation is
  finally discussable, so expect the L9 boundary to be the center of the grill.
- **`coding-standards`** — the engagement-guard and qwen-fix rig work (next move 2).
- **`/code-review`** + **`pr-lifecycle`** — apogee-sim's two unpushed commits.
- **`manage-llm-server`** — free the machine (gemma is still loaded) or switch models
  for rig-work testing.
- **`handoff`** — at session end, superseding this doc.

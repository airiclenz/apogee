# Handoff — qwen3.6-27B campaign launched from the devbox and CHECKPOINT-KILLED (12/14 constant, suite too easy); next: rig-work design conversation (harder / finer-grained corpus)

**Date:** 2026-07-20 (supersedes `archived/2026-07-19 - 08 - doc-corrections-committed-qwen-server-up-devbox-campaign-launch-verified-viable-awaiting-owner-go.md` — its launch went ahead and concluded; this doc records the outcome and reframes the next move). Work from this directory; apogee-sim is the sibling repo at `../apogee-sim`.

## What this session did

1. **Launched the qwen3.6-27B campaign from the devbox** (owner gave the explicit go +
   Mac-awake confirmation). Pre-launch verification: both repos clean/level, fresh
   `../apogee-sim/apogee-sim` binary at `6634376`, served model id re-verified verbatim
   against `/v1/models`, llama.cpp `b10068` single-slot. Created campaign
   `users-airic-ll-models-qwen-qwen3-6-27b-q4-k-s-gguf-20260719` (manifest 14×2×5=140,
   tool-call preflight passed, interrupted with 0 recorded), then drove the checkpoint
   slice detached (`-id <id> -reps 2`, setsid/nohup).
2. **Slice completed clean:** 56/56 recorded in 7h42m46s, 0 infra_failed (~8.3 min/run,
   under the 8–16 h estimate).
3. **Applied the pre-registered Discrimination checkpoint → KILL.** Engagement
   **verified** (all 56 runs ≥1 tool exec, 0 zero-exec in both arms), but **12/14 tasks
   constant** (≥12 threshold hit exactly ⇒ not-powered; the 10–11 judgment band was
   never reached). 9 of the 12 constants sit at top grade A — the pre-registration's
   "suite too easy" post-mortem, not "something is off" (100 % compile both arms; the
   two varying tasks are extract-and-test and implement-lru-cache). NI gate / δ /
   superiority were never computed. Full readout + launch-machine deviation note:
   **the bundle's `CHECKPOINT.md`**
   (`~/.apogee-sim/campaigns/users-airic-ll-models-qwen-qwen3-6-27b-q4-k-s-gguf-20260719/`,
   on the **devbox** — the first bundle that lives here). `analysis.json` / `report.md`
   sit beside it; slice/create logs at `~/.apogee-sim/qwen36-*.log`.
4. **Recorded the kill** per the disposition table: bundle left incomplete at 56/140,
   **no L9 ledger entry** (killed campaigns stay out of L9 — the qwythos precedent);
   this handoff is the kill's record. Auto-memory updated to match.
5. **Doc update:** outcome/status note added to the top of
   `../apogee-sim/docs/plans/qwen36-27b-first-aggregate-campaign-plan.md` pointing at
   the bundle's `CHECKPOINT.md` (the pre-registered content itself untouched).

## Next moves

1. **Rig-work design conversation (the pre-registered next move).** The disposition
   table says a checkpoint kill flips the answer to rig work — a harder / finer-grained
   corpus — **about the instrument, not about qwen**. Open questions for that grill:
   harder tasks vs finer grade bands (the 4-run constancy signal can't distinguish
   ceiling from coarse grading for the 3 tasks constant at C); whether the corpus stays
   frozen per ADR 0009 with a new suite version or per-model discriminating bands are
   allowed (bench-overfitting tension). Calibration data in hand: the 56 runs in the
   killed bundle. This is a design session — plan doc first per the owner's convention,
   implementation later.
2. **Operator, on whichever machine holds the qwen25 bundles:** runbook step 2
   (`campaign analyze` `not-engaged` stamps on `qwen25-coder-14b-20260707` +
   `-smoke`) — still outstanding, independent, machine unknown (locate via
   `ls ~/.apogee-sim/campaigns`; they are NOT on the devbox).
3. **Owner's-call housekeeping:** cut a release for the apogee `[Unreleased]` CHANGELOG
   block; decide whether to unload the qwen3.6 model (server left UP and idle).
4. **Carried deferred follow-ups (04–08, none urgent):** TUI in-transcript banner for
   the validated-set notice; behavioral-probe (medium-confidence) resolver; user-run
   validation tooling writing `~/.apogee/validated/`.

## Operational state at handoff

- apogee and apogee-sim both on `main`, synced with origin before this session's
  commits; apogee-sim at `6634376` + this session's plan-doc note. Verify with
  `git status -sb`, don't trust counts written here.
- **Devbox now has a campaign store:** `~/.apogee-sim/campaigns/` holds exactly the
  killed qwen3.6 bundle (56/140, incomplete by design — do not top it up).
- **Server UP and idle:** qwen3.6-27B-Q4_K_S resident on llama.cpp `b10068`, single
  slot, `0.0.0.0:1111` (devbox view `http://192.168.64.1:1111`); llama-launcher MCP at
  `http://192.168.64.1:7331/mcp`. Sandboxed Bash cannot reach the host — use
  unsandboxed curl; MCP handshake pattern is in the campaigns memory file.

## Explicitly NOT next (carried forward + new)

- **No L9 entry for the killed campaign** and no Validated-set writes — the bundle is
  incomplete evidence by pre-registration. Do not resume/top-up the killed bundle; do
  not re-run the campaign on the same suite (the kill's whole point).
- No qwen default flips or catalogue deletions from current evidence; no revisiting the
  item-4 fallback (deleting `exchangeStart`) without a design session; do not re-grill
  the (now-concluded) pre-registration — the next grill is about the *instrument*.
- Do not assume which machine holds the qwen25 bundles — locate first.

## Suggested skills

- **`grill-with-docs`** — the rig-work design conversation (next move 1): it will
  touch apogee-sim CONTEXT.md terms (Campaign, Discrimination checkpoint) and ADR 0009's
  frozen-suite rule, and should end in a plan doc per [[handoff-doc-for-large-plans]].
- **`manage-llm-server`** — server observation / unload decision (mutating calls only
  with owner confirmation).
- **`archive-handoffs`** — after writing a successor to this doc.
- **`handoff`** — at session end, superseding this doc.

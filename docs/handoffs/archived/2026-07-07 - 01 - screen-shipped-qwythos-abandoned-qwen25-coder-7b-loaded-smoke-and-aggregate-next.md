# Handoff — Screen shipped (items 1–6); qwythos-9b ABANDONED mid-campaign; qwen25-coder-7b loaded; Screen smoke + qwen aggregate campaign next

**Date:** 2026-07-07. **Status: the leave-one-out Screen protocol was grilled, ratified,
and implemented (plan items 1–6 done and committed in apogee-sim); the qwythos-9b
aggregate campaign was killed at 16/140 and the model abandoned as unfit; the replacement
model `qwen25-coder-7b` is loaded and serving.** This doc **supersedes**
`2026-07-07 - 00 - first-aggregate-campaign-done-…-qwythos-9b-next.md`: its pickup ("run
the qwythos-9b campaign") is DEAD (model abandoned, see below) and its step-3 instruction
("grill leave-one-out before writing its plan") is DONE. Work from this directory;
apogee-sim is the sibling repo at `../apogee-sim`.

## What happened this session (don't re-derive — read the artifacts)

1. **qwythos-9b is abandoned as a campaign subject.** Loading it first required a profile
   fix (its configured vision projector gguf doesn't exist on disk → llama-server aborted;
   the two `--mmproj` extra_args are now commented out in the llama-launcher config with a
   dated note). The aggregate campaign then ran 16/140 over several hours with a climbing
   ~50 h ETA: the model grinds huge `<think>` blocks (1 h 41 m wall for one 4-turn run; an
   80-turn death spiral graded D). Killed cleanly via SIGINT — **bundle
   `~/.apogee-sim/campaigns/qwythos-9b-20260707/` preserved (16 recorded, 0 infra_failed),
   resumable in principle, kept only as the record of why qwythos was dropped.**
2. **The Screen protocol was grilled and ratified** (8 questions, owner-answered), then
   captured in three artifacts — read these, they are the ground truth:
   - `../apogee-sim/docs/adr/0013-the-leave-one-out-screen-protocol.md` — the four
     trade-offs (attribution-primary contrast; batched elimination behind the Confirmation
     gate; off-ramps never left out; fresh in-bundle comparators).
   - `../apogee-sim/docs/plans/leave-one-out-campaign-plan.md` — ratified decisions
     L1–L12 + 7 work items with acceptance criteria.
   - `../apogee-sim/CONTEXT.md` — new terms: **Protocol, Screen, Conviction,
     Confirmation campaign**.
3. **Plan items 1–6 are DONE and committed** (another session executed them; commits
   `480088d..37ddcf8` in apogee-sim): off-ramp filter in `LeaveOneOutArms()`, manifest
   `Protocol`/`BHQ` fields, `campaign run --arms` + `--enable-mechanisms`, the Screen
   analyzer/report (attribution + BH conviction + replication readout), the
   `no-evidence` verdict wording, and docs close-out. **Only item 7 (live Screen smoke)
   remains — it is HOST-REQUIRED operator work.**
4. **`qwen25-coder-7b` is the replacement second model** (Qwen2.5-Coder-7B-Instruct
   Q4_K_M, 32K context, temp 0.7), profiled, loaded, and serving on `127.0.0.1:1111`.
   Chosen deliberately as a non-reasoning instruction-tuned coder after qwythos's
   think-block failure mode.

## The pickup (in order; each launch in a terminal YOU own)

0. **Rebuild the binary first**: `make build` in `../apogee-sim` — the checked-out source
   is newer than the built `apogee-sim` binary (items 4–6 landed after the Jul 6 build).
1. **Plan item 7 — live Screen smoke** (~2–2.5 h, 17 arms × 2 tasks × 1 rep = 34 runs):
   any loaded model is fine — use the already-loaded `qwen25-coder-7b`. See the item's
   acceptance criteria in the plan (verdict must read `no-evidence`/`no-conviction`,
   NEVER `convicted` or `inferior`; resume must skip 34/34). Needs a 2-prompt subset dir
   for `--prompts-dir`.
2. **The qwen25-coder-7b aggregate campaign** (140 runs; gemma's took ~10 h, a 7B coder
   should be comparable or faster):
   `nohup caffeinate -i ./apogee-sim campaign run --model qwen25-coder-7b --endpoint
   http://127.0.0.1:1111 --reps 5 >> ~/campaign-run.log 2>&1 &` from `../apogee-sim`,
   then `campaign analyze <id>` and read per ADR 0009 discipline (non-inferior ≠ flip).
   This replaces the abandoned qwythos run as the "does a more capable model benefit?"
   evidence.
3. **The gemma Screen** (~85 h, ≈4 overnights, resumable): reload profile
   `gemma-4-e4b-it-qat` first (fingerprint hard-refusal makes a silent swap impossible),
   then `campaign run --arms leave-one-out --model gemma-4-e4b-it-qat --reps 5`. Then the
   Confirmation campaign on the pruned set via `--enable-mechanisms`. Full operator
   sequence: plan §"After this plan".

## Operational lessons (carried forward + new)

- **Long campaigns run in a user-owned terminal** (`nohup caffeinate -i … &`), never as a
  harness background task (twice reaped on 2026-07-06). `analyze` and `list` are
  seconds-long and safe from a Claude session anytime, even mid-campaign.
- **`pkill -INT -f "apogee-sim campaign run"` is the clean kill** — the scheduler is
  SIGINT-safe, records nothing mid-flight, prints the resume command (confirmed again
  this session).
- **Verify the loaded model before every launch/resume**: `llama-launcher status --json`.
  Don't switch profiles mid-campaign; don't kill-and-resume a running campaign onto a
  freshly rebuilt binary.
- **llama-launcher profile gotcha**: a profile whose `--mmproj` file is missing aborts
  the whole server load with a 30 s health-check timeout; check
  `~/.config/llama-launcher/logs/` for the real error.

## Repo / tree state at handoff

- **apogee:** no code changes this session; defaults untouched. Handoff docs `00` and
  this `01` are untracked.
- **apogee-sim:** working tree clean; `main` is **21 commits ahead of origin (unpushed)**
  — includes the whole Screen implementation. Push when convenient (optionally
  `/code-review` the Screen commits first). The gitignored `apogee-sim` binary is STALE —
  rebuild before item 7.
- **Bundles:** `gemma-4-e4b-it-qat-20260706` (complete, gate FAILED — the ADR 0009 record
  for that model), `qwythos-9b-20260707` (partial 16/140, abandoned-model record),
  `gemma-…-smoke` (harness smoke).

## Explicitly NOT next (parked — carried forward)

- **Any default-ON flip / mechanism deletion / ledger write from partial evidence** —
  disposition is L9 of the plan: ledger entries only, and only from completed campaigns.
- **Resuming qwythos-9b** — the model is abandoned; keep the partial bundle as a record.
- Family-swap arms (ADR 0012 §2); the off-ramp firing-subpopulation instrument (ADR 0013
  §3); iterated greedy elimination (ADR 0013 §2 — one Screen + one Confirmation, then
  decide); a second-round Screen decision before the Confirmation result exists.
- Mechanism-authorship SPI / headless subcommand (rejected, ADR 0015); depth-1 relaxation
  (ADR 0014 §5); mid-Exchange auto-compaction (TODO.md); apogee↔apogee-sim imports
  (ADR 0001).

## Pointers (don't re-read into context unless needed)

- Screen protocol: `../apogee-sim/docs/adr/0013-…` · plan (items + L-table + operator
  sequence): `../apogee-sim/docs/plans/leave-one-out-campaign-plan.md` · terms:
  `../apogee-sim/CONTEXT.md`
- Decision rule: `docs/adr/0009-the-ab-decision-rule.md` · Bypass floor: ADR 0006 ·
  enable surface: ADR 0015 · bench contract: ADR 0001
- Campaign machinery: `../apogee-sim/internal/campaign/` · ADR 0012 · archived harness
  plan (D1–D10): `../apogee-sim/docs/plans/archived/campaign-harness-plan.md`
- Catalogue + ledger: `docs/design/mechanism-catalogue.md` · master roadmap:
  `docs/plans/implementation-plan-apogee-merge.md`
- Superseded handoff: `2026-07-07 - 00 - first-aggregate-campaign-done-…-qwythos-9b-next.md`

## Suggested skills for the next session

- **`manage-llm-server`** — verify `qwen25-coder-7b` is still the loaded model before the
  smoke and the aggregate campaign; reload `gemma-4-e4b-it-qat` before the Screen.
- **`archive-handoffs`** — the superseded `2026-07-07 - 00` handoff can move to
  `docs/handoffs/archived/`.
- **`/code-review`** — optional, on apogee-sim's 21 unpushed commits before pushing the
  Screen implementation.
- **`/handoff`** — at the end of the next evidence session, superseding this doc.

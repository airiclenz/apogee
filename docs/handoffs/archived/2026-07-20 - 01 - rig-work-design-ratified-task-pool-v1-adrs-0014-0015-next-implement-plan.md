# Handoff — Rig-work design ratified: Task Pool v1 (ADRs 0014/0015 + implementation plan); next session implements the plan

**Date:** 2026-07-20 (supersedes `archived/2026-07-20 - 00 - qwen36-campaign-checkpoint-killed-12of14-constant-next-is-rig-work-design.md` — its next move 1, the rig-work design conversation, is concluded; this doc records the outcome). Work from this directory; apogee-sim is the sibling repo at `../apogee-sim`.

## What this session did

1. **Ran the rig-work design grill** (grill-with-docs) against the killed qwen3.6-27B
   campaign's 56-run calibration data. Every design branch resolved; decisions ratified
   in the artifacts themselves — read those, not this doc, for the content:
   - **apogee-sim ADR 0014** (`../apogee-sim/docs/adr/0014-...`) — one frozen class-wide
     **Task Pool**; per-model **Bands** measured by the graduated Discrimination
     checkpoint, contrast-blind; instrument revisions never retroactively void evidence
     (gemma's Validated entry stands as recorded).
   - **apogee-sim ADR 0015** (`../apogee-sim/docs/adr/0015-...`) — primary outcome =
     author-owned acceptance-suite pass fraction; spec-traceable hidden checks;
     efficiency secondary-only, never gate-participating.
   - **apogee-sim CONTEXT.md** — new terms *Task Pool* and *Band*; *Discrimination
     checkpoint* entry updated to select-and-continue semantics.
   - **apogee ADR 0009** — gained a Refinements pointer; gate/δ/FDR statistics untouched.
2. **Wrote the implementation plan:** `../apogee-sim/docs/plans/task-pool-v1-plan.md` —
   8 numbered items with acceptance criteria (acceptance-suite scorer → retrofit the 14 →
   ~8–12 new hard tasks + promotions → pool roster/versioning → mechanical checkpoint
   (spread/Band/verdict, working K=10) → analyzer/report → docs → shakeout dry-run).
   Plan-doc-first per convention; **nothing was implemented this session.**
3. **Key code-level finding** (context section of ADR 0015): the 3 C-constant tasks were
   *structurally C-capped* — `computeGrade` cannot exceed C without author-owned runnable
   tests (compile-only gates; uncountable `console.assert`). Handoff-00's open question
   "ceiling vs coarse grading" was neither — instrument defect, not model floor.
4. **Housekeeping:** archived handoff 00 (superseded by this doc) and apogee-sim's
   `2026-07-07 - 00` leave-one-out handoff (its sole open item, the live Screen smoke,
   was overtaken in substance by the real gemma Screen `gemma-4-e4b-it-qat-20260708`);
   auto-memory updated; both repos committed and pushed.

## Next moves

1. **Implement the plan** (fresh session): `implement-plan` on
   `../apogee-sim/docs/plans/task-pool-v1-plan.md`, forwarding `coding-standards`.
   Items 1–2 are the critical path; the plan file is the resume state.
2. **After the rig work lands:** a fresh qwen3.6-27B campaign pre-registration on
   Pool v1 — its own grill session (ADR 0014 Consequences). NOT part of the
   implementation session.
3. **Carried operator item (machine-dependent, independent):** `campaign analyze`
   `not-engaged` stamps on `qwen25-coder-14b-20260707` + `-smoke` — machine unknown,
   locate via `ls ~/.apogee-sim/campaigns` (they are NOT on the devbox).
4. **Owner's-call housekeeping (carried):** cut a release for apogee's `[Unreleased]`
   CHANGELOG block; decide whether to unload qwen3.6 (server left UP and idle);
   paperwork — LOO plan item 7 was never formally stamped ✅ (satisfied in substance by
   the live gemma Screen 20260708); stamping it and archiving that plan is an owner call.
5. **Carried deferred follow-ups (04–08, none urgent):** TUI in-transcript banner for
   the validated-set notice; behavioral-probe (medium-confidence) resolver; user-run
   validation tooling writing `~/.apogee/validated/`.

## Operational state at handoff

- Both repos on `main`, committed and pushed this session (apogee: ADR 0009 pointer +
  this doc + archival; apogee-sim: ADRs 0014/0015, CONTEXT.md, plan doc, archival).
  Verify with `git status -sb`, don't trust this doc.
- **Killed qwen3.6 bundle** untouched on the devbox at 56/140 — incomplete by design,
  out of L9, do not top up. Its 56 runs are the plan's calibration data
  (`~/.apogee-sim/campaigns/users-airic-ll-models-qwen-qwen3-6-27b-q4-k-s-gguf-20260719/`).
- **Server UP and idle:** qwen3.6-27B-Q4_K_S on llama.cpp `b10068`, single slot, devbox
  view `http://192.168.64.1:1111`; llama-launcher MCP `http://192.168.64.1:7331/mcp`.
  Sandboxed Bash cannot reach the host — use unsandboxed curl; the MCP handshake pattern
  is in the campaigns memory file.

## Explicitly NOT next

- No campaign launch until the rig work lands; the qwen3.6 pre-registration is a
  separate future session.
- No re-run or top-up of the killed bundle; no L9 or Validated-set writes; no gemma
  re-earning (ADR 0014 §3).
- No per-Mechanism task picking, ever (ADR 0014 §2); no gate/δ/FDR statistics changes
  beyond the plan's N>20 path verification.
- Don't re-grill the ratified design — read ADRs 0014/0015 + the plan first.

## Suggested skills

- **`implement-plan`** — next move 1 (pass the plan path; forward `coding-standards`).
- **`grill-with-docs`** — later, for the qwen3.6-27B pre-registration on Pool v1.
- **`manage-llm-server`** — server observation / the unload decision (mutating calls
  only with owner confirmation).
- **`handoff`** + **`archive-handoffs`** — at session end, superseding this doc.

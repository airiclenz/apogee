# Handoff — qwen3.6-27B first aggregate Campaign pre-registered and pushed; next: host-side launch, or devbox work while it runs

**Date:** 2026-07-19 (supersedes `archived/2026-07-19 - 04 - validated-set-runtime-surface-shipped-next-is-campaign-design-or-housekeeping.md` — its move 1, next-campaign design, is done this session; its move 2, the qwen25 housekeeping, is folded into the new campaign's runbook; its move 3 surface follow-ups carry forward). Work from this directory; apogee-sim is the sibling repo at `../apogee-sim`.

## Where things stand

- **The next campaign is decided and pre-registered — do not re-derive it.** A
  grill session resolved the ADR 0016 consequence list ("transfer, superiority, or
  nothing") as: a fresh per-model validation aggregate on **qwen3.6-27B**, full
  `CandidateArm()` base stack vs Bypass, 14 × 2 × 5 = 140 runs, gated by a
  pre-registered **Discrimination checkpoint** at 56/140 (staged reps inside the one
  immutable manifest — no throwaway smoke). The complete design record (D1–D7),
  pre-registration, checkpoint rule, disposition table, and 7-step operator runbook:
  **`../apogee-sim/docs/plans/qwen36-27b-first-aggregate-campaign-plan.md`** (apogee-sim
  `6634376`, pushed).
- **"Discrimination checkpoint" is now a glossary term** in apogee-sim `CONTEXT.md`
  (beside Engagement and Tool-call preflight): a pre-registered mid-Campaign
  powered-ness gate — engagement stamp + per-task grade-constancy, never the arm
  contrast. No ADR was minted (reversible protocol refinement; the grill's reasoning is
  in the plan doc's D7).
- **Licensing decided up front:** an NI pass meets ADR 0016 §4 non-exploratorily and
  enters qwen3.6-27B's Validated set immediately (catalogue row + `shipped.json` row +
  pin test); a superiority pass licenses a ledger claim only (Recommended tier stays a
  future design); a checkpoint kill stays out of L9 and flips the conversation to
  instrument work.
- **Two doc corrections from a /refocus truth-check landed** (apogee `c66efd7`): the
  merge plan's Phase-5 confinement reference now points through ADR 0004 to ADR 0012,
  and CONTEXT.md marks the `apogee headless` CLI as deferred (no subcommands ship —
  `cmd/apogee/root.go`). Remaining known doc staleness is deliberate: `technical-design.md`
  self-flags as frozen at Phase-1 vintage; the merge plan's "~30-tool suite" is the
  historical apogee-code count (the shipped suite is 19 + conditional `ask_user`).

## Next moves

1. **Host-side: execute the runbook** (operator, on the Mac host — nothing launches
   from the devbox). Steps 1–7 in the plan doc. The load-bearing trap, restated: the
   host's apogee-sim binary is still the **Jul 8 build**, which predates both the
   tool-call preflight and the engagement guard — `make build` on the host is step 1
   and a hard prerequisite. Step 2 closes the carried qwen25 bundle re-analysis
   housekeeping with the same fresh binary.
2. **Devbox, while the campaign runs (or instead, if launch waits):** the
   architecture-deepening plan (`docs/plans/architecture-deepening-plan.md`) is READY
   and verified entirely unstarted — no ADR 0017, no `domaintest`, no `ExchangeView`
   anywhere in code. It is independent of all bench work. Run it via `implement-plan`
   with `coding-standards`.
3. **After the campaign completes (devbox, next session):** L9 ledger entry in
   `docs/design/mechanism-catalogue.md` whatever the outcome; on an NI pass the
   Validated-set writes per the plan doc's disposition table (catalogue table +
   `internal/validated/shipped.json` + `shipped_test.go` pin move together).
4. **Surface follow-ups (carried from 04, still deferred, none urgent):** TUI
   in-transcript banner for the validated-set notice; the behavioral-probe
   (medium-confidence) resolver; user-run validation tooling writing
   `~/.apogee/validated/`.

## Operational state at handoff

- Linux container on the Mac host. **Both repos clean and level with origin:** apogee
  `main` = `c66efd7` (this handoff commit on top), apogee-sim `main` = `6634376`.
- **Server state NOT re-verified this session** (no live-model work). Last-known facts,
  re-verify before launch (runbook step 3): endpoint `http://192.168.64.1:1111`,
  qwen3.6-27B-Q4_K_S on llama.cpp `b10068`, single slot; llama-launcher MCP at
  `http://192.168.64.1:7331/mcp`.
- GitHub: zero open issues, zero open PRs on apogee.

## Explicitly NOT next (carried forward, plus the grill's new rejections)

- Any apogee default flip / mechanism deletion from current evidence; a Confirmation
  campaign (no convicted set); iterated greedy elimination, family-swap arms, off-ramp
  SPI, depth-1 relaxation, mid-Exchange auto-compaction, apogee↔apogee-sim imports;
  plumbing `ModelProfile` through `coreagent.RunConfig`; engine-level auto-enable or a
  public embedder API for Validated sets (all carried from 04).
- **New from the grill:** running gemma's pruned 16 on qwen as the candidate (set
  transfer is a human alias, ADR 0016 §3); any 3-arm design; building the Recommended
  tier off a superiority pass; pre-committing a qwen Screen; an ADR for the
  Discrimination checkpoint.

## Suggested skills

- **`manage-llm-server`** — runbook step 3; re-verify the endpoint facts above before
  any live-model work.
- **`implement-plan`** (with `coding-standards`) — if the session picks move 2, the
  architecture-deepening plan is the target: `implement-plan
  docs/plans/architecture-deepening-plan.md with skills: coding-standards`.
- **`grill-with-docs`** — only if a campaign outcome (checkpoint kill, gate fail)
  reopens design; the pre-registration itself is settled, do not re-grill it.
- **`handoff`** — at session end, superseding this doc.

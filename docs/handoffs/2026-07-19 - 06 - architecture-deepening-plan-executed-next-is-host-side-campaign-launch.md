# Handoff — architecture-deepening plan executed and archived; next: host-side campaign launch (unchanged), or surface follow-ups

**Date:** 2026-07-19 (supersedes `archived/2026-07-19 - 05 - qwen36-27b-campaign-pre-registered-next-is-host-side-launch.md` — its move 2, the architecture-deepening plan, is done this session; its moves 1/3/4 carry forward unchanged). Work from this directory; apogee-sim is the sibling repo at `../apogee-sim`.

## Where things stand

- **The architecture-deepening plan ran to completion this session** via
  `implement-plan` + `coding-standards`: all 9 items implemented, independently
  verified, and committed one-by-one (`df942c1` → `cc3e175`), then archived by the
  closeout backstop (`1685237`, full `make check` green). The plan — including the
  dated NOTES lines that record every authorized deviation — is now
  `docs/plans/archived/architecture-deepening-plan.md`; the change list is
  CHANGELOG `[Unreleased]`. Highlights: ADR 0017 ratified (Exchange = derived domain
  working value), `internal/domain/domaintest` (hook-seam test adapter),
  `domain.ExchangeView`, `Budget` owns token arithmetic (two additive methods →
  minor), shared history-scan helpers, `toolSpec` embedding + typed arg decoding
  across all 20 built-in tools (metadata byte-identical, overlay-probe verified).
- **The one substantive design event — do not re-derive it:** item 4's precondition
  verification FAILED (`truncate_history` CAN drop the open Exchange's opening user
  message; pinned by `TestExchangeStartRepairedAfterMidExchangeTruncation`), so the
  owner chose D2's pre-registered fallback: the cached `exchangeStart` + S2 repair
  STAY, readers route through the engine's `exchangeBoundary()` helper, and
  `closeExchange` concentrates the Exchange end as planned. Trail: plan NOTES under
  item 4 + ADR 0017 §2 realisation note. A pure-derivation engine boundary is
  therefore known-unsound while the user-role gap note exists — any future attempt
  needs a design session first (behaviour change, unauthorized).
- **The next campaign remains decided, pre-registered, and NOT launched** — the
  qwen3.6-27B first aggregate (140 runs, Discrimination checkpoint at 56/140):
  `../apogee-sim/docs/plans/qwen36-27b-first-aggregate-campaign-plan.md` (apogee-sim
  `6634376`). Nothing bench-related happened this session; the host binary is
  presumably still the Jul 8 build (`make build` on the host is runbook step 1).
- **Doc hygiene this session:** `docs/plans/validated-set-runtime-surface.md` →
  `archived/` (shipped in `48485f9`; its deferrals are explicit non-goals carried
  here). Handoff 05 → `archived/`. The only live plan is
  `docs/plans/implementation-plan-apogee-merge.md` (umbrella; Phase 5 open).

## Next moves

1. **Housekeeping first:** the working tree holds the uncommitted archive moves
   (validated-set plan, handoff 05) plus this doc — commit when the owner says so
   (pattern: `docs: new handoff and archival of superseded docs`). Everything else
   is committed but **NOT pushed** — apogee `main` is ~11 commits ahead of origin;
   push only on the owner's ask.
2. **Host-side: execute the campaign runbook** (operator, on the Mac host — nothing
   launches from the devbox). Steps 1–7 in the apogee-sim plan doc; step 1 is the
   host `make build` (Jul 8 binary predates the tool-call preflight and engagement
   guard); step 2 closes the carried qwen25 bundle re-analysis.
3. **After the campaign completes (devbox):** L9 ledger entry in
   `../apogee-sim/docs/design/mechanism-catalogue.md` whatever the outcome; on an NI
   pass the Validated-set writes per the plan doc's disposition table.
4. **Surface follow-ups (carried from 04/05, still deferred, none urgent):** TUI
   in-transcript banner for the validated-set notice; the behavioral-probe
   (medium-confidence) resolver; user-run validation tooling writing
   `~/.apogee/validated/`. Also unclaimed: cutting a release for the
   `[Unreleased]` CHANGELOG block (owner's call on timing).

## Operational state at handoff

- Linux container on the Mac host. apogee `main` = `1685237` local (origin at
  `2c102f9`); working tree = the archive moves + this doc, nothing else. apogee-sim
  `main` = `6634376`, clean, level with origin.
- **Server state NOT re-verified this session** (no live-model work). Last-known
  facts, re-verify before launch (runbook step 3): endpoint
  `http://192.168.64.1:1111`, qwen3.6-27B-Q4_K_S on llama.cpp `b10068`, single slot;
  llama-launcher MCP at `http://192.168.64.1:7331/mcp`.
- GitHub: zero open issues, zero open PRs on apogee.

## Explicitly NOT next (carried forward)

- All of handoff 05's list, verbatim in `archived/…05…`: no apogee default flip /
  mechanism deletion from current evidence; no Confirmation campaign; no iterated
  greedy elimination, family-swap arms, off-ramp SPI, depth-1 relaxation,
  mid-Exchange auto-compaction, apogee↔apogee-sim imports; no `ModelProfile` through
  `coreagent.RunConfig`; no engine-level auto-enable / public embedder API for
  Validated sets; no running gemma's pruned 16 on qwen as candidate; no 3-arm
  design; no Recommended tier off a superiority pass; no pre-committed qwen Screen;
  no ADR for the Discrimination checkpoint.
- **New from this session:** no revisiting the item-4 fallback (deleting
  `exchangeStart` in favour of pure derivation) without a design session — the
  invariant it needs is provably violated today.

## Suggested skills

- **`manage-llm-server`** — runbook step 3; re-verify the endpoint facts above
  before any live-model work.
- **`grill-with-docs`** — only if a campaign outcome (checkpoint kill, gate fail)
  reopens design, or if someone wants the item-4 pure-derivation idea; the campaign
  pre-registration itself is settled, do not re-grill it.
- **`handoff`** — at session end, superseding this doc.

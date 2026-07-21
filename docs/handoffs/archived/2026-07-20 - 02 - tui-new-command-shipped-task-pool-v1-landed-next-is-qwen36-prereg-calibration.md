# Handoff — `/new` TUI command shipped; refocus truth-check; Task Pool v1 has landed → next research move is the qwen3.6-27B pre-registration (calibrate difficulty first)

**Date:** 2026-07-20 (supersedes `archived/2026-07-20 - 01 - rig-work-design-ratified-task-pool-v1-adrs-0014-0015-next-implement-plan.md` — its headline next-move, "implement the Task Pool v1 plan", is now DONE in apogee-sim; this doc records that and carries the still-open items forward). Work from this directory (`apogee`); apogee-sim is the sibling repo at `../apogee-sim`.

## What this session did

1. **Shipped a product feature — `/new` TUI command (alias of `/clear`).** Committed to `main`
   as `5f6209a` (`feat(tui): /new command as an alias of /clear`). The chat mini-language now
   recognises `/new` and routes it through the **same** synchronous context-reset path as `/clear`
   (`case "clear", "new"` in `internal/tui/model.go` `runCommand` → `Engine.ClearContext`, stays
   idle, launches no worker); the `/` autocomplete menu offers it. Full detail is in the CHANGELOG
   `[Unreleased] → Added` entry + the commit diff — **not duplicated here.**
   - **Design note (matters for the parked follow-on):** `/new` == `/clear` — it drops the
     *in-process* conversation history. It does **not** create or switch a session *file*; that is
     the still-parked "session-management UI" item in `TODO.md`. If `/new` should later mean "new
     *persisted* session," that is a separate, larger feature.
   - Touched: `internal/tui/{command,model,autocomplete,doc}.go` + tests (`command_test.go`,
     `minilang_test.go`) + `CHANGELOG.md`. `gofmt -l` clean, `go vet` clean, `go test -race
     ./internal/tui/` green. Built with `coding-standards` (Go extension) loaded.

2. **Ran `/refocus` (read-only) and truth-checked the docs.** The load-bearing finding:
   handoff-01's headline next-move — *implement the Task Pool v1 plan in apogee-sim* — is **already
   done**. apogee-sim `main` shows all items landed and the plan archived (`chore(plans): archive
   completed task-pool-v1 plan`), matching the campaigns auto-memory. Every other load-bearing doc
   claim verified against code (20-tool suite, four autonomy modes, `internal/validated` + shipped
   gemma set, ADR 0017 `ExchangeView`/`CurrentExchange`, `Budget.EstimateTokens`/
   `HistoryExceedsAllocation`, landlock/seatbelt confiners; clean build). The one stale claim
   (handoff-01's next-move) is resolved by superseding it with this doc.

## Where things actually stand

- **apogee product:** stable at `v1.3.0` (2026-07-05). A sizable `[Unreleased]` CHANGELOG block has
  accrued (ADR 0016 Validated-set runtime surface; ADR 0017 Exchange-as-derived-value; the
  architecture-deepening D1–D4 consolidations; and now `/new`) — **no release cut yet** (owner's
  call, carried below).
- **Broad merge plan (`docs/plans/implementation-plan-apogee-merge.md`) is STILL ACTIVE — not
  archived.** Phases 0–4 are ✅ complete; **Phase 5 (cross-platform hardening: the Windows
  shell/path backend + the Windows `Confiner`, `apogee probe`, proxy retirement) is OPEN.** Windows
  Auto-mode is gated on it (code `TODO(phase-5)` markers in `internal/tools/exec_pgroup_other.go`
  and `internal/platform/platform_windows.go`). This is why the plan folder still has one active
  plan after this session's archival pass.
- **apogee-sim research:** Task Pool v1 IMPLEMENTED & LANDED on sim `main` (acceptance-fraction
  scorer, 24-task `pool.yaml`, mechanical `campaign checkpoint` at K=10, gate/δ over the Band,
  Band-continue resume wiring; plan archived). ADRs 0014 (class-wide Task Pool + measured Bands) /
  0015 (acceptance-fraction primary) ratified. Detail lives in the apogee-sim repo, not here.

## Next moves

1. **[research frontier — apogee-sim] qwen3.6-27B pre-registration campaign on Task Pool v1.** Now
   unblocked (the rig work has landed). Its own `grill-with-docs` pre-registration session (ADR 0014
   Consequences). **Resolve the difficulty signal FIRST:** the Pool v1 shakeout had qwen3.6-27B
   solving every real task at acceptance-fraction 1.0 — a turn-capped **non-signal**, not mastery —
   so calibrate task difficulty before pre-registering. qwen3.6-27B is left LOADED & idle on `:1111`
   (unload = owner call).
2. **[apogee — owner's call] Cut a release** for the `[Unreleased]` block.
3. **[operator, machine-dependent] `campaign analyze` `not-engaged` stamps** on
   `qwen25-coder-14b-20260707` + `-smoke` — machine unknown; locate via `ls ~/.apogee-sim/campaigns`
   (they are NOT on the devbox).
4. **[apogee — parked, additive] Product backlog** (all in `TODO.md` / `ISSUES.md`; none active or
   blocking): the chat text-selection bug (`ISSUES.md` — the only real defect, not an enhancement);
   `/server` live server/model switching (P1, needs a swappable provider seam); in-TUI
   session-management UI (P1); raw-protocol inspector (P2); the parked tool×mode security matrix +
   url-safety config keys; the general system-prompt template.
5. **[carried paperwork — owner's call]** apogee-sim leave-one-out plan item 7 was never formally
   stamped ✅ (satisfied in substance by the live gemma Screen `gemma-4-e4b-it-qat-20260708`);
   stamping + archiving that sim plan is an owner call. Deferred follow-ups 04–08 (validated-set
   in-transcript TUI banner; behavioral-probe medium-confidence resolver; user-run validation
   tooling writing `~/.apogee/validated/`).

## Explicitly NOT next

- No re-grill of the ratified Task Pool v1 design (ADRs 0014/0015) — read them + the archived sim
  plan first.
- No re-run / top-up of the killed qwen3.6 bundle (56/140, out of L9, calibration data only); no L9
  or Validated-set writes; no gemma re-earning (ADR 0014 §3).
- The old bar "no campaign launch until the rig work lands" is **lifted** — the rig work has landed;
  the qwen3.6 pre-registration (move 1) is the path, but the difficulty calibration gates it.

## Operational state at handoff

- **apogee:** on `main`. The `/new` feature (`5f6209a`), this handoff, and the handoff-01 archival
  are committed and pushed to `origin/main` this session (this doc's own commit closes it). Verify
  with `git status -sb` / `git log --oneline -3` — **don't trust this doc.**
- **apogee-sim:** on `main`, Task Pool v1 landed & pushed (per its git log). Verify in that repo.
- **Server:** qwen3.6-27B-Q4_K_S UP and idle on llama.cpp, devbox view `http://192.168.64.1:1111`;
  llama-launcher MCP `http://192.168.64.1:7331/mcp`. Sandboxed Bash cannot reach the host — use
  unsandboxed curl; the MCP handshake pattern is in the campaigns auto-memory file.

## Suggested skills

- **`grill-with-docs`** — next move 1, the qwen3.6-27B pre-registration on Pool v1 (put the
  difficulty-calibration question front-and-centre).
- **`manage-llm-server`** — server observation / the unload decision (mutating calls only with
  owner confirmation).
- **`coding-standards`** — mandatory for any new Go (merge-plan Standing Requirement 1), e.g. the
  product-backlog work in move 4.
- **`feature-implementation`** — a single parked product item (the text-selection fix or `/server`).
- **`brew-release`** — move 2, cutting the `[Unreleased]` release.
- **`handoff`** + **`archive-handoffs`** — at session end, superseding this doc.

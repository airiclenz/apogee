# Handoff — docs truth-checked and corrected, `ISSUES.md` closed out → next move is **cutting the release** for the `[Unreleased]` block

**Date:** 2026-07-21 (supersedes `archived/2026-07-20 - 02 - tui-new-command-shipped-task-pool-v1-landed-next-is-qwen36-prereg-calibration.md` — its next-move list is carried forward here, minus the text-selection defect, which is now closed). Work from this directory (`apogee`); apogee-sim is the sibling repo at `../apogee-sim`.

## What this session did

1. **Ran `/refocus` — a full read-only truth-check of the docs against the code.** Build, `go vet`
   and the whole suite were green (1032 test funcs, 0 fail, 6 environment-gated skips: 2 need
   `APOGEE_LIVE_ENDPOINT`, 2 are macOS-only, 2 are the landlock escape-probes). Confirmed against
   code: the 20-tool suite, 21 catalogued mechanisms all default-off, the four-mode ladder, the
   flag > env > file > default precedence chain and the file-only key set, the unknown-mechanism
   startup refusal, auto-compaction on-by-default and surviving `--bypass`, the shipped gemma
   Validated set, every `make` target the README tabulates, and "no subcommands ship yet".

2. **Fixed five documentation discrepancies — committed as `ac3b856`** (`docs: truth-up the
   confinement/Auto wording and close the text-selection issue`). The commit message enumerates all
   five with their rationale — **read it rather than re-deriving them.** The load-bearing one, in
   case it matters later: `README` claimed Auto mode "is not yet available" on Windows; under
   ADR 0012 + `confinement-execution-contract` §4–5 the gate refuses only a **nil** `Confiner`, so a
   present-but-incapable backend enters Auto and falls back to Approval on the subprocess surface
   ("confine if you can, gate if you can't"). `internal/domain/errors.go`'s comment said the
   opposite of the code and is now corrected. Docs and comments only — **no behaviour change.**

3. **Closed the text-selection defect.** The owner re-verified in a live terminal that drag-select-
   to-copy works (shipped in `ffd1cd5`, 2026-07-03) and removed the `ISSUES.md` line himself; the
   2026-07-04 re-open (`b8fc36c`) was stale. That commit is folded into `ac3b856`.

## Where things actually stand

- **The product has no known bugs.** `ISSUES.md` is down to one entry, and it is a pointer into
  `TODO.md`'s feature-parity list, not a defect. Everything outstanding is planned work.
- **apogee:** stable at `v1.3.0` (2026-07-05) with a sizable, still-uncut `[Unreleased]` CHANGELOG
  block (ADR 0016 Validated-set runtime surface; ADR 0017 Exchange-as-derived-value; the
  architecture-deepening D1–D4 consolidations; `/new`; the guided-decomposition F1–F5 fixes; and
  now this session's doc corrections).
- **Broad merge plan (`docs/plans/implementation-plan-apogee-merge.md`) is STILL ACTIVE.** Phases
  0–4 ✅; **Phase 5 (Windows shell/path backend + Windows `Confiner`, `apogee probe`, proxy
  retirement) is OPEN.** Its two code markers are now correctly labelled `TODO(phase-5)` in
  `internal/platform/platform.go` (they had outlived Phase 3 as `TODO(phase-3)`).
- **apogee-sim research:** unchanged this session — Task Pool v1 landed, ADRs 0014/0015 ratified.

## Next moves

1. **[apogee — THIS IS THE HEADLINE] Cut the release for the `[Unreleased]` block.** The owner has
   designated it as the next session's work. Everything is staged for it: tree clean, tests green,
   no known bugs. Notes for whoever does it:
   - Additive only (new Events / hook points / API surface are a **minor** bump per the CHANGELOG
     header note), so `v1.4.0` is the expected number — but confirm with the owner.
   - **Pre-existing wart to decide on:** there is **no `v1.1.0` git tag** even though the CHANGELOG
     has a `## [1.1.0] — 2026-07-03` section. Tags today are `v1.0.0`, `v1.2.0`, `v1.3.0`. Either
     backfill it or consciously leave it; it is cosmetic. Do not silently ignore it.
   - The `brew-release` skill covers the mechanics (version bump → tag → GitHub release → tarball
     hash → formula update in the sibling `../homebrew-tap`).
2. **[research frontier — apogee-sim] qwen3.6-27B pre-registration campaign on Task Pool v1.**
   Unblocked, but **resolve the difficulty signal FIRST:** the Pool v1 shakeout had qwen3.6-27B
   solving every real task at acceptance-fraction 1.0 — a turn-capped **non-signal**, not mastery —
   so calibrate task difficulty before pre-registering. Its own `grill-with-docs` session
   (ADR 0014 Consequences). qwen3.6-27B is left LOADED & idle on `:1111` (unload = owner call).
3. **[operator, machine-dependent] `campaign analyze` `not-engaged` stamps** on
   `qwen25-coder-14b-20260707` + `-smoke` — machine unknown; locate via `ls ~/.apogee-sim/campaigns`
   (they are NOT on the devbox).
4. **[apogee — parked, additive] Product backlog** (all in `TODO.md`; none active or blocking, and
   **none is now a defect**): `/server` live server/model switching (P1, needs a swappable provider
   seam — `upstream` is immutable after construction); in-TUI session-management UI (P1);
   raw-protocol inspector (P2); undo-all-agent-changes (P2); the parked tool×mode security matrix +
   url-safety config keys; the general system-prompt template.
5. **[carried paperwork — owner's call]** apogee-sim leave-one-out plan item 7 was never formally
   stamped ✅ (satisfied in substance by the live gemma Screen `gemma-4-e4b-it-qat-20260708`);
   stamping + archiving that sim plan is an owner call. Deferred follow-ups 04–08 (validated-set
   in-transcript TUI banner; behavioral-probe medium-confidence resolver; user-run validation
   tooling writing `~/.apogee/validated/`).

## Explicitly NOT next

- **Do not re-run the `/refocus` truth-check.** It was just done end-to-end and the findings are
  committed; the docs and the code agree as of `ac3b856`.
- No re-grill of the ratified Task Pool v1 design (ADRs 0014/0015) — read them + the archived sim
  plan first. No re-run / top-up of the killed qwen3.6 bundle; no L9 or Validated-set writes; no
  gemma re-earning (ADR 0014 §3).
- **Do not reopen the text-selection issue** without a fresh live reproduction — it was verified
  working by the owner on 2026-07-21.

## Operational state at handoff

- **apogee:** on `main`. `ac3b856` (doc corrections) plus this handoff and the archival of
  handoff-02 are committed and pushed to `origin/main` this session. Verify with `git status -sb` /
  `git log --oneline -3` — **don't trust this doc.**
- **apogee-sim:** on `main`, Task Pool v1 landed & pushed. Verify in that repo.
- **Server:** qwen3.6-27B-Q4_K_S UP and idle on llama.cpp, devbox view `http://192.168.64.1:1111`;
  llama-launcher MCP `http://192.168.64.1:7331/mcp`. Sandboxed Bash cannot reach the host — use
  unsandboxed curl; the MCP handshake pattern is in the campaigns auto-memory file.

## Suggested skills

- **`brew-release`** — next move 1, the release cut. This is the session's main event.
- **`grill-with-docs`** — next move 2, the qwen3.6-27B pre-registration on Pool v1 (put the
  difficulty-calibration question front-and-centre).
- **`manage-llm-server`** — server observation / the unload decision (mutating calls only with
  owner confirmation).
- **`coding-standards`** — mandatory for any new Go (merge-plan Standing Requirement 1), e.g. the
  product-backlog work in move 4.
- **`feature-implementation`** — a single parked product item (`/server`, or the session-management
  UI).
- **`handoff`** + **`archive-handoffs`** — at session end, superseding this doc.

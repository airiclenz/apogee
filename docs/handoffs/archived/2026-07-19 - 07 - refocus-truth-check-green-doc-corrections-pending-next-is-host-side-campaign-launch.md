# Handoff — refocus truth-check: repo verified green, three doc corrections pending; next: host-side campaign launch (unchanged)

**Date:** 2026-07-19 (supersedes `archived/2026-07-19 - 06 - architecture-deepening-plan-executed-next-is-host-side-campaign-launch.md` — its move 1, the housekeeping commit, landed as `0225a86` this session; its moves 2/3/4 carry forward unchanged). Work from this directory; apogee-sim is the sibling repo at `../apogee-sim`.

## What this session was

A `/refocus` sweep: full workspace briefing plus a documentation truth-check
(three parallel read-only surveys — docs inventory, code map, planned-work — then
claim-by-claim verification against code). **No code or doc was modified.** The
findings below are the session's only new material; everything strategic is
unchanged from handoff 06.

## Verified state (all checked against code this session)

- **Repo health:** `go build ./...` clean; full `go test ./...` green across all
  19 packages. Working tree clean at `0225a86`. Latest tag `v1.3.0` (2026-07-05);
  51 unreleased commits since. GitHub: zero open issues, zero open PRs.
- **Docs are substantially truthful.** Confirmed against code: 20 built-in tools
  (20 `toolSpec` literals); config precedence and `~/.apogee` layout;
  `confine-to-workspace` defaults true and is global-file-only
  (`cmd/apogee/config.go:158`); auto-compact default-on;
  `AutoEligible()` = FSWrite-only (`internal/domain/confinement.go:62`); the
  gemma-4-e4b-it-qat Validated set is compiled in via `//go:embed shipped.json`
  (`internal/validated/shipped.go`); the ADR 0017 fallback is in code exactly as
  recorded (cached `exchangeStart` + S2 repair, readers via
  `Agent.exchangeBoundary()` — `internal/agent/agent.go:76,186`,
  `loop.go:255,272`).
- **README nuance, no action needed:** "the first Validated set ships with the
  binary" is true for any build from `main`, but no *tagged release* contains it
  yet — it sits in CHANGELOG `[Unreleased]` above v1.3.0.
- **Not verified (deliberately skipped):** `layout.md` (2026-06-24) against the
  current TUI rendering; `make cross`'s six-target claim; the MCP SDK version pin;
  host server/campaign state (unobservable from the devbox except via the
  llama-launcher MCP). `docs/design/technical-design.md` is stale by its own
  notice (frozen at Phase-1 vintage) — known, not a finding.

## Doc corrections found — PENDING, not yet approved or applied

Offered to the owner at session end; no reply yet. Apply only what the owner
approves, showing exact wording first:

1. `internal/mechanisms/doc.go:7` says "Phase-0 scaffold: no implementation yet;
   the catalogue is ported … in Phase 4" and `internal/mechanisms/catalogue.go:64`
   says the catalogue "starts EMPTY" — stale since Phase 4 completed 2026-07-04;
   22 mechanism constructors are registered across 19 files.
2. `docs/plans/implementation-plan-apogee-merge.md` Phase 3 says "Complete the
   **30-tool suite**" — the suite is 20 tools (README and CHANGELOG both say 20).
3. `internal/tui/command.go:38` says "/skill and /server are deferred (they need
   the skills package / a swappable provider seam)" — `/server` is genuinely
   deferred, but `/skill` shipped 2026-06-26 via the autocomplete chip-attach
   path, deliberately outside `knownCommands` (design note at
   `internal/tui/autocomplete.go:133-137`); the stated reason is stale.

## Next moves

1. **Host-side: execute the campaign runbook** (operator, on the Mac host —
   nothing launches from the devbox). The qwen3.6-27B first aggregate, 140 runs,
   Discrimination checkpoint at 56/140:
   `../apogee-sim/docs/plans/qwen36-27b-first-aggregate-campaign-plan.md`.
   Step 1 is the host `make build` (Jul 8 binary predates the tool-call preflight
   and engagement guard); step 3 re-verifies server state.
2. **After the campaign completes (devbox):** L9 ledger entry in
   `../apogee-sim/docs/design/mechanism-catalogue.md` whatever the outcome; on an
   NI pass the Validated-set writes per the plan doc's disposition table.
3. **Owner's-call housekeeping, both unclaimed:** push apogee `main` (11 commits
   ahead of origin at `2c102f9`); cut a release for the `[Unreleased]` CHANGELOG
   block.
4. **Apply the approved subset of the doc corrections above** (two-minute task).
5. **Surface follow-ups (carried from 04/05/06, still deferred, none urgent):**
   TUI in-transcript banner for the validated-set notice; the behavioral-probe
   (medium-confidence) resolver; user-run validation tooling writing
   `~/.apogee/validated/`.

## Operational state at handoff

- Linux container on the Mac host. apogee `main` = `0225a86` local, clean
  (origin at `2c102f9`, 11 ahead / 0 behind). apogee-sim not touched this
  session (last known: `6634376`, clean, level with origin).
- **Server state NOT re-verified this session** (no live-model work). Last-known
  facts, re-verify before launch (runbook step 3): endpoint
  `http://192.168.64.1:1111`, qwen3.6-27B-Q4_K_S on llama.cpp `b10068`, single
  slot; llama-launcher MCP at `http://192.168.64.1:7331/mcp`.

## Explicitly NOT next (carried forward)

- All of handoff 06's list, verbatim in `archived/…06…` (which itself carries
  05's): no default flips / mechanism deletions from current evidence, no new
  campaign designs, and — from the deepening session — **no revisiting the
  item-4 fallback** (deleting `exchangeStart` in favour of pure derivation)
  without a design session; the invariant it needs is provably violated today.
- New from this session: none. The truth-check surfaced no design questions —
  only the three comment/count staleness items above.

## Suggested skills

- **`manage-llm-server`** — runbook step 3; re-verify the endpoint facts above
  before any live-model work.
- **`grill-with-docs`** — only if a campaign outcome (checkpoint kill, gate
  fail) reopens design; the campaign pre-registration is settled, do not
  re-grill it.
- **`handoff`** — at session end, superseding this doc.

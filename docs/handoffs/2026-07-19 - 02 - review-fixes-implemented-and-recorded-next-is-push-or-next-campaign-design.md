# Handoff — engagement-guard review fixes implemented (`bd4bb81`) and recorded (`5f890b9`); next: push both repos, then qwen fix / Validated-set runtime surface / next-campaign design

**Date:** 2026-07-19 (supersedes `archived/2026-07-19 - 01 - engagement-guard-committed-with-review-fix-plan-next-is-implement-plan.md` — its next move 1 is done: the review-fix plan is implemented, committed, and recorded). Work from this directory; apogee-sim is the sibling repo at `../apogee-sim`.

**Amended later the same session:** next move 1 (the pushes) is done — both repos are
in sync with origin (apogee `ec91f89`, apogee-sim `5f890b9`), and the ⚠️ `go.sum`
drift below was committed as `ec91f89` (7 added `/go.mod` hash lines, nothing removed).
The remaining next moves are 2–5.

## Where things stand

- **ADR 0016 is accepted** (apogee `141dcb0`, unchanged): per-model **Validated sets**
  keyed by the confidence-tagged fingerprint; non-inferiority is the bar; auto-enable at
  ≥ medium confidence with notice + off-switch; gemma-4-e4b-it-qat holds the first set
  (the pruned 16). See `docs/adr/0016-curation-is-per-model-validated-sets-keyed-by-fingerprint.md`.
- **The engagement guard is committed** (apogee-sim `1153ef0`) **and its review-fix plan
  is fully implemented** (apogee-sim `bd4bb81`, this session): all 6 items, no
  deviations, no `NOTES:` lines needed. Report wording fixes (`renderScreen` dispatches
  on Screen state, not the overridable verdict string; `renderEngagement` is
  completeness-aware), five new test pins plus strengthened assertions, doc truth
  restored (CONTEXT.md gains *unmeasured*; `analyze.go`/`report.go` comments name the
  guard). Full record: the top entry in apogee-sim `CHANGELOG.md`; the plan is archived
  with a ✅ COMPLETE header at
  `../apogee-sim/docs/plans/archived/engagement-guard-review-fixes-plan.md` (`5f890b9`).
- **Note for the next agent:** the plan was written for an **implement-plan** skill that
  does not exist in this environment (checked `~/.claude/skills` and both repos); it was
  executed directly from the plan's own precedence/conventions sections, which proved
  sufficient. Item-1's new tests were verified to fail against the pre-fix `report.go`.
- Gates green throughout: `gofmt` clean, `go build ./...`, full `go test ./...`.

## Next moves (operator picks; none is pre-committed)

1. **Push both repos** — nothing has been pushed for three sessions: apogee-sim is
   **4 ahead** of origin (`1153ef0`, `71f41a1`, `bd4bb81`, `5f890b9`; origin at
   `777af3f`); apogee is 1 ahead (`6e3ec9e`) and will be 2 ahead after the commit
   carrying this handoff. `pr-lifecycle` covers the flow if PRs are wanted.
2. **qwen tool-call protocol fix** — the open other half of post-mortem consequence (c):
   chat-template/parser investigation of the fenced-JSON pseudo-tool-calls (see the qwen
   entry's 2026-07-08 amendment in `docs/design/mechanism-catalogue.md`). Live testing
   needs an LLM server (none was responding when last checked, 00 handoff — not
   re-tested since).
3. **Validated-set runtime surface in apogee** (ADR 0016 consequences):
   fingerprint-keyed storage (shipped + user-local `~/.apogee/`), match, per-session
   notice, config off-switch. Bigger design-and-build; wants a plan/grill first.
4. **Next-campaign design** — freed by ADR 0016: transfer test on a second model,
   superiority hunting for a Recommended tier, or nothing.
5. **Housekeeping (host-side):** re-running `campaign analyze` on the two qwen bundles
   will now stamp them `not-engaged` **and** carry the fixed report wording (the blocker
   the 01 handoff flagged is gone). The ledger entries stand as recorded and need no
   edit.

## Operational state at handoff

- **Linux container on the Mac host** (unchanged). `~/.apogee-sim/config.yaml` has
  `upstream.url: http://192.168.64.1:1111`; **no LLM server verified this session**
  (not re-tested). Bring one up before live work: llama-launcher MCP at
  `http://192.168.61.1:7331/mcp` per apogee-sim CLAUDE.md, or `manage-llm-server` from
  the host.
- **No campaign bundles in the container** (`~/.apogee-sim/campaigns/` absent); bundle
  work (qwen re-analyze) is host-side.
- **apogee:** `6e3ec9e` + the commit carrying this handoff. ⚠️ **Uncommitted `go.sum`
  drift** predating this session's work: additions of `/go.mod` hash lines only
  (`cloud.google.com/go/compute/metadata`, `bits-and-blooms/bitset`,
  `charmbracelet/harmonica`, …) — looks like a `go mod tidy`/build side effect, origin
  unknown (the 01 handoff recorded only docs changes). Left untouched; operator decides
  commit vs. `git checkout go.sum`.
- **apogee-sim:** `main` at `5f890b9`, working tree clean, **4 commits ahead of origin**
  (not pushed). Binary at repo root is still the Jul 8 **Mac** build — do not overwrite
  it from this Linux container (`go build ./...` is safe; `make build` is not).

## Explicitly NOT next (carried forward unchanged)

- Any apogee default flip / mechanism deletion from current evidence (ADR 0016 defines
  the separate per-model curation step, not global changes).
- A Confirmation campaign (still no convicted set).
- Iterated greedy elimination; family-swap arms, off-ramp SPI, depth-1 relaxation,
  mid-Exchange auto-compaction, apogee↔apogee-sim imports (all parked, see archived
  07-14 handoffs).
- The review's dropped `(uncertain)` off-manifest-arm observation (recorded in the
  archived plan's "After this plan"; revisit only if `AppendRun` gains callers outside
  the scheduler).

## Suggested skills

- **`pr-lifecycle`** — the pushes (next move 1).
- **`grill-with-docs`** — the Validated-set runtime surface or next-campaign design
  (next moves 3–4); expect ADR 0016's auto-enable semantics to be the center.
- **`manage-llm-server`** — bring a model up before live work (host-side).
- **`coding-standards`** — rides along with the qwen fix or any build work.
- **`handoff`** — at session end, superseding this doc.

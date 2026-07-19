# Handoff — engagement guard committed (`1153ef0`) with a 6-agent review's fix plan committed beside it (`71f41a1`); next: implement the plan, then qwen fix / Validated-set runtime surface / next-campaign design

**Date:** 2026-07-19 (supersedes `archived/2026-07-19 - 00 - adr-0016-landed-engagement-guard-built-uncommitted-in-apogee-sim.md` — its next move 1 is done: the guard was reviewed and committed; the review's findings became a plan instead of inline fixes). Work from this directory; apogee-sim is the sibling repo at `../apogee-sim`.

## Where things stand

- **ADR 0016 is accepted** (apogee `141dcb0`, unchanged since the 00 handoff): curation =
  per-model **Validated sets** keyed by the confidence-tagged fingerprint;
  non-inferiority is the bar; auto-enable at ≥ medium confidence with notice +
  off-switch; gemma-4-e4b-it-qat holds the first set (the pruned 16). See
  `docs/adr/0016-curation-is-per-model-validated-sets-keyed-by-fingerprint.md`.
- **The engagement guard is COMMITTED** (apogee-sim `1153ef0`, this session): a fully
  non-engaged arm now voids a complete campaign's verdict (`not-engaged`, both
  Protocols) instead of reading no-evidence. Full record: the top entry in apogee-sim
  `CHANGELOG.md`; canonical term: apogee-sim `CONTEXT.md` "Engagement".
- **A 6-agent `/code-review` of the guard ran before commit.** Seven findings survived
  (security + concurrency clean): two confirmed report-wording defects — the voided
  Screen renders the *powered* "underpowered for diffuse harm" no-conviction line
  instead of no-evidence (the wording is keyed on the overridable verdict string;
  `renderGate` is immune, the asymmetry is the defect), and the `## Engagement` section
  claims "the verdict is flagged `not-engaged`" on partial campaigns where D10 correctly
  withholds the flag — plus three test gaps and two doc-truth drifts. The guard's
  *semantics* (predicate, status set, void rules, D10 precedence, determinism, purity)
  all reviewed correct; the defects are report text only.
- **The fix plan is committed** (apogee-sim `71f41a1`):
  `../apogee-sim/docs/plans/engagement-guard-review-fixes-plan.md` — 6 work items in the
  house plan style (precedence, conventions, per-item files + acceptance), written to be
  executed with the **implement-plan** skill. The operator chose to commit the guard
  *ahead of* the fixes; the plan's "After this plan" section records that decision and
  the follow-up-commit shape (`fix(campaign): …`).

## Next moves (operator picks; none is pre-committed)

1. **Implement the review-fix plan** with the implement-plan skill (apogee-sim, items
   1–6 in order: two report.go wording fixes, three test-pin items, one doc-truth item).
   Everything is hermetic — no LLM server needed. Then one `fix(campaign)` commit.
2. **Push both repos** — nothing was pushed this session: apogee-sim is 2 ahead of
   origin (`1153ef0`, `71f41a1`, origin at `777af3f`); apogee will be 1 ahead after the
   handoff commit. `pr-lifecycle` covers the flow if PRs are wanted.
3. **qwen tool-call protocol fix** — the open other half of post-mortem consequence (c):
   chat-template/parser investigation of the fenced-JSON pseudo-tool-calls (see the qwen
   entry's 2026-07-08 amendment in `docs/design/mechanism-catalogue.md`). Live testing
   needs an LLM server (none was responding this session — see below).
4. **Validated-set runtime surface in apogee** (ADR 0016 consequences):
   fingerprint-keyed storage (shipped + user-local `~/.apogee/`), match, per-session
   notice, config off-switch. Bigger design-and-build; wants a plan/grill first.
5. **Next-campaign design** — freed by ADR 0016: transfer test on a second model,
   superiority hunting for a Recommended tier, or nothing.
6. **Housekeeping (host-side):** re-running `campaign analyze` on the two qwen bundles
   will now stamp them `not-engaged`; the ledger entries stand as recorded and need no
   edit. (Consider doing this *after* next move 1 so the refreshed reports carry the
   fixed wording.)

## Operational state at handoff

- **Linux container on the Mac host** (unchanged from the 00 handoff).
  `~/.apogee-sim/config.yaml` has `upstream.url: http://192.168.64.1:1111`; **no LLM
  server responded** at that address or `127.0.0.1:1111` when last checked (00 handoff —
  not re-tested this session). Bring one up before live work: llama-launcher MCP at
  `http://192.168.61.1:7331/mcp` per apogee-sim CLAUDE.md, or `manage-llm-server` from
  the host.
- **No campaign bundles in the container** (`~/.apogee-sim/campaigns/` absent); bundle
  work (qwen re-analyze) is host-side.
- **apogee:** `141dcb0` + the handoff commit that carries this file; only docs/handoffs
  changed this session.
- **apogee-sim:** `main` at `71f41a1`, working tree clean, **2 commits ahead of origin**
  (not pushed). Binary at repo root is still the Jul 8 **Mac** build — do not overwrite
  it from this Linux container (`go build ./...` is safe; `make build` is not).
- `go build ./...` + full `go test ./...` were green at commit time (the committed
  defects are report-wording only; no test currently fails on them — that is finding
  territory the plan's test items close).

## Review findings NOT in the plan (dispositioned)

- The `(uncertain)` off-manifest-arm observation (an arm present in a hand-edited
  `runs.jsonl` but not the manifest is counted in `Secondary` yet invisible to the
  guard) was judged unreachable via the tool's own writers and dropped; revisit only if
  `AppendRun` gains callers outside the scheduler. Recorded in the plan's "After this
  plan" section.

## Explicitly NOT next (carried forward unchanged)

- Any apogee default flip / mechanism deletion from current evidence (ADR 0016 defines
  the separate per-model curation step, not global changes).
- A Confirmation campaign (still no convicted set).
- Iterated greedy elimination; family-swap arms, off-ramp SPI, depth-1 relaxation,
  mid-Exchange auto-compaction, apogee↔apogee-sim imports (all parked, see archived
  07-14 handoffs).

## Suggested skills

- **implement-plan** — next move 1, executing
  `../apogee-sim/docs/plans/engagement-guard-review-fixes-plan.md` (the plan was written
  for it).
- **`coding-standards`** — rides along with any plan implementation or the qwen fix.
- **`pr-lifecycle`** — the pushes (next move 2).
- **`grill-with-docs`** — the Validated-set runtime surface or next-campaign design
  (next moves 4–5); expect ADR 0016's auto-enable semantics to be the center.
- **`manage-llm-server`** — bring a model up before live work (host-side).
- **`handoff`** — at session end, superseding this doc.

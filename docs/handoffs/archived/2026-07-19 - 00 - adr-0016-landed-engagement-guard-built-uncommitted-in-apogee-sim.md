# Handoff — ADR 0016 landed (curation designed, grill resolved); analyzer **engagement guard built and green but UNCOMMITTED** in apogee-sim; next: commit/review it, then qwen fix / Validated-set runtime surface / next-campaign design

**Date:** 2026-07-19 (supersedes `archived/2026-07-18 - 00 - probe-non-inferior-l9-committed-next-move-is-an-open-design-decision.md` — all three of its next moves are resolved: the grill produced ADR 0016, the rig work's first half shipped this session, the pushes were done before the session). Work from this directory; apogee-sim is the sibling repo at `../apogee-sim`.

## Where things stand

- **ADR 0016 is accepted** (apogee commit `141dcb0`, operator-authored 2026-07-19 morning —
  the 07-18 handoff's "grill the next campaign design" decision is resolved). Read
  `docs/adr/0016-curation-is-per-model-validated-sets-keyed-by-fingerprint.md` for the
  decision; in one line: curation = per-model **Validated sets** keyed by the
  confidence-tagged fingerprint, non-inferiority is the bar, auto-enable at ≥ medium
  confidence with notice + off-switch, and gemma-4-e4b-it-qat retroactively received the
  first set (the pruned 16). The catalogue gained a "Validated sets" curation section
  (separate from the L9 evidence ledger); CONTEXT.md gained Validated set + Curation.
- **The engagement guard is DONE but UNCOMMITTED** (this session, apogee-sim working
  tree): the analyzer half of the qwen post-mortem consequence (c), promoted to product
  prerequisite by ADR 0016 §4 / consequences. Full record: the new top entry in
  apogee-sim `CHANGELOG.md`. In one line: per-arm turns / tool-execs / zero-exec / wall
  in the secondary table; campaign-level status `verified` / `partial` / `not-engaged` /
  `unmeasured`; a fully non-engaged arm **voids a complete campaign's verdict** (tag
  `not-engaged`, both Protocols) instead of it reading no-evidence; report renders
  `## Engagement` before the grades (pre-registered read order). No record-schema change,
  no CLI change. `go build ./...` + full `go test ./...` green.
- **Modified files awaiting commit** (apogee-sim, on `main`, otherwise in sync with
  origin at `777af3f`): `internal/campaign/analyze.go`, `report.go`, `analyze_test.go`;
  docs `CHANGELOG.md`, `CLAUDE.md`, `CONTEXT.md` (new "Engagement" term); plus
  `go.mod`/`go.sum` — an **incidental module-graph sync** (sibling apogee now requires
  `golang.org/x/sys` via the `replace` directive; any build would have made it — keep it).

## Next moves (operator picks; none is pre-committed)

1. **Commit the engagement guard** (optionally `/code-review` first). One commit;
   suggested subject: `feat(campaign): analyzer engagement guard - not-engaged voids the
   verdict`. Then push.
2. **qwen tool-call protocol fix** — the open other half of consequence (c):
   chat-template/parser investigation of the fenced-JSON pseudo-tool-calls (64/70
   candidate, 48/70 bypass responses parsed as text; see the qwen entry's 2026-07-08
   amendment in `docs/design/mechanism-catalogue.md`). Live testing needs an LLM server
   (currently none responding — see below).
3. **Validated-set runtime surface in apogee** (ADR 0016 consequences): fingerprint-keyed
   storage (shipped + user-local `~/.apogee/`), match, per-session notice, config
   off-switch. Bigger design-and-build; wants a plan/grill first. Until it lands,
   Validated sets live as catalogue records + a config recipe.
4. **Next-campaign design** — freed by ADR 0016 from "confirm the TH claim": transfer
   test on a second model, superiority hunting for a Recommended tier, or nothing.
5. **Housekeeping (host-side):** re-running `campaign analyze` on the two qwen bundles
   will now stamp them `not-engaged`; the ledger entries stand as recorded and need no
   edit.

## Operational state at handoff

- **Environment changed vs the 07-18 handoff: this is now a Linux container** on the Mac
  host. `~/.apogee-sim/config.yaml` exists with `upstream.url: http://192.168.64.1:1111`
  (container→host route; auto-detect note says gpt-oss-20b was last seen). **No LLM
  server responded** at `192.168.64.1:1111` or `127.0.0.1:1111` this session — bring one
  up (llama-launcher MCP at `http://192.168.61.1:7331/mcp` per apogee-sim CLAUDE.md, or
  `manage-llm-server` from the host) before any live work.
- **No campaign bundles in the container** (`~/.apogee-sim/campaigns/` absent): the
  presence-gated real-bundle pin test skips here; bundle work (qwen re-analyze, ETA
  derivation from `wall_millis`) is host-side.
- **apogee:** clean at `141dcb0`, in sync with origin. (The 07-18 handoff's untracked
  `docs/kill-and-resume.md` is no longer present.)
- **apogee-sim:** `main` at `777af3f`, in sync with origin, **8 modified files
  uncommitted** (list above). Binary at repo root is still the Jul 8 **Mac** build — do
  not overwrite it from this Linux container (`go build ./...` is safe; `make build` is
  not).

## Design decisions made this session (rationale in the CHANGELOG entry)

- Engagement predicate = scored run with ≥ 1 tool execution (`len(ToolRuns) > 0`) —
  exactly the Probe's hand-check bar; infra_failed runs excluded (consistent denominator).
- **Any** fully non-engaged arm voids the whole complete-campaign verdict (conservative:
  a dead Bypass arm makes an engaged candidate spuriously pass; a dead without-X arm
  voids its attribution — the flag forces the operator to read the details, which the
  report names via `non_engaged_arms`).
- Partial campaigns keep `partial: N incomplete` (D10 invariant); the engagement block
  still reports mid-campaign. Partial engagement warns but never voids.
- Gate/Screen are still computed and rendered under the flag, marked "Computed but
  **void**" — transparency over suppression.
- Test fixtures: `gradedRecord` now carries one tool exec so existing tests exercise
  protocol verdicts, not the guard; `zeroExecRecord` is the explicit non-engaged builder.

## Explicitly NOT next (carried forward)

- Any apogee default flip / mechanism deletion from current evidence — unchanged; ADR
  0016 defines the *separate* per-model curation step, not global changes.
- A Confirmation campaign (still no convicted set).
- Iterated greedy elimination; family-swap arms, off-ramp SPI, depth-1 relaxation,
  mid-Exchange auto-compaction, apogee↔apogee-sim imports (all parked, see archived
  07-14 handoffs).

## Suggested skills

- **`/code-review`** + **`pr-lifecycle`** — the uncommitted engagement guard (next
  move 1).
- **`coding-standards`** — qwen fix or any further rig work (next move 2).
- **`grill-with-docs`** — the Validated-set runtime surface or next-campaign design
  (next moves 3–4); expect ADR 0016's auto-enable semantics to be the center.
- **`manage-llm-server`** — bring a model up before live work (host-side).
- **`handoff`** — at session end, superseding this doc.

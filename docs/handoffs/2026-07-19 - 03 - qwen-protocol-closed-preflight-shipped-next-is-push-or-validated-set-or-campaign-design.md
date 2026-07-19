# Handoff — qwen tool-call protocol closed (consequence (c) done, both halves); tool-call preflight shipped (`d61bf7f`); next: push, Validated-set runtime surface, or next-campaign design

**Date:** 2026-07-19 (supersedes `archived/2026-07-19 - 02 - review-fixes-implemented-and-recorded-next-is-push-or-next-campaign-design.md` — its next move 2, the qwen fix, is done; its moves 3–5 carry forward). Work from this directory; apogee-sim is the sibling repo at `../apogee-sim`.

## Where things stand

- **The qwen 20260707 post-mortem consequence (c) is fully closed** — both halves. The
  engagement guard (detection side) shipped last sessions (`1153ef0`, `bd4bb81`); this
  session ran the chat-template/parser investigation **live** and shipped the prevention
  side: apogee-sim `campaign run` now sends a **tool-call preflight** (one canary tools
  request right after `Fingerprint`, creation and resume; no structured `tool_calls` ⇒
  the campaign is refused before any disk state exists). Feature: apogee-sim `d61bf7f`;
  full investigation record: top entry of apogee-sim `CHANGELOG.md` (`77381a8`);
  catalogue amendment closing (c): apogee `ca6fa06` (the 2026-07-19 amendment in the
  qwen entry of `docs/design/mechanism-catalogue.md`). *Tool-call preflight* is now a
  CONTEXT.md term (apogee-sim) — prevention-side counterpart of the engagement guard.
- **Root cause, one line:** the rig is native-profile only (`coreagent` plumbs no
  `ModelProfile`); the failure was the server's parse-back half — qwen25-coder-14b
  emitted fenced JSON instead of its template's native call syntax and the calls passed
  through as visible text. The broken combo is **retired** (its llama-launcher profile
  no longer exists); template-lacked-tools vs parser-missed-emission is no longer
  distinguishable container-side (host-side option: the 20260707 bundle manifest's
  fingerprint, if it recorded `chat_template_caps`).
- **Verified live on the current stack** (llama.cpp `b10068` + qwen3.6-27B-Q4_K_S):
  structured `tool_calls` come back, and the full-loop live test
  `TestRun_FileEditTaskAgainstLiveModel` passes from THIS container (exchange-complete,
  2 turns, `write_file` executed) — the container routes to the server, despite the
  live test's own comment assuming it can't. A future qwen campaign targets
  **qwen3.6-27B** (verified), not the retired 20260707 model.
- Gates green throughout: `gofmt` clean, `go build ./...`, full `go test ./...`.

## Next moves (operator picks; none is pre-committed)

1. **Push both repos** — apogee-sim is 3 ahead (`d61bf7f`, `77381a8`, `b99ff96`);
   apogee is 1 ahead (only the commit carrying this handoff — `ca6fa06` was already
   pushed by the operator's host-side session, see below). `pr-lifecycle` covers the
   flow if PRs are wanted.
2. **Validated-set runtime surface in apogee** (ADR 0016 consequences):
   fingerprint-keyed storage (shipped + user-local `~/.apogee/`), match, per-session
   notice, config off-switch. Bigger design-and-build; wants a plan/grill first.
3. **Next-campaign design** — freed by ADR 0016: transfer test on a second model
   (qwen3.6-27B is now protocol-verified and loaded — the natural candidate),
   superiority hunting for a Recommended tier, or nothing.
4. **Housekeeping (host-side):** re-run `campaign analyze` on the two qwen bundles
   (stamps them `not-engaged` with the fixed wording; ledger entries need no edit).
   Optional: read the 20260707 bundle manifest's fingerprint to settle
   template-vs-parser for the retired combo — curiosity only, nothing depends on it.

## Operational state at handoff

- **Linux container on the Mac host.** `~/.apogee-sim/config.yaml` has
  `upstream.url: http://192.168.64.1:1111` — **server verified up this session** with
  `qwen3.6-27B-Q4_K_S` loaded (llama.cpp `b10068`, single slot — keep requests
  sequential). llama-launcher MCP control endpoint: **`http://192.168.64.1:7331/mcp`**
  (apogee-sim CLAUDE.md said `192.168.61.1` — fixed this session; profile roster is
  three gemma variants + Qwen3.6-27B; `qwen25-coder-14b` is gone).
- **No campaign bundles in the container** (`~/.apogee-sim/campaigns/` absent); bundle
  work (move 4) is host-side.
- **apogee:** the commit carrying this handoff on top of `c6757b2`, 1 ahead of origin.
  **An operator session ran host-side concurrently** through the shared mount: it
  committed `c6757b2` ("moved reviews to own folder" — the two
  `docs/architecture-review-*.html` files now live in `docs/reviews/`) on top of this
  session's `ca6fa06` and pushed origin to `c6757b2`. The transient working-tree
  deletions this agent observed mid-session were that move in progress; nothing is
  pending from it. Expect possible further host-side commits when reconciling.
- **apogee-sim:** `main` ahead of origin (see move 1), working tree clean apart from
  nothing pending. Binary at repo root is still the Jul 8 **Mac** build — do not
  overwrite it from this Linux container (`go build ./...` is safe; `make build` is
  not).

## Explicitly NOT next (carried forward unchanged)

- Any apogee default flip / mechanism deletion from current evidence (ADR 0016 defines
  the separate per-model curation step, not global changes).
- A Confirmation campaign (still no convicted set).
- Iterated greedy elimination; family-swap arms, off-ramp SPI, depth-1 relaxation,
  mid-Exchange auto-compaction, apogee↔apogee-sim imports (all parked, see archived
  07-14 handoffs).
- Plumbing `ModelProfile` through `coreagent.RunConfig` (a text-format-model campaign
  is out of scope until a model demands it; the preflight makes the gap loud instead
  of silent).
- The review's dropped `(uncertain)` off-manifest-arm observation (revisit only if
  `AppendRun` gains callers outside the scheduler).

## Suggested skills

- **`pr-lifecycle`** — the pushes (next move 1).
- **`grill-with-docs`** — the Validated-set runtime surface or next-campaign design
  (next moves 2–3); expect ADR 0016's auto-enable semantics to be the center.
- **`manage-llm-server`** — inspect/switch models; MCP endpoint above is verified.
- **`coding-standards`** — rides along with any build work.
- **`handoff`** — at session end, superseding this doc.

# Handoff — Validated-set runtime surface designed, built, shipped, and pushed; next: next-campaign design, host-side housekeeping, or surface follow-ups

**Date:** 2026-07-19 (supersedes `archived/2026-07-19 - 03 - qwen-protocol-closed-preflight-shipped-next-is-push-or-validated-set-or-campaign-design.md` — its move 1, the pushes, turned out already done host-side; its move 2, the Validated-set runtime surface, is done this session; moves 3–4 carry forward). Work from this directory; apogee-sim is the sibling repo at `../apogee-sim` (untouched this session).

## Where things stand

- **The Validated-set runtime surface (ADR 0016's first consequence) is designed, built,
  tested, committed, and pushed.** Two commits on apogee `main`: `c66984f` (design docs)
  and `48485f9` (the feature). Gates green throughout: `gofmt` clean, `go build ./...`,
  full `go test ./...`.
- **The design was fixed by a grill session**, recorded where it belongs — do not
  re-derive it:
  - ADR 0016 gained a dated **"Runtime-surface realisation (2026-07-19)"** section (the
    ADR 0015 amendment precedent) with the four authorized refinements: offer-below-medium
    (as written §5 could never fire — the resolver only produces low and hash-labelled
    high, and the entry keys are names), the `validated-sets: alias:` map as §3's explicit
    carry-over surface consulted at any confidence, whole-set-or-nothing (explicit
    `mechanisms:` block / Bypass wins; defects soft-skip; only a dangling alias is loud),
    and wire-time placement (engine and bench untouched; ADR 0015's single enable path
    stands).
  - CONTEXT.md's **Validated set** entry carries the glossary-level application semantics.
  - `docs/plans/validated-set-runtime-surface.md` is the implementation plan (schema,
    match ladder, notice wording, drift handling) — it shipped the same session, so it is
    a record now, not a to-do.
- **Shape of the code:** new `internal/validated` package (imports `internal/domain`
  only; descriptors passed in) + `cmd/apogee/validatedsets.go` (`resolveValidatedSet` +
  pure notice builders) + the `validated-sets:` config block threaded through
  `config.go`/`root.go`, folded into `Config.EnableMechanisms` in `wire.go` after the
  `mechanisms:` resolution. Shipped entries: `internal/validated/shipped.json` — the
  machine copy of the catalogue's Validated-sets table, pinned by
  `internal/validated/shipped_test.go` (a row added to one must be added to the other).
  First entry: the gemma-4-e4b-it-qat pruned 16.
- **A load-bearing fact discovered en route:** with no `model:` configured, apogee
  discovers the *served* model id from the Upstream (`resolveModel`, root.go), so the
  low-confidence fingerprint label lives in the same name-space as the bench fingerprint
  and the entry keys — the offer notice fires for real llama.cpp users out of the box.
  The surface is not dormant.

## Next moves (operator picks; none is pre-committed)

1. **Next-campaign design** (carried from 03): transfer test on a second model
   (qwen3.6-27B was protocol-verified and loaded last session — the natural candidate),
   superiority hunting for a Recommended tier, or nothing (rig work first). Wants a
   grill; ADR 0016's consequences frame the candidate purposes.
2. **Housekeeping (host-side, carried from 03):** re-run `campaign analyze` on the two
   qwen bundles (stamps them `not-engaged` with the fixed wording). Optional curiosity:
   read the 20260707 bundle manifest's fingerprint to settle template-vs-parser for the
   retired combo — nothing depends on it.
3. **Surface follow-ups (all deliberately deferred, none urgent):** a TUI in-transcript
   banner if the pre-TUI stderr notice proves easy to miss; the behavioral-probe
   (medium-confidence) resolver (D8 — would make direct auto-apply real); user-run
   validation tooling that writes `~/.apogee/validated/` entries (the drop-in one-file
   write model was designed for it). Each is its own design conversation.

## Operational state at handoff

- **Linux container on the Mac host.** Both repos clean and level with origin: apogee
  `main` = `48485f9` (handoff commit on top), apogee-sim `main` = `b99ff96` — pushed.
- **Upstream/server state was NOT re-verified this session** (no live-model work).
  Last session's facts: `~/.apogee-sim/config.yaml` → `http://192.168.64.1:1111`,
  qwen3.6-27B-Q4_K_S on llama.cpp `b10068`, single slot; llama-launcher MCP endpoint
  `http://192.168.64.1:7331/mcp`. Re-verify before relying on any of it.
- **apogee-sim binary at its repo root is still the Jul 8 Mac build** — `go build ./...`
  is safe from this container, `make build` is not.
- An operator session has previously run host-side concurrently through the shared
  mount; expect possible further host-side commits when reconciling.

## Explicitly NOT next (carried forward unchanged, plus one new)

- Any apogee default flip / mechanism deletion from current evidence (ADR 0016's
  per-model curation is the separate step; global changes need cross-model evidence).
- A Confirmation campaign (still no convicted set).
- Iterated greedy elimination; family-swap arms, off-ramp SPI, depth-1 relaxation,
  mid-Exchange auto-compaction, apogee↔apogee-sim imports (all parked, see archived
  07-14 handoffs).
- Plumbing `ModelProfile` through `coreagent.RunConfig` (the tool-call preflight makes
  the gap loud; a text-format-model campaign is out of scope until a model demands it).
- **New:** engine-level auto-enable or a public embedder API for Validated sets —
  wire-time-only was a deliberate grill decision; revisit only when a second consumer
  asks.

## Suggested skills

- **`grill-with-docs`** — next-campaign design (move 1); expect the ADR 0016
  consequence list ("transfer, superiority, or nothing") to frame it.
- **`manage-llm-server`** — inspect/switch models before any live-model work; re-verify
  the endpoint facts above.
- **`coding-standards`** — rides along with any build work.
- **`pr-lifecycle`** — only if the operator wants PRs instead of direct pushes.
- **`handoff`** — at session end, superseding this doc.

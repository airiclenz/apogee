---
title: ADR 0018 consequences
---

- A mid-Exchange overflow is survivable: the history folds, the same Turn re-sends, and the task
  continues from the summary. What the user loses is verbatim history, not the task.
- **`tool_result_cap` is no longer "the only reducer able to act mid-Exchange"** — the claim in
  `CONTEXT.md` is corrected, and the same premise sentence in
  ADR 0014 §4 is
  amended by this decision. ADR 0014's *decision* stands unchanged: `guided_decomposition` still
  `Requires` `tool_result_cap`, because the emergency fold is reactive, lossy, and capped at one
  per Turn — a rescue, not a way to keep accumulation down.
- The reducer taxonomy grows a second automatic Compaction trigger (`internal/context/doc.go`,
  `CONTEXT.md`): estimate-driven at the Exchange boundary, overflow-driven mid-Exchange. The
  generative fold itself is reused unchanged — one `context.Compact`, two triggers.
- `auto-compact: false` now opts out of overflow recovery as well as boundary folding. That is
  stated in the shipped config template's `auto-compact` comment, because the key's blast radius
  grew.
- The public API is untouched — no new Event variant, no facade change, `internal/provider`
  unmodified (its classification was already correct). The whole change is behaviour-only: a minor
  bump at most (ADR 0001 §consequences).
- The clamp rewrites the result *in the conversation*, so the raw text of a pathological result
  never reaches history, a snapshot, or the rendered transcript — the accepted price of a floor
  that must hold for every later reducer; the marker tells the model to re-read the omitted range
  with `start_line`/`end_line`.
- The floor's shared rendering is **line-based** (20 head + 20 tail lines, the same shape and
  marker `tool_result_cap` renders), so it bounds LINES, not History characters: a result of ~40
  very long lines can still exceed the History allocation after clamping, and one the head/tail
  form cannot shrink at all is passed through whole rather than grown. That is the prescribed
  rendering discipline both reducers share, not a defect — the emergency fold and the predictive
  guard remain the backstop for what the clamp cannot bound.
- Recovery is quiet by construction, so operationally a fold shows up only as the context gauge
  dropping on the next `UsageEvent`. The behaviour is pinned by hermetic `internal/agent` tests
  (overflow→success, overflow→overflow, opted-out, tool-continuation, cancel-during-fold); a live
  proof against a real ~32k-window profile is the implementing plan's closing gate.

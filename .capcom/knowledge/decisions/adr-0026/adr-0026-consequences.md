---
title: ADR 0026 consequences
---

- **`apogee.Config` gains one additive field, `ContextFiles []string`** — no exported name changes
  (ADR 0010), so this is a **minor** bump. `internal/domain` gains `ContextFileNote` /
  `ContextFilesReport` (aliased from the root facade, since an embedder cannot name an `internal`
  type), `Agent` gains the read-only `ContextFilesReport()`, and the TUI's `Engine` interface grows
  the matching method. An embedder that sets nothing is byte-identical to before.
- **A fresh install picks up this repo's own `AGENTS.md`** the first time apogee is started in it,
  and says so. That is the intended behaviour and it is also the largest behavioural change in the
  feature: standing content the user did not type now reaches the model. It is bounded by being the
  user's *own workspace* — the same trust boundary as every file the model reads anyway — by riding a
  header that names its source, and by `enable: false`.
- **The Budget measures it.** The seeded message is part of `req.State()`, so the predictive overflow
  guard and the estimator account for the content — honest accounting, at the price of less room for
  history on a small window. On a ~32k window a large `AGENTS.md` is a real cost, which is what §8's
  warning exists to surface.
- **ADR 0023 §6's zero-byte anchor is narrowed**, from "no prompt" to "no prompt AND no context
  files". ADR 0023 is left as written with a dated note pointing here.
- **ADR 0023's marker-suppression residual widens.** `Request.AppendToSystem(marker, text)` is
  idempotent by `strings.Contains` over the first system message, so a **workspace** file that
  happens to contain a Mechanism's marker phrase now suppresses that directive too — the same
  accepted suppression, from one more source, with the same bounded blast radius (catalogued nudge
  Mechanisms, default-off, enabled per model on bench evidence). Still recorded as a residual in
  `ISSUES.md` rather than fixed here.
- **The shipped template documents the block, commented.** `system-prompt-text:` remains the ONE
  active key a fresh install gets (ADR 0023 §8), and `TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt`
  still holds — the defaults come from resolution, not from the file.
  *(Superseded 2026-08-31 by ADR 0064: the
  template ships `system-prompt-text:` COMMENTED and the default prompt is embedded in the binary,
  so a fresh install gets no active prompt key at all. The named test still holds, with its
  invariant rewritten to match; nothing else in this bullet changes.)*
- **CONTEXT.md gains one term — *Context files*** — worded to match this ADR and distinguished from
  the System prompt it rides with, from an `@file` reference, and from a Skill.

---
title: ADR 0023 considered options
---

- **Seeding through a construction-time PreRequest hook** — rejected. Hooks dispatch
  catalogued-first, so a Mechanism's `AppendToSystem` would create the system message *before* a
  seeding hook ran, putting mechanism directives **ahead** of the prompt; and the prompt is
  structural configuration, not a Mechanism — it must apply under `--bypass`
  (ADR 0006), which Mechanism plumbing does not
  guarantee.
- **Seeding at the wire seam (`toProviderRequest`)** — rejected. `AppendToSystem` would then create
  its *own* system message in the domain Request, and the wire seam would need a second merge pass to
  order the two; worse, pre-request hooks and the predictive overflow guard (`requestExceedsWindow`,
  through `req.State()`) would not see the prompt at all, so the Budget estimate would understate
  every request by the prompt's size.
- **The template language in `internal/domain`** — rejected. Domain is declarative data and
  loop-facing working values; scanning, substitution and a date format are a *processing* concern
  (ADR 0010). The `ModelProfile` precedent is exact: domain keeps the profile as **data** and
  delegates parsing outward.
- **The template language in `internal/processing`** — rejected. That package is the model profile's
  parse/emit seam; its vocabulary is tool-call formats and thinking channels. The system-prompt
  template is a host/config concern with different consumers and a different lifetime. One concern
  per package.
- **A rendered string on `Config` instead of a template** — rejected: the mode and the date are live,
  so a rendered string would be stale from the first mode switch, and every embedder would have to
  re-render it themselves on a schedule they cannot know.
- **A compiled-in default prompt** (a fallback used whenever the key is absent) — rejected: it would
  change behaviour for **every existing install** on upgrade, and hide the text where it cannot be
  read or edited. The shipped-file default of §8 gives new users the prompt and old users their
  current behaviour, with one visible source of truth.
- **A `--system-prompt` flag / `APOGEE_SYSTEM_PROMPT` env var** — rejected with the other newer keys:
  the precedence ladder exists for per-invocation switches, and a prompt is not one. A file path in
  the config is already the way to swap prompts.
- **Per-model entries that MERGE with the global prompt** (append, or fill in the missing spelling) —
  rejected: merge order and duplicate-persona questions have no obviously right answer, and
  whole-entry replacement is the rule a user can predict from the config file alone.
- **Automatic per-capability-tier prompt shortening** (ADR 0021's parked idea) — not built here. The
  `system-prompt-models:` map is the **manual** per-model lever, which is evidence-free and
  predictable; automatic tiering stays parked until measurement justifies it.

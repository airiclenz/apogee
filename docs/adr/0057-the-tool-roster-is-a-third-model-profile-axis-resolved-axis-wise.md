---
Status: accepted
---

# The tool roster is a third Model-profile axis, resolved axis-wise

## Context

The 2026-08-16 tool-surface poll round ratified the identity framing — apogee is built for
smaller local models while working even better with bigger ones — and promoted per-profile tool
rosters as the mechanism serving both classes: the default roster stays tuned for the models
that need the help, feedback from larger models lands in a profile tier instead of re-tuning the
default, and a new tool can ship default-off/profile-enabled instead of costing every small
model a tool-list slot (`docs/design/tool-surface-findings.md`). What existed was one global
`tools.disabled:` list applied to the default tool set, and the per-model
[Model profile](0044-model-profiles-are-per-model-and-mostly-shipped.md) with its two wire-shape
axes. A grill on 2026-08-23 settled the shape; this record is that ratification.

## Decision

**1 — The roster is a third axis on the Model profile.** A `model-profiles:` entry gains a
`tools:` axis; the roster reuses the profile's pattern match, its three-layer resolution and its
ride on Rebind rather than growing a parallel per-model map. The glossary term widens
accordingly: the Model profile describes how apogee *equips and speaks to* a model, not wire
shape alone (CONTEXT.md amended).

**2 — The axis is a pair of delta lists against the default roster:**
`tools: {disabled: [...], enabled: [...]}`. Deltas survive roster evolution (a full replacement
list would silently starve profiled models of every tool a later release adds) and give the axis
the enable direction the promoted use case needs. The vocabulary mirrors the global key's.

**3 — Tools gain a build-level default-off state, and the global config gains `tools.enabled:`.**
A tool registration can mark itself default-off: present in the build, absent from the menu
until a profile `enabled:` or the global `tools.enabled:` lifts it. The global counterpart
exists so "I want this tool everywhere" never forces a catch-all pattern entry. No current tool
ships default-off — the state ships empty, for the first tool that wants it.

**4 — Precedence is a specificity ladder: profile > global > build default.** The most specific
word wins, so a profile `enabled:` re-enables a globally disabled tool (the ratifying use case:
a tool off by default, on for the big model) and a profile `disabled:` turns off what global
allows. Mirrors ADR 0044's user-beats-shipped layering. A tool named in both lists of ONE scope
is a startup NOTICE and disabled wins — fail closed, never a refusal (the `tools.disabled`
unknown-name policy extends: an unknown name in any of these lists is a NOTICE).

**5 — Profile resolution becomes axis-wise, all three axes — this amends ADR 0044 decision 4.**
Each axis resolves independently through user ▸ shipped ▸ zero: an axis ABSENT from the nearest
matching entry defers to the next layer; an explicitly spelled zero (`thinking: {style: none}`,
`tool-call-format: native`, empty `tools:` lists) overrides. Whole-profile replacement had
become a trap the moment rosters joined the map: the *likely* user entry is a tools-only line
for a model whose wire shape the SHIPPED table carries (gpt-oss), and rule-4 replacement would
silently wipe the harmony parsing that table exists to provide. The 'inherit' spelling ADR 0044
deferred is thereby obsolete: absence IS the inherit spelling, and axis presence is a fact the
config layer reads from the YAML (key present vs absent) before handing the engine one resolved
`domain.ModelProfile` — the engine sees no layering, exactly as before.

**6 — The shipped shape table carries no tools axis for now.** Rosters are bench-evidence
territory ("nothing leaves the roster on poll evidence alone"); no shipped entry has per-family
bench evidence. The struct supports the axis everywhere — a future shipped roster needs its own
ratification, not new machinery.

**7 — The resolved roster is a per-model binding and rides Rebind.** `/model` to the big model
and its enabled tools appear; switch back and they are gone. Joins the profile, the Validated
set and the system prompt in `RebindSpec`; switches commit at the ADR 0024 boundary, so the set
never changes mid-Exchange. RebindSpec's "tools … are session state" exclusion clause is
rewritten: the ROSTER is per-model; mode, approvals, confinement and the conversation remain
session state.

**8 — A switch whose roster deltas are non-empty announces a one-liner**
(`tools: +web_search −single_find_and_replace (profile)`); an entry with no tools axis stays
silent. The precedent is ADR 0044 decision 7: announce what changes observable behavior — a
silently vanished tool is otherwise a mystery with no trail.

## Bounds (stated, not separately ratified)

- Deltas apply to the DEFAULT tool set only; an embedder-injected `Config.Tools` is the host's
  authority verbatim, exactly like `DisabledTools` today (ADR 0031's doors — the engine still
  never reads config; resolution runs in the composition root).
- Built-in tools only: MCP-served tools ride their own surface, untouched.
- The roster is plain config, not a Mechanism — no gating, no Bypass interaction.
- A sub-agent resolves its own model's profile, so a delegation running a different model
  (sub-agent-server work) gets its own roster for free.

## Considered and rejected

- **A separate `tool-rosters:` pattern map** — keeps the profile pure wire-shape at the cost of
  a second per-model map with identical matching; two places to key the same model.
- **Per-server rosters** — the roster describes a model, and the launcher swaps models under one
  endpoint.
- **Disabled-only axis** — cannot express default-off/profile-enabled, the promoted use case.
- **Full replacement roster** — churns against every release's roster evolution.
- **Global `tools.disabled` as a hard off** — kills the off-by-default/on-for-one-model
  pattern; the ladder keeps global as the default word, not the last word.
- **Keeping rule-4 whole-profile replacement** (or axis-wise for the tools axis alone) — the
  silent wire-shape wipe on the most common edit; a half-axis-wise map means two resolution
  rules side by side.

## Consequences

- ADR 0044 decision 4 is superseded by decision 5 above (reconciled in place with a dated
  pointer); its deferred 'inherit' spelling closes as obsolete.
- `RebindSpec`'s and `SetProfile`'s doc comments change again (the roster joins the per-model
  bindings); ADR 0024 §4's "what stands across a rebind" reading follows.
- CONTEXT.md's Model-profile entry widens to three axes and axis-wise resolution.
- The bench arms in `docs/design/tool-surface-findings.md` gain their landing zone: an arm that
  concludes "remove X for the small class" ships as a default-roster or profile change instead
  of a global removal.

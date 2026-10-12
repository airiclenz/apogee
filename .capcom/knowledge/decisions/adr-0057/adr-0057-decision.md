---
title: ADR 0057 decision
---

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
ships default-off — the state ships empty, for the first tool that wants it. (Amended 2026-10-02:
six tools now ship default-off — `console_open`, `console_send`, `console_read` and
`console_close` (ADR 0059), `fan_out` (ADR 0087 D8) and `workflow` — each reporting it through
`domain.DefaultOffTool`.)

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
*(**Amended 2026-08-24 by ADR 0058:**
the thinking axis named here as one of the three itself resolves as two sub-axes — the channel
style, carrying its Start/End tokens, and the effort dial — each through user ▸ shipped ▸ zero on
its own. The rule stands; an `effort:`-only entry had re-opened the same trap one level down,
wiping a shipped style silently. The rest of this record stands.)*

**6 — The shipped shape table carries no tools axis for now.** Rosters are bench-evidence
territory ("nothing leaves the roster on poll evidence alone"); no shipped entry has per-family
bench evidence. The struct supports the axis everywhere — a future shipped roster needs its own
ratification, not new machinery.

*(**Amended 2026-08-25 by ADR 0059 §3:**
the first shipped tools axis is the Console family on the qwen3.8 entry; the rule stands — each
shipped roster is its own ratification.)*

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

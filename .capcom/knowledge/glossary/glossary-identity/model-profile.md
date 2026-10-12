---
title: "Model profile: how apogee equips and speaks to a given model, on three axes"
---

**Model profile**:
The per-model description Apogee carries of *how it equips and speaks to a given model* — three
**orthogonal** axes: its **tool-call format** (native structured `tool_calls`, or a text format
the model emits inline in its content — **markdown-fenced** or a **custom regex**), its
**thinking channel** style, and its **tool roster** (delta lists — `disabled`/`enabled` —
against the default tool set, so a tool can be off for the small class and on for a big model,
and a new tool can ship default-off/profile-enabled). Orthogonal because a model can emit native
tool calls *and* inline thinking (gpt-oss does both); the roster axis is capability tuning, not
wire shape. The profile drives **both directions at the seams**: on the
**parse** side it selects which parser and content-stripper the loop applies to incoming
content; on the **emit** side, for a non-native tool-call format, the engine tells the model
how to speak — rendering the tool menu and format-emission instructions as text into the
request and suppressing the native `tools` array (a non-native template would otherwise be
double-told, or choke on an array it cannot render). A **zero profile is the native,
no-inline-thinking default** (today's behaviour) — it adds nothing to the request in either
direction. It is a `domain` type on `Config` (declarative data — ADR 0010),
translated to the `processing` parsers at the boundary, not the parsers' own config.
Which profile a model gets is **resolved per model in three layers** — the user's
`model-profiles:` pattern map ▸ apogee's **shipped shape table** ▸ the zero profile — matched by
case-insensitive substring on the model name (longest pattern wins within a layer; any user
entry beats any shipped one) and resolved **axis-wise**: each axis takes the nearest layer that
spells it, an absent axis defers downward, an explicitly spelled zero overrides
(ADR 0057,
amending ADR 0044's whole-replacement rule). The thinking axis itself resolves in **two halves**
— the channel **style**, carrying its delimiter tokens, and the **effort** dial — each on its own
through the same three layers, so an entry that dials only `effort:` keeps the layer's channel
style (ADR 0058).
The shipped table carries a roster only where an ADR ratified that one — first the Console family
for Qwen3.8
(ADR 0059, amending
ADR 0057 decision 6) — and wire-shape axes everywhere else. A shipped match announces itself
with a one-line notice (`model profile: <pattern> (built-in) — thinking: <style>`); a user match
applies silently, except that a switch
whose roster deltas are non-empty announces them in one line. The resolution rides every model switch — the profile is
one of the per-model bindings Rebind applies at the boundary
(ADR 0024) —
and the retired *global* `model-profile:` block is refused at startup with the map spelling to
paste. See ADR 0044.
_Avoid_: "model config" (overloaded with sampling/endpoint knobs), "adapter", "format" alone
(there are two axes, not one), "global profile" (the single global block is retired — the profile
is per-model, and mostly shipped).

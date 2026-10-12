---
title: ADR 0044 decision
---

**The model profile is resolved per model, in three layers — the user's `model-profiles:` pattern
map ▸ apogee's shipped shape table ▸ the zero profile — and the resolved value rides every model
switch as one of Rebind's per-model bindings.** Concretely:

**1 — Apogee ships a shape table.** A small built-in set of known model shapes that auto-applies
by model name, so an out-of-the-box run against a known family speaks correctly with an empty
`config.yaml`. It launches as the verified trio only — gemma-family delimited `<think>` (the
ported oracle vectors), gpt-oss harmony (live runs), minimax-m3 delimited `<mm:think>` (the
2026-08-11 session) — and grows one entry per *sighting*, never per guess. An unmatched model
stays zero-profile pass-through, exactly today's behavior.

**2 — The match rule is a case-insensitive substring of the advertised/resolved model name, with
no confidence gate.** The name is what every Upstream reports and what quants, providers and
`:tag` suffixes all keep some spelling of (`minimax/minimax-m3:exacto`, `minimax-m3-Q4_K_M`), and
the tag shape is a chat-template/family fact that survives all of those. Deliberately NOT reused
from the Validated-set surface: its confidence-graded rule (auto-apply at ≥ medium, offer at low)
requires the explicit model probe battery (ADR 0021),
which never runs against a remote server — gating the table on it would leave the table dark in
precisely the out-of-box case that motivates it.

**3 — The global `model-profile:` block is retired.** A `config.yaml` that still spells it is
refused at startup with a LOUD error whose message shows the new spelling, echoing the user's own
block as the example to paste. No back-compat layer — apogee is pre-production, and a silently
tolerated old key is a second source of truth.

**4 — A matching user entry replaces the WHOLE profile, both axes.** The system-prompt-models rule
verbatim. No per-axis merge: an absent axis already *means* native/none, so merging would need an
'inherit' spelling to distinguish "leave it" from "turn it off".
*(**Superseded 2026-08-23 by ADR 0057:**
resolution is now axis-wise — an absent axis defers to the next layer, an explicitly spelled zero
overrides. The tools axis made whole-replacement a trap: a tools-only user entry would silently
wipe a shipped wire shape. The rest of this record stands.)*

**5 — User entries key by the same pattern rule; longest pattern wins within a tier, and any user
match beats any shipped match** — even a shorter user pattern against a longer shipped one. Equal
lengths break lexicographically, so resolution is stable and testable. The escape hatch for a
false-positive shipped match is therefore an ordinary user entry (`thinking: {style: none}`), not
a separate off-switch key.

**6 — Both engine doors stay, funnelling into one internal swap.** `RebindSpec` grows a `Profile`
field, so an observed model change applies the profile atomically with the system prompt, the
window and the Mechanism set at the ADR 0024 (ADR 0024)
boundary; `SetProfile` remains the same-model door a settings/config edit uses
(ADR 0037 — every settings edit
applies to the running session; ADR 0041 — the config file is
watched). Two doors, one unexported `applyProfile` behind them; the parser/stripper replacement
exists once.

**7 — A shipped match announces itself; a user match is silent.** One line at startup/switch when
the SHIPPED table fires (`model profile: <pattern> (built-in) — thinking: <style>`), because a
built-in that matches the wrong model is otherwise invisible and this line is the first debugging
clue. A user-map match says nothing — the user wrote it.

**8 — The engine still never reads config.** Resolution runs in the composition root (cmd/apogee),
which hands the engine a resolved `domain.ModelProfile` at startup, in the rebind spec, and on a
watched-config edit. The engine stays wire-silent about the host's configuration
(ADR 0031),
so a bench Driver drives the same doors directly.
*(**Amended 2026-09-15:** the two inputs a Driver needs to dial the product's agent are reachable
through the facade — `apogee.DefaultSystemPrompt()` forwards the embedded default template
(ADR 0064) and
`apogee.ShippedProfile(model)` resolves the shipped tier alone, reporting whether it matched. The
resolution proper — the user's `model-profiles:` tier over the shipped one, with its notices —
stays in cmd/apogee (`resolveModelBindings`, one function behind startup and the rebind); the facade
exports the product's two constants so a bench that cannot import internal/* builds a Config that
carries a non-empty prompt and a shipped shape rather than a promptless, zero-profile approximation
of the product, and this point is not weakened by it.)*

---
Status: accepted
---

# Model profiles are per-model, and mostly shipped

## Context

A live session on 2026-08-11 put a stray `</mm:think>` in front of minimax-m3's every visible
reply: the server's chat template opens the thinking span itself, so the content apogee receives
begins already *inside* the channel and carries only the closer. The stripper fix is mechanical
(an orphan closer closes an implicit span opened at position 0). What the incident exposed is the
surface around it.

The [Model profile](../../CONTEXT.md) — the two-axis description of *how a given model speaks the
wire*, tool-call format and thinking channel ([ADR 0010](0010-package-layout-domain-core-and-thin-root-facade.md):
declarative domain data, translated to the `processing` parsers at the seam) — is configured as
one **global** `model-profile:` block. Global is wrong twice over:

- **It churns on every switch.** The profile describes a *model*, and the session changes models —
  `/model`, a Launch-profile load ([ADR 0029](0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md)),
  a `/server` switch. A human who runs three models edits `config.yaml` on every switch, and a
  human who forgets leaves the previous model's stripper installed against the new one.
- **It is documented as deliberately excluded from the one mechanism that re-resolves per-model
  bindings.** `RebindSpec` (`internal/agent/rebind.go`) says so in as many words: the spec is
  "deliberately NOT the whole Config: mode, approvals, confinement, tools, the model profile
  (`model-profile` is global, not per-model — see Config.Profile; SetProfile is its separate,
  explicit door) and the conversation are session state that a model switch has no business
  resetting." `SetProfile`'s own doc repeats it — Rebind "leaves it alone when the Upstream's
  loaded model changes."

That stance was coherent while the profile meant "the human's chosen dialect". It is not coherent
once the profile means "this model's tag shape", which is what it has always actually meant: the
delimiters are a chat-template fact of a model family, not a preference.

And there is a second half. Even spelled per-model, a config key only helps a human who already
knows that minimax-m3 hides reasoning in `<mm:think>` — which they learn by seeing the leak. The
tag shape of a known family is a fact apogee can simply *carry*, the way it carries per-model
Validated sets ([ADR 0016](0016-curation-is-per-model-validated-sets-keyed-by-fingerprint.md)) and
per-model system-prompt templates ([ADR 0023](0023-the-system-prompt-is-a-configured-template-rendered-per-request.md)).

A grill on 2026-08-11 settled the shape; this record is that ratification.

## Decision

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
requires the explicit model probe battery ([ADR 0021](0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md)),
which never runs against a remote server — gating the table on it would leave the table dark in
precisely the out-of-box case that motivates it.

**3 — The global `model-profile:` block is retired.** A `config.yaml` that still spells it is
refused at startup with a LOUD error whose message shows the new spelling, echoing the user's own
block as the example to paste. No back-compat layer — apogee is pre-production, and a silently
tolerated old key is a second source of truth.

**4 — A matching user entry replaces the WHOLE profile, both axes.** The system-prompt-models rule
verbatim. No per-axis merge: an absent axis already *means* native/none, so merging would need an
'inherit' spelling to distinguish "leave it" from "turn it off".

**5 — User entries key by the same pattern rule; longest pattern wins within a tier, and any user
match beats any shipped match** — even a shorter user pattern against a longer shipped one. Equal
lengths break lexicographically, so resolution is stable and testable. The escape hatch for a
false-positive shipped match is therefore an ordinary user entry (`thinking: {style: none}`), not
a separate off-switch key.

**6 — Both engine doors stay, funnelling into one internal swap.** `RebindSpec` grows a `Profile`
field, so an observed model change applies the profile atomically with the system prompt, the
window and the Mechanism set at the ADR [0024](0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md)
boundary; `SetProfile` remains the same-model door a settings/config edit uses
([ADR 0037](0037-every-settings-edit-applies-to-the-running-session.md) — every settings edit
applies to the running session; [ADR 0041](0041-the-config-file-is-watched.md) — the config file is
watched). Two doors, one unexported `applyProfile` behind them; the parser/stripper replacement
exists once.

**7 — A shipped match announces itself; a user match is silent.** One line at startup/switch when
the SHIPPED table fires (`model profile: <pattern> (built-in) — thinking: <style>`), because a
built-in that matches the wrong model is otherwise invisible and this line is the first debugging
clue. A user-map match says nothing — the user wrote it.

**8 — The engine still never reads config.** Resolution runs in the composition root (cmd/apogee),
which hands the engine a resolved `domain.ModelProfile` at startup, in the rebind spec, and on a
watched-config edit. The engine stays wire-silent about the host's configuration
([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)),
so a bench Driver drives the same doors directly.

## Considered and rejected

- **Keep the global block, just document it better.** Does nothing about the churn, and nothing at
  all for the human who does not yet know the tag shape.
- **Gate the shipped table on fingerprint confidence** (the Validated-set rule): kills the
  out-of-box case — see decision 2.
- **Per-axis merge of a user entry over a shipped one**: needs an 'inherit' spelling; the same
  ambiguity replace-whole exists to avoid (decision 4).
- **A separate `disable-shipped-profiles:` off-switch**: a user entry already expresses it, and a
  second key would need its own precedence story.
- **A back-compat shim that reads the old key as a `*` pattern**: two spellings for one thing,
  forever, in a pre-production tool.
- **JSON/embedded-bundle machinery for the table** (the Validated-set bundle shape): three entries
  of declarative Go data need no loader.

## Deferred (dated 2026-08-11, not denied)

- **An 'inherit' spelling** enabling per-axis overlay of a user entry on a shipped one — wanted
  only once a shipped entry sets both axes and a user disagrees with one.
- ~~**Profiles in the seeded config template.** `model-profiles:` stays the one key the seeded
  template does not document, deliberately: the shipped table is meant to make it unnecessary.~~
  *(**Reversed 2026-08-12 on the owner's report** that the seeded template had not been updated.
  The shipped table removes the need to WRITE an entry, not the need to know the key exists: it is
  what a reader reaches for when a dialect is wrong or a built-in matched the wrong model, and it
  is the spelling the retirement error tells them to paste. The template now documents it as a
  commented example like every other key, and names the retired global key as retired. Nothing
  else in this record changes.)*

## Consequences

- **`RebindSpec`'s exclusion note is reversed and must be rewritten.** The clause "the model
  profile (`model-profile` is global, not per-model — see Config.Profile; SetProfile is its
  separate, explicit door)" no longer describes the system: the profile joins Model,
  SystemPrompt, MaxContextTokens and EnableMechanisms as a per-model binding. `SetProfile`'s
  "leaves it alone when the Upstream's loaded model changes" goes with it.
- **Two clauses of older records are superseded — clauses, not whole ADRs.**
  [ADR 0024](0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md) §4's
  "what stands across a rebind" list, where the model profile stands with "`model-profile` is
  global, not per-model", and
  [ADR 0028](0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md)'s
  Consequences note that the switchable profile abstraction "stays deliberately global". Both are
  struck in place and point here; everything else in those two records stands.
- **`SetProfile` narrows to one job:** applying a profile to the CURRENT model when the config
  changed under a stable model. It keeps its mid-Exchange refusal; Rebind stays atomic and
  idle-only.
- **A wrong profile is safe to auto-apply, which is why no confidence gate is owed.** It is
  visible (the notice names the pattern that fired), reversible (one user entry), and bounded to
  request/parse shaping — no bench-honesty claim rides on it, unlike a Validated set, where a
  wrongly applied Mechanism stack would contaminate an A/B arm (ADR 0016). The
  [ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md) floor is untouched: the profile is
  wire dialect, not a Mechanism, and it is on under Bypass exactly as it is today.
- **New package `internal/profiles`** (entries, pattern match, shipped set, notice posture),
  shaped like `internal/validated` and importing `internal/domain` only.
- **Config grows `model-profiles:` as a pattern→profile map** reusing the existing block schema
  unchanged, layered like `mechanisms:` (a present map in a nearer layer replaces the farther one
  whole), and loses the global key.
- **[ADR 0045](0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md)'s Delegation
  target consumes this**: the profile latched for a Sub-agent server is the one resolved for the
  model that server is OBSERVED to serve, through the same `Resolve`.

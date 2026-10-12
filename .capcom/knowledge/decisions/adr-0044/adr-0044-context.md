---
title: ADR 0044 context
---

A live session on 2026-08-11 put a stray `</mm:think>` in front of minimax-m3's every visible
reply: the server's chat template opens the thinking span itself, so the content apogee receives
begins already *inside* the channel and carries only the closer. The stripper fix is mechanical
(an orphan closer closes an implicit span opened at position 0). What the incident exposed is the
surface around it.

The Model profile — the two-axis description of *how a given model speaks the
wire*, tool-call format and thinking channel (ADR 0010:
declarative domain data, translated to the `processing` parsers at the seam) — is configured as
one **global** `model-profile:` block. Global is wrong twice over:

- **It churns on every switch.** The profile describes a *model*, and the session changes models —
  `/model`, a Launch-profile load (ADR 0029),
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
Validated sets (ADR 0016) and
per-model system-prompt templates (ADR 0023).

A grill on 2026-08-11 settled the shape; this record is that ratification.

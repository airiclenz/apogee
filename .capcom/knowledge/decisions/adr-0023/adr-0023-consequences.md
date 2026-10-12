---
title: ADR 0023 consequences
---

- **`apogee.Config` gains one additive field, `SystemPrompt`** — no exported name changes (ADR 0010),
  so this is a **minor** bump. An embedder that sets nothing is byte-identical to before, and one
  that sets a bad template fails `New`/`Resume` loudly rather than at request time.
- **The Budget now measures the prompt.** `req.State()` carries the seeded message, so the
  predictive overflow guard and the estimator's calibration account for it — honest accounting of
  what the request actually costs, at the price of slightly less room for history on a small window.
  Post-response scanners reading `req.View()` see a leading system message whenever a prompt is
  configured; that is the shape a seeding pre-request hook already produced.
- **A user prompt containing a Mechanism's marker phrase silently suppresses that directive.**
  `Request.AppendToSystem(marker, text)` is idempotent by `strings.Contains(first system message,
  marker)`, and the markers are natural-language phrases — `decompose`'s `"Focus on one action"`,
  `cot`'s `"have not read any files yet"`, `library`'s `"Apogee context notes"`. Until now the
  first system message was engine-written, so that check only ever saw the Mechanism's own earlier
  injection; now the user's prompt **is** that message, and a prompt that happens to contain the
  phrase reads as "already injected". This is a **suppression, not a corruption**: the request stays
  well-formed, and the user's own sentence on that subject is what the model reads instead. The
  blast radius is bounded to the catalogued nudge Mechanisms, which are default-off and enabled per
  model on bench evidence ([ADR 0015).
  It is **accepted, not fixed, here**: a non-textual idempotency channel changes the hook API's
  contract for every Mechanism, and the alternative — opaque sentinel strings — would put text in
  front of the model whose only reader is apogee. Recorded as a residual in `ISSUES.md`; revisit if a
  real prompt trips it.
- **ADR 0018 §1's
  aside is now this ADR's to own.** That decision noted "the shipped config template stays
  behaviour-neutral — nothing here is a new default to enable", which was true of *that* change and
  is still true of it; §8 above is what ends the template-wide neutrality it leaned on. ADR 0018 is
  left as written, with a dated note pointing here.
- **The host-override knob for the rendered instruction block stays additive-later, and composes.**
  This is the obligation the parked TODO note attached to the general template: because §6 merges
  prompt, directives and tool block into **one** system message, a future host-supplied block
  replaces the rendered one *inside the same message* without reshaping anything decided here.
- **CONTEXT.md gains one term — *System prompt*** — worded to match this ADR, and distinguished from
  the summariser's and the battery's dedicated prompts.
- **`{{datetime}}` is a per-day cache boundary.** A session running across midnight re-renders the
  first system message and the server's prefix cache misses once. That is the accepted cost of a
  correct date, and the reason the placeholder is not a timestamp.

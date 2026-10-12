---
title: ADR 0064 consequences
---

- **ADR 0023 §8 is superseded in part and gains a pointer note.** Its "no compiled-in fallback"
  sentence and its "A compiled-in default prompt" rejected alternative no longer hold; the rest of
  §8 — that the shipped template sets exactly one key, and that every other key parses to nothing —
  is unaffected, and the key it sets is now commented out (§4).
- **A stock run now seeds a system message.** ADR 0023 §6's byte-identical native anchor is reached
  only with `use-default-prompt: false` and no configured prompt or context files; the anchor test
  states that condition rather than assuming it. This is the one behaviour change an upgrade brings
  to a user who configured nothing, and `use-default-prompt` is its opt-out.
- **The default text is now apogee's to get right, continuously.** Because it ships with the binary,
  a bad sentence reaches every user on the next upgrade — the same exposure the orientation block
  carries, and the reason §6 makes prompt quality a floor concern rather than a taste question.
- **ADR 0023's marker-phrase suppression residual becomes checkable.** A user prompt containing a
  Mechanism's marker silently suppresses that directive; the *default* prompt is text apogee writes,
  so it can be kept clear of the catalogued markers by inspection instead of hope.
- **The Budget measures the default like any other prompt** (ADR 0023 Consequences): a stock small
  window now spends the default's tokens on every request, which is the cost §5's "steering, not
  facts" boundary exists to keep bounded.
- **CONTEXT.md's *System prompt* entry gains the embedded default and the ladder**, and the
  placement rule of §5 is the answer to "where does this sentence go?" for every later wave.

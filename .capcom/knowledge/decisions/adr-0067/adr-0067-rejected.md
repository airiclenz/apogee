---
title: ADR 0067 considered options
---

- **Make the existing keys additive by default** — so `system-prompt-text` is appended to the
  embedded default unless the user opts out. Rejected on two counts. It stacks **two personas**:
  the shipped default opens by saying who the assistant is, and a user's own prompt usually opens
  the same way, so the model reads two answers to one question — the duplicate-persona objection
  ADR 0023 already used to reject merging per-model entries, and ADR 0064 re-used to reject
  appending the default beneath an explicit prompt. And it **moves the floor**: by ADR 0064 §6 the
  default prompt is config-tier and part of the Bypass floor in both bench arms, so making it ride
  along under every configured prompt changes what every existing config sends, silently, on
  upgrade — the same "changes nothing for anyone already running" property ADR 0023 §8 protected
  and ADR 0064 §4 preserved as a silent keep. An additive channel has to be one the user **turned
  on**.
- **Let layers stack behind the embedded default** (leave every prompt key unset and have the
  default fire as rung 3 with layers appended) — rejected, as §3 states. It is the previous option
  wearing a list: it re-introduces two-persona stacking and re-opens the floor question, with the
  extra hazard that the text being appended to is text the user has never read. A user who wants
  the shipped default as a *base* takes ADR 0064 §7's editor seam and owns the result as rung-2
  configuration — which is an explicit, visible copy, not an invisible composition.
- **Fold layering into `system-prompt-text` as a list-or-string** — accept either a scalar or a
  sequence under the existing key. Rejected. It makes one key mean two things by its YAML *type*,
  which is the failure mode this ADR exists to remove; it collides with ADR 0023 §2's whole-entry
  replacement, since a per-model entry replacing a *list* would have to answer whether it replaces
  every element or the last; and it breaks the settings surface, where `system-prompt-text` is a
  multi-line editor row (ADR 0037) that cannot show a list.
- **Composition in the settings editor only** — no key at all; the user pastes what they want
  into the pre-filled multi-line editor. Rejected: this is the status quo, and the Context says
  why it fails. The paste is a frozen copy of the embedded default, invisible to every later
  upgrade, and it cannot express "this sentence under every model" at all.
- **Per-model layers** (a `layers:` list inside a `system-prompt-models` entry) — not built here.
  The global list already covers the case the surprise names, and a per-model list would
  immediately raise the replace-or-concatenate question §2 avoids by having exactly one list. It
  stays open; the key's shape does not foreclose it.
- **A prepend channel, or per-layer placement** (before/after the selected prompt) — rejected as
  unevidenced surface. Appending is the order that keeps the selected prompt's opening persona
  first, which is the position a small model weights most; a user who needs their text first can
  put it in the prompt.
- **An `export`/`import` command for layers** — rejected with ADR 0064 §7's reasoning: a second
  copy on disk is the freeze. A layer's `file:` spelling already puts a layer in a file the user
  owns.

---
title: ADR 0067 decision
---

**A new top-level `system-prompt-layers:` key holds an ordered list of text fragments that are
appended, in listed order, to whatever prompt the ladder selected. If it is enabled it is sent,
and nothing the user did not enable is sent.**

**1. The key is `system-prompt-layers:`, a list, beside its siblings.** It is top-level and
file-only, following the same convention as the three keys ADR 0023 §1 named — a standing
statement about how you work, so no flag and no environment variable. It sits beside
`system-prompt-text:` and `system-prompt-file:` in the config file and in `/settings`. The **full
top-level key set** is therefore five: `system-prompt-text`, `system-prompt-file`,
`system-prompt-models`, `system-prompt-layers`, `use-default-prompt`. This supersedes ADR 0023
§1's "three top-level keys" count (already amended to four by ADR 0064 §3); the character of every
one of those keys is unchanged.

**2. Layers are NOT a fifth rung — they append AFTER the selected prompt.** Rung selection runs
exactly as ADR 0064 §2 describes and picks at most one whole prompt; the layers are then appended
to whatever it picked. A layer therefore **never competes with** the rung it lands behind, and a
per-model entry that replaces the global prompt does **not** replace the layers: standing text a
user wants under every model is written once and rides every rung. Whole-entry replacement is
intact because it is a rule about the *selected* prompt, and the layers are not one — they are a
second, explicitly enabled channel.

**3. Layers never trigger the embedded default, and never suppress it.** The default's role stays
exactly what ADR 0064 §2–§3 made it: the third rung, fired when **nothing at all** is configured
and `use-default-prompt` is not `false`. Configuring layers alone is not configuring a prompt, so
the question the default answers — "did the user configure a prompt?" — is answered by the
ladder's own inputs and by nothing the layers do. Two boundary readings follow and are binding:

- **Layers alone, `use-default-prompt` untouched**: the ladder selects nothing (no text, no file,
  no matching model entry), so the run sends **the layers alone**. The default does not fire
  behind them. This is the reading that keeps the flag honest: a layer is an explicit act, and
  firing the default underneath it would be apogee sending fifty lines the user did not ask for —
  the very surprise §1 of the Context describes, inverted.
- **A prompt plus layers**: the selected prompt, then the layers. `use-default-prompt` is not
  consulted at all, as ADR 0064 §3 already says of the configured case.

The composition this does **not** offer is "the shipped default, plus my lines". By ADR 0064 §2
the default is a rung, and a rung that fires supplies the whole prompt; layering behind it would
be appending to a fallback, which stops being a fallback the moment anything rides on it. A user
who wants the shipped text as a base takes ADR 0064 §7's editor seam and owns the result as rung-2
configuration — see the rejected alternatives for why that stays the answer.

**4. Composition order is fixed and shallow: selected prompt first, then layers in listed YAML
order, joined by a blank line.** The parts are joined with `"\n\n"` — the separator a reader of
the rendered prompt sees as a paragraph break, and the one the engine already produces between the
standing sources it composes. Order is the file's order, top to bottom, with no sorting, no
priority key and no per-layer placement: the user reads the composition off the config file, in
the order it is written. Nothing is deduplicated and nothing is trimmed — a layer that repeats the
prompt's own sentence sends it twice, which is a defect in the config and reads as one.

**5. A layer states exactly one of `text:` or `file:`.** The shape mirrors ADR 0023 §1's two
spellings, one entry at a time:

```yaml
system-prompt-layers:
  - text: "This box has no network. Prefer rg over grep."
  - file: house-conventions.md
```

A `file:` resolves exactly as `system-prompt-file` does — a leading `~` expands, and a relative
path resolves against the **apogee home**, never the workspace, for ADR 0023 §1's reason: the key
lives in a global file that travels with the home. Stating **both** spellings in one entry, or
**neither**, is a defect in the file itself and is a validation error naming the offending index
(`system-prompt-layers[N]`), the same class of check ADR 0023 §3 puts at every level while config
resolves. Every layer is a **template** in the same closed four-placeholder language (ADR 0023
§4); an unknown placeholder in a layer is a startup error naming the index and the known four,
never raw braces on the wire.

Unlike a non-matching `system-prompt-models` entry, a layer's file **is** read on every run that
resolves the prompt: a layer is unconditional by construction, so ADR 0023 §2's travelling-config
argument does not apply to it — an unreadable layer file is a startup error naming the index and
the path.

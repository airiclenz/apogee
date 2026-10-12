---
title: System prompt
---

**System prompt**:
The standing instructions apogee sends ahead of the user's own messages — the **first system
message of every request**, rendered fresh per request from a **template** the user configures
in `~/.apogee/config.yaml` (`system-prompt-text` / `system-prompt-file`, plus per-model
overrides in `system-prompt-models` and the additive `system-prompt-layers`; file-only — no
flag or env) — or, when the user configures
none, from the **embedded default** apogee compiles into the binary and refreshes with every
upgrade, unless `use-default-prompt: false` turns it off. Resolution is a four-rung ladder, first
hit winning and supplying the *whole* prompt: matching per-model entry > top-level text/file >
embedded default > nothing. **Layers** are the one additive channel beside that ladder:
`system-prompt-layers:` is an ordered list whose entries each state exactly one of `text:` or
`file:` (a `file:` resolving like `system-prompt-file`), appended to whatever the ladder
picked — selected prompt first, then the layers in listed order, joined by a blank line. They
are **not a rung**: a per-model entry replaces the selected prompt but never the layers, layers
alone are sent alone, and they never fire the embedded default, which stays the fallback for a
wholly unconfigured prompt. The template's whole vocabulary is four strictly-spelled
placeholders — `{{workspace}}`, `{{datetime}}` (the **date** only, so a local server's prefix
cache survives a turn), `{{mode}}`, `{{scratch}}` — and an unknown one is a startup error, never
raw braces on the wire. It is **request-scoped**: seeded into the
request projection at position 0 and never committed to the conversation, so it appears in no
history and in no Session record, and a Reaction's directives and the Model profile's rendered
tool menu fold in **after** it within that one message, as does the engine's
**Orientation block** (prompt → orientation → delegate block (delegations only) → task list (once
the model has written one) → context files → directives → tool block). A
**Sub-agent inherits** it. Distinct from apogee's two **internal** prompts, which it never
reaches by construction: the Compaction summariser's instruction and the probe battery's. It is
**config-tier**, part of the Bypass floor in both arms, never a Reaction — and which home a new
sentence of guidance belongs in (host fact → Orientation block, standing steering → this template,
floor-wide and failure-shaped → Floor guard, model-gated and measured → Reaction,
task-shaped → Skill) is ADR 0064's placement rule. See
ADR 0023 and
ADR 0064 and
ADR 0067.
_Avoid_: "persona", "preamble" (either may be its *content*; the term names the channel),
"prompt" alone (in TUI speech that is the user's own input line), "instructions block" (that is
the profile's engine-owned tool menu, which follows it), "rung" for a **layer** (a layer appends
after the ladder's pick and never competes with it). Distinct from a **Skill**, which is
turn-local and invoked from one message.

---
title: ADR 0037 context
---

ADR 0035 built the settings
surface and drew two lines around it that were true when it was written and are not design. Its
decision 9 made `mode` **the only live apply**: every other committed edit rendered a
`→ value (next launch)` marker, a row already overridden by an env var or flag said so *instead of*
applying, and "the pane never rebinds". Its decision 3 kept v1 to **simple keys only**: bools, ints,
strings and enums are editable, while `servers`, `mcp-servers`, `mechanisms`, `validated-sets`,
`system-prompt-models`, `model-profile`, `context-files.names`, `system-prompt-text` and
`system-prompt-file` rendered read-only behind an **"edit in `config.yaml`"** pointer.

Both lines were drawn for the same reason: the engine had one live mutator (`SetMode`) and the pane
had one editor (an append/pop text buffer). Neither limitation was argued from what a settings
screen *should* be — 0035 says so itself, calling the marker "the visible cost of decision 9" and
the pointer "honest about where that editing happens today". What that honesty ships is a screen
listing forty keys that, for most of them, tells you to restart or to go somewhere else — and the
surface met its first user the day it landed. The owner's 2026-08-06 layout note put it plainly: *"settings must take effect immediately … I do not
want `→ new value (next launch)`"* — and the row should show the new value in place, marked ` *`.

What changed underneath is that the seams exist or are one small mutator away. `SetMode` and
`SetConfineToWorkspace` established the **anytime-safe** class (each behind its own mutex, read live
by the worker); `Rebind` established the **idle-only validate-then-commit** class, and the TUI
already defers a rebind that arrives mid-run through `pendingRebind` and applies it at the next
quiescent boundary (ADR 0024,
ADR 0007). `/server` already performs a whole live
rehome and records the choice (ADR 0028,
ADR 0036).
The question left is not *can a running session absorb a settings change* but *which boundary each
change lands on, and what the row says while it waits*.

A 2026-08-06 grill settled that, question by question, with the owner. This record is its
ratification; it is implemented by `docs/plans/2026-08-06 - 00 - settings-live-apply-plan.md` and its
visual half by `docs/layout/settings-screen-layout.md`.

The constraints it lands inside:
ADR 0031
— every mutator must be a Driver-agnostic engine seam, and the engine stays wire-silent (it is
handed resolved values; it never reads a config file);
ADR 0011 — the renderer is thin, its
Model is copied by value, and no YAML enters it;
ADR 0012 — the
confinement loosen stays single-homed in `/confine`;
ADR 0026 and
ADR 0023 — prompt-prefix
data is session-scoped on purpose, because a prefix that changes mid-session throws away the KV
cache;
and ADR 0035's own decision 2, whose persistence contract this record leaves entirely alone.

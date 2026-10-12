---
title: "Context cost: the tokens apogee itself puts before the user's message"
---

**Context cost**:
The tokens apogee **itself** puts in front of the model before the user's message — the standing
system content (the System prompt, the Orientation block, the delegate
report and task list blocks when present, the Context files) plus the tool
menu (native `tools`, or the rendered tool-instruction block on a non-native profile) — reported as
a **total and a per-piece breakdown**. It has two sources that are never blended: the
**estimate** (chars/token, offline, always spelled `~`, taken idle after construction) and, once a
request has gone out, the **measured** Turn-1 `prompt_tokens` the provider itself reports. The
Reactions' directives are part of it **only when measured** — the pre-request cascade is never
dry-run for an idle number. The user's message, a **Skill** body and an `@file` reference are
never part of it (per-message, not Turn-1 standing cost). The engine produces it
(`domain.ContextCost`, `Agent.ContextCost()`, facade `apogee.ContextCost`); `apogee probe context`
prints the estimate table for free and `--live` sends one fixed request for the measured number
(paid only on the flag); headless carries both on `run_finished`. Its **tripwire** pins the
injected *bytes* per piece in a golden, never the tokens, so growth in what apogee injects is a
read diff rather than a silent drift. See
ADR 0079.
_Avoid_: "context usage", "fill" (those are the whole window's occupancy over a session — the
Budget's gauge — where this is apogee's own Turn-1 share), "standing
tokens" (the older whole-string estimate `StandingTokens`, which this report now derives).

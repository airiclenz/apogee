---
title: Hook
---

**Hook**:
The colloquial alias for a **user**-origin **Reaction** — "what
other tools call a hook" — offered in the manual's introduction and nowhere else in the glossary
(ADR 0076 D12). The
observe-only Hook of ADR 0073
is the **observe** cell of the user row: an entry naming the notice **Moment**(s) it fires on and
one `run:` — an argv, or a webhook mapping — optionally filtered to one `workspace:`, composed by
every **Driver** from one shared library and run after the fact on the user's own machine, outside
confinement, as the user's config. A user Reaction that returns text (`advise:`) or a decision
(`gate:`) is still a Reaction, not a "hook that talks back".
_Avoid_: "Hook" as a glossary term (say **Reaction**, and name the class), "plugin" (a Reaction
adds no capability to apogee), "trigger" (that names the `on:` half of a daemon Schedule's
envelope).

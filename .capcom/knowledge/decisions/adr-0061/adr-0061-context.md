---
title: ADR 0061 context
---

apogee's skill catalog is **host-side**. Skills are discovered from layered directories and the
model learns that a given skill exists only when the user names its id as a `/token` in a message
(ADR 0027,
ADR 0032): no id, no display name, no
summary and no count reaches the prompt otherwise. That is a deliberate property, and it holds
today only because the catalog is small enough to remember.

It will not stay small. A user's global library plus a repo's own `skills/` runs to dozens of
entries, and at that size the human types a message without knowing that the skill which fits it
is sitting in the library. The affordance the TUI offers — type `/`, read the merged menu — only
helps someone who already suspects the skill exists.

The industry default is to tell the **model** instead: Claude Code and Codex advertise every
skill's name and description in the prompt and let the model pick. apogee refuses that default for
two reasons. It spends context on **every** request for something relevant in a small minority of
them; and any behaviour that spends context to steer the model is a **Mechanism** in our
catalogue — gated, catalogued and bound by the Bypass invariant that Mechanisms on must never make
a model perform worse than Mechanisms off. A catalog blurb prepended to every request has never
been benched against that floor and is exactly the kind of always-on tax the invariant exists to
refuse. What the human actually needs is not model-side at all: a hint while they type.

The precedent survey (2026-08-27 brainstorm) says the same thing from four directions. Anthropic's
tool-search tools (`tool_search_tool_bm25` / `_regex`) rank a large catalogue **on demand** and
leave the cached prompt prefix untouched; Codex's skills list concedes the cost outright with a
2 % context cap that shortens, then omits, once the catalogue outgrows it; OpenCode exposes a
model-callable `skill` tool; Cursor auto-attaches rules by glob without asking anyone. The first is
the shape apogee wants (rank against the request, pay nothing in the prefix) and the last three are
the shapes it is deferring. Where the result is painted is bound by
ADR 0053 — the filtered picker the band opens is
the one list surface, not a ninth bespoke overlay — and its knob by
ADR 0037, every settings edit applying
live. The split between who ranks and who paints is
ADR 0031's:
the engine must stay sufficient for any Driver, so the matcher is engine-level and the band is not.

---
Status: accepted
---

# Skill suggestions are Driver-side, painted over an engine-level matcher

## Context

apogee's skill catalog is **host-side**. Skills are discovered from layered directories and the
model learns that a given skill exists only when the user names its id as a `/token` in a message
([ADR 0027](0027-one-slash-namespace-with-inline-skill-tokens.md),
[ADR 0032](0032-the-user-skill-library-outranks-the-workspace.md)): no id, no display name, no
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
[ADR 0053](0053-popup-surfaces-embed-one-list-surface.md) — the filtered picker the band opens is
the one list surface, not a ninth bespoke overlay — and its knob by
[ADR 0037](0037-every-settings-edit-applies-to-the-running-session.md), every settings edit applying
live. The split between who ranks and who paints is
[ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)'s:
the engine must stay sufficient for any Driver, so the matcher is engine-level and the band is not.

## Decision

**1 — Suggestion is a Driver concern painted from an engine-level matcher.** The ranking lives in
the engine as `skills.Catalog.Suggest`: BM25 over a document of id + display name + summary + the
skill's optional author-declared `triggers:` phrases (bodies are **not** indexed), a trigger-phrase
hit adding a fixed boost on top of the score, an evidence gate admitting a skill only on a trigger
hit or ≥ 2 distinct non-stopword draft terms, and the top 3 returned. It needs no model, no network
and no embedding. The TUI paints those rows in a one-row band above the input box, sibling of the
staged-interjection band, and opens the filtered `/` picker on Tab. Any other Driver — headless, the
daemon, a bench harness — can call the same API and present it however it likes, or not at all.

**2 — Nothing about the catalog reaches the model.** A suggestion is a fact about the *draft*,
computed and displayed on the host; it changes no request. A suggested skill becomes model-visible
by exactly the route every skill has always taken: the user accepts it into a `/id` token in the
message text and the loop prepends that one body to that one message. Because the model's input is
unchanged, suggestion is **not a Mechanism** — there is nothing for Bypass to switch off and nothing
for a bench arm to measure.

**3 — A skill shown at send is spent for the session.** The skills standing in the band at the
moment a message is sent — a submit or a staged interjection alike — are never suggested again in
that session; a skill already invoked in the draft is never suggested at all. Before a send the band
is free to change with every keystroke. The spent set is in-memory Driver state, cleared by `/clear`
and `/new`, and a restored session starts empty. The rule exists so that a suggestion the user has
already declined, in the one moment where declining it was a decision, stops asking.

**4 — Model-facing discovery is deferred, and reopening it needs a superseding ADR.** Two shapes
are named and not built: **B1**, auto-attaching a skill body to a message that carries no `/id`, and
**B2**, a model-callable `load_skill` tool. B1 puts prompt text into the request that the user did
not ask for — a Mechanism, which must be catalogued, gated and benched against the Bypass floor
before it can ship. B2 does that and more: it makes a skill something the model *calls*, which
contradicts CONTEXT.md's *Skill* entry directly ("_Avoid_: 'tool'"), so it is a domain-language
change before it is a feature. Either one may well be right later; neither is decided here. A later
ADR must supersede this record **explicitly** to build them, and the deferral, with what must
precede it, is parked in `ISSUES.md`.

## Consequences

- **The engine gains a ranking API and stays wire-silent.** `skills.Catalog.Suggest` reads the
  catalog snapshot and returns rows; it emits no Event, touches no request and knows no Driver.
  ADR 0031's wire-silent invariant is untouched, and the benchable-all-the-way-up one is not
  engaged, because suggestion cannot change a model's output.
- **`triggers:` becomes a frontmatter key skill authors write.** It is read only by the matcher and
  never shown to the model, so it is free to be verbose; a malformed value is a soft field error
  that leaves the skill loaded with no triggers rather than sinking it.
- **The band is presentation and behaves like one.** It is governed by `ui.skill-suggestions`
  (default on, applied live per ADR 0037); off, the band never paints and Tab stays inert. It never
  goes modal and never steals `⏎`.
- **The cost is host-side and bounded.** The index is built once per catalog snapshot, the draft is
  re-ranked per keystroke over a corpus of dozens of short documents, and nothing is computed at all
  until the draft holds three content words.
- **A suggestion is a new term in the domain language.** CONTEXT.md's *Skill* entry defines it and
  points here; it is a hint, never an invocation, and the distinction is what keeps "the model sees
  a skill only through a `/id`" true after this ships.

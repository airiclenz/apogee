---
title: ADR 0061 decision
---

**1 — Suggestion is a Driver concern painted from an engine-level matcher.** The ranking lives in
the engine as `skills.Catalog.Suggest`: BM25 over a document of id + display name + description +
the skill's optional author-declared `triggers:` phrases (bodies are **not** indexed; the
description is indexed unclamped — the 200-rune summary clamp is the `/` menu's alone), a draft
term matching a document term exactly or by a ≥ 4-rune prefix either way, an id/display-name hit
adding one IDF's worth of bonus, a trigger-phrase hit adding a fixed boost on top of the score, an
evidence gate admitting a skill only on a trigger hit or ≥ 2 distinct non-stopword draft terms, and
the top 3 returned. It needs no model, no network and no embedding. The TUI paints those rows in a
one-row band above the input box, sibling of the staged-interjection band, and opens the filtered
`/` picker on Tab. Any other Driver — headless, the daemon, a bench harness — can call the same API
and present it however it likes, or not at all.

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

*Amended 2026-08-27* (plan `2026-08-27 - 03`, suggest-filtering predictability): Decision 1 now
reads as amended — the enough-draft gate counts raw words, stopwords included, rather than content
words; a draft term also matches a document term by ≥ 4-rune prefix either way; an id/display-name
hit adds one IDF's worth of bonus; and the document indexes the skill's full description rather than
the 200-rune menu summary. An amendment, not a supersession: the decision's substance — Driver-side
painting over an engine-level matcher that never reaches the model — is unchanged.

*Amended 2026-09-14* (plan `2026-09-14 - 00`, skill-band precision): Decision 1's "the top 3
returned" now reads "up to 3 returned" — `Suggest` keeps a row only when its score reaches 60 % of
the top row's (`minRelativeScore`; the top row always survives, the cutoff is relative, never
absolute), so a sentence that shares two common words with three skills no longer earns three rows,
and the band names the skills that clearly fit rather than the closest three. Three dev-generic
words — `file`, `files`, `add` — join the stopword set, so a draft that merely edits a file admits
nothing on those words alone; the set stays closed to any word a shipped trigger phrase uses
(`change`, `update`, `fix`, `code` stay content terms). The cutoff is `Suggest`'s alone: `load_skill`
(ADR 0065 §6) keeps the evidence gate and its own `confidentMargin`, and must still see every
admitted skill to name candidates — only the stopword change reaches it. The prefix rule is
untouched: a draft term still matches a document term by ≥ 4-rune prefix either way. An amendment,
not a supersession, on the same terms as the one above.

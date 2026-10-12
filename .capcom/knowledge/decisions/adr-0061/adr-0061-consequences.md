---
title: ADR 0061 consequences
---

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
  until the draft holds three words (stopwords included) and at least one content term.
- **A suggestion is a new term in the domain language.** CONTEXT.md's *Skill* entry defines it and
  points here; it is a hint, never an invocation, and the distinction is what keeps "the model sees
  a skill only through a `/id`" true after this ships.

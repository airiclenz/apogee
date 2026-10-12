---
title: ADR 0064 context
---

ADR 0023 made the system
prompt a configured template and put the shipped default in **one place**: an uncommented
`system-prompt-text:` in `internal/config/defaults/config.yaml`, seeded into `~/.apogee/config.yaml`
on first run. §8 chose that deliberately over a compiled-in fallback — "deleting the key is how you
turn it off, and an upgrade therefore changes nothing for anyone already running" — and the
rejected-alternatives list repeats the reasoning.

The property that bought is the property that now costs. `seedConfig` never overwrites, so the
default a user received on the day they installed apogee is the default they still have. Every
sentence of standing guidance written since then — every convention learned from a bench run, every
correction of a small model's habitual failure — reaches nobody who installed earlier. The default
is not merely un-upgradable; it is **frozen per install**, so there is no single text whose quality
can be improved at all.

ADR 0023's own 2026-08-25 amendment (ADR 0023)
already named this gap in as many words — "a fact added to the shipped template after a user's first
run reaches that user never" — and routed *around* it for one class of content: host **facts** moved
into the engine-composed orientation block, which no upgrade can miss and no edit can delete. That
was the right fix for facts. It left **steering** — who the assistant is, how it works, what it does
before it edits — with no maintained channel at all, and it left the general question unanswered: a
new sentence of guidance now has four plausible homes (orientation block, template, Mechanism,
skill) and no written rule for choosing between them.

What forces the question now is the shipped-skills wave
(ADR 0065), which gives task-shaped guidance a
maintained home in the binary. Standing guidance needs the same, and the two must not compete for
the same sentences.

This ADR records the design the owner ratified on 2026-08-31, implemented by
`docs/plans/2026-08-31 - 00 - base-guidance-and-shipped-skills-plan.md`. It **supersedes ADR 0023
§8's "no compiled-in fallback" rule and the matching rejected alternative**, and nothing else.

---
title: ADR 0067 context
---

ADR 0023 §2 made the
system prompt **whole-entry replacement**, and
ADR 0064 §2 extended that rule down the
whole four-rung ladder: a rung that hits supplies the **entire** prompt, and the embedded default
"is never appended to, merged into, or filled in behind an explicit prompt — half a prompt is
still not a prompt". That rule is right and is not in question here. What it leaves the user
without is a way to say the *other* thing: keep whatever prompt you would have sent, and add
these lines to it.

The gap shows as a **one-flag-two-meanings surprise**. `use-default-prompt` reads, to a user
scanning the config file, as "use apogee's default prompt" — so a user who sets it `true` (or
leaves it at its default) alongside their own `system-prompt-text:` expects both. ADR 0064 §3
gives it exactly one meaning — it governs the nothing-configured case and "has no effect
whatsoever when text or a file is configured at either level" — so the second reading is silently
wrong: the user's five lines *replaced* fifty they never saw. Nothing in the file says so, because
the thing that vanished was never in the file.

The workaround the surface offers is the **paste-into-the-editor** one, and it is the one thing
ADR 0064 set out to end. §7 pre-fills the settings editor with the embedded default so a user can
"open it, change a line, save"; a user who wants the default *plus* their own lines saves the
pre-filled bytes with their lines appended, and the saved result is ordinary rung-2 configuration.
That is a second copy of the default on disk, frozen at the day it was pasted — precisely the
per-install freeze §1 unfroze, re-created by hand and now invisible to every upgrade. The same
applies to a user's own standing text they want under *both* a per-model entry and the global one:
the only way to have one sentence in two prompts is to write it twice.

There is also no additive channel for text that is **not** persona at all. The engine has one —
the Orientation block (ADR 0023's 2026-08-25 amendment) — but it is engine-owned host facts by
construction, and ADR 0064 §5's placement rule sends everything else to the template, a Mechanism
or a Skill. A user's own standing addendum ("this box has no network", "prefer `rg`") fits none of
them, and today can only be spliced into whichever prompt is selected.

This ADR records the design the owner ratified on 2026-09-01, implemented by
`docs/plans/2026-09-01 - 01 - system-prompt-layers-plan.md`. It **supersedes** ADR 0023 §1's
top-level key count and nothing else: the ladder, whole-entry replacement and the embedded
default's fallback-only role all stand exactly as written.

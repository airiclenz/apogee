---
title: "Skill: a reusable block of instructions the user invokes from a message"
---

**Skill**:
A reusable block of instructions the user *invokes* from a message — a folder holding a `SKILL.md`
(YAML frontmatter — id, display name, summary, optional triggers — plus a Markdown body).
Skills are discovered from layered dirs (the project's `.apogee/skills`, — when
`use-project-skills` is on — the project's `skills/`, and the global `<apogee home>/skills`) plus
a fourth, **lowest-priority** source: the skills apogee **ships embedded in the binary**
(`debugging`, `planning`, `code-review`, `commit-hygiene`, and the `audit` Recipe), never
installed to disk, refreshed by every upgrade and switched off wholesale by `use-shipped-skills`. The **user's global library wins
any cross-source id clash** while the two project dirs keep their order between themselves, and a
shipped skill is the weakest claim on an id in the system; a repo may contribute a new skill but
never replace one of the user's, and every shadowed copy is recorded so `/skills` names both
files. A skill is invoked by naming its id as a **`/token`** in the message
text — `/code-audit please check the parser` — at a word boundary and whitespace-delimited,
exactly parallel to an `@path`. The token **stays in the text** the model reads, and only a token
the catalog confirms is a reference: any other `/word` inside a message is prose (a path survives
untouched), and a **command verb shadows** a skill whose id OPENS with that verb — the cut the
command parser makes, not whole-id equality, so an id carrying arguments cannot slip past the
shadow and be parsed as the command it names. An id is **one token**: the loader refuses one
holding whitespace or a control character, because a repo writes both the frontmatter and the
folder name an id can come from. Like an `@file`, a skill is
**turn-local**: the loop resolves the extracted IDs (`UserInput.SkillIDs`) through `Config.Skills`
and prepends each body to *that one* user message, so a skill never persists as a system-prompt
edit; each body meets the same **structural floor** a **File reference** block does, elided
head/tail before its header against that message's shared reference split, so an outsized
`SKILL.md` cannot wedge the Turn it was invoked to steer. The TUI parses and offers (one merged
`/` menu, and `/skills` to browse the catalog — every row labelled with the source it came from,
and `/skills export <id>` copying a shipped skill's folder into the global library, refusing to
overwrite one already there); the agent resolves. The
`/token` is not the model's only door: **`load_skill`** is a default-on **tool** — an ordinary
`tools.enabled`/`tools.disabled` entry, never a Reaction — with which the model fetches a body on
its own initiative, one adaptive call returning an exact id's body, a confident match's body plus
the other ids that matched, or id-and-summary candidates to call again with. The catalog itself
still never enters the standing prompt. See
ADR 0027,
ADR 0032,
ADR 0061 and
ADR 0065.
A **Suggestion** is a Driver-side hint naming the catalog skills whose id, name, description or
`triggers:` best match the draft (`skills.Catalog.Suggest`, BM25 + evidence gate); painted by the
TUI in the suggestion band above the input box, accepted via Tab into a `/token`, and spent for the
session once a message is sent with it showing. A suggestion never reaches the model — see
ADR 0061 above.
_Avoid_: "plugin", "tool" (a skill is prompt text, not executable; it adds no capability — it
steers the model; `load_skill` is a tool that *fetches* that text, the way `read_file` fetches a
file without making a file a tool), "attachment"/"chip" (a skill is text *in* the message, not
state beside it — chips are retired from every surface: the strip above the box went with ADR 0027
and the sent block's `✦ name` row with its 2026-08-04 addendum, which paints the `/token` in the
skill violet where it stands instead). Distinct from a **Reaction** (a loop behaviour
that fires at a Moment).

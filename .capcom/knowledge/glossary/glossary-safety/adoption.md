---
title: "Adoption: the user's recorded acceptance of one granting Project config entry"
---

**Adoption**:
The user's recorded acceptance of one granting **Project config** entry — an **Allow rule**, an
`mcp-servers:` entry or a `reactions:` entry — pinned in `~/.apogee` to that entry and its **Project
root**. An Allow rule is pinned by its exact text; a server or reaction by a canonical form of the
fields that decide what runs or where data goes (the whole server entry; a reaction's `id`, `run:`,
`advise:` and `gate:`), together with the SHA-256 of every in-root project file its argv or an `env:`
value names, so a changed script re-proposes the entry and the pinned files are re-checked before
every spawn (ADR 0104). An entry with no matching pin — new, edited, pulled from a teammate, or
written behind apogee's back — is *proposed* and does nothing until adopted. An adopted repo entry
keeps tighter rules than a global one: the global entry of the same name or id wins, a stdio
server starts on the platform environment floor, a reaction's `run:` is fenced and scrubbed and runs
in the Project root, a program inside the root runs only when pinned, and only a project-adopted
Allow rule or a session approval answers a repo server's tools. An adopted script runs whatever it
contains. A **Driver** reads Adoption through one immutable *Adoption snapshot* per event
(`internal/project`): the layered config, the Project config's offer and the entries it asks about,
all from one resolution; two snapshots' difference names the kinds — Allow rules, servers,
reactions — whose in-force set moved, and only those are applied.
_Avoid_: "trust" (adoption is per entry, never a whole-folder yes), "approval" (that is one call).

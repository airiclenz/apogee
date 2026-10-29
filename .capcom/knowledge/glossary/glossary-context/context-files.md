---
title: "Context files: the project's standing text discovered in the workspace root"
---

**Context files**:
The **project's** standing text: files apogee **discovers** in the workspace root — configured by
NAME (`context-files:`, default `AGENTS.md`; file-only, root-only, no walk-up) — whose content is
folded into the standing system content beside the System prompt. Every
listed name that exists is included, in list order, each **fenced** between a
`## Workspace context: <name>` header and a `## End of workspace context: <name>` footer, and the
merged first system message reads **prompt → Orientation block → Delegate report block
(delegations only) → Task list block (once the model has written one) → context files → Reaction
directives → tool menu** — the engine's own blocks first, so no workspace text precedes them;
either configured source alone seeds the message. Content is **data, never a template**: it
bypasses the placeholder language entirely, so a repo's own `{{braces}}` travel verbatim and can
never fail startup — the one rewrite is the fence, which prefixes with `[workspace text] ` any
content line spelling the standing message's own furniture — that header, that footer, an
engine-owned block's opening line (Orientation, Delegate report, Task list) or the advice and
engine-note fences (`standingFences`, derived from the standing-block table, so a new block joins it
at once). The read is
**session-scoped** — at construction and at each session boundary
(`/clear`, `/new`, a restore), never per request and never mid-conversation — so the bytes are
fixed for a session (the server's prefix cache survives) and an edit lands on the next `/new`; a
**Sub-agent inherits the parent's bytes**, copied rather than re-read. A missing or empty file is
skipped silently, a present-but-unreadable one **loudly** (a note, never a startup failure — it was
discovered, not named); a malformed *name* is a startup error. Oversize is **advisory only**: the
host names each loaded file and warns when the standing content exceeds the
Budget's **advisory ceiling** (`StandingAdvisory`, a fixed 15% of the
working room) — which, since the Budget began reserving what the standing parts *measure*, is no
longer the room actually reserved for them: the ceiling is a warning line, not an allocation, and
nothing is ever capped or truncated. See
ADR 0026.
_Avoid_: "project prompt", "workspace prompt" (the System prompt is the user's, these are the
project's — the terms must stay separable), "AGENTS.md support" (that is the default name, not the
concept), "loaded into context" (they are request-scoped standing content, not history). Distinct
from a **File reference** (`@file`, turn-local and user-named) and from a **Skill** (invoked by
`/token` in one message).

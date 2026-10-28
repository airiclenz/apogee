---
title: "Project pin: what an adopted repo entry is checked against before every spawn"
---

**Project pin**:
What an adopted repo `reactions:` entry or repo stdio `mcp-servers:` entry carries to its spawn:
the **Project root** it was adopted under and the text of its **Adoption**, with the SHA-256 of
every in-root project file that text names (ADR 0104 §4, §8). Before every spawn the pin *admits*
the launch — it re-reads those files once and hands back the Project root, which is the spawn's
working directory and the base its relative argv[0] resolves against, together with the pinned
programs, the only files inside the workspace exec fence the spawn may run — or refuses it as
*moved*: one typed refusal naming each file that changed, went missing or is newly named, which
each launch kind words with its own prefix. A pin whose files cannot be checked at all refuses the
same way and cues the same re-read, but on that re-read the config layer cannot pin the entry
either, so it drops the entry with its refused notice rather than proposing it again. argv[0] is
resolved by each launch kind, not by the pin.
_Avoid_: "trust" (a pin admits one launch of one entry), "hash" for the whole pin (the digests are
one part of it).

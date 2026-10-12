---
title: Key class
---

**Key class**:
What a **Project config** may do with one config key, carried by the key's registry row
(ADR 0096 §6). Four exist.
**Project parameters** describe the project and widen nothing, so the project's value outranks the
global one and is live as written (`context-files.*`, `use-project-skills`, the three `workflow-*`
limits). **Tighten-only** keys may narrow and never widen: the project's entries are added to the
global ones, never put in their place (`tools.disabled`, `url-safety.deny-hosts`, `dangerous-rules:`
whose project `remove:` is ignored). **Granting** is the **Allow rule**, inert until **Adoption**.
**Global-only** is every other key and the default, so a new key is safe until marked otherwise: a
project file that states one is ignored for it, with one start-up line naming the keys. Precedence
for a key a project may set is default < global < project < env < flag; a tighten-only key unions
instead. The **Source mark** says which layer won.
_Avoid_: "trust level" (the class belongs to the key, not to the repository), "override" for a
tighten-only key (it adds to the global value, never replaces it).

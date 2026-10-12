---
title: "Project root: the folder whose `.apogee/` holds the project's config and skills"
---

**Project root**:
The folder whose `.apogee/` holds the project's **Project config** and skills: the nearest folder
with a `.apogee/`, from the **Workspace** up to — never past — its git top-level; the Workspace
itself when there is none or no repo. Never `$HOME`'s `~/.apogee`, which is the global config.
_Avoid_: "repo root", "git root" (the Project root may sit below either).

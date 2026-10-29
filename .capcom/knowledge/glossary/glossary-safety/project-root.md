---
title: "Project root: the folder whose `.apogee/` holds the project's config and skills"
---

**Project root**:
The folder whose `.apogee/` holds the project's **Project config** and skills: the nearest folder
with a `.apogee/`, from the **Workspace** up to — never past — its git top-level; the Workspace
itself when there is none or no repo. Never `$HOME`'s `~/.apogee`, which is the global config.
A running session fixes its Project root at boot and every reader uses that answer, the live config
re-read (`/settings`, the file watcher) included: a `.apogee/` or `git init` that appears below it
mid-session never moves, adds or drops the project layer until the next start
(`config.Options.ProjectRoot`/`ProjectRootPinned`; an empty fixed root means no layer) (2026-10-14,
docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md).
_Avoid_: "repo root", "git root" (the Project root may sit below either).

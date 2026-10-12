---
title: ADR 0048 considered and rejected
---

- **Writing the launcher's own config — an "active profile" or autostart key in its YAML** — rejected
  on ADR 0029 decision 4 (apogee never writes the profile store) and on preset-vs-state grounds: that
  file is a curated library of recipes, shared across machines, read by more clients than this one, and
  what one apogee session loaded last is not a property of the recipe. Boot autostart, if it is wanted,
  is llama-launcher's own feature to build, in its own file, for every client at once.
- **One key for both classes — writing `model:` on launcher-fronted entries too** — rejected: on that
  class `model:` is an empty discovery hint, and a wire id recorded beside a profile is a second answer
  to "what runs here" that goes stale the first time the launcher loads anything else.
- **Validating that the recorded profile exists at config load** — rejected: it would make a config
  check depend on the launcher's file, which is read fresh at use precisely so edits in the launcher's
  own TUI are live. A profile deleted between two sessions is a boot-time note, not a file defect.
- **Insisting on the restore — unloading what runs, or starting a second instance beside it** —
  rejected: it would displace a server the human started by hand and oversubscribe the one GPU these
  hosts have. Yielding costs a line of transcript; insisting costs the session.
- **Restoring in headless / bench runs** — rejected: those Drivers actuate nothing, and a bench that
  silently loaded a model would be measuring a different machine than the one it was pointed at.
- **A per-server override of the toggle** — deferred, not denied: one top-level key until somebody
  wants a mixed posture, at which point the registry has the shape for it.
- **Recording passive rebinds too** ("remember whatever is bound at the end of the session") —
  rejected: a heartbeat-observed change is the server telling apogee what happened, and writing it back
  would let a stranger's `/load` on a shared box, or a launcher restart, rewrite this user's config
  under a feature they enabled to remember **their** choices.

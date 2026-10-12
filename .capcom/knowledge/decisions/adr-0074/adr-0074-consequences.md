---
title: ADR 0074 consequences
---

- `/undo` survives a relaunch, a crash and the end of a headless run, and it reaches the writes of
  `terminal`, `python_exec`, MCP servers and git checkout forms that ADR 0051 documented as
  uncovered. `/redo` exists for the first time.
- apogee gains a durable store holding copies of workspace file contents under `~/.apogee/`. Its
  bound is the workspace (decision 9), its lifetime the session record's plus the scratch sweep's
  age rule (decision 13), and both are the first things to revisit if the footprint is ever a
  sighting.
- Undo becomes a two-tier feature: full on a machine with git, ADR 0051's behaviour without it.
  Every test that covers undo gains a git-absent arm, and the sentence `/undo` prints in that state
  is part of the contract, not an error message.
- The engine keeps two capture paths where it had one. That is not redundancy: decision 10 gives
  each an area the other cannot see, and decision 12 names the residue where only the funnel can
  answer.
- A daemon Firing and a headless run get a revert surface they never had — one command, printed
  where the run reports what it wrote (bead `apogee-kk0.7`).
- A Console's lifetime is untouched: ADR 0059 §1 keeps its own class, and this record claims no
  authority over it.

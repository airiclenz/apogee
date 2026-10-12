---
title: "Session fork: parent pointer on Meta and /fork from a transcript block"
status: done
type: feature
priority: 2
aliases: [apogee-zci]
created: 2026-09-07T10:48:29Z
closed: 2026-09-16T20:20:27Z
reason: "test(cmd/apogee): a fork keeps the prefix, resumes on its own and names its parent"
---

Gap 5 of the 2026-09-06 contender gap assessment, ACCEPTED by the owner 2026-09-07 as wave 2 (after gap 3).

Today sessions are flat — Meta has no parent, so there is no fork or branch. Rivals: pi has /tree, /fork, /clone and abandoned-branch summarisation; opencode and Claude Code both fork.

Fit: a parent pointer on Meta plus /fork from a selected transcript block. Structural, no bench gate. Effort S-M.

## Acceptance

/fork from a transcript block creates a new session whose Meta records the parent and whose history is the prefix up to that block; the parent session is unmodified; session listing shows the relationship.

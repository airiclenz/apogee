---
title: ADR 0042 considered options
---

- **Document hard prerequisites** ("install git, ripgrep and Python before running apogee") —
  rejected: it moves the failure to the user on exactly the hosts where they can do least about it,
  and it makes the agent's tool surface a property of the reader's diligence. A tool that reports its
  own absence is self-documenting; a README section is not enforced by anything.
- **Detect once at start-up and refuse what is missing** — rejected: the refusal would be a wall of
  warnings about capabilities most sessions never use, and PATH is not constant for a process that
  lives for hours. Detection belongs at the tool that needs the program.
- **Vendor or embed the external programs** (a bundled `git`, a bundled formatter) — rejected: it
  multiplies binary size and licensing surface per platform, and it freezes a version of somebody
  else's tool that the user's project may not want.
- **cgo-linked libraries** (libgit2, tree-sitter) for the same capabilities in-process — rejected:
  each one ends the CGO-free cross-build, which is the property that makes six release archives a
  loop rather than a build farm.
- **Refuse to start when confinement is unavailable** — rejected: it contradicts ADR 0012's
  gate-if-you-can't net and would make a kernel version a prerequisite for a Plan-mode session that
  never spawns anything.

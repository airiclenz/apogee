---
title: ADR 0098 considered options
---

- **A hidden `apogee release` subcommand**, following the `audit-split` precedent
  (ADR 0065, 2026-10-10 amendment). Rejected.
  `audit-split` lives in the shipped binary because a skill that end users run calls it.
  Releasing is maintainer-only, so the same move would put release code into every user's
  binary for nothing.
- **Keep bash and treat "Windows" as "Git Bash".** Rejected. `make` and `zip` are still missing
  there, and the publish logic would stay untested apart from `DRY_RUN`.
- **Granular publish verbs that the skill sequences and resumes.** Rejected for the converging
  `publish`. Resume logic written in agent prose is exactly the part that should be code.
- **One skill host (Claude Code only, or apogee only).** Rejected. Pinning the repo's release to
  one vendor's agent defeats putting it in the repo. Pinning it to apogee alone ties notes
  quality to whichever model apogee is pointed at. With host-agnostic skills, dogfooding through
  apogee stays a choice.

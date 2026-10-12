---
title: ADR 0080 alternatives rejected
---

- **gitleaks (or any external scanner) at the seam.** Far better recall, but an install
  dependency for a floor that ships default-on, a rule set apogee does not own, and a
  per-commit cost that would push the check off the floor into an opt-in — which for a
  mistake-guard is the same as off.
- **Scanning at write time (`write_file` / `file_edit` payloads).** Would catch the secret
  earlier, but every write is a legitimate place for one (a fixture, a scratch file, a
  gitignored `.env`) and only the commit makes it durable; write-time scanning would also
  breach the "never the payload" contract for every write tool rather than for one call.
  `terminal` `git commit` is likewise out: the shell view reads a command for what it writes,
  not for what git would stage.
- **A hard refuse (Tier 1).** A committed test fixture with a throwaway private key is a
  legitimate commit; refusing it outright with no way through would teach the operator to
  disable the guard. The Tier-2 look with the finding on the `Fix:` row is the speed-bump
  the two-tier design exists for.
- **A git pre-commit hook.** Closed by `core.hooksPath=` and by the control-plane rule, as
  the context records; a hook would also protect one repository rather than the agent.
- **An allow-list key in v1.** Deferred until a real false-positive pattern shows up; the
  approval prompt is the allow-list until then.

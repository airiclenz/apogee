---
title: ADR 0066 considered and rejected
---

- **Keep the flag and add the root key beside it.** Two spellings for one fact, and a conflict
  rule (which wins?) that ADR 0036's single-definition claim exists to avoid. The flag goes.
- **Refuse a stale `sub-agents-server:` name at startup.** A name written once and read for
  months would take the whole config hostage over a typo; the notice plus the degrade says more
  and costs nothing.
- **A CLI flag or env var for the target.** The target is a recorded session choice, like
  `server:`; the file and the picker are its two doors, and a third would need a precedence rule
  for a key that already moves mid-session.
- **A `· current` marker on the picker's rows.** `/server`'s third cell means "the session is
  bound here", which is the one thing a delegation target is not — the mark would tell a human
  their delegations run on the box they are talking to, on every session where they do not.
- **A "never for this entry" row on the migration offer.** It would persist a line that nothing
  decodes.

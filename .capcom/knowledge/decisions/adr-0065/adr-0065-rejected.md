---
title: ADR 0065 considered options
---

- **Keep shipping no skills and document how to write one** — rejected. It leaves every skill
  surface empty on a fresh install and leaves ADR 0064 §5's placement rule with nowhere to send
  task-shaped guidance.
- **Install the four skills into `~/.apogee/skills/` on first run** (the `seedConfig` pattern) —
  rejected on ADR 0064's evidence: a seeded copy is frozen per install and never improves, and it
  would additionally make shipped skills *outrank* the workspace by landing in the highest-priority
  dir, inverting §1.
- **Ship them at the TOP of the priority order** so the user gets apogee's version by default —
  rejected. ADR 0032 settled that a user's own library outranks content they did not write; content
  apogee wrote is no different, and silently replacing a hand-written `planning` skill is the exact
  failure ADR 0032 was opened for.
- **Extract bundled files to a temp dir and mount it as a real read root** — rejected. It writes
  bytes nobody asked for, needs a lifetime and a GC, and makes the read root's contents differ per
  run; a virtual namespace has none of those and is refused for writes by construction.
- **A `list_skills` + `get_skill` tool pair** — rejected by §6: it is two round trips for one
  answer, and the adaptive return gives a model that guessed a name a usable body on the first call.
- **`load_skill` as a Mechanism** — rejected by §6. It adds a tool, not a directive; there is no
  standing text for Bypass to remove and no arm for a bench to measure, since the model's input
  changes only when the model itself calls.
- **`load_skill` default-OFF, opted into per model** — rejected. Default-off tools are for
  capabilities with a blast radius; this one returns text from a catalog the user already installed,
  and `tools.disabled` is there for anyone who disagrees.
- **B1 auto-attach, shipped alongside** — deferred by §8, unchanged from ADR 0061.

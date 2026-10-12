---
title: ADR 0032 considered options
---

- **Keep the oracle order** — rejected: parity is worth real weight for *portability of skill
  files*, but not at the price of letting a clone redefine a command the user invokes by muscle
  memory. Nothing about a shared file format requires a shared collision rule.
- **Gate `.apogee/skills` behind `use-project-skills`** — rejected: it answers a different
  question. The problem is silent *replacement*, not contribution; a repo shipping genuinely new
  skill ids is a feature (ADR 0002),
  and defaulting it off would break the ordinary case to fix the adversarial one.
- **Warn but keep workspace priority** — rejected: it leaves the dangerous default in place and
  charges the user for it, on a notice they see once at load and not at the moment they type
  `/<id>`.
- **Refuse to load a colliding workspace skill entirely** — rejected: harsher than needed and it
  loses information. Recording the pair keeps the diagnosis (`/skills` can name both files) while
  the resolution alone decides what runs.

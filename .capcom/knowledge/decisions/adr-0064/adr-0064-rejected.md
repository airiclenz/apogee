---
title: ADR 0064 considered options
---

- **Keep ADR 0023 §8's seeded-file default** — rejected, by the whole Context above: it is frozen at
  first run, so the text cannot be improved for anyone already running.
- **Re-seed or merge the shipped text into `~/.apogee/config.yaml` on upgrade** — rejected.
  `seedConfig`'s never-overwrite is load-bearing (it is what makes the config file safe to hand-edit
  at all), and a merge would have to guess which of the user's lines were theirs.
- **An export command mirroring `/color-scheme export`** — rejected. The scheme case exports a
  file the resolver then reads *instead* of the built-in, which is the freeze this ADR removes.
  Pre-filling the editor gives the same starting point without creating a stale copy.
- **Make the default prompt a Mechanism** so it can be gated per model — rejected by §6. A Mechanism
  is measured against a floor; the base guidance *is* part of the floor, and a floor that switches
  itself off under Bypass is not a floor.
- **Append the embedded default beneath an explicit prompt** (so a user gets both) — rejected with
  ADR 0023 §2's merge alternatives: order and duplicate-persona questions have no obviously right
  answer, and a user who wants both can paste.
- **Per-model embedded defaults** (a short variant for small models, a long one for large) — not
  built here. It is ADR 0021's parked automatic-tiering idea in new clothes, still evidence-free;
  `system-prompt-models` is the manual lever until measurement justifies more.
- **Ship the guidance as an always-loaded skill instead** — rejected by §5: a skill is invoked and
  turn-local by ADR 0027, and an always-loaded one is a standing prompt wearing the wrong word.

---
title: ADR 0036 consequences
---

- **`config.yaml`'s schema breaks, with a migration that repairs it in place.** Four top-level keys
  go, `server:` arrives. A user who never opens the file sees one startup line and keeps working; a
  user who reads it finds a backup beside it. The change warrants a **minor** bump when the next
  version is cut — that call is the owner's, and nothing in the implementing plan touches a version
  identifier.
- **ADR 0035's authorized write set gains two members**, both named by this record: the `server:`
  recording that rides a `/server` switch, and the one-time legacy migration. Every other write
  still needs an ADR of its own; the migration in particular is bounded by decision 9 and does not
  generalize into "apogee may update your config".
- **ADR 0028 is amended, not replaced.** Its decisions 1–4, 6 and 7 stand entirely — the picker is
  still UI over the prepared layers, the hint still follows the binding, a switch still unbinds the
  model and the first beat still completes it, the Monitor is still swapped whole, the post-switch
  seed is still announced. Only D5's second half moves.
- **The seeded template becomes the teaching surface for the new schema.** `servers:` grows the
  per-field documentation the quadruple used to carry, and `server:` gets a commented example so the
  splice writer's insert-below-example rule lands a recorded choice under its own prose — the
  template-and-registry step-keeping obligation ADR 0035 recorded, applied to this change.
- **CONTEXT.md's Upstream and Heartbeat entries carry the change and gain no noun.** "Startup
  server" is a role played by an ordinary list entry, not a new term; the `/server` paragraph loses
  its session-scoped sentence and gains the recording and the first-boot ask.
- **`ISSUES.md`'s config-schema entry closes** — the grilling it asked for is this record. The
  pre-existing launch-endpoint drift it exposes (`rebindSpecFor` and `scheduleWiring.fire` keep
  reading launch `opts` after a switch) is not created here, but it becomes more visible once a
  switch persists, so it moves to `ISSUES.md` as its own entry rather than riding along.
- **The TUI grows a pre-bound phase**, a state in which no engine exists. It is contained by
  decision 8 (no other Driver has it) and by ADR 0024 (nothing is constructed endpoint-less), and it
  is the price of asking instead of guessing on first run.

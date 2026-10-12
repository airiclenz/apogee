---
title: ADR 0022 consequences
---

- **`internal/session` grows from a stub into the storage layer**: id-addressed `Record`s with
  `List`/`Load`/`LoadPath`/`Delete`/`Rename`, atomic writes (temp file + rename), update-in-place,
  and a **legacy sniff** that wraps pre-plan bare-envelope files so old `~/.apogee/sessions/`
  files still list, load, and resume (workspace-unknown, no scrollback).
- **The engine's public surface gains `RestoreSession` and `InExchange`** — the live-restore
  variant of the ADR 0001 snapshot/resume feature. Because `apogee.Agent` is a type alias for
  `agent.Agent`, they are public with no facade delegator.
- **A fourth persisted surface's story is now settled** alongside config, library, validated, and
  probe records under `~/.apogee/` — owner-private (`0o700`/`0o600`), versioned, soft on every
  defect, individually deletable.
- **The user gets continuous autosave, a `/sessions` browser** (list · relative time · N msgs,
  resume/delete/rename, workspace toggle), **`--continue`** (most-recent in this workspace) and
  **`--resume <id-or-path>`**, **scrollback replay on resume**, and **interrupted-task
  `/continue`** — closing the P1 "Session management UI" parity gap.
- **Retention/pruning and a cross-instance lock are recorded TODOs**, deliberately out of this
  plan. There is no auto-pruning: manual `^d` only. *Amended:* both have since shipped — retention
  under the `sessions:` block (`max-age`, `max-count`), and the lock as the live-session hold of
  decision 7's 2026-09-19 note (`apogee-3b3`).
- **The bench is untouched.** `session.Store`'s new API stays embeddable and the bench keeps
  composing `Snapshot`/`Encode` directly (ADR 0001) — no bench code depends on the store.

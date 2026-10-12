---
title: ADR 0012 considered options
---

- **Keep ADR 0004 (network-default-deny, both-required, no escape hatch)** — rejected: impractical for
  package-manager workflows (the concrete complaint), and it was what forced the now-deleted in-process
  per-thread landlock contortions.
- **Fully unconfined Auto by default** (apogee-code / `--dangerously-skip-permissions` as the default)
  — rejected *as the default*: it discards the near-free filesystem fence — the one thing that stops
  the worst autonomous-small-model disaster (writing outside the project: `~/.ssh`, sibling repos) —
  for *every* user, to satisfy the VM-user subset. Offered instead as the explicit `confine=false`
  opt-in, so the default floor stays high and VM users say so once.
- **A configurable per-tool × mode disposition matrix as the loosening mechanism** — deferred
  (`ISSUES.md`), post-v1, additive. When built it is **tighten-only**; the only blanket loosen is
  `confine=false`.

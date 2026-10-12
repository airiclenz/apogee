---
title: ADR 0080 consequences
---

- The dangerous-action guard now has one member whose evidence is not in the call but in
  what git would stage — the first **content-derived** rule. The "action text, never payload"
  contract of every other rule is unchanged; `commit-secrets` is the deliberate exception,
  scoped to the one call where the payload is the action. `CONTEXT.md` and the manual name
  it among the Tier-2 members.
- Part B adds nothing to the prompt (ADR 0031): the model meets the rule only through a
  denied call's result text, exactly as it meets every other guard.
- Every `git_commit` outside Plan mode now costs four short git children (rev-parse, add,
  two diffs) on a shadow index before it resolves — bounded by the timeout, side-effect free
  on the repository, and skipped the moment git is unavailable.
- The pattern and glob lists are code, not config. Extending them is a code change with a
  table-test row; a configurable rule set waits on the ADR 0012 merge seam no key calls
  today (apogee-089).
- A secret that matches no built-in class — a bespoke API key, a password in a YAML value —
  passes. That is the precision-over-recall trade every guard rule makes: the floor catches
  the obvious mistake, it is not a scanner.

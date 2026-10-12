---
title: ADR 0080 context
---

A model that has just written a `.env`, dropped a PEM block into a fixture or pasted a token
into a config file will, a few calls later, run `git_commit` over it — and a commit is the
one write whose blast radius outlives the workspace: once pushed, a secret is compromised
whether or not the next commit removes it. Every other guard apogee ships judges a call's
**action text** — the tool, its target paths, its command lines — and never the payload a
write carries (`internal/security/doc.go`, ADR 0012); a commit's payload is exactly what
matters here, and none of the existing rules can see it.

Two obvious mechanisms are closed by decisions already on record:

- **A git hook cannot be the mechanism.** Every git apogee runs goes through
  `internal/gitexec` with `-c core.hooksPath=` (the hardening option that resolves every hook
  to a path no in-workspace write can create), so a pre-commit hook apogee installed would be
  blanked along with the attacker-authored ones it exists to blank. The Tier-1
  `write-git-control-plane` rule refuses any write under `.git/hooks` for the same reason — a
  hook is a program the repository names, and apogee does not execute those. A hook would
  also be a repository-local artefact where the guard is a property of the agent.
- **The verdict must be computed before anything executes.** `resolve()` is hermetically pure
  (`internal/agent/resolution.go`, D6): it does no I/O and reads only the facts dispatch
  precomputed. A secrets check that ran inside the tool, after `git add`, would have already
  written the secret blob into the real object store and staged it in the real index — the
  denied call would leave both behind.

The IDEAS.md entry ("Prevent secrets, certificates, public-keys … from being committed")
asked for a brainstorming grill; the owner ratified the calls below on 2026-09-17.

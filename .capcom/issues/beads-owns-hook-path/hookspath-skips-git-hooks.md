---
title: core.hooksPath points at .beads/hooks, so the repo's own .git/hooks no longer run
status: done
type: task
priority: 1
labels: [tooling]
plan: 2026-09-03 - 02 - deferred-items-sweep-plan
aliases: [apogee-5qe.1]
created: 2026-09-03T18:41:55Z
closed: 2026-09-04T21:28:08Z
reason: Delivered by docs/plans/2026-09-03 - 02 - deferred-items-sweep-plan.md item 1 (the live git-hook path and beads' pre-commit reproduction, recorded in AGENTS.md)
---

- [ ] **`core.hooksPath` now points at `.beads/hooks`, so the repo's own `.git/hooks` no longer
  run.** `bd init` set the path repo-wide, which retires `.git/hooks` wholesale rather than
  layering on it. The commit-msg attribution stripper (AGENTS.md: no AI attribution trailers) was
  copied into `.beads/hooks/commit-msg`, so that behaviour survives — but the repo's hooks and
  beads' are now one path that beads owns, and the next `bd` upgrade that rewrites the directory
  is the thing standing between the stripper and silence. `.git/hooks/commit-msg` is still on disk
  and now dead.

---
title: beads' pre-commit hook sweeps unrelated staged files into the commit
status: done
type: bug
priority: 1
labels: [tooling]
plan: 2026-09-03 - 02 - deferred-items-sweep-plan
aliases: [apogee-5qe.2]
created: 2026-09-03T18:41:56Z
closed: 2026-09-04T21:28:08Z
reason: Delivered by docs/plans/2026-09-03 - 02 - deferred-items-sweep-plan.md item 1 (the live git-hook path and beads' pre-commit reproduction, recorded in AGENTS.md)
---

- [ ] **beads' `pre-commit` hook sweeps unrelated staged files into the commit.**
  `.beads/hooks/pre-commit` delegates to `bd hooks run pre-commit`, which stages tracker state of
  its own; during the retirement wave it swallowed item 17's 22 staged deletions into the `bd init`
  commit, and recovering took a ratified history split. Until that is understood, a commit in this
  repo has to be made with `git commit --no-verify` to be sure it carries only what was staged for
  it.

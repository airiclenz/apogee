---
title: Click the footer's git segment for a git actions panel (push, pull, sync, switch branch...)
status: done
type: feature
priority: 3
plan: 2026-11-05 - 03 - footer-git-actions-plan.md
created: 2026-11-05T08:48:26Z
closed: 2026-11-05T14:59:12Z
reason: "Resolved by plan 2026-11-05 - 03 - footer-git-actions-plan (archived a17b1ed5): git segment click and /git open the git actions picker (fetch, pull, push, sync, switch, rebase) with driven tests and docs."
---

Clicking the footer's git segment (`<branch> NΔ N↓ N↑`, layout.md "The git state") does nothing today: the mode marker is the footer's only clickable control (`handleFooterModeClick`, `internal/tui/mouse.go`). Clicking the branch should open a pop-up panel of git actions on the workspace.

Candidate actions, to be decided in design: push, pull, sync (pull then push), fast-forward, rebase onto upstream, switch branch (a branch picker), fetch now.

## Why

The segment already says the tree is behind or ahead; acting on that means leaving apogee for a shell.

## Open questions

- Which actions are reasonable. Destructive or conflict-prone ones (rebase, switch with a dirty tree, force-push) may be left out or need a confirmation.
- How an action relates to the session's mode and the Approver: is it a user action outside the gate, or does it run through the same gate the model's git calls do?
- What it may do while a Turn is running (refuse, or queue until the engine is quiet).
- Where output and failures land: the transcript, a toast, or the panel itself.
- Refused repositories (config naming a command to run) stay refused, as the git segment already does.
- Keyboard reach for the same panel, not mouse only.
- Which existing pop-up surface it reuses (layout.md, docs/layout/).

## Acceptance

- The action set and the questions above are settled in a plan or ADR.
- Clicking the git segment opens the panel; each action reports its result, and the segment's counts refresh after it.
- Driven tests cover the click, each action's success and failure path, and a refused repository.

---
title: ADR 0108 decision
---

**The footer's git segment and `/git` open a picker of git actions, and each action runs as the
user's own act: outside the Approver and the mode, through the hardened git runner, and only while
the session is idle.**

1. **Six actions, and only these.**
   - **fetch** updates the remote-tracking refs.
   - **pull** runs `--ff-only`, so it never makes a merge commit.
   - **push** is never forced: no `--force`, no `--force-with-lease`.
   - **sync** runs pull, then push, and stops at the first failure.
   - **switch** lists local branches only.
   - **rebase** rebases the branch onto its upstream.

   pull, sync and rebase need an upstream, so they are listed only when the branch has one.
2. **The user's own act, outside the Approver and the mode.** The person at the keyboard chose the
   action. It makes no Approver call, and no mode refuses or gates it, Plan included, just as with
   `/undo`. Nothing model-facing reaches it: no tool offers it, no Reaction fires on it, and its
   result is a transcript note, not part of the model's history.
3. **The hardened runner, and refused stays refused.** Every action runs git through `gitexec`,
   behind an `Options` host interface implemented in `cmd/apogee` (ADR 0054). It therefore carries
   `hardeningOptions`, `hardeningEnv`, the exec fence and the repository command-config refusal.
   A refused repository gets no action, as it gets no git segment. The networked actions (fetch,
   pull, push, sync) carry the footer fetch's transport: no controlling terminal, no prompt,
   batch-mode ssh unless the operator named an ssh command, the command-running transports closed,
   and no submodule recursion. Where that transport cannot run safely, the action is refused with a
   note and is never skipped in silence. An example is an ssh command of the operator's own on
   Windows, where the child cannot be detached from the terminal. A slow push or pull is bounded
   only by a generous backstop (the slow-box rule in `AGENTS.md`).
4. **Idle only, and it holds the engine while it runs.** While a Turn runs or a decision is pending
   (an approval, an `ask_user` question), a click on the segment or `/git` is refused with a note.
   The picker opens only when the session is idle. While an action runs, it is an engine hold: a
   submitted prompt and a second action wait until it lands.
5. **switch and rebase guard the tree.** switch is refused on a dirty tree. rebase asks for
   confirmation first, and requires a clean tree. On a conflict, apogee runs `git rebase --abort`,
   and the note names the conflicting files. A footer act never leaves the repository mid-rebase.
6. **The push upstream rule.** A branch with no upstream is pushed with `push -u origin <branch>`
   when a remote named `origin` exists. Otherwise the push is refused with a note naming the
   remotes.
7. **Results are transcript notes, and the counts are read again.** Each action, success or
   failure, reports one transcript note. A failure's note carries git's own message. The segment's
   counts are then read again.
8. **Reach and surface.** The mouse reaches the actions through the git segment and the keyboard
   through `/git`. Both open the same picker, through two new picker kinds (the action list and the
   branch list) that embed the one list surface (ADR 0053).
9. **The TUI Driver only, and the engine is untouched.** The acts live in the Driver and its host.
   The engine stays wire-silent and no ADR 0031 door closes. `apogee daemon` and `apogee headless`
   offer no git actions.

**Out of scope:** force-push, merge, stash, creating or deleting branches, remote branches in the
switch list, and conflict resolution.

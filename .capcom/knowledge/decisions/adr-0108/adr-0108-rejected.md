---
title: ADR 0108 considered options
---

- **Run the actions through the Approver and the mode, as the model's git runs.** The user would be
  asked to approve their own click, and Plan would refuse their own push. The gate exists to
  supervise the model, and here no model is acting. `/undo` already sets this precedent.
- **Queue an action until the engine is quiet.** A queued push or pull would land at a moment the
  user did not choose, against a tree a Turn may have changed since. A refusal with a note leaves
  the timing to the user.
- **Allow an action while a Turn runs.** A pull, switch or rebase would race the model's own tools
  on the same tree, and the tree the model is reasoning about would change under it mid-Exchange.
- **Report results in a toast.** No toast surface exists. Transcript notes are the existing surface
  for a user verb's outcome (`/undo`), and they stay readable in scrollback.
- **A dedicated git panel.** ADR 0053 makes every pop-up surface embed one list surface. Two new
  picker kinds reuse it instead of growing a second one.
- **Leave a conflicted rebase for the user to resolve.** Conflict resolution is out of scope, and a
  repository left mid-rebase is a broken tree the model would meet on its next Turn. Aborting
  restores the state from before the rebase, and the note says which files conflicted.
- **Let hooks run for the user's own acts (drop `core.hooksPath=`).** The actions would behave
  like git typed in a shell, a git-lfs pre-push upload included. But the same runner would then
  execute hooks that the repository's bytes supply, such as a `.git` shipped with a tarball or a
  single in-workspace write into `.git/hooks/`. That is the threat `hardeningOptions` closes. One
  runner and one hardening for every git apogee runs keeps that seam closed.
- **Force-push, or `--force-with-lease`.** It overwrites remote history and is out of scope.
- **Push to the only remote, or the first one, when there is no `origin`.** That guesses where the
  user's work should go. The note names the remotes instead, and the user decides.
- **A merging or rebasing pull.** `--ff-only` never makes a commit the user did not write. The
  rebase is its own act, which the user confirms.
- **Switch with a dirty tree, carrying the changes as `git switch` does when it can.** The user
  would get a mixed tree they may not expect, and stash is out of scope. switch is refused on a
  dirty tree.

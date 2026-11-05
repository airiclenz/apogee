---
title: ADR 0108 consequences
---

- A new `Options` host interface for the git-actions family (ADR 0054), implemented in
  `cmd/apogee` beside `gitStatusHost` over `gitexec`. `internal/tui` still imports neither
  `gitexec` nor `subprocess` (`TestGitStatusImportsNoGitRunner` holds).
- Two new picker kinds embed the one list surface. The git segment becomes the footer's second
  clickable control beside the mode marker, and `/git` joins the command set.
- **Repository hooks never run on a footer act.** `gitexec`'s `-c core.hooksPath=`
  (`hardeningOptions`) empties every hook, so these acts differ from the user typing the same git
  in a shell:
  - no `pre-push` runs on push or sync, so a git-lfs pre-push upload is skipped and a push of
    LFS-tracked files can reach the remote without their objects;
  - no `pre-rebase` or `post-rewrite` runs on rebase;
  - no `post-merge` runs on pull;
  - no `post-checkout` runs on switch.

  This trade is chosen, not discovered: one hardened runner serves every git apogee runs. A user
  who relies on a hook runs that git in a shell. The operator's own global config still applies
  (`HOME` stays on the allowlist), so a globally installed filter, git-lfs's smudge among them,
  still runs on switch and pull.
- The networked actions never prompt. An ssh key that is not in the agent, or a remote that needs a
  typed password, fails, and git's message lands in the note. The operator's ssh agent and global
  credential helpers keep working.
- The tree moves under the session between Turns, as it does after a shell edit. The next Turn's
  tools read the new tree, and `/undo` treats the paths a pull, switch or rebase changed as edited
  outside the Exchange.
- A slow push or pull on a loaded single-board box holds the engine until it lands. A submitted
  prompt waits for it. No deadline short of the backstop fails it.
- No engine change, no Event, no model-visible change, no bench arm. The glossary gains no term.
- Plan: `2026-11-05 - 03 - footer-git-actions-plan`; issue `footer-git-actions`.

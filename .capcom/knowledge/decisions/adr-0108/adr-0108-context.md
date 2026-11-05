---
title: ADR 0108 context
---

The owner asked for the footer's git segment to become a control (issue `footer-git-actions`,
2026-11-05). The segment (`<branch> NΔ N↓ N↑`, `layout.md` "The git state") already says that the
tree is behind or ahead. Acting on that meant leaving apogee for a shell.

What the code had when the plan was written (2026-11-05, base 5cedb3a3):

- **The segment reads git, and never writes it.** A poll reads `git status` off the frame through
  the `GitStatusHost` interface (`internal/tui/gitstatus.go`). Its host, `gitStatusHost`
  (`cmd/apogee/gitstatus_host.go`), runs git through `internal/gitexec`. A background fetch every
  5 minutes (`ui.git-fetch`) keeps `↓` current. It carries its own transport: no controlling
  terminal, no prompt (`gitNoPromptEnv`), the operator's ssh agent and ssh command passed through
  (`gitSSHPassThrough`), batch-mode ssh where the operator named none (`gitBatchModeSSH`), the
  command-running transports closed (`gitFetchArgs`), and a backstop rather than a budget
  (`gitStatusBackstop`).
- **Every git apogee runs is hardened in one place.** `gitexec` adds `hardeningOptions`
  (`-c core.hooksPath=`, `-c core.fsmonitor=false`) and `hardeningEnv` (`GIT_CONFIG_NOSYSTEM=1`)
  to every call. It also refuses a repository whose own config names a program git would execute,
  on the read paths as much as the write ones (ADR 0012 amendment 0906, ADR 0056 decision 4). The
  segment is absent in a refused repository.
- **The TUI never imports a git runner.** `TestGitStatusImportsNoGitRunner` keeps `gitexec` and
  `subprocess` out of `internal/tui`. A host capability that is a family of acts joins `Options` as
  one named interface (ADR 0054).
- **The footer has one control.** The mode marker is the only clickable footer target
  (`handleFooterModeClick`, `internal/tui/mouse.go`).
- **The surfaces already exist.** Pop-up surfaces embed one list surface, the picker (ADR 0053).
  Results that are not a model's land as transcript notes, the surface `/undo` reports on. No
  toast surface exists.
- **What may hold the engine is a set.** `internal/tui/engineholds.go` names each holder (a running
  worker, the actuation latch of ADR 0029, a session load, a record write, and others), and
  idle-only acts read that set.
- **The model's git is gated.** The git tools run through the Approver, and the mode decides what
  they may do. `/undo` is the precedent for a user verb that changes the tree without it.

The issue left open which actions are reasonable, how an action relates to the mode and the
Approver, what it may do while a Turn runs, where its result lands, and its keyboard reach. The
owner settled the action set, the gate, the busy rule, the rebase and the push rule on 2026-11-05.
The plan's writer settled the rest the same day: results, keyboard reach, the surface, the switch
list, upstream-only actions, the hold while an action runs, and the backstop
(`docs/plans/2026-11-05 - 03 - footer-git-actions-plan.md`).

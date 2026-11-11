---
title: "A git timeout names its first argument, so a snapshot add reads \"git -c: timed out\""
status: done
type: bug
priority: 3
plan: 2026-11-11 - 00 - pdf-parser-fork-and-defects-plan
created: 2026-11-11T05:56:07Z
closed: 2026-11-11T10:11:35Z
reason: "Resolved by item 4: git failure messages name the subcommand past leading global options (plan '2026-11-11 - 00 - pdf-parser-fork-and-defects-plan', archived at 87846d40)"
---

gitexec's timeout message is `fmt.Sprintf("git %s: timed out after %s", args[0], timeout)` (`internal/gitexec/gitexec.go:465`). A call whose args lead with a global option names that option, not the subcommand: the snapshot store's `add` starts with `-c core.excludesFile=…` (`internal/snapshot/store.go`), so its timeout reads "git -c: timed out after …". Observed by the audit-defects run, item 9 (docs/plans/archived/2026-11-10 - 02 - audit-defects-plan.md); pre-existing.

## Why

The message is what a user or the model reads when undo snapshots fail; it should say which git command hung.

## Acceptance

- A timed-out git call whose args begin with -c (or another global option) names its subcommand in the error, pinned by a test in internal/gitexec/gitexec_test.go

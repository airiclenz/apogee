---
title: A guard fixed for one case misses its sibling cases
status: open
type: task
priority: 3
created: 2026-11-27T18:35:35Z
---

The Sep 26 audit named the pattern: a guard built for one case "never extended to sibling
cases". The git option denylist shows it best — `git-remote-program-keys`,
`git-ssh-keycommand-denylist` and the `gitexec`/`shellwrites.go` co-edits each added one key
family after an audit found it. The read cache and the hard-refuse guard (an MCP argument rename)
went the same way.

Wanted, for each guard that refuses by a list (git config keys and options that run a program,
hard-refuse argument names, read-cache invalidation triggers, path spellings):
- the list lives in one declared table, which the guard reads;
- a test enumerates the whole family from an outside source — for git, every config key the
  installed `git help --config` reports that names a program or command — and fails when a
  member is not in the table, instead of a test that pins the one key from the bug report.

Survey first: list the list-shaped guards and say for each whether a family enumeration exists.

## Why

A family test catches the sibling before an audit does; a single-case test only proves the fix
from the last report.

## Acceptance

- A survey of the list-shaped guards is recorded in this issue's log.
- Each guard the survey names reads one declared table, and has a test that checks the table against its whole family.
- The git test fails if a git version adds a program-running config key the table lacks (or skips with a clear message when `git` is absent).

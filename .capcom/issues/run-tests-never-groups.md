---
title: run_tests with no arguments never joins the grouped Tools block (empty target)
status: open
type: bug
priority: 3
plan: 2026-11-12 - 01 - session-log-wrap-and-tool-grouping-plan.md
created: 2026-11-12T18:48:26Z
---

A `run_tests` call made with no arguments — the normal "test the whole project" call — never joins the grouped **Tools** block and is not counted in it. It gets its own full block, which breaks a run of grouped calls. Same class as git-status-never-groups (owner report, 2026-11-12).

## Cause

- `groupable` (`internal/tui/toolbranch.go`) admits a call only when its Target is non-empty.
- `runTestsTarget` (`internal/tui/toolregistry.go`) builds the target from the two OPTIONAL arguments, `path` and `filter` (`internal/tools/run_tests.go` schema). With neither, it returns `""`. A call with a path or a filter does group, so the same tool groups or not depending on its arguments.

## Wanted

`run_tests` always has a target. With no `path`, the target names the whole project (e.g. `whole project`, or `.`), still qualified by `· <filter>` when a filter is given. The tool's own default is "the whole project", so the target says the same.

## Acceptance

- `run_tests` with no arguments renders as a member row of a Tools umbrella among other calls, and the umbrella counts it.
- `run_tests` with only a `filter` shows the whole-project word qualified by the filter.
- A table test on `runTestsTarget` covers no args, path only, filter only, both.

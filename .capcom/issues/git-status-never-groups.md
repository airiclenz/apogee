---
title: git_status never joins the grouped Tools block (no target)
status: done
type: bug
priority: 3
plan: 2026-11-12 - 01 - session-log-wrap-and-tool-grouping-plan.md
created: 2026-11-12T18:46:27Z
closed: 2026-11-12T20:22:36Z
reason: "Resolved by plan '2026-11-12 - 01 - session-log-wrap-and-tool-grouping-plan' item 3 (archived at df06255f): git_status targets 'working tree' and groups/counts in the Tools umbrella."
---

A `git_status` call never joins the grouped **Tools** block. Its neighbours fold into one umbrella with one row per call, and the umbrella counts them; `git_status` breaks the run with its own full block, and is not counted with them. To the user it looks like a special tool, but it is an ordinary read-only call (owner report, 2026-11-12).

## Cause

- `groupable` (`internal/tui/toolbranch.go`) admits a call only when it has a Target: `tv.Target != "" && !tv.solo`. A member row of a group is led by its target, and a targetless block uses its detail lines as its branches instead.
- The `git_status` row in `toolRegistry` (`internal/tui/toolregistry.go`) has no `target` extractor, on purpose: "the tool takes no arguments — the repository IS the target." So its Target is always empty, and it can never group.
- The only other targetless row is `task_list`, which has its own card and fold, so `git_status` is the one ordinary tool this hits.

## Wanted

`git_status` groups and counts like any other read-only tool call.

Design points:

- Give it a target that holds before the result lands, because the row is drawn while the call runs: the workspace directory name, or a fixed word such as `working tree`. The branch name is only known from the result, so it can only be added after that, if at all.
- Its stat (`changedFilesStat`) and body (`gitStatusBody`) stay as they are: as a member row it shows the leader row and keeps the report behind its own indicator (`renderGroupMember`), like a Terminal call with output.
- Check whether any other tool can return an empty Target in practice (a tool whose target comes from an optional argument) and so drop out of grouping the same way; such a tool needs a fallback target too.
- Update `docs/layout/tool-layout.md` if it names the targetless shape for `git_status`.

## Acceptance

- A run of read_file, grep, git_status and git_log calls renders as one Tools umbrella with four member rows, and its count includes the `git_status` call.
- An expanded `git_status` member still shows the branch and the staged / unstaged / untracked report.
- A driven TUI test pins `git_status` inside a group, collapsed and expanded.

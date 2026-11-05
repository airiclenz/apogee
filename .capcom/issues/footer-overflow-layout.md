---
title: "Footer overflows with long paths or branch names: configurable row layout"
status: open
type: feature
priority: 3
plan: 2026-11-05 - 02 - footer-rows-and-colour-plan.md
created: 2026-11-05T08:48:26Z
---

The footer is one row: `host ✦ model ✦ effort ✦ workdir ✦ git ✦ spend` plus the mode marker. With a long workdir path or a long branch name the run does not fit, and the priority ladder (`internal/tui/footerfit.go`) gives segments up whole: spend, then git, then effort, then workdir, then host. On a normal-width window a deep path or a long branch can push out the git state or the effort word that would otherwise fit.

## Options to weigh

1. **Wrap on overflow:** when the row does not fit, flow the remaining segments onto a second row instead of dropping them.
2. **Fixed two-row split:** row 1 states where the session points upstream (host, model, effort, mode); row 2 states the workspace (workdir, git, spend). This is the shape of the owner's Claude Code status line script.
3. **User layout config:** a `ui:` key that names the rows and the segment order, with a default, like llama-launcher's status layout config.
4. **Shorten before dropping:** middle-elide the workdir (`~/r/a/…/pkg`) and the branch before the ladder gives anything up.
5. **A mix:** a default two-row layout, wrap-or-shorten as the fallback, and config to override.

## Constraints

- The mode marker is never given up (footerfit.go); any layout keeps that.
- A second row costs a transcript row on every frame; layout.md's chrome rows and their mirrors must follow.
- The painter and the click handler read one layout (footerfit.go); a second row must keep that single source, including the git segment's click target (`footer-git-actions`).
- Config keys need load-time refusal of bad values (config-refusal parity) and a docs/manual/configuration.md entry.
- Related: `status-line-priority-fit` (the status line above the box, parked).

## Decision (2026-11-05, owner)

Option 3, with option 2 as its default and option 4 always on:

- A `ui:` footer key names the rows and the segment order in each, e.g. `rows: [[host, model, effort, mode], [workdir, git, spend]]`.
- The default is that two-row split. A one-row setting keeps today's shape for anyone who wants it.
- Each row keeps the priority fit (footerfit.go). The workdir and the branch are middle-elided before any segment is dropped.
- Still open for the plan: the exact key name and schema, whether `mode` and `offline` can move or stay pinned, how bad values are refused at load, and how per-segment colour (`footer-segment-colour`) attaches to the same key.

## Acceptance

- A plan for this decision, with layout.md and docs/manual/configuration.md updated.
- With a long workdir and a long branch on an 80-column window, the git state and the effort word are still readable.
- Golden or driven tests cover narrow, normal and wide windows.

## Log

- 2026-11-05 Airic Lenz: Owner chose option 3 (configurable rows, default two-row split, middle-elide before dropping) on 2026-11-05.

---
title: Watching a live session, the footer shows apogee's own folder and branch, not the watched session's
status: open
type: bug
priority: 2
labels: [tui]
created: 2026-11-05T15:08:07Z
---

While watching a live session read-only, the footer status bar still shows the folder and git branch of the workspace apogee was started in. It should show the folder and branch the watched session is running in.

It should also mark that the displayed folder is not the workspace this apogee was started in (for example a distinct tone or a 'watching' marker on the folder segment), so the user cannot mistake it for their own workspace — the footer git actions picker acts on the host workspace, so the distinction matters.

Code to start from: `internal/tui/footerfit.go` (footer segments, git segment), the watch path in `internal/tui/runview.go` / `model.go`. Related: commit 055f11fa (route a landed footer git action while watching another session).

## Why

The footer is the user's at-a-glance answer to 'where is this running'; while watching, it currently answers wrongly.

## Acceptance

- While watching a live session, the footer's folder and branch segments show the watched session's workspace and branch
- The folder segment visibly marks that it is not apogee's own workspace while watching, and returns to normal on leaving the watch
- Footer git actions while watching keep acting on the right workspace (no regression of 055f11fa), and the footer layout spec documents the watch state
- A driven TUI test covers the watch-state footer

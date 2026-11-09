---
title: /mcp is missing from the report-pane order and scroll tests
status: done
type: task
priority: 3
labels: [test-gap]
plan: 2026-11-09 - 00 - mcp-test-gaps-and-session-death-plan
created: 2026-11-09T09:10:45Z
closed: 2026-11-09T12:25:04Z
reason: "Resolved by plan '2026-11-09 - 00 - mcp-test-gaps-and-session-death-plan' item 1 (archived at 556778e7): reportCases gains a scrolling /mcp case; frame-order test renamed TestTheFiveReportsPaintInTheFramePaneOrder and stacks all five."
---

Item 18 of `2026-11-08 - 03 - adr-0111-navigation-and-mcp-status-plan` (commit `f5343420`) adds `/mcp` as a fifth report pane. The shared report-pane tests still cover only the original four reports: `reportCases` (`internal/tui/reportpane_test.go:32`) lists `/usage`, `/inspect`, `/thinking` and `/advice`, and the tests that range over it (`internal/tui/reportpane_test.go:139`, `:213`, `:240`, `:687`, `:711`) never open `/mcp`. `TestTheFourReportsPaintInTheFramePaneOrder` (`internal/tui/panes_test.go:79`) stacks only those four, so the paint order with `/mcp` open beside them is not tested.

## Why

The `/mcp` pane's scroll, clamp and frame-order behaviour is shared with the other reports, but nothing tests it there. A regression in how `/mcp` stacks or scrolls would pass the suite.

## Acceptance

- `reportCases` includes a `/mcp` case with enough servers to scroll, so every test that ranges over it covers `/mcp`.
- The frame-order test stacks all five reports, `/mcp` included, and is renamed to match.

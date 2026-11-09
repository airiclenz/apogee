---
title: /mcp is missing from the report-pane order and scroll tests
status: open
type: task
priority: 3
labels: [test-gap]
created: 2026-11-09T09:10:45Z
---

Item 18 of `2026-11-08 - 03 - adr-0111-navigation-and-mcp-status-plan` (commit `f5343420`) adds `/mcp` as a fifth report pane. The shared report-pane tests still cover only the original four reports: `reportCases` (`internal/tui/reportpane_test.go:32`) lists `/usage`, `/inspect`, `/thinking` and `/advice`, and the tests that range over it (`internal/tui/reportpane_test.go:139`, `:213`, `:240`, `:687`, `:711`) never open `/mcp`. `TestTheFourReportsPaintInTheFramePaneOrder` (`internal/tui/panes_test.go:79`) stacks only those four, so the paint order with `/mcp` open beside them is not tested.

## Why

The `/mcp` pane's scroll, clamp and frame-order behaviour is shared with the other reports, but nothing tests it there. A regression in how `/mcp` stacks or scrolls would pass the suite.

## Acceptance

- `reportCases` includes a `/mcp` case with enough servers to scroll, so every test that ranges over it covers `/mcp`.
- The frame-order test stacks all five reports, `/mcp` included, and is renamed to match.

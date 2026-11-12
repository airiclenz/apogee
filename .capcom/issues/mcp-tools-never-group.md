---
title: MCP and other unregistered tools never join the grouped Tools block (no target)
status: open
type: bug
priority: 3
plan: 2026-11-12 - 01 - session-log-wrap-and-tool-grouping-plan.md
created: 2026-11-12T18:48:26Z
---

A call to an MCP tool (qualified name `<server>__<tool>`, `internal/mcp/tool.go`) never joins the grouped **Tools** block and is not counted in it. The same holds for any other tool with no row in `toolRegistry`. Each such call gets its own full block, with its arguments listed as branches (owner report, 2026-11-12).

## Cause

- `groupable` (`internal/tui/toolbranch.go`) admits a call only when its Target is non-empty.
- `presentToolCall` (`internal/tui/toolview.go`) handles a tool with no registry row by giving it a label (the raw tool name), a verb and `argumentDetails` as its body — but never a Target. So an unregistered tool is targetless by construction and can never group.

## Wanted

An unregistered tool's call groups like any other. It gets a generic target built from its arguments — for example the first string argument's first line, or a short `key=value` summary of the arguments, bounded — with the full argument list kept as its body behind its own indicator. For an MCP tool, the label may also split the qualified name into server and tool (`<server> · <tool>`), so the row reads well; the raw name must stay the key the rest of the TUI matches on.

A call with no arguments at all still needs a target: fall back to the tool name, or a fixed word, so the shape never depends on the argument list.

## Acceptance

- Two calls to one MCP tool, between a read_file and a grep, render as one Tools umbrella with four member rows, and the count includes both.
- An MCP call with no arguments still groups.
- Expanding an MCP member shows its arguments as before.
- A driven TUI test pins an unregistered tool inside a group, collapsed and expanded.

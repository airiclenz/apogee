---
title: "A process run's ending travels typed (architecture review 2026-10-14 #7)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:22Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#7), re-verified 2026-11-05 at f7ec72b6. A subprocess's typed ending is still written as prose ("[exit code N]", the Console status line) by subprocessToolResult (internal/tools/terminal.go) and the console tools, then regexed back by two readers with different rules: exitCodeMarker and consoleStatusMarker in internal/tui/toolregistry.go (anchored at the end) and exitCodeMarker in internal/agent/recipe.go, read by scriptOutput (last match, so a Reaction that rewrites Content changes a recipe's code). The review proposes a ProcessEnding variant on the sealed domain.ToolSummary (code, timed out, denial, alive / killed), set where the prose is written, which the TUI slot and scriptOutput read instead; ToolSummary has no wire form, so ADR 0083 §1 is untouched. Its symptom (a confined failure losing its exit code) was fixed by item 1 of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.

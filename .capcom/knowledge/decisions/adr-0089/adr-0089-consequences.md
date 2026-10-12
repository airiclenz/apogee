---
title: ADR 0089 consequences
---

- The TUI gains a `/workflows` view and a status-line indicator for background workflows. The
  headless NDJSON stream gains workflow events.
- The bench judges the background switch and the `workflow` tool per model class, separately from
  `fan_out` itself.
- CONTEXT.md gains **Background workflow**. **Exchange** gains the wake as a second way one opens.

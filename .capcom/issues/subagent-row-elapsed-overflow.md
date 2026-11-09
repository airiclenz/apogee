---
title: Sub-agent row columns break once elapsed passes an hour
status: open
type: bug
priority: 3
created: 2026-11-08T21:33:39Z
---

The side panel's sub-agent row formats its facts as `%-7s %6s %4s`
(`subAgentFactsFormat`, `internal/tui/sidepanel.go`). Elapsed past an hour renders as
`HH:MM:SSs` (9 characters, `internal/tui/activity.go`), spilling past its 6-column field and
pushing the fill column out of line. Found in the ADR 0111 code survey, 2026-11-08.

## Acceptance

- A sub-agent row with 1h 2m 3s elapsed keeps every column aligned with a row under a minute, at the default and the minimum panel width.

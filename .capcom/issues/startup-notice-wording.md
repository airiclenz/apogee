---
title: Shorten the startup notices (residual banner, mandatory-reasoning note)
status: done
type: task
priority: 2
created: 2026-11-12T08:19:33Z
closed: 2026-11-12T08:23:56Z
reason: residual banner one line per token; mandatory-reasoning note shortened; probe/headless tests pin the wording
---

The notes under the start-up box are long. The Auto residual banner (`probe.ResidualNotice`) takes 3 wrapped lines per residual and shows on every Linux and macOS start in Auto (ADR 0112). The mandatory-reasoning note (`mandatoryEffortNote`) is one long sentence. Shorten both to one line per fact while keeping what ADR 0112 decision 11 requires: each write-class residual named with its consequence.

## Why

The start-up screen reads as overwhelming (owner screenshot, 2026-11-12).

## Acceptance

- The residual banner is a head line plus one line per residual, each naming the token and what a confined command can still do through it.
- The mandatory-reasoning note is shorter and still names the model and the compaction/title cost.
- probe, headless and tui tests pin the new wording; docs that quote it follow.

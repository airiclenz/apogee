---
title: Decide whether Sub-agent, Skill groups and lone tool blocks auto-collapse when settled
status: open
type: feature
priority: 3
deps: [settled-tool-group-clutter]
created: 2026-11-18T11:30:56Z
---

`settled-tool-group-clutter` auto-collapses settled, untouched `✦ Tools` umbrellas to one Group summary row. On purpose, it leaves out `✦ Sub-agents (N)` and `✦ Skill (N)` groups (spec `docs/layout/tool-layout.md` §Grouped Sub-agents, §Grouped Skills) and lone tool blocks, which collapsed are still a header plus one row (§Vocabulary, *Remainder*). This issue decides, case by case, whether each of those follows the same rule under `ui.tools-auto-collapse`, and what its one row reads. A sub-agent row already carries a gist and verdict, so its group may need a different summary.

## Why

This was scoped out of the first cut to keep that change small (owner, 2026-11-18). The transcript stays only partly decluttered until these are decided.

## Acceptance

- A decision recorded for each of the three surfaces (collapse or not, and what the one row says), then implemented or declined with the reason.

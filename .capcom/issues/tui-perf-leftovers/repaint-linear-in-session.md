---
title: Repaints stay linear in session length
status: open
type: task
priority: 3
created: 2026-11-27T18:35:35Z
---

An all-hit repaint is ~7.6 ms and a cold repaint ~111 ms at 900 blocks (results doc §What stays
open). Lazy, visible-first painting is the structural remedy; plan 08 kept it out by design.
Needs design work (likely an ADR amendment to the paint cache) before a plan.

## Acceptance

- A cold repaint costs in proportion to the visible rows, not the session length; golden frames and the nil-cache equality tests still pass.

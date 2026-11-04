---
title: "delegate-max-depth: 0 and negative count keys silently resolve to defaults"
status: open
type: bug
priority: 2
plan: 2026-11-04 - 00 - config-refusal-parity-plan.md
created: 2026-11-04T20:46:43Z
---

Seqcom docs-a7-v0310 (apogee v0.31.0). `delegate-max-depth` is a plain `WholeCount` clamped by `atLeast(1, …)`, so 0 resolves to 1 with no notice; the manual and template say it must be at least 1. Negative `delegate-max-steps`, `delegate-fanout-rounds`, `delegate-max-tokens`, `workflow-retries`, `workflow-continuations` and `working-window` silently become defaults the same way.

## Why

A value the docs call impossible loads without complaint.

## Acceptance

- delegate-max-depth below 1, and a negative value for the six sibling keys, are refused at load naming the key
- Each key's meaning for 0 is unchanged

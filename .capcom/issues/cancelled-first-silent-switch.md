---
title: Sub-agents fallover goes unannounced when the First caller is cancelled
status: open
type: bug
priority: 3
plan: 2026-11-02 - 01 - fallback-followups-plan
created: 2026-11-02T14:46:59Z
---

fallbackResolver.Resolve hands First only to the caller that started the run, and Agent.announcesRehome emits the Sub-agents-seat UpstreamSwitchedEvent only for First. If that caller's ctx ends during the grace wait (or before it rehomes), its siblings still move with First false and no event is emitted: no TUI note, no headless stderr line, no daemon log line, nothing in the journal. Found in item 11 NOTES of plan 2026-11-02 - 00 - midrun-model-fallback-plan.

## Why

A seat moves to another server with no trace for the user.

## Acceptance

- With the starting routed sibling cancelled mid-grace, the first sibling that applies the switch emits exactly one upstream_switched event, pinned by a test

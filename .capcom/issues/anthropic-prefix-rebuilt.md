---
title: Per-request system-prompt rebuild and tail notes edit the prefix preserved thinking was signed over
status: done
type: bug
priority: 2
aliases: [apogee-anthropic-prefix-rebuilt-per-request]
created: 2026-10-02T16:24:22Z
closed: 2026-10-02T22:20:34Z
reason: "Resolved by plan 2026-10-02 - 02 - refocus-findings-plan, archived at 4fa71d90 (item 5: prefix-digest guard per ADR 0092)"
---

Each request rebuilds the top-level system prompt (buildRequest) and injects per-request text into earlier turns: NoteOnTail on the closing tool result, AppendToSystem directives (internal/agent/loop.go ~1100-1126). model-migration 'Breaking change 3' names exactly these edits ('injecting per-request text into an earlier turn… that you remove or rebuild', 'rebuilding the top-level system prompt') as a 400 for accounts created on or after 2026-08-31 once signed thinking is replayed. Unconfirmed: whether compaction's fold (internal/agent/compact.go) replays kept tail turns with their thinking and so hits the same rule. Design needed: keep the replayed prefix byte-stable (append-only injections, stable system prompt) or drop thinking when the prefix changed.

## Acceptance

Design recorded (ADR amendment or plan); a request that replays signed thinking sends a prefix byte-identical to the one the thinking was produced on, pinned by an agent-level test; compaction behaviour decided and tested.

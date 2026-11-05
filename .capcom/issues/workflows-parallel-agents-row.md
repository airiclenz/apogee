---
title: workflows.md keys table lists parallel-agents as a top-level key; it is per server entry
status: done
type: bug
priority: 2
created: 2026-11-05T20:39:54Z
closed: 2026-11-05T20:40:15Z
reason: docs/manual/workflows.md "The keys, in one place" row now names a server entry's `parallel-agents:`, set under that entry in `servers:`, not at the top level.
---

Seqcom docs-a10-v0310 (apogee v0.31.0). docs/manual/workflows.md, "The keys, in one place", lists `parallel-agents:` beside the top-level `workflow-` keys. It is a key of a `servers:` entry; at the top level the reader ignores it as an unknown key, so a user following the table loses their width setting.

## Why

A user who copies the table writes a key that is silently ignored.

## Acceptance

- The row names `parallel-agents:` as a server entry key, set under that entry in `servers:`, not at the top level

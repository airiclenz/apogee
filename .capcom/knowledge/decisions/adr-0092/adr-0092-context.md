---
title: ADR 0092 context
---

On `wire: anthropic` apogee keeps a reply's signed reasoning blocks (`thinking` with its
`signature`, `redacted_thinking` with its `data`) verbatim on the committed assistant message and
sends them back in their original places on later requests to the same model
(ADR 0078, Amendment
2026-10-02; `internal/agent/wire.go` `replayedThinking`).

A signature does not cover only its own block. It binds the request the reply was produced on:
the top-level `system`, the `tools`, and every message before the reply. The models that preserve
thinking across turns check that binding, and a replayed block whose prefix has changed since it
was signed is refused with a 400.

apogee rebuilds that prefix for every request, and it is not stable. The system prompt carries
standing blocks that change mid-session (`internal/agent/standingblocks.go`): the task-list
block after a `task_list` call, and the orientation block. A mode change filters the tool menu.
Tail notes (`Request.NoteOnTail`) ride the newest message of one request and are gone from it in
the next. The pre-request seam rewrites earlier messages in the projected request: `floor.CapToolResults` (`internal/floor/resultcap.go`) elides old tool results
once the window fills, pruning replaces an old result with a stub, and a cancelled Exchange's last
tool result carries the engine's cancel note (`internal/agent/turn.go` `turnLifecycle.settle`). A
session record never stores advice (`internal/domain/advice.go` `Message.recordContent`), so a
resumed conversation is not byte-for-byte the one the replies were produced on. Any of these, once
a reply carries signed thinking, made the next request a 400 (bead
`apogee-anthropic-prefix-rebuilt-per-request`).

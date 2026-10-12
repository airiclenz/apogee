---
title: ADR 0051 context
---

`ISSUES.md` carried "**[P2] Undo all agent changes**" from the predecessor's feature sweep: the
human can watch the agent rewrite twenty files and has no way back except the editor's own undo,
file by file, or a `git checkout` that also throws away everything they wrote themselves. The
entry came with a claimed oracle — apogee-code, the VS Code extension, whose TDD describes the
feature at `Apogee-Code-TDD.md:164`. Verified against that source repo at v0.2.58 and at head,
the claim is stale: no implementation exists there. There is no behavioural precedent to port,
so every property below is a decision rather than a match.

Two facts about the codebase bound the design before any preference does. First, the writes
already funnel: every mutation a first-party tool performs passes a funnel in
`internal/tools/path_safety.go` — `safeWriteFile` for the content verbs, `journaledMutation` for
the byte-moving trio (see the 2026-08-24 amendment; since 2026-09-16 the pair is the write-side
value's own `write` and `journaled`, in `write_target.go`) — into `internal/security`'s fenced
primitives, and the tools that do it carry the unexported `workspaceScopedWriter` marker
(`internal/tools/workspace_scoped.go`) that dispatch uses to path-bound rather than confine them.
Second, only filesystem writes are durable and reconstructible at all — ADR
0008 settled that terminal, network
and subprocess effects are neither forkable nor undoable, and nothing here reopens it.

The owner resolved the open design questions on 2026-08-19 by question round; the plan author
resolved the rest against the sources above. This record ratifies both sets. It supersedes
nothing.

---
title: ADR 0049 context
---

The confinement execution contract's §4 ladder table has promised since P3.1 that a
workspace-scoped write whose target lies **outside** the workspace is *gated* in Ask-Before,
Allow-Edits and Auto — the human is asked, and "Allow" runs the write. Half of that promise
landed: dispatch classifies the target with `resolveTargetUnbounded`
(`internal/tools/workspace_scoped.go`), so the call reaches the Gate and the approval pane shows
the resolved path. The other half never did: every write tool executes through an `os.Root` fence
pinned at the workspace root (`safeWriteFile` → `security.SafeWriteFile`), which refuses the
escape whatever the verdict was. The human deliberates, approves, and receives an error result —
a gate whose "Allow" cannot succeed. The same fence also nullifies the **Auto ·
`confine=false`** cell, where the table says the write plainly *runs*: the VM opt-in whose whole
meaning is "the VM is the box" still carried a fifth, undocumented fence for native writes only.

The gap was flagged in the contract ("Realisation gap — half-landed"), tracked as the open
`ISSUES.md` defect "an *approved* out-of-workspace write still errors at Execute", and named an
owner call: land the reconciliation the contract promises, or ratify strict fencing as permanent
and amend the row. This record ratifies the calls the owner made on 2026-08-14 (grill session,
each branch resolved by AskUserQuestion). It supersedes nothing; it completes ADR 0012's ladder
semantics for the WS-write class.

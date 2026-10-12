---
title: "Undo journal: the per-Exchange record of what writes replaced, which `/undo` restores"
---

**Undo journal**:
The engine's per-Exchange record of what the agent's writes **replaced**,
and what `/undo` puts back — one Exchange per step, most recent first, previewed before it applies,
`/redo` beside it. Two capture paths feed it: a **pair** of workspace-tree snapshots, one before an
Exchange's first write-capable tool call and one at its close, into a git object database the
session owns under `~/.apogee/`, outside the workspace; and the shared write funnel (the
write-side value's `write` and `journaled` methods in `internal/tools`, through which every
first-party writer lands its bytes and takes its capture — ADR 0051), whose **pre-image**
wins wherever both see a path. Coverage is therefore the workspace tree, `terminal`, `python_exec`,
MCP and git-checkout writes included; without git only the funnel remains. A revert touches only
the paths that Exchange changed, and one whose content no longer matches what the agent left is
**skipped and reported**: the human's own later edit outranks the undo. Groups materialize
**lazily**, an Interjection opens none, and a sub-agent writes into its
parent's. It is **persisted host state, never Session state** (ADR 0022 §8):
kept per session id, reopened under the id `/new`, `/clear` or a resume puts in force. See
ADR 0074.
_Avoid_: "undo history" (it is a stack of Exchange groups, not a per-keystroke history), "undo
stack" for the record itself (the stack is how `/undo` walks it; the journal is what is written),
"rollback" (that word belongs to the abort-rollback boundary in Exchange).

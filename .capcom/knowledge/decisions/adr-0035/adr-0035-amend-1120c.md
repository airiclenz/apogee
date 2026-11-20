---
title: "ADR 0035 amendment of 2026-11-20 (c): every tool call is a tool group"
refs: [docs/layout/tool-layout.md, internal/tui/transcript.go, internal/tui/toolblock.go, internal/tui/render.go]
---

**Why now.** `adr-0035-amend-1120` (b) gave a lone groupable call the auto-collapse fold and no
other: once the reader opened it, it stayed open for the session and grew no header toggle, so a
call opened to be read could never be folded away again (issue `lone-call-no-refold`). A lone
delegation and a lone skill fetch had no fold at all, while two of either folded from their group
header. One call read as a different kind of block from two calls of the same tool. The owner
asked for one shape everywhere (owner decisions 2026-11-20, plan
`docs/plans/2026-11-20 - 00 - tool-group-of-one-plan.md`).

**What this supersedes.**

- `adr-0035-amend-1120` (a)'s "lone groupable tool call" as a shape of its own, and its list of
  blocks never auto-collapsed where it names "a lone skill fetch, a lone delegation run block".
- `adr-0035-amend-1120` (b) as a whole: a lone call is no longer a block with only the settled fold.
- `adr-0035-amend-1120b` (b)'s second case, "the reader had opened the closing member", together
  with its "The umbrella stays open until it settles by the earlier rule". The touched-group-grows
  rule in (c) below replaces it.
- `adr-0035-amend-1120b`'s closing sentences on a lone delegation or a lone skill fetch, which said
  such a call is never auto-collapsed and that the group the next Turn forms beside it stays open.

Everything else in `adr-0035-amend-1118`, `-1120` and `-1120b` stands as written: what settled and
touched mean, the Turn-boundary settle, the wait on the user that opens a block, the click on an
auto-collapsed block that opens it alone and writes nothing, the settings keys and their defaults.

**(a) Every tool call is a tool group (owner).** A groupable call standing alone is a **group of
one**: a `✦ Tools` umbrella of that one call, headed by its Group summary (`✦ Read 1 file ⋯ ▼`,
`✦ Ran 1 command ⋯ ▼`). A lone delegation is a `✦ Sub-agents (1)` group and a lone skill fetch a
`✦ Skill (1)` group, each with its one member row. A group of one folds, opens and re-folds from
its header by the same rules as a larger group, live or settled, auto-collapse on or off. Three
things stay out of the group, because they have no tool block to fold or no phrase to summarise:
a task-list card (its own `ui.task-list-open` fold), an answered `ask_user` record, and a call with
no target. A workflow block stays out as before.

**(b) The open group of one paints the call's own block (owner).** Open, a group of one is its
group header and, directly beneath it, the block the call painted standing alone: its `✦` label
header, its target row and, opened, its body, diff or output. It has no type row, because one call
has no run to list. Folded, it is the header line alone under a `▶`. A Sub-agents or Skill group
of one paints its one member row, as any member of a larger group paints. When the next Turn's call
joins it, the group is a group of two and paints in that shape: type rows for an umbrella, a second
member row for a Sub-agents or Skill group.

**(c) A touched group stays open as it grows (owner).** With auto-collapse off, a group of one
starts open and folds on its own session-only flag. With it on, a settled, untouched group of one
folds to its header, and the Turn boundary keeps it folded while the next Turn's call joins it.
A group the reader has opened stays as the reader left it, open, when the next Turn's call grows
it. It is never auto-folded again; the reader folds it with a click or `enter` on its header. The
touch belongs to the group's head, and a group of one and the group it grows into share that head,
so the touch carries.

**(d) The Group summary paints in the tools header colour (owner).** On a group header, the Group
summary's phrases, any clipped remainder of them and the base of the diffstat counts paint bold in
the scheme's `tool-header` role, the tone of a tool's label. The `✦`, the dot leader, the `▶`/`▼`
and the red outcome counts keep their own styles; `th.toolHeader` itself is not restyled.

All of it is TUI view state and paint. No engine, `domain.ToolSummary`, config-key or
session-record change is made, so the engine stays Driver-neutral (ADR 0031) and nothing reaches
the model. Spec: `docs/layout/tool-layout.md`, "Vocabulary", "Fold states and interaction" and the
group-of-one sketches.

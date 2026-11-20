---
title: "Group summary: the plain-words header of a Tools umbrella"
refs: [docs/layout/tool-layout.md, internal/tui/groupsummary.go]
---

**Group summary**:
The one-line **header of a `✦ Tools` umbrella**, saying in plain words what the umbrella's calls
did: `✦ Read 3 files, ran 4 commands, edited 2 files +18 −5 · 1 failed`. Every umbrella wears it,
open, folded, running or settled — a **group of one** included, the umbrella a groupable call
standing alone forms, whose summary is that one call's (`✦ Read 1 file`, `✦ Ran 1 command`;
every tool call is a tool group, `adr-0035-amend-1120c`) — and it is the whole row an umbrella
folds to, from a click on it, or on its own once it is settled and untouched under
`ui.tools-auto-collapse` (`adr-0035-amend-1118`; settled from the moment the next Turn of its run
starts, `adr-0035-amend-1120b`). A Sub-agent or Skill group, which folds the same ways, folds to
its own `✦ Sub-agents (N)` / `✦ Skill (N)` header, not to a Group summary; a delegation or skill
fetch standing alone is such a group of one, `(1)`. The phrases, any clipped remainder of them and
the base of the diffstat counts paint bold in the tools header colour (the scheme's `tool-header`
role, a tool label's tone); the `✦`, the dot leader, the fold glyph and the outcome counts keep
their own styles. It reads, in order: **verb phrases**, one per kind of work in the order each kind
first ran, a phrase being what a tool's presenter row names (`read_file` "read N files"; `grep` and `find_files` one "searched N patterns"; the four writing
tools one "edited N files", never "created"; the git readers "checked git"), counting the distinct
paths where the phrase says so (three reads of one file read 1 file), and taking its live verb
(`Reading 2 files`) while one of its calls is in flight; a tool with no phrase, an MCP tool among
them, reads `<Label> ×N`. Then the **diffstat** summed over the diffed edits, and last the
**outcome**: `· N failed` and `· N denied` in the failure red, a refused call counting as denied.
Where the row is too narrow the detail goes in a fixed order (phrases after the first two become
"and K more", then the diffstat, then the head falls back to `Tools (N calls)`, then it is
clipped), the outcome counts last of all.
It is **built in the TUI** from each member's view and its **Tool summary** (`tool-summary`), the
structured facts already on the result: display data a host derives, never sent to the model,
never written to the session record, and no engine or `domain.ToolSummary` field of its own, so
the engine stays Driver-neutral (ADR 0031). Spec: `docs/layout/tool-layout.md`, Vocabulary.
Not to be confused with the **Tool summary** of one call, which is its input, nor with a type
row's aggregate outcome slot.
_Avoid_: "tools breakdown" (the retired `Tools (3 Read, 4 Terminal)` header), "group stats".

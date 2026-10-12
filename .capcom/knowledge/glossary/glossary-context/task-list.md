---
title: "Task list: the model's own checklist, re-rendered into the standing system content"
---

**Task list**:
The model's **own** checklist — the rows it wrote about its own work — held by the engine as
Session state and re-rendered into the standing system content on every
request, so a decomposition survives **Compaction** and a `--resume`. The `task_list` tool is its
**only** writer: the engine never appends a row, no `/command` edits one, and no
Reaction injects one — which is what keeps it a tool rather than a
Reaction steering the model's plan (the retired guided decomposition — see
Retired terms). One call carries the **complete** list and **replaces** it: the array of
`{text, done}` it is given becomes the list, so ticking a row off is resending it with
`done: true`, clearing it is sending `[]`, and there are **no item ids** to mint or remember. It
renders as the **last of the engine's standing blocks**, after the **Delegate report block** and
ahead of the **Context files** — model-authored text behind every host-authored one — and it
**rides along** under the Orientation block's rule, an empty list rendering nothing;
`tasklist.Fence` fences a context line that spells its opening, exactly as a forged orientation
header is fenced. It is the first engine-composed standing part that is **not** a per-session
constant: every call invalidates the upstream prefix cache from the block onward, a re-encode the
model chose to spend. `IsReadOnly` on **blast radius** (nothing on disk, no process, no network),
so it is ungated in every mode and offered in **Plan**; **default-on** for every model with no
config key of its own (`tools.disabled: [task_list]` is the off switch); a **delegation gets a
fresh empty list** and inherits, shares and returns nothing. See
ADR 0072.
_Avoid_: "todo list" (the markdown habit this replaces; the term is the task list), "plan" (a
Plan-mode reply is prose in the conversation, this is engine-held state), "the agent's plan" (the
host never authors or trusts a row — it is model-authored text, fenced as such).

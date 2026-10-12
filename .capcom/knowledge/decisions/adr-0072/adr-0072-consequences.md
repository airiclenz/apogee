---
title: ADR 0072 consequences
---

- **A prefix re-encode per `task_list` call.** Accepted and recorded here so no later reader has to
  re-derive it. The list sits ahead of the context files, so a change invalidates the cached prefix
  from that point through the tool block. Sessions whose model never calls the tool are unaffected,
  because an empty list renders `""`.
- **The standing content is no longer wholly engine- and user-authored.** Model text now rides in
  it. `tasklist.Fence` and the context-file fencing keep a workspace file from forging the block,
  and decision 4's position keeps the model's own text behind the host's statements.
- **`--resume` restores the checklist.** A resumed session reads back what it had left to do, which
  is the first session-state addition since the record's own fields.
- **The `task_list` roster slot is spent for every model.** A profile that would rather not have it
  writes one `disabled:` line.
- **The tool-surface denial is reversed in place.** `docs/design/tool-surface-findings.md` keeps its
  dated record and carries the reversal beside it, so the reasoning that produced the denial stays
  readable next to the distinction that overturned it.

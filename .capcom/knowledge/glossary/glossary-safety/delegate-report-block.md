---
title: "Delegate report block: the standing system content only a delegated agent carries"
---

**Delegate report block**:
The engine-composed part of the **standing system content** that a **delegated** agent — and only a
delegated one — carries: it tells the child that the agent which delegated the task sees nothing of
this conversation and receives only its **final reply**, so anything not written there is lost, and
asks it to report what it found, what it changed and what remains unfinished by citing `path:line`
rather than pasting file contents. Like the **Orientation block** it is **harness text, not persona
text** — engine-owned, no config key, not a **Reaction**, so it is on under **Bypass** and no
edit to `system-prompt-text` and no workspace **Context files** can remove it (a context line that
spells its opening sentence is fenced `[workspace text] `, exactly as a forged orientation header
is). It is gated on **depth**, not on configuration: every agent below the top level gets it, at any
depth, routed or unrouted — except a **workflow** item's child, which reports through `finish`
rather than its final reply and carries the workflow finish block (the same opening sentence, then
the finish instructions) in its place — and a top-level session's first system message is
byte-identical without
it. It **rides along** under the Orientation block's rule — composed in only when a standing system
message exists anyway — and sits between the orientation and the **Task list** block, ahead of the
**Context files** blocks, so every engine-owned part still precedes the repo-controlled text. It
does not compete with the
**Sub-agent** step cap's wrap-up directive: that says which turn is the last, this says what the
final reply is for. See
ADR 0023's
2026-09-02 addendum.
_Avoid_: "wrap-up directive" (that is the step cap's one-turn notice), "sub-agent prompt" (a child
inherits the same **System prompt**; this is a separate engine-owned block).

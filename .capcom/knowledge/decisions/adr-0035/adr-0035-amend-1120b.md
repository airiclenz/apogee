---
title: "ADR 0035 amendment of 2026-11-20 (b): a finished tool block folds when the next Turn starts"
refs: [docs/layout/tool-layout.md, internal/tui/transcript.go]
---

**Why now.** Under `ui.tools-auto-collapse` a finished block — a `✦ Sub-agents (N)` group, a
`✦ Tools` umbrella, a lone groupable call — stood open through the whole of the next Turn and
folded only when that Turn's reply was written into the transcript. The reasoning and the reply
tokens of the next Turn are painted live, outside the committed entries the settled rule read, so
the block the reader was done with stayed open beside the very text that moved past it. The owner
asked for the fold to land when the next Turn starts instead (owner report and decisions
2026-11-20).

**What this supersedes.** `adr-0035-amend-1118` (a)'s "*Settled* is derived at paint and stored
nowhere" and the restatement of that rule in `adr-0035-amend-1120` (a) ("a later entry of its own
run follows it or its run is over"). Everything else in both amendments stands: who is in scope,
what touched means, the folded lines, the click that opens a block alone and writes nothing, the
settings key and its default.

**(a) A block settles when the next Turn of its run starts.** A *Turn* is one iteration of the
loop (glossary `Turn`), not the whole running Exchange the TUI calls "the human's Turn". When one
of a run's Turns reaches its boundary with more Turns to come, the run's last entry is marked
closed by that boundary — a session-only mark on the entry, never written to the session record
or to config. A foldable block with a member so marked is settled from that moment, even while
the next Turn is still reasoning or streaming, and nothing yet follows it. The engine answers every
call a Turn issues before it reports that Turn's boundary, so the marked block's own calls have all
returned. The same holds inside a delegated run, at that child's own boundary; one run's boundary
never settles another run's block. The rule that was there before still settles a block too: every
member finished and a later entry of its own run follows it, or its run is over. A replayed block
is still settled by construction.

**(b) It stays settled while the next Turn extends it.** Groupable calls of two Turns merge into
one block when nothing is committed between them, so the next Turn's first call can join the block
the boundary folded. The block stays folded as it grows: an umbrella shows its live verb on the
folded header while the new call runs, and a group shows its grown count. Two cases lift that, and
the block is then open until it settles by the earlier rule:

- **A member is waiting on the user (owner).** A pending `ask_user` question, or a call held on its
  approval pane, opens the block for as long as that wait lasts, so the human sees the call they are
  being asked about. Once it is answered or decided, the block folds again.
- **The reader had opened the closing member (owner).** A lone call the reader opened, which the
  next Turn's call then turns into an umbrella, is not folded by the boundary its lone shape
  received. The umbrella stays open until it settles by the earlier rule, as before this amendment.

A block the reader touched is never folded by a boundary, exactly as `adr-0035-amend-1118` (b)
says. The boundary marks only an entry that ends a block auto-collapse folds at that moment — an
umbrella, a lone groupable call, a Sub-agent or Skill group of two or more. A lone delegation or a
lone skill fetch is never auto-collapsed, so its boundary folds nothing, and the group the next
Turn's delegation or fetch forms beside it stays open while that new member runs.

All of it is TUI view state and paint: no engine, config-key or session-record change, so the
engine stays Driver-neutral (ADR 0031) and nothing reaches the model. Spec:
`docs/layout/tool-layout.md`, "Fold states and interaction".

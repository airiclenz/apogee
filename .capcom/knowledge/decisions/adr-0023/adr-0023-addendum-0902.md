---
title: "ADR 0023 addendum of 2026-09-02: a FOURTH, child-only part: the delegate report block"
---

§6's decision stands and so does its order; only the composition list grows again, and this time
the new part is conditional on **who** the agent is rather than on what is configured. On a
**delegated** agent — `depth > 0`, routed and unrouted alike — the engine composes in its own
**delegate report block** (`internal/agent/delegatereport.go`), so a child's wire order reads
**prompt → orientation → delegate block → context files → mechanism directives → tool block**. A
top-level agent's message is byte-identical to what it was, because the block is composed only when
the depth gate opens.

- **Why the engine owns it.** The block states the one fact a delegate cannot learn from any
  configured source: the agent that delegated the task sees nothing of the child's conversation and
  receives only its FINAL reply, so anything not written there is lost. It then asks for that reply
  in the shape a parent can act on — what was found, what changed, what is unfinished, cited by
  `path:line` rather than pasted. A `system-prompt-text` is written by a user who may never delegate,
  and a workspace `AGENTS.md` is written by a repository that cannot know it is being read by a
  child; neither can be relied on to carry it, and no edit to either should be able to delete it.
  It is engine-owned with no config key and no **Mechanism** gate — `wrapUpDirectiveFormat`'s
  precedent (`internal/agent/subagent.go`) — so it is on under **Bypass** too.
- **Position, on the same F-19 reasoning.** It sits after the orientation block and **ahead of the
  context files**, for the reason the 2026-08-26 addendum moved the orientation there: every
  engine-owned part precedes the repo-controlled blocks, so no workspace text can arrive after the
  host's own statement and read as a correction of it. The other half of that guard is the fence —
  a context-file line spelling the block's opening sentence is prefixed `[workspace text] ` exactly
  as a forged orientation header is (ADR 0026's
  2026-08-26 addendum).
- **Ride-along, not a fifth source.** The empty check on the two configured sources is still taken
  **before** the block is asked for, so `standingSystem()` still returns `""` for no prompt AND no
  context files and §6's seeds-nothing anchor stays byte-identical. Per the 2026-08-31 amendment
  that posture is now reached only through `use-default-prompt: false` or an explicitly empty
  configured template, so in practice a stock delegation always carries the block — but the rule is
  unchanged and still binding.
- **It states the new order rather than rewriting the old records.** This ADR's 2026-08-26 addendum
  and ADR 0026's own order
  sentence are dated records of what was decided then; they are left as written, and this addendum
  is where the current order is read off.
- **It does not contradict the wrap-up directive.** A capped child's request carries both: the
  wrap-up directive says *this* turn is the last one it gets, the report block says what the final
  reply is **for**. Neither claims the other's reply is the last.

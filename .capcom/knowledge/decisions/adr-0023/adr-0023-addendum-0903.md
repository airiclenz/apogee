---
title: "ADR 0023 addendum of 2026-09-03: a FIFTH part, and the first engine block that is NOT a session constant"
---

§6's decision stands and so does its position-0 seeding; the composition list grows once more, and
this time the new part breaks the one property every earlier engine-composed part shared. The
engine now composes in the **task list block** (`internal/agent/tasklistblock.go`) — the model's own
checklist, held on the engine and written only through the `task_list` tool
(ADR 0072) — so the standing content is
composed of **five** parts and the wire order reads **prompt → orientation → delegate report → task
list → context files → mechanism directives → tool block**.

- **Position, on the 2026-08-26 F-19 reasoning read in both directions.** The block rides ahead of
  the workspace context files, so every engine-owned part still precedes the repo-controlled blocks
  and no workspace text can arrive after the host's own statements and read as a correction of
  them. It goes **last** of the engine's four for the complementary reason: its content is
  MODEL-authored, and text the model wrote must not sit where the host's facts sit. The other half
  of the guard is the fence — a context-file line spelling `tasklist.Fence` is prefixed
  `[workspace text] ` exactly as a forged orientation header is
  (ADR 0026's 2026-08-26
  addendum).
- **Ride-along, not a sixth source.** The empty check on the two configured sources is still taken
  **before** the block is asked for, and an empty list renders `""` besides, so `standingSystem()`
  still returns `""` for no prompt AND no context files and §6's seeds-nothing anchor stays
  byte-identical.
- **THE VOLATILITY EXCEPTION — what this addendum amends.** The 2026-08-25 amendment's "Live
  inputs, per-session constants" bullet holds that an engine-composed block reads live inputs which
  are nevertheless constant *within* a session, so the block is prefix-KV-cache stable exactly as
  `{{scratch}}` is. The task list is the first engine-owned part for which that is false: every
  `task_list` call changes the standing content and invalidates the server's prefix cache from this
  block onward. That bullet is amended to admit an engine block whose volatility is **under the
  MODEL's control** — the property that makes the cost payable rather than imposed, since a session
  whose model never calls the tool pays nothing and one that does is spending a re-encode it asked
  for. The rule for every other engine block is unchanged: per-session constants, or state why not.
  (The standing content as a whole was never wholly constant — the rendered template varies with its
  live `{{mode}}`, so Shift+Tab already moved the prefix; what is new is an *engine-composed* part
  that does.)
- **A delegation gets its own empty list.** §7's wholesale `cfg` copy carries nothing here: the
  child is constructed with a fresh list, so its block starts `""` and fills only from its own calls
  (ADR 0072 decision 7).
- **It states the new order rather than rewriting the old records.** The 2026-08-26 and 2026-09-02
  addenda are dated records of what was decided then and are left as written; this addendum is
  where the current order is read off.

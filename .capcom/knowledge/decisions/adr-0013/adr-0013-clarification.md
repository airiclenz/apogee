---
title: "ADR 0013 clarification of 2026-07-02: guard tiers at the recursion point"
---

§1 leaves the dangerous-action tiers at the delegation call implicit; made explicit while
collapsing the dispatch decision into the Resolution verdict:

- **Tier-1 (hard-refuse) applies to the delegation call itself.** A hard-refusal needs no
  human judgment, so it fails fast — no child loop is spawned just to flounder.
- **Tier-2 (force-approval) does NOT gate the delegation as a unit** — consistent with §1's
  "never gated as a unit." Nothing executes at delegation; the shared read-only floor (§3)
  re-fires on the child's *actual* dangerous tool call, which threads the parent's Approver.
  The human gets the speed-bump at the point of execution with the real command in the prompt
  (better judgment context than a prose task description), and is not double-prompted (a
  forced gate ignores the allow-for-session cache, so a delegation-level gate would not have
  pre-cleared the child's anyway).

This is a rule of the Resolution table (leaf verdicts honour force-approval; `Delegate` does
not), pinned by a table-test row, not an accident of check ordering.

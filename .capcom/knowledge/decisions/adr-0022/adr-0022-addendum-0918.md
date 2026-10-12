---
title: "ADR 0022 addendum of 2026-09-18: a retained capped delegation is engine memory, never a record; D8 stands"
---

Plan `2026-09-18 - 00` (capped-delegate report loss) has the engine **fold** a delegated child's
conversation when it hits a bound (`context.Summarize` under the delegate-fold brief — a summary
call, not a Compaction: the child's history is untouched and no "context compacted" trace is
written) and hand the parent that fold under `[engine summary]` on every capped result, and it lets
the parent **continue** such a child — `sub_agent` with `continue: "<name>"` spawns a fresh child
from the retained task plus that fold. To do so the parent Agent retains, per capped child that
ended its run wearing a name, `{task, name, tools, output_path, fold, closing report, bound}` for
the rest of its own Exchange (`internal/agent/children.go`, cleared as the next Exchange opens).

**Decision 8 stands, and this is the case it was written for.** The retained entry is **live engine
state, never session state**: nothing of it is serialised, the snapshot schema is untouched, and a
session restored by `--resume`, `--continue` or the in-TUI live restore holds an empty retention —
a `continue` after a resume is refused with `retained: none`, by design. What DOES persist is what
always did: the capped result — head line, fold, closing report and the
`[to continue this delegate: sub_agent with continue: "<name>"]` note — is a committed tool result in
the parent's conversation, so the record carries everything the model was told, and a new
delegation after a resume can quote the fold from the transcript. The child's own `Session` stays
ephemeral; the fold is not a second record of it, it is a summary the engine authored INTO the
parent's conversation. This supersedes the result body the archived plan
`docs/plans/archived/2026-08-26 - 00 - delegate-token-runaway-plan.md` recorded — the child's last
narrated text under a head saying so; it was scavenged narration then and is an authored fold now
— and changes nothing about what is saved or
when: the 2026-08-25 addendum's progress save still fires on the same child events during a
continuation as during a first run, and its engine half is still the last quiescent-boundary
snapshot, which never held a delegation in flight and holds no retained fold now.

> **Amended 2026-09-26 (ADR 0086 D3) — the retention half is reversed; D8 stands.** A
> retained delegation is now session state: the parent Agent's retained set rides the engine
> snapshot under an additive `retained` key — no schema version bump, the precedent `Tasks`
> (ADR 0072) set — written at the same quiescent boundaries every snapshot is, so `--resume`,
> `--continue` and the live restore load it, and "a `continue` after a resume is refused with
> `retained: none`, by design" no longer holds. `/clear` drops it, a fork (`CutSession`) keeps only
> the rounds spawned before its cut, and a Turn rolled back by cancel restores the set it began
> with. Decision 8 itself stands: a child's `Session` is still never a record, and the retained
> entry is the parent's engine state, not the child's. The 2026-08-25 addendum's progress save and
> its engine half are unchanged.

> **Amended 2026-09-27 (ADR 0088 D1–D2).** A cancel no longer rolls the Turn in flight back: it is
> settled, its finished calls keeping their results, so the record written at idle holds that Turn
> too. The retained set is no longer restored to its Turn-start value on a cancel.

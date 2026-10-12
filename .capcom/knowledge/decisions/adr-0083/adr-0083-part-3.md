---
title: "ADR 0083 part 3: A delegate's runtime state is not one value; delegation is not the whole spawn (2026-09-16, review 09-16 #26)"
---

**Proposed.** `Agent.delegate *delegateState` (nil at depth 0) holding the delegate-only fields
`internal/agent/agent.go` keeps flat — depth, call id, console owner, task, seat fallback, cap
requested, name, mailbox, steered, step cap, token and time caps, output path and target,
mid-Exchange compaction, step notice, live mode — with `isDelegate` reading "set", and
`Run`/`finishAtStepCap`/`delegationResult` reading the value whole; a follow-on to plan
`2026-09-15 - 00` item 8, which made a child be *built from* a `delegation` value.

**Denied** (owner, 2026-09-15, reaffirmed 2026-09-16 and 2026-09-20). Item 8's regression guard
kept the fields flat deliberately, so that the test files that fake a child by setting a field
keep compiling; the 2026-09-16 review itself filed the candidate *Speculative* — "the owner
deferred this knowingly on 09-15 for test-churn reasons … raise only if the test-surface
argument outweighs the churn" — and noted it is not the denied Posture value, since nothing in
it is a live setter. The construction half is done and stays: the `delegation` value is the
constructor's INPUT, and `newChildAgentOn` makes no post-construction writes. What is declined is
the second half — making that value the child's *runtime* state — because it moves nothing the
loop reads on one clock, and `runSubAgent` still writes call-derived facts after construction
and reads the child's fields for the report because those facts arrive at different moments of
the spawn.

**Reopens on.** A test-surface count that outweighs the churn — for instance, a child fake that
has to set more fields than the constructor takes, or a delegate-only field added that a flat
layout leaves readable at depth 0 and a bug results from it.

> **Note 2026-10-01 (review 09-30 #3; plan `2026-09-30 - 00`, item 17).** This section stands:
> what moved is the run protocol, not the runtime-state value. `runSubAgent`'s register → arm →
> Run → disarm → stop verdict → fold sequence is now the shared child-run lifecycle
> (`internal/agent/childrun.go`, `runChild` and `reapChild`) the workflow spawner also runs on;
> the delegate's fields stay flat, and `runSubAgent` still makes its call-derived writes after
> construction and reads the child's fields for the report.

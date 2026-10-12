---
title: Step cap
---

**Step cap**:
The number of **Turns** a **delegate** may take in its one Exchange before the engine ends it —
the `delegate-max-steps` key, default **80**, `0` = unbounded. It bounds child agents ONLY: the
main loop is the human's to stop, a delegate's is nobody's, and an uncapped delegation is how a
single `/code-audit` run reached 633 Turns and a billion prompt tokens. A `sub_agent` call's
optional `max_steps` argument may LOWER the cap for that one Run, never raise it — the rule is
per Run, so a continuation (below) starts under `min(max_steps, delegate-max-steps)` of its own
rather than the remainder of the Run it continues. Two
sibling bounds ride the same path (`Config.Delegation.MaxTokens`, `.Timeout`): the
`delegate-max-tokens` key, default **20000000**, bounds the child's cumulative PROMPT tokens
(its own usage tally), and the `delegate-timeout` key, default **2h**, bounds the wall clock
from the child's first request; `0` disables either, and both are read at spawn. On a step,
token or time bound the child's Exchange ends **cleanly, not faulted** (`StepResult.StepCapped`)
— but not before two things happen. First the **engine fold**: the engine summarizes the
child's whole conversation as it stands at the bound (`context.Summarize` under the
delegate-fold brief — files read by path, facts established with `path:line` references, what
was concluded, what remains unfinished; no file contents) on the child's own model and budget.
It is a summary call, not a Compaction: the child's history is untouched, its usage is booked
`Maintenance` with the `DelegateFold` flag so no "context compacted" trace is written, and it is
no Turn. Then the engine spends one further Turn on the child's CLOSING REPORT: that request
goes out with the tool menu withdrawn, and the directive telling the delegate which bound it
hit, why its tools are gone, and asking it to report to the agent that delegated the task,
unfinished work included — not to continue the task, not to write what a tool would have
printed — rides the closing tool result of the capping Turn as an **engine note**, fenced
`[engine — wrap-up]` … `[end engine — wrap-up]` where the model reads next (the advise slot's
seam under the engine's own header, ADR 0076 D6 addendum; where the menu is withdrawn wholesale
the tool-less wire degrades that message to user role whole, so the fence reaches the model as
the tail of a user message). Only
when the tail is not a tool result does the directive fall back to the system prompt. That Turn
is EXTRA — it sits outside the cap, so `delegate-max-steps: 3` still buys
three working Turns plus this one reply. The fold IS the report: the parent receives it on every
bound under `[engine summary]`, and the child's closing text follows under `[delegate's closing
report]` — the wrap-up's reply, or whatever the child last said out loud when that Turn faults,
errors or answers with a tool call, or `(no visible text)` when it never spoke; a fold whose summary
call faulted is replaced by `[engine summary unavailable — <cause>]` and the closing report
still lands. The withdrawal has ONE exception
(2026-09-15, reversing the 2026-09-01 tool-less call; widened 2026-09-18 from "only when named"
to every delegate): the wrap-up Turn keeps `write_file`, ONCE, so a capped child can save its
report or partial output rather than lose it — the directive says so (`You may still call
write_file once, to save your report or partial output before you reply.`), the engine dispatches
the first `write_file` call before ending and drops any later one, and the write meets the
Mode's ladder as any other (Ask-Before still prompts). A delegation whose `sub_agent` call named
an `output_path` — the file it is expected to write, resolved through the same write fence as
`write_file`'s own target — has that one write narrowed to exactly that path: the clause names
it (`You may still call write_file once, for <path> only.`), and a `write_file` aimed anywhere
else is refused with `wrap-up: only <path> may be written`. The engine reads no path out of the
task text; the `sub_agent` description tells the model to pass a file the task names as
`output_path`. The exception is offered only where the child still holds `write_file` and its
Mode admits a workspace write — a Plan-mode child's wrap-up stays tool-less — so it is never
announced and then refused. The parent receives a non-error
result whose first line marks it partial and names the bound that tripped (`step cap`, `token
budget` or `time limit`) — `[delegate stopped at its <bound>; partial result — engine summary
and closing report follow]` — followed by that body, so Turns of real work are not thrown away,
and what the parent reads is authored rather than scavenged from whatever the child happened to
narrate alongside its last tool call. A closing text that is NOT a report is named, never
dropped (owner call, 2026-09-18): when it is unparsed tool-call markup, opens on a `read_file`
header (`[File: …, N lines total`) within its first three non-blank lines, is mostly `path:line:`
grep hits, or ends on a stated next step ("Let me …", "I'll …") with no `PHASE:`/`STATUS:`-style
receipt line before it, the head becomes `[delegate stopped at its <bound>; no closing report —
the delegate's last reply reads as <tool-call markup | a file dump | a grep dump | narration of
its next step>, not a finding; engine summary follows]` and the text follows the fold whole under
`[delegate's closing report — read as narration, not a finding]`. The prefix is unchanged, so the
TUI still reads the bound; blank text is the wordless path above, never a non-report; and no
wording rule faults a finding — the closed acknowledgement list is the only text withheld. A
bound is not the end of the work, either (2026-09-18, P6; session-long since 2026-09-25, ADR 0086):
the parent **retains** every capped delegation that ended its run wearing a **Delegation name** (one
its call gave or the namer generated; the task-first-line fallback is a Driver's display rule, not a
name) as a **Retained delegation** — the task as the first call spelled it, the `tools` roster and
`output_path` the latest call asked for, the bound, and one **round** per run under the name: its
instructions (the first round's are the task) and its report, which for a capped run is the fold
and the closing text — keyed by that name, for the rest of the **Session**. The set rides the
engine snapshot under an additive `retained` key (no schema bump; ADR 0022 D8 stands — the child's
own Session is still never a record), so `--resume`, `--continue` and a live restore load it,
`/clear` drops it, a fork keeps only the rounds spawned before its cut, and a cancel keeps what
its finished and stopped children left (ADR 0088 D2); Compaction and `/undo` leave it alone. A **faulted**
delegation is retained the same way (2026-09-20, ADR 0082,
`apogee-60x`): its Run ends abandoned with the parent's ctx still live, the engine writes the fold
at the fault — no wrap-up Turn, the fold is the only model call, under one `stream-idle-timeout`
of its own so the error result lands at most one idle window late; a child that completed no Turn
is retained without a fold request, its fold the unavailable marker saying so, and a fold that
fails retains the marker naming the cause — and its last narration stands as the closing text;
the error result keeps its fault head and gains the continue line. A cancel no longer unwinds the
delegation: it stops a running child as `^x` does, so that child is retained like a stopped one
(ADR 0088 D2). The fault that
reaches retention is one the child's
Turn has already ridden out to its **re-stream budget** — the same three re-sends under the
doubling hold-off depth 0 gets, each idle window counted — so a retained faulted delegate is one
whose Upstream stayed down past that budget, never one that met a blip
(ADR 0082).
A delegation the human **stopped** is retained the same way too, folded at the stop as at a fault
(**Stop (a delegation)**), and so is a **completed** one whose call gave it a name — a generated
name never reaches the model for a completed run, so it retains nothing. The capped, faulted or
stopped result's last body note spells
the handle back — `[to continue
this delegate: sub_agent with continue: "<name>"]` — and a `sub_agent` call carrying
`continue: "<name>"` spawns a **fresh child** whose opening task is the retained task, then the
rounds — chosen newest first into the same 4096-token budget the fold is held to, the newest always
whole, and laid in oldest first, each under `[round N — instructions]` (from round 2) and
`[round N — report]` or, for a fold, `[round N — engine summary]`, behind `[K earlier rounds
omitted]` when any were dropped — and the call's own `task` under `[continuation
instructions]`; the name, roster and `output_path` are inherited wherever the call leaves them
unset (an inherited name is re-announced for the new run), and the same name replaces an earlier
entry. A continuation takes its entry as it SPAWNS — a continue call
refused on its own arguments (an invalid `run_on`, an unknown tool name) keeps it, so a corrected
retry still finds it — and its run, whatever its outcome (a cancel stops a running continuation as
`^x` does, ADR 0088 D2), is appended as the next round and retained anew under the name that run ended wearing, still over the ORIGINAL task, so a
later continuation composes over the rounds, never a fold of a fold. An unknown
name is refused with an error result naming the retained names — the 16 most recently used, then
`(and N more)` — (`[no delegate named "<name>" to
continue — retained: <a, b | none>]`). Each continuation
is a new Run with a cap of its own — the engine puts no limit on how many times a delegation is
continued, and an unnamed delegation, having no handle, is not retained. The cap
also ANNOUNCES itself before it lands — the **step-budget notice** (2026-09-19, ADR 0077
addendum): the tool result that closes the Turn reaching **ceil(0.75 × cap)** carries one engine
note, fenced `[engine — step budget]` … `[end engine — step budget]` (never the advice fence — a
delegate has been seen reading an advice fence on a `read_file` result as part of the file),
`steps: N of M used — K left before the wrap-up Turn; write your output now`, so the child writes
while it still holds its tools. It fires ONCE per delegation while its copy survives in the
history — its latch is the note's own presence, so a fold, a prune stub or a rollback that took
the note away leaves the next result to be told again — silent at depth
0 and for an unbounded delegation, and it is part of the bound's contract, not a Reaction: no key,
no Reaction id, no firing booked, on under Bypass (the `step-budget-notice` key of the 2026-09-15
addendum is retired; a file still carrying it is ignored without an unknown-key notice). The token
bound announces itself the same way — the **token-budget notice** (2026-09-19, ADR 0077 addendum,
`apogee-zuu`): the tool result closing the Turn at which the child's cumulative prompt tokens reach
**ceil(0.75 × delegate-max-tokens)** carries one engine note, fenced `[engine — token budget]` …
`[end engine — token budget]`, `tokens: 15.2M of 20.0M spent — 4.8M left before the wrap-up Turn;
write your output now`, latched the same way (by its own note's presence) and beside the step
note on the result.
Neither notice is a rung of the **context-fill notice** ladder: the ladder measures one request
against the window, these measure the delegation's whole spend against its cap — which is why a
child under a wide `working-window:` heard nothing at 10.7M of 20M before the twin existed. It is a
**structural floor**
(ADR 0006), not an armed **Reaction** —
it stays on under **Bypass** and nothing withdraws it at runtime. Enforced in exactly one place,
`Agent.Run`.
_Avoid_: "turn limit", "iteration cap" (the unit is the Step/Turn the model spends).

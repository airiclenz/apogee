---
Status: accepted
Amends: ADR 0013 §5 and its 2026-09-18 amendment (a stopped child returns a partial result; retention outlives the Exchange), ADR 0022's 2026-09-18 addendum (retention is session state), ADR 0063 D4 and its "What stays out" consequence (per-child stop; prompting a finished child)
---

# A delegation is stopped singly, and a named one stays continuable for the session

## Context

`IDEAS.md` → "Sub-Agents" item 3 asks that "a master agent … stop its sub agents and …
communicate with its sub agents". The handoff
`docs/handoffs/2026-09-24 - 00 - sub-agent-control-handoff.md` surveyed the ground at 69565337, and
the owner's 2026-09-25 grilling session settled it.

A delegation blocks its parent: `runSubAgent` runs the child to its Exchange boundary inside the
parent's Turn (ADR 0013 §5), and a fan-out joins every child before the parent continues (ADR 0039
D4). The parent therefore cannot act while a child runs, so "the parent stops a child" is
impossible in the current model rather than merely unbuilt. Two things exist that come close. The
human can message a running child (ADR 0063, `InterjectChild`), and the parent can `continue:` a
**named** delegation — but only one that was capped or faulted, only for the rest of the Exchange
(`loop.go` clears the retention as the next opens), and only once per retained entry. The one stop
there is `esc×2` at the top level, which cancels the whole Turn and rolls it back with no partial
result.

## Decision

**We keep the blocking model and do not build asynchronous delegation.** A parent that spawns,
messages, stops and waits on children that outlive its Turn — option A in the handoff — would
supersede ADR 0013 §5 and ADR 0039 D4, reach about ten subsystems, and hand small local models
four more tools to juggle. The Floor invariant would ship it off until the bench showed it helped,
so it would deliver nothing to users for a long time. This ADR takes the smaller step, whose two
parts are both prerequisites of A if A is ever grilled: the parent may go back to any named
delegation it ran this session, and the **human** may stop one delegation without stopping the Turn.
The parent itself still cannot stop a running child — it is not running while the child is.

**D1 — A named delegation stays continuable for the whole session.** A delegation that completed
normally is retained when the `sub_agent` call **named** it; naming is the parent's own opt-in, so
no new text reaches the model and nothing changes for a model that never names. A name the
out-of-band namer generated (ADR 0068) never reaches the model for a completed run and does not
retain it. Capped, faulted and stopped (D4) delegations keep today's rule — retained under their
name, generated or given, with the name spelled back as the continue line — because their result
already tells the parent the handle. Retention no longer clears at the next Exchange: an entry lives
until `/clear`, a fork that cuts it off, or the rollback of the Turn that made it (D3). The same name
still replaces the earlier entry.

**D2 — A continued delegation is seeded from its history, trimmed at use.** A child's conversation
is still discarded when it ends — the continuation re-spawns from a summary, it does not resume, and
ADR 0007's "suspended sub-agent" slot stays empty. What the entry keeps is the **original task** and
one **round** per run under that name: the round's instructions (the first round's are the task) and
its report — the child's own final report for a completed round, the engine fold and closing text
for a capped, faulted or stopped one. No summary call is added for a completed round: its final
report is the child's own account of the work. Nothing is trimmed on disk. When a `continue:` spawns,
the rounds are laid in, newest first, until they fill the same 4096-token budget the fold is held to,
and older rounds are dropped with a marker saying how many. The refusal for an unknown name lists the
**16 most recently used** retained names and `(and N more)` past them, so the listing a confused
model reads is bounded however long the session runs.

**D3 — Retention is session state, and history operations carry it.** The retained entries are
saved in the engine's snapshot as an additive `retained` key — no schema version bump, the precedent
`Tasks` (ADR 0072) set — at the same quiescent boundaries every snapshot is taken at. Each entry
records the conversation point its spawning result was committed at. The operations that move
history treat it as follows:

| Operation | Retention |
|---|---|
| `--resume`, `--continue`, live restore | loaded from the snapshot |
| `/clear` | dropped whole |
| fork (`CutSession`) | kept only for entries spawned before the cut |
| a Turn rolled back by cancel | restored to its value at the Turn's start |
| Compaction | untouched — a forgotten name is recovered from the refusal's listing |
| `/undo` | untouched — it reverts files, not the conversation |

Restoring on rollback also closes two existing leaks: a pooled sibling that capped before the
cancel stays retained after its result is rolled back, and a continuation cancelled after it spawned
loses the entry it consumed.

**D4 — The human stops one delegation, and the Turn goes on.** A stop ends one child — and every
child under it — with the parent's context still live. It is not a cancel: nothing is rolled back,
the parent's Turn continues, and the delegation's tool result is committed like any other. The
engine folds the child's conversation at the stop, as `finishAtFault` folds it at a fault — no
wrap-up Turn, one summary call under one `stream-idle-timeout` of its own — because files the child
already wrote stay written, and the parent must know what they are. A second stop while the fold is
running skips it, leaving the unavailable marker in its place. The result's head is
`[stopped by the user — engine summary follows]`, followed by the fold, the closing text, any
undelivered interjections and the continue line. A pooled child stopped before it started runs
nothing and folds nothing. The delegate ledger's outcome word is **`stopped`**, a fifth beside
`completed | capped | faulted | cancelled | refused`: "stop" is now the human's single delegation,
"cancel" the whole Turn, and an engine bound is "capped", never "stopped".

**D5 — The stop is an engine capability addressed by run id.** The engine gains a per-child stop
keyed by the delegation's **run id**, which ADR 0039's 2026-09-24 amendment already mints, alongside
the interjection path that bead `apogee-interject-by-run-id` moves to the same key. The TUI binds
it to **`ctrl+x`**, which is unbound today: inside a run view it stops the viewed run, and on a
member row of the `✦ Sub-Agent (N)` umbrella it stops that row's run. The run view's hint reads
`esc back · ^x stop`. There is no confirmation, because nothing is rolled back. `esc` keeps meaning
back, and `esc×2` at the top level keeps cancelling the whole Turn. A bench, headless run or future
daemon reaches the same engine call, so ADR 0031's doors stay open; no CLI surface for it is built
here.

## Considered options

- **Asynchronous delegation (handoff option A).** Rejected for now, not denied. It is the literal
  ask, but it replaces ADR 0013 §5 and ADR 0039 D4, and it ships dark under the Floor invariant.
  D1 and D4 are both parts it would need, so none of this is thrown away if it is grilled later.
- **Retention for the Exchange only.** Rejected by the owner. It covers the parent correcting a child
  before it answers, but not "ask the refactor helper to also update the docs" a message later.
- **A summary call at every completed delegation, or a saved child transcript.** Rejected: the first
  costs a model call on every named completion, including those never continued, and the second is
  option A's resume and overflows a small model's window. The child's own final report is free.
- **A continue line on every result, so every delegation is continuable.** Rejected: one more line
  of model-facing text per delegation for every model, which the Floor invariant would ship off.
- **Caps on what is saved** (the 16 newest, a 4096-token report). Rejected by the owner: disk is
  cheap, and the only cost that matters is the model's context, so the caps apply at use (D2).
- **`esc×2` inside a run view as the single stop.** Rejected: the first `esc` walks up a level, so the
  double tap would land at the top level and cancel everything.
- **Folding `apogee-2un` in** (a Turn cancel keeps the siblings that already finished). Kept
  separate: it changes what `esc×2` means and needs its own design for the wire shape of a rolled-back
  Turn that keeps some tool results. The single stop covers most of its need, and the two share code.

## Consequences

- **ADR 0013 §5 gains a third ending.** A child either completes inside the parent's Turn, or a
  cancel rolls that Turn back with no partial result — or, from this ADR, the human stops it and
  the parent receives a partial result and goes on. §5's coarse resume and "no snapshot mid-child"
  are unchanged; its 2026-09-18 amendment's "engine memory that dies with the Exchange" no longer
  holds.
- **ADR 0022's 2026-09-18 addendum is reversed.** "A `continue` after a resume is refused with
  `retained: none`, by design" no longer holds. D8 itself stands: a child's `Session` is still never
  a record, and the retained entry is the parent's engine state, not the child's.
- **ADR 0063's deferrals close.** D4's "stopping stays whole-run from the top level" and the
  "What stays out" lines on per-child stop and on prompting a finished child are amended. A run view
  of a finished child stays read-only: the human messages a running child, and it is the parent that
  continues a finished one. `layout.md`'s "`esc` means back before it means stop" section is
  restated against `^x`.
- **ADR 0039 D4 stands.** A stop is not a cancel, and a pool still joins every child before the
  parent continues; a stopped child simply reaches that join sooner. The 2026-09-14 amendment's
  "running children are never cancelled by a message" also stands — a stop is a key, not a message.
- **Model-facing text moves slightly, and ships on.** The `sub_agent` schema's `continue` description
  widens to named delegations of this session (fixing its current "in this conversation" drift), a
  stopped result is new, and the refusal's listing is capped. None of it is a Reaction: it is the
  shape of an existing tool, structural like the continue line it extends, and on under Bypass.
- **Glossary.** `CONTEXT.md` gains **Stop (a delegation)** and **Retained delegation**. The
  **Sub-agent** entry's "for the rest of its Exchange, in memory only" is restated when this lands.

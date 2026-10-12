---
title: "Workflow: one engine-run orchestration of Sub-agent runs in stages of items"
---

**Workflow**:
One engine-run orchestration of **Sub-agent** runs: an ordered list of **stages**, each covering a
list of **items**, where every item is done by a fresh child **run** whose detail goes to a file and
whose outcome comes back as a **Receipt** — handed back by the child itself, except for a background
`sub_agent`'s one item, whose child writes a prose report and whose Receipt the engine builds. The engine, never the parent, runs the stages — fanning out in
waves, retrying, verifying and merging — and the parent reads back one line per item plus a report
path, so the orchestration costs its context almost nothing. A workflow has three sources: the
top-level model's `fan_out` call (one fan-out stage, optionally followed by a verify and a merge
stage), a human-written **Recipe** (any number of stages), and a `sub_agent` call carrying
`background: true` (one item, run as a **Background workflow**; 2026-10-05,
ADR 0094). The model asks for the fan-out; the
engine never plans work on its behalf (the distinction from the retired **Guided decomposition**).
A workflow lives in a folder under the session's **Scratch dir**, so it survives `esc`, a crash and
a resume: cancel stops it and keeps every finished item, and asking again with the same task,
items and inputs picks it up where it stopped — unless a **Background workflow** is still driving its
folder, when a blocking re-issue is refused, naming that run, and nothing starts (2026-09-30). Its
item tally is the engine's: the fan-out items, each on its latest round's receipt, a verify or
merge receipt never an item — the set the finish note, the TUI's totals line and the headless and
daemon exit verdict all count. It runs **blocking** by default, or as a
**Background workflow**. Ratified 2026-09-27 (ADR 0087).
In the TUI each run of a blocking workflow draws one block — a Recipe's own, or the `fan_out` call's card — and
its item runs never paint in the conversation: a Recipe's block shows one row per stage from the
start (dim and `pending` until the stage starts), each opening its **Stage view** or, for a one-item
stage, that item's **Run view**; a `fan_out` card, having one stage, shows one row per item. Every
item run has a head of its own, so it is opened, messaged and stopped as a delegation is, and the
block's structure is kept in the **Session record** so its rows reopen after a resume
(ADR 0090, 2026-09-28). A stopped
workflow says how to re-run it: its result ends on a `to resume: …` line the model reads (the typed
recipe line, the recipe with the same inputs, or `fan_out` with the same arguments), and a stopped
Recipe block's footer shows the user the same command. Re-running it opens a **new block** for the
new run and leaves the stopped block frozen as it ended; this re-run is distinct from a session
resume (`--resume`, `--continue`, `/sessions`), after which a replayed stopped block still shows its
command (ADR 0088 D3 and ADR 0090 D1, amended 2026-09-29).
_Avoid_: "run" (one child's execution — a workflow is made of runs), "job", "batch" (a count of
items per child, not the whole), "sweep", "pipeline".

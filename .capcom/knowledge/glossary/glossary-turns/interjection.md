---
title: "Interjection: a message the human sends into a running Exchange"
---

**Interjection**:
A message the human interjects into a **running** Exchange — the remark that reaches the model
mid-task ("also check the tests") instead of waiting for the Exchange to end. It is committed
history, not a request-scoped injection: `Agent.Interject` appends it as a real `RoleUser`
message at a **between-Steps boundary** — the class of engine call the worker's `Snapshot`
already occupies, where the driving goroutine owns the conversation and the boundary is the
synchronization, no mutex — so it survives Turns, compaction, and session save/restore. It
carries `Message.Interjected`, and the derived Exchange **opening skips it**, so the remark
joins the running Exchange's body rather than starting a new one and every Reaction reading the
boundary keeps seeing the whole task as shared context. It reaches the model after the tool
results already in the tail (legal OpenAI chat; strict Gemma-class templates are a model-profile
concern, ADR 0025). A message typed while the model works is **staged** (queued for the next
boundary), and a queue left standing by Esc or a loop error is **held** (nothing auto-sends after
a stop; the next ⏎ sends it, Backspace on an empty box pops the newest back into the editor, and
with nothing queued drops the newest pending image — an image attached for the next idle send, which
a staged message never takes).
A staged message **waits for the wave**: one sent with ⏎ while a **Sub-agent** delegation group
runs lets every member finish, the ones still queued behind the cap included, and lands at the
boundary their results close. Only a message staged **now** — with `ctrl+g` while a turn runs, in
the main prompt or a **Run view** (idle, `ctrl+g` submits like ⏎) — **pre-empts** the members not
started yet: the engine reads the staging as a predicate (`Config.InterjectionPending`, the host's
mailbox answering yes only while a now row is staged) the instant it is about to start one, skips
it with an explicit tool result instead, lets the children already running finish, and lands the
message at the boundary their results close — the boundary is unmoved, it just comes sooner. While
a group has queued members the queued readout says `after the wave · ctrl+g sends now` (ADR 0025,
amended 2026-09-14 and 2026-10-05; a `/schedule` Firing keeps waiting for a quiescent host, ADR
0033 D7).
Staged and held rows are session-ephemeral — sessions record what was committed (ADR 0022).
Mid-run delivery is 1:1 (one row, one marked message); a flush at idle joins the rows into ONE
**unmarked** message, because exactly one unmarked user message opens an Exchange. A **Skill** or
a **File reference** named in a staged message is message content and rides the Interjection with
it; a `/command` is not message content, and which ones may run mid-run is a **per-command**
policy rather than a blanket "at idle" rule — the reporting verbs (`/version`, `/help`, `/skills`,
`/usage`, `/inspect`, `/advice`, `/thinking`, `/workflows`, `/confine` status), `/effort`, the
Schedule pair (`/schedule`, `/schedule-stop`, which touch only the scheduler library and never this
session's engine — ADR 0033) and `/sub-agents-server` (it moves only where the next delegation
spawns, ADR 0045) — the verbs `whileRunning` marks in internal/tui/command.go — run immediately,
every other verb is offered *tagged* `— runs at idle` in the menu and, typed or
accepted mid-run, is **queued**: it shows as a `queued command: /verb` row in the band above the
box (below the staged messages; Backspace on an empty box pops it back first), and the queue runs
FIFO at the next idle — a completion, a stop, or an error's dismissal — before any held or staged
message is sent, so a queued `/clear` clears before a queued message lands (ADR 0025 decisions 7
and 10, amended 2026-09-14, superseding ADR 0027 decision 6's refusal). The queue is the host's
alone — the engine learns nothing of a queued command until it runs (ADR 0031). See
ADR 0025.
The same message shape reaches a **Sub-agent**. Inside its **Run view** the prompt box addresses
that child: `Agent.InterjectChild` queues the message into the child's own engine-side mailbox and
the goroutine driving that child delivers it at its next between-Steps boundary, so what lands is
an ordinary Interjection in the child's conversation with the child's tools, mode and Confinement
unchanged. It is ADR 0025's boundary unmoved, and the child is the one exception to that ADR's
"drive `Step` yourself" advice — nobody else can drive a child's Steps — so the two options it
rejected (a Run drain, an interjection Event) are superseded for **depth > 0 only**
(ADR 0063). A staged row addressed to
a child names its run (`queued for <name> — …`), and the child's own delivery report is what takes
it off the band: a message that landed becomes that child's user block inside its run, and one that
did not becomes a note naming why — `<name> finished before your message landed`, `<name> was
capped before your message landed`, `<name> was stopped by you before your message landed` (a stop
found the child running — the human's stop of that one child, or the cancel of the whole Turn), `<name>
failed before your message landed`, `<name> was cancelled before your message landed` (the child
ended cancelled without a stop finding it running), or `<name>
could not take your message` where the child, still running, refused it at the boundary.
_Avoid_: "steering" / "steer" (ADR 0014's guided-decomposition sense — a Mechanism shaping the
model's own primary call, not a human speaking), "scheduled message" (nothing is clock-timed;
it means deliver-at-the-next-boundary), "queued input" alone (the queue is the staging, the
Interjection is the message).

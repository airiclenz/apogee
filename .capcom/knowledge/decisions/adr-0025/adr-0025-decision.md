---
title: ADR 0025 decision
---

**1. The noun is *Interjection*, and it is the human's, not a Mechanism's.** An Interjection is a
message the human interjects into a *running* Exchange: committed history, delivered at a
between-Steps boundary, marked so the Exchange derivation skips it. The verb is `Agent.Interject`;
the things waiting to go out are *staged* (mid-run) or *held* (after a stop). It is deliberately
**not** spelled "steer" — ADR 0014
owns that word for guided decomposition steering the model's own primary call, and one word for a
Mechanism's nudge and a human's remark would blur two different actors. It is not "scheduled"
either: the issue's word means *deliver when possible*, and nothing clock-timed ships.

**2. Delivery is ASAP, INTO the running Exchange.** One queue. A message entered while the model
works is delivered at the **next tool-round boundary**, so the model reads it mid-task, with the
remaining Turns still available to act on it. If the model is already writing its final answer there
is no next boundary, so the message arrives at Exchange end and opens a new one. That is the whole
of "scheduled": queue, and deliver at the first boundary that exists.

> **Amended 2026-09-14 — a staged message PRE-EMPTS the delegations not yet started.** The boundary
> is unmoved: the message still commits only where this decision puts it. What changes is how soon
> that boundary comes when a delegation group is running. Before, a message queued while the model
> had fanned out three sub-agents waited for all three — the ones still queued behind the
> `parallel-agents` cap included — because the group was one tool round. Now the staging is also a
> **predicate** the engine reads: `Config.InterjectionPending` (answered by the host's mailbox at
> depth 0; a child's own mailbox answers for its grandchildren at depth > 0) is consulted the instant a
> pool worker — or the serial dispatch — dequeues a delegation, and a slot that finds it true is
> **skipped rather than run**: committed in call order like a refused slot, with the error-shaped
> tool result `sub-agent not started: the user sent a message while this group was running; delegate
> again if the task is still needed` and a finished lifecycle phase (no started phase, no audit entry),
> so the model is told and may delegate again. The children **already running finish, untouched** —
> a message never cancels anything (ADR 0013 D5's rollback stays the only cancel) — and the message
> lands at the boundary their results close, as an ordinary Interjection. The seam is a predicate and
> nothing more: the engine never reads the message through it, never drains the host's queue, and
> commits nothing before the boundary. `/schedule` Firings keep waiting for a quiescent host
> (ADR 0033 D7 stands).
> Ratified 2026-09-14, implemented by `docs/plans/2026-09-14 - 01`; ADR 0039 is amended the same day.

> **Amended 2026-10-05 — a message waits for the wave unless it is sent *now*.** The 2026-09-14
> amendment above is narrowed, not reversed. A message staged with ⏎ while a `sub_agent` wave runs
> no longer pre-empts anything: it waits for the **whole wave** — the members still queued behind
> the `parallel-agents` cap included — and lands at the boundary the wave's results close, as an
> ordinary Interjection. Only a message staged with **`ctrl+g`** — the *now* send, offered while a
> turn runs in the main prompt and in a Run view (idle, `ctrl+g` submits like ⏎) — keeps the
> pre-empt: the queued members it finds are skipped exactly as that amendment describes, and the
> running ones still finish untouched. The seam keeps its shape: `Config.InterjectionPending` stays
> a yes/no the Driver answers (ADR 0031:
> the engine never reads the queue, nor learns which key staged a row), and the TUI's Bridge now
> answers yes only while a *now* row is staged. The rule holds at every depth: a child's own mailbox
> makes its serial dispatch skip its unstarted grandchildren only while it holds a message sent now
> — `Agent.InterjectChildNow(runID, in)`, a sibling of `Agent.InterjectChild(runID, in)`, which
> stays the ordinary send — and the `workflow` tool's `message` action, the model's own send to a
> running child, is never a now send, so the model never pre-empts. What the human sees: while a
> group has queued members the queued readout adds `after the wave · ctrl+g sends now`, and the
> armed-esc hint offers `ctrl+g`, not ⏎, as the way to skip the queued members. A member a now
> message skipped reads the neutral `not started · your message` verdict, and one a turn cancel
> caught queued reads `stopped by you` — both in the marker tone, no ✓, never the red `error`, while
> their wire results keep `IsError: true`; a session replay re-derives the verdict from the stored
> result, so an old session's skipped rows read neutral too. The fan-out ceiling's refusal stays
> `error`. The Driver's source for the queued count is `domain.SubAgentGroupEvent`, which the
> engine emits once before the first delegation of a reply's group of two or more is prepared,
> carrying the group's size (capped at the fan-out ceiling) and its run width at every width. A
> serial group (`parallel-agents: 1`) draws its members one at a time, so its own events alone
> could never show the members still to run. The event is in-process only and writes no NDJSON
> line, so the engine stays wire-silent (added by `docs/plans/2026-10-05 - 01`). Ratified
> 2026-10-05, implemented by `docs/plans/2026-10-05 - 00`; ADR 0039 is amended the same day.

> **Amended 2026-10-17 (ADR 0103, `adr-0025-amend-1017`).** "Writes no NDJSON line" no longer
> holds: the Driver's encoder writes a `sub_agent_group` line for this event. The engine stays
> wire-silent.

**3. Three parties, split by what each one owns.** The staging is the TUI's, the delivery is the
worker's, the commit is the engine's — and the split follows ownership, not convenience:

- **The TUI stages.** Display rows, the hold-on-stop rule, the Backspace pop back into the editor,
  the `N queued` readout: all of it is UI. The engine never reads a message before it is
  delivered — since the 2026-09-14 amendment above it may ask whether one is *pending*
  (`Config.InterjectionPending`, a yes/no the Bridge answers over the live mailbox — since the
  2026-10-05 amendment, yes only for a message sent *now*), and that answer is all it learns; the message itself stays the TUI's until the worker delivers it.
- **The worker drains.** A per-Exchange mailbox (`interjectBox`) is written by the Update goroutine
  and read by the worker between Steps. It is the ONE place the two goroutines touch shared state
  — and, since 2026-09-14, the one place the engine's dispatching goroutine and its pool workers
  touch it too, though only through the `pending` predicate, never a row — so it carries the one
  real mutex in the delivery path (the Bridge's registration of the live box, added with the
  predicate, guards a pointer, not a row), and it is held **by pointer** on the
  value-copied `Model` (ADR 0011's no-copy invariant — a `sync.Mutex` copied by value would hand
  each copy its own lock and unsynchronise the two goroutines *silently*).
- **The engine commits.** `Agent.Interject(domain.UserInput) error` appends one marked user message,
  resolving attached skills and `@file` references **at delivery** so the model reads a file as it
  stands then, not as it stood when the remark was typed. *Amended 2026-09-15 (plan
  `2026-09-15 - 00`, item 16):* the signature is `Interject(ctx context.Context, in
  domain.UserInput) error` — the caller passes the context of the Step it is about to make, and it
  bounds **only** the reference resolution (a cancelled Step stops a document extraction
  mid-walk, as it already did for a Submitted message). The return contract is unchanged: a
  cut-short reference is skipped with the same `ErrorEvent`, the remark still commits, and a
  cancel is not a third refusal.

**4. `Interject` names a THIRD engine-call class: between-Steps calls by the driving goroutine.**
It takes **no new mutex**, and that is a decision, not an omission. The class already existed in the
tree: the worker calls `eng.Snapshot()` between Steps for the per-Turn session save
(ADR 0022), on the goroutine
that drives `Step`, and it is safe for a structural reason — between Steps that goroutine is the
engine's single driver, so the boundary IS the synchronization. `Interject` occupies exactly that
class and inherits exactly that argument.

So ADR 0011's closing rule now reads with three members, and a consumer must copy all three:

| Class | Who calls | When | Guard |
|---|---|---|---|
| Idle-only | `Update` goroutine | no worker in flight | the state machine |
| `SetMode`-class | any goroutine | any time | a documented mutex on one field |
| **Between-Steps** | **the goroutine driving `Step`** | **between Steps of the Exchange it drives** | **the boundary itself** |

The distinction that makes the third class safe and keeps it narrow: the driving goroutine already
owns the conversation there. `Interject` is deliberately NOT promoted to the `SetMode` class — that
class guards a single scalar behind an `RWMutex`, and appending to history is not a scalar write.
Calling `Interject` while a `Step` is in flight races the loop's own history writes, and its doc
comment says so.

**5. The message is committed and MARKED; the derivation skips it in exactly one place.**
`domain.Message` gains an exported `Interjected bool` (`json:"interjected,omitempty"`), set only by
`Agent.Interject`. The Exchange opening becomes *the last non-interjected `RoleUser` message*, and
the skip lives at ONE site: `lastExchangeOpening`, a one-line caller of the domain's single backward
scan (`lastMatchIndex`), read by `CurrentExchange` — and therefore by every Mechanism — and by
`Request.InjectContext`, which anchors its insert on the opening for the same reason. So an
interjection joins the running Exchange's *body*; the boundary does not move, the shared task context
every Mechanism reads stays whole, and a request-scoped injection still lands immediately before the
ask rather than above the remark.

`conversationView.LastUser` deliberately does **not** skip: a Mechanism asking "what did the human
last say" wants the remark. Two questions, two locators, both documented as such.

Persistence is free. `Message` marshals through `messageJSON` with unknown-sibling passthrough, so
one `omitempty` field and one `messageKnownKeys` entry round-trip a session with **no
`SessionVersion` bump** — an old snapshot lacks the key and decodes `false`; an old binary preserves
it as an unknown sibling. The wire projection (`toProviderRequest`) maps fields explicitly, so the
marker never leaves the process.

**6. The wire posture: user-after-tool is accepted, and its risk is a model-profile concern.** At a
tool-round boundary the conversation tail is `assistant(tool_calls), tool…`, so an interjection
reaches the model as a user message *after* tool results. That is legal OpenAI chat and is what makes
mid-task delivery possible at all. It is also known to break **strict Gemma-class chat templates**,
which is precisely why `InjectContext` routes around it for its request-scoped inserts. The owner
accepts the trade: the value of the feature is the mid-task delivery, and if a template ever refuses
the shape it is the **model-profile layer's** problem to absorb (the layer that already rewrites how
a non-native model is told about tools), not a reason to redesign history. Explicitly deferred, not
overlooked.

**7. Stop and error HOLD the queue; only a natural completion flushes it.** Esc means stop
*everything*, including what was waiting to go out. After a cancel or a loop error the staged rows
stay put under a one-time note (`N queued messages held — ⏎ sends them`); the next `⏎` — even on an
empty box — sends them, and Backspace on an empty box pops the newest back into the editor,
editable (with nothing queued it drops the newest pending image instead — amended 2026-10-03,
below). A natural completion (`exchangeDoneMsg`, and `compactDoneMsg`, which is a completion too)
auto-sends what is left, because the human pressed `⏎` on those rows and has no reason to expect a
second keypress is needed. A deferred quit beats a flush outright.

> **Amended 2026-09-14 — the hold is a hold on MESSAGES.** The commands queued while the worker ran
> (decision 10, amended the same day) are not held by a stop or a fault: they run at that very idle,
> FIFO, before the `N queued messages held` note is stated — so Esc×2 does not drop a queued `/clear`,
> it runs it — while the staged messages stay held for the next `⏎` exactly as before. The
> errored → idle `⏎` dismissal drains the queue the same way. A natural completion runs the queued
> commands first and then auto-sends what is staged, unless a queued verb opened a worker of its own,
> in which case the flush waits for that worker's fold.

**8. Mid-run delivery is 1:1; an idle flush joins into ONE message.** Each row delivered at a
boundary becomes its own marked user message, so the transcript↔history mapping is exact and the
scrollback records what the model saw *and when*. A flush instead joins every held row's text with
blank lines (oldest first, the editor's own text last when a non-empty `⏎` triggered it) into ONE
**unmarked** user message. That asymmetry is load-bearing in both directions: exactly one unmarked
user message opens the next Exchange, so the derived boundary stays trivially correct, and every
Mechanism reading it sees the whole request as the shared context it is.

**9. Staged rows are session-ephemeral.** Sessions record what was **committed**
(ADR 0022); an undelivered row
is outgoing input, not context, and dies with the process. For the same reason `/clear` at idle
**keeps** held rows while wiping the conversation, and quitting with rows staged just exits.

**10. Typing is admitted by EDITABILITY, one predicate, keyboard and mouse alike.** `inputEditable()`
— idle, ask, and now running — is the routing class for keys, paste, and mouse-caret placement
together, so the keyboard and the mouse can never disagree about which states are live. At
`stateAwaitingApproval` and `stateErrored` the box stays inert: a/d/s and Enter-dismiss own the
keyboard there. Commands never queue — a `/command` typed while running earns a transcript note
(`commands run at idle — not queued`) and keeps the human's line in the box. The `@file`
autocomplete works while running (refs are useful in a remark); the `/` and `/skill` pickers stay
idle-only, because offering a command that would be refused misleads.

> **Amended 2026-07-28 by ADR 0027
> (decision 6).** The second clause no longer holds: **every** completion region opens while the
> model works, including `/`. What would have misled is answered by *tagging* the row `— idle only`
> rather than hiding the namespace, and the per-command `whileRunning` policy lets the reporting
> verbs (`/version`, `/skills`, `/confine` status) actually run mid-run. The rest of this decision
> stands unchanged — commands still never queue, and an idle-only verb still earns
> `commands run at idle — not queued` with the human's line left in the box. ADR 0027 also repairs
> a defect on this path: a staged interjection carries its `SkillIDs` instead of dropping them.

> **Amended 2026-09-14 — commands QUEUE.** The "commands never queue" clause of this decision, and
> the 2026-07-28 block's restatement of it, no longer hold; the same amendment retires
> ADR 0027 decision 6's refusal clause
> ("accepting it prints `commandsAtIdleNote` with the draft untouched") and the matching sentence
> of its *What this amends in ADR 0025* section. An idle-only verb typed while a worker works — any
> verb without `whileRunning` (`/clear`, `/color-scheme`, `/compact`, `/continue`, `/model`, `/new`,
> `/redo`, `/rename`, `/server`, `/sessions`, `/settings`, `/stop-server`, `/undo`,
> `/unload-model`) — is **queued** rather than refused: the parsed line joins
> `Model.deferredCommands`, the band above the box paints it as its own row
> (`queued command: /verb`, below the staged messages, so it is always the row nearest the box),
> the menu's tag reads `— runs at idle`, and the queue drains FIFO through the ordinary command
> path at the next idle — a natural completion, a stop, or the errored → idle dismissal — **before**
> any held or staged message is sent, so a queued `/clear` clears before a queued message lands. A
> verb that opens a worker of its own (`/compact`, `/continue`) stops the drain, and what is left
> waits for that worker's own terminal fold. Backspace on an empty box pops the newest queued command
> back into the editor first (it is nearest the box), then the newest staged message, and only with
> nothing queued drops the newest pending image (amended 2026-10-03, below); the reporting
> verbs still run on the spot; a line that could not run even at idle (a parse error) is answered by
> its usage note at once rather than deferred. The queue is the host's own bookkeeping — the engine
> learns nothing of a queued command until it runs (ADR 0031) — and it is session-ephemeral like the
> staged rows (decision 9): a deferred quit runs nothing. The dropdown accept queues the bare verb
> cut out of the draft and leaves the rest of the message verbatim, exactly as it would have run it.

**11. The one deliberate behavioural loss: the single-key transcript scroll while running.** Keys
that used to scroll the transcript mid-run (j/k/space) now type. PgUp/PgDn and the mouse wheel keep
scrolling in every state. This is the plan's one accepted regression and it is the direct cost of
the feature.

---
Status: accepted
---

# Interjections commit at the between-Steps boundary

## Context

Apogee's prompt box was dead while the model worked. `handleKey` fed the textarea only at
`stateIdle` and `stateAwaitingAsk`; every other key while busy was routed to `scrollViewport`,
`Enter`'s running branch was an explicit no-op, and a paste was dropped. So the human waiting on a
five-Turn task could not even *begin* the next message, let alone say "also check the tests" while
there was still a test-checking Turn left to spend it on. The owner's report was two wishes in one
sentence: *"I cannot start writing the next prompt when the model is working"* and *"send off
messages to the model while it is working — scheduled messages … sent when possible even when the
model is still working"* (ISSUES items 1–2, this repo).

The engine was equally closed. `Agent.Submit` refuses mid-Exchange with `ErrInputPending`, its
single-slot `pendingInput` is consumed at the top of `step()`, and the Agent's own contract is blunt:
drive it from ONE goroutine, and the only anytime-goroutine-safe mutators are `SetMode` /
`SetConfineToWorkspace` behind sibling mutexes. [ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md)'s
closing rule names exactly two legal call classes for the host: idle-only calls guarded by the state
machine (`ClearContext`, `Compact`, `AbortExchange`, `Snapshot`, and — since
[ADR 0024](0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md) — `Rebind`),
and mid-`Step` calls behind a `SetMode`-class mutex. A mid-run message fits neither.

Three existing mechanisms look like they could carry the message, and none of them can:

- **`Request.InjectContext`** already knows about user-after-tool and routes *around* it. It is
  **request-scoped**: never committed, gone after one request. A remark that vanishes the moment the
  Turn that carried it ends is not what "I told the model something" means.
- **The deferred-injection pipe** ([ADR 0017](0017-the-exchange-is-a-derived-domain-working-value.md) F6)
  is **Exchange-scoped by contract** — `closeExchange` clears it — and request-scoped on delivery. An
  interjection must outlive both: it is history the next compaction summarises and the next session
  restores.
- **The derived Exchange opening** is `the last RoleUser message`, and
  [ADR 0017](0017-the-exchange-is-a-derived-domain-working-value.md) states plainly *why* that is
  stable: nothing commits a user message mid-Exchange. Committing one naively would move the opening
  to the remark, collapsing the Exchange to the remark alone — and roughly nine Mechanism call sites
  read that boundary (guided decomposition, decompose, library, cot, empty-response, tool-use
  enforcer, filehint, toolfilter), several of them relying on it as the shared task context.

This ADR records the design the owner ratified in the 2026-07-27 grill (six decisions, plus three
more in a second grill the same day), implemented by
`docs/plans/2026-07-27 - 01 - interjection-and-terminal-cursor-plan.md`. It supersedes nothing. It
extends ADR 0011's engine-call taxonomy by naming a third class that was already in the tree
unnamed, and it amends ADR 0017's stability argument with the one exception that argument now has.

## Decision

**1. The noun is *Interjection*, and it is the human's, not a Mechanism's.** An Interjection is a
message the human interjects into a *running* Exchange: committed history, delivered at a
between-Steps boundary, marked so the Exchange derivation skips it. The verb is `Agent.Interject`;
the things waiting to go out are *staged* (mid-run) or *held* (after a stop). It is deliberately
**not** spelled "steer" — [ADR 0014](0014-guided-decomposition-steers-the-primary-call-and-serializes-delegation.md)
owns that word for guided decomposition steering the model's own primary call, and one word for a
Mechanism's nudge and a human's remark would blur two different actors. It is not "scheduled"
either: the issue's word means *deliver when possible*, and nothing clock-timed ships.

**2. Delivery is ASAP, INTO the running Exchange.** One queue. A message entered while the model
works is delivered at the **next tool-round boundary**, so the model reads it mid-task, with the
remaining Turns still available to act on it. If the model is already writing its final answer there
is no next boundary, so the message arrives at Exchange end and opens a new one. That is the whole
of "scheduled": queue, and deliver at the first boundary that exists.

**3. Three parties, split by what each one owns.** The staging is the TUI's, the delivery is the
worker's, the commit is the engine's — and the split follows ownership, not convenience:

- **The TUI stages.** Display rows, the hold-on-stop rule, the Backspace pop back into the editor,
  the `N queued` readout: all of it is UI. The engine never learns a message exists until it is
  delivered.
- **The worker drains.** A per-Exchange mailbox (`interjectBox`) is written by the Update goroutine
  and read by the worker between Steps. It is the ONE place the two goroutines touch shared state,
  so it carries the one real mutex in the whole feature, and it is held **by pointer** on the
  value-copied `Model` (ADR 0011's no-copy invariant — a `sync.Mutex` copied by value would hand
  each copy its own lock and unsynchronise the two goroutines *silently*).
- **The engine commits.** `Agent.Interject(domain.UserInput) error` appends one marked user message,
  resolving attached skills and `@file` references **at delivery** so the model reads a file as it
  stands then, not as it stood when the remark was typed.

**4. `Interject` names a THIRD engine-call class: between-Steps calls by the driving goroutine.**
It takes **no new mutex**, and that is a decision, not an omission. The class already existed in the
tree: the worker calls `eng.Snapshot()` between Steps for the per-Turn session save
([ADR 0022](0022-sessions-persist-per-turn-as-dual-representation-records.md)), on the goroutine
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
editable. A natural completion (`exchangeDoneMsg`, and `compactDoneMsg`, which is a completion too)
auto-sends what is left, because the human pressed `⏎` on those rows and has no reason to expect a
second keypress is needed. A deferred quit beats a flush outright.

**8. Mid-run delivery is 1:1; an idle flush joins into ONE message.** Each row delivered at a
boundary becomes its own marked user message, so the transcript↔history mapping is exact and the
scrollback records what the model saw *and when*. A flush instead joins every held row's text with
blank lines (oldest first, the editor's own text last when a non-empty `⏎` triggered it) into ONE
**unmarked** user message. That asymmetry is load-bearing in both directions: exactly one unmarked
user message opens the next Exchange, so the derived boundary stays trivially correct, and every
Mechanism reading it sees the whole request as the shared context it is.

**9. Staged rows are session-ephemeral.** Sessions record what was **committed**
([ADR 0022](0022-sessions-persist-per-turn-as-dual-representation-records.md)); an undelivered row
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

> **Amended 2026-07-28 by [ADR 0027](0027-one-slash-namespace-with-inline-skill-tokens.md)
> (decision 6).** The second clause no longer holds: **every** completion region opens while the
> model works, including `/`. What would have misled is answered by *tagging* the row `— idle only`
> rather than hiding the namespace, and the per-command `whileRunning` policy lets the reporting
> verbs (`/version`, `/skills`, `/confine` status) actually run mid-run. The rest of this decision
> stands unchanged — commands still never queue, and an idle-only verb still earns
> `commands run at idle — not queued` with the human's line left in the box. ADR 0027 also repairs
> a defect on this path: a staged interjection carries its `SkillIDs` instead of dropping them.

**11. The one deliberate behavioural loss: the single-key transcript scroll while running.** Keys
that used to scroll the transcript mid-run (j/k/space) now type. PgUp/PgDn and the mouse wheel keep
scrolling in every state. This is the plan's one accepted regression and it is the direct cost of
the feature.

## Considered options

- **Reuse the Exchange-scoped deferred-injection pipe (ADR 0017 F6).** Rejected on two counts, each
  fatal: it is cleared by `closeExchange`, and it delivers request-scoped. An interjection must
  survive the Exchange, the compaction that summarises it, and the session file that stores it.
- **Keep it request-scoped like `InjectContext`.** Rejected — the same lifetime problem, plus the
  transcript would show a message the history does not contain, which is the dishonesty the whole
  design is arranged against.
- **A second `pendingInput` slot on the Agent, consumed by `step()`.** Rejected: it puts the queue
  in the engine, where the hold-on-stop rule, the pop-back-to-editor and the display rows have no
  business being, and it would deliver a Turn later than the boundary does.
- **Give `Interject` a mutex and let the `Update` goroutine call it any time.** Rejected. A lock over
  `Conversation` would have to be taken by every reader in the hot loop to be worth anything, and it
  would still be *wrong*: a message appended mid-`Step` could land after the request was built,
  reaching the model a Turn late or not at all. The boundary is both cheaper and stronger — the same
  argument ADR 0024 made for idle-only rebind.
- **Deliver an unmarked user message and let the Exchange re-open.** Rejected: it collapses the
  derived Exchange to the remark and strips the shared task context out from under nine Mechanism
  call sites, mid-task. The marker exists to make the honest history and the stable boundary
  compatible.
- **Cache the Exchange opening instead of marking messages.** Rejected — ADR 0017's whole point is
  that the Exchange is *derived*; the one cached boundary (the abort rollback) is a recorded
  exception, not a pattern to grow.
- **Bump `SessionVersion` for the marker.** Rejected as unnecessary: the unknown-sibling passthrough
  and `omitempty` make the field bidirectionally compatible already, and a version bump would refuse
  sessions that are perfectly readable.
- **Auto-send the queue after Esc too.** Rejected by owner decision 2: Esc must mean stop, without
  a footnote. A held queue that needs one `⏎` is a smaller surprise than a stop that sends three
  messages.
- **A new engine `Event` for interjections.** Rejected for now — the TUI notifies itself
  (`interjectedMsg`), and an `events.go` variant stays additively available if an embedder ever wants
  the observability.
- **A drain hook on `Agent.Run` for embedders.** Rejected: `Run` is the convenience loop, and an
  embedder who wants mid-run interjection drives `Step` themselves, which is exactly what the TUI
  does. `Run`'s doc says so.
- **Clock-timed scheduling ("send at 15:00").** Rejected — a misreading of the issue's word
  "scheduled", and a timer is a feature nobody asked for.
- **Freeze transcript repaints while a drag is held**, to keep a selection alive across the stream.
  Rejected in the second grill: the stream would visibly stall under every drag. The
  keep-if-unchanged rule (below) achieves the same guarantee without stopping anything.

## Consequences

- **The public Go surface gains `Agent.Interject(domain.UserInput) error`** — published for free
  through the `apogee.Agent` alias — plus the exported `domain.Message.Interjected` field and the
  `domain.ErrNoOpenExchange` sentinel, re-exported at the root and pinned by `example_test.go`'s
  completeness guard. All additive ([ADR 0010](0010-package-layout-domain-core-and-thin-root-facade.md)),
  so a **minor** bump; an embedder who never calls it is byte-identical to before. The internal
  `tui.Engine` seam gains the method too.
- **`Interject` is refusable, and both refusals are honest.** No open Exchange ⇒
  `domain.ErrNoOpenExchange` (the caller wanted `Submit`); an input carrying no text, refs or skills
  ⇒ an unexported empty-interjection error, because a blank remark would land as an empty user
  message wedged after the tool results. On either refusal the conversation is untouched.
- **Delivery has fates, and they are documented rather than defended against.** An interjection
  survives a **cancelled Turn** (the rollback boundary is armed after the between-Steps window) and
  rides snapshot/resume with its marker intact. `AbortExchange` **discards** it with the rest of the
  scrapped Exchange — which is the point: the human threw the Exchange away. The transcript keeps the
  visual record either way.

  > **Amended 2026-08-02.** "Discards it with the rest of the scrapped Exchange" was the engine's
  > half of the fate, and the TUI never wrote the other half: a row the worker delivered a moment
  > before an Esc was dropped from the conversation by `AbortExchange` *and* was already off the
  > queue (the delivery report took it), so it was sent by nobody and held by nobody — surviving only
  > as a `⧖` transcript entry claiming a delivery the model no longer remembered. That contradicted
  > decision 7 ("Esc stops everything, **including what was waiting to go out**"), which only ever
  > held for rows the worker had not reached yet. Two changes make the hold total, and neither moves
  > a decision above: the worker **skips the drain outright when its ctx is already cancelled**
  > (`deliverInterjections`), so rows at a doomed boundary stay in the mailbox the display queue
  > mirrors; and the `cancelledMsg` fold **re-stages what this Exchange did deliver**
  > (`Model.restageDelivered`), ahead of what is still queued, where the hold note counts it and the
  > next `⏎` sends it. The engine's fate is unchanged — the Exchange is still scrapped whole — and so
  > is the transcript, which keeps the `⧖` record beside the `cancelled` note exactly as it keeps a
  > stopped Turn's streamed partial. A natural completion is untouched: past `finishWorker` a
  > delivered row is committed history and no later stop can resurrect it (audit 2026-08-01).
  >
  > **Amended 2026-08-03 (owner ruling).** The **restage half** of the amendment above is reversed;
  > the drain-skip half stands. Living with it showed the two surfaces contradicting each other: the
  > `⧖` block said *the model saw this*, while the staged band and the hold note beside it said
  > *waiting to be sent* — and the next `⏎` then re-sent a message the human had watched being read.
  > The ruling is **sent is sent**. A row the worker delivered before the Esc **dies with the
  > scrapped Exchange** — the engine's fate was always exactly this — survives as the `⧖` transcript
  > record, and is **not** re-staged (`Model.restageDelivered` is gone). So decision 7's hold covers
  > exactly the rows the worker never delivered, which is the reading it always had before
  > 2026-08-02: the queue holds what the model never received, and nothing else. The drain-skip in
  > `deliverInterjections` stays and earns its keep on this reading too — it does not compensate for
  > the fate, it **narrows** it, keeping rows out of an Exchange that is already doomed wherever the
  > cancel is visible in time. A natural completion remains untouched.
- **A row is never silently lost and never delivered twice.** The drain is unconditional and the
  Model's display copy is the queue of record: a row the delivery report does not name stays staged
  and goes out at the terminal boundary instead. The Backspace pop withdraws from the mailbox first
  and gives up if the worker already drained the row — handing the human an editor copy of a message
  the model has just read would invite sending it twice. *(Qualified by the 2026-08-03 amendment
  above: a row delivered into an Exchange the human then scraps is dropped **with** that Exchange —
  visibly, as the `⧖` record standing beside the `cancelled` note, which is not silent loss.)*
- **The drain waits for the Exchange to OPEN.** `Submit` only sets `pendingInput`; the Exchange opens
  inside the first `Step`. So the Submit path deliberately skips the drain before that Step, while
  the resume path (`/continue` into a restored Exchange) drains immediately, because its Exchange is
  already open. Without the distinction a row staged in that window would be drained, refused with
  `ErrNoOpenExchange`, and overtaken by a row staged later — the FIFO order the human typed in,
  broken by a one-Step race.
- **Every running Exchange takes a mailbox, and only a running Exchange has one.** `submit`,
  `/continue`'s resume and `/continue`'s canned turn each create a fresh box; `/compact` takes
  **none** (it drives no Exchange) and `finishWorker` clears it. So `m.box` is non-nil exactly while
  a worker that can deliver is running, and a nil box is a documented, usable state — push is a
  no-op, drain yields nothing, and the row reaches the model at the terminal boundary instead.
- **A delivered interjection is a transcript entry of its own kind** (`entryInterjected`, a `⧖`-marked
  user-styled block) and deliberately does **not** join the sticky-header set: the Exchange's
  *opening* prompt stays the pinned context. The kind is additive within the existing transcript
  codec version — an older build skips a kind it does not know — so no bump there either.
- **The staged-row strip is capped at three visible rows** under a `… N more queued` marker. The
  strip steals its height from the transcript viewport, so an unbounded queue would squeeze the
  conversation off the screen; the newest rows are the ones shown, because the nearest row is the one
  Backspace takes back.
- **`blockedUpstream` still guards the three paths a HUMAN opens an Exchange with** — a message,
  `/continue`, `/compact`. The auto-flush is a fourth opener and is deliberately not gated: it runs
  inside a natural completion's own fold, where the heartbeat's own rule (a failed beat during an
  in-flight Exchange is ignored) means the offline state cannot have moved since the Exchange that
  just completed was allowed to start. Recorded because it is an invariant, not a coincidence: if the
  offline debounce ever becomes able to cross mid-Exchange, the flush needs the guard.
- **Selection got its own answer, and it needed no ADR of its own.** ISSUES item 3 ("I cannot select
  text while the model is working") rode this plan: a transcript selection now survives a repaint by
  the **keep-if-unchanged** rule — it lives exactly while every rendered line it spans is identical
  before and after, and drops the moment the text under it moves. So copy always equals sight, by
  construction, since the release slices the very lines the rule protected. Transcript selection
  works in **every** state; prompt selection follows **editability** (decision 10), which is why it
  arrived with the typing work rather than beside it. The rule lives in `internal/tui/mouse.go`'s
  header narration and in the CHANGELOG; it decides only *when* the existing selection machinery lets
  go.
- **CONTEXT.md gains one term — *Interjection*** — cross-referenced against ADR 0014's "steer" in
  both directions, so the two senses cannot blur.
- **The bench and every headless embedder are untouched.** They drive `Step` and never call
  `Interject`; nothing in the loop, the request builder or the wire changed shape for them.

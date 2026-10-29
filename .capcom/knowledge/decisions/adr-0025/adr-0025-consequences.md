---
title: ADR 0025 consequences
---

- **The public Go surface gains `Agent.Interject(domain.UserInput) error`** (since 2026-09-15
  `Interject(ctx, in)`, per the amendment in decision 3) — published for free
  through the `apogee.Agent` alias — plus the exported `domain.Message.Interjected` field and the
  `domain.ErrNoOpenExchange` sentinel, re-exported at the root and pinned by `example_test.go`'s
  completeness guard. All additive (ADR 0010),
  so a **minor** bump; an embedder who never calls it is byte-identical to before. The internal
  `tui.Engine` seam gains the method too.
- **`Interject` is refusable, and both refusals are honest.** No open Exchange ⇒
  `domain.ErrNoOpenExchange` (the caller wanted `Submit`); an input carrying no text, refs or skills
  ⇒ an unexported empty-interjection error, because a blank remark would land as an empty user
  message wedged after the tool results. On either refusal the conversation is untouched. *(Amended
  2026-10-29 — since widened: images count as content in the emptiness test, and Interject also
  refuses an input that would launch a recipe (errRecipeInterjection, ADR 0087) and images the bound
  server cannot take (checkInputImages). On all four refusals the conversation is still untouched —
  internal/agent/interject.go.)*
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
  >
  > **Amended 2026-09-19 (`apogee-2un`, Stage A).** The engine's fate on Esc is no longer "discards
  > it with the rest of the scrapped Exchange": the TUI's cancel and loop-error folds now close the
  > Exchange with `Agent.SettleExchange`, which **keeps** the Turns that finished before the stop and
  > marks the cut with an ephemeral `[engine — cancelled]` engine note on the last tool result. A
  > row delivered *after* that tool result stays in the conversation with them — so the settled
  > tail reads `[…, tool(note), user(Interjected)]` and the next Submit opens user→user, the tail a
  > faulted Exchange already leaves (`endAbandoned`). The 2026-08-03 ruling therefore holds in a
  > stronger form: **sent is sent**, and a delivered row is now not only *remembered by the
  > transcript* but *kept by the model*. Two cases still drop the row: an Exchange with no finished
  > Turn (the lone opening, or the opening plus the interjection Turn 0's cancel left behind) has
  > no tool result to carry the note and falls through to `AbortExchange`'s rollback; and `/clear`
  > on an interrupted session still calls `AbortExchange` outright — the explicit throw-away. The
  > drain-skip in `deliverInterjections` stays: a row committed at a doomed boundary would now be a
  > delivered message no Turn answers, so keeping it in the mailbox for the next `⏎` is still the
  > better fate. The note is **live-session only** (ADR 0076 D6): a saved record and a resumed
  > conversation hold the kept results and the kept row with no cut marker. Plan `2026-09-19 - 01`.
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
  *(Amended 2026-09-16: the box rides the Model's in-flight `worker` value — `m.worker.box`, written
  by `worker.start` and dropped by `worker.finish` — under the same non-nil-while-running contract.)*
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

---

**Clarified 2026-08-04 — the approval prompt is a menu now.** Decision 10's closing example
("at `stateAwaitingApproval` and `stateErrored` the box stays inert: a/d/s and Enter-dismiss own the
keyboard there") still describes the BOX correctly: neither state is editable, `inputEditable()` is
unchanged, and `a`/`s`/`d` still send their decision on the spot. What has moved is what the
approval pane does with the two keys beside them. It is a menu (`docs/layout/user-questions-layout.md`,
`layout.md`): ↑/↓ walk the highlight over `Allow` / `Always allow this session` / `Deny` / `Cancel`
(amended 2026-10-29: since ADR 0096 also `Always in this project…` (`p`) between the session row and
Deny; a request's own menu drops rows it cannot honour — internal/tui/approval.go `approvalMenu`),
and ⏎ **resolves the highlighted row** rather than dismissing anything — Enter-dismiss is the errored
state's alone. Everything the menu does not claim, PgUp/PgDn included, still falls through to
`scrollViewport`, so the prompt stays as soft-modal as this decision left it.

---

**Amended 2026-10-03 — pending images sit below the queue.** An image attached in the TUI (ctrl+v
off the clipboard, or a bracketed paste that is exactly an image file's path) waits on one line
above the box until the next idle send carries it as `UserInput.Images`. It is not a staged row:
an interjection or a child message is staged text-only and leaves the images pending, and a flush
of held rows carries them only when it is the ⏎ that sends the box. Backspace on an empty box keeps
the order decision 7 and the 2026-09-14 amendment set — the newest queued command, then the newest
staged message — and only with nothing queued drops the newest pending image.

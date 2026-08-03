# Esc drops delivered interjections — sent is sent

- **Goal:** a queued message the worker already delivered into the running Exchange is NOT
  re-queued when Esc cancels that Exchange. It dies with the scrapped Exchange like every other
  message committed into it, surviving only as the transcript's ⧖ record. The queue holds only
  what the model never received. This resolves the ISSUES.md entry "queued messages do not seem
  to be removed from the queue even after they have been sent to the model … when cancelling the
  session (ESC), the queued message is displayed as queued again."
- **Date:** 2026-08-03
- **Status:** not started
- **Authoritative sources:**
  - The owner's ruling of 2026-08-03 (this plan's header IS its record): on Esc, *sent is sent* —
    a delivered interjection is not the queue's to hold. This reverses the **restage half** of ADR
    0025's 2026-08-02 amendment (`Model.restageDelivered`, commit `8b79e2f`). The **drain-skip
    half** of that amendment stays: `deliverInterjections` still skips the drain outright when its
    ctx is already cancelled, so rows are kept out of a doomed Exchange wherever the cancel is
    already visible.
  - ADR 0025 `docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md` — every other
    decision in it is untouched, decision 7's hold-on-stop for **undelivered** rows included.
- **Standing requirements:** forward `skills: coding-standards` when executing this plan.
- **Out of scope:**
  - Any change to `AbortExchange` / Esc-scraps-the-Exchange semantics (`internal/agent/agent.go`)
    — the engine's fate is unchanged.
  - Any transcript surgery on the ⧖ block (the rejected "keep re-queue, fix the display" option) —
    the ⧖ record deliberately stays in the scrollback, exactly as a stopped Turn's streamed
    partial does.
  - The hold behavior for rows the worker never delivered — unchanged (ADR 0025 decision 7).
  - Editing archived plans that mention `deliveredInterjections`
    (`docs/plans/archived/2026-08-01 - 01 - engine-tui-correctness-wave-plan.md`) — archives are
    historical records.
  - Any version-identifier change (VERSION, CHANGELOG release heading, tags).

## 1. Remove the restage machinery from the cancel fold — ✅ DONE (2026-08-03)

**What:** delete the third copy of the queue and the cancel-fold restage, in `internal/tui`:

- `interject.go`: delete `Model.restageDelivered` whole. In `foldInterjected`, delete the
  `m.deliveredInterjections = append(m.deliveredInterjections, items...)` line and rewrite the
  doc comment's closing paragraph ("The rows also go onto deliveredInterjections …"): a folded
  row is committed history; if a stop later scraps the Exchange it was committed into, it dies
  with it — the ⧖ transcript block is the surviving record (sent is sent, owner ruling
  2026-08-03).
- `model.go`: delete the `deliveredInterjections []queuedInterjection` field and its doc
  paragraph ("deliveredInterjections is the third copy …") in the Model struct. In the
  `cancelledMsg` fold, delete the `m.restageDelivered()` call and rewrite the comment block above
  it ("\"Held\" has to cover what the worker already DELIVERED, too. …"): held covers only what
  the worker never delivered; a row it did deliver is dropped from the conversation by the
  AbortExchange above and stays dropped — visible in the scrollback as the ⧖ record beside the
  "cancelled" note, not on the queue. In `finishWorker`, delete the
  `m.deliveredInterjections = nil` line and the comment paragraph that explains it ("The rows
  this Exchange DELIVERED are forgotten here …").
- `worker.go`: rewrite the two `deliverInterjections` doc paragraphs that frame the ctx-cancelled
  skip as "the first half" of a two-half guarantee closed by `Model.restageDelivered`. The skip
  now stands alone: it keeps rows out of an Exchange that is already doomed, so they stay on the
  queue of record; a cancel that lands *after* the check means the committed rows die with the
  scrapped Exchange — the accepted sent-is-sent fate, not a defect to compensate for.
- Nothing else references the field or the method (`grep -rn "deliveredInterjections\|restageDelivered"
  internal/` must come back empty after the change).

**Tests:** in `internal/tui/interject_test.go`:

- Replace `TestCancelRestagesADeliveredRow` with `TestCancelDropsADeliveredRow`, same fixture
  (open an Exchange, stage "also check the tests" and "and the docs", fold
  `interjectedMsg{items: …[:1]}`, then `cancelledMsg{}`), asserting the reversed fate: the queue
  holds exactly ONE row (`"and the docs"` — the undelivered one); the hold note is `heldNote(1)`
  written exactly once; the ⧖ record stays (exactly one `entryInterjected` transcript entry);
  and the follow-up ⏎ on the empty box submits exactly `"and the docs"` — the delivered row is
  not re-sent.
- `TestCancelHoldsWithSingleNote`, `TestNaturalCompletionKeepsDeliveredRowsDelivered`, and
  `TestErrorHolds` must still pass unchanged — they pin the hold for undelivered rows, and the
  no-resurrection rule after a natural completion, which both survive this change.

**Acceptance:**

- `go test ./internal/tui/ -run 'TestCancel|TestNaturalCompletion|TestErrorHolds|TestIdleEnter|TestClearKeeps' -count=1` passes.
- `grep -rn "deliveredInterjections\|restageDelivered" internal/` → no matches.
- `make check` passes.

**Commit:** `fix(tui): a delivered interjection dies with the Exchange Esc scraps`

## 2. Re-amend ADR 0025, retire the ISSUES entry, record the change — ✅ DONE (2026-08-03)

Depends on item 1.

NOTES (2026-08-03): the CHANGELOG entry was written by rewriting the existing `[Unreleased]` →
`### Fixed` bullet ("Stopping with Esc can no longer swallow a message you queued…") rather than
adding a second bullet beside it. That bullet was the never-released 2026-08-02 restage entry, and
its "**put back on the queue**" clause is the one match the item's own acceptance grep expects to
find in its new sense — two bullets would have left `[Unreleased]` telling both stories at once.
Still one entry, and it carries the required wording.

**What:**

- `docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md`: append a dated
  follow-up (**Amended 2026-08-03**) to the 2026-08-02 amendment block in Consequences,
  recording the owner's reversal of its restage half: living with the behavior showed the two
  surfaces contradicting each other — the ⧖ block claiming "the model saw this" while the band
  and hold note claimed "waiting to be sent", and the next ⏎ then re-sent a message the human
  had watched being read. The ruling is *sent is sent*: a row the worker delivered before the
  Esc dies with the scrapped Exchange (the engine's fate was always this), survives as the ⧖
  transcript record, and is NOT re-staged; decision 7's hold now covers exactly the rows the
  worker never delivered. The drain-skip in `deliverInterjections` stays — it narrows how many
  rows can meet that fate. Also qualify the "A row is never silently lost and never delivered
  twice" consequence bullet with a pointer to the 2026-08-03 amendment: a row delivered into an
  Exchange the human then scraps is dropped WITH that Exchange — visibly, beside the "cancelled"
  note, which is not *silent* loss.
- `ISSUES.md`: delete the resolved entry ("queued messages do not seem to be removed from the
  queue …", currently the last item).
- `CHANGELOG.md` under `## [Unreleased]` → `### Fixed`: one entry — cancelling with Esc no
  longer puts a message the model already received back on the queue; the queue holds only what
  was never delivered, and the ⧖ chat block remains the record of what the model read before the
  Exchange was scrapped.

**Tests:** none (docs only).

**Acceptance:**

- `grep -n "Amended 2026-08-03" "docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md"` → one match inside the Consequences amendment block.
- `grep -c "queued messages do not seem" ISSUES.md` → 0.
- `grep -n "back on the queue" CHANGELOG.md` → one match under `[Unreleased]` / `### Fixed`.
- `make check` passes.

**Commit:** `docs(adr): amend ADR 0025 — Esc holds only undelivered interjections`

---

**Suggested version bump:** none performed. When the next release is cut, this is a patch-level
fix (behavioral bug fix, no API change) — the owner decides whether and when.

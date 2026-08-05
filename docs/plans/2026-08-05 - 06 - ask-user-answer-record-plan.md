# ask_user answer record in the tool block — implementation plan

- **Goal:** after an `ask_user` question is answered, its collapsible transcript block
  carries a well-formatted permanent record — the full question text plus a `[x]`/`[ ]`
  checkbox list of the offered choices with the given answer(s) ticked — and the tool's
  model-facing description tells the model not to restate the question/choices in its own
  message beforehand (token waste).
- **Date:** 2026-08-05
- **Status:** ready to execute
- **Origin:** ISSUES.md — "the (multiple option) question tool should print out a well
  formatted summary of the question/options and given answer(s) in the collapsable tool
  after the question(s) have been answered. The tool's description for the model should
  also briefly state to no print the questions beforehand - as this uses up tokens
  unnecessaryly." (entry flipped to `[P]` when this plan was saved.)
- **Authoritative sources:**
  - `internal/tools/ask_user.go` — the tool: spec at `:11-23` (description string `:13`),
    `Execute` `:59-88`, result is exactly `AskAnswer.Text` via `okResult` (`:87`).
  - `internal/domain/ask.go:52-58` — reply contract: a multi-select answer is one ticked
    label per line, verbatim. This plan does NOT change the wire contract.
  - `internal/tui/toolpresent.go` — presenter seam: `toolPresenter` `:227-260`,
    registry `:279-408` (ask_user entry `:393-398`), `enrichWithResult` `:544-576`,
    helpers `clipDetail`/`detailClipRunes=160` `:956-961`, `firstLine` `:1130`.
  - `internal/tui/render.go` — block painting: `renderToolBlock` `:885-908`,
    `collapsedBodyCap = 1` `:1093`, `groupable` `:1265-1267`,
    `blockHidesWhenCollapsed` `:1216-1233`.
  - `layout.md` — "The rules behind the tool-call sketch" (§ around line 369) and
    "Collapsed and expanded blocks" (§ around line 494) govern block rendering; the claim
    around lines 442-447 that a multi-select answer's later lines "ride the expandable
    detail" is FALSE today (code drops them) and is corrected by item 2.
  - `docs/design/user-questions-layout.md` — pins the ASCII `[x]`/`[ ]` checkbox pair
    (box-drawing checkboxes render as tofu); markers also at
    `internal/tui/model.go:4315-4316`.
  - ADR 0031 — the engine stays wire-silent: the record body is a render-time act only;
    no extra tokens ever go to the model because of it.
- **Ratified design calls** (owner via AskUserQuestion, 2026-08-05):
  1. **Record format = full question + checkbox list.** The answered block's body opens
     with every line of the full `question` text (even when that repeats the one-line
     header target), followed by one line per offered choice marked `[x]` (ticked) or
     `[ ]` (not ticked). The branch summary stays the quoted first line of the answer,
     never respelled. Accepted alternative costs: the common one-line question is
     repeated in the body.
  2. **Timing = after answering only.** While the question is pending the popup is the
     live view and the block stays summary-only exactly as today; the record body
     materialises when the answer lands.
  - Plan-author corollaries (this plan, 2026-08-05): answer lines that match no choice
    label (typed custom answers, including multi-line ones) are appended after the
    checkbox list so no part of an answer is ever dropped; markers are the pinned ASCII
    `[x]`/`[ ]` pair for single- and multi-select alike (the record is a record, not a
    live menu); the exact description sentence is fixed in item 3's **What**.
- **Standing requirements:** `skills: coding-standards`. Presenter code in
  `toolpresent.go` stays pure (no lipgloss). Any authorized deviation from item text
  lands as a dated NOTES line under the item.
- **Out of scope:** the question popup itself (`model.go` askPrompt/askChoiceRows), the
  wire-level reply contract (`domain.AskAnswer`), keyboard collapse/expand (separate
  ISSUES.md entry), the `clipRunes` rune-vs-cell mismatch (plan `2026-08-05 - 04`),
  grouping/collapse machinery changes beyond what the new body triggers automatically,
  any version bump.

## 1. Presenter: answered ask_user calls carry a question-and-choices record body

**What:**

- In `internal/tui/toolpresent.go`, extend the `toolPresenter` seam with an optional
  result hook that sees BOTH the call arguments and the result content — shape:
  `outcome func(args map[string]any, content string) toolOutcome` — taking precedence
  over `detail` inside `enrichWithResult` (`toolpresent.go:544-576`). If
  `enrichWithResult` cannot currently see the original call args, retain the parsed args
  (or just the fields ask_user needs) on `toolView` at `presentToolCall` time; keep the
  addition unexported and minimal.
- Point the `ask_user` registry entry (`toolpresent.go:393-398`) at the new hook:
  - **Summary** (branch line): unchanged behavior — the quoted first line of the answer
    (`promotedOutput`/quoted, the user's own words, never respelled).
  - **Body** (only when a result is present — pending calls keep no body, per ratified
    call 2), built as `detailPlain` lines, each clipped via `clipDetail`:
    1. every line of the full `question` argument, verbatim, in order;
    2. one line per sanitised choice (same trimming semantics as
       `sanitiseChoices`, `internal/tools/ask_user.go:94-108`), prefixed `[x] ` when the
       choice label equals one of the answer's lines (answer = result content split on
       newlines, per the `domain/ask.go:52-58` contract), `[ ] ` otherwise;
    3. any answer lines that match no choice label, appended after the list — this also
       fixes today's silent drop of a multi-line answer's later lines
       (`quotedFirstLineDetail`, `toolpresent.go:704-706`).
  - A call with no `choices` still gets the body (question lines + unmatched answer
    lines beyond the first) — the record is uniform.
- The engine and wire stay untouched: `ask_user.go` result content remains exactly
  `AskAnswer.Text` (ADR 0031 — the record is render-side only).

**Tests** (extend `internal/tui/toolpresent_test.go`, `TestPresentToolCall` table and/or
a dedicated test using the `detailsText` helper):

- single-select: 3 choices, answer picks one → body = question line, `[x]` on the picked
  choice, `[ ]` on the others.
- multi-select: answer is two labels on two lines → both `[x]`, third `[ ]`.
- custom typed answer with choices offered → all `[ ]`, the custom line appended after
  the list, branch summary is the quoted answer.
- multi-line custom answer → every line retained (none dropped).
- multi-line question → all question lines present in the body.
- no choices passed → body is the question line(s) (+ any answer lines beyond the
  first); summary unchanged.
- pending call (no result yet) → no body, presentation identical to today.

**Acceptance:**

- `go test ./internal/tui/ -run 'TestPresentToolCall'` passes with the new rows.
- `go test ./internal/tui/` passes.
- `make check` passes.

**Commit:** `feat(tui): answered ask_user blocks record the question, choices and answers`

## 2. Paint behavior and layout.md spec for the answer record

Depends on item 1.

**What:**

- Add paint-level tests in `internal/tui/render_test.go` (plain-text assertions via
  `renderPlain`, matching the file's conventions) pinning the block-machinery
  consequences that follow automatically once an answered ask_user call has a body:
  - the answered block wears the ▸/▾ indicator and is a toggle target
    (`blockHidesWhenCollapsed`, `render.go:1216-1233`);
  - it no longer groups with adjacent Ask User calls (`groupable`,
    `render.go:1265-1267`);
  - collapsed, it shows the branch plus exactly one body line and the
    `… +N more line(s)` remainder marker (`collapsedBodyCap = 1`, `render.go:1093`);
  - expanded, the whole record paints.
- Amend `layout.md` (this item is the single owner of the doc change):
  - correct the false claim around lines 442-447 that a multi-select answer's later
    lines "ride the expandable detail beneath it" — replace it with a description of
    the actual answered-record body (full question lines, `[x]`/`[ ]` choice list,
    unmatched answer lines appended);
  - note under "Collapsed and expanded blocks" that answered Ask User blocks are
    ordinary body-bearing blocks (indicator, one-line collapsed cap, no grouping).

**Tests:** the `render_test.go` additions above are the item's tests.

**Acceptance:**

- `go test ./internal/tui/` passes, including the new paint tests.
- `layout.md` no longer contains the "ride the expandable detail" claim for
  multi-select answers and does describe the checkbox record.
- `make check` passes.

**Commit:** `test(tui): paint coverage and layout.md spec for the ask_user answer record`

## 3. Model-facing description: never restate the question beforehand

Depends on item 1 (the sentence promises a transcript record; the record must exist).

**What:**

- In `internal/tools/ask_user.go:13`, append this exact sentence to the `askUserSpec`
  description (binding text): `Never repeat the question or the choices in your own
  message before calling — the tool shows them to the human and the transcript keeps a
  record after they answer; restating them wastes tokens.`
- Extend the description assertion in `internal/tools/ask_user_test.go` (around
  `:280-282`) with a substring check on the new sentence.
- Flip this plan's ISSUES.md entry (the "(multiple option) question tool" bullet) from
  `[P]` to `[X]`.

**Tests:** the extended description assertion in `ask_user_test.go`.

**Acceptance:**

- `go test ./internal/tools/` passes.
- `make check` passes.
- The ISSUES.md bullet reads `[X]`.

**Commit:** `feat(tools): ask_user description tells the model not to restate the question`

---

**Suggested version bump:** none dedicated — this is a small UX/spec polish; let it ride
the next release the owner cuts. No version identifier is touched by this plan.

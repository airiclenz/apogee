# Ask-user multi-select — implementation plan

- **Goal:** Let the model ask a question whose answer is *several* of the offered choices:
  a new opt-in `multi_select` flag on the `ask_user` tool, space-toggled `[x]` checkboxes
  on the ask prompt, and the chosen labels travelling back newline-joined in the one
  answer string. Single-select questions stay byte-identical in schema, interaction, and
  wire format.
- **Date:** 2026-08-05 · **Status:** ready
- **Authoritative sources (precedence order):**
  1. The ratified design calls below (owner, 2026-08-05) and the pinned mockup they
     include — where anything disagrees with them, they win.
  2. `docs/design/user-questions-layout.md` as amended by the ratified calls of
     `docs/plans/archived/2026-08-04 - 03 - user-questions-menu-layout-plan.md`
     (menu-style rows: `❯` pointer + accented label, dim `·` rows, no background bar,
     no digit shortcuts, free-text custom answers always stay).
  3. `layout.md` — the TUI layout spec; its width/height/give-way and popup-budget rules
     bind every rendering change here.
  4. ADR 0011 (value-copied Model — no no-copy types by value), ADR 0030 (one width
     authority — every painted line squared through `th.measure`), and the D9 rule in
     `internal/domain/ask.go` (labels travel back in `AskAnswer.Text`, never indices).
- **Ratified design calls (owner, 2026-08-05, this session):**
  - **Interaction — space toggles, ⏎ sends; no Send pseudo-row.** While the input box is
    empty on a multi-select question, `␣` toggles `[x]` on the highlighted row; `⏎`
    submits every checked row, and with *nothing* checked submits the highlighted row
    alone (today's single-select fast path as the degenerate case). Rejected: a
    dedicated last "Send" row, and ⏎-toggles semantics.
  - **Opt-in — `multi_select: true` on the tool schema.** One optional boolean; absent or
    false is today's single-select, on the wire and in the TUI. `domain.AskRequest` gains
    an additive `MultiSelect bool`; the `Asker` interface and `AskAnswer` shape are
    untouched.
  - **Wire format — newline-joined labels.** Each chosen label on its own line in
    `AskAnswer.Text`, exact escape-stripped label text, in the order the choices were
    presented (the schema array order), nothing else. A single selection is
    byte-identical to today's reply.
  - **Free text — typing replaces the checks.** Typing swaps the question to free-text
    exactly as today (the offering hides while the box is non-empty) and ⏎ sends only the
    typed answer; deleting back to empty restores the offering with the checked set
    untouched. Rejected: typed text as an extra item alongside the checks.
  - **Pinned mockup** (owner-selected preview, 2026-08-05 — marker glyphs `[x]`/`[ ]`,
    pointer and dim rows as the menu style already draws them):

    ```
    ╭──────────────────────────────────────────────╮
    │ Which findings should I fix?                 │
    │                                              │
    │ ❯ [x] Fix the nil-check in submitAnswer      │
    │                                              │
    │ · [ ] Add the missing layout() call          │
    │                                              │
    │ · [x] Guard the empty-choices path           │
    │                                              │
    │  ↑↓ select · ␣ toggle · ⏎ send · …           │
    ╰──────────────────────────────────────────────╯
    ```
- **Standing requirements:**
  - skills: coding-standards
  - Never change VERSION / CHANGELOG release heading / manifest versions / tags (see the
    closing note).
  - Any authorized deviation from item text lands as a dated NOTES line under the item.
  - `make check` green before every commit.
- **Out of scope:** the approval prompt (a safety gate with fixed single decisions);
  picker, `/sessions` browser, autocomplete; digit shortcuts; a `max_choices` bound;
  combining typed text with checked labels in one answer; any change to the `Asker`
  interface, `AskAnswer`, or the Approver contract; mouse interaction with choice rows
  (none exists today); headless (`internal/run`, Asker=nil → tool unregistered),
  sub-agents, and Mechanisms — all untouched by construction; transcript collapse
  machinery (a multi-line answer renders through the existing
  `quotedFirstLineDetail` + uniform-collapse path like any multi-line detail); version
  bump.

---

## 1. Domain + tool schema: the `multi_select` opt-in — ✅ DONE (2026-08-05)

**What:** Add the additive flag end-to-end below the TUI.
- `internal/domain/ask.go`: new field `MultiSelect bool` on `AskRequest`, doc comment in
  the file's voice noting it is exactly the anticipated freeze-safe additive refinement;
  amend the `AskAnswer.Text` doc comment to say the reply carries each chosen label on
  its own line when the request was multi-select (labels, never indices — D9 stands).
- `internal/tools/ask_user.go`: schema gains the optional property, exactly:
  `"multi_select": {"type": "boolean", "description": "Optional: set true when several of the choices may apply at once. The human can then select any number of them, and the answer returns each chosen option on its own line. Leave it unset for a single-answer question."}`
  Extend `askUserArgs` with `MultiSelect bool \`json:"multi_select"\`` and plumb it into
  the `domain.AskRequest` built in `Execute`. Append one sentence to the tool
  `description` string: `"When several choices could apply at once, set multi_select to true so the human can pick more than one."`
  `sanitiseChoices` and every error path stay as they are; `multi_select` with absent,
  empty, or single-entry `choices` is carried through untouched (the Driver simply has
  little or nothing to toggle — no new validation).
- No change to `apogee.go` (the `AskRequest` alias at line 207 re-exports the new field
  by construction) and none to `internal/run` (nil Asker) or `internal/tools/sub_agent.go`.

**Tests:** In `internal/tools/ask_user_test.go`: decoding `multi_select: true` /
`false` / absent yields the matching `AskRequest.MultiSelect` at a scripted Asker; the
advertised schema contains the property; a scripted Asker returning a multi-line
`AskAnswer.Text` round-trips verbatim into the tool result; existing single-select cases
unchanged.

**Acceptance:**
- `go test ./internal/tools/ -run TestAskUser -v` — new cases pass, no prior case broken
- `go test ./internal/domain/ ./...` compiles clean via `make check`

**Commit:** `feat(tools): add multi_select opt-in to the ask_user question schema`

## 2. TUI state and keys: checked set, space toggle, multi-answer submit — ✅ DONE (2026-08-05)

Depends on item 1.

NOTES (2026-08-05): the key case is `case "space"`, not the item's literal `case " "` —
Bubble Tea v2's `Key.String()` explicitly falls back to `Keystroke()` when the key's text is a
single space (ultraviolet `key.go`), so a space keypress stringifies as `"space"` and `" "` would
never match. Verified empirically before writing the case; behaviour is otherwise exactly as the
item specifies. The checked-label extraction lives in a small `checkedLabels` helper on `Model` so
item 3's renderer can read the same ordering rule rather than re-deriving it.

**What:** In `internal/tui/model.go`:
- New Model field `askChecked []bool` next to `askSel` (~line 146) — a plain slice, with
  a comment noting the ADR 0011 rule (no no-copy types by value; a bare slice is fine on
  the value-copied Model).
- `askReqMsg` fold (~line 538): initialise `askChecked = make([]bool, len(Choices))` when
  `Request.MultiSelect` and choices exist, `nil` otherwise.
- `finishWorker` (~line 1638) and `submitAnswer` clear `askChecked` alongside
  `pendingAsk` — no path leaves a dead checked set behind.
- Key routing (~line 944, the empty-input choices guard): add `case " "` — only when
  `pendingAsk.Request.MultiSelect` — toggling `askChecked[askSel]` and returning. On a
  single-select question space falls through to the textarea exactly as today (it starts
  a free-text answer). ↑/↓ handling is untouched; typing still hides the offering via the
  existing `choicesShown` guard and must NOT clear `askChecked` (deleting back to empty
  re-shows the offering with the checks intact — the ratified free-text call).
- `submitAnswer` (~line 1550): with choices offered and an empty input, when
  `MultiSelect` and at least one row is checked, the answer is the checked labels —
  `stripEscapes`-stripped exactly as today — joined with `"\n"` in choice order; with
  none checked it stays the highlighted label (unchanged code path); a non-empty input
  stays the trimmed typed text. Single-select questions hit only the existing branches.

**Tests:** In `internal/tui/model_test.go` (house driver helpers): space toggles and
un-toggles the highlighted row; ⏎ with rows 0 and 2 checked delivers `"A\nC"` (choice
order, even when toggled 2-then-0) on the reply channel; ⏎ with nothing checked delivers
the highlighted label; typing then ⏎ delivers only the typed text; typing then deleting
back to empty leaves the checked set intact and ⏎ then delivers it; on a single-select
question space inserts a space into the box (regression pin); `finishWorker` (esc-stop
path) clears `askChecked` and the borrowed-draft restore still runs.

**Acceptance:**
- `go test ./internal/tui/ -run 'TestAsk|TestSubmit|TestModel' -v` — new cases pass, no prior case broken
- `make check`

**Commit:** `feat(tui): space-toggled multi-select answers on the ask prompt`

## 3. TUI rendering: checkbox marker column and the multi hint

Depends on items 1 and 2.

**What:** In `internal/tui/model.go` (`askPrompt`, ~line 4313) with the shared painter
`internal/tui/popup.go` untouched unless a cell capability is genuinely missing:
- Multi-select questions build two-cell `popupRow`s — a marker cell (`[x]` / `[ ]` from
  `askChecked`, glyphs pinned by the mockup) followed by the label cell — through the
  existing popup column machinery, so markers align vertically and a wrapped choice's
  continuation lines indent under the *label* column exactly as the menu style wraps
  every other columned row. Single-select questions keep `singleCellRows` and must render
  byte-identical to before this plan.
- The same two-cell rows feed the height/budget math (`popupWrappedRowHeights` →
  `popupRowBlockLines` → `popupBudget`) so wrapped heights, `maxAskChoiceRows`, the
  anchor floor (`askAnchorRowLines`) and the elision counts stay truthful — no separate
  row set for measuring versus painting.
- Hint line for multi-select questions, pinned:
  `"↑↓ select · ␣ toggle · ⏎ send · type for a custom answer · esc cancel"`.
  The single-select and free-text hints are unchanged.
- Every painted line squares through `th.measure` (ADR 0030); no new lipgloss surface.

**Tests:** In `internal/tui/model_test.go` / `internal/tui/popup_test.go` (wherever the
existing askPrompt render pins live): a multi-select render shows aligned `[x]`/`[ ]`
markers with the pointer/dim styling unchanged; toggling repaints the marker; a choice
long enough to wrap indents its continuation under the label column; the multi hint
renders as pinned and elides on narrow widths through the existing hint machinery; a
single-select question's render is byte-identical to the pre-plan golden.

**Acceptance:**
- `go test ./internal/tui/ -run 'TestAskPrompt|TestPopup|TestRender' -v` — new cases pass, no prior case broken
- `make check`

**Commit:** `feat(tui): checkbox marker column on multi-select ask prompts`

## 4. Docs reconcile

Depends on items 1–3. This item owns every cross-cutting doc amendment — no other item
touches these files.

**What:**
- `CONTEXT.md` (Ask-user entry, ~line 307): two or three sentences — the optional
  `multi_select` flag, space-toggled checks in the TUI, the newline-joined labels reply,
  single-select as the unchanged default.
- `layout.md` (ask-prompt rules): amend for the marker column (two-cell rows, wrap
  under the label column), the space-toggle key rule and its single-select fall-through,
  the pinned multi hint, and the ⏎ rule (checked set; highlighted row when none
  checked). One sentence recording that the transcript needs no new rule: the answer is
  still the human's own words on the branch, and a multi-line answer rides the existing
  first-line-promoted, expandable-detail path.
- `docs/design/user-questions-layout.md`: append the pinned multi-select mockup from
  this plan's header with a dated one-line note naming this plan as the amending
  authority.
- `CHANGELOG.md` (`[Unreleased]` → `### Added`): one entry in the house voice (bold
  lead sentence, sub-bullets for the key rules) covering the model-facing flag and the
  space-toggle interaction. Do not touch any release heading.

**Tests:** none (docs only); the verifier checks the four files say what this item
states and nothing else changed.

**Acceptance:**
- `git diff --stat` touches only `CONTEXT.md`, `layout.md`,
  `docs/design/user-questions-layout.md`, `CHANGELOG.md`
- `make check`

**Commit:** `docs: record ask_user multi-select in CONTEXT, layout spec, and changelog`

---

**Suggested version bump (not performed):** minor — `v0.11.0`. `domain.AskRequest`
(re-exported as `apogee.AskRequest`) gains an additive public field and the tool schema
a new capability; the CHANGELOG's SemVer note classes additive surface growth as minor.
The owner decides whether and when.

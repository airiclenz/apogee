# Plan — ask_user multiple-choice + ask/approval prompts onto the popup module

**Date:** 2026-07-24
**Status:** READY (design grilled 2026-07-24; all forks owner-resolved — no needs-design-call
escalation expected). Touches `internal/tui`, `internal/domain/ask.go`,
`internal/tools/ask_user.go` (+ CHANGELOG).
**Source:** handoff `docs/handoffs/archived/2026-07-24 - 00 - ask-prompt-popup-adoption-design.md` +
owner grilling 2026-07-24. Owner explicitly widened scope mid-grill: the ask_user layout
redesign is folded in NOW (multiple-choice + free text), not left as a future adopter note.
**Track:** post-`v1.0.0` TUI quality + one additive tool-surface feature. The `AskRequest` /
`AskAnswer` structs were built for exactly this ("a post-v1 Choices field is additive",
`internal/domain/ask.go:24` — D7 freeze-safety); no breaking domain change.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`, no
PRs, no Claude co-author trailers. `go test ./...`, `go test -race ./...`, `gofmt -l .` empty,
`go vet ./...` green gate every item.

---

## The problem (grounded, verified 2026-07-24)

1. **Two plain-text overlays remain outside the popup module.** `askPrompt`
   (`internal/tui/model.go:1681`) and `approvalPrompt` (`model.go:1665`) render bold+faint
   plain text while every other boxed selector goes through `renderPopup`
   (`internal/tui/popup.go`). The module's head comment names them "deliberate future
   adopters" (old plan D2) — that note must go once this lands.
2. **The module cannot wrap.** `renderPopup`'s contract truncates every row to the inner
   budget ("no line can ever wrap the box"), but both prompts carry prose/structured bodies
   that must word-wrap: the ask question, and the approval prompt's Reason + pretty-printed
   JSON args (`prettyJSON`, `transcript.go:466`, multi-line indented output).
3. **Escape-stripping gap (hardening).** `askPrompt` renders `req.Question` — untrusted model
   output — WITHOUT `stripEscapes` (`transcript.go:405`); `approvalPrompt` likewise renders
   `req.Tool`, `req.Reason`, and the args raw. The popup contract requires content
   "pre-composed and escape-stripped".
4. **ask_user is free-text only.** The owner dislikes the current ask layout; the redesign
   they want is the one the domain layer pre-planned: optional multiple-choice, painted by the
   popup's existing `rows`/`selected`/marker machinery, with free text always available.
5. **A pending question exists nowhere else.** The question is NOT appended to the transcript
   (`model.go:276-285` records `pendingAsk` only), so any hidden tail is unrecoverable — the
   human would answer blind. And a View taller than the terminal clips the BOTTOM: the input
   box the answer is typed into. Long bodies therefore need a screen-budget cap that is never
   silent.

Layout facts the items rely on (verified):

- Popups span the full window width: callers pass `m.width` (90dda7f — NOT
  `transcriptWidth()`; the archived selector-popup plan's D3 is superseded by the code).
- The View slot is `lipgloss.Height`-generic (`model.go:1153-1186`): the prompt shrinks the
  viewport on a local copy, floor 1 row. `askPrompt`/`approvalPrompt` run before the shrink,
  so `m.viewport.Height()` there is the full stored layout height — usable as the budget base.
- Key routing in `stateAwaitingAsk`: `enter` is intercepted (`model.go:468` →
  `submitAnswer`); every other key falls through to `m.input.Update` (`model.go:506-521`), so
  ↑/↓ currently move the textarea cursor. The autocomplete dropdown already sets the in-repo
  precedent for stealing keys from a live input while an overlay is open.
- Mouse: only the input and transcript rectangles exist (`mouse.go`); no popup row anywhere is
  clickable today.
- `wrapText` (`render.go:554`) word-wraps plain text to a column limit and hard-breaks
  over-long words; it is ANSI-unaware, so the module must wrap PLAIN text and style after.
- Test exposure: ask-flow tests are state/contains-based, no look-pinning (`asker_test.go`,
  `model_test.go:765-`); `popup_test.go` pins the painter's geometry.

## Decisions (grilled 2026-07-24 — owner-resolved)

- **D1 — the module wraps.** `popupSpec` gains a `body` field the module word-wraps itself.
  Owner: longer questions must wrap; the module must support it. (Rejected: caller-side
  pre-wrap — it leaks the frame geometry to every caller and the approval prompt would re-pay
  it.)
- **D2 — screen-budget cap, never silent.** `popupSpec` gains `maxBodyRows` (≤ 0 = uncapped);
  the CALLER derives it from the live layout so the popup never pushes the input box
  off-screen. When the module truncates the body it appends an explicit faint
  `… (+N more lines)` marker row, counted INSIDE the cap. (Rejected: show-all — clips the
  input box on pathological questions; fixed constant — ignores the actual screen.)
- **D3 — scope: everything now.** Both prompts adopt the popup in this plan, AND ask_user
  gains multiple-choice (owner folded the redesign in rather than leaving the layout open).
  The module's "future adopters" note is retired.
- **D4 — multiple-choice shape.** ask_user's schema gains optional `choices: []string`. With
  choices: question = body, choices = selectable rows (↑/↓ + ⏎, module highlight); typing in
  the input box instead answers free-text (the built-in "Other"). Without choices: today's
  free-text flow, restyled in popup chrome.
- **D5 — key arbitration: empty input = choices mode.** While the input box is EMPTY, ↑/↓
  move the choice highlight (first choice pre-selected) and ⏎ submits the highlighted label.
  The moment text is typed the highlight drops (rendered `selected: -1`), ↑/↓ return to
  cursor duty, ⏎ submits the typed text; deleting back to empty restores choices mode. The
  effective selection is DERIVED at render (input empty && choices ⇒ live index, else −1) —
  minimal state. (Rejected: tab-toggle focus — a focus concept no other overlay has; arrows
  always-on-choices — breaks multi-line free-text editing.)
- **D6 — body look.** Body lines render normal (non-faint, non-bold) white on the black fill,
  flush-left with title and hint (the two-space marker column belongs to ROWS — it is the
  selection cue; a body is not a row). Hierarchy: title bold / body normal / chrome (hint,
  unselected rows) faint.
- **D7 — approval mapping: Reason → body top.** Title is the short, never-truncated
  `approve <raw tool>?` (raw `req.Tool` verbatim — the security constraint). A non-empty
  Reason becomes the body's first line(s) — `reason: <text>`, wrapped by the module so it can
  never be silently cut on the approval surface — then a blank line, then the pretty-printed
  args. (Rejected: reason in the title — silent tail loss on a security surface.)
- **D8 — escape-strip hardening.** Every model-authored string entering a popup is
  `stripEscapes`-ed at the call site (question, choices, tool name, reason, args). Stripping
  removes only the ESC byte, so the raw-tool-name-verbatim requirement is preserved.
- **D9 — answer wire format.** The chosen label is returned as `AskAnswer.Text` — exactly
  what `okResult` already forwards to the model; no `Choice` index field is added (the struct
  comment keeps naming it as a future additive field). One less schema concept; the model
  reads text either way.
- **D10 — no mouse on choice rows.** No popup row anywhere is clickable today; ask rows stay
  consistent. Popup-wide mouse support is a future, global concern.
- **D11 — this layout is still not final.** The owner reserves further layout changes;
  nothing here forecloses them (body/rows/hint are orthogonal spec fields).

---

## 1. The popup painter learns a wrapping body block — `internal/tui/popup.go` — ✅ DONE (2026-07-24)

NOTES (2026-07-24): The body split/wrap/cap/marker logic lives in a small unexported helper
`popupBodyLines` in `popup.go` that `renderPopup` calls between the title and the rows —
behaviour is exactly as specified, only factored out for readability. Each wrapped body line and
the marker are also `truncateLabel`-clipped to the inner budget (defensive: honours the module's
"no line can ever wrap the box" contract at inner width 1, where the marker text is wider than the
cell). Test helper `popupInterior` added in `popup_test.go` to strip border+padding for exact-match
assertions.

All in `popup.go` + `popup_test.go` + `theme.go`:

- `popupSpec` gains two fields: `body string` — plain, escape-stripped prose the MODULE wraps
  (unlike `rows`, which stay truncate-only) — and `maxBodyRows int` — the body block's row cap
  including the overflow marker; ≤ 0 shows every wrapped line (D2).
- `renderPopup` renders the body between the title and the rows: split `body` on `"\n"`
  (embedded newlines are layout — the approval args' JSON indentation and blank separator
  lines must survive), `wrapText` each segment to the inner budget (an empty segment renders
  one blank row), and flatten. When the flattened line count exceeds `maxBodyRows` (> 0), show
  the first `maxBodyRows − 1` lines plus a final faint `… (+N more lines)` marker row, where N
  is the count of hidden lines — the block never exceeds `maxBodyRows` rows and truncation is
  never silent. An empty `body` adds no rows (same drop-if-empty as title/hint).
- Body lines render flush-left (no two-space marker column — that inset is the ROW selection
  cue, D6) in a new `theme.popupBody` style: normal white foreground, black background, no
  bold/faint — sitting between `presentTitle` (bold) and `statusFaint` (chrome) in the
  hierarchy. Each line is padded on the same black field as every other content line
  (`blackFill`).
- `wrapText` is ANSI-unaware, so the contract line: `body` arrives PLAIN and escape-stripped;
  the module wraps first, styles after.
- The file-head contract comment documents the body block (wrapping vs truncating rows, the
  cap + marker, plain-text requirement). The "deliberate future adopters" note stays until
  item 5 removes it (all adopters land in items 3–4).

**Tests** (`popup_test.go`, on `ansi.Strip`-ed lines except the style assertion):

- a body longer than the inner width wraps to multiple lines and every rendered line is still
  exactly `width` display cells; no body line ever exceeds the box;
- embedded newlines are preserved: a body `"a\n\nb"` renders three body rows with a blank
  middle row (the approval reason/args separator case);
- a single token wider than the inner budget hard-breaks (wrapText's guarantee, pinned here);
- `maxBodyRows` cap: a 10-line body with `maxBodyRows: 4` renders exactly 4 body rows, the
  last being `… (+7 more lines)`; a body exactly at the cap shows all lines and NO marker;
  `maxBodyRows: 0` shows everything;
- composition order title / body / rows / hint, with rows still truncate-only and the
  selected-row highlight unaffected; empty body adds no rows;
- the body row's un-stripped line does NOT carry the faint SGR (loose assertion — it must
  differ from the hint's styling);
- degenerate width (≤ frame) still returns `""`; inner width 1 neither panics nor overflows.

**Acceptance:** gates green; `rows` behavior byte-identical for a spec with empty `body`
(existing popup tests pass unmodified). Commit:
`feat(tui): popup painter learns a wrapping body block`.

---

## 2. ask_user gains optional multiple-choice — `internal/domain/ask.go` + `internal/tools/ask_user.go` — ✅ DONE (2026-07-24)

NOTES (2026-07-24): The choice sanitisation (trim + drop whitespace-only, all-blank/absent ⇒ nil)
is factored into a small unexported helper `sanitiseChoices` in `ask_user.go` that `Execute`
calls — behaviour exactly as specified, only extracted to keep `Execute` small (coding-standards).
Test helper `askCallWithChoices` and a `seenReq` capture on `scriptedAsker` added in
`ask_user_test.go`.

- `AskRequest` gains `Choices []string` — the optional short answer options put to the human
  alongside the free-text box; nil/empty means free-text only (today's behavior). The struct
  doc comment updates: Choices has landed; the freeze-safety note now points at the NEXT
  anticipated additive field. `AskAnswer` is untouched (D9 — the chosen label travels in
  `Text`; the comment keeps naming a `Choice` index as future-additive).
- `askUserSpec.schema` gains an optional `choices` property: array of strings, description
  telling the model to offer 2–5 SHORT, single-line answer options when the question has a
  natural closed set, and that the human can always type a custom free-text answer instead —
  so choices never gate the reply.
- `askUserArgs` gains `Choices []string`. `Execute` sanitises: trim each choice, drop
  whitespace-only entries; an all-blank or absent array degrades to free-text-only (no
  result-level error — a sloppy model still gets its question through). The sanitised list
  passes into `domain.AskRequest{Question, Choices}`. Everything else (empty-question guard,
  nil-asker guard, ctx handling, `okResult(call.ID, answer.Text)`) is unchanged.
- The tool `description` gains one sentence naming the optional choices.

**Tests** (`internal/tools`, existing ask_user test file):

- choices decode and pass through to the Asker verbatim after sanitisation (fake Asker
  captures the request);
- whitespace-only choices are dropped; an all-blank array yields nil Choices;
- absent choices behaves exactly as today (regression);
- the answer returned to the model is `AskAnswer.Text` regardless of how the human produced
  it (the tool cannot tell a picked label from typed text — by design, D9).

**Acceptance:** gates green; `internal/tui` does not compile against anything new yet (the
TUI adoption is item 3 — this item is engine/tool surface only). Commit:
`feat(tools,domain): ask_user gains optional multiple-choice`.

---

## 3. The ask prompt adopts the popup, with selectable choices — `internal/tui` — ✅ DONE (2026-07-24)

All in `model.go` (+ `model_test.go`, `asker_test.go` untouched unless noted):

- **State.** `Model` gains `askSel int` — the choice highlight index. The `askReqMsg` handler
  (`model.go:276`) resets it to 0 (first choice pre-selected, D5). No no-copy types (ADR
  0011 — it is a bare int).
- **Key routing** (D5). In `handleKey`, before the `m.input.Update` fall-through
  (`model.go:506`): when `m.state == stateAwaitingAsk`, the pending request has choices, and
  `m.input.Value() == ""`, intercept `"up"`/`"down"` to move `askSel` (clamped to
  `[0, len(choices)-1]`, matching the session browser's non-wrapping selection). Every other
  key — and ↑/↓ the moment the input is non-empty — falls through to the textarea exactly as
  today, so multi-line editing (⇧⏎/⌥⏎) is untouched.
- **Submit.** `submitAnswer` (`model.go:773`) resolves the answer: choices present AND input
  empty ⇒ the SANITISED choice label at `askSel` (the same string the popup displayed);
  otherwise the trimmed typed text (empty allowed when no choices, as today — with choices an
  empty input always means the highlighted choice, so an empty free-text answer is
  unreachable there; deliberate, D5). Reply/reset/state flow unchanged.
- **`askPrompt` rebuilds onto `renderPopup`** (the D2/D5/D6/D8 mapping):
  - width `m.width` (full window, 90dda7f);
  - title `"the assistant is asking:"`;
  - body `stripEscapes(req.Question)`;
  - rows `stripEscapesAll(req.Choices)`; `selected` DERIVED: input empty ⇒ `askSel`
    (clamped), else −1 (highlight off = ⏎ sends text — the mode is always visible);
  - hint dynamic: with choices and empty input
    `"↑↓ select · ⏎ send · type for a custom answer · esc cancel"`; otherwise today's
    `"type your answer below · ⏎ send · ⇧⏎/⌥⏎ newline · esc cancel"`;
  - budget arithmetic (D2), new const `maxAskChoiceRows = 8` (the `maxAutocompleteItems`
    convention): `avail = max(6, m.viewport.Height() − 3)` (≥ 3 transcript rows stay
    visible; `m.viewport.Height()` is the pre-shrink stored layout height — verified);
    `chrome = 4` (2 borders + title + hint);
    `rowsShown = min(len(choices), maxAskChoiceRows, max(0, avail − chrome − 1))` — passed as
    `maxRows` (the module windows around the selection);
    `maxBodyRows = max(1, avail − chrome − rowsShown)` — rows get priority (they are what the
    human acts on), the body keeps ≥ 1 row and overflows into the explicit marker.
  - `approvalStyle` stays for now (item 4 owns `approvalPrompt`).
- `View` needs no changes (the slot is `lipgloss.Height`-generic; verified
  `model.go:1153-1186`).

**Tests** (`model_test.go`, `plain(m.View())` style + rendezvous helpers from
`newAskModel`):

- free-text round-trip unchanged (existing tests pass unmodified — they are contains-based);
- choices flow: an ask with 3 choices renders the question body, all 3 rows, ❯ on the first;
  `down` then ⏎ replies with the SECOND label over the rendezvous channel;
- typed-text override: with choices pending, typing renders no ❯ highlight (selected −1) and
  ⏎ replies with the typed text; deleting back to empty restores the highlight and ⏎ picks;
- ↑/↓ with text in the input move the textarea cursor, not the selection (multi-line answer
  regression);
- escape-strip: a question and a choice containing `\x1b` render without the ESC byte
  (hardening, D8);
- a long question at the harness window (100×30) caps its body rows and shows the
  `… (+N more lines)` marker; the input box row is still present in the View (the
  never-clip guarantee);
- cancel (esc) flow unchanged.

**Acceptance:** gates green; `askPrompt` contains exactly one `renderPopup` call and no
manual styling of question/hint; `grep -n 'toolDetail' internal/tui/model.go` shows no ask
usage. Commit: `feat(tui): ask prompt adopts the popup with selectable choices`.
**Depends on:** items 1, 2.

---

## 4. The approval prompt adopts the popup chrome — `internal/tui` — ✅ DONE (2026-07-24)

All in `model.go` (+ `model_test.go`):

- **`approvalPrompt` rebuilds onto `renderPopup`** (D7/D8):
  - width `m.width`; title `"approve " + stripEscapes(req.Tool) + "?"` — the RAW tool name,
    verbatim (the approval flow is a security surface; stripping removes only the ESC byte,
    never visible characters);
  - body: `reason: <stripEscapes(req.Reason)>` as the first segment when Reason is non-empty
    (wrapped by the module — a long reason can never silently truncate, D7), then a blank
    line, then `stripEscapes(prettyJSON(req.Arguments))`; empty/null args add no segment (the
    existing `prettyJSON` empty contract); Reason-only and args-only compose without stray
    blank lines;
  - no rows (`selected: -1` irrelevant — nil rows);
  - hint `"a allow · d deny · s allow-session · esc cancel"` verbatim;
  - `maxBodyRows` per item 3's arithmetic with `rowsShown = 0`:
    `max(1, max(6, m.viewport.Height() − 3) − 4)`. Extract the shared budget derivation into
    one small helper both prompts call (e.g. `popupBudget(rows int) (maxBody, maxRows int)`)
    rather than duplicating the clamps.
- `approvalStyle` (`model.go:1658`) is deleted if nothing else uses it (item 3 already freed
  the ask usage; verify with grep before removal).
- Key handling (`handleApprovalKey`) and the View slot are untouched — this is purely the
  painter swap the handoff promised.

**Tests** (`model_test.go`):

- the approval View contains the raw tool name in the title row, the decision legend, and the
  args lines; existing approval tests pass unmodified (contains-based);
- a multi-word Reason longer than the window width appears IN FULL across wrapped body lines
  (no `…` in the reason text) — the D7 guarantee;
- a tool name / reason / args value containing `\x1b` renders without the ESC byte (D8);
- pretty-printed args keep their two-space indentation on the rendered lines (embedded-newline
  preservation, end to end);
- an over-tall args body caps with the `… (+N more lines)` marker and the input box row
  remains in the View.

**Acceptance:** gates green; `approvalPrompt` contains exactly one `renderPopup` call;
`approvalStyle` no longer exists (or a grep-verified remaining user is documented in the
item's NOTES). Commit: `feat(tui): approval prompt adopts the popup chrome`.
**Depends on:** item 1 (item 3 only for the shared budget helper and `approvalStyle`
removal — implement 3 first).

---

## 5. Closeout — contract note, doc sweep, CHANGELOG — ✅ DONE (2026-07-24)

NOTES (2026-07-24): Doc sweep found no stale "plain text / bold+faint" look-descriptions of either
prompt outside the popup.go head paragraph (removed). Beyond the literal "look references" check, the
View comment's prompt-set was corrected too — "The approval prompt … sits between the transcript and
the blank line" → "The approval or ask prompt …" (the ask prompt shares the slot; consistent with the
already-"approval/ask prompt" wording at model.go:1194/1231). popup.go's opening paragraph also now
names all four adopters (was "…both compose their pane through it"). CHANGELOG's "Fixed/Security"
escape-strip entry landed under a new `### Security` heading. doc.go/layout.md had no prompt
look-descriptions to sweep.

- `popup.go` file-head comment: the "approval / ask prompts … deliberate future adopters"
  paragraph is REMOVED (all named adopters have landed, D3); in its place one line records
  that every overlay pane — sessions, dropdowns, ask, approval — paints through this module.
- Sweep for stale look-descriptions of either prompt: `model.go`'s View comment
  ("The approval prompt, when one is pending, sits between the transcript and the blank
  line" — position still true, look references need checking), `doc.go`, `layout.md`, and the
  `askPrompt`/`approvalPrompt` doc comments themselves (rewritten in items 3–4; verify
  nothing else describes them as plain text). The handoff's own "must shrink or go" note is
  satisfied by the removal above.
- CHANGELOG under `## [Unreleased]`:
  - Added — ask_user offers optional multiple-choice answers: the model may pass `choices`,
    the human picks with ↑/↓ + ⏎ or types a custom answer as before;
  - Changed — the ask and approval prompts render as bordered popup panes (shared popup
    module): wrapped question/reason/args bodies, screen-budget cap with an explicit
    `… (+N more lines)` marker, full window width;
  - Fixed/Security — the ask and approval prompts now escape-strip all model-authored text
    (question, choices, tool name, reason, args) before rendering.
- Full gates one last time: `go test ./...`, `go test -race ./...`, `gofmt -l .` empty,
  `go vet ./...`.

**Acceptance:** gates green;
`grep -rn 'future adopter' internal/tui --include='*.go'` returns nothing;
`grep -rn 'plain-text form' internal/tui --include='*.go'` returns nothing. Commit:
`docs(tui,changelog): record the ask/approval popup adoption`.
**Depends on:** items 3, 4.

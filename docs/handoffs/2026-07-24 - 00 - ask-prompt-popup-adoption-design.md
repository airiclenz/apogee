# Handoff — Design the plan: ask_user question prompt onto the popup module

**Date:** 2026-07-24
**From:** selector-popup session (plan written, implemented, and archived the same day)
**Next session's job:** DESIGN ONLY — grill the owner through the open forks below, then save an
implementation plan in the house format to `docs/plans/2026-07-24 - NN - <slug>-plan.md`
(numbered `## N.` H2 items with What/Tests/Acceptance/commit — the implement-plan scout rejects
checkbox items). Do NOT implement in-session and do NOT ask for approval via ExitPlanMode — the
saved plan doc IS the deliverable. Implementation happens later via `implement-plan` with
`coding-standards` forwarded.

## What just landed (don't re-derive)

The shared selector-popup painter is **implemented and on `main`**:

- Plan (archived, complete): `docs/plans/archived/2026-07-24 - 03 - selector-popup-module-plan.md`
- Commits: `4d928d2` (painter), `a6da02b` (/sessions adopts), `099f2d2` (dropdowns adopt),
  `4c7b125` (docs), and the owner-driven look change `90dda7f`.
- Module: `internal/tui/popup.go` — `popupSpec{title, rows, selected, hint, maxRows}` +
  `renderPopup(th, spec, width)` + `popupRowWindow`. Read its head comment first; it is the
  contract.

**`90dda7f` superseded two of the archived plan's decisions — trust the code, not that plan:**
popups now span the **full window width** (`m.width`, flush with the input box — callers pass
`m.width`, not `transcriptWidth()`) and are **filled solid black** (`theme.popupBorder` carries
black bg + border-bg; `renderPopup` pads every content line to the inner width on a black field).

## The target

`askPrompt` (`internal/tui/model.go:1681`) — the ask_user question shown while
`stateAwaitingAsk`: bold lead "the assistant is asking:", the question via `th.toolDetail`, a
hint line ("type your answer below · ⏎ send · ⇧⏎/⌥⏎ newline · esc cancel"). It renders in the
transcript-side slot (`View`, `model.go:1153-1158`), the viewport shrink is `lipgloss.Height`-
generic — so adoption is **purely a painter swap**; no state-machine or key-routing changes
(the input box stays live for the answer; the prompt is not modal, unlike the /sessions browser).
`popup.go`'s head comment already names the approval/ask prompts as "deliberate future adopters"
(old plan D2) — that note must shrink or go once this lands.

## Design tensions the plan must resolve (grill the owner where marked)

1. **Wrap vs truncate — the central one.** `renderPopup` truncates every row to the inner budget
   ("no line can ever wrap the box"); a question is prose that must word-WRAP. Two shapes:
   (a) caller-side pre-wrap — split `req.Question` on newlines, wrap each via the existing
   `wrapText` (`markdown.go`), pass the wrapped lines as `rows` with `selected: -1`; or
   (b) extend `popupSpec` with a wrapping body/text field the module wraps itself. (b) is the
   better long-term module (the approval prompt's pretty-printed JSON args have the same need),
   but changes the module contract. Owner's standing preference is best-long-term architecture —
   still worth a grill question since (a) ships with zero module churn.
2. **Escape-stripping gap (fix regardless).** `askPrompt` renders `req.Question` — untrusted
   model output — WITHOUT `stripEscapes` (`transcript.go:405`). The popup contract requires rows
   "pre-composed and escape-stripped". Adoption must strip; note it as a hardening fix.
3. **Long questions.** With `selected: -1`, `popupRowWindow` shows the FIRST `maxRows` rows (a
   negative selection clamps the window start to 0 — verified). Grill: cap a very long question
   (which silently hides its tail — dangerous for a question the human must answer) vs
   `maxRows: 0` show-all (which can crowd a short terminal; the viewport floors at 1 row).
4. **Marker column.** The module prefixes every row with a two-space marker column (❯ on the
   selected row). A `selected: -1` spec still gets the two-space indent on each question line —
   probably fine (reads as the popup's body inset), but it is a visible choice.
5. **Mapping.** Title `"the assistant is asking:"` (module paints titles `presentTitle` bold
   white — today's lead uses `approvalStyle` bold; near-identical). Hint = the existing legend
   verbatim. Question style: `toolDetail` faint today — on the black pane, faint-on-black is the
   dropdown look; decide faint vs white for readability.
6. **Scope — grill.** Owner asked for the question prompt only. The approval prompt
   (`approvalPrompt`, `model.go:1665`) is the one remaining plain-text overlay, with the same
   wrap need (JSON args) plus a security constraint (raw tool name must stay verbatim). Ask
   whether it joins this plan or stays a named future adopter.

## Test exposure (verified)

Ask-flow tests are state/contains-based, no look-pinning: `asker_test.go` (rendezvous),
`model_test.go:765-` (`newAskModel`, round-trip, cancel). `popup_test.go` covers the painter
(width exactness, no-wrap, windowing, degenerate widths) — extend there if the module grows a
body field. Gates: `go test ./...`, `-race`, `gofmt`, `go vet` — green every item.

## House rules that bind the plan

- Commit direct to `main`, no PRs; never add Claude co-author trailers.
- lipgloss v2 `Width(n)` = TOTAL width, border+padding folded in (`set.go:283`) — the painter
  and `renderStartupBox` both rely on it.
- The Bubble Tea Model is value-copied — no `strings.Builder` (or any no-copy type) held by
  value anywhere it reaches (ADR 0011; `TestModelNoBuilderByValue`).

## Suggested skills

- `grill-me` — the owner expects to be interviewed through the forks above (esp. 1, 3, 6)
  before the plan is written.
- `coding-standards` — not needed for the design session itself; name it in the plan's standing
  requirement so `implement-plan` forwards it to the implementer/verifier sub-agents.

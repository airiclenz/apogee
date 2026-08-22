# Pane wheel routing — every list overlay owns the notch over it

**Goal:** a mouse-wheel notch over an open overlay scrolls THAT overlay. Today only the
/settings pane, the /usage report and the /inspect pane consume one; over the /sessions
browser, the /model | /server picker, the approval or ask prompt and the autocomplete
dropdown the notch falls through to the transcript scrolling behind them, which reads to
the human as "scrolling is broken in that panel".

**Date:** 2026-08-22
**Status:** unexecuted
**sized for:** ~200k-context host

## Authoritative sources

- `internal/tui/mouse.go` — `foldMouseWheel` (the routing chain), `settingsWheel` (the
  pane-wheel pattern every item copies), and the module doc's pane doctrine.
- `internal/tui/listsurface.go` — the shared list module (ADR 0053): `listCursor`,
  `listSurface`, `listCursor.move`, `listCursor.key`, `listWrap`.
- `internal/tui/reportpane.go` — `reportWheel`, `reportPaneRect`: the rect-gate + clamp
  shape, and the `handled bool` contract every pane wheel returns.
- `internal/tui/model.go` — `framePane`, `frameSpans`, `stackTranscriptSlot`,
  `Model.frameSpans`, `View` (the input-side slot stacking at the `ov.dropdown` append).
- ADR 0011 (the value-copied Model), ADR 0053 (the shared list surface).

## Ratified design calls

Decided by the owner (Airic Lenz) on 2026-08-22, in the session that opened this plan:

1. **The wheel CLAMPS where the keys WRAP.** A notch over any of these panes moves the
   highlight one row and stops at the ends, even where ↑/↓ wrap around
   (`listWrapsAround`). This is not a new call — `settingsWheel` and `reportWheel`
   already state and follow it ("↑/↓ walk a list as a cycle, while a wheel is a scroll —
   rolling past the last row and landing back on the first would move the human somewhere
   they did not aim"). This plan moves that rule into the shared module instead of
   restating it a fourth time.
2. **The two soft-modal decision panes take the notch inside their box.** The approval
   menu and the ask offering are soft-modal — the transcript stays scrollable underneath
   them — but a notch whose pointer is INSIDE the pane's rectangle walks that pane's
   options; outside it, the transcript scrolls exactly as it does today. The rule that
   wins is the one the other five panes already follow: the pane under the pointer owns
   the notch. Their soft-modality remains a fact about KEYS.
3. **The ask pane's wheel is NOT gated on an empty input box.** `askChoiceKey` claims ↑/↓
   only while `m.input.Value() == ""` (D5) because those keys double as the textarea's
   cursor keys. A wheel has no such second duty, so the notch walks the choices whether or
   not a draft answer has been typed.
4. **The dropdown gets published geometry, not a bottom-up arithmetic.** The autocomplete
   dropdown has no rectangle at all — `frameSpans` deliberately keeps its entry as the
   zero span. It gains one the way every other pane has one: the frame PUBLISHES where it
   landed while stacking it. No second prefix sum is hand-written from `m.height`
   downwards.

## Standing requirements

- skills: coding-standards
- No version bump: `VERSION` and the `CHANGELOG.md` release headings are untouched by
  every item here. Each item's own `[Unreleased]` changelog entry is the verifier's
  ordinary job.
- ADR 0011 holds throughout: everything added here is plain values on the value-copied
  Model — no `strings.Builder`, no pointer, no map, no self-referential no-copy type.
- Any authorized deviation from an item's text lands as a dated `NOTES` line under that
  item.

## Out of scope

- **Click-to-select on these panes.** Item 2 publishes the dropdown's rectangle, which is
  exactly what a click would also need, but no click routing is added here. The wheel is
  the reported defect; the pointer's other gestures are a separate plan.
- **Drag-scrolling, scrollbar gutters or momentum** on any overlay.
- **The `/settings`, `/usage` and `/inspect` panes' own wheel behaviour** — they already
  work and their handlers keep their current shape. Item 1 does not rewrite them.
- **`listWrap` for the keyboard.** Every pane keeps the wrap answer it has today.

## Execution note

Items 3–7 each touch `internal/tui/mouse.go` (the routing chain) and its test file, so
their declared Files overlap by construction and they run SERIALLY. That is deliberate:
each of those items is independently verifiable end-to-end — the notch actually reaches
the pane through `Update` — which is worth more than the wall-clock a wave would save.
Items 1 and 2 are disjoint from each other and from everything else.

---

## 1. Give the shared list module a wheel contract — ✅ DONE (2026-08-22)

NOTES (2026-08-22): `wheel` guards `n < 1` explicitly instead of leaning on `listCursor.move`'s own
`n == 0` early return — `move` clamps against `n-1`, so a negative count would seat the highlight at a
negative index rather than moving nothing. The item's stated contract ("`n < 1` moves nothing") is
unchanged; only the mechanism the parenthetical assumed is.

**What:** add `listCursor.wheel` to `internal/tui/listsurface.go`, beside
`listCursor.key`, as the package's one answer to "what does a wheel notch do to a list
overlay":

```go
func (l *listCursor) wheel(msg tea.MouseWheelMsg, n int)
```

It moves the highlight one row per notch — `tea.MouseWheelUp` by −1, `tea.MouseWheelDown`
by +1, every other button by nothing — through the existing `listCursor.move` with
`listStopsAtEnds` passed unconditionally, so the clamp is structural rather than a
parameter a caller can get wrong. It takes no `listWrap` argument for that reason: a wheel
that wraps is not a scroll. `n < 1` moves nothing (`move` already clamps, and a list with
no rows has no highlight to walk).

Document it in the module's own register: state ratified design call 1 there — the keys
walk a list as a cycle, the wheel scrolls it and stops at the ends — and note that
`settingsWheel` and `reportWheel` are the two panes that stated the rule first and keep
their own row arithmetic (they scroll a WINDOW and a caret respectively, not a bare
cursor), so this method is the shared answer for the five panes that hold a `listCursor`.

Add the rule to the module doc comment's bulleted "What a list surface is, stated once"
list: what ↑/↓ do at the ends is a parameter (`listWrap`), and what the WHEEL does at the
ends is not.

This item adds no caller — the five panes adopt it in items 3–6. An unused method is a
legal Go build; the acceptance below is the unit test.

**Files:** `internal/tui/listsurface.go`, `internal/tui/listsurface_test.go`

**Tests:** in `internal/tui/listsurface_test.go`, table-driven over the existing test
idiom in that file:
- a notch up and a notch down each move the highlight exactly one row;
- an up notch on row 0 stays on row 0, and a down notch on the last row stays on it —
  the clamp, asserted against a cursor whose pane wraps for the keyboard, so the two
  answers are pinned as deliberately different;
- `n == 0` and a cursor pointing past the end move nothing and do not panic;
- a wheel button that is neither up nor down (e.g. `tea.MouseWheelLeft`) moves nothing.

**Acceptance:**
- `go build ./internal/tui/`
- `go test ./internal/tui/ -run 'TestListCursorWheel' -count=1`

**Commit:** `feat(tui): give the shared list surface a wheel contract`

---

## 2. Publish the autocomplete dropdown's rectangle — ✅ DONE (2026-08-22)

**What:** the dropdown is the one open overlay with no geometry — `frameSpans`'s doc says
so outright ("The autocomplete dropdown's entry is always the zero span: it is in the
other slot, hugging the input box, and no pointer addresses it by rectangle"). Give the
input-side slot the same treatment the transcript-side slot already has, in
`internal/tui/model.go`:

- Add `stackInputSlot(rows []string, ov frameOverlays, y0 int) ([]string, blockSpan)`
  beside `stackTranscriptSlot`, built on the same shape: append the dropdown block when
  `ov.dropdown != ""`, measure it with `lipgloss.Height`, and report where it landed. The
  staged-interjection strip (`ov.queued`) is stacked by it too but is NOT a `framePane`
  and gets no span — it is appended below the dropdown, unchanged, so the slot is composed
  in one place rather than two.
- Have `stackTranscriptSlot` report the screen row directly BELOW the slot it stacked, so
  the input slot's `y0` is `thatRow + topRuleHeight + statusHeight` and no term of the sum
  is restated. Follow `settingsPaneRect`'s stated reason verbatim: an omitted term is
  exactly the off-by-one that puts a gesture on the wrong row.
- Write the returned span into `frameSpans.panes[paneDropdown]` in both places a frame is
  composed: `View` (which currently discards the spans with `rows, _ =`) and
  `Model.frameSpans` (which walks the slot without painting the transcript). The two must
  agree by construction, not by two arithmetics that agree today — same doctrine
  `stackTranscriptSlot` already carries.
- Update the `frameSpans` doc comment: delete the "always the zero span" paragraph and
  replace it with what is now true — the dropdown's span is published from the OTHER slot,
  and the zero span means for it what it means for every pane, that it is not on this
  frame.

Load-bearing standards for this item: one deep function owns the slot walk (do not inline
a second copy in `View`); the geometry is derived once and read, never re-derived per
gesture; and the layering direction is unchanged — `stackInputSlot` is a pure function of
`(rows, ov, y0)` exactly as its sibling is, with no `Model` receiver.

**Files:** `internal/tui/model.go`, `internal/tui/chromelayout_test.go`

**Tests:** in `internal/tui/chromelayout_test.go`, beside the existing frame-geometry
tests:
- with a dropdown open at a known window size, `m.frameSpans().pane(paneDropdown)` reports
  `ok == true` and a `y0`/`h` that match where the dropdown's first line actually appears
  in `m.View()` — find the line in the rendered frame rather than recomputing the sum, so
  the test fails if the two ever diverge;
- the same assertion with a staged-interjection strip ALSO up, pinning that the strip sits
  below the dropdown and does not shift the reported rectangle;
- with the dropdown closed, `pane(paneDropdown)` reports `ok == false`;
- the published `paneBrowser` / `panePicker` / `paneSettings` spans are unchanged by this
  refactor for a frame that has one of them open — the transcript-slot geometry is not
  allowed to move.

**Acceptance:**
- `go build ./internal/tui/`
- `go test ./internal/tui/ -run 'TestFrameSpans|TestChrome|TestDropdownSpan' -count=1`

**Commit:** `refactor(tui): publish the input-side slot's geometry for the dropdown`

---

## 3. The /sessions browser takes the notch over it — ✅ DONE (2026-08-22)

NOTES (2026-08-22): `browserWheel` short-circuits on a CLOSED browser before asking
`m.frameSpans().pane(paneBrowser)`, rather than gating on the rectangle alone as the item's text
reads. The rectangle is still the only gate that decides anything; the guard exists because the wheel
path publishes no frame, so an unguarded `frameSpans()` would compose (and render) every overlay of
the frame on every notch the transcript owns — the reason `settingsPaneRect` and `reportPaneRect`
state verbatim for the same short-circuit.

Depends on item 1.

**What:** add `browserWheel` to `internal/tui/sessions.go`, modelled on `settingsWheel`
(`internal/tui/mouse.go`) line for line:

```go
func (m Model) browserWheel(msg tea.MouseWheelMsg) (Model, bool)
```

- Gate on the rectangle: `m.frameSpans().pane(paneBrowser)`, `ok == false` or a `msg.Y`
  outside `[y0, y0+h)` returns `handled == false` and leaves the notch to the transcript.
- Inside the rectangle it is ALWAYS `handled == true`, in every state the browser can be
  in — the browser is modal, and a notch over it must never reach the transcript behind
  it. While a rename edit or a delete confirm is live (`m.sessionBrowser.renaming`,
  `.confirming`) the notch is swallowed and moves NOTHING: those are modal surfaces of
  their own that own the pane until they are answered, exactly as `settingsWheel` treats
  its value buffer and its armed reset.
- Otherwise walk the highlight with the item-1 contract against the FILTERED row count —
  `m.sessionBrowserView()`, the one derivation every key route and the painter already
  share — so a notch and an ↑ can never disagree about which record is highlighted.

Route it in `foldMouseWheel` (`internal/tui/mouse.go`) after the inspector and before
`scrollViewport`, with a comment stating what the others state: the panes never share a
frame, so the order among them is arbitrary; what is not arbitrary is that every pane is
asked before the transcript.

**Files:** `internal/tui/sessions.go`, `internal/tui/mouse.go`,
`internal/tui/mouse_test.go`

**Tests:** in `internal/tui/mouse_test.go`, cloning the shape of the existing usage and
inspector wheel tests (they build a Model, open the pane, and drive `tea.MouseWheelMsg`
through `step`):
- a down notch inside the pane moves the highlight one row; two up notches return it;
- the clamp at BOTH ends, asserted against the browser's wrapping ↑/↓ so the difference is
  pinned deliberately;
- a notch above `paneTop` leaves the browser's highlight untouched (it belongs to the
  transcript);
- a notch inside the pane while `renaming` and again while `confirming` changes neither
  the highlight nor the transcript's offset;
- with a filter typed that prunes the rows, the highlight walks the FILTERED list and
  clamps to its length.

**Acceptance:**
- `go build ./internal/tui/`
- `go test ./internal/tui/ -run 'TestBrowserWheel|TestSessionBrowser' -count=1`

**Commit:** `feat(tui): scroll the /sessions browser with the mouse wheel`

---

## 4. The /model | /server picker takes the notch over it

Depends on items 1 and 3.

**What:** add `pickerWheel` to `internal/tui/picker.go` with the same signature and the
same three rules as item 3, against `m.frameSpans().pane(panePicker)` and the picker's own
filtered view (`m.picker.view(m.pickerOfferingRows())`, the derivation `pickerKey` and the
painter already share — the highlight indexes the FILTERED rows, so walking anything else
would move the human to a row they cannot see).

The picker is modal, so a notch inside its rectangle is always `handled == true` whatever
`m.picker.kind` is open — including the two-step `/schedule` flow and the start-up key
migration, where the highlight walks the offering the CURRENT step is showing and nothing
else changes. No kind is special-cased: the rows come from `pickerOfferingRows`, which
already answers per kind.

Route it in `foldMouseWheel` directly after the browser.

**Files:** `internal/tui/picker.go`, `internal/tui/mouse.go`,
`internal/tui/mouse_test.go`

**Tests:** in `internal/tui/mouse_test.go`, the item-3 battery applied to the picker:
one-row step both ways, clamp at both ends against its wrapping keys, a notch above
`paneTop` leaving it untouched, and a filtered offering walked by the filtered count. Plus
one kind-coverage case: with a `/schedule` step open, a notch walks that step's rows and
leaves `m.picker.draft` untouched.

**Acceptance:**
- `go build ./internal/tui/`
- `go test ./internal/tui/ -run 'TestPickerWheel|TestPicker' -count=1`

**Commit:** `feat(tui): scroll the /model and /server picker with the mouse wheel`

---

## 5. The approval and ask prompts take the notch inside their box

Depends on items 1 and 4.

**What:** the two decision panes share one rectangle (`panePrompt` is documented as "the
approval or the ask prompt"), so they share one handler. Add `promptWheel` to
`internal/tui/approval.go`:

```go
func (m Model) promptWheel(msg tea.MouseWheelMsg) (Model, bool)
```

- Gate on `m.frameSpans().pane(panePrompt)` and `msg.Y`. Outside the rectangle:
  `handled == false`, and the transcript scrolls exactly as it does today. This is the
  whole of what keeps ratified design call 2 compatible with the panes' soft-modality —
  they are soft-modal about the SURFACE UNDER THEM, and there is no surface of theirs
  under the pointer when the pointer is in their box.
- Inside it, walk whichever pane is actually up, using the item-1 contract:
  `m.approvalSel` over `len(approvalMenu)` while a request is pending, `m.askSel` over
  `len(m.pendingAsk.Request.Choices)` at `stateAwaitingAsk`. Both already walk with
  `listStopsAtEnds` for the keyboard, so the wheel and the arrows agree at the ends here
  by construction.
- Per ratified design call 3, the ask branch does NOT test `m.input.Value()`: the D5 guard
  exists because ↑/↓ double as the textarea's cursor keys, and a wheel has no second duty.
  State that reason in the doc comment — it is the one place this pane's wheel and its
  keys deliberately disagree.
- An ask question with no choices, or a prompt rectangle up with neither state live, is
  `handled == true` (the pane is under the pointer) and moves nothing.

Route it in `foldMouseWheel` after the picker.

**Files:** `internal/tui/approval.go`, `internal/tui/mouse.go`,
`internal/tui/mouse_test.go`

**Tests:** in `internal/tui/mouse_test.go`:
- with an approval pending, a notch inside the box walks `approvalSel` one row and clamps
  at both ends;
- with a choice question up, the same for `askSel`;
- with a choice question up AND a non-empty draft in the input box, a notch still walks
  the choices — the D5 asymmetry, pinned;
- a notch OUTSIDE the box with either pane up scrolls the transcript and leaves both
  cursors untouched — the soft-modal floor;
- a multi-select question: a notch moves the highlight and leaves `m.askChecked`
  untouched (the wheel walks, ␣ ticks);
- an ask question with zero choices swallows the notch and panics on nothing.

**Acceptance:**
- `go build ./internal/tui/`
- `go test ./internal/tui/ -run 'TestPromptWheel|TestApproval|TestAsk' -count=1`

**Commit:** `feat(tui): scroll the approval and ask prompts with the mouse wheel`

---

## 6. The autocomplete dropdown takes the notch over it

Depends on items 1, 2 and 5.

**What:** add `dropdownWheel` to `internal/tui/autocomplete.go`, gated on the rectangle
item 2 published (`m.frameSpans().pane(paneDropdown)`) and walking `m.autocomplete`'s
embedded `listCursor` over `len(m.autocomplete.items)` with the item-1 contract.

This is the one list here that is NOT modal — it hangs over a chat box the human is still
typing in — and its wheel keeps that posture the same way its keys do: `handled == true`
only for a notch inside its rectangle, and only when it has items to walk; everywhere else
`false`, so the transcript scrolls. It is otherwise the same three rules as items 3–5.

The dropdown is re-derived on the next keystroke (`recomputeAutocomplete`), so a wheel that
moves the highlight moves nothing else: no filter, no splice, no dismissal.

Route it in `foldMouseWheel` after the prompt.

**Files:** `internal/tui/autocomplete.go`, `internal/tui/mouse.go`,
`internal/tui/mouse_test.go`

**Tests:** in `internal/tui/mouse_test.go`:
- with a "/" dropdown open, a notch inside its rectangle walks the highlight one row and
  clamps at both ends (against its wrapping ↑/↓);
- with an "@" file dropdown open, the same;
- a notch above the dropdown's rectangle (over the transcript) leaves the highlight
  untouched;
- a notch does not dismiss the dropdown and does not alter the input box's value;
- a frozen dropdown left open when a modal prompt arrives: the prompt's rectangle wins for
  a notch inside it (this is the ordering item 5 and this item establish together).

**Acceptance:**
- `go build ./internal/tui/`
- `go test ./internal/tui/ -run 'TestDropdownWheel|TestAutocomplete' -count=1`

**Commit:** `feat(tui): scroll the autocomplete dropdown with the mouse wheel`

---

## 7. State the completed doctrine once

Depends on items 1–6.

**What:** the package's prose still describes the world before this plan. This item owns
every cross-cutting doc amendment so no earlier item has to reach outside its scope:

- `internal/tui/mouse.go` — `foldMouseWheel`'s doc comment currently says a notch goes "to
  whichever open pane holds the pointer, and to the transcript everywhere else" while
  routing only three panes. Make it true: name the full chain in the order it asks, state
  that the transcript is the floor rather than the default, and state ratified design
  call 2 — a soft-modal pane's box owns the notch inside it, and only inside it.
- `internal/tui/mouse.go` — the module doc's pane inventory (the paragraph listing what
  takes its rows OFF the transcript) gains the wheel's answer beside the click's.
- `internal/tui/doc.go` — the paragraph on the list module and the panes that borrow it
  gains one sentence: the wheel contract is the module's, the clamp is unconditional, and
  the two panes with their own row arithmetic (`settingsWheel`'s caret,
  `reportWheel`'s window) keep it.
- `layout.md` — if the spec states which surfaces the wheel scrolls, bring it in line;
  if it does not, add one sentence saying every open overlay scrolls under the pointer.
- `ISSUES.md` — remove any open entry describing this defect, if one is registered. The
  closed trail is the changelog's alone.

No behaviour changes in this item; no code outside doc comments.

**Files:** `internal/tui/mouse.go`, `internal/tui/doc.go`, `layout.md`, `ISSUES.md`

**Tests:** none of its own — this item changes prose only. `docmap_test.go` (the package's
doc-drift guard) must still pass, which is the check that the doc comments this item
rewrites still name real symbols.

**Acceptance:**
- `go build ./internal/tui/`
- `go test ./internal/tui/ -run 'TestDocMap|TestDoc' -count=1`
- `go vet ./internal/tui/`

**Commit:** `docs(tui): state the pane-wheel routing doctrine once`

---

## Suggested version bump

Not performed by this plan. Four panes gaining a gesture they did not have is a user-
visible feature, so a `VERSION` micro-bump (v0.15.10 → v0.15.11) looks warranted at
closeout — the owner's call, and only on an explicit instruction.

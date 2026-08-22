---
Status: accepted
---

# Popup surfaces embed one list surface

## Context

`internal/tui` paints eight popup-bordered surfaces — the /model | /server picker, the /sessions
browser, the /settings pane and its sub-lists, the autocomplete dropdown, the approval and ask
prompts, the /usage and /inspect reports — and `popup.go` had already deepened the **painting** of
all of them into one module. Nothing had deepened their **state**. The 2026-08-19 TUI architecture
review (`docs/reviews/2026-08-19 - 00 - tui-architecture-review.md`, Candidate 1) measured what that
cost, counting sub-modes as ~21 list variants over six files:

- `clampSelection` written three times, verbatim-equivalent.
- The wrap-around arrow idiom (`sel = (sel-1+n)%n` on ↑, its mirror on ↓) written **eight** times,
  plus two non-wrapping variants and two scroll-only ones — **four different answers** to "what does
  ↓ do at the bottom of the list", none of them recorded as a decision.
- The type-to-filter block (printable rune appended, backspace by rune, re-clamp) written twice
  byte-for-byte, comment included.
- The budget→render boilerplate (`popupFloor` claim → `popupBudget` → seated check → `renderPopup`)
  written **seven** times.

Two further facts bounded the design. First, the deepened shape was already proven in-package:
`filterPopupRows` / `pickerView` had two callers and exists precisely to kill the
accept-against-the-unfiltered-list bug class — with a filter set, "the third painted row" and "the
third model advertised" are different rows. Second, the state that needed a home was already
directly testable in principle but not in practice: "↓ at the bottom of a **filtered** list" was
reachable only by opening a real overlay and pressing keys through `Model.Update`, once per pane, a
tax ~13,400 test lines over these surfaces were paying.

This record ratifies the shape introduced by
`docs/plans/2026-08-19 - 04 - tui-architecture-deepening-plan.md` item 15. It supersedes nothing.

## Decision

**Every popup surface that is a LIST embeds one `listSurface`, and a pane's own code is its rows,
its accept and its wording.** The module is `internal/tui/listsurface.go`
([ADR 0043](0043-files-split-by-concern-and-config-gets-a-package.md): one file per concern, one
`doc.go` line).

**1 — The surface owns the state a list keeps between keypresses, and nothing else.** That is two
values: where the highlight stands, and what has been typed into the filter. Everything a list shows
is derived per frame from the state it describes and handed IN — a beat carrying a shorter offering
or a re-listed session store refreshes the pane in place, and the surface holds nothing that could
go stale against it.

**2 — The surface owns the key contract every modal list shares.** esc, ↑/↓ (and ^p/^n),
type-to-filter with backspace as its undo, and the ⏎ that lands on a row. Both clamps of a
keypress — before anything acts, and again after a key that moved the filter — read the same
composition of the list, so the count a key is measured against and the count it leaves behind can
never come from two different readings.

**3 — The surface decides nothing about what a key MEANS.** It answers with a **verdict**
(`listCloses`, `listAccepts`, `listSwallowed`, `listUnclaimed`) and the pane spends it. Closing an
overlay, resuming a session, rebinding a model are the pane's own acts with the pane's own
consequences; `listUnclaimed` is what leaves room for a pane's own verbs (the browser's ^r / ^d / ^a)
without this module knowing they exist.

**4 — What ↑/↓ do at the ENDS is a parameter, not a rule.** `listWrapsAround` / `listStopsAtEnds` is
passed per call, and every pane keeps exactly the answer it had: the picker and the /sessions browser
wrap, the approval and ask selections stop. The review found four undocumented answers; this makes
the difference a stated argument at the call site rather than an idiom re-spelled per pane.

**5 — Every accept resolves through the filter.** The highlight indexes the FILTERED rows, and
`pickerView.offeringIndex` maps it back to the list underneath before anything is touched. A pane
whose list can move between the two reads of one keypress still bounds-checks what comes back.

**6 — The filter LINE rides the surface, not the painter's spec.** Its label, its caret, its two
blank lines, the budget claim for all three, and the trade a short window makes (the rows shrink, the
line the human is typing stays) are stated once in `renderList`. A pane states its rows, its title,
its hint and its slot in the frame; it cannot forget the pads or set them out of step with the claim.

**7 — It is a plain value, embedded.** No lock, no self-pointer, no no-copy type, so it lives inline
in the `Model` that is copied on every `Update`
([ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md)). It is embedded rather
than named, so `m.picker.selected` and `m.picker.filter` still read as the pane's own fields. Its
zero value is "nothing typed, first row highlighted" — the state a whole-struct reset
(`m.picker = picker{}`) already leaves behind, so no close path needs to learn about it.

**8 — Which panes.** Every list: the picker (all seven `pickerKind`s), the /sessions browser (all
three modes), the /settings sub-lists, autocomplete's two variants, and the approval and ask
selections with wrapping off. The read-only **reports** (/usage, /inspect) deliberately stay on
`reportPane`: a report has no selection and no filter, it is scrolled rather than walked, and forcing
it here would hand it two fields it never reads.

**9 — A pane with several sub-lists NAMES its surfaces; it does not embed several.** A surface's
filter is a `lineEditor`, and a `lineEditor` is a `textarea` — thousands of bytes in a `Model` that
is copied on every `Update`. Adopting this module must move fields, never multiply them: a pane whose
sub-lists cannot share one surface holds them as named fields and reuses the editor it already has.

## Amendment (2026-08-20) — the surface is two values: a cursor every list embeds, and the field only a filtering list adds

Decision 9 bounded what adopting this module may cost; the remaining panes then made the bound bite.
Item 16 of the deepening plan moved the /settings key list, its two sub-lists and the "/" | "@"
dropdown onto the module, and **none of those four filters** — /settings is walked with arrows and
answers backspace with an armed reset, and the dropdown is narrowed by the token in the chat box it
hangs over. Handing each of them a `listSurface` would have handed each of them a `lineEditor` it
never types into: **+40,704 bytes** on a `Model` copied on every `Update` (three widgets at 13,568
bytes), which is exactly the multiplication decision 9 forbids.

So the value is split where the panes already split:

- **`listCursor`** — `{selected}`, eight bytes — is where the highlight stands and the key contract
  that walks it: `clampSelection`, the wrap rule, `highlight`, and `key` (esc, ↑/↓ and ^p/^n, the ⏎
  that lands on a row, everything else handed back as `listUnclaimed`). Every list in the package
  embeds it.
- **`listSurface`** — that cursor plus `filter lineEditor` — is a list that also FILTERS, and adds
  only the keys that type: `Model.listKey` asks the cursor first and claims the printable keys and
  backspace out of what it hands back. The picker and the /sessions browser are its two panes.

Decisions 1–8 stand as written for a filtering list; decision 1's "two values" is now two values in
the surface and one in the cursor, and decision 8's "every list embeds" is "embeds the one of the two
its pane is". `Model` measured **104,592 bytes before and after** item 16, which is decision 9 held.

**Adoption note (2026-08-20, item 17).** The two DECISION panes take the cursor a third way, and
decisions 4, 7 and 8 read this way for them. The approval menu (`approval.go`) and the ask_user
offering (`ask.go`) have no pane struct to embed it in — their state IS the `Model`'s own — so they
**name** it (`m.approvalSel`, `m.askSel`), and they take `move` and `highlight` **without `key`**:
both panes are soft-modal, so every key they do not claim has somewhere else to be
(the transcript under the approval prompt, the answer box under the question), and which keys they
claim is a fact about the surface underneath them rather than about lists. Each says it at its own
switch. What decision 4 promised is delivered whole — `listStopsAtEnds` is a stated argument at both
call sites now, and the two non-wrapping arrow idioms the review counted are gone.

The render call split the same way, for the same reason: `renderList` takes a pane's own body block
(the /settings sub-list's question — the pane it replaced is where the human read the key's name),
and `renderFilterList` fills that block with the filter line, its label and its two pads before
delegating. Decision 6 is unchanged — the line is still stated in exactly one place.

## Consequences

- The marginal cost of a new list pane is **rows + accept**. Adding one no longer means re-deriving
  a clamp, a wrap rule, a filter and a budget call.
- "↓ at the bottom of a filtered list" is a direct unit test on `Model.listKey`
  (`listsurface_test.go`) instead of a scripted overlay session, and it is proved for both wrap
  answers at once.
- The three answers a list used to give per pane — the clamp, the wrap, the accept target — became
  one, and every pane that adopts the module deletes its copy.
- Behaviour is unchanged everywhere by construction: the wrap flag, the filter semantics and the
  budget claim are the same values the panes were passing, now passed from one place.
- `popupSpec.bodyPadAbove` / `bodyPadBelow` remain the painter's contract, because the pads are spent
  against the WRAPPED body's height inside `renderPopupPlaced` and dropped whole when they do not
  fit — a caller cannot compute that. What changed is that `listsurface.go` is now their only setter.
- The `Model` did not grow: adopting the module in the picker and the browser moved four fields into
  two embedded values and cost **nothing** (104,600 → 104,592 bytes). Decision 9 is what keeps that
  true as the remaining panes adopt it.

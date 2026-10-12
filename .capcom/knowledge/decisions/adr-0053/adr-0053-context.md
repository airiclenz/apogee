---
title: ADR 0053 context
---

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

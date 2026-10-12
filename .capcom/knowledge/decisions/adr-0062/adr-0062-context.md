---
title: ADR 0062 context
---

The release checklist for v0.17.1→HEAD
(`docs/test-checklists/2026-08-27 - 00 - since-v0.17.1.md`) shipped with **19 of its 25 items
marked manual** — T-03, T-04, T-06, T-07, T-10–T-16, T-18–T-25. Reading them, almost none of those
claims is actually subjective: "the streamed reply commits complete and in order", "the approval
keys are armed before the pane paints", "a hostile tool name cannot forge a row", "the outcome slot
reports the tool's data, not its prose". Those are assertions about bytes on a screen. They became a
human's job for three structural reasons.

**1 — The TUI had no driver.** Nothing in the repo can start the real composition, type into it and
read back what the terminal shows. The existing tests reach `tui.Model` with a `fakeEngine`, which
proves the reducer and says nothing about the program that runs it — not the Bridge, not the event
sink, not the renderer, not the terminal. Anything that only goes wrong once bytes reach a terminal
was, by construction, unobservable.

**2 — Every upstream in tests was an ad-hoc `httptest` closure.** Each test that needed a streamed
reply hand-rolled its own SSE frames. Nothing was reusable, so per-token timing, `reasoning_content`,
tool calls, `usage.prompt_tokens_details.cached_tokens`, HTTP failures and hangs were each
re-invented per test — or, more often, skipped and handed to the checklist.

**3 — The judgment halves had no observer at all.** "The blast-radius wording reads as one row",
"the pane's wording is honest about what was refused", "the manual matches what the binary does":
no assertion form existed for a claim about wording or shape, so those halves went to a human's
eyes by default and dragged their mechanical siblings with them.

ADR 0031
already settles the principle this record applies: the embeddable engine must stay **sufficient for
any Driver**, and everything must stay benchable all the way up. A test harness that needs a hook
the engine does not already offer is evidence the engine is *not* sufficient. So a test harness is
not a special case to be granted privileges — it is another **Driver** (CONTEXT.md), and building it
exercises the north star instead of bending it.

The kit's shape was ratified with the owner on 2026-08-27 and is realised item-by-item by
`docs/plans/2026-08-27 - 02 - test-drivers-kit-plan.md`. Two existing invariants bound it:
ADR 0011 (the `Model` is copied by
value on every `Update`, so no no-copy type may reach it) and
ADR 0010 (`internal/` never imports the
root module path).

---
title: Pop-up formatting redesign + click-to-select on every boxed pop-up
status: done
type: feature
priority: 2
plan: 2026-09-06 - 00 - popup-breathing-rows-and-click-to-select-plan
aliases: [apogee-p94]
created: 2026-09-06T08:48:03Z
closed: 2026-09-06T15:56:15Z
reason: "Delivered by plan 'docs/plans/archived/2026-09-06 - 00 - popup-breathing-rows-and-click-to-select-plan.md' (13 items, commits 5e2eddd8..188be5a6, closeout a1d54871): breathing rows in every boxed pop-up, eleven goldens held, click-to-select on all five wheel-only pop-ups."
---

Improve the formatting of every boxed pop-up (all paint through renderPopup in internal/tui/popup.go), the ask_user question dialog above all, and let the pointer pick an option. Baseline golden frames of all seven pop-up states were recorded 2026-09-06 in cmd/apogee/testdata/frames/popup-*.txt (TestE2EPopupFrames*); the owner edits those frames into the target layout, then an implementation plan (docs/plans/, /implement-plan Write mode) is written against them. Clickability is independent of the layout and can be planned now.

## Design

Ratified 2026-09-06 (do not re-ask): (1) sequencing = frames first, owner hand-edits them, plan written against the edited frames; (2) frames cover ask single/multi/typed/free, approval, picker, /sessions browser, / dropdown; (3) click scope = all five wheel-only pop-ups (ask, approval, picker, sessions browser, dropdown) via one shared row hit-test over popupPlacement + frameSpans.pane, the way /settings already does it (mouse.go handleSettingsClick); (4) click meaning = first click moves the highlight (multi-select: toggles the box), a click on the already-highlighted row acts as Enter; never select-and-send in one click. Canon: docs/layout/user-questions-layout.md; wheel doctrine in mouse.go (pane under the pointer owns the event).

## Log

- 2026-09-06 beads: later: the owner hand-edited the frames BEFORE commit c45ed545, so the committed popup-*.txt ARE the target designs; t10-forced-pane.txt (60-col forced approval pane, e2e_approval_test.go) is a design too by the owner's word — NINE goldens in all. TestE2EPopupFramesLists/Prompts and the t10 golden comparison are gated behind the cmd/apogee test flag -popup-design (go test ./cmd/apogee -run 'TestE2EPopupFrames|TestE2EApprovalForcesALook' -popup-design -v): skipped under make check, failing by design with the flag, their diff is the spec; never -update before the layout lands; the plan's layout item deletes the flag, the skips and the t10 gate. Design delta so far: one blank row under a pane's heading (title row / Reason: line / Fix: block / free-text question) and one blank row above its hint row; the choice-bearing ask panes already breathe (askRowStyle) and matched unchanged; the owner was still editing popup-approval.txt (Reason: with two spaces?) at hand-off — read the tree. Handoff: docs/handoffs/2026-09-06 - 00 - popup-redesign-plan-handoff.md (untracked by design, docs/handoffs/.gitignore) with open calls A-E, the sub-agent survey list and a suggested plan shape.
- 2026-09-06 beads: (later): four more frames are designs, gated the same way: t10-forced-pane (blank row under Reason: and under the Fix: block), t12-pane-60 (blank row under Reason:), t16-settings-rows (blank row above the hint row -> the rule reaches /settings, resolving open call A as uniform), popup-approval unchanged (its double-space edit was a slip, owner confirmed; committed frame stands). Eleven goldens in all.

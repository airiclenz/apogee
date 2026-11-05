---
title: "Tools super-group header: per-tool-kind breakdown instead of total calls only"
status: done
type: task
priority: 2
plan: 2026-11-05 - 01 - tools-header-breakdown-plan.md
created: 2026-11-05T18:04:40Z
closed: 2026-11-05T20:36:28Z
reason: "Resolved by plan '2026-11-05 - 01 - tools-header-breakdown-plan' (archived at c6fd5716): umbrella header paints a per-kind breakdown leader row with (N calls) fallback (829a65c4), docs/layout sketches updated (23a967e5)."
---

## What

The tool super-group's header line paints only a total call count — `✦ Tools (7 calls)` (internal/tui/toolblock.go, renderSuperGroup: `label := th.toolLabel.Render(superGroupLabel) + " " + th.toolIndicator.Render("("+plural(calls, superCallNoun)+")"`). The raw material for a per-kind breakdown is already at hand in the same function: `runs []toolRunView`, each run carrying its label (`views[0].Label`) and count (`len(r.views)`), and adjacent runs of the same tool are merged into one run — so `3 Read, 4 Terminal` is a one-pass reduction over the parameter.

Wanted header shape (owner sketch):

Expanded:

✦ Tools (3 Read, 4 Terminal) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
  ┝ Read ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ 17 lines ▶
  ┝ Terminal (2) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ exit 0 ▶
  ┝ Read ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ 85 lines ▶
  ┝ Terminal (2) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ 1 error ▶
  ┕ Read ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ 149 lines ▶

Collapsed:

✦ Tools (3 Read, 4 Terminal) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶

Two deltas from the current shape:
1. The count becomes a per-tool-kind breakdown ("(3 Read, 4 Terminal)") instead of "(N calls)". Keep the existing faint `toolIndicator` tone for the numbers, per the layout rule that the count is not part of the name (layout.md "never a target" + faint indicator tone).
2. The header carries leader dots (`toolLeader`, flexing to the 1-dot floor) from the breakdown to the trailing arrow — today the header has no leader row; dots exist only on the child/type rows. Filled header (no room for dots) should degrade the same way a type row does.

Open design questions for the plan to settle:
- Runs of 1 today print a bare `Read` with no `(1)` (groupCountFormat, typeRowText). In the header breakdown, does a single-call run count ("1 Read") or render bare? Suggest: always number in the header, since the breakdown is a count list.
- Does the header still state a total anywhere (e.g. when breakdown would overflow)? Suggest falling back to `(N calls)` on narrow rows.
- Interaction with the two-tier fold display (small umbrella session flag vs large umbrella `ui.tools-open`): none expected — this is a paint change only; the header stays marked `targetUmbrella` in both states.

## Why

The reader currently learns how the work divides only by unfolding. A per-kind breakdown answers "what kind of work was this?" on the header line itself, matching the glanceable-summary role the header already plays, at zero cost to the model (transcript is host-side paint; nothing reaches the wire).

## Acceptance

- An expanded super-group header reads `✦ Tools (3 Read, 4 Terminal) ▼` for 3 Read + 4 Terminal calls, matching the count tones of the current header.
- Same header collapsed reads `✦ Tools (3 Read, 4 Terminal) ▶` — the fold glyph is the only difference.
- Header carries a leader-dot run to the trailing arrow when width allows; degrades to 1 dot at the floor.
- Behavior for the existing tests pinning `✦ Tools (N calls)` (internal/tui/toolblock_test.go) is updated alongside — those pins assert the old shape.
- `docs/layout/tool-layout.md` and `layout.md` header sketches are updated to the new shape (docs specify this surface; layout.md defers to tool-layout.md where they disagree).

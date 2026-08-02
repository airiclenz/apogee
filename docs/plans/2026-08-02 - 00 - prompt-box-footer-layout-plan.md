# Plan — The gauge owns the window, the footer names the workspace, the box closes its frame

**Date:** 2026-08-02
**Status:** PLANNED — not started.
**Track:** TUI presentation wave for two ISSUES.md items: "work-dir path needs to be displayed
in the bottom instead of the context size" and "remove the frame from the lower part of the
prompt box". No engine, protocol, or persistence changes.

**Authoritative sources (precedence):** the mockup `docs/design/prompt-box-layout.md`
(currently untracked — item 1 commits it) and the owner-ratified decisions below outrank every
item's prose. `layout.md` is amended BY this plan, so where today's `layout.md` disagrees with
the mockup, the mockup wins. ADR 0011 / `internal/tui/doc.go` (the `Model` is value-copied —
no `strings.Builder` or other no-copy type held by value) and ADR 0030 (measure widths only
through `m.th.measure`) still bind every item. File:line anchors below are as of `1af67c2`
(2026-08-02) and may drift a few lines.

**Standing requirements:** forward skill `coding-standards` when executing
(`/implement-plan <this file> with skills: coding-standards`). Run `make check` before every
commit. Changelog bullets go under `## [Unreleased]` — never touch VERSION or a release heading.

**Owner-ratified decisions (2026-08-02) — implement, do not re-litigate:**

1. The `▔` hairline capping the bottom chrome STAYS. The mockup simply starts at the status
   line; its blank top rows stand for the transcript.
2. The gauge stays **self-hiding**: before the first UsageEvent the window shows nowhere but
   the startup box, and the right-slot key hints (`esc stop`, `enter dismiss`) keep the slot.
3. Token counts keep the technically correct SI prefix — lowercase `k` (`8k/98k 8%`), i.e.
   `formatTokens` is unchanged. The mockups' capital `K` is not to be copied.
4. The status line itself is unchanged — the mockup's `responding · 16s · 30t/sek` is sketch
   shorthand for the existing `phrase · clock · N tok/s`.

**The target, in the mockup's own rows** (frame chrome only — hairline kept per decision 1):

```
▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔
  responding · 16s · 30 tok/s                             8k/98k 8% █░░░░░
╭────────────────────────────────────────────────────────────────────────╮
│ A new prompt                                                           │
╰────────────────────────────────────────────────────────────────────────╯
  server-name ✦ model-name ✦ ~/Repos/apogee                   ask before
```

**Out of scope:** every other ISSUES.md bullet (sub-agent context display, click
responsiveness, popup spacer rows, keyboard collapse/expand, in-line skill marks, the `/skill`
idle-tag); the startup box (it keeps stating the context window); the heartbeat's transcript
change-notes (`context 32k → 16k` — the window stays a session fact, it just leaves the footer
chrome); any version bump.

**Working-tree note for Execute mode:** `ISSUES.md` is already dirty in the working tree — the
owner's own wording of the two issues this plan implements. Carrying that edit until item 4
retires the bullets is expected; tell the Phase 0 dirty-tree stop to proceed.

## 1. The context gauge names its window — `8k/98k 8% ▉…` — ✅ DONE (2026-08-02)

NOTES (2026-08-02): `docs/design/prompt-box-layout.md` was no longer untracked at execution time —
commit `731246a` (the plan's own commit) already carries it — so this item's "commit the mockup
with this item" clause was already satisfied and no change to that file was needed.

**What:**

- `contextUsage.view` (`internal/tui/model.go:3394`): the numeric prefix becomes
  `fmt.Sprintf("%s/%s %d%% ", formatTokens(c.Used), formatTokens(c.Limit), pct)`. Everything
  else stays exactly as documented there: self-hiding on `Used <= 0 || Limit <= 0`
  (decision 2), the percentage clamped to 100, `renderGaugeBar` untouched, and Used
  deliberately unclamped so an over-window fill reads `137k/98k 100%` — the over-window
  honesty rule in the function's comment must survive the respelling.
- `layout.md`: only the two gauge spellings — the opening sketch's status row (~line 50,
  `16k 50% █████░░░░░` → `16k/32k 50% █████░░░░░`) and the gauge example in "The status
  line's right slot" (~line 478). Do NOT touch the sketch's frame rows — those are item 3's.
- `CHANGELOG.md` under `[Unreleased]`: the gauge now reads `used/window pct%`.
- Commit `docs/design/prompt-box-layout.md` with this item — it is the authoritative mockup
  this plan implements and is currently untracked.

**Tests:** extend `TestUsageEventDrivesGaugeAndThroughput` and `TestContextGaugeBarRendering`
(`internal/tui/model_test.go:286,338`) to assert the `used/limit` prefix; keep
`TestGaugePercentClamped` (`internal/tui/heartbeat_test.go:541`) honest with the new prefix
(the clamped `%` beside an unclamped Used); `TestStatusLineGaugeEndsShortOfEdge`
(`model_test.go:2122`) must still hold — the slot grew by the `/limit` cells and must still
end `bodyIndent` short of the edge. The compact-flow gauge tests (`minilang_test.go`) assert
reset/leave semantics, not spelling, and must stay green unmodified unless they quote the
prefix.

**Acceptance:**

- `go test ./internal/tui/`
- `make check`

**Commit:** `feat(tui): the context gauge names its window (8k/98k 8%)`

## 2. The footer names the workspace, not the window — ✅ DONE (2026-08-02)

NOTES (2026-08-02): two small extensions past the item's literal rules for `workdirDisplay`, both to
keep it consistent with `newWorkspaceRoot` in the same file. (a) A degenerate home of `/` (or a bare
volume root) is treated as a boundary match rather than falling into "anything else": `/proj` against
home `/` reads `~/proj`. The literal "home + separator + rest" test would need a doubled separator
there and so would leave the path unchanged. (b) `path`/`home` are `strings.TrimSpace`d first, so a
whitespace-only workspace names nothing, exactly as `newWorkspaceRoot` treats one. Both cases are
pinned as rows in `TestWorkdirDisplay`.

Depends on item 1 (the window must already read in the gauge before it leaves the footer).

**What:**

- New pure helper `workdirDisplay(path, home string) string` in
  `internal/tui/workspacepath.go`: the cleaned workspace path respelled for display, with the
  home prefix replaced by `~` ONLY at a component boundary — `path == home` → `~`;
  `home + separator + rest` → `~` + separator + rest (native separators preserved); anything
  else, including a sibling like `/Users/username-other/...` against home `/Users/username`,
  is returned unchanged; empty `path` → `""`. `home` is a parameter so the function is
  testable off the real environment.
- Seed once per session, beside `newWorkspaceRoot` (`internal/tui/model.go:258`): `newModel`
  computes the display string from `opts.Workspace` and `os.UserHomeDir()` into a new Model
  string field (e.g. `m.workdir`) — resolved once, never re-derived per repaint. A
  `UserHomeDir` error means home `""`, which leaves the path unchanged.
- `upstreamSegments` (`model.go:3128`): drop `formatTokens(m.opts.ContextWindow)`. The
  footer's upstream facts are now the model slot alone; `connecting…` and
  `loading <profile>…` replace just that one word (the "replaced together" rule collapses —
  there is no window word left to pair with).
- `footerContent` (`model.go:3080`): the info join becomes
  `host ✦ <upstream> ✦ <workdir>` through the existing `nonEmpty`, so an empty workdir drops
  its segment and separator. The `✦ offline` marker stays appended at the END of the left
  slot in the error tone, exactly as today — now after the workdir.
- Docs owned by this item: `layout.md` "The footer's upstream slot" (~line 501) — the slot
  carries `host ✦ model ✦ workdir`, the window has moved to the gauge, the stand-in words
  replace the model word alone, and the workdir is a local fact that never changes with
  upstream state; the opening sketch's footer TEXT (~line 58) only (its frame rows are
  item 3's); the footer summary in `internal/tui/doc.go` (~line 30); a `CHANGELOG.md`
  `[Unreleased]` bullet.

**Tests:** table-driven unit tests for `workdirDisplay` (home respelled at the boundary; the
sibling-prefix case NOT respelled; exact home → `~`; empty path → `""`; empty home → path
unchanged); update the footer assertions that expect `32k` (`model_test.go:1940` and
neighbours) to expect the workdir form; `TestFooterShowsOfflineAndConnecting`
(`heartbeat_test.go:512`) — `connecting…` replaces the model word only, host and workdir stay
put; `TestActuationFooterNarratesTheVerb` (`actuation_test.go:312`) likewise.

**Acceptance:**

- `go test ./internal/tui/`
- `make check`

**Commit:** `feat(tui): the footer names the workspace, not the window`

## 3. The prompt box closes its frame; the footer sheds its — ✅ DONE (2026-08-02)

NOTES (2026-08-02): the item gained a **bottom hairline** mid-run. The owner amended the authoritative
mockup `docs/design/prompt-box-layout.md` to carry a full-width rule below the footer ("Now the status
line … is printed at the very bottom of the session chat. That does not look very good. Could we add a
hairline (just inverted for the bottom) below the status line?"), and the plan's precedence rule puts the
mockup over the item's prose. So the footer stays frameless in the sense the item means — no box borders
around it — and a `bottomRule` row closes the screen under it. Four consequences worth flagging:

1. **The glyph is `▁`, not the mockup's `_`.** The mockup is a plain-text sketch (the same file writes
   `8K/98K` and `30t/sek`, which ratified decisions 3 and 4 already say not to copy literally). The
   owner's word was *inverted*, and `▁` (LOWER ONE EIGHTH BLOCK) is the exact inversion of the existing
   `▔` (UPPER ONE EIGHTH BLOCK). Both render through ONE theme role, renamed `topDivider` → `hairline`
   for that reason. A literal `_` is a one-line change if the owner meant the underscore.
2. **Every row budget is unchanged from before this item.** The footer gave up two rows and the frame
   gained two (the box's own bottom border, the hairline), so `frameFixedRows` is 5 + a 2-row box border,
   `frameFloorRows` is **8** again, `smallestOverlayWindow` is **12** again, and `transcriptBudget` /
   `draftRowsCeiling` land on their original values. The numeric re-anchoring an earlier pass made
   (7-row floor, 11-row pane floor, the popup/band/draft-cap tables) has been reverted rather than kept.
3. `ruleHeight` is split into `topRuleHeight` and `bottomRuleHeight` (both 1). They are the same number
   under two names on purpose: what stands ABOVE the box and what stands BELOW it are different
   questions, and `inputContentRect` asks only the second — a merged constant is exactly the off-by-one
   that puts the caret on the wrong row.
4. Smaller deviations from the item's literal text: `footerView`'s `w < 3` guard is gone (a frameless row
   is exactly one line at every width, which is what makes `footerHeight` exact — the old guard returned
   `""`, still one row, and so was already a fiction); `TestFooterViewThinRules` is rewritten AND renamed
   to `TestFooterViewIsOneFramelessLine`, now forbidding every border glyph (the retired heavy rune
   included), which subsumes the thin-rule property it held; and `layout.md`'s "The footer's upstream
   slot" (item 2's section) gains two paragraphs — "What it is" and "And the hairline under it" — because
   there was no other prose home for the footer's new shape. New tests: `TestBottomRuleHairlineRow` and
   `TestClickOnBottomChromeSelectsNothing` (the box's bottom border, the footer line and the hairline,
   asserted as one contiguous unaddressable run ending on the terminal's last row).

Depends on item 2 (it reframes the footer line item 2 finished wording).

**What:**

- `internal/tui/theme.go:181` `inputBorder`: the box gets its full rounded border back — the
  bottom edge (`╰─╯`) is the box's own again.
- Constants (`internal/tui/model.go:2399-2431`): `footerHeight` 3 → 1 (one frameless content
  line — no divider, no bottom rule); `inputBorderRows` 1 → 2 (top + bottom). The derived
  `frameFixedRows` and `frameFloorRows` shift by the constants alone — the floor lands at
  **7 rows** (gap + hairline + status + box top border + one content row + box bottom border
  + footer line). Rewrite the surrounding comment block: every "the footer is three rows" and
  "the box has no bottom edge — the footer's divider" sentence.
- `footerView` (`model.go:3059`): collapses to the single content line — delete the `├──┤`
  divider and `╰──╯` bottom rule. `footerContent` takes the status line's posture
  (`statusLine`, `model.go:3255`): `bodyIndent` lead, segments on a black field filled to the
  full width, the mode marker ending `bodyIndent` short of the window edge — the same column
  the gauge ends in ("The status line's right slot", `layout.md`). No `│` borders, no
  one-column inner margins. Narrow-window semantics keep today's shape: too narrow for both
  ends, the left info truncates with `…` and the mode drops whole.
- `inputView` and its comments (`model.go:2983-3007`): the box owns its bottom edge;
  `inputElisionEdge` is untouched (it addresses the top border row only).
- `inputContentRect` (`internal/tui/mouse.go:130-141`): restate `boxTop` in the constants —
  `m.height - footerHeight - (h + inputBorderRows)` — so the mouse mapping cannot drift from
  the layout arithmetic; rewrite the "three-row footer" / "no bottom edge" comments.
- `layout.md` owned amendments: redraw the opening sketch's frame rows (~lines 51-59) to the
  mockup — the box closes with `╰…╯`, the footer is one frameless line below it; restate
  every "footer's three rows" mention (~lines 99, 116, 141) and the eight-row-floor section
  (~lines 159-168) at the new seven-row floor, keeping the section's meaning (the floor is
  asserted exactly, never clipped); confirm "The status line's right slot" cross-reference —
  the mode marker's end column — still reads true.
- `CHANGELOG.md` `[Unreleased]` bullet.

**Tests:** `TestFooterViewThinRules` (`model_test.go:1955`) rewritten for the frameless line
(black field to full width, `bodyIndent` margins, no border glyphs); the frame-height property
tests re-anchored to the 7-row floor and updated, never weakened — the floor assertions assert
the NEW floor exactly, not `<=`: `TestFrameNeverExceedsTheTerminalHeight`,
`TestFrameFitsEveryHeightDownToItsFloor`, `TestDecisionSurfaceStaysOnTheFrame`,
`TestFrameSurfacesGiveWayInOrder` (`sessions_test.go`),
`TestFrameRowBoundaryAgreesWithTheMouseMapping` (`mouse_test.go:1460`), the paint tests
(`paint_test.go:141,211`), and the queued-band budget tests (`interject_test.go:630,658`).
Mouse-mapping tests must cover a click on the box's new bottom border row (selects nothing in
the box) and on the footer line.

**Acceptance:**

- `go test ./internal/tui/`
- `make check`

**Commit:** `feat(tui): the prompt box closes its frame and the footer sheds its`

## 4. Retire the two issues — ✅ DONE (2026-08-02)

Depends on items 1-3.

**What:** delete the two solved bullets from `ISSUES.md` — the "work-dir path needs to be
displayed in the bottom" bullet and the "remove the frame from the lower part of the promt
box" bullet — leaving every other bullet untouched. The owner's pre-existing working-tree edit
of `ISSUES.md` lands here at the latest. Confirm `CHANGELOG.md` `[Unreleased]` carries the
three bullets from items 1-3.

**Tests:** none (docs only).

**Acceptance:**

- `! grep -q "work-dir path needs to be displayed" ISSUES.md`
- `! grep -q "remove the frame from the lower part" ISSUES.md`
- `make check`

**Commit:** `chore(issues): retire the footer-path and prompt-box-frame issues`

---

**Suggested version bump:** minor (`0.10.4` → `0.11.0`) — a user-visible TUI layout change
(gauge format, footer content, frame shape), additive and non-breaking. The owner decides; no
item in this plan touches VERSION or a release heading.

# Mouse tracking re-assert — implementation plan

**Goal:** a tool child that resets the terminal's mouse tracking behind apogee's back must
no longer leave block toggles and drag-select dead for the rest of the session: apogee
re-asserts cell-motion tracking itself after every tool result and resize. Two latent
row-map / decoder bugs found in the diagnosis are closed alongside, and `--tui-diag`
gains the mouse lines that would have made this diagnosable.

**Date:** 2026-08-29 · **Status:** unexecuted · **sized for:** ~200k-context host

**Authoritative sources:** `docs/handoffs/2026-08-29 - 00 - mouse-toggle-dead-mid-session-diagnosis.md`
(root cause + verified-sound chain); Bubble Tea v2.0.8 `cursed_renderer.go:350` (mouse
sequence emitted only on `MouseMode` diff), `raw.go` (`tea.Raw` → `RawMsg`, written verbatim
at `tea.go:858`); `internal/tui/model.go:2431-2432` (MouseMode per frame), `:524-525`
(`SoftWrap = true`), `:822-828` (`WindowSizeMsg`), `:854-873` (`eventMsg` → `foldEvent`);
`internal/tui/mouse.go:456-459` (motion drop), `:492-533` (release); ADR 0030 §6–§7;
pinned tree at commit `a634a083`.

**Ratified design calls** (owner, 2026-08-29):
- **Fix shape:** re-assert via `tea.Raw` after every folded `domain.ToolResultEvent` and on
  `tea.WindowSizeMsg`; no timer, no `Setsid` for tool children (confinement contract untouched).
- **Soft-wrap seam:** `viewport.SoftWrap = false` — rows 1:1 with stored lines; the painter's
  cap (ADR 0030 §7) is the only wrapper.
- **Decoder quirk:** a `MouseNone` motion while a press is armed is treated as the release.
- **Diagnostics:** `--tui-diag` records `mouse-kind` (change-suppressed) and `mouse-reassert`.
- **Presented path/URL lines (owner, 2026-08-29):** the startup card never overflows (`drawBox`
  squares every row, startupbox.go:76-113); the only width-ignored lines are the presented block's
  Path/Location lines (startupbox.go:39-44, doc :17-21). `renderPresentedBlock` hard-wraps those
  lines with the painter's `wrapText` at the transcript width (no indent, no style) so nothing is
  lost on a narrow window; this supersedes the "each ONE physical line, unwrapped and unclipped"
  invariant pinned by `TestPresentedEntryKeepsPathAndURLWhole` (presenter_test.go:564) and the
  startupbox.go:17-21 comment (ADR 0019 rung 0) — re-pin that test to "the rows join to the whole
  token" and amend the comment; the archived responsive-startup-box plan line is NOT superseded
  (its card still fits).

**Regression check (2026-08-29, a634a083):**
- 1: guard folded — `runWrites`/`drainToQuit` expand a `tea.BatchMsg`; sessionsave_test.go:332
  asserts `issued == nil` and a `tea.RawMsg` from `progressed` instead of `progressed == nil`.
- 2: recast — the `MouseNone`→release conversion is gated on a new plain-bool press latch on
  `Model`, never on the selections' `.active` (that means "highlight shown"); tests (3)/(4) reshaped.
- 3: recast — horizontal scroll dropped/disabled (`XOffset() == 0` asserted), test (1) observes
  `TotalLineCount()`, `mouse.go:29`/`model.go:2012` rewritten, and the startup card hard-wraps its
  own overlong lines (supersedes the soft-wrap floor in
  `docs/plans/archived/2026-07-23 - 03 - responsive-startup-box-plan.md:195`).
- 4: SAFE.
- 3 (round 2, 2026-08-29, a634a083): recast — corrects the round-1 line above: the startup card
  never overflows (`drawBox` squares every row, startupbox.go:76-113), so it does not hard-wrap
  anything and the archived responsive-startup-box plan line is NOT superseded; the hard-wrap
  moves to `renderPresentedBlock`'s Path/Location lines (startupbox.go:39-44) via `wrapText`,
  superseding the one-physical-line invariant pinned by `TestPresentedEntryKeepsPathAndURLWhole`
  (presenter_test.go:564) and the startupbox.go:17-21 comment (ADR 0019 rung 0); test (4)
  retargeted to a presented entry; guard (a) becomes `vp.SetHorizontalStep(0)` beside
  model.go:525 (`scrollViewport` is model.go:1502, not mouse.go:1336); mouse.go:687 joins the
  comment rewrite with mouse.go:29 and model.go:2012.
- 2 (round 2): SAFE.

**Standing requirements:** `skills: coding-standards`. Model is value-copied every Update
(ADR 0011, `internal/tui/doc.go`): new state is plain fields (ints, bools), never a no-copy type.

**Out of scope:** the owner-run revival test (`/settings` → foreground editor → quit) on the
stuck session — manual, not a plan item; `Setsid`/tty detachment of tool children; a periodic
re-assert timer; archiving the handoff; VERSION/CHANGELOG (no item touches them).

## 1. Re-assert mouse tracking after tool results and resizes — ✅ DONE (2026-08-29)

NOTES (2026-08-29): `internal/tui/doc.go` is edited beyond the item's Files list — the package map
must name every file in the package or `TestDocMapNamesEveryFile` fails; the new file gets one
sentence at the end of the mouse cluster.

NOTES (2026-08-29): the diag key is a named constant `diagMouseReassert` declared beside
`mouseTrackingSeq` (the recorded key text is unchanged, `"mouse-reassert"`), matching how
`diagnostics.go` holds its other keys; it is not added to that file's const block because
`diagnostics.go` is not in this item's scope.

NOTES (2026-08-29): test (5) reads the batch's SHAPE (a `tea.BatchMsg` of exactly two Cmds) rather
than counting the re-assert inside it — identifying a member means running it, which would perform
the very record write the test exists to prove `runWrites` performs. The re-assert's identity at
depth 1 is pinned instead by the reshaped assertion in
`TestProgressSaveTriggersCoalesceBehindAnInFlightSave`.

NOTES (2026-08-29): one test beyond the item's list — `TestPreReadyToolResultDoesNotReassertMouseTracking`
pins the `m.ready` binding the item states.

**What:** new file `internal/tui/mousereassert.go`: const `mouseTrackingSeq =
"\x1b[?1002h\x1b[?1006h"` (exactly the bytes Bubble Tea's renderer emits for
`MouseModeCellMotion` — `ansi.SetModeMouseButtonEvent + ansi.SetModeMouseExtSgr`,
`cursed_renderer.go:362`) and method `func (m Model) reassertMouse() (Model, tea.Cmd)` that
increments a new plain field `mouseReasserts int`, records `m.diag.record("mouse-reassert",
strconv.Itoa(m.mouseReasserts))` (nil-safe already), and returns `tea.Raw(mouseTrackingSeq)`.
Wire two call sites in `internal/tui/model.go`: (a) the `case tea.WindowSizeMsg:` (:822-828)
returns the cmd instead of `nil`; (b) the `case eventMsg:` (:854-873) — when
`msg.Event` is a `domain.ToolResultEvent`, batch the cmd into whatever that case already
returns (`tea.Batch`). Binding: the cmd is emitted only when `m.ready` is true (the pre-ready
frame keeps `MouseModeNone`, `model.go:2365-2375`); sub-agent tool results (delegation) count
too — the sequence is idempotent. Nothing else changes MouseMode; the renderer's `lastView`
bookkeeping stays consistent because `View()` still sets `MouseModeCellMotion` every frame.

**Regression guard.** Every `ToolResultEvent` now returns at least `tea.Raw(mouseTrackingSeq)`, and a
depth-1 result's progress save becomes `tea.Batch(save, raw)`; `Update` has no `tea.BatchMsg` case
(model.go:1089 → `foldWidgetMsg`), so `runWrites`/`drainToQuit` (model_test.go:2847-2858) must expand a
`tea.BatchMsg` — run each member, step its msg — or `TestDelegationBoundariesFireTheProgressSave`
(sessionsave_test.go:279-280, :298 "Save calls = 2, want 3") goes red. In
`TestProgressSaveTriggersCoalesceBehindAnInFlightSave` (sessionsave_test.go:330-334) assert only
`issued == nil` and that `cmdMsg(progressed)` is a `tea.RawMsg` (no save dispatched); the
`len(m.pendingWrites) == 1` check at :335 already pins the coalescing.

**Files:** `internal/tui/mousereassert.go`, `internal/tui/model.go`,
`internal/tui/mousereassert_test.go`, `internal/tui/model_test.go`,
`internal/tui/sessionsave_test.go`

**Tests** (`mousereassert_test.go`): (1) `Update(tea.WindowSizeMsg{…})` on a model → the
returned cmd, executed, yields a `tea.RawMsg` whose `Msg` stringifies to exactly
`"\x1b[?1002h\x1b[?1006h"`; (2) `Update(eventMsg{Event: domain.ToolResultEvent{…}})` on a
ready model → the returned cmd (unwrap a `tea.BatchMsg` if returned, cf. `fireBatch`
`mouse_test.go:292`) contains that same `RawMsg`; (3) a non-tool event (e.g. a token event)
produces no `RawMsg`; (4) with `diag` open on a temp file, two re-asserts write
`mouse-reassert: 1` then `mouse-reassert: 2`; (5) `runWrites` given a cmd that resolves to a
`tea.BatchMsg` of a record write plus `tea.Raw(…)` still lands the write (the existing
`TestDelegationBoundariesFireTheProgressSave` stays green: 3 saves), and
`TestProgressSaveTriggersCoalesceBehindAnInFlightSave` passes with its :332 assertion reshaped
as the guard says.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'Reassert|Mouse|ProgressSave|DelegationBoundaries' -count=1`

**Commit:** `fix(tui): re-assert mouse tracking after tool results and resizes`

---

## 2. Treat a button-less motion as the release while a press is armed — ✅ DONE (2026-08-29)

NOTES (2026-08-29): test (1) builds its collapsed tool block with `modelWithToolBlock`, not the
`modelWithTranscript` the item's Tests line names — `modelWithTranscript` seeds only a user prompt
and has no block to toggle, so it cannot express the assertion the item asks for.

NOTES (2026-08-29): test (2) asserts "model unchanged" as named state (block state, all three
selections, flash, the latch) rather than a whole-Model comparison — `Model` holds slices and maps
and is not comparable.

NOTES (2026-08-29): the doc comment at `mouse.go:450-455` gained a short paragraph rather than the
single sentence the item names, and the `tea.MouseMotionMsg` arm of the Update dispatch
(`model.go:1098`) gained a clause — it is the only dispatch site for the converted event and its
map line said motion only ever extends a live selection.

**What:** Recast at the regression check (2026-08-29). In `handleMouseMotion`
(`internal/tui/mouse.go:456`), before the `msg.Button != tea.MouseLeft` drop: when
`msg.Button == tea.MouseNone` and a press is in flight (the new press latch below), return
`m.handleMouseRelease(tea.MouseReleaseMsg{X: msg.X, Y: msg.Y, Button: tea.MouseLeft,
Mod: msg.Mod})` — the SGR `ESC[<35;x;ym` release-with-motion-bit decodes as a `MouseNone`
motion (ultraviolet `decoder.go:1600-1616`) and would otherwise leave the press armed
forever. A `MouseNone` motion with nothing armed stays a no-op. Extend the doc comment at
`mouse.go:450-455` with one sentence naming the quirk. Fix item: closes the latent bug 2 of
the handoff (lost release on Windows-Terminal-style encodings).

**Regression guard.** Gate the MouseNone→release conversion on a NEW dedicated plain-bool press latch field on Model (set in handleMouseClick when a left press lands, cleared on every handleMouseRelease exit), never on `sel.active || transcriptSel.active || settings.sel.active` — `.active` means "highlight shown" after a copy (mouse.go:479-483, :615-628), not "press in flight". Test (3) becomes: press, then `leftDrag` to another cell (anchor != head), then a `MouseNone` motion at that cell → routed as release into the copy path, no toggle; add test (4): after a drag-copy completed, a later `MouseNone` motion is a no-op (no copyFlash, no clipboard cmd).

**Files:** `internal/tui/mouse.go`, `internal/tui/model.go` (the latch field on `Model`),
`internal/tui/mouse_test.go`

**Tests** (`mouse_test.go`, beside `leftClick`/`leftRelease` :35-41): (1) press on a
collapsed tool block via `modelWithTranscript`, then `tea.MouseMotionMsg{X, Y, Button:
tea.MouseNone}` at the same cell → the block toggles (same assertion as the existing
click-release toggle test) and `transcriptSel.active` is false; (2) the same `MouseNone`
motion with nothing armed → model unchanged, nil cmd; (3) press, then `leftDrag` to another
cell (anchor != head), then a `MouseNone` motion at that cell → routed as release into the copy
path, no toggle, and the latch is cleared; (4) after a drag-copy completed (highlight still
`.active`), a later `MouseNone` motion is a no-op — no copyFlash, no clipboard cmd.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'Mouse|Toggle|Drag' -count=1`

**Commit:** `fix(tui): treat a button-less motion as the release while a press is armed`

---

## 3. Transcript viewport stops soft-wrapping: rows 1:1 with stored lines — ✅ DONE (2026-08-29)

NOTES (2026-08-29): the presented block's hard wrap is a new `rawLink` helper (`startupbox.go`) that
wraps at the width the line's lead-in leaves — the two-space body indent, or the styled `▤` marker
when there is no title — rather than at the full transcript width the item's text names. Wrapping
`lead+token` at the full width puts a blank row ahead of the token (the wrap breaks after the
indent), and wrapping the bare token at the full width overruns layout.md's absolute cap by the
lead's own cells and drops the `▤` marker from an untitled block. As written, a line that fits is
byte-identical to before (`TestPresentedEntryRendering` unchanged) and every row stays inside the
width.

NOTES (2026-08-29): test (1)'s click is aimed at the block's DRAWN row via a new `drawnRow` helper
(the row the human sees, read off `m.viewport.View()`) rather than at `screenRow(header)`, which
converts a stored-line index and so assumes the very 1:1 mapping the test exists to prove — aimed
that way the assertion passes on the pre-item tree. It clicks the block's LAST row (the leader),
which is the discriminating one: a row of drift there falls past the block entirely.

NOTES (2026-08-29): the plan's line numbers had shifted on the pinned tree — `SoftWrap` was
model.go:537 (not :525), the refreshViewport comment :2037 (not :2012) and the selection-text
comment mouse.go:698 (not :687). All three sites were rewritten as the item's guard (c) asks.

NOTES (2026-08-29): all four tests were confirmed to bite — (1) and (2) fail with `SoftWrap = true`
restored, (3) fails with `SetHorizontalStep(0)` removed, and (4) plus the re-pinned
`TestPresentedEntryKeepsPathAndURLWhole` fail with `rawLink` reverted to the raw appends.

**What:** Recast at the regression check (2026-08-29). `internal/tui/model.go:525` —
`vp.SoftWrap = false`. The painter already hard-wraps
every line at the width authority (ADR 0030 §7, `wrap.go:236`); with `SoftWrap = true` the
widget re-wraps in `ansi.StringWidth` (GraphemeWidth), so a full line carrying two glyphs
that measure 1 cell WcWidth / 2 cells GraphemeWidth (`⚠️ ✔️ ℹ️`) splits into two screen rows
and every `contentLineAt(row) = YOffset() + row` reader below it (`model.go:2512-2523`,
`stickyHeaderSpan`, `refreshViewportAnchored`, `followBlockCursor`, `highlightTranscript`)
lands one row off. Binding: after this item a line the widget measures wider than its width
is clipped at the right edge, never wrapped, and the transcript's horizontal offset stays 0
(no key is forwarded to the viewport's horizontal-scroll bindings — verify and assert).
Amend ADR 0030 §6 (`docs/adr/0030-…md:106`): replace "the viewport soft-wraps with
`ansi.StringWidth`" with the new fact (the viewport no longer wraps; its rows are the
painter's lines) — the `cellToRuneOffset` column rule of §6 is unchanged. Fix item: closes
the handoff's latent bug 1 (row map drift on VS16 emoji).

**Regression guard.** (a) `vp.SetHorizontalStep(0)` beside model.go:525 (viewport.go:540-545 disables Left/Right keys, wheel-left/right and shift-wheel together); assert `m.viewport.XOffset() == 0` after a wheel-left notch and after an inert-state left/right key on an overlong line (`scrollViewport`, if it needs citing, is model.go:1502 — not mouse.go:1336). (b) Test (1) observes rows via `m.viewport.TotalLineCount()` (== stored line count when !SoftWrap) against `len(m.lines)`, or the row count of `m.viewport.View()` — there is no `visibleLines()` helper. (c) Add `internal/tui/mouse.go` and `internal/tui/startupbox.go` to Files; rewrite model.go:2012 ("the viewport soft-wraps the stored lines against its own full width"), mouse.go:29 ("soft-wraps are copied verbatim") and mouse.go:687 ("soft-wrap breaks are copied verbatim") to the new fact together with the ADR 0030 §6 amendment. (d) Ratified design call (owner, 2026-08-29) — **Presented path/URL lines:** the startup card never overflows (`drawBox` squares every row, startupbox.go:76-113); the only width-ignored lines are the presented block's Path/Location lines (startupbox.go:39-44, doc :17-21). `renderPresentedBlock` hard-wraps those lines with the painter's `wrapText` at the transcript width (no indent, no style) so nothing is lost on a narrow window; this supersedes the "each ONE physical line, unwrapped and unclipped" invariant pinned by `TestPresentedEntryKeepsPathAndURLWhole` (presenter_test.go:564) and the startupbox.go:17-21 comment (ADR 0019 rung 0) — re-pin that test to "the rows join to the whole token" and amend the comment; the archived responsive-startup-box plan line (`docs/plans/archived/2026-07-23 - 03 - responsive-startup-box-plan.md:195`) is NOT superseded (its card still fits). No startup-card wrapping is done anywhere. Test (4): a presented entry with a long URL at width 24 — the rendered rows join to the whole URL, `m.viewport.XOffset() == 0`, and the row count equals the stored line count.

**Files:** `internal/tui/model.go`, `internal/tui/mouse.go`, `internal/tui/startupbox.go`,
`docs/adr/0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md`,
`internal/tui/model_test.go`, `internal/tui/presenter_test.go`

**Tests** (`model_test.go`): (1) a transcript whose first assistant line is exactly the
transcript width and contains two `⚠️` (U+26A0 U+FE0F) glyphs under the WcWidth method,
followed by a second block: `m.viewport.TotalLineCount()` equals `len(m.lines)` (or the row
count of `m.viewport.View()` equals the stored line count), and a `leftClick`+`leftRelease` on
the second block's header row toggles THAT block (fails on the pre-item tree: the click lands
one row off); (2) an overlong line (wider than the width in GraphemeWidth only) renders on one
row, clipped; (3) on that overlong line a `MouseWheelLeft` notch, a shift-modified wheel, and
an inert-state left/right key each leave `m.viewport.XOffset() == 0`; (4) (`presenter_test.go`)
a presented entry with a long URL at width 24: the rendered rows join to the whole URL,
`m.viewport.XOffset() == 0`, and the row count equals the stored line count — nothing clipped;
`TestPresentedEntryKeepsPathAndURLWhole` (presenter_test.go:564) is re-pinned to "the rows join
to the whole token".

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'Viewport|RowMap|Toggle|Sticky|Follow|Highlight|Startup|Presented|Wheel' -count=1`

**Commit:** `fix(tui): transcript viewport no longer soft-wraps, rows stay 1:1 with lines`

---

## 4. `--tui-diag` records mouse event kinds

**What:** `internal/tui/diagnostics.go` `observe` (:234-250) gains cases for
`tea.MouseClickMsg` → `record("mouse-kind", "press")`, `tea.MouseMotionMsg` → `"motion"`,
`tea.MouseReleaseMsg` → `"release"`, `tea.MouseWheelMsg` → `"wheel"`; add the key to the key
consts (:112-114). `record` is change-suppressed (:154-164), so a drag logs
`press`/`motion`/`release` once each and consecutive motions collapse — a stuck session shows
`press` lines with no `release`/`motion` ever following. Document the two new keys
(`mouse-kind`, `mouse-reassert` from item 1) wherever the existing keys are listed
(`diagnostics.go` doc comment; `docs/manual/` if the diag file's keys are documented there —
grep `color-profile`).

**Files:** `internal/tui/diagnostics.go`, `internal/tui/diagnostics_test.go`, `docs/manual/`
(only if it lists the diag keys)

**Tests** (`diagnostics_test.go`, using the existing temp-file pattern): feed
click, motion, motion, release, wheel → file lines are exactly `mouse-kind: press`,
`mouse-kind: motion`, `mouse-kind: release`, `mouse-kind: wheel`; nil `diagLog` → no panic.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'Diag' -count=1`

**Commit:** `feat(tui): --tui-diag records mouse event kinds`

---

**Suggested version bump:** patch (`v0.18.8`) — three user-visible fixes and one
diagnostic addition; the owner decides.

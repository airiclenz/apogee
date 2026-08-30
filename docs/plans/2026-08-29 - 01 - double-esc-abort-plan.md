# Double-ESC abort — implementation plan

**Goal:** a stray ESC must no longer kill a running turn. Mirroring the existing
double-Ctrl+C quit gesture, the first ESC arms a one-second window (status hint shows
the armed state); a second ESC inside the window stops the in-flight worker — anywhere
the worker is running, including while an approval prompt is up. The gesture disarms
on the window's expiry.

**Date:** 2026-08-29 · **Status:** unexecuted · **sized for:** ~200k-context host

**Authoritative sources:** `internal/tui/model.go` (the `ctrl+c` double-tap at
model.go:1355-1362, `lastCtrlC` model.go:280, `ctrlCQuitWindow` model.go:1141,
`ctrlCResetMsg` model.go:1145 (folded at :881), `busy()` model.go:1837, `statusRight`
model.go:3182, the frame `case "esc"` model.go:1382, the approval routing model.go:1466);
`internal/tui/approval.go:204` (resolveApproval's ⏎-Cancel row `stopWorker` — `handleApprovalKey`
has no esc case); pinned tree at commit `4e184a16`.

**Ratified design calls** (owner, 2026-08-29):
- **Approval prompt:** the double-tap applies everywhere the worker is running,
  including at the approval prompt — the pane's Cancel row stays labelled `[esc]`; one
  consistent rule.
- **Running-state hints:** both announced hints reword to the compact glyph form:
  status line `"esc×2 stop"`, running input-box placeholder
  `"queue a message…  ⏎ queue · ↑ recall · esc×2 stop"`.
- **Timing window:** `time.Second`, same as `ctrlCQuitWindow` (ratified implicitly with
  the "like double Ctrl+C" request).

**Regression check (2026-08-30, 4e184a16):**
- 1: recast — ask-prompt carve-out (one-press cancel kept), `handleApprovalKey` change dropped
  (frame `case "esc"` covers the pane), cmd/apogee e2e drivers press Esc twice, four more
  internal/tui tests named; approval.go:16-17/:40-41 "one path" comments superseded by the
  ratified call (reworded in item 2). HEAD line numbers throughout.
- 2: guard folded — golden frames `t10-forced-pane.txt:27` / `t12-pane-60.txt:21`, `README.md:133`,
  `layout.md:1872`, `doc.go:307`; rewording extended per the writer's decision (approval.go
  comments, model_test.go:2722,2736, layout.md:487,506,1833, user-questions-layout.md:47 only
  where they describe the running-state esc as one press).
- Round 2 (2026-08-30, 4e184a16) — 1: guard folded — confirming press zeroes `lastEsc` before
  `stopWorker` (TestModelStatusLineActivity's idle-empty closing assertion stays green); the
  two-press e2e edit is kept to the run-stopping set (hostile:221, smoke:120, support:555 press
  esc at idle and stay one press); model.go:1409 comment superseded by the ratified approval
  call. 2: SAFE. Header "Out of scope" reworded per the writer's decision.

**Standing requirements:** `skills: coding-standards`. Model is value-copied every
Update (ADR 0011, `internal/tui/doc.go`): the new arm timestamp is a plain
`time.Time` field like `lastCtrlC`, never a no-copy type.

**Out of scope:** single-ESC behaviour outside the running-state gesture. The ask prompt's
esc reaches the frame `case "esc"` and keeps one-press cancel through item 1's
`stateAwaitingAsk` carve-out; the overlays (settings, session browser, pickers, report
pane, block cursor, key migration) claim the key via `keyClaimOrder` before `handleKey`
and keep one-press dismissal. Also out: the ask prompt's `esc cancel` hint; the approval
Cancel row's label; quitting (Ctrl+C); VERSION/CHANGELOG (no item touches them).

## 1. Double-ESC abort gesture in the frame key switch — ✅ DONE (2026-08-30)

NOTES (2026-08-30): the plan names `TestStatusLineQuietSuffix` (model_test.go:3943/:4006) as needing the two-press update, but that test presses no esc key and is green untouched. The esc-driven stop assertion actually lives in `TestStatusLineQuietSuffixGivesWayFirst`'s "a stopping worker never shows it" subtest (model_test.go:4131) — that is the one updated to two presses.

NOTES (2026-08-30): two sites beyond the plan's enumeration needed the second press to keep their premise honest, both in files the plan already lists: `TestModelApprovalStaleArmDoesNotArmTheNextPane` (model_test.go:858, the first pane must really be cancelled before the next one opens) and the `TestStatusLineQuietSuffixGivesWayFirst` subtest above. The plan's enumeration is a floor, so they are folded in.

NOTES (2026-08-30): prose kept truthful alongside the key change — the approval `⏎` case comment (model.go, the "Esc … stays live throughout" line the plan calls out), the two `cmd/apogee/e2e_approval_test.go` cancel comments and `e2e_outcome_test.go`'s "Step 3 — Esc stops it" now say esc×2.

NOTES (2026-08-30): the idle/errored esc subtests gained a `lastEsc.IsZero()` assertion so the item's binding — the gesture never arms when not busy — is pinned, not just implied.

NOTES (2026-08-30): pre-existing debt, NOT from this item — `go test ./cmd/apogee/` already fails at HEAD (cbc7f8ef) with this item's changes stashed, on the same three tests and only those: `TestE2EDelegationStepCap` (golden `t04-step-cap-block`), `TestE2EHostileSurfacesKeepTheirOwnRows` (golden `t12-skills`) and `TestE2EOutcomeCancelledDelegationCarriesTheFailureTone` (golden `t15-cancelled-delegation`). All three are one-column version-string width drifts in the golden frames, left stale by the v0.18.10 VERSION bump; `go test ./cmd/apogee -update` in a commit of its own would close them. `./internal/tui/` is fully green.

**What:** Recast at the regression check (2026-08-30). Change what one ESC does while a
worker is in flight. In `handleKey`
(`internal/tui/model.go`), the `case "esc"` (model.go:1382) becomes: when `m.busy()`
— second press inside `escStopWindow` (new const, `time.Second`, beside
`ctrlCQuitWindow` model.go:1141) → zero `lastEsc`, then `m.stopWorker()`; first
press → set new field `lastEsc time.Time` beside `lastCtrlC` (model.go:280), return
`tea.Tick` of a new `escStopResetMsg` (newtype beside `ctrlCResetMsg`, model.go:1145)
that zeroes `lastEsc` when folded (case beside `ctrlCResetMsg` in `Update`, model.go:881).
Not busy → no-op, as today. Before the arm/abort logic, a `m.state == stateAwaitingAsk`
carve-out calls `m.stopWorker()` directly (the ask prompt keeps its one-press cancel).
No change in `handleApprovalKey`: the frame `case "esc"` is reached before the approval
routing (model.go:1466) and covers the pane on its own (`busy()` includes
`stateAwaitingApproval`) — first press arms + hint, second inside the window stops; the
`[esc]` Cancel row label is untouched. `statusRight`
(model.go:3182) gains an armed branch ABOVE the gauge (exactly like `lastCtrlC`):
`if !m.lastEsc.IsZero() { … "press esc again to stop" }`; the `stateRunning` hint
(model.go:3196) changes `"esc stop"` → `"esc×2 stop"`. `prompteditor.go` is NOT touched
in this item (item 2 owns the placeholder string). Binding: the gesture never arms
when not busy (a stray ESC at idle/errored stays a no-op), and a second ESC after the
window lapsed only re-arms (assert on the stamp, as the Ctrl+C tests do).

**Regression guard.** The ask prompt keeps one-press cancel, as the header's out-of-scope list already ratifies: in the frame `case "esc"` (model.go, HEAD ~1382) a `m.state == stateAwaitingAsk` carve-out calls stopWorker directly before the arm/abort logic; the pane's `esc cancel` hint (ask.go) and TestModelAskCancelClearsPrompt stay green and unchanged. Drop the handleApprovalKey change entirely — the frame case alone covers the approval pane (busy() includes stateAwaitingApproval); approval.go stays in Files only for its :16-17 / :40-41 comment rewording, which item 2 owns: those comments' "one path" claim is superseded by the ratified call (⏎-Cancel row one press, Esc two presses at the pane). Tests: name TestModelApprovalCancelClearsPrompt (replacing TestModelApprovalEscapeStrips — an ESC-byte stripping test that presses no esc key), TestModelStatusLineActivity, TestStatusLineQuietSuffix and TestStatusLineRightSlotOccupantsShareTheMargin as tests to update (two presses / the "esc×2 stop" literal). Files gains the cmd/apogee e2e tests that stop a run with one `drv.Press(tuitest.Esc)` — e2e_outcome_test.go, e2e_approval_test.go, e2e_hostile_test.go, e2e_stream_test.go, e2e_console_test.go — each run-stopping press becomes two (the driver's escapeGap of 70 ms is inside the 1 s window); Acceptance widens to `go test ./internal/tui/ ./cmd/apogee/`.
Round 2 (2026-08-30): the confirming press zeroes `m.lastEsc` BEFORE `stopWorker` — otherwise the armed `statusRight` branch, sitting above every occupant, shows "press esc again to stop" at idle for up to 1 s after the worker unwound and TestModelStatusLineActivity's closing `statusText()==""` assertion (model_test.go:3905-3908) goes red; the lapsed-window test tells re-arm (stamp refreshed) from stop (stamp zeroed). Keep the two-press e2e edit to the run-stopping set only: outcome:148, approval:141,192, hostile:234,307, stream:142,319, console:149 — hostile:221 (backs out of the /settings mode sub-list, idle; a second press would close the pane under closePane's feet), smoke:120 (follows the synchronous /skills note, idle) and support:555 (leaves the block cursor, idle) stay ONE press; e2e_support_test.go and e2e_smoke_test.go are not touched. The model.go:1409 comment (approval ⏎ case: "Esc, the safe direction, is claimed above and stays live throughout") records the one-press pane cancel and is superseded by the ratified approval call — reword it in this item alongside the esc case. Use HEAD line numbers (model.go esc case 1382, ctrlCResetMsg fold 881, statusRight 3182, "esc stop" 3196, approval routing 1466) and pin the tree at 4e184a16.
The idle/errored "esc never quits" and the claim-order overlay
tests (e.g. `TestModelPickerEscCloses`, `TestSessionBrowserEscLayers`) must pass
untouched: only the `case "esc"` fall-through in `handleKey` (with its ask carve-out)
changes — no claim-order entry, hint legend or pane is reworded in this item.

**Files:** `internal/tui/model.go`, `internal/tui/model_test.go`,
`cmd/apogee/e2e_outcome_test.go`, `cmd/apogee/e2e_approval_test.go`,
`cmd/apogee/e2e_hostile_test.go`, `cmd/apogee/e2e_stream_test.go`,
`cmd/apogee/e2e_console_test.go`

**Tests:** extend `TestModelStopKeys` (model_test.go:593): rework
"esc while running cancels but does not quit" into the two-press journey (first esc
arms, `m.cancel` not fired, hint "press esc again to stop" in `View()`, no Quit; second
esc inside the window fires `m.cancel`, state still `stateRunning`, no Quit); keep the
idle/errored no-op subtests green. Add: second esc after a lapsed window (stamp backdated
`2*escStopWindow`, as model_test.go:676) re-arms instead of cancelling — assert the stamp is
refreshed (not zero) there, and zero after the confirming press. Extend the
approval esc tests (model_test.go:845 `TestModelApprovalEscapeIsLiveBeforeArming`,
:921 `TestModelApprovalCancelClearsPrompt`) for the two-press journey at the pane. Update
`TestModelStatusLineActivity` (:3867, "stopping" after two presses; its closing idle-empty
assertion at :3905-3908 stays as is and must pass), `TestStatusLineQuietSuffix`
(:3943) and `TestStatusLineRightSlotOccupantsShareTheMargin` (:4252, the `"esc×2 stop"`
literal + bodyIndent). `TestModelAskCancelClearsPrompt` (:1855) stays green unchanged. In
the cmd/apogee e2e tests listed in Files, every run-stopping `drv.Press(tuitest.Esc)`
(e2e_outcome_test.go:148, e2e_approval_test.go:141,192, e2e_hostile_test.go:234,307,
e2e_stream_test.go:142,319, e2e_console_test.go:149) becomes two presses; the idle presses at
e2e_hostile_test.go:221, e2e_smoke_test.go:120 and e2e_support_test.go:555 stay one. Assert
the exact armed-hint string and the exact `"esc×2 stop"` running hint via the status
line as rendered.

**Acceptance:** `go build ./... && go test ./internal/tui/ ./cmd/apogee/`

**Commit:** `feat(tui): require a second esc within the window to stop a run`

## 2. Announced-surface and doc wording for double-ESC

**What:** reword every remaining announcement of the one-press stop. `runningPlaceholder`
(`internal/tui/prompteditor.go:113`) → `"queue a message…  ⏎ queue · ↑ recall · esc×2
stop"` — the constant's consumers are the `setPlaceholder(runningPlaceholder)` call
sites (ask.go:126, commandrun.go:90,292,310,424), all unaffected. The doc comment at
`internal/tui/doc.go:307` quotes the old string in-line — update the quotation. Docs:
`docs/manual/commands.md:68` (`esc` stops a run) and :76 (esc cancels from the instant
it is up) — describe the two-press gesture and the one-second window; `layout.md:1173`
(names the `esc stop` hint) — new wording. The ask prompt's `esc cancel` hints
(ask.go:317-325) and the approval pane's `[esc]` row (`user-questions-layout.md`) stay
verbatim.

**Regression guard.** The golden frames `cmd/apogee/testdata/frames/t10-forced-pane.txt:27`
and `t12-pane-60.txt:21` pin the old placeholder byte-for-byte (`tuitest.Golden` at
e2e_approval_test.go:87, e2e_hostile_test.go:232): refresh them with `go test ./cmd/apogee -update`
in this item's commit. `README.md:133` announces "`esc` stops a run" — reword it; `layout.md:1872`
quotes the old placeholder beside the :1173 hint — reword both. The announced-surface rewording additionally covers approval.go:16-17 and :40-41 comments, model_test.go:2722,2736, layout.md:487,506,1833 and docs/layout/user-questions-layout.md:47 ONLY where they describe the running-state esc as one press — the ask prompt's `esc cancel` wording stays (one press there is ratified).
`prompteditor_test.go` and `interject_test.go` pin the old
placeholder byte-for-byte — the item must land the constant change and the test updates
together in one commit; `doc.go` is a comment-only change.

**Files:** `internal/tui/prompteditor.go`, `internal/tui/doc.go`,
`internal/tui/prompteditor_test.go`, `internal/tui/approval.go`, `docs/manual/commands.md`,
`layout.md`, `README.md`, `cmd/apogee/testdata/frames/t10-forced-pane.txt`,
`cmd/apogee/testdata/frames/t12-pane-60.txt`; only where the guard's condition holds:
`internal/tui/model_test.go`, `docs/layout/user-questions-layout.md`

**Tests:** `TestPlaceholderFollowsTheExchange` (interject_test.go:1167) already pins
the constant — keep it green. Add/adjust a `prompteditor_test.go` assertion on the
EXACT new placeholder string. The item's journey test is the placeholder string itself:
assert the literal `"queue a message…  ⏎ queue · ↑ recall · esc×2 stop"`, not the
constant, in at least one test that renders the empty box while running. The refreshed
golden frames `t10-forced-pane` and `t12-pane-60` must carry the new placeholder and
nothing else changed.

**Acceptance:** `go test ./internal/tui/ ./cmd/apogee/`

**Commit:** `docs(tui): announce the double-esc stop gesture in hints and manual`

---

**Suggested version bump:** `VERSION` minor (0.x feature bump) at closeout, per the
house convention — owner's call; no item performs it.

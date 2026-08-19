# Inspector note pairing + pre-click frame plan

**Goal:** close the two open defects in `ISSUES.md` (Run residuals, 2026-08-18): the Inspector's
unrecorded-reply note mispairs in a parallel fan-out, and a click in the band the inspector box
grows into is swallowed instead of falling through.

**Date:** 2026-08-19
**Status:** ready to execute
**Sized for:** ~200k-context host

**Authoritative sources:**

- `ISSUES.md:30-42` — the two defect entries (marked `[P]` when this plan was written).
- `internal/domain/events.go:37-58` — the `EventBase.CallID` contract: CallID is the field an
  observer demultiplexes concurrent streams by; depth alone cannot separate siblings.
- ADR 0011 (the Model is a value-copied immutable), ADR 0039 (delegations fan out concurrently),
  ADR 0045 (a routed spawn builds its own client and wire tap).
- `docs/plans/archived/2026-08-18 - 00 - open-defects-plan.md` item 4 — the prior ratification of
  the successor-only rule this plan supersedes.

**Precedence:** where an item's line reference has drifted, the current code and the named sources
above win; all references were verified against the tree on 2026-08-19.

**Ratified design calls:**

1. **Whole-chain pre-click frame** (owner, 2026-08-19, write-time question): the click fix covers
   the entire `handleMouseClick` dispatch chain, not only the usage/inspector rect pair — all
   geometry resolves against one pre-click Model value; mutations apply to the live model.
2. **The wire stream key is `(depth, callID)`, not `(turn, depth)`** (settled at write time from
   `internal/domain/events.go:46-50` and the tap binding in `internal/agent/construct.go:452-483`):
   two concurrent routed children share `(turn, depth)` and only CallID separates them. Turn
   orders records *within* a stream; it is not identity.
3. **The unrouted-sibling braid is an accepted residual** (write time): unrouted concurrent
   sub-agents share the parent's client and tap, so their records carry identical
   `(depth, callID)` (`internal/agent/construct.go:452-455`, `internal/agent/subagent.go:230-232`)
   and no field can separate them. The fix documents this limit at the pairing function; it does
   not try to solve it.

**Standing requirements:**

- skills: coding-standards
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**

- The wheel dispatch chain (`settingsWheel`/`usageWheel`/`inspectorWheel`,
  `internal/tui/model.go:1007-1018`) — a wheel miss does not dismiss a pane, so the
  mutate-then-resolve hazard does not arise there. If an implementer finds a wheel-side dismissal
  after all, record it as a DEFER finding; do not fix it in this plan.
- Showing CallID in the inspector record header to disambiguate siblings — unrouted siblings share
  CallID, so the header could not separate them either; not asked for.
- Recording non-streaming success bodies on the wire (`internal/provider/wire.go:152-154`) — the
  legitimate source of reply gaps stays as designed.
- Any version identifier change (see the closing note).

---

## 1. Pair the inspector's unrecorded-reply note by wire stream — ✅ DONE (2026-08-19)

NOTES (2026-08-19): the package doc (`internal/tui/doc.go`) and the `Model.wire` field comment
(`internal/tui/model.go`) did not state the successor-only rule at all, so there was no stale prose
to replace there; both gained one clause stating the `(depth, callID)` stream key instead.
NOTES (2026-08-19): `wireEvent` keeps its signature and delegates to a new `wireEventOfCall`
variant, so no existing call site had to be migrated.

**What:**

- Add a `callID string` field to `wireRecord` (`internal/tui/inspector.go:52-58`) and have
  `foldWire` (`internal/tui/inspector.go:138-158`) copy `we.CallID` into it — the event already
  carries it; today it is dropped.
- Rewrite `hasUnrecordedReply` (`internal/tui/inspector.go:329-337`) to apply the existing
  successor rule *within a stream* instead of across the whole ring. A record's stream key is
  `(depth, callID)` (design call 2). A request record has an unrecorded reply **iff** the next
  record in the same stream exists and is not a response. No in-stream successor → no note (the
  reply may still be in flight). Do **not** key on turn — it orders within a stream only.
- This preserves the note's main case: in non-streaming mode a success reply is never recorded, so
  a stream reading `[req turn 1, req turn 2]` still notes the first request.
- Update every prose surface that states the successor-only rule: the function's doc comment
  (`internal/tui/inspector.go:322-328` — it must now also state the accepted unrouted-sibling
  residual, design call 3), the `inspectorNoReplyRow` constant's comment
  (`internal/tui/inspector.go:102-108`), the package doc (`internal/tui/doc.go:667-670`), and the
  `Model.wire` field comment (`internal/tui/model.go:142-148`) — adjust drifted line numbers as
  found; skip a surface only if it does not in fact describe the pairing rule.
- Remove this defect's bullet (the unrecorded-reply one) from `ISSUES.md`'s "Run residuals — open
  (2026-08-18, open-defects plan)" section. Leave the section heading and the other bullet in
  place — item 2 owns those.

**Files:** internal/tui/inspector.go, internal/tui/inspector_test.go, internal/tui/doc.go,
internal/tui/model.go, ISSUES.md

**Tests** (in `internal/tui/inspector_test.go`; extend the `wireEvent` helper at `:16-22` with a
CallID-carrying variant and migrate call sites as needed):

- The ISSUES repro: fold `[req d0/cidA, req d1/cidB, resp d1/cidB, resp d0/cidA]` → no
  unrecorded-reply note under any record.
- The pending half: fold `[req d0/cidA, req d1/cidB, resp d1/cidB]` → no note under the d0
  request (its stream has no successor yet).
- The serial regression: fold two requests in one stream (same depth+CallID, turns 1 and 2) → the
  note still lands under the first request.
- `TestInspectorNamesAnUnrecordedReply` and `TestInspectorSaysNothingWhenTheReplyWasRecorded`
  stay green (helper-signature updates only).

**Acceptance:**

```
go vet ./internal/tui/ && go test ./internal/tui/
```

**Commit:** `fix(tui): pair the inspector's unrecorded-reply note by wire stream`

---

## 2. Resolve mouse-click geometry against the pre-click frame

Depends on item 1 (both items edit the same `ISSUES.md` section; this one removes its remainder).

**What:**

- In `handleMouseClick` (`internal/tui/mouse.go:383-425`), capture the pre-click frame as a Model
  value — `pre := m` — before the first mutation (the Model is a value, ADR 0011; the copy is the
  snapshot). The binding invariant, per design call 1: **every geometry resolution in the click
  chain reads the pre-click value; every state predicate and every mutation runs on the live
  model.** Geometry means: the settings-pane rects inside `handleSettingsClick`, `usagePaneRect`
  for the usage claim (`internal/tui/mouse.go:1337-1346`), `inspectorPaneRect` for the inspector
  claim (`:1449-1458`, `:1395-1414`), `pointInputRow` for the prompt claim, and
  `pointTranscriptRow`/`contentLineAt` for the transcript claim. The handler shape that threads
  the pre-click value through (extra parameter, split rect-vs-mutate helpers, or inlined checks)
  is the implementer's choice; the invariant is one pre-click Model value answering all geometry.
- Ratified behaviour: a click landing where a dismissed pane was drawn only dismisses — it is
  never claimed by the regrown inspector, never selects a transcript row the pre-click frame did
  not show at that Y, and never lands in the prompt on post-dismissal geometry.
- Replace the stale safety argument in the doc comment at `internal/tui/mouse.go:1381-1385` ("the
  box can only grow upward, so a point inside the box is still inside the box") with the pre-click
  frame rule; add one line to the `frameOverlays` purity comment
  (`internal/tui/model.go:2480-2483`) noting its "same answer" guarantee holds per Model value,
  which is why the click chain snapshots one.
- Remove this defect's bullet from `ISSUES.md` and — item 1 having removed the other bullet — the
  now-empty "Run residuals — open (2026-08-18, open-defects plan)" heading and its one-line intro.

**Files:** internal/tui/mouse.go, internal/tui/mouse_test.go, ISSUES.md

**Tests** (in `internal/tui/mouse_test.go`; no existing test opens the usage and inspector panes
together — build that model once as a helper):

- Band fall-through: usage + inspector both open, the usage report drawn shorter than its grant
  (so the inspector's regrowth exceeds the vacated rows and the band is non-empty); click at a Y
  inside the band — between the post-dismissal inspector top and the pre-click inspector top —
  asserts both panes dismiss, the inspector does not claim the click, and no transcript selection
  is made.
- Inside-inspector regression: both panes open, click inside the inspector's pre-click rect →
  the inspector claims it and is not dismissed.
- Dismissal-then-transcript guard: click at a Y that the pre-click frame drew as pane/gap rows
  but the post-dismissal model maps to a transcript row → no transcript selection.
- Existing single-pane tests (`TestUsageReportUnderTheClick`, `TestInspectorPaneUnderTheClick`,
  the settings-pane suite) stay green.

**Acceptance:**

```
go vet ./internal/tui/ && go test ./internal/tui/
```

**Commit:** `fix(tui): resolve mouse click geometry against the pre-click frame`

---

**Suggested version bump:** patch — two user-visible TUI defect fixes, no API or behaviour
additions. Whether and when to bump is the owner's call; no plan item changes a version
identifier.

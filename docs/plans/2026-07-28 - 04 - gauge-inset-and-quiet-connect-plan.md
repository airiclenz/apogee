# Plan — Status-line right-slot inset + quiet first-contact "connected:" note

**Date:** 2026-07-28
**Status:** READY (not grilled — both items are small, issue-driven behaviour changes; the
*Design decisions* below derive from the two ISSUES entries, a measured render probe of the
current status line, and the heartbeat/rebind orchestration as committed at `238fd2c`).
**Source:**
- `ISSUES.md:6` — "The context usage gauge should be move 2 spaced to the left (same as all
  other information printes there - eg: 'esc stop')."
- `ISSUES.md:8` — "I do not want the 'connected: ...' message printed when apogee is stared
  and a server was up and running and apogee connected immediatelly."

**Terminology:** the **gauge** is the live context-usage bar in the status line's right slot
(`contextGauge`/`contextUsage.view`, `internal/tui/model.go:2387-2422`). The **right slot** is
everything `statusRight` can put there: the gauge, the "esc stop"/"enter dismiss" hints, the
primed-Ctrl+C hint, and the mouse-copy flash (`model.go:2361-2381`). The **connected note** is
`rebindNote`'s late-seed wording — `"connected: <model>[, context <n>]"` (`model.go:1683`).
**First contact** (this plan's term) is a landed beat before which NO beat has ever landed and
NO beat has ever failed — the pre-fold state `!hb.everOnline && hb.failures == 0`.
**Track:** post-`v0.9.7` working tree. CHANGELOG entries under `## [Unreleased]`; `VERSION`
untouched (rides the next cut).
**Public API:** none. Everything lives in `internal/tui`; no exported name moves.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`,
no PRs (owner directive).

Per-item green gate:

```
gofmt -l .                # empty
make check                # vet + lint + go test -race -count=1 ./...
```

**Dependencies.** Items 1 and 2 are independent of each other (different regions of
`model.go`, different test files' regions); item 3 (docs close-out) depends on both.
`/implement-plan` may stop after any item and the tree is coherent.

**Deviations leave a trail.** Any authorized deviation from an item's text must land as a
dated `NOTES:` line under that item's heading in this file, per the sub-agent templates.

**Authoritative sources**, in precedence order, for every item:

1. This document.
2. `ISSUES.md:6` and `ISSUES.md:8` — the two asks, as quoted above.
3. `internal/tui/model.go:2307-2343` (`statusLine`) — the full-width black-band posture: the
   row is filled edge to edge with black-bg cells, and the pre-styled gauge is concatenated
   raw, never re-wrapped (the reasoning at `:2332-2336` stays true after item 1).
4. `internal/tui/model.go:1538-1698` — the heartbeat fold and rebind orchestration: fold
   order, the fail-once posture, and `rebindNote`'s existing "" contract (a note is returned
   only when something VISIBLE moved).
5. ADR 0011 / `internal/tui/doc.go` — the value-copied `Model` holds no no-copy type by
   value. This plan adds NO Model state beyond one `bool` on `rebindIntent` (a plain value,
   copy-safe by design); `TestModelNoBuilderByValue` must pass untouched.

---

## Design decisions (2026-07-28, from the issues + a measured probe of the current render)

### Item 1 — the right slot moves as a whole, by `bodyIndent`

- **The issue's premise was measured and is false in code — and that changes the shape of the
  fix.** A throwaway render probe (60 cols, 2026-07-28) showed EVERY right-slot occupant ends
  flush at `m.width` today: `"esc stop"` and the gauge alike (`statusLine` justifies `right`
  to the last column, `model.go:2338-2342`). The hints only READ as inset because a text
  glyph does not fill its cell, while the gauge's track paints solid background right up to
  the terminal edge. Insetting only the gauge would therefore put it 2 cells left of the
  hints that time-share the same slot. The fix insets the WHOLE slot, so the gauge and every
  hint end in the same column — which is what "same as all other information printed there"
  asks for going forward.
- **The inset is `bodyIndent` (2 cols), statusBar-styled, appended at ONE seam.** In
  `statusLine`, after `right := m.statusRight()`: a non-empty `right` gets
  `m.th.statusBar.Render(bodyIndent)` appended. One seam moves every occupant together, and
  the gap arithmetic (`lipgloss.Width(right)`, `:2338`) adapts for free. `bodyIndent`
  (`theme.go:74-80`) rather than a new constant: the left slot already LEADS with it
  (`:2318`), so the row becomes symmetric — two body columns in on the left, two short of the
  edge on the right — and the slot's last text column lands at `width-2`, the same column the
  footer's mode marker ends in (`mode + " " + "│"`, `footerContent`, `model.go:2181-2182`).
- **Styled spaces, not bare ones.** The suffix is rendered through `th.statusBar` (faint on
  black) so the row stays one solid black band to the edge — the exact seam reasoning at
  `model.go:2332-2336`. An EMPTY right slot gets no suffix: the justify gap already paints
  black to the last column, and two orphan cells would change nothing visible.
- **The `gap < 1` truncation path (`:2339-2341`) is untouched.** A window too narrow for both
  slots still drops `right` entirely; the drop threshold shifts 2 columns earlier, which is
  the correct price of the margin.

### Item 2 — suppress the connected note on first contact only

- **What is suppressed, precisely:** `rebindNote`'s seed case (`oldModel == ""`,
  `model.go:1682-1687`) returns `""` when the seed's observation landed at **first contact**
  — captured BEFORE `foldBeat` resets the evidence (`m.hb.failures = 0; m.hb.everOnline =
  true` at `:1550-1551` run before `observeBinding` at `:1557`, so the capture must be the
  fold's first statement). In that case the restated start-up box
  (`applyRebind → transcript.refreshStartup`, `:1631`) already presents host, model, and
  context a few rows above where the note would print — the note is a duplicate of chrome
  the human is already looking at.
- **What still notes, deliberately:** (a) a cold start against a down server — the first
  beat FAILS (`failures > 0`, offline immediately per `TestColdStartFailureIsOfflineImmediately`),
  so when the server appears the seed is no longer first contact and "connected:" prints; it
  doubles as the recovery statement (`foldBeat`'s `crossed && !rebound` rule at `:1558-1560`
  keeps the weaker `onlineNote` suppressed — unchanged). (b) a server that was up but
  MODELLESS at launch — beats land with no active model (`observeBinding` sees no change),
  `everOnline` goes true, and when a model finally loads the seed prints. Both are genuine
  news; only the boring happy path goes quiet. First contact ∧ offline-crossing is impossible
  (a crossing requires a prior failure), so the `onlineNote` interplay needs no new rule.
- **The flag rides the intent.** `rebindIntent` (`model.go:1480-1483`) gains
  `quietSeed bool`: `foldBeat` captures first contact pre-fold and hands it to
  `observeBinding`, which stamps it into the intent; `applyRebind` passes it to `rebindNote`,
  which owns the wording policy (its doc comment's "" contract grows one clause: the quiet
  first-contact seed). Riding the intent keeps the deferred path (`pendingRebind`,
  `applyPendingRebind`) correct by construction — even though a seed can never actually defer
  (submits are refused before the first bind: `blockedUpstream`, `model.go:1763`, pinned by
  `TestSubmitBlockedBeforeFirstBind`). A plain `bool` on a value-carried struct: ADR 0011 safe.
- **NOT suppressed alongside it:** `unknownWindowNote` (`:1638-1640`) — actionable honesty
  about Budget/compaction, not connection narration — and `result.Notices` (`:1635-1637`),
  which carry facts like the validated-set announcement. Both print on a quiet seed exactly
  as they do today.
- **Out of scope, deliberately:** the footer's `connecting…`/offline words, the offline/online
  notes themselves, session-resume paths (a restored session has `opts.Model != ""` — never
  the seed case), and the start-up box (already restated in place; nothing to change).

---

## The ground (verified 2026-07-28 against the working tree at `238fd2c`)

**Status line** (`internal/tui/model.go:2307-2343`): `left` leads with `bodyIndent` (`:2318`);
`right := m.statusRight()` (`:2337`); the justify gap is black-filled statusBar spaces and
`right` is concatenated raw because the gauge is pre-styled (`:2332-2342`). `statusRight`
(`:2361-2381`) time-shares one slot between the Ctrl+C hint, the flash, the gauge, and the
state hints ("esc stop" running / "enter dismiss" errored / "" idle). The gauge chain:
`contextGauge` (`:2387-2389`) → `contextUsage.view` (`:2415-2422`, `"<used> <pct>% <bar>"`)
→ `renderGaugeBar` (`:2430-2457`, fill + eighth-cell + dark-gray track). Probe result: at
width 60, both the hint frame and the gauge frame end at column 60 exactly — no occupant is
inset today. The footer's mode marker ends 2 cells short of the edge (`footerContent`,
`:2181-2182`). `th.statusBar` is faint-on-black with no padding (`theme.go:122`, `:185`);
`bodyIndent = "  "` (`theme.go:80`).

**Heartbeat fold** (`internal/tui/model.go`): `heartbeatState` (`:1442-1473`) — `failures`
counts consecutive failed idle beats, `everOnline` records any landed beat. `foldBeat`
(`:1546-1562`) resets both BEFORE calling `observeBinding` — so first contact must be
captured at the top. `observeBinding` (`:1574-1594`) → `applyRebind` (`:1610-1642`) →
`rebindNote` (`:1680-1698`); the seed wording is the `oldModel == ""` arm (`:1682-1687`).
`applyRebind` also refreshes the start-up box (`:1631`) and returns `true` on any successful
rebind — the repaint the quiet seed still needs (box + footer move even with no note).

**Existing tests.**
- `internal/tui/model_test.go`: `contextGauge` hide/show pins (`:287-300`); a status-line
  ANSI-strip helper that TRIMS whitespace (`:1622` — inset assertions must strip WITHOUT
  trimming); the leading-columns alignment pin (`:1693`); a width sweep asserting
  `ansi.StringWidth(m.statusLine()) <= width` (`:1708`) — all stay green untouched.
- `internal/tui/heartbeat_test.go`: helpers `wireHeartbeat`/`wireRebind`/`unbound` (`:54`,
  `:103`, `:111`), `upBeat`/`downBeat` (`:61`, `:71`), `foldBeatMsg` (`:74`),
  `noteTexts`/`countNotes` (`:118`, `:129`). Tests that currently EXPECT the connected note
  on an immediate seed and therefore change under item 2:
  `TestLateSeedBindsThroughRebind` (`:575`, wants exactly 1),
  `TestPinnedWindowResultSticks` (`:726`, reads the pinned window out of the note's words),
  `TestBeatScriptNarratesEachChangeOnce` (`:826`, the note opens the expected narration),
  `TestUnknownWindowNotedOnBind` (`:851`, wants the no-window-clause wording),
  `TestRebindNoticesSurfaceAsNotes` (`:872`, wants `["connected: …", "validated set: …"]`).

**No overlap with in-flight work.** All four 2026-07-28 plans (00–03) are implemented and
archived; the working tree's only local edit is `ISSUES.md`. Nothing else touches these
regions.

---

## 1. The right slot ends two cells short of the window edge — ✅ DONE (2026-07-28)

**What.** Every right-slot occupant — gauge, hints, flash, Ctrl+C prime — ends at column
`width-2`, aligned with the footer's mode marker below and mirroring the left slot's
`bodyIndent` lead.

- `internal/tui/model.go`, `statusLine` (`:2317-2343`): after `right := m.statusRight()`,
  append `m.th.statusBar.Render(bodyIndent)` when `right != ""`. Nothing else moves — the
  gap arithmetic and the `gap < 1` truncation path already do the right thing with the wider
  `right`.
- Doc comments in the house voice: `statusLine`'s comment gains the margin (the right slot
  ends `bodyIndent` short of the edge — the left lead's mirror, and the footer mode marker's
  column); `statusRight`'s comment notes the caller adds the margin, so its branches stay
  margin-free. The raw-concatenation reasoning at `:2332-2336` is restated to cover the
  suffix (styled spaces, same black band).
- No theme change, no new constant, no Model state.

**Tests.** In `internal/tui/model_test.go`, house harness (`newTestModel`, ANSI-strip
WITHOUT trimming for edge assertions):

- *The gauge is inset:* running model with `ctxUsed`/`ContextWindow` set — the raw
  `m.statusLine()` string ends with `m.th.statusBar.Render(bodyIndent)` (pinning the styled
  suffix without asserting SGR bytes), and its `ansi.StringWidth` still equals the window
  width exactly.
- *The hints move with it:* running model with NO usage — the ANSI-stripped, untrimmed
  status line ends with `"esc stop" + bodyIndent`; one more occupant (the flash, or the
  errored "enter dismiss") asserted the same way, pinning that the WHOLE slot moved, not the
  gauge alone.
- *Empty slot unchanged:* idle with no usage — the stripped line is all black-band fill with
  no phantom suffix; width invariant holds.
- The existing width sweep (`:1708`) and alignment pin (`:1693`) stay green untouched.

**Acceptance.** Green gate passes. Live: with a conversation running, the gauge's last track
cell sits two columns in from the terminal edge, directly above the footer mode marker's last
character; before first usage, "esc stop" ends in that same column. No right-slot occupant
touches the window edge in any state.

**Commit.** `fix(tui): status-line right slot — gauge and hints — ends two cells short of the edge`

## 2. First contact is quiet — no "connected:" note when the server was already up

**What.** The connected note prints only when it carries news: a recovery after a failed
start, or a model appearing on a previously modelless server. The ordinary launch — server
up, first beat lands, model bound — refreshes the start-up box and footer silently.

- `internal/tui/model.go`:
  - `foldBeat` (`:1546`): capture `firstContact := !m.hb.everOnline && m.hb.failures == 0`
    as the fold's FIRST statement (before the `:1550-1551` resets), and pass it to
    `observeBinding`.
  - `observeBinding` (`:1574`): new parameter; stamps `quietSeed: firstContact` into the
    `rebindIntent` it builds.
  - `rebindIntent` (`:1480-1483`): gains `quietSeed bool` with a comment in the house voice
    (an observation FACT — "captured at first contact" — not presentation state; plain value,
    ADR 0011 safe).
  - `applyRebind` (`:1610`): passes `intent.quietSeed` through to `rebindNote`.
  - `rebindNote` (`:1680`): the `oldModel == ""` arm returns `""` when `quietSeed`; its doc
    comment's "" contract grows the clause (the pinned-window case AND the quiet
    first-contact seed — in both, nothing the human is not already shown moved).
  - `unknownWindowNote` and `result.Notices` handling in `applyRebind` unchanged —
    explicitly NOT gated on `quietSeed`.
- `foldBeat`'s doc comment (`:1538-1545`) is extended: first contact says nothing at all —
  the box restated in place IS the statement.

**Tests.** In `internal/tui/heartbeat_test.go`, existing helpers:

- `TestLateSeedBindsThroughRebind` (`:575`): the connected-note count flips to want **0**;
  every other assertion (rebind call, adopted bindings, restated box, footer, unblocked
  upstream) stays — the test becomes the quiet-happy-path pin.
- *New* `TestDelayedConnectNotesOnce`: `downBeat` first (cold start, server down), then
  `upBeat("served-model", 16384)` — exactly one `"connected: served-model, context 16k"`
  note, and zero `onlineNote`s (the crossed-AND-rebound single-statement rule, now pinned
  from the suppression side).
- *New* `TestModellessStartSeedsWithNote`: `upBeat("", 0)` first (server up, nothing
  loaded — no change observed, upstream still blocked), then `upBeat("served-model", 16384)`
  — exactly one connected note: `everOnline` alone defeats `quietSeed`.
- `TestPinnedWindowResultSticks` (`:726`): prepend a `downBeat` so the seed is delayed and
  the pinned-window wording (`"…context 8k"`) still has a note to be read from; assertions
  otherwise unchanged.
- `TestBeatScriptNarratesEachChangeOnce` (`:826`): prepend a `downBeat` to the script and
  fold the resulting offline note into the expected narration; the connected line keeps its
  place. The test keeps pinning once-per-change.
- `TestUnknownWindowNotedOnBind` (`:851`): stays on the IMMEDIATE seed — now asserts zero
  connected notes AND still exactly one `unknownWindowNote` (the honesty line survives the
  quiet seed); the no-window-clause WORDING assertion moves into `TestDelayedConnectNotesOnce`'s
  shape via a window-0 variant there (delayed seed, `upBeat("served-model", 0)` →
  `"connected: served-model"` with no clause).
- `TestRebindNoticesSurfaceAsNotes` (`:872`): stays on the immediate seed — `want` becomes
  `["validated set: strict-json"]` alone, pinning that notices survive the suppression.

**Acceptance.** Green gate passes. Live, owner-verified (not a gate): launching apogee
against a running server shows the start-up box binding straight to the model with NO
"connected:" line under it; killing the server, launching apogee, then starting the server
shows offline → one "connected: …" line; `context window unknown` and validated-set notices
still appear when applicable.

**Commit.** `fix(tui): no "connected:" note when the first heartbeat lands clean`

## 3. Documentation close-out

**What.**

- `layout.md`: two touches in the existing voice — (a) in the bottom-chrome/status-line
  prose, one clause stating the right slot's margin: whatever occupies the right slot (the
  context gauge or a key hint) ends `bodyIndent` short of the window edge, mirroring the
  left slot's lead and the footer mode marker's column; (b) nothing for item 2 (layout.md
  specs geometry, not transcript wording).
- `CHANGELOG.md`, under `## [Unreleased]`: one `### Changed` entry per item — the
  status-line right slot (gauge and hints) now ends two cells short of the window edge; the
  "connected:" note is no longer printed when the first heartbeat lands clean (it still
  announces delayed connections and late-loading models). `VERSION` untouched.
- `ISSUES.md`: mark lines 6 and 8 `[X]` per the legend at `ISSUES.md:1-3`.

**Tests.** None new; `make check` green.

**Acceptance.** Green gate passes; layout.md's status-line prose lets a reader place the
right slot without reading the code; CHANGELOG and ISSUES reflect both landings.

**Commit.** `docs: layout.md right-slot margin, CHANGELOG and ISSUES for the gauge/connect fixes`

# /usage command + cumulative usage accounting — implementation plan

- **Goal:** a `/usage` slash command that opens the shared popup and shows cumulative
  token usage for the whole session — main agent, every sub-agent, and a session total —
  backed by accounting that lives in the engine event path so every Driver (TUI,
  headless, bench) reads the same numbers.
- **Date:** 2026-08-11 · **Status:** not started
- **Sized for:** ~200k-context host
- **Authoritative sources:** ADR 0031 (engine north star; wire-silent, benchable-all-the-way-up);
  `internal/domain/events.go` (UsageEvent contract); `layout.md` (TUI layout spec);
  `README.md` command table (~line 257).
- **Ratified design calls** (owner, 2026-08-11, via AskUserQuestion):
  1. **Scope:** compaction tokens counted, headless/bench totals surfaced, totals persist
     across session save/reload — all three IN.
  2. **Accounting lives in the engine.** Each agent (main + every sub-agent) keeps its own
     running totals and stamps them on every `UsageEvent` it emits. Drivers keep the
     LATEST event per agent — the existing "latest total wins" rule extends to the
     cumulative fields; no driver ever sums events.
  3. **Compaction surfaces via a flagged event:** the compaction call emits a `UsageEvent`
     with `Maintenance: true`. Fill/gauge/tok-per-sec readers SKIP flagged events; totals
     readers accept them. `/usage` is accurate immediately after `/compact`.
  4. **Popup layout: detailed columns** — agent · calls · prompt · completion · total ·
     ctx fill — one row for main, one per sub-agent (indented, gist as name), and a
     session-total row when at least one sub-agent row exists.
  - Mechanical pins by the plan author (2026-08-11): public field names
    `CumulativePromptTokens`, `CumulativeCompletionTokens`, `CumulativeTotalTokens`,
    `CumulativeCalls`, `Maintenance`; popup closes on `esc` (settings precedent);
    sub-agents with zero recorded usage are omitted from the popup.
- **Standing requirements:** skills: coding-standards. Run `make check` before every
  commit. Never change VERSION/CHANGELOG release headings (suggestion only, see close).
- **Out of scope:** cost model (local models — no dollars), per-agent wall-clock
  durations, per-Turn/Step breakdowns, changes to the tokens/sec readout or the status-bar
  gauge rendering, retroactive totals for sessions recorded before this feature (they
  reload with zero totals — acceptable), mechanisms/back-off behaviour
  (`internal/mechanisms/library.go` reads `Budget`, not UsageEvent — untouched).

## 1. Engine: per-agent cumulative counters stamped on UsageEvent — ✅ DONE (2026-08-11)

**What:** `domain.UsageEvent` (`internal/domain/events.go:158`) gains additive fields
`CumulativePromptTokens`, `CumulativeCompletionTokens`, `CumulativeTotalTokens`,
`CumulativeCalls int`, plus `Maintenance bool` (emitted `false` here; first set by item 2).
Each `Agent` (`internal/agent/agent.go`) owns a small unexported tally struct
(prompt/completion/total/calls ints); `runLoop`'s `DeltaDone` arm
(`internal/agent/loop.go:500-519`) increments it before emitting and stamps the cumulative
fields on the event. A child agent built by `newChildAgent`
(`internal/agent/subagent.go:163-219`) gets a FRESH tally, so its events carry
child-local cumulative totals — per-agent grouping stays driver-side via the existing
Depth + CallID stamps. Extend the `UsageEvent` doc comment: cumulative fields are
per-emitting-agent and latest-wins, like the fill fields; `Maintenance` marks
non-Turn accounting events that fill readers must skip. The public re-export
(`apogee.go:174`) is a type alias — no change needed there.

**Tests:** unit test in `internal/agent`: two consecutive completions through a fake
upstream emit events with increasing cumulative fields (calls 1 then 2, sums add up);
a sub-agent run emits Depth>0 events whose cumulative fields count only the child's
calls, and the parent's next event is unaffected.

**Acceptance:** `go build ./... && go test ./internal/agent/... ./internal/domain/...`
then `make check`.

**Commit:** `feat(engine): accumulate per-agent usage and stamp cumulative totals on UsageEvent`

## 2. Engine: compaction usage counted via flagged maintenance event — ✅ DONE (2026-08-11)

Depends on item 1.

NOTES (2026-08-11): beyond the item's literal text, the existing test that pinned the OLD
silence contract had to move with it — `TestCompactEmitsNoTokenOrUsageEvents` is renamed
`TestCompactEmitsNoTokenEventAndNoUsageWithoutServerReport` (its fake reports no usage, so the
fold accounts for nothing) and `compact_test.go`'s file header restated. The emission is
conditional on the server reporting usage and sits past the cancel/fault exits, so a faulted or
cancelled fold accounts for nothing; unlike `streamResponse` the compaction call does NOT
calibrate the chars→token estimator (its prompt is a rendered transcript, not the conversation).
The item-1 `UsageEvent` doc comment already covers `Maintenance` — left unchanged.

**What:** `compactCompleter.Complete` (`internal/agent/compact.go:311`) today deliberately
emits no UsageEvent (comment at `compact.go:302-308`). Keep the "must not move the live
gauge" intent but stop losing the tokens: capture the compaction stream's `DeltaDone`
usage, add it to the owning agent's tally (calls +1), and emit a `UsageEvent` with
`Maintenance: true`, the compaction call's own prompt/completion/total in the fill
fields, and the updated cumulative fields. Rewrite the `compact.go:302-308` comment to
state the new contract (accounting event, flagged, gauge readers skip). Update the
UsageEvent doc comment only if item 1's wording needs sharpening — no consumer changes
here; consumers learn to skip the flag in items 3 and 5.

**Tests:** unit test in `internal/agent`: a compaction run emits exactly one
`Maintenance: true` UsageEvent whose cumulative fields include the compaction call; the
next regular Turn's event continues from those totals with `Maintenance: false`.

**Acceptance:** `go build ./... && go test ./internal/agent/...` then `make check`.

**Commit:** `feat(engine): count compaction usage via flagged maintenance UsageEvent`

## 3. TUI: fold cumulative usage per agent and persist it — ✅ DONE (2026-08-11)

Depends on items 1 and 2.

**What:** consume the new fields, keeping latest-wins:
- `foldStats` (`internal/tui/fold.go:46-82`): Depth-0 events update new Model fields
  holding the main agent's latest cumulative reading; events with `Maintenance: true`
  must NOT update `m.ctxUsed` or the tokens/sec clock (they DO update the cumulative
  reading).
- `transcript.applyUsage` (`internal/tui/transcript.go:706-735`): sub-agent head entries
  (`transcript.go:175-176`) gain cumulative fields (prompt/completion/total/calls) folded
  from Depth>0 events by CallID, latest-wins; maintenance events skip the
  `ctxUsed`/`ctxLimit` update, fold the cumulative fields.
- Persistence: round-trip the new sub-agent entry fields in
  `internal/tui/transcriptcodec.go` (alongside ctxUsed/ctxLimit at `:78`, `:279`, `:358`);
  main-agent totals join session `Meta` (`internal/session/store.go:96`, next to
  `CtxUsed`) and are restored on session reopen (`internal/tui/sessions.go:498`,
  `internal/tui/model.go:389`). Pre-feature sessions load as zero totals.
- Respect the value-Model rule (ADR 0011): plain ints only, no no-copy types.

**Tests:** fold unit tests (depth-0 cumulative tracked; maintenance event leaves ctxUsed
and tok/sec untouched but advances totals; sub-agent entry folds by CallID); codec
round-trip test extended for the new fields; Meta save/restore test.

**Acceptance:** `go build ./... && go test ./internal/tui/... ./internal/session/...`
then `make check`.

**Commit:** `feat(tui): fold cumulative usage per agent and persist it with the session`

## 4. TUI: /usage command + popup pane — ✅ DONE (2026-08-11)

Depends on item 3.

NOTES (2026-08-11): two additions beyond the item's literal row list. (a) The rows open with a COLUMN
HEADER row (`agent · calls · prompt · completion · total · ctx`, `popupRowHeading` kind, the /settings
section-label precedent) — six numeric columns are unreadable unlabelled; it is dropped with the rows
in the empty state. (b) `esc` is claimed just BELOW the autocomplete claim rather than beside the modal
ones at `model.go:987-1005`: the pane is not modal (it owns no other key and the box behind it stays
live), so a dropdown the human opened over it must answer its own esc first. Three existing enumerations
gained the new pane for accuracy — the framePane give-way comment, `frameOverlays`'s doc, and
layout.md's two pane lists — and `command_test.go`'s two pinned verb sets (parser order, noRecall set)
were extended, which is what those guards are for.

**What:**
- Command: add `{name: "usage", summary: "session token usage — main agent and every sub-agent", whileRunning: true, noRecall: true}`
  to `commandSpecs` (`internal/tui/command.go:144-163`, alphabetical — between
  `unload-model` and `version`); dispatch `case "usage"` in `runCommand`
  (`internal/tui/commandrun.go:223`) opening the pane (settings precedent:
  `commandrun.go:267` → `settings.go:322-330`).
- Pane: new `paneUsage` constant in the ordered `framePane` enum
  (`internal/tui/model.go:3351-3358`; place it with the transient popups per the enum's
  give-way-order comment), predicate in `openPanes()` (`model.go:3370-3393`), slot in
  `frameOverlays` (`model.go:2306-2352`), stacked in `View()` (`model.go:2405-2420`).
  Not full-height — normal floating popup.
- Renderer: `renderUsage()` calls `m.popupBudget(paneUsage, …)` then
  `renderPopup(m.th, popupSpec{…}, m.width)` (`internal/tui/popup.go:299-366`).
  Columns per ratified call 4: agent · calls · prompt · completion · total · ctx.
  Rows: `main` first (Model's depth-0 totals; ctx = current fill % from
  ctxUsed/ctxLimit), then sub-agents in transcript order (indented, sub-agent
  name/gist truncated; totals and fill from the entry fields of item 3; zero-usage
  sub-agents omitted), then a `session` total row (sums of the rows above) only when at
  least one sub-agent row exists. Token counts via `format.Tokens` (the
  `subagentblock.go:322` precedent). Empty state (no usage yet): a single body line
  saying no usage has been reported yet.
- Keys: `esc` closes (route alongside `model.go:987-1005`); pane owns no other input.
- `layout.md` gains a short usage-popup section (this item OWNS that doc edit).

**Tests:** renderer unit test (rows, session-total presence/absence, empty state) in the
house colour-agnostic style; alphabetical-order command test already pins the table;
openPanes/give-way covered by existing pane tests if present, else a minimal predicate test.

**Acceptance:** `go build ./... && go test ./internal/tui/...` then `make check`.

**Commit:** `feat(tui): /usage command with session usage popup`

## 5. Headless/run: cumulative totals in Result — ✅ DONE (2026-08-11)

Depends on items 1 and 2 (independent of items 3–4).

**What:** `internal/run/run.go` — `SubAgentUsage` (`run.go:93-110`) gains
`Calls, PromptTokens, CompletionTokens, TotalTokens int` (cumulative, latest-wins from
the child's events); `Result` (`run.go:76`) gains a main-agent totals struct
(`Usage` with the same four fields). `eventTap` (`run.go:318-430`): `noteUsage` keeps
latest cumulative per agent and skips `Maintenance: true` events for the `Used` fill
while still folding their cumulative fields. Headless output
(`cmd/apogee/headless.go:474-507`) prints a short usage summary block (main totals +
per-sub-agent lines) alongside the existing sub-agent report.

**Tests:** `internal/run` unit test: synthetic event stream (including a maintenance
event) yields correct Result totals and SubAgentUsage fields; fill unaffected by the
maintenance event.

**Acceptance:** `go build ./... && go test ./internal/run/...` then `make check`.

**Commit:** `feat(run): surface cumulative usage totals in Result and headless output`

## 6. TUI: mouse support for the usage popup

Depends on item 4.

**What:** render via `renderPopupPlaced` (`internal/tui/popup.go:372`) and register the
placement for hit-testing in `internal/tui/mouse.go` following the existing popup
precedent (`mouse.go:778`, `:913`): click outside closes; wheel scrolls when rows exceed
the popup's row budget.

**Tests:** mouse-mapping unit test in the existing `mouse.go` test style (click-outside
closes; hit inside is consumed).

**Acceptance:** `go build ./... && go test ./internal/tui/...` then `make check`.

**Commit:** `feat(tui): mouse hit-testing for the usage popup`

## 7. Docs + changelog

Depends on items 4 and 5.

**What:** add the `/usage` row to the README command table (~`README.md:257`); one
`CHANGELOG.md` line under `[Unreleased]` describing the feature (command + engine
accounting + headless totals). No release heading, no VERSION change. Verify
`layout.md` already carries the item-4 section (owned there — do not duplicate).

**Tests:** none (docs).

**Acceptance:** `grep -n "/usage" README.md CHANGELOG.md` shows both entries;
`make check` still passes.

**Commit:** `docs: document the /usage command and cumulative usage accounting`

---

**Suggested version bump** (not performed): micro feature bump per house convention
(VERSION micro-bumps per shipped feature) once the plan is executed — owner's call.

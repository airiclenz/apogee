# Busy indicator plan

**Goal:** every slow wait the TUI holds outside a running Turn shows the user's `ui.spinner` glyph, a phrase and
an elapsed clock in the status line, and the pop-up `loading…` rows animate with the same glyph, so a slow box
never looks stuck. Pure TUI presentation (Driver-local); the engine is untouched.
**Date:** 2026-11-06
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 2afc5788
**Closes:** idle-wait-no-spinner

**Sources:**
- `.capcom/issues/idle-wait-no-spinner.md`
- `internal/tui/spinner.go` (the chain, `spinnerAnim`, `foldSpinnerTick`), `internal/tui/doc.go` (Model copy rules)
- `layout.md` — "The status line's spinner", "The live star", the footer model slot, the `/sessions` pop-up
- ADR 0011 (value-copied Model), ADR 0024 (generation-guarded tick chains), ADR 0029 D6 (a load is narrated, not modal), ADR 0040 (scheme switch moves spinner colour)

**Ratified design calls** (owner, 2026-11-06):
- **Status line form:** glyph + phrase + clock, the running row's shape — `⠶⠶ loading qwen3… · 01:04s`. The footer model slot keeps its own word unchanged.
- **Pop-up loading rows:** animate — the row reads `<glyph> loading…` while a pane's listing is read.
- **MCP reconnect freeze:** out of this plan — its own issue, `mcp-reconnect-freezes-tui`.
- **Style:** "snail" in the request means the existing `snake` style; the busy indicator draws whatever `ui.spinner` / `ui.spinner-color` / `ui.glyphs` select — no new style, no new setting.
- **Phrases:** reuse the words apogee already shows for each wait (`actuationLabel`, `connectingLabel`, the git action note `git <verb>…`, `naming this session…`) plus `loading session…` for a `/sessions` record load.
- **Live star:** blinks only while `stateRunning`; a busy chain in idle advances frames but never flips a live star.

**Regression check (2026-11-06, 2afc5788):**
- 1: guard folded (test ticks on `spin.gen`; every spinner-to-worker comment, `worker.gen`/`resume` retired or redocumented).
- 2: guard folded (owner decision: idle-only tail arm, shared tick-stripping test helper; current-gen drop; no offline/prebound).
- 3: guard folded (owner decision: reuse item 2's helper; the branch listing is not `git switch…`).
- 4: guard folded (owner decision: reuse item 2's helper; adjacent styled runs; source ranks last); yields to popup.go's row contract.
- 5: guard folded (`capcom know add --id … --title … --parent <section id>`).

**Standing requirements:**
- skills: coding-standards
- This box: run tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./internal/tui/` (one package, one test pattern); never a package pattern, never two runs at once.
- Bubble Tea v2 names (`tea.KeyPressMsg`, `msg.String()`); no `strings.Builder` or `lipgloss.Style` held by value on the Model (doc.go).
- A pointer-method arm binds its Cmd in its own statement, never `return m, m.spin.arm(…)`.

**Out of scope:**
- The MCP reconnect freeze (`mcp-reconnect-freezes-tui`).
- Waits before the TUI opens (MCP start-up connect, `--resume` replay).
- Sub-second waits with no pane (`/workflows` list, `/bg` launch, `/skills` rescan, clipboard paste).
- A new spinner style or a busy-indicator setting; headless and daemon Drivers.

## 1. The spinner chain owns its generation

**What:**
**Goal:** the spinner's tick chain is guarded by a generation `spinnerAnim` owns, and it stays alive exactly while a
single predicate on the Model (`spinning`) holds; at this item that predicate is `state == stateRunning`, so every
visible behaviour is unchanged.
**Approach (assumed at the header base):** add a `gen int` to `spinnerAnim`; `arm` bumps it and zeroes the frame,
`tick` stamps it. `foldSpinnerTick` drops a tick unless `msg.gen == m.spin.gen && m.spinning()`. The three arm
sites (`enterRunning`, `resumeRunning`, the watch follower in `watch.go`) call `arm` without `worker.gen`; keep
`worker.start` / `worker.resume` as they are for their other readers. Re-arming still retires an in-flight chain,
so an approval or ask round-trip cannot double the frame rate. Update the comments in `spinner.go` and
`model.go` that say the chain's generation is the worker's (rule: every comment naming `worker.gen` as the
spinner's generation; `grep -n "worker.gen\|worker\.gen" internal/tui/*.go`).
**Regression guard.** Every test tick moves to `m.spin.gen` (`grep -n 'spinnerTickMsg{gen: .*worker.gen'
internal/tui/*_test.go`), and `TestSpinnerTickChainGeneration` asserts on `spin.gen`, not `worker.gen`. The comment
rule is every comment tying the spinner chain to the worker (`grep -n "worker\.gen\|worker\.start\|worker\.resume\|tick-chain generation\|generation the worker" internal/tui/*.go`, worker.go's `gen` doc included); no production code
reads `worker.gen` after the item, so retire `worker.gen` / `worker.resume` (and watch.go's call) or redocument them.
**Files:** internal/tui/spinner.go, internal/tui/model.go, internal/tui/watch.go, internal/tui/worker.go, internal/tui/spinner_test.go, internal/tui/mouse_test.go
**Read first:** internal/tui/spinner.go — arm, foldSpinnerTick; internal/tui/model.go — enterRunning, resumeRunning;
internal/tui/watch.go — foldWatchBatch; internal/tui/worker.go — worker.gen, resume; internal/tui/spinner_test.go — TestSpinnerTickChainGeneration
**Tests:** existing spinner generation tests pass unchanged in meaning, their ticks carrying `spin.gen` (spinner_test.go, `TestTranscriptClickTogglesALiveBlockAcrossTheBlink`); add `TestSpinnerChainOwnsGeneration` — arm twice, a tick carrying the first generation is dropped, one carrying the second advances the frame.
**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSpinner|TestTickRepaint|TestBlinkingStar|TestTranscriptClickTogglesALiveBlockAcrossTheBlink' ./internal/tui/`
**Commit:** `refactor(tui): the spinner chain owns its generation`

## 2. Busy waits keep the spinner turning in the status line

Depends on item 1.

**What:**
**Goal:** while the session is idle and a launcher verb is in flight or the footer says `connecting…`, the status
line reads `<glyph> <phrase> · <clock>` — the phrase being the footer's own word for that wait — the chain ticks,
and the clock counts from when that wait began; once the wait ends the slot is as it was.
**Approach (assumed at the header base):** new file `internal/tui/busy.go` is the one module that knows what the
TUI is waiting on: `func (m Model) busyWait() (phrase string, ok bool)`, derived at call time from the existing
latches — `actuationLabel()` first, then `connectingLabel` under the same condition `upstreamSegments` uses —
never a mirrored flag. A small value `busyClock{phrase string; since time.Time}` on the Model is restamped
whenever `busyWait`'s phrase changes (so a load handing over to `connecting…` restarts the clock). `spinning()`
becomes `stateRunning || (stateIdle && busyWait ok)`. Update's tail (where `settle` runs) arms the chain when
`spinning()` holds and no chain is live (track liveness on `spinnerAnim`; a dropped tick clears it). The
`stateIdle` branch of `statusLeftWithin` shows the busy form after `settingsGiveWayPhrase` and `preboundPhrase`
(they keep precedence), composed like `runningPhrase` with `formatElapsed`; no `· quiet` qualifier. Gate the
frame key's `blink` (`paintcache.go` `frameKey`) and every other `spin.blink` reader on `stateRunning`.
**Regression guard.** The Update-tail arm fires ONLY for an idle busy wait (stateIdle && busyWait ok && no chain live); a running chain stays armed solely by the existing arm sites (enterRunning, resumeRunning, the watch follower), so startStubWorker-style fixtures that set stateRunning without arming get no extra Cmd. Item 2 also owns one shared test helper that strips spinnerTickMsg from a Cmd tree (flattening tea.Batch); every existing test that asserts `cmd == nil` or folds a Cmd unflattened after starting a wait item 2 makes busy is moved onto that helper, and item 2's Acceptance -run pattern widens to the existing test families of the waits it touches (actuation, heartbeat/connecting, start-up frames) as the reviewers' reports name them.
Only a drop of the CURRENT generation clears liveness (`msg.gen == m.spin.gen && !m.spinning()`); a stale drop
leaves it, every `arm` sets it. The connecting source leaves out `m.hb.offline` and `m.prebound()` (`CanObserve` is
always true), so an offline or prebound idle session runs no ticks. Supersedes `statusLeftWithin`'s "Idle says
nothing … with TWO exceptions" comment (model.go): rewrite it for the third.
**Files:** internal/tui/busy.go, internal/tui/busy_test.go, internal/tui/spinner.go, internal/tui/model.go, internal/tui/paintcache.go, internal/tui/render.go, internal/tui/actuation_test.go, internal/tui/workflows_test.go, internal/tui/settings_test.go
**Read first:** internal/tui/model.go — Update (deferred tail), statusLeftWithin, upstreamSegments; internal/tui/paintcache.go — frameKey;
internal/tui/prebound.go — prebound; internal/tui/actuation.go — actuationLabel; internal/tui/actuation_test.go — pumpItem, wireLauncher
**Tests:** `TestBusyLoadShowsSpinnerPhraseClock` (drive `/model <profile>` against a launcher stub: frame holds the glyph and `loading <profile>… · `), `TestBusyConnectingShowsSpinner`, `TestBusyClockRestartsOnHandover`, `TestBusyEndsClearsSlot`, `TestBusyChainStopsWhenWaitEnds`, `TestLiveStarHoldsOutsideRunning` (a live workflow block in idle under a busy wait never flips), `TestSpinnerStaleTickKeepsChainLive`, `TestBusyOfflineAndPreboundRunNoTicks`; `pumpItem`/`driveActuation` and the `cmd == nil` asserts move onto the tick-stripping helper.
**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestBusy|TestLiveStar|TestSpinner|TestActuation|TestProfileLoad|TestServerActuation|TestWorkflowsView|TestSettingsPane' ./internal/tui/`
**Commit:** `feat(tui): the status line spins while a launcher verb or the first beat is awaited`

## 3. Session load, git actions and /rename naming are busy waits

Depends on item 2.

**What:**
**Goal:** a `/sessions` record load shows `<glyph> loading session… · <clock>`, a footer git action shows
`<glyph> git <verb>… · <clock>`, and a bare `/rename` shows `<glyph> naming this session… · <clock>` in the status
line until each lands.
**Approach (assumed at the header base):** extend `busyWait` in `busy.go` (no second module) with three more
sources, in this precedence after the actuation and before `connecting…`: `holdGitAction` (phrase from the same
`git <verb>…` text `startGitAction` notes — share one helper, not two literals), `holdSessionLoad`
(`loading session…`, a named constant), and a bare `/rename` in flight. The last has no latch today: add a plain
`bool` set where `autotitle.go` issues the manual `titleCmd` and cleared in `foldManualTitle`, zero value meaning
"not naming". Each source is read from the existing hold (`engineholds.go`), never mirrored.
**Regression guard.** reuse item 2's spinnerTickMsg-stripping helper for the existing session-load, git-action and /rename tests it affects, and widen item 3's Acceptance -run pattern to those existing test families as the reviewers' reports name them.
The branch listing (`m.git.action == gitActionSwitch && m.git.branch == ""`, `startGitBranches`) is no `git switch…`
wait: skip it or give it its own phrase, pinned in `TestBusyGitAction`.
**Files:** internal/tui/busy.go, internal/tui/busy_test.go, internal/tui/autotitle.go, internal/tui/model.go, internal/tui/gitactions.go, internal/tui/sessions_test.go, internal/tui/watch_test.go, internal/tui/autotitle_test.go, internal/tui/gitactions_test.go
**Read first:** internal/tui/gitactions.go — startGitAction, startGitBranches; internal/tui/autotitle.go — runRename, foldManualTitle;
internal/tui/engineholds.go — holdGitAction, holdSessionLoad; internal/tui/sessions_test.go — filterableBrowser; internal/tui/autotitle_test.go — newTitlingModel
**Tests:** `TestBusySessionLoad`, `TestBusyGitAction` (the branch listing shows no `git switch…`), `TestBusyManualRename` (each drives the real verb with its reply held unrun, asserts the status line, then folds the reply and asserts the slot clears); the `cmdMsg(cmd).(` / `cmd().(` asserts in sessions_test.go, watch_test.go and autotitle_test.go unwrap through the helper.
**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestBusy|TestSessionBrowser|TestWatchOwner|TestRename|TestGitAction' ./internal/tui/`
**Commit:** `feat(tui): session loads, git actions and /rename naming show the busy spinner`

## 4. Pop-up loading rows animate

Depends on item 2.

**What:**
**Goal:** the `/sessions` browser and the workflow item detail paint their loading row as `<glyph> loading…` with
the current spinner frame, and the chain ticks while either is loading in idle.
**Approach (assumed at the header base):** `popupLoadingRow` (`popup.go`) stays the one spelling of the word; a
helper in `popup.go` composes the glyph, a space and the word for both panes (`sessions.go`, `workflows.go`).
Paint the glyph through `spinnerAnim` on the pop-up's own background, not `spinnerBase` (the status bar's); a new
style lives in `themeStyles`, never on the Model by value. Width: the row measures the glyph (snake and glitter 2
columns, classic and `ui.glyphs: safe` 1). `busyWait` gains a pop-up-loading source with no status-line phrase:
it keeps `spinning()` true but leaves the status slot blank. The loading legend (`esc close` alone) and the
inert keys, clicks and pastes are unchanged.
**Regression guard.** reuse item 2's spinnerTickMsg-stripping helper wherever a pop-up-loading wait adds the tail-arm Cmd to an existing sessions/workflows test, and include those existing test families in item 4's Acceptance -run pattern.
Compose the loading line as adjacent styled runs (glyph run + `statusFaint`-on-surface run), as `runningForms` does
`glyph + statusBar.Render(" ")`, never an ANSI-carrying cell (`popupRowLine` wraps the row in `statusFaint`; the glyph's
reset strips it); `spinnerBase` is already `Background(surface)`, the pop-up's `blackFill`. Yields to popup.go's row
contract "Cells arrive escape-stripped". The source ranks last (after `connecting…`); the busy form needs a phrase.
**Files:** internal/tui/popup.go, internal/tui/sessions.go, internal/tui/workflows.go, internal/tui/busy.go, internal/tui/theme.go, internal/tui/model.go, internal/tui/sessions_test.go, internal/tui/workflows_test.go, internal/tui/autotitle_test.go, internal/tui/watch_test.go, internal/tui/sessionsave_test.go, internal/tui/commandrun_test.go
**Read first:** internal/tui/popup.go — popupLoadingRow, popupRowLine; internal/tui/spinner.go — spinnerAnim.view; internal/tui/model.go — runningForms;
internal/tui/sessions.go — browserListContent; internal/tui/workflows.go — workflowsRows; internal/tui/sessions_test.go — openBrowser, beginBrowserOpen
**Tests:** extend `TestSessionBrowserOpensLoadingBeforeList` (the row holds a spinner glyph before `loading…`; two ticks later the glyph differs under `snake`); `TestWorkflowItemLoadingAnimates`; `TestPopupLoadingRowSafeGlyphs` (one-column ASCII frame); `openBrowser` / `beginBrowserOpen` fold through the helper; a pop-up loading over an in-flight `/model x` keeps `loading x… · clock`.
**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSessionBrowser|TestWorkflowItem|TestPopupLoading|TestWorkflowsView|TestAutoTitle|TestSessionName|TestWatchCarries|TestWatchFooter|TestRestoreCachesBoundarySnapshot|TestResumedRecord|TestSessionSwitch' ./internal/tui/`
**Commit:** `feat(tui): pop-up loading rows turn the spinner`

## 5. Document the busy indicator

Depends on items 3 and 4.

**What:**
**Goal:** `layout.md`, the manual and the glossary describe the busy indicator: which waits show it, its form, the
pop-up loading row's glyph, and that live stars blink only in a running Turn.
**Approach (assumed at the header base):** in `layout.md`, extend "The status line's spinner" with the idle busy
form and its waits; amend the `/sessions` pop-up's `loading…` sentence to the animated row (keep `esc close`
within two lines after it). In `docs/manual/sessions.md` amend the loading-row sentence; in
`docs/manual/configuration.md` say `ui.spinner` also drives the busy indicator. Add the glossary Note
"Busy indicator" (`capcom know add --parent glossary` under the fitting section; read `capcom know tree glossary
--depth 1` first). Rule for the sweep: every doc sentence saying the spinner runs only during a Turn, or that a
`loading…` row is plain text — `grep -rn "loading…\|spinner" layout.md docs/manual/`.
**Regression guard.** `--parent glossary` would file the Note beside the sections: spell it `capcom know add --id
busy-indicator --title "Busy indicator: …" --parent <section id>` (e.g. `glossary-safety`, beside `color-scheme`).
**Files:** layout.md, docs/manual/sessions.md, docs/manual/configuration.md, .capcom/knowledge/glossary/
**Read first:** layout.md — "The status line's spinner", "The live star", "The firing block" (spinner belongs to the worker), /sessions loading sentence;
docs/manual/sessions.md — /sessions bullet; docs/manual/configuration.md — ui.spinner paragraph; .capcom/knowledge/glossary/glossary-safety — color-scheme.md;
cmd/apogee/docs_settings_test.go — TestManualDocumentsEverySettingsKey
**Tests:** none (docs).
**Acceptance:**
- `grep -n "loading…" docs/manual/sessions.md layout.md`
- `grep -n -A2 'loading…' docs/manual/sessions.md layout.md | grep 'esc close'` — one hit per file
- `capcom know show "Busy indicator"`
- `capcom check`
**Commit:** `docs(tui): the busy indicator`

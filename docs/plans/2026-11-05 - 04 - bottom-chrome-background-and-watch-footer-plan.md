# Bottom chrome background and watch footer — implementation plan

**Goal:** the bottom of the screen paints one background to the full width under every scheme, and while watching a live session the footer names the watched session's folder and branch, marked as not apogee's own workspace.
**Date:** 2026-11-05
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** af1928b2

**Sources:**
- `.capcom/issues/prompt-box-uneven-background.md`, `.capcom/issues/watch-footer-wrong-workspace.md`
- `layout.md` (field paragraph ~line 114, prompt-box half rows ~line 22, footer colouring ~121-131, "While you watch" ~1945, git segment ~1961-1990)
- `docs/design/test-drivers.md` (§tuitest/Frame), `capcom know show "watch-mode"`, `capcom know show "live-session"`
- commit 055f11fa (watch routing of footer git action landings); `docs/plans/archived/2026-11-05 - 03 - footer-git-actions-plan.md`

**Ratified design calls** (owner, 2026-11-05):
- **Background:** the TUI sets the terminal's default background (`tea.View.BackgroundColor`, OSC 11) to the scheme's `surface`; the whole screen, transcript included, takes it; the gap row and gauge cells are also painted explicitly; layout.md's "no full-screen background" text is amended.
- **Watch marker:** a `watching` word as its own footer piece before the path, both in the `footer-accent` tone; the path elides first, the marker is kept; an unknown watched workspace shows the marker alone, no path, no git segment.
- **Watched git:** the one git status chain is retargeted to the watched workspace while watching; the fetch chain is off during a watch; leaving re-reads the host at once.
- **Git click while watching:** refused with the existing watch-refused flash; the picker never opens during a watch.
- **ADR 0040 D9:** superseded by the Background call via adr-0040-amend-1105b (owner, 2026-11-05).

**Closes:** none at plan level (each item names its own).

**Standing requirements:**
- skills: coding-standards
- Tests run one package and named tests only (`go test -race -count=1 -run '<names>' ./internal/tui/`); never a multi-package `go test`.
- Any deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Footer host, model and effort segments while watching (they stay the watcher's).
- Any git action targeting the watched workspace.
- A second, concurrent git poll.
- Terminals that ignore OSC 11 beyond the explicit row painting item 1 adds.

**Regression check (2026-11-05, af1928b2):**
- 1: guard folded (writer decision: BackgroundColor only under a colour profile; supersedes ADR 0040 Decision 9 via adr-0040-amend-1105b, glossary Color scheme amended).
- 2: guard folded (watchLive takes the record's workspace when the journal frame carries none).
- 3: guard folded (Acceptance -run pattern corrected; refusal driven through openGitActions' gate by the footer click; writer decision on the shared test).
- 4: recast (writer decision: one chain on the startup watch, host re-read beside the resume Sequence, nil Cmd leaves inFlight false while watching an unknown workspace).
- 5: guard folded (marker seated before workdir in footerRow, priority below workdir, per-root fakeGitHost, watched display form computed at enterWatch; writer decision on TestWatchRefusesTheGitActionsPicker).
- 4 (re-check): guard folded (gitRoot answers the watch's root while any watch is active; inFlight set only beside a non-nil gitStatusCmd at every site, enterStartupWatch re-derives it, leaveWatch issues directly; doc.go and layout.md chain prose names the watch).

## 1. Paint the scheme surface as the terminal background — ✅ DONE (2026-11-05)

NOTES (2026-11-05): the colour-profile guard is a new theme field `setsBackground`, set from `padsPrompts(msg.Profile)` in Update's tea.ColorProfileMsg case beside padPrompts (same colour test, no new predicate) and carried across applyColorScheme with measure/padPrompts/glyphs; gofmt realigned the neighbouring `measure:`/`glyphs:` lines of newTheme's literal.
NOTES (2026-11-05): the gauge's empty track and partial cell keep their `chrome` groove by design (renderGaugeBar), so TestBottomChromePaintsOneBackground allows one contiguous run of 1..gaugeWidth chrome cells on the status line and requires `surface` on every other cell (gauge filled cells included); the test seeds ctxUsed so the gauge is drawn — the literal "every cell carries surface" would forbid the groove.
NOTES (2026-11-05): TestViewSetsSurfaceAsTerminalBackground covers NoTTY beside Ascii and asserts the nil background survives a scheme switch on a colourless profile.
NOTES (2026-11-05): layout.md sentences at the breadcrumb (~1441) and staged-interjection band (~2200) saying "the terminal's own background never shows through" were left as written: they describe explicit row painting, which still holds and still matters on terminals that ignore OSC 11 or carry no colour.

**What:**
**Goal:** `Model.View` returns a `tea.View` whose `BackgroundColor` is the active scheme's `surface` (following a live scheme switch), and every cell of the gap row above the prompt box, the top rule, status line, prompt box rows, both footer rows and the bottom rule carries the `surface` background to the full terminal width.
Fixes the defect in `prompt-box-uneven-background`: chrome paints explicit `surface` while the gap row and EL-cleared trailing blanks show the terminal's own default, so rows band.
**Approach (assumed at the header base):**
- `internal/tui/model.go` `Model.View`: set `v.BackgroundColor = m.th.surface` beside `v.AltScreen`. Read it from `m.th` so `applyColorScheme` (`internal/tui/settingsapply.go`) moves it with no extra code.
- The gap row (`rows = append(rows, "")` in `View`): render with `squareOnField(m.th.measure, m.th.statusBar, "", m.width)` instead of a bare empty string.
- `internal/tui/theme.go` `gaugeFill`: add `Background(surface)`.
- Re-read `promptPad`'s comment in `theme.go` (half-row cells assume the terminal's own bg on the other half); update the comment to the new field, no behaviour change.
- `layout.md`: amend the field paragraph (~114) and the half-row wording (~22) to say the terminal background is set to `surface` while apogee runs and restored on exit; state the rule, then grep `layout.md` for every other "terminal's own background" / "no full-screen background" sentence and amend each.
**Regression guard.** (a) View.BackgroundColor is set only while the colour profile carries colour — a theme flag folded in the tea.ColorProfileMsg case beside padPrompts and carried across applyColorScheme; NO_COLOR / Ascii / NoTTY leave it nil, and TestViewSetsSurfaceAsTerminalBackground gains an Ascii case asserting nil. (b) The item explicitly supersedes ADR 0040 Decision 9 (`.capcom/knowledge/decisions/adr-0040/adr-0040-decision.md`, "No full-screen background painting") by the owner's ratified Background call (2026-11-05): it adds a dated amendment Note adr-0040-amend-1105b (`capcom know add --id adr-0040-amend-1105b --parent adr-0040`), names it on ADR 0040's Status line and in its reading-order list, and amends the glossary Color scheme Note (.capcom/knowledge/glossary/glossary-safety/color-scheme.md, the "terminal's background" sentence); both paths go on **Files:**.
**Files:** internal/tui/model.go; internal/tui/theme.go; internal/tui/settingsapply.go; internal/tui/background_test.go; layout.md; .capcom/knowledge/decisions/adr-0040.md; .capcom/knowledge/decisions/adr-0040/adr-0040-amend-1105b.md; .capcom/knowledge/glossary/glossary-safety/color-scheme.md
**Read first:** internal/tui/model.go — Model.View, ColorProfileMsg case in Update; internal/tui/settingsapply.go — applyColorScheme; internal/tui/theme.go — newTheme (gaugeFill, promptPad, statusBar), padsPrompts;
internal/tui/boxdraw.go — squareOnField; internal/tui/paint_test.go — paintFrame; charm.land/bubbletea/v2@v2.0.8 cursed_renderer.go — close, flush colour loop
**Tests:** new `internal/tui/background_test.go`:
- `TestViewSetsSurfaceAsTerminalBackground` — dark and light schemes, and after `applyColorScheme` switches scheme, `View().BackgroundColor` equals `m.th.surface`; under an Ascii (NO_COLOR) profile `View().BackgroundColor` is nil.
- `TestBottomChromePaintsOneBackground` — paint `View().Content` into a `uv.ScreenBuffer` (variant of `paintFrame` in `paint_test.go`), and for every cell x in `[0,width)` of the gap row through the bottom rule assert `Style.Bg` equals `surface`, including cells past the end of each row's text (footer row 2 with only workdir+git, row 1 after the mode label, status line margin after the gauge).
**Acceptance:**
- `go build ./...`
- `go test -race -count=1 -run 'TestViewSetsSurfaceAsTerminalBackground|TestBottomChromePaintsOneBackground|TestRenderPopupIsFullyBlackFilled|TestFooter' ./internal/tui/`
**Closes:** prompt-box-uneven-background
**Commit:** `fix(tui): paint the scheme surface as the terminal background`

## 2. Carry the watched session's workspace into the watch — ✅ DONE (2026-11-05)

NOTES (2026-11-05): the workspace lives on watchSource only, not also as a separate watchState field: watchState already holds the source, so a second copy would duplicate it; leaveWatch's reset of watchState clears it all the same. Accessor is `watchedWorkspace() (workspace string, isWatching bool)` beside topLevelModel, the bool telling a watch of an unknown workspace from no watch.
NOTES (2026-11-05): sessions_test.go needed no change (the test reuses its liveRowWith/watchedFinished/storeMeta helpers); newWatchFixture needed no workspace argument since the browser rows and Options.Watch carry it.

**What:**
**Goal:** while a watch is active, the model can answer the watched session's workspace path (the journal frame's workspace, else the record's; empty when neither carries one), for watches entered from a live browser row, a finished browser row and the `--resume` startup watch; leaving the watch clears it.
Part of the fix for `watch-footer-wrong-workspace` (the value is dropped on entry).
**Approach (assumed at the header base):**
- `internal/tui/watch.go`: add `workspace string` to `watchSource` and to `watchState` (plain value, ADR 0011); `watchSourceOf` copies `LiveSession.Workspace`; `enterWatch` stores it; `leaveWatch` resets it with the rest of `watchState`. Add an accessor beside `topLevelWindow`/`topLevelModel` (`internal/tui/model.go`), e.g. `watchedWorkspace() (string, bool)`.
- `internal/tui/sessions.go` `watchFinished`: set the built `LiveSession.Workspace` from `meta.Workspace`.
- Keep `TestSessionViewHoldsEveryFieldTheBoundariesReset` and `TestWatchIsolatesRoutesEveryFoldedMsg` green; extend their field tables if they enumerate `watchState`.
**Regression guard.** A live row whose journal frame will not decode but whose record is listed reaches `watchLive` with `LiveSession.Workspace` "" (`cmd/apogee/wire_session.go` `liveSessionOf`): `watchLive` passes `cmp.Or(live.Workspace, meta.Workspace)` (as it already does for the title), and `TestWatchCarriesTheWatchedWorkspace` covers that case.
**Files:** internal/tui/watch.go; internal/tui/sessions.go; internal/tui/model.go; internal/tui/watch_test.go; internal/tui/sessions_test.go
**Read first:** internal/tui/watch.go — watchState, watchSourceOf, enterWatch, enterStartupWatch; internal/tui/sessions.go — watchLive, watchFinished;
cmd/apogee/wire_session.go — liveSessionOf; internal/tui/watch_test.go — newWatchFixture
**Tests:** `TestWatchCarriesTheWatchedWorkspace` in `watch_test.go` (live row via `liveRowWith`, finished row, startup `Options.Watch`; a live row with an empty frame workspace takes the record's; empty workspace stays empty; cleared after `leaveWatch`). `newWatchFixture` gains a workspace argument if the frame needs one.
**Acceptance:**
- `go build ./...`
- `go test -race -count=1 -run 'TestWatch|TestSessionBrowser|TestSessionsStartupWatch|TestSessionViewHoldsEveryFieldTheBoundariesReset' ./internal/tui/`
**Commit:** `fix(tui): carry the watched session's workspace into the watch`

## 3. Refuse the footer git actions picker while watching

**What:**
**Goal:** while a watch is active, no entry point opens the git actions picker: a footer git-segment click and any other opener show the watch-refused flash; outside a watch the picker opens as before, and a git action landing during a watch still routes to its owner.
Fixes part of `watch-footer-wrong-workspace`: the host's picker opened over the watched footer.
**Approach (assumed at the header base):**
- `internal/tui/gitactions.go` `openGitActions`: when `m.watch.isActive`, return the `watchRefusedFlash` result instead of opening. Gate at this one opener so `footerGitHit` (`internal/tui/mouse.go`) and any keyboard path share it; touch `mouse.go` only if the click bypasses `openGitActions`.
- `acceptPicker`'s existing refusal stays.
**Regression guard.** TestWatchRefusesTheGitActionsPicker must not rely on the host snapshot painting the git segment during a watch (item 5 replaces it): the test drives the refusal through openGitActions' gate via the footer click with a git segment present, and item 5 keeps the test green by giving its watch a known workspace and a watched snapshot. Build it from `gitActionsModelWith` + `footerGitCells` + `leftClick`, the watch entered with a `newWatchFixture` source; the Acceptance pattern names the real tests (`TestGitAction`, `TestClickOnTheFooterGitSegment` — `TestGitActions` matches none).
**Files:** internal/tui/gitactions.go; internal/tui/mouse.go; internal/tui/watch_test.go
**Read first:** internal/tui/gitactions.go — openGitActions; internal/tui/mouse.go — handleFooterGitClick, footerGitHit; internal/tui/watch.go — watchRefusedFlash, refuseWatchInput;
internal/tui/mouse_test.go — TestClickOnTheFooterGitSegmentOpensTheGitActionsPicker, footerGitCells; internal/tui/gitactions_test.go — gitActionsModelWith
**Tests:** `TestWatchRefusesTheGitActionsPicker` in `watch_test.go` (built from `gitActionsModelWith`, `footerGitCells`, `leftClick` and a `newWatchFixture` source) — drive a click on the footer git segment while watching, the git segment present: picker not open, flash shown; after esc the same click opens it. `TestWatchRoutesGitActionLandingsToTheOwner` stays green.
**Acceptance:**
- `go build ./...`
- `go test -race -count=1 -run 'TestWatchRefusesTheGitActionsPicker|TestWatchRoutesGitActionLandingsToTheOwner|TestGitAction|TestClickOnTheFooterGitSegment' ./internal/tui/`
**Commit:** `fix(tui): refuse the git actions picker while watching`

## 4. Retarget the git status chain to the watched workspace

**What:**
Recast at the regression check (2026-11-05).
**Goal:** while a watch with a known workspace is active, git status reads run against the watched workspace and their snapshot is held apart from the host snapshot; no fetch runs during a watch; leaving the watch retires any in-flight watched read and re-reads the host at once.
Part of the fix for `watch-footer-wrong-workspace`. Depends on item 2.
**Approach (assumed at the header base):**
- `internal/tui/gitstatus.go`: a `gitRoot()` accessor answers the watched workspace while a watch is active (`("", false)` when that workspace is empty, per guard (c)), and `m.opts.Workspace` only when no watch is active; `gitStatusCmd` uses it. `gitFetchCmd` keeps the host root, and `gitFetchWanted` is false while watching.
- `enterWatch` / `leaveWatch` (`internal/tui/watch.go`) bump `m.git.gen` and issue a fresh read so a stale read or tick from the other root is retired.
- The watched snapshot lives in `watchState` (e.g. `gitSnap`, `hasGitSnap`), reset with it; `m.git.snap` keeps the host's last read untouched during the watch. The route for gitStatus/Tick msgs in `watch.go` folds a watched read into `watchState`; a route change is a design decision bound here — update the route table guarded by `TestWatchIsolatesRoutesEveryFoldedMsg`.
- No read when the watched workspace is empty.
**Regression guard.** (a) Startup `--resume` watch: enterStartupWatch keeps any git read out of startupWatchRead; Init's own gitStatusCmd, reading gitRoot(), is the watched read, so one chain runs. In-session entry: enterWatch bumps m.git.gen and issues one read. (b) leaveWatch's host re-read is never sequenced before the record load: resumeWatched returns tea.Batch(tea.Sequence(redelivered, opened), gitRead) (or the re-read stays out of leaveWatch's redeliver Cmd); pin with a resume test whose fakeGitHost blocks. (c) While watching with an empty workspace, gitRoot answers ("", false) and gitStatusCmd returns nil. The rule, not a list: every site that sets `git.inFlight = true` (grep it: newModel, refreshGit, the dirty re-issue in foldGitStatus, foldGitStatusTick, plus the new enterWatch/leaveWatch) sets it only when its gitStatusCmd is non-nil; Init is a value receiver and cannot, so enterStartupWatch re-derives inFlight after enterWatch; leaveWatch bumps m.git.gen and issues its read directly, never via refreshGit. (d) Tests driving enterWatch/leaveWatch Cmds with wired git expand the batch (expandBatch) rather than step(t, m, cmd()). (e) Every comment or layout.md sentence describing the status chain's root or the fetch chain's liveness (`grep -n 'fetch chain\|status chain\|git poll' internal/tui/*.go layout.md` — doc.go's git chain paragraph, gitState's and gitFetchWanted's comments, layout.md's `git fetch` every-5-minutes sentence) is updated to name the watch.
**Files:** internal/tui/gitstatus.go; internal/tui/watch.go; internal/tui/sessions.go; internal/tui/model.go; internal/tui/doc.go; layout.md; internal/tui/gitstatus_test.go; internal/tui/watch_test.go
**Read first:** internal/tui/gitstatus.go — gitStatusCmd, refreshGit, foldGitStatusTick; internal/tui/watch.go — enterWatch, enterStartupWatch, leaveWatch;
internal/tui/model.go — newModel (git.gen/inFlight arm); internal/tui/sessions.go — resumeWatched
**Tests:** `fakeGitHost` (`gitstatus_test.go`) records the `root` of each call. `TestWatchReadsGitInTheWatchedWorkspace` — enter watch: next read uses the watched root, `m.git.snap` unchanged; no fetch during watch; leave: next read uses the host root at once and a late watched read is dropped. Unknown workspace: no read during the watch, and `refreshGit` / a tick leave `inFlight` false. Startup `--resume` watch: one status chain, read at the watched root. Startup `--resume` watch of an unknown workspace, then `esc`: `inFlight` was false throughout and the host read issues at once on leaving. `⏎` resume of a finished watch with a blocking `fakeGitHost`: the record load is not held behind the host re-read. Cmds from `enterWatch`/`leaveWatch` are expanded with `expandBatch`.
**Acceptance:**
- `go build ./...`
- `go test -race -count=1 -run 'TestWatchReadsGitInTheWatchedWorkspace|TestGit|TestWatchIsolatesRoutesEveryFoldedMsg' ./internal/tui/`
**Commit:** `fix(tui): read git in the watched workspace while watching`

## 5. Footer names the watched folder and branch

**What:**
**Goal:** while watching, the footer's workdir segment shows a `watching` marker piece followed by the watched session's workspace (display form, escape-stripped), both in the `footer-accent` tone and immune to a `ui.footer.colors` `workdir` override; the git segment shows the watched snapshot; with an unknown workspace only the marker shows and no git segment; on leaving, the host's folder and git segment return unmarked. `layout.md` documents the watch-state footer.
Closes the defect in `watch-footer-wrong-workspace`. Depends on items 2, 3 and 4.
**Approach (assumed at the header base):**
- `internal/tui/model.go` `footerLeftText`: while watching emit the marker as its own segment before the workdir (the path is formatted with `workdirDisplay` + `stripEscapes`, `internal/tui/workspacepath.go`); `footerGitSegment` reads the watched snapshot from `watchState`. `footerToned`: the watch check precedes `footerOverride` (layout.md: users recolour facts, never state).
- `internal/tui/footerfit.go`: give the marker a fit priority number below the workdir's 2 (1, beside the host) so `elidePath` shortens the path first and never touches the marker.
- `layout.md` "While you watch" (~1945): host, model and effort stay the watcher's; workdir and git become the watched session's with the marker; the git segment click is refused while watching (item 3). Add the marker to the colouring paragraph (~121-131) as state.
**Regression guard.** TestWatchRefusesTheGitActionsPicker (item 3) is updated so its watch has a known workspace and a watched git snapshot via fakeGitHost, keeping the git segment hittable; it is named in item 5's Acceptance -run pattern. Also: `footerRow` keeps only the segments the layout names and `ParseFooterRows` accepts only `footerSegmentNames`, so the marker is seated just before `workdir` wherever a row names `workdir` (no row naming `workdir`: no marker) — pinned under a non-default `ui.footer.rows`. `priority()` drops the higher number sooner, so the marker takes a number below workdir's 2 (1, beside the host) and is kept through every drop down to the priority-0 cut. `fakeGitHost` answers per-root snapshots so the watched-then-host branch assertion can bite. The watched display form (`workdirDisplay` + `stripEscapes`) is computed once at `enterWatch` and stored in `watchState`, never per frame (newModel's resolve-once rule for `m.workdir`).
**Files:** internal/tui/model.go; internal/tui/footerfit.go; internal/tui/watch.go; internal/tui/watch_test.go; internal/tui/mode_test.go; internal/tui/gitstatus_test.go; layout.md
**Read first:** internal/tui/model.go — footerLeftText, footerRow, footerToned, footerGitSegment, newModel (m.workdir); internal/tui/footerfit.go — footerSegmentID.priority;
internal/domain/uiprefs.go — footerSegmentNames; internal/tui/watch_test.go — TestWatchLeavesTheFooterUnchanged
**Tests:** rename/extend `TestWatchLeavesTheFooterUnchanged` to `TestWatchFooterNamesTheWatchedWorkspace` — driven entry from a live browser row (`TestSessionBrowserMarksLiveRowsAndWatchesThem` shape), painted footer shows `watching` + the watched path and branch, effort/host unchanged; esc restores the host path and branch. Narrow width: path elided, marker intact. A non-default `ui.footer.rows` layout: marker seated before `workdir`. Unknown workspace: marker only, no git. `fakeGitHost` per-root snapshots tell the watched branch from the host's. `TestWatchRefusesTheGitActionsPicker` lands a watched read first. A `workdir` colour override does not recolour the marker (`m.footerContent` raw ANSI).
**Acceptance:**
- `go build ./...`
- `go test -race -count=1 -run 'TestWatchFooterNamesTheWatchedWorkspace|TestWatchRefusesTheGitActionsPicker|TestFooter|TestWatch' ./internal/tui/`
**Closes:** watch-footer-wrong-workspace
**Commit:** `fix(tui): name the watched folder and branch in the footer`

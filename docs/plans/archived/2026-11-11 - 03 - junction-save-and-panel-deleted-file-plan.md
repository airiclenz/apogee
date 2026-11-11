# Junction config save and deleted-file panel rows — plan

**Goal:** A settings save on a config home reached through a Windows NTFS junction writes through instead of failing with `resolve config`. A file that no longer exists drops out of the side panel's changed-files list.
**Date:** 2026-11-11
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** ee967f90

**Regression check (2026-11-11, ee967f90):**
- 1: guard folded
- 2: guard folded (writer decision)
- 3: guard folded
- 4: guard folded
- 5: recast (writer decision): both report guards folded, supersedes ADR 0110 d4, d10, d13 and glossary side-panel.md:21 through `adr-0110-amend-1111`
- 5 (re-check round): guard folded — Decision-text hits stay, grep takes `from engine Events`, Acceptance gains the `delete_file` and leftover-hit greps

**Sources:**
- `.capcom/issues/evalsymlink-junction-save.md`
- `.capcom/issues/tui-side-panel/panel-deleted-file-entry.md`
- ADR 0110 (`capcom know show "ADR 0110"`), decisions 4, 13, 14
- `docs/layout/side-panel-layout.md` — "Changed-file rows"
- golang/go#82447, golang/go#63703 (EvalSymlinks under a junction)

**Ratified design calls** (owner, 2026-11-11):
- **Junction scope:** fix only `writeConfigAtomically`. Project-root-under-junction paths are split out as issue `junction-project-root`.
- **Deleted rows:** a successful `delete_file` removes the row in `sidepanel.State`, and the TUI also re-stats listed paths off-thread (Turn end + git-status cadence, never at paint) and hides the missing ones. ADR 0110 d4/d13 get a dated amendment.

**Standing requirements:**
- skills: coding-standards
- Bubble Tea v2 names only (`tea.KeyPressMsg`); the Model is copied by value — no no-copy type held by value (ADR 0011); `TestModelFitsTheStack` stays green.
- No fixed deadline on the disk re-check (slow-box rule); durations in tests go through `internal/testtime`.

**Out of scope:**
- `junction-project-root` (prepareProjectFile, insideRoot, adoption key, readProjectLayer).
- Showing terminal-tool writes as changes (ADR 0110 d13 stands for additions).
- A workspace file watcher.

## 1. Write a config through a Windows junction — ✅ DONE (2026-11-11)

NOTES (2026-11-11): on this windows/arm64 box symlinks are unavailable, so TestWriteConfigAtomicallyWritesThroughASymlink, ...RefusesALinkIntoAReadOnlyDirectory and the new ...FallsBackWhenResolveFails skip here (they run on Linux/macOS CI); the two junction tests and ...KeepsTheResolveErrorForAMissingPath pass.
NOTES (2026-11-11): a whole-package `go test ./internal/config/` on this checkout fails 20 tests, 14 identical at HEAD in a fresh worktree; the other 6 (TestSaveServer*, TestSpliceScalarSettingGoldenOps) fail on golden-file comparisons because this working tree holds CRLF copies of eol=lf files (`git ls-files --eol`) — environment, not this item.

**What:** Fixes `evalsymlink-junction-save`: `writeConfigAtomically` refuses a save when `filepath.EvalSymlinks` fails under a junction although the file exists.
**Regression guard.** Name the two unnamed tests `TestWriteConfigAtomicallyKeepsTheResolveErrorForAMissingPath` (configsplice_test.go) and `TestWriteConfigAtomicallySavesASidePanelSettingThroughAJunction` (configsplice_windows_test.go), so the Acceptance `-run` reaches them — no `TestSaveSidePanel*` exists at the base.
The forced-failure tests that swap `evalSymlinks` must not call `t.Parallel`, and restore it with `t.Cleanup`: the package's other tests run in parallel and reach `writeConfigAtomically` through `editHeld`.
**Goal:** `writeConfigAtomically` writes a config whose path passes under a junction: the junction target's file holds the new bytes, its mode is carried over, no temp file is left, and the symlink write-through and read-only-target refusal behave as before.
**Approach (assumed at the header base):** In `internal/config/configsplice.go`, replace the bare `filepath.EvalSymlinks(path)` at the head of `writeConfigAtomically` with one unexported resolver (`resolveWriteTarget`, same file):
- Try `EvalSymlinks` first.
- On error: `os.Lstat(path)`. If that fails too, return the original `resolve config` error unchanged.
- Otherwise follow the final component's own links by hand (`Lstat` + `os.Readlink`, a relative target joined to the link's `filepath.Dir`), at most 40 hops (error past that). Retry `EvalSymlinks` on the result and keep the unresolved result if it still fails.
- A path whose final component is not a link resolves to itself, so the temp file is created beside it under the junction.

Route the resolver call through a package-level `evalSymlinks` func var defaulting to `filepath.EvalSymlinks`, so a portable test can force the failure. Every later step (`linkedFrom`, `CreateTemp`, `Chmod`, `Rename`) is unchanged.
**Files:** internal/config/configsplice.go, internal/config/configsplice_test.go, internal/config/configsplice_windows_test.go
**Read first:** internal/config/configsplice.go — writeConfigAtomically, linkedFrom; internal/config/configedit.go — editHeld; internal/config/configsplice_test.go — symlinkConfigOrSkip, TestWriteConfigAtomicallyWritesThroughASymlink, TestWriteConfigAtomicallyRefusesALinkIntoAReadOnlyDirectory;
internal/config/configedit_test.go — onlyFileIn; internal/config/configwrite_sections.go — SaveSidePanelSetting
**Closes:** evalsymlink-junction-save
**Tests:**
- `configsplice_test.go` (the forced-failure tests do not call `t.Parallel` and restore `evalSymlinks` with `t.Cleanup`):
  - `TestWriteConfigAtomicallyFallsBackWhenResolveFails`: forces `evalSymlinks` to fail on a symlinked config (`symlinkConfigOrSkip`). The write still lands on the target, the link stays a link and nothing else is left in either directory.
  - `TestWriteConfigAtomicallyKeepsTheResolveErrorForAMissingPath`: same forced failure on a missing path: the `resolve config` error is still returned.
- New `configsplice_windows_test.go` (`//go:build windows`):
  - `TestWriteConfigAtomicallyWritesThroughAJunction`: a junction-creating helper (`cmd /c mklink /J`, skip if it fails). The config path passes under the junction. Asserts the target file has the new bytes, the junction is still a junction (Lstat reparse point), and no temp file is left.
  - `TestWriteConfigAtomicallySavesASidePanelSettingThroughAJunction`: drives `SaveSidePanelSetting` with a config home under the junction: no error, and the key is on disk.
**Acceptance:**
- `go build ./internal/config/`
- `go test -count=1 -run 'TestWriteConfigAtomically|TestSaveSidePanel' ./internal/config/`
**Commit:** `fix(config): write a config through a Windows junction when EvalSymlinks fails`

## 2. Fold a delete_file result as a row removal — ✅ DONE (2026-11-11)

**What:** Part of `panel-deleted-file-entry`: today a successful `delete_file` is folded as one more write.
**Regression guard.** test fixtures for the delete_file removal set ToolResultEvent.Tool explicitly (no existing TUI/sidepanel fixture sets it), and item 3's resume test does the same for its live half
**Goal:** In `sidepanel.State`, a successful `delete_file` result removes its path from `ChangedFiles` at every scope. A later write to the same path lists it again, ordered by its first write after the removal, with stats counted only from writes after the removal.
**Approach (assumed at the header base):** In `internal/sidepanel/sidepanel.go` `foldToolResult`, when `e.Tool` equals the delete_file tool name (one unexported constant in `sidepanel`, matching `internal/tools/delete_file.go`'s registered name) and `!e.Result.IsError` and `e.WriteTarget != ""`, record a removal for that path instead of a write. Add an exported `State.RemoveWrite(path)` used by the fold and by item 3. Record the removal as a marker in the append-only `writes` slice (or drop the path's earlier writes) so `Clone` stays a `slices.Clone`. `ChangedFiles` builds its rows from the writes after each path's last removal. The section is live only while at least one row remains.
**Files:** internal/sidepanel/sidepanel.go, internal/sidepanel/sidepanel_test.go
**Read first:** internal/sidepanel/sidepanel.go — foldToolResult, AddWrite, ChangedFiles, Clone; internal/sidepanel/sidepanel_test.go — TestChangedFiles, TestClone_WritesToTheCloneLeaveTheOriginal;
internal/domain/events.go — ToolResultEvent (Tool, WriteTarget); internal/tools/delete_file.go — deleteFileSpec
**Tests:** `TestChangedFiles` gains these cases (every delete fixture sets `ToolResultEvent.Tool` explicitly):
- delete removes the row at the top level and inside its run
- a sub-agent delete removes a top-level write
- a write after the delete re-lists the path with its new stat
- a failed delete leaves the row
- delete-only path: no row, section not live

A `Clone` independence case covers removals.
**Acceptance:**
- `go build ./internal/sidepanel/`
- `go test -count=1 ./internal/sidepanel/`
**Commit:** `fix(sidepanel): drop a changed-file row when delete_file removes the file`

## 3. Replay delete_file as a removal on resume — ✅ DONE (2026-11-11)

NOTES (2026-11-11): the new test `TestSidePanelResumeDropsADeletedFile` uses a real `t.TempDir()` workspace and `filepath.Join` paths instead of the `/w` fixture, so it runs on Windows too; `TestSidePanelResumeRebuildsThroughBothDoors` and `TestSidePanelPaintsEachSection` fail on this Windows box at HEAD as well (the `/w` workspace is not absolute there), unchanged by this item.
NOTES (2026-11-11): a delete card with no earlier write in the record leaves the state nil (no `ensure()`), keeping `rebuildSidePanel`'s "nil where the record holds nothing the panel shows" contract.

**What:** Depends on item 2. Part of `panel-deleted-file-entry`: `rebuildSidePanel` replays a `delete_file` card as a write (`panelWriterTargetArgs`).
**Regression guard.** `panelToolPair` (internal/tui/sidepanel_test.go) sets `Tool: tool` on its `ToolResultEvent` — or a new `panelDelete` helper does — so the test's live half folds the delete; without it item 2's `e.Tool` check never fires and the live panel keeps the deleted row. No TUI code reads `ToolResultEvent.Tool` today, so no existing test moves.
**Goal:** A resumed session's panel shows the same changed-files rows as the live session did, deleted files included: a path whose last file-tool fact was a successful `delete_file` has no row after resume.
**Approach (assumed at the header base):** In `internal/tui/sidepanel.go` `rebuildSidePanel`, a successful `delete_file` card keys its path through `replayedWriteKey` and calls `State.RemoveWrite`. It does not add a write. Live folding (`foldSidePanel`) needs no change beyond item 2, because the Event path already carries `Tool`.
**Files:** internal/tui/sidepanel.go, internal/tui/sidepanel_test.go
**Read first:** internal/tui/sidepanel.go — rebuildSidePanel, replayedWriteKey, panelWriterTargetArgs, panelWriteKey; internal/tui/sidepanel_test.go — TestSidePanelResumeRebuildsThroughBothDoors, assertResumedPanel, panelToolPair;
internal/security/pathsafety.go — EvalRealPath (a missing file keys through its nearest existing ancestor, so write and delete cards agree)
**Tests:** extend `TestSidePanelResumeRebuildsThroughBothDoors` (or add a sibling using `assertResumedPanel`) with a write then delete of one path and a surviving second path; the live half's delete result carries `Tool` (`panelToolPair` sets it, or a `panelDelete` helper). Live and resumed panels agree, and only the surviving path is listed.
**Acceptance:**
- `go build ./internal/tui/`
- `go test -count=1 -run 'TestSidePanelResume' ./internal/tui/`
**Commit:** `fix(tui): replay delete_file as a changed-file removal on resume`

## 4. Hide changed-file rows whose file is gone from disk — ✅ DONE (2026-11-11)

NOTES (2026-11-11): consequential edit — internal/tui/model.go: made necessary by the new panelGoneMsg, which Model.fold must route to foldPanelGone
NOTES (2026-11-11): consequential edit — internal/tui/watch.go: made necessary by the new panelGoneMsg, which TestWatchIsolatesRoutesEveryFoldedMsg requires watchRouteOf to route (owner route, beside panelLingerMsg)
NOTES (2026-11-11): the whole re-check state (gone set, generation, in-flight, dirty) is one sessionView field `panelDisk`, not only the gone set: resetSessionView/resumeLoaded bump the generation, so TestSessionViewHoldsEveryFieldTheBoundariesReset needs it there, and a watch parks it with the owner's view so an owner reply finds the owner's generation. The cadence launch (foldGitStatusTick) is skipped while a watch is active, because the reply routes to the owner's parked view; the owner's Turn ends still re-check. A replacement (resetPanelDisk) also clears in-flight/dirty so the chain is never left waiting on a dropped reply.
NOTES (2026-11-11): a write folded while a re-check is in flight marks it dirty (its stat may predate the write), so the landing re-checks once more; only an fs.ErrNotExist from os.Lstat counts as gone — any other stat error keeps the row.
NOTES (2026-11-11): navmode_test.go was not touched: the nav-stop and click-target assertions live in the new sidepanel_test.go tests (assertPanelFiles). Pre-existing Windows-only failures in the acceptance set (TestSidePanelPaintsEachSection/changed_files, TestSidePanelResumeRebuildsThroughBothDoors — `/w` path separators) fail identically at 168ad6cd.

**What:** Depends on item 2. Closes the rest of `panel-deleted-file-entry`: a file removed by a terminal `rm`, a `move_file` source or `/undo` stays listed.
**Regression guard.** Single-flight the re-check as `refreshGit` is (in-flight + dirty, one follow-up on landing), and bump its generation only where panel state is replaced (`resetSessionView`, `resumeLoaded`, `rebuildSidePanel`), never per launch. `changedFileRows` recomputes `Summary` and `IsLive` from the filtered rows.
A landed write folded in `foldSidePanel` drops its key from the gone set, and `resetSessionView` / `resumeLoaded` clear the set beside `m.panel = nil` (a `sessionView` field, so `TestSessionViewHoldsEveryFieldTheBoundariesReset` stays green).
The test builds its model with `opts.Workspace = t.TempDir()` via `newModel` (as `TestSidePanelResumeRebuildsThroughBothDoors` sets a workspace), so rows are workspace-relative and the join against `ws.realRoot` is what it bites on.
**Goal:** Within one re-check after a listed file disappears from disk, its changed-files row is hidden in the painted panel, in nav-mode stops and in click targets alike. A file that exists again is shown again. No `os.Stat` runs during `View`/paint.
**Approach (assumed at the header base):**
- A TUI-side re-check in `internal/tui/sidepanel.go`. A `tea.Cmd` stats every top-level `ChangedFiles` path off the Update goroutine; workspace-relative keys are resolved against the workspace root `panelWriteKey` used. It returns `panelGoneMsg{gen, gone map[string]bool}`.
- The Cmd is launched beside every git-status read: `refreshGit` cadence and `gitAfterTurn`, but not gated on a git work tree being present.
- A generation counter discards stale replies, and there is no deadline.
- The fold stores the set on `sessionView` (a map, so the Model size does not grow by value).
- One Model method, `changedFileRows(scope)`, returns `m.panel.ChangedFiles(scope)` minus gone paths. `panelSection`, the nav-mode stop builder and the click-target builder all read it, and none call `ChangedFiles` directly.
- The section is not live when every row is hidden.
**Files:** internal/tui/sidepanel.go, internal/tui/gitstatus.go, internal/tui/sessionview.go, internal/tui/commandrun.go, internal/tui/sessions.go, internal/tui/sidepanel_test.go, internal/tui/navmode_test.go
**Read first:** internal/tui/sidepanel.go — panelSection, foldSidePanel; internal/tui/panelstops.go — panelStops, panelBodyStop (stops and click targets read section.files, so only panelSection needs the filter);
internal/tui/gitstatus.go — refreshGit; internal/tui/sessionview.go — sessionView (panel, panelLingerGen); internal/tui/commandrun.go — resetSessionView; internal/tui/sessions.go — resumeLoaded
**Tests:**
- `TestSidePanelHidesAChangedFileGoneFromDisk`: a model built via `newModel` with `opts.Workspace = t.TempDir()`, `panelWrite` of two workspace files, then `os.Remove` one. Before the reply is folded the removed row is still painted, nav-stopped and clickable. Run the re-check Cmd and feed its msg. Painted rows, nav stops and click targets list only the survivor, and the header count reads 1.
- Recreate the file and re-check: it is listed again.
- A stale-generation reply is ignored.
- A launch while a re-check is in flight only marks it dirty; its landing issues one follow-up.
- A write folded after a re-check unhides its path; `resetSessionView` and `resumeLoaded` clear the gone set.
- `TestSessionViewHoldsEveryFieldTheBoundariesReset` stays green.
**Acceptance:**
- `go build ./internal/tui/`
- `go test -count=1 -run 'TestSidePanel|TestPanel|TestNav|TestModelFitsTheStack|TestSessionView' ./internal/tui/`
**Commit:** `fix(tui): hide changed-file rows whose file no longer exists`
**Closes:** panel-deleted-file-entry

## 5. Document deleted-file rows — ✅ DONE (2026-11-11)

NOTES (2026-11-11): the plan's note that docs/manual/configuration.md carries unrelated uncommitted edits did not hold — the tree was clean at dispatch, so the whole file's diff is this item's.
NOTES (2026-11-11): added the optional pointer blockquotes under ADR 0110 decisions 4, 10 and 13 in adr-0110-decision.md; the Decision text itself is unchanged.
NOTES (2026-11-11): on this windows/arm64 box the five Acceptance packages fail many unrelated tests whole-package (the known Windows-cannot-run-full-suite posture); every doc-reading drift test passes (TestManual*/TestDocsEnv* in cmd/apogee, TestShippedDangerousRuleIDsAreDocumented and TestManualStatesTheFloorGuardCount in internal/config, TestManualListsEveryKnownToolName in internal/tools, internal/processing ok), and TestSidePanel* fails only the two pre-existing `/w` cases items 3 and 4 recorded.
NOTES (2026-11-11): retry — restored the `adr-0110-amend-1110` parenthetical on ADR 0110's Status line that the first attempt dropped; the new `adr-0110-amend-1111` clause follows it unchanged.

**What:** Recast at the regression check (2026-11-11). Depends on items 2–4. Owns every prose site that says a deletion counts as a change, that the list only grows, that it keeps first-write order, that the changed-files section goes live once the session has written a file, or that the panel renders from Events alone. Find them with `grep -rn "deletion included\|has written a file\|changed this session\|first-write order\|folds from \*\*Events\|renders from engine Events\|from engine Events" docs/ layout.md .capcom/knowledge/`.
**Regression guard.** Both of the check's guards fold: `adr-0110-amend-1111` amends ADR 0110 d10 as well as d4 and d13, and ADR 0110's Status line names all three; the prose rule and grep cover every sentence giving the changed-files live rule, row order, or that the panel renders from Events alone.
That includes the glossary's "Every section folds from **Events**" line (glossary-identity/side-panel.md:21) and the spec's "One row per path, in first-write order" (side-panel-layout.md:215). Superseded explicitly through the amendment (items 2 and 4): ADR 0110 d4 "Events only" (adr-0110/adr-0110-decision.md:28), d10's "changed files: once the session has written a file" (:65), d13, and the glossary line.
docs/manual/configuration.md carries unrelated uncommitted edits in the working tree (Windows token posture) — the item's commit stages only its own hunks (git add -p), never the whole file.
Grep hits inside adr-0110-decision.md (the headline :7, d4 :28, d10 :65) are Decision text and stay, superseded by the amendment — never rewrite them; pointer blockquotes under d4, d10 and d13 (as d11 has for `adr-0110-amend-1109`, decision.md:73-74) are optional. adr-0110.md:13's summary ("Every section folds from engine Events alone") is reworded or pointed at the amendment.
**Goal:** The spec, manual, glossary and ADR 0110 all say the same thing: a successful `delete_file` removes its row; a listed file found missing on disk is hidden until it exists again; terminal-tool writes are still not shown.
**Approach (assumed at the header base):**
- Edit `docs/layout/side-panel-layout.md` "Changed-file rows" and its sections table row.
- Edit `docs/manual/configuration.md` (the changed-files paragraphs) and the glossary note `side-panel`.
- Add a dated amendment child Note `adr-0110-amend-1111` via `capcom know add --id adr-0110-amend-1111 --parent adr-0110`. It amends d4 (a TUI-side disk re-check filters the Event-folded rows), d10 (changed files is live only while a row remains) and d13 (a deletion removes a row). Name it, with all three decisions, on ADR 0110's Status line and in its reading-order list.
**Files:** docs/layout/side-panel-layout.md, docs/manual/configuration.md, .capcom/knowledge/glossary/glossary-identity/side-panel.md, .capcom/knowledge/decisions/adr-0110.md, .capcom/knowledge/decisions/adr-0110/adr-0110-decision.md, .capcom/knowledge/decisions/adr-0110/adr-0110-amend-1111.md
**Read first:** docs/layout/side-panel-layout.md — "Changed-file rows"; docs/manual/configuration.md — "The side panel" sections table changed-files row, the "Only apogee's own file tools count" paragraph; .capcom/knowledge/glossary/glossary-identity/side-panel.md — Side panel ("Every section folds from **Events**");
.capcom/knowledge/decisions/adr-0110.md — Status line, summary paragraph; .capcom/knowledge/decisions/adr-0110/adr-0110-decision.md — decisions 4, 10, 11 (blockquote precedent), 13; .capcom/knowledge/decisions/adr-0110/adr-0110-amend-1110.md — frontmatter shape for the new amendment
**Tests:** drift tests that pin the public docs keep passing; no drift test pins the changed-files wording, so the Acceptance greps check it.
**Acceptance:**
- `capcom check`
- `capcom know show "ADR 0110"` names the amendment, and its Status line names d4, d10 and d13
- `grep -n "delete_file" docs/layout/side-panel-layout.md docs/manual/configuration.md .capcom/knowledge/glossary/glossary-identity/side-panel.md` hits each file
- the widened grep from **What**, run over this item's **Files**, hits only adr-0110-decision.md lines, a line pointing at `adr-0110-amend-1111`, or a harmless "files changed this session" sentence
- each package from `grep -rlE 'side-panel-layout|configuration\.md' --include=*_test.go .` tested alone: `go test -count=1 ./<pkg>/`
**Commit:** `docs(side-panel): a deleted or missing file drops out of changed files`

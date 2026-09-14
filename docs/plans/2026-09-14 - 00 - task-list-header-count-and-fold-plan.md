# Task-list card: header count and remembered fold

**Goal:** the `task_list` card's header reads `✦ Task List (done/total)` instead of carrying an `N open` stat, the card folds again (collapsed = header only), and its fold state is one shared preference for every task-list card, remembered across sessions in `ui.task-list-open`.

**Date:** 2026-09-14
**Status:** unexecuted
**Sized for:** ~200k-context host

**Sources:**
- `internal/tui/toolregistry.go` (`task_list` entry, `openTasksStat`, `taskListDetail`), `toolblock.go` (header paint, umbrella `(N calls)` count), `toolbranch.go` (`collapsedCall`), `blockstate.go` (`blockHidesWhenCollapsed`), `transcript.go` (`entry.expanded`, `toggleExpanded`), `transcriptbridge.go` (`fromWireToolView`), `settingsapply.go` (`settingsApplyLocal`), `settingswatcher.go`
- `internal/config/registry.go` (`ui.*` rows), `config.go` (`UI` options), `defaults/config.yaml`
- `docs/layout/tool-layout.md` ("Fold states", tool table, task_list notes), `layout.md` ("Collapsed and expanded blocks")
- Commit `2bc0062b` (the always-open card this plan reverses), ADR 0035, ADR 0037, ADR 0041 decision 8, ADR 0072

**Ratified design calls (owner, 2026-09-14):**
- **Header count:** `✦ Task List (done/total)` — done rows over all rows, painted as the label plus the faint `(…)` count tone the umbrella `✦ Tools (7 calls)` uses. The `N open` outcome-slot stat is dropped; the card's slot is blank.
- **Empty or errored result:** plain `✦ Task List`, no count. The card still folds.
- **Collapsed paint:** header plus `▶` only — no task rows, no `+N more lines`. Expanded paint: every row, uncapped, as today.
- **State scope:** ONE shared fold state for every task-list card in the transcript; toggling any of them flips them all.
- **Persistence:** `ui.task-list-open` (bool, default `true`) in `config.yaml`, a registry row like `ui.skill-suggestions` — so it shows in `/settings` and a hand-edit live-applies. A toggle writes it back SILENTLY through the settings seam (no `saved` note); only a failed write warns. A nil settings host toggles in-session and writes nothing.
- **Errored / row-less card:** folds like any targetless block, first rows visible (owner call 2026-09-14, at the regression check).
- **See-less footer:** none on the open task-list card, the header `▼` is the fold (owner call 2026-09-14, at the re-check).

**Regression check (2026-09-14, 4528c8df):**
- 1: guard folded — a replayed record's retained `N open` Summary is discarded where the entry carries a `count` hook (one count per resumed card); the counter is worded over row lines and called from both producers; `TestAlwaysOpenBlockIsNoToggleTarget`'s height count drops the stat row.
- 2: guard folded — header-only collapse needs task rows; an errored / row-less card takes the ordinary targetless collapsed shape (new ratified call); the prose grep is case-insensitive. Supersedes the `[Unreleased]` CHANGELOG.md:13 entry of `2bc0062b`, which item 5 deletes.
- 3: guard folded — Acceptance is `internal/config` only, the `cmd/apogee` gates move to item 4; the `keyAccessors` row is named.
- 4: recast — the fold FACT stays on `entry.expanded`, the option is `TaskListFolded` (zero = open) with one `setExpanded` sweep per toggle / apply; yields to `internal/tui/tui.go:830-835` (a renderer Option's zero value means today's behaviour, polarity flipped at the composition root).
- 5: guard folded — the `2bc0062b` CHANGELOG bullet is deleted as an explicit edit (the closeout only inserts sidecar entries); the `tool-layout.md:363-366` note is reworded and the guard grep widened.
- 2 (re-check): decision applied — the open task-list card carries no `see less…` footer; `renderToolBranch`'s expanded paint for a `collapsesToHeader` view skips `seeLessFooter`, the exact-lines test expects header + rows only (new ratified call: **See-less footer**).
- 4 (re-check): guard folded — the silent `ui.task-list-open` write yields to ADR 0035:175-178 ("nothing may join the authorized-write set without amending this record"): the record is amended, not superseded — item 5 carries the Consequences addendum and the CONTEXT.md:1013-1016 sentence; the e2e row is spelled `✦ Task List (1/3) ▶` per the shipped `tasklist.yaml` fixture (no new fixture).
- 5 (re-check): decision applied — a dated Consequences addendum to ADR 0035 and the matching CONTEXT.md sentence admit the transcript's task-list fold gesture as the third silent program-written key; both paths join Files.

**Standing requirements:**
- `skills: coding-standards`
- Deviations from item text land as a dated NOTES line under the item.
- Bubble Tea v2 (`charm.land/bubbletea/v2`): `tea.KeyPressMsg`, `msg.String()`. The `Model` is value-copied on every `Update`: no `strings.Builder` by value, slices rebuilt rather than appended in place.

**Out of scope:**
- Per-card fold memory, any change to the task_list tool, its wire text or the engine block (ADR 0072).
- Fold memory for any other tool card; a general per-kind fold default.
- The `/settings` pane's own rendering of the new row (it lists every registry row already).

## 1. Header count replaces the `N open` stat — ✅ DONE (2026-09-14)

NOTES (2026-09-14): the `count` hook is `func(lines []string) (string, bool)` (the regression guard's `taskListCount(lines)` wording), not the `func(domain.ToolResult)` the What bullet spells — the wire keeps no result content, so one counter serves both producers; the IsError decline is enrichWithResult's early return, and a replayed error record carries no marker row.
NOTES (2026-09-14): `internal/tui/subagentblock.go` untouched — a grouped member's row (`renderGroupMember`) is a leaderRow with no label on it, and a task_list call is targetless so it never groups (`groupable`); the count is painted at the one site that paints the label (`renderToolBlock`).
NOTES (2026-09-14): the replay discard is narrowed to the record's own WORDING of the slot — a `failed` verdict (`error`) and a `quoted` promoted line are kept, since the live card keeps both too (absorbFailure; outputDetail's one-liner survives the blank stat); pinned by `TestToolRegistryTaskListReplayKeepsAVerdictAndAPromotedLine`.

**What:** The task-list card's header carries `(done/total)`; its outcome slot goes blank. In `internal/tui`:
- `toolView` gains a display field for a header count (a string such as `1/3`), escape-stripped by `sanitize` like every other display field and NOT on the wire form: it is re-derived from the retained result on replay, exactly as `alwaysOpen` is today (`transcriptbridge.go` `fromWireToolView`).
- `toolPresenter` gains a `count` hook `func(domain.ToolResult) (string, bool)`; the `task_list` entry sets it to a `taskListCount` that counts `taskListDoneMarker` rows over `taskListDoneMarker`+`taskListOpenMarker` rows of `res.Content` and declines (`false`) on `res.IsError` or zero rows. Its `stat` becomes `blankStat`; `openTasksStat` is deleted.
- `renderToolBlock` (`toolblock.go`) paints a non-empty count as `th.toolLabel.Render(label) + " " + th.toolIndicator.Render("("+count+")")`, the umbrella's own shape (`superGroupLabel`/`groupCountFormat` site), BEFORE any `▶`/`▼` the targetless shape appends; a grouped member (`renderGroupMember`) paints it the same way. A call still in flight (no result) shows no count.
- Rows are counted from the result the model read, not from `internal/tasklist` state — the card is a render of a retained result (ADR 0031: nothing crosses the wire for presentation).

**Regression guard.** In `fromWireToolView` a record whose registry entry carries a `count` hook DISCARDS the record's retained Summary text (the `N open` a session saved before this change replays with) — the header count supersedes it, so a resumed card shows one count, never two; add a replay test with a record carrying Summary `2 open` that paints `✦ Task List (1/3)` and no `2 open` row. The wire keeps no result content (`session.ToolView`, `internal/session/transcript.go:160-174`; `transcriptbridge_test.go` pins its members), so the counter is worded over row LINES — `taskListCount(lines []string)` over the `taskListDoneMarker`/`taskListOpenMarker` prefixes — and called from both producers: `enrichWithResult` on `splitLines(res.Content)`, `fromWireToolView` on the decoded `w.Details` texts (task_list's body is exactly its rows, `taskListDetail`). `TestAlwaysOpenBlockIsNoToggleTarget` (`blocktarget_test.go`) counts the card `2 + rows` tall — header, rows, the stat row — and goes red at this item's commit: drop the stat row from that count (`1 + rows`, the "the stat" wording gone); item 2 renames the test.

**Files:** `internal/tui/toolregistry.go`, `internal/tui/toolview.go`, `internal/tui/toolblock.go`, `internal/tui/subagentblock.go`, `internal/tui/transcriptbridge.go`, `internal/tui/toolregistry_test.go`, `internal/tui/toolpresent_test.go`, `internal/tui/blocktarget_test.go`

**Tests:**
- `TestToolRegistryPresentsTheTaskListCall`: paint is exactly `✦ Task List (1/3)` / `┝ [✔] read the plan` / `┝ [ ] write the code` / `┕ [ ] run the tests` — no `2 open` row.
- `TestToolRegistryTaskListStatCountsOpenRows` → `TestToolRegistryTaskListCountsDoneOverTotal`: `1/3`, `3/3`, `0/2`; declines on error and on a fence-only result (header is plain `✦ Task List`).
- `TestToolRegistryTaskListReplaysAlwaysOpen`: replayed paint byte-equal to live, count included (the count re-derived from the decoded `Details` rows).
- Replay test: a record carrying Summary `2 open` (a session saved before this change) paints `✦ Task List (1/3)` and no `2 open` row.
- `TestAlwaysOpenBlockIsNoToggleTarget`: its height count drops the stat row (`1 + rows`).
- Sanitize test: a count containing an escape sequence is stripped.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'TaskList|Sanitize|ToolRegistry|AlwaysOpen'`

**Commit:** `feat(tui): task_list header counts done over total; the open-count stat goes`

## 2. The task-list card folds again, collapsed to its header — ✅ DONE (2026-09-14)

NOTES (2026-09-14): `internal/tui/toolblock.go` is on the item's Files list but needed no edit — the targetless header already hangs `▶`/`▼` and marks the block under `blockHidesWhenCollapsed`, which now answers true through `collapsedCall`.
NOTES (2026-09-14): the 40-column half of `TestTaskListBlockCollapsesToHeader` asserts the open paint carries all three task rows (each wrapping whole under its marker, as `renderBranchList` has always painted the expanded targetless shape) rather than the old clip-per-row check, which was the collapsed paint's; the exact header+rows line count is pinned at 80 columns.
NOTES (2026-09-14): `TestToolRegistryTaskListReplaysAlwaysOpen` renamed to `TestToolRegistryTaskListReplaysCollapsesToHeader` with the identifier it names; `hasTaskRows` (beside the row markers in toolregistry.go) is the row-less precondition `collapsedCall` asks.

**What:** Reverse the always-open shape of `2bc0062b` deliberately (ratified): rename `alwaysOpen` to `collapsesToHeader` on `toolPresenter` and `toolView` (both producers: `presentToolCall`, `fromWireToolView`), keep `task_list` as its one user, and give it the shape the calls fix:
- `collapsedCall` returns NO lines for a `collapsesToHeader` view that has any branch line, with `truncated = true` and an EMPTY remainder (no `+N more lines`); a view with no lines at all returns `truncated = false` (nothing to reveal, no indicator).
- `blockHidesWhenCollapsed` therefore answers true whenever the card has rows; the width arm stays skipped for this shape (every row is painted when open, clipped rows say their own `…`).
- The header wears `▶`/`▼` and is the click surface, as every other targetless block (`toggle = targetHeader`); `renderToolBranch`'s expanded paint stays every row, uncapped — and for a `collapsesToHeader` view it skips `seeLessFooter` (`toolbranch.go:66,85`): the open card carries NO `see less…` footer, the header `▼` and a click anywhere on the block are its fold.
- Depends on item 1 (the collapsed header must already carry the count).
- Prose guard rule: every comment in `internal/tui` naming the always-open block or its "one state"/"no toggle" is reworded (`grep -rni 'always-open\|alwaysOpen\|always open\|one state\|no toggle' internal/tui`).

**Regression guard.** Header-only collapse applies only to a `collapsesToHeader` view that has task rows (lines prefixed `taskListOpenMarker`/`taskListDoneMarker`); a row-less view — an errored call (its red `error` line and message), a fence-only result — takes the ORDINARY targetless collapsed shape (`collapseAtCap` at `collapsedBodyCap`), so the failure verdict is never folded away (owner call 2026-09-14, "Ratified design calls": **Errored / row-less card:** folds like any targetless block, first rows visible); add a test that an errored task_list card collapsed still shows its error line. The prose grep is case-insensitive (`-rni`): the two comments spelled `ALWAYS-OPEN` (`toolbranch.go:239`, `blockstate.go:136`) match only through the `alwaysOpen` identifier beside them, which this item renames; `one state` also hits unrelated files (`reportpane.go`, `settings.go`, `doc.go`, `model.go`), which the rule's "naming the always-open block" already excludes. This item supersedes the `[Unreleased]` CHANGELOG.md:13 entry of `2bc0062b` (the always-open, no-▶/▼, click-selects card); item 5 deletes that bullet so the new entry does not sit beside it. Re-check (2026-09-14): the open task-list card carries NO `see less…` footer — the header `▼` and a click anywhere on the block are its fold; `renderToolBranch`'s expanded paint for a `collapsesToHeader` view skips `seeLessFooter`, and the exact-lines test expects header + rows only (owner call 2026-09-14; "Ratified design calls": **See-less footer:** none on the open task-list card, the header `▼` is the fold).

**Files:** `internal/tui/toolregistry.go`, `internal/tui/toolview.go`, `internal/tui/toolbranch.go`, `internal/tui/blockstate.go`, `internal/tui/toolblock.go`, `internal/tui/transcriptbridge.go`, `internal/tui/blocktarget_test.go`, `internal/tui/toolregistry_test.go`, `internal/tui/toolblock_test.go`

**Tests:**
- `TestAlwaysOpenBlockIsNoToggleTarget` → `TestTaskListBlockCollapsesToHeader`: collapsed paint at 80 and 40 cols is exactly one row `✦ Task List (1/3) ▶`; expanded paint is exactly header `✦ Task List (1/3) ▼` plus every row and NO `seeLessFooterLine` (header + rows only); the block is a toggle target; a click on the header and `enter` at the block cursor both flip it.
- A fence-only (row-less) result: no indicator, not a toggle target.
- An errored task_list call (`IsError`): the collapsed card is the ordinary targetless shape — header `▶` plus the first `collapsedBodyCap` rows — and its red `error` line is visible collapsed.
- `TestTargetlessBlockStillCapsAndToggles`: `git_status` is still capped at `collapsedBodyCap` (the new shape is task_list's alone).
- Replay test: a resumed card decodes `collapsesToHeader == true` off the retained name.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'TaskList|Targetless|BlockTarget|Replay'`

**Commit:** `feat(tui): the task_list card folds again, collapsing to its counted header`

## 3. `ui.task-list-open` config key — ✅ DONE (2026-09-14)

NOTES (2026-09-14): `internal/config/defaults_test.go` (named in Files) needed no edit — the active `task-list-open: true` template line passes the template gate and the ships-active comparison unchanged, since it spells the default.
NOTES (2026-09-14): the ten spelled-out `UISettings` literals in `config_test.go` gained `TaskListOpen: true` (they pin the shipped defaults by hand, so the new field had to be stated); the manual's "Four more keys live under `ui:`" count became "Five" with the new paragraph in that run.
NOTES (2026-09-14): `cmd/apogee` `TestSettingsRowsFormatEffectiveValues` now fails (57 values pinned for 58 keys) — item 4's gate per the plan's regression guard, not run here.

**What:** Add the key that item 4 reads and writes, exactly as `ui.skill-suggestions` is built:
- `internal/config/config.go`: `UI.TaskListOpen bool` (default `true`) and the user-file `*bool` with `yaml:"task-list-open"`, merged like `SkillSuggestions`.
- `internal/config/registry.go`: row `Path: "ui.task-list-open", Kind: KindBool, Default: "true", Editable: true`, `Desc: "Start with the task-list cards in the transcript open; a click on one folds them all and records the choice here."`, `Read` off `o.UI.TaskListOpen`.
- `internal/config/defaults/config.yaml`: `  task-list-open: true   # false = task-list cards start folded; toggling a card writes this back` under `ui:`.
- `docs/manual/configuration.md`: a paragraph beside `ui.show-scrollbar` stating the key, its default, that a toggle in the transcript writes it, and that it shows in `/settings`.

**Regression guard.** Acceptance names `go build ./... && go test ./internal/config/` only — the `cmd/apogee` gates (`docs_settings_test`, `settingsrows_test`, `wire_settings_test`) move to item 4's Acceptance, since the Editable row needs item 4's renderer apply to pass them. `TestKeyAccessorsBindDescribedKeys` (`internal/config/config_test.go`) gates the `keyAccessors` table in `config.go`: the key needs its row `{row: mustKey("ui.task-list-open"), fromFile: fileUI}` beside `ui.skill-suggestions` — four sites in `config.go`, not three.

**Files:** `internal/config/config.go`, `internal/config/registry.go`, `internal/config/defaults/config.yaml`, `internal/config/registry_test.go`, `internal/config/config_test.go`, `internal/config/defaults_test.go`, `docs/manual/configuration.md`

**Tests:**
- `registry_test.go`: the row exists, is a bool, editable, default `true`, and reads the option.
- `config_test.go`: a user file with `ui: {task-list-open: false}` resolves `UI.TaskListOpen == false`; an absent key resolves `true`.
- `defaults_test.go` (template gated against the registry) and `TestKeyAccessorsBindDescribedKeys` pass unchanged in shape; `cmd/apogee/docs_settings_test.go` (manual documents every key), `settingsrows_test.go` and `wire_settings_test.go` are item 4's gates.

**Acceptance:** `go build ./... && go test ./internal/config/`

**Commit:** `feat(config): ui.task-list-open records whether task-list cards start open`

## 4. One shared fold state, written back on toggle — ✅ DONE (2026-09-14)

NOTES (2026-09-14): the seed lives on the transcript as `taskListOpen` (zero = folded, the ordinary block default a hand-built test transcript gets), mirrored from `Options.TaskListFolded` at `newModel` and moved only through `Model.setTaskListFolded` → `transcript.setTaskListOpen` (the one sweep) — `addToolCall` and `replay` are reached through `apply`/`decodeTranscript` with no Model in sight, the same reason `transcript.ws` is a mirror; the option itself is `TaskListFolded` with the zero value open, as the item pins.
NOTES (2026-09-14): `internal/tui/blockcursor.go`, `toolblock.go`, `subagentblock.go`, `render.go` and `paintcache.go` are on the item's Files list but needed no edit — ⏎ reaches the shared toggle through `toggleBlockAt`, and the paint cache already keys on each entry's `expanded` via `spanFlags`, so the sweep moves every card's key by itself.
NOTES (2026-09-14): `modelWithTaskListBlock` (blocktarget_test.go) now takes the `Options` the model is built from, and its existing click/⏎ subtest asserts the card starts OPEN — the default preference this item seeds — where it asserted the collapsed default item 2 left; `addTaskListCard`, `taskListEntries`, `headerLineOf` and `taskListHeaders` are the helpers the new shared-fold tests read the model and its painted frame with.
NOTES (2026-09-14): the errored / row-less task-list card carries the `collapsesToHeader` mark too, so where it is a toggle target (more lines than the targetless cap) its click flips the SHARED fold and writes the key, per the item's "a toggle on such an entry" — its own paint stays the ordinary targetless shape item 2 ratified.
NOTES (2026-09-14): consequential edit — docs/manual/sessions.md: made necessary by the resume seeding ("A resumed session opens … with everything folded shut" now has the task-list card as its one exception).

**What:** Recast at the regression check (2026-09-14). Depends on items 2 and 3. The task-list cards' fold is one shared preference, `m.opts.TaskListFolded`, whose FACT stays on each entry's `expanded` (the paint cache keys on it via `spanFlags`; painters read `paintInput.expanded`, never `m.opts`):
- `tui.Options` gains `TaskListFolded bool` (zero value = open = today's behaviour); `cmd/apogee/wire_options.go` sets it from `!opts.UI.TaskListOpen`, inverted exactly as `HideScrollbar` is.
- A NEW `collapsesToHeader` entry — appended live or decoded on replay — is seeded `expanded = !m.opts.TaskListFolded` at the point the Model adds it; single block and grouped member alike paint off `entry.expanded`, as every block does.
- A toggle on such an entry — mouse (`toggleBlockAt`), `enter` at the block cursor (`toggleAtBlockCursor`) — flips `m.opts.TaskListFolded`, performs ONE sweep calling `setExpanded` on every `collapsesToHeader` entry in the transcript (so every card's paint key moves), re-lays the frame, and persists it through the existing settings seam: `SettingsHost.Write("ui.task-list-open", "true"|"false")` followed by `recordSettingEdit`, with NO note on success; a write error lands one transcript warning (`task-list-open: not saved: <err>`), the session keeps the flipped state. A nil `SettingsHost` flips and writes nothing (the Driver degrade, ADR 0031). The binary's baseline refresh on a seam write (ADR 0041 decision 8) keeps the watcher from re-reporting it.
- `settingsApplyLocal` gains `case settingKeyTaskListOpen` (`"ui.task-list-open"`) setting `m.opts.TaskListFolded = v != settingTrue` and running the same sweep — this is what makes a `/settings` edit and a hand-edited file apply live, and a resumed session paint every task-list card per the file.
- The `/settings` row mirrors the toggle through the journal entry `recordSettingEdit` records (the ` *` marker, `settingsEditedValueCell`) — `Rows()` reads the launch snapshot, so that call is load-bearing.

**Regression guard.** The fold FACT stays on `entry.expanded` (the paint cache keys on it via `spanFlags`, and painters read `paintInput.expanded`, never `m.opts`): (a) the option is `tui.Options.TaskListFolded bool` (zero value = open = today's behaviour, per internal/tui/tui.go:830-835), inverted at `cmd/apogee/wire_options.go` from `opts.UI.TaskListOpen` exactly as `HideScrollbar` is; (b) a toggle on a `collapsesToHeader` entry flips `m.opts.TaskListFolded` and performs ONE sweep calling `setExpanded` on every `collapsesToHeader` entry in the transcript (so every card's paint key moves), then the seam write; (c) a NEW such entry — appended live or decoded on replay — is seeded `expanded = !m.opts.TaskListFolded` at the point the Model adds it; (d) `settingsApplyLocal("ui.task-list-open", v)` sets `m.opts.TaskListFolded = v != settingTrue` and runs the same sweep; (e) the `/settings` row mirrors the toggle through the journal entry `recordSettingEdit` records (the ` *` marker, `settingsEditedValueCell`) — `Rows()` reads the launch snapshot, so that call is load-bearing, and the item's "binary re-reads the file" sentence is deleted; (f) `**Files:**` adds `internal/tui/render.go`, `internal/tui/paintcache.go`, `internal/tui/transcript.go`, `cmd/apogee/settingsrows_test.go`; tests: the two-card test asserts BOTH entries' `expanded` flipped and the paint through `newModel` (cached path) changes on the next frame. Yields to `internal/tui/tui.go:830-835` (a renderer Option's zero value means today's behaviour, polarity flipped at the composition root) — hence `TaskListFolded`, not `TaskListOpen`. The `cmd/apogee` gates moved here from item 3: `"ui.task-list-open"` joins `settingKeysAppliedByTheRenderer` (`wire_settings_test.go`) and `TestSettingsRowsFormatEffectiveValues` (`settingsrows_test.go`) pins its value. Re-check (2026-09-14): the silent card-click write of `ui.task-list-open` yields to ADR 0035:175-178 (the authorized-write set is "editable scalars edited in the settings surface", each act naming the file and entry, and nothing joins it "without amending this record"; CONTEXT.md:1013-1016 restates it) — the record is AMENDED, not superseded: item 5 carries the ADR 0035 Consequences addendum and the CONTEXT.md sentence admitting the task-list fold gesture as a member that writes `ui.task-list-open` silently (ratified 2026-09-14); the write itself stays as designed here, and ADR 0041 decision 8 is not the amendment. The e2e row is spelled `✦ Task List (1/3) ▶`: the one task-list script the harness ships, `cmd/apogee/testdata/stubllm/tasklist.yaml:31`, carries one done task of three (`taskListDoneRow`, `e2e_tasklist_test.go:40`), and no new fixture is added.

**Files:** `internal/tui/tui.go`, `internal/tui/model.go`, `internal/tui/mouse.go`, `internal/tui/blockcursor.go`, `internal/tui/toolblock.go`, `internal/tui/subagentblock.go`, `internal/tui/settingsapply.go`, `internal/tui/render.go`, `internal/tui/paintcache.go`, `internal/tui/transcript.go`, `cmd/apogee/wire_options.go`, `internal/tui/blocktarget_test.go`, `internal/tui/settings_test.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/settingsrows_test.go`, `cmd/apogee/e2e_tasklist_test.go`

**Tests:**
- `internal/tui`: two task-list cards in one transcript built through `newModel` (the cached paint path); a click on the second collapses both — BOTH entries' `expanded` flipped and the paint changes on the next frame; `enter` on the first opens both; the fake `SettingsHost` records exactly one `Write("ui.task-list-open","false")` then one `("…","true")`; no note is added; a failing host adds the warning and the flip stands; a nil host flips with no write. A card appended after the flip is seeded from `TaskListFolded`.
- `settings_test.go`: `settingsApplyLocal("ui.task-list-open","false")` sets `TaskListFolded` and the sweep collapses every card on the next frame; `"true"` reopens; the `/settings` row shows the toggled value with the ` *` marker.
- `wire_settings_test.go`: `TaskListFolded` wired inverted from the resolved `UI.TaskListOpen`; `TestEveryEditableSettingKeyHasAnApply` passes with the key in `settingKeysAppliedByTheRenderer`.
- `cmd/apogee/settingsrows_test.go`: `TestSettingsRowsFormatEffectiveValues` pins `"ui.task-list-open"`; `docs_settings_test.go` passes.
- `cmd/apogee/e2e_tasklist_test.go`: with `ui: {task-list-open: false}` in the test config home, the card's first paint is the one-row `✦ Task List (1/3) ▶` (the shipped `tasklist.yaml` list: one done task of three); with the default it is open. A toggle followed by a resume paints the card in the toggled state and the config file carries `task-list-open: false`.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'TaskList|Settings|BlockTarget' && go test ./cmd/apogee/ -run 'TaskList|WireSettings|Docs|Settings'`

**Commit:** `feat(tui): task-list cards share one fold state, remembered in ui.task-list-open`

## 5. Layout specs and the changelog trail

**What:** Depends on item 4. Reword every spec sentence that records the always-open card:
- `docs/layout/tool-layout.md`: "Fold states" (the single-state exception becomes: the task-list block collapses to its counted header, `▶`/`▼` on the header, click and `enter` toggle every task-list block together, state remembered in `ui.task-list-open`); the tool table row (`task_list | Task list (done/total) | — | — | the list, one row per task; collapsed = header only`); the task_list notes.
- `layout.md` "Collapsed and expanded blocks": the "one exception is the always-open block" paragraph is replaced by the header-only collapsed shape and the shared, remembered state.
- Guard rule: every sentence in `docs/layout/`, `layout.md`, `docs/manual/` and `CONTEXT.md` naming the always-open card or its `N open` stat (`grep -rin 'always.open\|open rows\|[0-9n] open\|no toggle' docs/layout layout.md docs/manual CONTEXT.md`).
- CHANGELOG: delete the `[Unreleased]` bullet beginning "The `task_list` block in the transcript is always open" (from `2bc0062b`) as an explicit edit of `CHANGELOG.md`; the sidecar carries only this plan's new entry.
- ADR 0035 + CONTEXT.md: a dated Consequences addendum to `docs/adr/0035-the-settings-surface-persists-one-key-per-deliberate-edit.md` admitting the transcript's task-list fold gesture as the third silent program-written key (`ui.task-list-open`, beside `server:` per ADR 0036 D2 and `launch-profile:` under `remember-model:`), and the matching sentence in `CONTEXT.md` (the Settings-surface passage at CONTEXT.md:1013-1016).

**Regression guard.** The closeout only INSERTS sidecar entries under `[Unreleased]` and the implementer never edits `CHANGELOG.md` on its own, so a "replaced" entry cannot happen through the sidecar: `CHANGELOG.md` is in this item's Files and the `2bc0062b` bullet (CHANGELOG.md:13) is deleted by hand, the new entry landing through the sidecar. The guard grep is `grep -rin 'always.open\|open rows\|[0-9n] open\|no toggle'` over the same paths: the 2026-09-03 note at `docs/layout/tool-layout.md:363-366` ("its slot counts the OPEN rows … rather than reading `0 open`") escapes a literal `N open`, and is reworded to the done/total header count. Re-check (2026-09-14): item 5 gains a dated Consequences addendum to `docs/adr/0035-the-settings-surface-persists-one-key-per-deliberate-edit.md` admitting the transcript's task-list fold gesture as the third silent program-written key (`ui.task-list-open`, beside `server:` per ADR 0036 D2 and `launch-profile:` under `remember-model:`) and the matching `CONTEXT.md` sentence (the one near CONTEXT.md:1013-1016); both paths join this item's `**Files:**`; the e2e row in item 4 is spelled `✦ Task List (1/3) ▶` per the existing `tasklist.yaml` fixture (no new fixture).

**Files:** `docs/layout/tool-layout.md`, `layout.md`, `docs/manual/configuration.md`, `CONTEXT.md`, `CHANGELOG.md`, `docs/adr/0035-the-settings-surface-persists-one-key-per-deliberate-edit.md`

**Tests:** none (docs only); `cmd/apogee/docs_settings_test.go` and `readme_test.go` still pass.

**Acceptance:** `grep -rin 'always.open\|open rows\|[0-9n] open' docs/layout layout.md docs/manual CONTEXT.md | grep -i task; grep -n 'always open' CHANGELOG.md | grep -i task_list; echo "(expect no output)"; grep -c 'ui.task-list-open' docs/adr/0035-the-settings-surface-persists-one-key-per-deliberate-edit.md CONTEXT.md; echo "(expect a non-zero count for each)"; go test ./cmd/apogee/ -run 'Docs|Readme'`

**Commit:** `docs(layout): task-list card collapses to its counted header; fold remembered`

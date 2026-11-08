# Side panel drag resize — plan

- **Goal:** let the user resize the Side panel by dragging its separator with the mouse, as
  `capcom ui` does: live re-wrap, clamped limits, persisted as `ui.side-panel.width`, double-click
  and `/panel width <n>|reset` to reset or set it.
- **Date:** 2026-11-08
- **Status:** unexecuted
- **sized for:** ~200k-context host
- **base:** b66d5000
- **Sources:** issue `side-panel-resize`; ADR 0110 (`capcom know show "ADR 0110" --with-children`);
  ADR 0106 (Decision 5 covers only button-less motion — not amended by this plan); the
  motion-never-repaints rule (`internal/tui/model.go` settle/settleFrame, `internal/tui/doc.go`,
  `TestMouseMotionNeverRepaints`, plan `docs/plans/archived/2026-09-16 - 00 - host-config-and-tui-deepening-plan.md`
  item 18), whose one exception this plan records in `adr-0110-amend-1108`; ADR 0030; ADR 0011; ADR 0035/0037; `docs/layout/side-panel-layout.md`;
  `layout.md` §"The side panel"; glossary Note `side-panel`; reference behaviour
  `../capcom/internal/ui/mouse.go` and `../capcom/internal/ui/layout.go` (divider drag).
- **Closes:** side-panel-resize
- **Ratified design calls** (owner, 2026-11-08):
  - **Drag feel:** live reflow — panel and transcript re-wrap on every column moved, like `capcom ui`.
    A motion during an active separator drag is the one exception to "mouse motion never repaints",
    recorded in `adr-0110-amend-1108` (not an ADR 0106 amendment).
  - **Limits:** panel ≥ 24 cols; panel ≤ window − transcript floor (100 `auto`, 80 `on`) − scroll-bar
    column; the show/hide gate (137 / 117, computed with the default 36) is unchanged.
  - **Persist:** global key `ui.side-panel.width`, columns, default 36, written once on release when
    the width changed; editable in `/settings`; clamped at layout time, never rewritten by a clamp.
  - **Extras:** double-click on the separator resets to 36; `/panel width <n>|reset`; separator drawn
    in the accent tone, bold, while a drag is active.
  - **Hit zone:** the separator column and the gutter column right of it, rows above the slot bottom.
  - **Watch mode:** the separator drag and `/panel width` are inert there, as `/panel` already is.
  - **Hover shape:** unchanged (no resize cursor; ADR 0106 allows only default|text|pointer).
- **Standing requirements:**
  - skills: coding-standards
  - Tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run '<TestX>' ./<one package>/`; never a
    package pattern, never two runs at once.
  - A new file in `internal/tui`, `internal/config` or `internal/domain` is named in its `doc.go`.
  - The Model stays under `TestModelFitsTheStack`; drag state is a small plain struct (ADR 0011).
- **Out of scope:** a key binding; a resize hover shape; ratio/proportional width; vertical resize;
  a drag that starts elsewhere; scroll-bar dragging; daemon/headless rendering.

**Regression check (2026-11-08, b66d5000):**
- 1: recast — the ADR 0106 amendment is dropped; the live-drag repaint exception goes in
  `adr-0110-amend-1108`; guard folded (every fixed-width sentence in ADR 0110 and the glossary Note);
  second pass: recast — Goal and Approach rewritten to match the guard (no ADR 0106 Note or edit).
- 2: recast — a zero width means unset (36); `domain.ParseSidePanelWidth`; registry path and test files corrected;
  second pass: guard folded (`wantUIDefault` + default-resolution -run patterns; commented `# width: 36` between
  `show:` and `# sections:`, registry row between them; `uiStallAfter`-shaped descriptor in `keyfield.go`;
  writer test file and `sidepanelprefs.go` in Files); `cmd/apogee/wire_options.go` needs no change.
- 3: guard folded (0 resolves to 36 through one method; panelRow pad floored; frameKey width 0 while hidden; comments).
- 4: guard folded (relayout licensed by model.go or a `// geometry:` comment; drag arm after the SGR-35
  reroute; press-time effective width; motion-never-repaints prose names the drag exception).
- 5: guard folded (a new injectable Model clock; no existing one to reuse).
- 6: guard folded (parse in parsePanel via `domain.ParseSidePanelWidth`; reset via `Settings.Reset`; `panelHelpWords` third group).
- 7: guard folded (`internal/tuitest/driver_test.go`, `msgLog.mouse()` keeps motion; `sgrMouse` takes a button).
- 8: guard folded (rule + grep incl. `config.yaml`; owns `/panel width` in §Commands and "From the prompt.").

## 1. Amend ADR 0110 and ADR 0106, update the glossary term — ✅ DONE (2026-11-08)

NOTES (2026-11-08): ADR 0110's superseded original sentences (summary :8,:12, decision headline, D6, D7, consequences re-key bullet, the Rejected "Proportional or user-set width" entry) are left verbatim and named in adr-0110-amend-1108's "What this supersedes" paragraph, per the house rule that a record is corrected by amendment, not by rewriting its Decision text; adr-0110.md gained only the Status-line and reading-order mentions.
NOTES (2026-11-08): no ADR 0106 Note or edit, per the plan's recast item; the issue side-panel-resize's Acceptance line still says "ADR 0110 and 0106 amended" — the amendment records why 0106 needs none (its Decision 5 covers button-less motion only).

**What:**
Recast at the regression check (2026-11-08).
**Goal:** the decision records and the `side-panel` glossary Note allow a user-set panel width
changed by a separator drag, with the limits, persistence and the live-drag repaint exception — all
recorded in ADR 0110's amendment; ADR 0106 gets no Note and no edit.
**Approach (assumed at the header base):** add child Note `adr-0110-amend-1108`
(`capcom know add --id adr-0110-amend-1108 --parent adr-0110`) superseding Decision 6 ("width
changes only on a gate crossing or toggle"), Decision 7 (fixed 36) and the Rejected entry
"Proportional or user-set width" with this plan's ratified calls, and recording the one exception to
"mouse motion never repaints" — a motion while a separator drag is active relayouts — named against
the rule's home: `internal/tui/model.go` settle/settleFrame, `internal/tui/doc.go`,
`TestMouseMotionNeverRepaints`, plan `docs/plans/archived/2026-09-16 - 00 - host-config-and-tui-deepening-plan.md`
item 18. Name the amendment on ADR 0110's Status line and in its reading-order list. Add no
`adr-0106-amend-1108` Note and edit no ADR 0106 file. Edit the glossary Note `side-panel`
(`.capcom/knowledge/glossary/glossary-identity/side-panel.md`): the column is 36 by default, the user
may drag it within limits. Run `capcom check`.
**Regression guard.** ADR 0106 Decision 5 covers only button-less motion, so drop the adr-0106-amend-1108 Note and the ADR 0106 files — the live-drag repaint exception is recorded in adr-0110-amend-1108 instead, naming the rule's real home (model.go settle/settleFrame, internal/tui/doc.go, TestMouseMotionNeverRepaints, plan 2026-09-16 item 18).
State the rule, not a list: the amendment supersedes, and the glossary Note rewords, every sentence
that says the column's width is fixed or changes only on a gate crossing or toggle (`adr-0110.md`:8,12;
`adr-0110-decision.md` headline 5-6, D6, D7; `adr-0110-consequences.md`:11; `side-panel.md` title, 7, 11) —
`grep -rn -i 'fixed\|\b36\b\|only on\|only when\|re-keys' .capcom/knowledge/decisions/adr-0110.md .capcom/knowledge/decisions/adr-0110/ .capcom/knowledge/glossary/glossary-identity/side-panel.md`; name `/panel width` beside D8's `/panel` forms.
**Files:** `.capcom/knowledge/decisions/adr-0110.md`, `.capcom/knowledge/decisions/adr-0110/adr-0110-amend-1108.md`, `.capcom/knowledge/glossary/glossary-identity/side-panel.md`
**Read first:** .capcom/knowledge/decisions/adr-0110/adr-0110-decision.md — headline, decisions 6, 7, 8; .capcom/knowledge/decisions/adr-0110.md — Status line, reading-order sentence;
.capcom/knowledge/decisions/adr-0110/adr-0110-rejected.md — "Proportional or user-set width", "list order stays fixed"; .capcom/knowledge/decisions/adr-0110/adr-0110-consequences.md — cached-frame re-key bullet;
.capcom/knowledge/glossary/glossary-identity/side-panel.md — title, definition; .capcom/knowledge/decisions/adr-0012/adr-0012-amend-0914.md — amendment Note shape
**Tests:** none (docs only).
**Acceptance:** `capcom check`; `capcom know show "ADR 0110" --with-children | grep -n amend-1108`;
the grep above returns, besides `adr-0110-rejected.md`'s "list order stays fixed" and the superseded
originals the amendment names, only sentences that state the default 36 beside the drag and its limits.
**Commit:** `docs(adr): amend ADR 0110 for a user-dragged side panel width`

## 2. The `ui.side-panel.width` setting — ✅ DONE (2026-11-08)

NOTES (2026-11-08): the block writer's show-only save and reset were folded into one scalar path (`saveSidePanelScalar` plus a `sidePanelScalarLeaves` map) serving `show` and `width`, instead of a second copy of `saveSidePanelShow` for the width.
NOTES (2026-11-08): `UIPrefs.SidePanelWidth` is a plain int whose 0 means unset; the one resolution method that turns 0 into 36 for the layout is left to item 3, as its regression-check note assigns.
NOTES (2026-11-08): besides `wantUIDefault`, the eleven other full `domain.UIPrefs` literals in `internal/config/config_test.go` gained `SidePanelWidth: 36` (they compare against a resolved default block).
NOTES (2026-11-08): consequential edit — cmd/apogee/wire_options.go: made necessary by the width becoming a third key of the `ui.side-panel:` block ("The two keys of the block" comment now reads "The keys of the block"; no code change).
NOTES (2026-11-08): docs/manual/configuration.md §"The side panel" still says "a 36-column panel" (line ~1353); left for item 8, which owns the manual's side-panel section.

**What:**
Recast at the regression check (2026-11-08).
**Goal:** `ui.side-panel.width` is a global, editable integer UI key (default 36, valid ≥ 24): read
from config, shown and edited in `/settings`, written and reset by the side-panel block writer,
applied live, documented in the template and `docs/manual/configuration.md`.
**Approach (assumed at the header base):** follow archived plan
`docs/plans/archived/2026-11-08 - 00 - tui-side-panel-plan.md` item 5 for `ui.side-panel.show`:
domain `UIKeySidePanelWidth` in `uiKeys`, `DefaultUIPrefs`, `Validate` (reject < 24 with a message
naming the minimum), `Set`; field on `uiSidePanelConfig`; template line under `ui: side-panel:`;
`IsSidePanelKey` plus `SaveSidePanelSetting`/`ResetSidePanelSetting` in
`internal/config/configwrite_sections.go`; registry row; `settingsApplyLocal` case setting the
panel's stored width (consumed by item 3; until then it is stored only). Update every count comment
of `ui:` keys and the tests the archived item lists (`TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase`,
`TestRegistryIsBijectionWithFileConfig`, `TestTemplateMentionsEveryRegistryKey`,
`TestManualDocumentsEverySettingsKey`, `TestSettingsApplyLocalLandsEveryUIKeyThroughSet`, …).
**Regression guard.** A zero `ui.side-panel.width` means unset and resolves to 36 (the Glyphs "" posture) — UIPrefs.Validate refuses only 1..23 and negatives, so a zero UIPrefs stays valid; a new domain.ParseSidePanelWidth (shaped like ParseToolsFoldOver) refuses non-integers and anything below 24, 0 included, and is used by Set, the file pass and the registry Validate hook; add a zero case to the Validate/Set table (0 validates; Set("0") and Set("23") refused); fix the registry path to internal/config/registry.go and add the reviewer's named test files (cmd/apogee/wire_settings_test.go settingKeysAppliedByTheRenderer, cmd/apogee/settingsrows_test.go with a non-default width in its fixture, internal/config/registry_test.go owns/value tables and everyKeyFileConfig, internal/tui/settingsapply_test.go) to Files and their -run patterns to Acceptance.
Add `SidePanelWidth: 36` to `wantUIDefault` (`internal/config/config_test.go`, a hand literal) — the default-resolution tests read it.
Ship the template line commented (`# width: 36`) between `show:` and `# sections:` — an active line makes `sidePanelInsertion` append
`sections:` after it — and put the registry row between the `ui.side-panel.show` and `ui.side-panel.sections` rows (`TestRegistryFollowsTheTemplateOrder`).
The descriptor is `uiStallAfter`'s shape (`internal/config/keyfield.go`), not the count row's `uiField`+`countPtr`: `Width *WholeCount` on
`uiSidePanelConfig`, `fileText = countText(fc.UI.SidePanel.Width)`, `fileLand = land(domain.ParseSidePanelWidth, f.at)` — so a file `width: 0` or `width: 10` is refused at load.
`cmd/apogee/wire_options.go` needs no change — it routes by `IsSidePanelKey`, so naming the width key in that predicate is enough.
**Files:** `internal/domain/uiprefs.go`, `internal/domain/uiprefs_test.go`, `internal/domain/sidepanelprefs.go`, `internal/domain/sidepanelprefs_test.go` (if `ParseSidePanelWidth` sits beside `ParseSidePanelShow`), `internal/config/config.go`, `internal/config/keyfield.go`, `internal/config/defaults/config.yaml`, `internal/config/configwrite_sections.go`, `internal/config/configwrite_sections_test.go`, `internal/config/config_test.go` (`everyKeyFileConfig` and `wantUIDefault` live here), `internal/config/registry.go`, `internal/config/registry_test.go`, `internal/tui/settingsapply.go`, `internal/tui/settingsapply_test.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/settingsrows_test.go`, `docs/manual/configuration.md`
**Read first:** internal/domain/uiprefs.go — uiKeys, DefaultUIPrefs, UIPrefs.Validate, UIPrefs.land, ParseToolsFoldOver; internal/config/keyfield.go — uiField, uiStallAfter, countText; internal/config/registry.go — ui.side-panel.show row, ui.side-panel.sections row, validateSidePanelShow, land;
internal/config/configwrite_sections.go — IsSidePanelKey, SaveSidePanelSetting, ResetSidePanelSetting, saveSidePanelShow, sidePanelInsertion; internal/config/config_test.go — wantUIDefault, everyKeyFileConfig; internal/config/registry_test.go — TestRegistrySetIsTheInverseOfRead owns, TestUIRowsLandThroughUIPrefsSet documented;
internal/tui/settingsapply.go — settingsApplyLocal side-panel arm; cmd/apogee/settingsrows_test.go — fabricatedSettings, TestSettingsRowsFormatEffectiveValues
**Tests:** domain Validate/Set table for 0/23/24/36/200 (0 validates; `Set("0")` and `Set("23")` refused;
`ParseSidePanelWidth` refuses a non-integer and anything below 24); a load test that a file `width: 0` and
`width: 10` are refused; block writer save and reset round-trip
keeping `show` and `sections` (`configwrite_sections_test.go`); registry read/set inverse; settings row value (non-default width in the
`settingsrows_test.go` fixture).
**Acceptance:** `go build ./...`; one package per run:
`-run 'TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase|TestUIPrefs|TestSidePanel|TestParse' ./internal/domain/`;
`-run 'TestRegistryIsBijectionWithFileConfig|TestRegistrySetIsTheInverseOfRead|TestUIRowsLandThroughUIPrefsSet|TestTemplateMentionsEveryRegistryKey|TestSidePanel|TestResolvePrecedence|TestApplyConfig|TestLoadFileConfigAbsentIsTheDefaults|TestEmbeddedDefaultConfig|TestUIRowsLoadFromTheGlobalFile|TestRegistryFollowsTheTemplateOrder' ./internal/config/`;
`-run 'TestSettingsApplyLocalLandsEveryUIKeyThroughSet' ./internal/tui/`;
`-run 'TestManualDocumentsEverySettingsKey|TestSettingsRowsFormatEffectiveValues|TestEveryEditableSettingKeyHasAnApply' ./cmd/apogee/`.
**Commit:** `feat(config): add ui.side-panel.width`

## 3. Variable side panel width in the layout

Depends on item 2.
**What:**
**Goal:** the painted panel width is `clamp(stored, 24, m.width − floor − scrollbarWidth)` (floor
100 for `auto`, 80 for `on`), the gate still uses the default 36, the transcript slot is the
window minus that width, rows are squared to the matching content width, and the cached frame
re-keys when the effective width changes.
**Approach (assumed at the header base):** replace uses of the const `sidePanelWidth` and
`sidePanelContentWidth` in `internal/tui/sidepanel.go` (`sidePanelColumn`, `panelRow`,
`panelFileRow`) and `slotWidth()` in `model.go` with a method returning the effective width (and
one for content width); `sidePanelLeastWindow` keeps the default constant. Add the effective width
to `frameKey` in `paintcache.go` (replacing or beside `sidePanel bool`) and correct its comment.
A clamp never writes the setting.
**Regression guard.** Every reader of the width resolves 0 to the default 36 through one method (the zero UIPrefs in test harnesses paints 36).
`panelRow`'s pad (`strings.Repeat(" ", room-w(text)+1)`) floors at 0 — at width 24 a viewed sub-agent
row or a ≥1h row drives it negative and panics; `squareLine` cuts the overflow. The `frameKey` width
reads 0 whenever `!panelShown()` (clamp only when shown), so a show-mode switch still re-keys.
Correct every comment naming the fixed width or its only movers: `grep -n 'sidePanelWidth\|fixed column\|36 columns\|moves only' internal/tui/*.go`.
**Files:** `internal/tui/sidepanel.go`, `internal/tui/model.go`, `internal/tui/paintcache.go`, `internal/tui/sidepanel_test.go`
**Read first:** internal/tui/sidepanel.go — sidePanelColumn, panelRow, panelFileRow, sidePanelLeastWindow; internal/tui/model.go — slotWidth, panelShown, layout; internal/tui/paintcache.go — frameKey;
internal/tui/sidepanel_test.go — TestSidePanelGate, TestSidePanelSectionChangeMovesNothing; internal/tui/model_test.go — testUIPrefs; internal/tui/boxdraw.go — squareLine
**Tests:** table — stored × window × mode → painted width (stored 0 → 36, 24 floor, transcript-floor
cap, gate unchanged at 137/117); a width-24 row with a viewed running sub-agent and a ≥1h row paints
without panic; separator at `slotWidth()` for a non-default width; transcript re-wraps once when the
width changes and not on a section change (`TestSidePanelSectionChangeMovesNothing`).
**Acceptance:** `go build ./internal/tui/`; `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSidePanel' ./internal/tui/`.
**Commit:** `feat(tui): paint the side panel at the configured width`

## 4. Drag the separator

Depends on item 3.
**What:**
**Goal:** a left press on the separator or gutter column (rows above the slot bottom, panel shown,
not watching) starts a resize drag; held-button motion sets the width live from
`x − grabOffset`, clamped, relayouting only when the width changed; release writes
`ui.side-panel.width` through the settings host only when the width differs from the press-time
width; a terminal resize that hides the panel abandons the drag and restores the press-time width;
the separator renders in the accent tone, bold, while the drag is active.
**Approach (assumed at the header base):** in `handleSidePanelClick` (`sidepanel.go`) claim the
separator/gutter cells for a new small drag struct on the Model (active, grabOffset, startWidth);
read it first in `handleMouseMotion` and `handleMouseRelease` (`mouse.go`), before the selection
branches. Live relayout: the motion handler calls `layout()` itself; `settleFrame`'s motion skip
stays for every other motion, and `TestMouseMotionNeverRepaints` is narrowed to "no active
separator drag" with a sibling test for the exception (ADR 0106 amend-1108). Persist like
`setPanelSectionMode`: screen first, `opts.Settings.Write`, `recordSettingEdit` on success, one
`side panel width: not saved: …` note on failure, session-only with a nil host. Add an accent-bold
style to the theme for the active separator. The `WindowSizeMsg` arm abandons a drag when the panel
hides.
**Regression guard.** Amend the motion-never-repaints prose where it lives (every comment and doc.go passage stating that no mouse motion repaints — find with grep for "MouseMotionMsg" and "never repaint" in internal/tui) to name the active separator drag as the one exception, and cite adr-0110-amend-1108 rather than an ADR 0106 amendment; add the ADR 0106 header Sources/Ratified wording accordingly.
The exception narrows (does not supersede) plan "2026-09-16 - 00" item 18, cited at model.go settleFrame.
Relayout from model.go (settleFrame settles a motion while the drag is active) or give a `layout()` in
mouse.go a same-line `// geometry:` comment (`TestArmsLeaveLayoutToTail`). The drag arm sits after the
SGR-35 reroute and the `Button != MouseLeft` filter, before `handleSettingsMotion`; the release arm is
first in `handleMouseRelease`. Record the press-time EFFECTIVE width; motion leaves stored untouched
while the computed width equals it, and release compares against it.
**Files:** `internal/tui/sidepanel.go`, `internal/tui/mouse.go`, `internal/tui/model.go`, `internal/tui/theme.go`, `internal/tui/doc.go`, `internal/tui/sidepanel_test.go`, `internal/tui/model_test.go`
**Read first:** internal/tui/mouse.go — handleMouseClick, handleMouseMotion, handleMouseRelease; internal/tui/sidepanel.go — handleSidePanelClick, setPanelSectionMode; internal/tui/model.go — settleFrame, settle, Update WindowSizeMsg case;
internal/tui/settled_test.go — TestArmsLeaveLayoutToTail; internal/tui/model_test.go — TestMouseMotionNeverRepaints; internal/tui/theme.go — themeStyles
**Tests:** drag moves and writes once; drag past limits clamps; press+release writes nothing; drag
back to press column writes nothing (also with a stored width above the cap, e.g. 60 at a 140-col
`auto` window); failed write → one note, width stands; drag abandoned by a hiding resize; drag inert
while watching; a button-less motion (SGR 35) mid-drag ends it; transcript selection still stops at
the separator (`TestSidePanelDragStopsAtTheSeparator`); accent separator mid-drag; motion-repaint exception.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSidePanel|TestMouseMotion|TestArmsLeaveLayoutToTail' ./internal/tui/`.
**Commit:** `feat(tui): resize the side panel by dragging its separator`

## 5. Double-click on the separator resets the width

Depends on item 4.
**What:**
**Goal:** two presses on the separator/gutter within 500 ms reset the width to 36 through the
settings host's Reset of `ui.side-panel.width` and start no drag; a press right after a drag that
moved the separator is never a double-click.
**Approach (assumed at the header base):** record the last separator press time on the drag struct
from the Model's injectable clock (use the existing one the TUI tests already stub; add none if one
exists); on reset, follow the same note/record contract as item 4 with `Settings.Reset`.
**Regression guard.** No Model clock exists (the transcript's `now` stamps entries; `sidepanel.State`'s
is unexported and `m.panel` is nil until an Event): add a `now func() time.Time` on the Model, seeded
`time.Now` in `newModel`, which tests overwrite to stub "two slow presses".
**Files:** `internal/tui/sidepanel.go`, `internal/tui/model.go`, `internal/tui/sidepanel_test.go`
**Read first:** internal/tui/sidepanel.go — handleSidePanelClick, setPanelSectionMode; internal/tui/model.go — newModel, Model; internal/tui/tui.go — SettingsHost.Reset; internal/tui/settingsapply.go — settingsReset, recordSettingEdit;
internal/tui/settings.go — settingEdit; internal/tui/settings_test.go — fakeSettingsHost
**Tests:** double-click resets and starts no drag; two slow presses keep the width; press after a
moving drag starts a drag; reset with a failing host → one note.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSidePanel' ./internal/tui/`.
**Commit:** `feat(tui): double-click the side panel separator to reset its width`

## 6. `/panel width <n>|reset`

Depends on item 2.
**What:**
**Goal:** `/panel width <n>` sets `ui.side-panel.width` (refusing n < 24 or a non-integer with the
domain's message), `/panel width reset` resets it to 36; both apply live, write through the
settings host, are refused in watch mode like the other `/panel` forms, and appear in `/help` and
`docs/manual/commands.md`.
**Approach (assumed at the header base):** extend `setPanelShow`'s command parser in
`internal/tui/sidepanel.go` and the `/panel` registration in `command.go`; reuse item 2's
`Validate`; update `panelHelpWords`-driven tests.
**Regression guard.** Parse in `parsePanel` (not `setPanelShow`, which only writes) through
`domain.ParseSidePanelWidth`, refusing 0, anything below 24 and any non-integer; add the width form to
`panelUsage` and to the `/panel` prose in `command.go` (summary, the verb comment, the idle-only list). Reset
via `m.opts.Settings.Reset`, recorded as `settingEdit{reset: true}`; the spy gains a reset func. Make
`panelHelpWords` return the width group as a third group (it Fatalfs unless exactly two) and drive
`/panel width` from it. Item 6 touches `docs/manual/commands.md` only; item 8 owns the other `/panel` lists.
**Files:** `internal/tui/sidepanel.go`, `internal/tui/command.go`, `internal/tui/sidepanel_test.go`, `docs/manual/commands.md`
**Read first:** internal/tui/sidepanel.go — parsePanel, panelArgs, panelUsage, runPanelCommand, setPanelShow; internal/tui/sidepanel_test.go — panelHelpWords, settingsSpy, TestSidePanelCommandDrivesTheSettings;
internal/tui/command.go — panel commandSpec, safeWhileRunning; internal/tui/tui.go — SettingsHost.Reset; internal/tui/settings_test.go — fakeSettingsHost
**Tests:** set, reset (through the host's Reset), too small, zero, non-integer, watch-mode refusal, help line.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSidePanelCommand' ./internal/tui/`.
**Commit:** `feat(tui): /panel width sets or resets the side panel width`

## 7. Drag journey through the terminal driver

Depends on item 4.
**What:**
**Goal:** `internal/tuitest` can send a held-left-button motion report (`ESC[<32;x;yM`), and one
`cmd/apogee` e2e test drags the separator on a 160-column window and sees the panel width change in
the frame and the width written to the config file.
**Approach (assumed at the header base):** add `tuitest.Drag(x, y)` beside `Click`/`Release`
(`sgrMouse`) and pin it in `TestKeysDecodeAsIntended`; write the e2e beside
`cmd/apogee/e2e_popups_test.go`, using the driver's config fixture with `ui.side-panel.show: auto`.
**Regression guard.** `TestKeysDecodeAsIntended` lives in `internal/tuitest/driver_test.go` (no
`keys_test.go`): add `tea.MouseMotionMsg` to `msgLog.mouse()`'s filter and assert the decoded msg is a
MouseMotionMsg, Button MouseLeft, at (x, y). Give `sgrMouse` a button argument (Drag sends 32) and
update its comment; fold `cmd/apogee/e2e_copy_test.go` `dragAcross`'s inline report onto `tuitest.Drag`.
**Files:** `internal/tuitest/keys.go`, `internal/tuitest/driver_test.go`, `cmd/apogee/e2e_copy_test.go`, `cmd/apogee/e2e_sidepanel_drag_test.go`
**Read first:** internal/tuitest/keys.go — Click, Release, sgrMouse; internal/tuitest/driver_test.go — TestKeysDecodeAsIntended, msgLog.mouse; cmd/apogee/e2e_copy_test.go — dragAcross;
cmd/apogee/e2e_support_test.go — launchTUIConfigured, appendHomeConfig; cmd/apogee/e2e_approval_test.go — sidePanelOffConfig; internal/tui/sidepanel.go — sidePanelSeparator
**Tests:** the decode pin; the e2e journey; the copy e2e still passes on `tuitest.Drag`.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestKeysDecodeAsIntended' ./internal/tuitest/`;
`GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2ESidePanelDrag|TestE2ECopy' ./cmd/apogee/`.
**Commit:** `test(e2e): drag the side panel separator through the terminal driver`

## 8. Layout spec and manual

Depends on items 4, 5, 6.
**What:**
**Goal:** every doc sentence stating a fixed 36-column panel, or that the column changes width only
on a gate crossing or a `ui.side-panel.show` change, instead states the default 36, the drag, the
limits, double-click reset and `/panel width`.
**Approach (assumed at the header base):** find sites with
`grep -rn '36\|changes width\|appears or vanishes\|stops at the separator\|No key binding' layout.md docs/layout/side-panel-layout.md docs/manual/configuration.md`;
reword `layout.md` §"The side panel", `docs/layout/side-panel-layout.md` (width, gate, §Pointer),
`docs/manual/configuration.md` (panel prose and "The mouse." paragraph).
**Regression guard.** Item 8 owns docs/layout/side-panel-layout.md §Commands and docs/manual/configuration.md's "From the prompt." paragraph for `/panel width`; it rewords after item 2's width paragraph has landed in configuration.md and never duplicates it — item 2 adds only the key's own entry; item 6 touches commands.md only.
The rule: every sentence calling the column fixed, 36-column, or moved only by the gate/show, and every
list of `/panel` forms — `grep -rn 'fixed column\|36-column\|36 painted\|changes width\|appears or vanishes' layout.md docs/layout/side-panel-layout.md docs/manual/configuration.md internal/config/defaults/config.yaml`.
**Files:** `layout.md`, `docs/layout/side-panel-layout.md`, `docs/manual/configuration.md`, `internal/config/defaults/config.yaml`
**Read first:** docs/layout/side-panel-layout.md — intro, Where it sits (Columns), When it shows, Pointer, Commands; layout.md — §The side panel; docs/manual/configuration.md — ui.side-panel.show paragraph, §The side panel (When it shows, The mouse., From the prompt.);
internal/config/defaults/config.yaml — side-panel comment block; cmd/apogee/docs_settings_test.go — TestManualDocumentsEverySettingsKey
**Tests:** none (docs only).
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManualDocuments' ./cmd/apogee/`; the
rule's grep returns only hits that state the default 36 alongside the drag and its limits (plus the
unrelated popup line `layout.md` "not to a fixed column").
**Commit:** `docs(layout): describe the draggable side panel width`

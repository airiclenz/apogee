# Footer rows and colour — a configurable row layout, middle-elision, meaning-driven colour

**Goal:** The footer's segments are laid out in the rows `ui.footer.rows` names (default: two rows, upstream then workspace), each row priority-fitted, with the workdir and the branch middle-elided before any segment drops. Git counts and the model carry meaning-driven colour, and `ui.footer.colors` overrides a segment's colour with a scheme role.
**Date:** 2026-11-05
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 5cedb3a3

**Sources:**
- `.capcom/issues/footer-overflow-layout.md` (owner decision 2026-11-05), `.capcom/issues/footer-segment-colour.md`
- layout.md "The footer's upstream slot", "What "height" means", "What "colour" means"
- internal/tui/footerfit.go (`footerFit`, `footerInput`, `footerLayout`); internal/tui/model.go (`footerRow`, `footerContent`, `footerHeight`, `frameFixedRows`, `frameFloorRows`); internal/tui/mouse.go (`footerModeHit`, `footerRowY`)
- internal/scheme (role table, `schemes/dark.yaml`, `schemes/light.yaml`); internal/config/registry.go `ui.*` rows; `domain.UIPrefs`
- Refusal precedent: commits 919b1c4c, ad35e191 (`checkedField`, load-time refusal)

**Ratified design calls** (owner, 2026-11-05):
- **Key:** a `ui.footer` block; registry keys `ui.footer.rows` and `ui.footer.colors`. Global-only like every `ui.*` key.
- **Rows:** a list of 1 or 2 rows of segment names `host, model, effort, mode, workdir, git, spend`; default `[[host, model, effort, mode], [workdir, git, spend]]`; one row `[[host, model, effort, workdir, git, spend, mode]]` is today's shape.
- **Mode:** `mode` must appear exactly once; the marker sits at the right edge of the row naming it. `offline` is not a segment: it sits just left of the mode marker.
- **Fit:** each row keeps today's priorities (spend 5, git 4, effort 3, workdir 2, host 1, model 0). Before any segment drops, the workdir middle-elides (`~/repos/…/internal/tui`, floor `…/<last>`), then the branch (plain middle `…`).
- **Colour default (meaning only):** git `Δ` and `↓` non-zero in `warning`, `↑` non-zero in `success`, zero counts muted; the model in a new `footer-accent` role. Nothing else changes tone.
- **Overrides:** `ui.footer.colors` maps `host | model | effort | workdir | branch | spend` to a scheme role name; no raw colours; `mode` and `offline` cannot be overridden; an unknown segment or role is refused at load.

**Standing requirements:**
- skills: coding-standards
- Any deviation from item text lands as a dated NOTES line under the item.
- Raspberry Pi 5 host: never `-race`/`-cover` over a whole heavy package or a pattern; targeted `GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./pkg/`; full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make check`; one test run at a time. Forward to every sub-agent.
- `footerFit` measures only through `in.measure` (ADR 0030 §5). The painter and the click handler keep reading one layout (`footerRow`).
- No item changes `VERSION`, a CHANGELOG release heading or a tag.

**Out of scope:**
- The footer git actions panel (`footer-git-actions`, plan `2026-11-05 - 03 - footer-git-actions-plan.md`).
- The status line's priority fit (`status-line-priority-fit`); a context gauge or spend thresholds in the footer.

**Regression check (2026-11-05, 5cedb3a3):**
- 1: guard folded (outside footerInput readers; Goal reworded to "construction adapted")
- 2: guard folded (single span carrier; narrowing tests restated; footerGit paints kept spans; acceptance grep)
- 3: recast (comparable rows value; three-level path support; watch-homes-shaped row); yields to ADR 0043 amendment 0920
- 4: guard folded (constant-free grep; adapted sites in `-run`)
- 5: recast (TUI-local settings-table arm for `ui.footer.rows`; `footerContent(w)` stays joined)
- 6: guard folded (every footerRow caller; `idleRuleFromBottom`; `wantUIDefault` literals)
- 7: guard folded (single span carrier; bright-prose sweep); supersedes layout.md:1948 and CHANGELOG.md:48 (0.32.0)
- 8: recast (omitted role inherits silently; 29 → 30 role counts); yields to ADR 0040 decision 3
- 9: recast (comparable overrides; live settings arm; exported scheme role names); supersedes ADR 0040 decision 8 for this key
- 3 (re-check): recast (parseable canonical `Read` + `UIPrefs.Set` arm); guard folded (Validate accepts zero rows, `rows: []` refused in `fromFile`; spelled-out `domain.UIPrefs{Spinner:` literals; `TestRegistryKeyClassesArePinned` unchanged)
- 5 (re-check): recast (TUI-local arm in internal/tui `settingsApplyLocal`, not cmd/apogee/wire_settings.go)
- 8 (re-check): guard folded (role-count grep widened to ADR 0040; 30th key by a dated ADR 0040 amendment); supersedes the count at adr-0040-amend-0815.md:10 and adr-0040-consequences.md:6
- 9 (re-check): recast (extends item 3's parseable `Read` and `Set` arm; arm in `settingsApplyLocal`; ADR 0040 decision 8 amendment); guard folded (overrides resolved at newModel, applyColorScheme and the colors arm, off `themeStyles`)

## 1. Footer segments by name — ✅ DONE (2026-11-05)

NOTES (2026-11-05): footerModeSpan keeps reading the layout's mode/col/hasMode: the mode marker stays the fit's own atom (footerSegMode names where it sits, for item 3's rows) rather than an entry in the span table, which holds the six left-run segments; the priority-0 rung truncates the joined priority-0 run (today the model alone) and names the cut segment by its cut text.

**What:** a refactor with no visible change, so later items can order and colour segments.

**Regression guard.** footerInput's named fields have readers outside footerfit_test.go: `m.footerLeftText().spend` (fork_test.go:207 `TestForkStartsAtZeroSpend`, usage_test.go:842 `assertSpend`) and `.effort` (watch_test.go:409 `TestWatchLeavesTheFooterUnchanged`). Give footerInput a `segment(id) string` accessor those three call (or adapt them); all three stay green.

**Goal:** `footerFit` fits one row from an ordered list of named segments (`host, model, effort, mode, workdir, git, spend`) and returns, per kept segment, its text and its cell span; every existing footerfit, mode and model footer test passes with only its input/span-table construction adapted; its expected strings unchanged.

**Approach (assumed at the header base):** replace the positional `footerInput` fields with an ordered `[]footerSegment{id, text}` plus the mode and offline atoms; drive the existing rung ladder from each segment's priority instead of the hard-coded rung slice; generalise `footerLayout.git`/`gitAt` into a per-segment span table that `footerLeft`, `footerGit` and `footerModeSpan` read. Extends footerfit.go; no new module.

**Files:** internal/tui/footerfit.go, internal/tui/footerfit_test.go, internal/tui/model.go, internal/tui/fork_test.go, internal/tui/usage_test.go, internal/tui/watch_test.go
**Read first:** internal/tui/footerfit.go — footerFit, footerInput, footerLayout; internal/tui/model.go — footerRow, footerLeftText, footerGit;
internal/tui/footerfit_test.go — footerFitCase; internal/tui/usage_test.go — assertSpend

**Tests:** existing `TestFooterFit*` adapted to the new input only; the three outside readers above read through the accessor; one new test that the span table's cells agree with the painted row for every rung.

**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFooter|TestGitFooterSpans' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestForkStartsAtZeroSpend|TestWatchLeavesTheFooter|TestResumedSessionCountsUnderItsOwnLabel|TestSessionsResumeSeatsTheRecordsLabel' ./internal/tui/`

**Commit:** `refactor(tui): footer fit works on named segments`

## 2. Middle-elide the workdir and the branch before any segment drops — ✅ DONE (2026-11-05)

NOTES (2026-11-05): the workdir's steps keep head and tail k components deep with k shrinking to 1 (then the floor `…/<last>`), which reproduces the Goal's example sequence exactly; the Approach's "head component and as many trailing components as fit" would yield `~/…/apogee/internal/tui` rather than the Goal's `~/repos/…/internal/tui`, so the Goal wins. A form that saves no cell (`~/apogee` → `…/apogee`) is skipped.
NOTES (2026-11-05): the plan names no branch floor; chosen as 8 cells of the branch's own name around the ellipsis (`footerBranchFloorCells`, `feat…name`). The branch elides to exactly the width the row lacks (one rung computed from the row's slack), not in fixed steps.
NOTES (2026-11-05): the git segment's toned pieces now ride on `footerSegment.pieces` and `footerSegmentSpan.pieces` (the one span carrier), with `footerSpan.isBranch` marking the elidable piece; `Model.footerGit` takes the kept span and paints its pieces, and `Model.footerGitText` became `Model.footerGitSegment` (returns the segment with its pieces). Positional `footerSegment` literals in model.go and footerfit_test.go became keyed.
NOTES (2026-11-05): consequential edit — internal/tui/doc.go: made necessary by the fit now middle-eliding before it drops (package map describes the fit's order).
NOTES (2026-11-05): pre-existing lint finding outside this item: `golangci-lint run ./internal/tui/` reports SA4006 at internal/tui/toolpresent_test.go:2116 (unused `got`), untouched by this item.

**What:** Depends on item 1.

**Regression guard.** item 1's per-segment span table is the single carrier — item 2 elides inside it and item 7 retones it, no second span type. footerGit (model.go:3935) paints the spans the layout kept, never spans re-derived from the snapshot (an elided branch never re-joins, so today's plain fallback would drop the count tones). A rung after a segment drop composes the FULL workdir again, so `TestFooterDropsSegmentsBeforeTheModeMarker`'s `{60, "test-host ✦ test-model ✦ /ws/proj"}` case stays as it is; layout.md names the rule with the word `middle-elide`.

**Goal:** when a row does not fit, the workdir shortens stepwise by middle-elision (`~/repos/apogee/internal/tui` → `~/repos/…/internal/tui` → `~/…/tui` → `…/tui`), then the branch middle-elides (`feature/long-…-name`), each to its floor, before the first segment drops; a row that fits is untouched.

**Approach (assumed at the header base):** a path-aware elider (keeps the head component and as many trailing components as fit) and a plain middle elider, both built on `widthAuthority.Cut`/`Width`, private to footerfit.go; `footerFit` runs them as rungs ahead of the "minus spend" rung. The git segment's branch is elided inside its spans so its count tones survive.

**Files:** internal/tui/footerfit.go, internal/tui/footerfit_test.go, internal/tui/model.go, internal/tui/mode_test.go, layout.md
**Read first:** internal/tui/footerfit.go — footerFit, gitFooterSpans; internal/tui/model.go — footerGit; internal/tui/mode_test.go — TestFooterStatesTheSessionSpend,
TestFooterStatesTheGitSegment, TestFooterDropsSegmentsBeforeTheModeMarker; internal/tui/model_test.go — footerFactsModel; internal/tui/workspacepath.go — workdirDisplay

**Tests:** table tests per width for a deep workdir, a long branch, both, wide glyphs and `~`-free paths; the existing priority-ladder test still drops in its order after both floors; the narrowing subtests of `TestFooterStatesTheSessionSpend` and `TestFooterStatesTheGitSegment` (mode_test.go:176-188, 247-263) restated — the first change is the elided workdir, then the elided branch, then the drop; at a width that elides the branch, the non-zero counts still open their tone.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFooter|TestGitFooterSpans' ./internal/tui/`
- `grep -n 'middle-elide' layout.md`

**Commit:** `feat(tui): footer middle-elides the workdir and branch before dropping a segment`

## 3. The `ui.footer.rows` key — ✅ DONE (2026-11-05)

NOTES (2026-11-05): the rows ride in `domain.UIPrefs` as `FooterRows`, a string type holding the one canonical spelling (`SpellFooterRows` writes it, `ParseFooterRows` reads it back into `[][]FooterSegment`), so the block stays comparable with `==`; `""` reads as the default (Glyphs' posture).
NOTES (2026-11-05): the starter template states the default as a COMMENTED example under `ui:` (plus a prose paragraph in the block header), not an active line, so a seeded file does not pin the one-row shape once the default moves (item 6) and the template stays behaviour-neutral (TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt).
NOTES (2026-11-05): the three-level template predicates (templateMentionsSetting/templateFirstMention) are now one indentation-tracking walk; the manual predicate's fence arm walks nested blocks and its leaf arm scopes to the leaf's immediate block (`footer:`); settingKeyLine walks blocks for a path deeper than two segments (yaml.v3 imported in cmd/apogee/settingsedit.go). ScalarTargetIn is unchanged.
NOTES (2026-11-05): the global-only project-layer test (TestUIRowsInAProjectFileAreDroppedAsGlobalOnly) lives in internal/config/config_test.go beside the other TestUIRows tests, reusing layer_test.go's helpers, rather than in layer_test.go (not in Files).
NOTES (2026-11-05): consequential edit — cmd/apogee/settingsrows_test.go: made necessary by the new registry row (the row-value table pins one value per registry key; the fixture now carries two rows against the one-row default).
NOTES (2026-11-05): consequential edit — cmd/apogee/testdata/frames/t16-settings-rows.txt: made necessary by the new row's Read text — the /settings pane sizes its value column to the widest value, and the canonical rows spelling (52 cells) widens it; regenerated with -update.
NOTES (2026-11-05): the manual paragraph describes the layout the key names; the TUI reads the key only from item 5, as the item states.

**What:** Recast at the regression check (2026-11-05). config only; the TUI does not read it until item 5.

**Regression guard.** `domain.UIPrefs` stays comparable with `==` — the rows ride as a comparable value (a fixed-size array with a row count, or a canonical string parsed once; implementer's choice, no `==` site changes meaning), and item 3 owns the three-level registry path support (`ui.footer.rows` in the starter-template predicate, the manual predicate, and `settingKeyLine` in cmd/apogee/settingsedit.go finding the nested line) — add those files to Files. Yields to ADR 0043 amendment 0920: the row is written like `sessions.watch-homes` (registry.go:1248 — hand-written `fromFile`, `Read`, `Structure`, no field; the keyfield.go header's "seventeen" list stays), refused through `UIPrefs.Validate` (run at load, config.go:3883), not `checkedField`. The row's `Default` stays empty (the default lives in `DefaultUIPrefs` and the template); the footer keys stay out of `uiKeys`/`UIKeys()`; `DefaultUIPrefs` carries the one-row value explicitly (non-nil) and `wantUIDefault` (config_test.go:37) gains it here.
Re-check (2026-11-05): the `ui.footer.rows` row's `Read` text is parseable back into the value (one canonical spelling) and `UIPrefs.Set` gains an arm for it, so a live re-read lands through the same parse as the file. `UIPrefs.Validate` accepts zero rows (unset ⇒ the default, Glyphs' `""` posture), so the zero block (uiprefs_test.go:320) and the one-key validators (`validateSpinnerName`, `validateGlyphSetName`, `validateNotifyMode`, registry.go:1761-1773) stay green; an explicit `rows: []` is refused in the row's hand-written `fromFile`. Every spelled-out `domain.UIPrefs{Spinner:` literal compared to a resolved block gains the default rows (`grep -n 'domain.UIPrefs{Spinner' internal/config/*_test.go` — config_test.go:293, :5734, :6242-6300). `TestRegistryKeyClassesArePinned` stays unchanged (it lists only non-global-only keys); the project-layer notice test is the global-only proof.

**Goal:** `ui.footer.rows` loads from the global config into `domain.UIPrefs`, defaulting to the one-row shape `[[host, model, effort, workdir, git, spend, mode]]`; load refuses, naming the key and value, a list of 0 or more than 2 rows, an empty row, an unknown segment name, a repeated name, and a list without exactly one `mode`; docs/manual/configuration.md "The terminal UI — `ui:`" documents it.

**Approach (assumed at the header base):** a `footer:` block in `uiConfig`; a `KindStructured` registry row (edited in the user's editor, as `servers`); refusal through `UIPrefs.Validate` with the `apogee: invalid ui.footer.rows …` wording; the starter template `internal/config/defaults/config.yaml` states the default.

**Files:** internal/config/config.go, internal/config/registry.go, internal/config/defaults/config.yaml, internal/config/config_test.go, internal/config/registry_test.go, internal/config/defaults_test.go, internal/domain/uiprefs.go, internal/domain/uiprefs_test.go, cmd/apogee/docs_settings_test.go, cmd/apogee/settingsedit.go, cmd/apogee/settingsedit_test.go, docs/manual/configuration.md
**Read first:** internal/domain/uiprefs.go — UIPrefs, DefaultUIPrefs, Validate, land; internal/config/registry.go — sessions.watch-homes row, validateSpinnerName; internal/config/config.go — uiConfig, fileWatchHomes, applyFile, resolveOptions;
internal/config/config_test.go — wantUIDefault, TestApplyConfigUIPartialKeepsTheOtherDefault, TestApplyConfigUI; internal/config/defaults_test.go — templateMentionsSetting, templateFirstMention;
cmd/apogee/settingsedit.go — settingKeyLine; internal/config/configwrite_scalarsplice.go — ScalarTargetIn

**Tests:** table-driven refusal test per bad shape (`rows: []` refused by `fromFile`; the domain half in uiprefs_test.go); `UIPrefs{}.Validate()` still passes; `UIPrefs.Set` lands the row's `Read` text back to the same value; a project-layer test that a project file stating the key is ignored with the global-only notice; `TestManualDocumentsEverySettingsKey` updated, `TestRegistryKeyClassesArePinned` unchanged; the spelled-out `domain.UIPrefs{Spinner:` literals in config_test.go gain the default rows; `templateMentionsSetting`/`templateFirstMention` and `manualDocumentsSetting` find a three-level key (their negative tests stay green); a `settingKeyLine` test finds the nested `rows:` line; `TestRegistryDefaultsReadBackFromAnEmptyFile` and `TestFieldCarriedByEveryValueRow` stay green; `wantUIDefault` carries the one-row value.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestApplyConfig|TestResolvePrecedence|TestRegistry|TestLayered|TestUIRows|TestTemplate|TestEmbeddedDefault|TestFieldCarried' ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestUIPrefs' ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManualDocumentsEverySettingsKey|TestSettingKeyLine' ./cmd/apogee/`

**Commit:** `feat(config): ui.footer.rows names the footer's rows and their order`

## 4. The frame's row budget follows the footer's row count — ✅ DONE (2026-11-05)

NOTES (2026-11-05): the doc comments of the two new methods open with `frameFixedRows()` / `frameFloorRows()` (call form) rather than the bare name, so the item's constant-free grep (comments included) holds.
NOTES (2026-11-05): table-driven tests whose height list named the floor before any model exists (TestFrameFitsEveryHeightDownToItsFloor, TestAttachLineKeepsTheFrameInsideTheTerminal, TestInputBoxCountsTheDraftRowsItCannotShow, TestInputBoxWindowFollowsTheCaretAt8Rows) read it from a reference model built with the same options (`modelWithOverlayRoomAt(t, 80, 24, opts).frameFloorRows()`); per-case assertions read the case's own model.

**What:** a refactor; the footer is still one row.

**Regression guard.** No constant footer height survives: `footerHeight`, `frameFixedRows` and `frameFloorRows` appear only as method calls or declarations, comments included (the grep below). The adapted sites outside the run pattern — `TestAttachLineKeepsTheFrameInsideTheTerminal`, `TestSettingsPaneFitsEveryWindowItIsDrawnIn`, `TestInputBoxCountsTheDraftRowsItCannotShow`, `TestInputBoxWindowFollowsTheCaretAt8Rows`, `TestTopRuleHairlineRow` — run in Acceptance.

**Goal:** no code reads a constant footer height: the transcript budget, the draft ceiling, pending-image rows, the frame floor, the input box's top and the footer's hit row all derive from one Model method giving the footer's row count; every test that hard-codes one footer row uses that method.

**Approach (assumed at the header base):** replace `footerHeight` with a `Model` method (returning 1 here), and `frameFixedRows` / `frameFloorRows` with methods over it; update `pendingImageRows`, `transcriptBudget`, `draftRowsCeiling`, `inputContentRect` and `footerRowY` (which becomes a first-row accessor), and the mouse_test, model_test, prompteditor_test, settings_test and sessions_test sites.

**Files:** internal/tui/model.go, internal/tui/mouse.go, internal/tui/mouse_test.go, internal/tui/model_test.go, internal/tui/prompteditor_test.go, internal/tui/settings_test.go, internal/tui/sessions_test.go
**Read first:** internal/tui/model.go — footerHeight, frameFixedRows, frameFloorRows, transcriptBudget, draftRowsCeiling; internal/tui/mouse.go — inputContentRect, footerRowY;
internal/tui/sessions_test.go — TestFrameFitsEveryHeightDownToItsFloor

**Tests:** existing tests, adapted; `TestFooterViewIsOneFramelessLine` asserts against the method.

**Acceptance:**
- `go build ./internal/tui/`
- `! grep -nE '\b(footerHeight|frameFixedRows|frameFloorRows)\b([^(]|$)' internal/tui/*.go`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFooter|TestClick|TestFrame|TestTranscriptBudget|TestDraft|TestAttachLine|TestSettingsPaneFits|TestInputBox|TestTopRule' ./internal/tui/`

**Commit:** `refactor(tui): the frame's row budget reads the footer's row count`

## 5. Paint the configured footer rows — ✅ DONE (2026-11-05)

NOTES (2026-11-05): `footerRow(row, w)` fits one configured row (its segments in the row's order; only the `mode` row gets the marker and the offline word); `footerContent(w)` joins every row's paint (new `footerRowContent(row, w)`) with newlines, so its flat-text callers stay unchanged; `footerLines()` parses `Options.UI.FooterRows` (a refused value falls back to the default) and `footerModeRow()` names the marker's row. A row without `mode` is fitted by `footerFit` with an empty marker, so it keeps one blank cell before the trailing margin (footerfit.go untouched).
NOTES (2026-11-05): `footerRowY()` became `footerRowY(row)` (the row-th footer row's screen row); `footerModeHit` checks `footerRowY(footerModeRow())`. pointershape.go needed no edit: `pointerShapeAt` asks `footerModeHit`, so it takes the row from there (covered by the new mouse test).
NOTES (2026-11-05): the `ui.footer.rows` arm in `settingsApplyLocal` calls `m.layout()` itself (guarded by `m.ready`) instead of leaving the height change to the repaint tail, so `applyReloaded` called outside Update (the editor return trip, and the test) re-lays the frame too.
NOTES (2026-11-05): the empty-value test asserts an unset key paints exactly what `domain.DefaultFooterRows` paints (today the one-row shape), not a hard-coded one-row spelling, so it follows the default when item 6 moves it.
NOTES (2026-11-05): consequential edit — internal/tui/footerfit_test.go: made necessary by `footerRow` taking a row index (`m.footerRow(0, w)` in TestFooterSpanTableAgreesWithThePaintedRow).
NOTES (2026-11-05): consequential edit — cmd/apogee/wire_settings_test.go: made necessary by the new renderer-applied key (`settingKeysAppliedByTheRenderer` says a key added to `settingsApplyLocal` belongs there; the list exempts it from TestEveryEditableSettingKeyHasAnApply, which already skips it as non-Editable).
NOTES (2026-11-05): consequential edit — docs/manual/configuration.md: made necessary by the live apply — the `ui.footer.rows` paragraph now says a saved layout redraws the running session's footer at once.

**What:** Recast at the regression check (2026-11-05). Depends on items 1–4.

**Regression guard.** the TUI-local arm for `ui.footer.rows` lives in internal/tui `settingsApplyLocal` (re-running `m.layout()` as show-scrollbar does), never in cmd/apogee/wire_settings.go: it lands the value through `m.opts.UI.Set(path, value)` (item 3's arm), `settingsTable` gets no entry, and a hand-edit re-read applies live instead of `cannotApply`, with a test; item 9 claims `ui.footer.colors` the same way. `footerContent(w)` keeps returning the joined footer (all rows) and a new per-row painter carries the rows, so its flat-text callers (actuation_test.go, e2e_test.go, effort_test.go, heartbeat_test.go, picker_test.go) stay unchanged.

**Goal:** the TUI paints one footer row per `m.opts.UI` footer row, each fitted on its own, the mode marker (with `offline` to its left) at the right edge of the row naming `mode`; the mode click hits that row only; the transcript gives up one row per extra footer row; a live `/settings` edit of the rows re-lays the frame; an empty rows value paints today's one-row shape.

**Approach (assumed at the header base):** `footerRow` becomes per-row and `footerView` joins the rows; the footer row-count method from item 4 reads the configured count; `footerModeHit` and `pointerShapeAt` take the row from the layout; a TUI-local arm for the key in internal/tui `settingsApplyLocal` re-lays the frame with `m.layout()`. layout.md "What it is" and the "frame's own floor" paragraph state the row rule.

**Files:** internal/tui/model.go, internal/tui/mouse.go, internal/tui/pointershape.go, internal/tui/settingsapply.go, internal/tui/settingsapply_test.go, internal/tui/model_test.go, internal/tui/mouse_test.go, internal/tui/mode_test.go, layout.md
**Read first:** internal/tui/model.go — footerRow, footerContent, footerModeSpan, footerLeft, footerView; internal/tui/mouse.go — footerModeHit, footerRowY, handleFooterModeClick;
internal/tui/settingsapply.go — settingsApplyLocal, settingsApplyLive, settingsApplied; internal/tui/settingswatcher.go — applyReloaded; cmd/apogee/settingsedit.go — externalEdit.changed, appliedValue

**Tests:** two-row Options at 60, 80 and 140 columns; at 80 columns a long workdir and a long branch keep the git state and the effort word; mode click on the marker's row opens the mode picker and on the other row does not; transcript height drops by one; a hand-edit re-read of `ui.footer.rows` (driven through `applyReloaded`, not a pane commit) re-lays the frame and is not answered `cannotApply`.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFooter|TestClick|TestSettings' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestApplySetting|TestSettingsTable|TestEveryEditableSettingKeyHasAnApply' ./cmd/apogee/`

**Commit:** `feat(tui): paint the footer in the rows ui.footer.rows names`

## 6. Default to the two-row footer — ✅ DONE (2026-11-05)

NOTES (2026-11-05): re-derived from the assumption that only cmd/apogee followed the flip. internal/tui's unit tests build models with a zero `FooterRows`, which now reads as two rows, so 45 of them failed. The harness's `testUIPrefs` (internal/tui/model_test.go) now pins the one-row shape (`testFooterOneRow`), and `footerFactsModel`, `TestFooterRowsEachTakeATranscriptRow` and `TestFooterTwoRowsKeepTheGitStateAndTheEffortWordAt80` (mode_test.go) name that shape instead of `""`. `TestFooterUnsetRowsPaintTheDefaultLayout`'s comment now names the two-row default.
NOTES (2026-11-05): re-derived from the assumption that the default lived only where the plan's Files name it. internal/domain/uiprefs_test.go pinned the old default: `TestUIPrefsFooterRowsDefaultToOneRow` is renamed `TestUIPrefsFooterRowsDefaultToTwoRows` and asserts the two-row rows.
NOTES (2026-11-05): internal/config/registry_test.go was left unchanged. The `ui.footer.rows` registry row declares no Default, so TestRegistryDefaults* pass untouched. TestUIRowsLoadFromTheGlobalFile gained a one-row case, because its two-row case now equals the default and no longer tells a written value from it.
NOTES (2026-11-05): e2e footer helpers. `footerRowIndex(f, line)` (e2e_hostile_test.go) now finds the footer by position: the `footerRows` (2) rows directly above the ▁ hairline. `footerRow` returns the upstream row (mode, host, model), and the new `footerWorkspaceRow` returns the workdir/git/spend row. e2e_gitstatus reads the workspace row. assertModelSegmentIsNotRed reads the upstream row and assertWorkdirCellIsNotRed reads the workspace row.
NOTES (2026-11-05): golden redaction. The new `footerWorkspaceRedaction` (e2e_delegation_test.go) replaces the whole row above the hairline with `<footer workspace row>`, because the temp workdir's length decides how much of that row is stated. goldenRedactions and hostileRedactions both carry it, and the 20 goldens were re-recorded with `-update`.
NOTES (2026-11-05): re-derived TestE2ESmokeInProcess step 9. At 60 columns the upstream row no longer drops anything, since the workspace moved to its own row. The old "wide states more separators than narrow" claim is now "the narrow upstream row states as many separated facts as the wide one", and the wide workspace check reads the workspace row.
NOTES (2026-11-05): two driven tests needed room for the extra footer row. `narrowHostileSize` grows from 25 to 26 rows so the approval pane still shows two argument rows (TestE2EHostileWrapsUnderItsOwnIndent). In TestE2ESettingsTypeToFilter, `settingsOtherRow` is now `sub-agents-choice`, because `currency` fell off the settings pane's first screen at 140x30.
NOTES (2026-11-05): `idleRuleFromBottom` goes from 7 to 8. Its comment now says t17 records the rule on row 23, and the synthetic chrome in TestE2ESubAgentViewRuleAnchor/RuleMissing gained a second footer row (h 10 → 11).
NOTES (2026-11-05): consequential edit — layout.md: made necessary by the default flip. The frame floor is now nine rows (eight under a one-row footer). Several counts follow it: the pending-image line ("at nine rows and below"), the pane floor ("thirteen rows", twelve under a one-row footer, in four passages), the footer's "unset" sentence, and the two frame sketches, which now draw two footer rows.
NOTES (2026-11-05): consequential edit — internal/tui/approval.go: made necessary by the default flip (the no-pane window is under thirteen rows, twelve under a one-row footer).
NOTES (2026-11-05): consequential edit — internal/tui/model.go: made necessary by the default flip (the no-pane window is under thirteen rows, twelve under a one-row footer).
NOTES (2026-11-05): consequential edit — internal/tui/settings.go: made necessary by the default flip (the no-pane window is under thirteen rows, twelve under a one-row footer).
NOTES (2026-11-05): consequential edit — cmd/apogee/e2e_approval_test.go: made necessary by the default flip. The 140-column rationale comment now speaks of a one-row footer. The same comment fix lands in e2e_livestate_test.go's liveStateSize.
NOTES (2026-11-05): TestArmsLeaveLayoutToTail fails on the clean tree at 5c6da57e (item 5). The `m.layout()` call at internal/tui/settingsapply.go:489 has no `// geometry:` comment. That is item 5's defect, not this item's, and is reported as FOLLOW-UP.

**What:** Depends on item 5.

**Regression guard.** Every `footerRow`/`footerRowIndex` caller reads the row holding the segment it tests (`grep -n 'footerRow' cmd/apogee/*_test.go`), e2e_livestate_test.go's mode-row checks included; `idleRuleFromBottom` (e2e_subagent_view_test.go:378) becomes 8, with its comment and the t17 golden following; `wantUIDefault` and the spelled-out `want domain.UIPrefs` literals in config_test.go follow the flip.

**Goal:** the default of `ui.footer.rows` is `[[host, model, effort, mode], [workdir, git, spend]]` in `domain.UIPrefs`, the starter template and configuration.md; the whole-frame goldens and the e2e footer helpers follow the two rows; layout.md's frame floor counts the default.

**Approach (assumed at the header base):** flip the default; re-record `cmd/apogee/testdata/frames/*.txt` with `-update`; extend `hostileRedactions` (and the delegation redaction) to the workspace row; make `footerRow` (e2e_smoke_test.go), `footerRowIndex` (e2e_hostile_test.go) and the e2e_approval_test check read the row holding the segment they test.

**Files:** internal/domain/uiprefs.go, internal/config/defaults/config.yaml, internal/config/registry_test.go, internal/config/config_test.go, docs/manual/configuration.md, layout.md, cmd/apogee/testdata/frames/, cmd/apogee/e2e_smoke_test.go, cmd/apogee/e2e_hostile_test.go, cmd/apogee/e2e_delegation_test.go, cmd/apogee/e2e_approval_test.go, cmd/apogee/e2e_gitstatus_test.go, cmd/apogee/e2e_livestate_test.go, cmd/apogee/e2e_subagent_view_test.go
**Read first:** cmd/apogee/e2e_smoke_test.go — footerRow; cmd/apogee/e2e_livestate_test.go — TestE2ELiveStateFollowsTheRunningSession; cmd/apogee/e2e_subagent_view_test.go — idleRuleFromBottom, lastRuleRow;
cmd/apogee/e2e_hostile_test.go — footerRowIndex, hostileRedactions; internal/config/config_test.go — wantUIDefault, wantDefaults

**Tests:** re-recorded goldens; the e2e tests above; `TestE2ELiveStateFollowsTheRunningSession` and the run-view tests (`assertLastBodyRow`) on two rows; `TestApplyConfig` against the flipped `wantUIDefault`.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRegistryDefaults|TestApplyConfig' ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2E' ./cmd/apogee/`

**Closes:** footer-overflow-layout

**Commit:** `feat(tui): the footer defaults to two rows, upstream then workspace`

## 7. Git counts carry their meaning's tone — ✅ DONE (2026-11-05)

NOTES (2026-11-05): footerSpan's `bright` bool became a `footerTone` enum (muted / warning / success); theme.go gained `footerWarning` and `footerSuccess` styles in themeStyles, read through `theme.footerToneStyle`. The theme's raw `mutedBright` colour had no other reader, so it was deleted.
NOTES (2026-11-05): the mode_test.go subtest "an elided branch keeps its counts' tone" (in the branch-elision test) used the removed `footerBrightOpener`, so it now checks the per-role openers. layout.md's "What colour means" now also names the new warning/success distinctness guard.

**What:** Depends on item 1.

**Regression guard.** item 1's per-segment span table is the single carrier — item 2 elides inside it and item 7 retones it, no second span type. Every comment and doc line that names muted-bright / brighter for the footer's git counts follows the new tones (`grep -n -i 'bright' internal/tui/footerfit.go internal/tui/model.go internal/tui/theme.go layout.md | grep -i 'git\|count\|footer'`). Supersedes layout.md:1948 and CHANGELOG.md:48 (0.32.0, released: "a step brighter … no role of its own") per the header's ratified colour call; the item's `[Unreleased]` entry says it replaces that tone.

**Goal:** a non-zero `Δ` and `↓` paint in the `warning` role, a non-zero `↑` in `success`, zero counts stay `muted`; under each built-in scheme the three tones are distinct from every `mode-*` role and from `error`; under an Ascii colour profile the row's text is unchanged.

**Approach (assumed at the header base):** `footerSpan` carries a role instead of the `bright` bool; `gitFooterSpans` sets it; `footerGit` paints by role via theme.go styles.

**Files:** internal/tui/footerfit.go, internal/tui/model.go, internal/tui/theme.go, internal/tui/footerfit_test.go, internal/tui/mode_test.go, internal/scheme/builtins_test.go, layout.md
**Read first:** internal/tui/footerfit.go — footerSpan, gitFooterSpans; internal/tui/model.go — footerGit; internal/tui/theme.go — theme.mutedBright, newTheme;
internal/tui/mode_test.go — TestFooterTonesOnlyNonZeroGitCounts; internal/scheme/builtins_test.go — TestBuiltinSchemesKeepModeColorsDistinct

**Tests:** `TestFooterTonesOnlyNonZeroGitCounts` becomes per-role; a per-built-in-scheme test asserts each span's SGR; an Ascii-profile test asserts the plain text; a builtins test asserts `warning`/`success` differ from every `mode-*` and `error`.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFooter|TestGitFooterSpans' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/scheme/`

**Commit:** `feat(tui): footer git counts take warning and success tones`

## 8. A `footer-accent` role for the model

**What:** Recast at the regression check (2026-11-05). Depends on item 7.

**Regression guard.** the item yields to ADR 0040 decision 3 — an omitted `footer-accent` in a custom scheme inherits the built-in dark value silently, as every omitted role does, and its test asserts no warning; every stated role count moves from 29 to 30 by the rule "every prose or const count of the scheme's roles" (`grep -rn '29 \(semantic \)\?roles\|29 of them\|29 keys\|29 today\|wantRoleCount' layout.md internal .capcom/knowledge/glossary .capcom/knowledge/decisions/adr-0040*`), with the glossary Note `.capcom/knowledge/glossary/glossary-safety/color-scheme.md` added to Files and the roleTable entry plus the count const named in Tests.
Re-check (2026-11-05): ADR 0040 states the count (adr-0040-amend-0815.md:10 "**29 keys** today, pinned by `wantRoleCount`"; adr-0040-consequences.md:6 "29 today"); `footer-accent` is recorded as the 30th key by a dated amendment named on ADR 0040's Status line (AGENTS.md amendment rule), which supersedes those two counts: the grep's ADR 0040 hits are answered by that amendment, not rewritten.

**Goal:** the scheme role `footer-accent` exists in the role table and in `dark.yaml` and `light.yaml`, distinct from every `mode-*` role and `error`; the footer paints the model name in it; a custom scheme without it inherits the built-in dark value silently, as every omitted role does.

**Approach (assumed at the header base):** add the field to `scheme.Scheme` and the role table; a theme.go style; the painter tones the model segment's span. The value sits between `muted-bright` and `user-text` in brightness.

**Files:** internal/scheme/scheme.go, internal/scheme/schemes/dark.yaml, internal/scheme/schemes/light.yaml, internal/scheme/builtins_test.go, internal/scheme/scheme_test.go, internal/tui/theme.go, internal/tui/model.go, internal/tui/mode_test.go, layout.md, .capcom/knowledge/glossary/glossary-safety/color-scheme.md, .capcom/knowledge/decisions/adr-0040.md, .capcom/knowledge/decisions/adr-0040/ (the dated amendment)
**Read first:** internal/scheme/scheme.go — Scheme, buildRoles, parseInto; internal/scheme/scheme_test.go — roleTable, TestRoleTableCoversEveryRole, TestParsePartialFileInheritsDefaults;
internal/scheme/builtins_test.go — TestBuiltinSchemesStateEveryRole, TestBuiltinSchemesKeepModeColorsDistinct; internal/tui/theme.go — theme, newTheme; internal/tui/model.go — footerLeft

**Tests:** `TestBuiltinSchemesStateEveryRole` and `TestRoleTableCoversEveryRole` cover the role — its `roleTable` entry added and `wantRoleCount` moved from 29 to 30; a distinctness test; a footer test per built-in scheme for the model span; a Parse test that a custom scheme omitting `footer-accent` takes dark's value with no warning.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/scheme/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFooter' ./internal/tui/`

**Commit:** `feat(scheme): footer-accent role tones the footer's model`

## 9. The `ui.footer.colors` key

**What:** Recast at the regression check (2026-11-05). Depends on items 3 and 8.

**Regression guard.** the colour overrides ride the same comparable way (e.g. a fixed array indexed by segment) on the path support item 3 built. Item 9 claims `ui.footer.colors` as a TUI-local settings arm in the settings table, as item 5 does the rows, so a hand-edit re-read applies live instead of `cannotApply` (cmd/apogee/wire_settings.go). internal/scheme exports its role names (a `RoleNames()` and a role lookup for theme.go); domain cannot import scheme (scheme.go:12 imports domain), so the role check runs in internal/config at load, beside `UIPrefs.Validate`. Supersedes, for this key only, ADR 0040 decision 8 (forgiving load, restated at internal/domain/uiprefs.go:43-48) per the header's ratified Overrides call.
Re-check (2026-11-05): `ui.footer.colors` extends item 3's parseable `Read` and `UIPrefs.Set` arm, its TUI-local arm lives in internal/tui `settingsApplyLocal` (not cmd/apogee/wire_settings.go); and the item adds a dated amendment to ADR 0040 (a `> **Amended 2026-11-05 (docs/plans/2026-11-05 - 02 - footer-rows-and-colour-plan.md).**` blockquote under decision 8 in .capcom/knowledge/decisions/adr-0040/adr-0040-decision.md, named on the record's Status line) saying role names in `ui.footer.colors` are refused at load because the role set is compiled in, unlike scheme files, which stay forgiving. A live colors edit changes no scheme, so overrides resolve to colour values from `m.opts.colorScheme()` at newModel, `applyColorScheme` AND the colors arm (or at paint), held off `themeStyles` (TestThemeStylesAreWrittenOnlyByNewTheme, theme_test.go:439); a whole `scheme.Scheme` never goes on the theme header (TestThemeHeaderFitsInAKilobyte, theme_test.go:408).

**Goal:** `ui.footer.colors` maps `host | model | effort | workdir | branch | spend` to a scheme role name and the footer paints each named segment in that role; load refuses, naming the key and value, an unknown segment, `mode` or `offline`, and an unknown role; configuration.md documents it and layout.md "What "colour" means" states the footer's colour rules (defaults, overrides, the mode marker stays loudest).

**Approach (assumed at the header base):** a `colors:` map under the `footer:` block, a `KindStructured` row written like item 3's (no field), refused at load in internal/config against `scheme.RoleNames()`; the TUI resolves each override to a colour value at newModel, `applyColorScheme` and the colors arm (see the guard).

**Files:** internal/config/config.go, internal/config/registry.go, internal/config/defaults/config.yaml, internal/config/config_test.go, internal/domain/uiprefs.go, internal/domain/uiprefs_test.go, internal/scheme/scheme.go, internal/scheme/scheme_test.go, internal/tui/theme.go, internal/tui/model.go, internal/tui/settingsapply.go, internal/tui/settingsapply_test.go, internal/tui/mode_test.go, docs/manual/configuration.md, layout.md, .capcom/knowledge/decisions/adr-0040.md, .capcom/knowledge/decisions/adr-0040/adr-0040-decision.md
**Read first:** internal/tui/settingsapply.go — settingsApplyLocal, applyColorScheme; internal/tui/theme.go — theme, newTheme; internal/tui/tui.go — Options.colorScheme; internal/scheme/scheme.go — roleKeys, fieldIndex;
internal/config/config.go — uiConfig, resolveOptions; internal/config/registry.go — sessions.watch-homes row; cmd/apogee/settingsedit.go — appliedValue; internal/tui/theme_test.go — TestThemeStylesAreWrittenOnlyByNewTheme

**Tests:** refusal table; an override test per built-in scheme; an Ascii-profile test that overrides change no text; `scheme.RoleNames()` lists every role the table declares; `UIPrefs.Set` lands the colors row's `Read` text back to the same value; a hand-edit re-read of `ui.footer.colors` applies live (repainting in the current scheme's role colours with no scheme change) and is not answered `cannotApply`; `TestThemeStylesAreWrittenOnlyByNewTheme` and `TestThemeHeaderFitsInAKilobyte` stay green.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestApplyConfig|TestRegistry|TestUIRows|TestTemplate' ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/scheme/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFooter|TestTheme|TestSettings' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestUIPrefs' ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManualDocumentsEverySettingsKey|TestApplySetting|TestSettingsTable|TestEveryEditableSettingKeyHasAnApply' ./cmd/apogee/`
- `capcom check`

**Closes:** footer-segment-colour

**Commit:** `feat(config): ui.footer.colors sets a footer segment's role`

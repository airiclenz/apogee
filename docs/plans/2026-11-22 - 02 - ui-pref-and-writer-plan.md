# A UI pref declared once, applied once, written through one writer — plan

**Goal:** every `ui.*` key is declared in one domain table from which landing, key lists, defaults and per-key validation derive; the TUI applies and persists a key through one module and one persist door; the config writer splices any registry path (depth 1–3), so the side-panel splice engine and the two panel-only `SettingsHost` methods go.
**Date:** 2026-11-22
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 4d5d8263

**Sources:**
- `.capcom/issues/ui-pref-declared-thrice.md`, `.capcom/issues/ui-pref-apply-restated.md`, `.capcom/issues/side-panel-writer-split.md`
- `docs/reviews/architecture-review-2026-11-10.html` (#2, #3, #8)
- ADR 0035 D2 — splice contract (node-guided text splice, re-parse, `sameApartFrom`, atomic rename) stays binding: `capcom know show "ADR 0035" --with-children`
- ADR 0011 / `internal/tui/doc.go` — Model copied by value; tui never imports `internal/config`

**Ratified design calls** (owner, 2026-11-22):
- **Panel writes:** sections/folded stay a merge in the writer: the gesture sends a one-section delta through `Write`; a per-key merge hook applies it to the file's list under the edit lock (a partial hand-kept list stays partial; no lost update).
- **Write result:** `SettingsHost.Write(key, value)` returns `(landed string, err error)` — the key's canonical text as the file now resolves it; `SettingsHost` drops to 6 methods.
- **Width type:** a `domain.SidePanelWidth` value type (stated | unset) replaces the `0 = unset` int.
- **Delta spelling:** sections `"<section>: <mode>"`; folded `"<section>: folded"` or `"<section>: open"`.
- **Footer keys** (`ui.footer.rows`, `ui.footer.colors`) stay unwritable by gesture and pane (structured, edited in the file).

**Standing requirements:**
- skills: coding-standards
- Machine rule (Raspberry Pi 5, 8 GB): never `go test -race`/`-cover` over a whole heavy package or a pattern; one package + one `-run` with `GOMEMLIMIT=2GiB`; full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test`; never two test runs at once. Forward to every sub-agent.
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`); never a `strings.Builder` held by value in the Model.
- The starter template `internal/config/defaults/config.yaml` stays hand-written documentation; `wantUIDefault` in `internal/config/config_test.go` stays hand-spelled.
- An authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- `switchColorScheme` (`internal/tui/colorscheme.go`) keeps its persist-first order and its own wording.
- Making the footer keys gesture- or pane-writable; any change to the file pass's "unvalidated typed copy" rule (`internal/config/keyfield.go` header).
- The possible gap that `externalEdit.refresh()` re-baselines over an undelivered hand edit to other keys (unconfirmed; not this plan).
- Non-`ui.*` keys' precedence/source questions (`setting-source-split`, `config-read-entries`).

**Regression check (2026-11-22, 4d5d8263):**
- 1: guard folded (writer decision: "default text" meaning; doc.go file map; color-scheme `""` rule)
- 2: guard folded; yields to registry.go's structured-row "No Default" comment
- 3: guard folded
- 4: guard folded (writer decision); yields to ADR 0043 amendment 0916 (`adr-0043-amend-0916`)
- 5: guard folded (writer decision: delta-only at `SaveConfigSetting`)
- 6: guard folded (writer decision: Files)
- 7: guard folded (writer decision: grep scope)
- 8: guard folded (writer decision: grep scope)
- 9: guard folded
- 10: guard folded

## 1. Declare every ui.* key once, in a domain table — ✅ DONE (2026-11-22)

NOTES (2026-11-22): Validate now runs the per-key refusals in template (table) order — spinner, glyphs, stall-after, tools-fold-over, notify, side-panel show/width/sections/folded, footer rows — so a block with two bad fields reports a different first refusal than before; each single refusal's sentence is unchanged.
NOTES (2026-11-22): a row's format func spells the field as the registry row reads it today (width 0 → "44", folded "" → "[]", stall-after via Duration.String); DefaultUIPrefs is built by landing each row's "" and panics if a row's "" ever fails to land (an invariant pinned by TestDefaultUIPrefsPinsTheShippedValues).
NOTES (2026-11-22): reworded the "seventeen keys" comment on TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase (test body unchanged) per the no-key-count-prose rule.

**What:**
**Goal:** `internal/domain` holds one ordered table covering all 21 `ui.*` keys; `UIPrefs.Set`, `UIKeys()` (the 17 template-list keys, order unchanged), a new `AllUIKeys()` (all 21), `DefaultUIPrefs()` and `UIPrefs.Validate()` derive from it; a new `ValidateUIKey(key, text string) error` checks one key without relying on any other field's zero value.
**Approach (assumed at the header base):**
- New `internal/domain/uikeys.go`: unexported table in template order; each entry carries path, whether it is on the template list (structured keys are not), a land func with the key's own `""` rule (Notify `""` → default; width `""` → unset; ColorScheme stored as written; structured keys land their `Spell*` canonical spelling), a format func, the default text, and a per-key validator over `UIPrefs`. Go needs one element type: an interface or func-valued struct, not generics per row.
- `land`'s switch, the `uiKeys` slice and the `DefaultUIPrefs` literal go; `Validate` folds the per-key validators. Footer colour roles stay validated in config (`footerColorRoles`; `internal/scheme` imports domain).
- `ValidateUIKey`: land `text` onto a `DefaultUIPrefs()` copy and run only that key's validator.
- `internal/domain/doc.go`: the "sixteen keys" sentence states no count (rule: no prose in `internal/domain` spells the ui key count — `grep -rn 'keys in template order' internal/domain`).
**Regression guard.** A key's "default text" in the domain table is the registry row's documented text (ui.stall-after "120s", ui.side-panel.width "44"), empty for the four structured keys (footer rows/colors, side-panel sections/folded); it is never derived by formatting DefaultUIPrefs() (which would give width unset and stall "2m0s"). Items 2 and 9 use this same meaning — state it once in item 1 and refer to it.
Also: `internal/domain/doc.go`'s file map gains a `uikeys.go is …` line beside `uiprefs.go` (`TestDocMapNamesEveryFile`). ColorScheme's land rule is spelled exactly: `""` lands `DefaultColorSchemeName`, any other text as written (today's `land`); `Validate` keeps exactly today's ten refusals, every zero field valid.
**Files:** internal/domain/uikeys.go, internal/domain/uiprefs.go, internal/domain/uiprefs_test.go, internal/domain/doc.go
**Read first:** internal/domain/uiprefs.go — UIPrefs.land, UIPrefs.Validate, UIPrefs.Set, DefaultUIPrefs, uiKeys, landBool; internal/domain/uiprefs_test.go — uiKeyCases, TestUIPrefsSetLandsEveryKey, TestUIPrefsValidateSpeaksTheParsersSentences, TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase;
  internal/domain/doc.go — package file map; internal/domain/docmap_test.go — TestDocMapNamesEveryFile; internal/domain/sidepanelprefs.go — ParseSidePanelWidth, validSidePanelWidth, ParseSidePanelShow
**Tests:** `uiprefs_test.go`: `DefaultUIPrefs()` equals a hand-spelled literal (pins today's values); every `UIKey*` const appears in `AllUIKeys()` exactly once; `UIKeys()` unchanged (existing `TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase`); `ValidateUIKey` accepts/refuses for each enum key; `Set` round-trips each key's documented values; the table's default text is `"120s"` for stall-after, `"44"` for width and `""` for the four structured keys; existing `TestUIPrefsSetLandsEveryKey`, `TestUIPrefsValidateSpeaksTheParsersSentences` and `TestDocMapNamesEveryFile` pass unchanged.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/domain/`
**Commit:** `refactor(domain): declare every ui key once in a per-key table`

## 2. The registry's ui.* rows read the domain table — ✅ DONE (2026-11-22)

NOTES (2026-11-22): the six named ui validators (spinner, glyphs, notify, side-panel show, side-panel width, stall-after) plus validateToolsFoldOver are replaced by one factory, uiKeyValidator(key), that calls domain.ValidateUIKey — one hook shape instead of seven near-identical functions; fold-over was not named by the item but is the same ui-row duplicate, and ValidateUIKey gives the same answer and sentence as each parser it replaces.
NOTES (2026-11-22): new accessor is domain.UIDefaultText(key) (the plan's example name); an unknown or structured key reads "". All 17 template-listed rows' Default (ui.color-scheme included, formerly scheme.DefaultName) now read it.
NOTES (2026-11-22): consequential edit — internal/config/config.go: made necessary by the ui.stall-after row reading its Default from domain.UIDefaultText; the defaultStallAfterText const had no other user and was deleted as dead code.
NOTES (2026-11-22): internal/config/keyfield.go named in Files needed no change: the uiField file projection stays in config as the item requires, and nothing there restated a default or a validator.

Depends on item 1.

**What:**
**Goal:** no `internal/config` code constructs `domain.UIPrefs{X: v}` to validate one key; every `ui.*` registry row's `Default` string equals the domain table's default text for that key (width reads `"44"`).
**Approach (assumed at the header base):**
- `validateSpinnerName`, `validateGlyphSetName`, `validateNotifyMode`, `validateSidePanelShow` (registry.go) call `domain.ValidateUIKey(key, v)`; `validateSidePanelWidth` and `validateStallAfter` may too where it removes a duplicate parse.
- Each `ui.*` row's `Default` comes from an exported domain accessor for a key's default text (e.g. `domain.UIDefaultText(key)`); width formats through its formatter, never stringifies the zero.
- The file projection (`uiField`'s `file` func, `uiConfig` yaml types) stays in config (keyfield.go rule).
**Regression guard.** "Default text" is item 1's meaning: stall-after reads the hand-written `"120s"` (never `format(DefaultStallAfter)` → `"2m0s"`), width `"44"`. The four structured rows keep `Default ""` — the item yields to registry.go's structured-row comment ("No Default: the default lives in domain.DefaultUIPrefs and the starter template"), so `readsAlike` (which refuses a KindStructured default through `renderSettingValue`) never sees one. The Default-equality sweep asserts the 17 `domain.UIKeys()` rows; the 4 structured rows are asserted once each to keep `Default ""`. The Goal's no-`domain.UIPrefs{X: v}` clause is checked in Acceptance.
**Files:** internal/config/registry.go, internal/config/keyfield.go, internal/config/registry_test.go, internal/domain/uikeys.go
**Read first:** internal/config/registry.go — KeyRegistry ui.* rows, validateSpinnerName, validateGlyphSetName, validateNotifyMode, validateSidePanelShow, validateSidePanelWidth; internal/config/registry_test.go — TestRegistryDefaultsReadBackFromAnEmptyFile, readsAlike, TestRegistryDefaultsFollowTheStarterTemplate, TestUIRowsLandThroughUIPrefsSet;
  internal/config/configwrite_scalar.go — validateSettingValue, renderSettingValue; internal/config/keyfield.go — uiField, uiStallAfter; cmd/apogee/settingsrows.go — row Default fallback
**Tests:** `registry_test.go`: a sweep over `domain.AllUIKeys()` asserting each has exactly one registry row; each `domain.UIKeys()` row's `Default` equals the domain default text, each of the 4 structured rows keeps `Default ""`; existing `TestUIRowsLandThroughUIPrefsSet`, `TestRegistryDefaultsFollowTheStarterTemplate` (`"120s"`) and the absent-file Read-equals-Default test pass.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/config/ ./internal/domain/ && test -z "$(grep -rn 'domain\.UIPrefs{' internal/config --include='*.go' --exclude='*_test.go')"`
**Commit:** `refactor(config): derive ui registry defaults and validators from the domain table`

## 3. The scalar splice reaches any registry depth — ✅ DONE (2026-11-22)

NOTES (2026-11-22): ScalarTarget's new chain/found/missing fields are unexported (only this package reads them); Parent/ParentKey/ParentBody/ParentNull stay the immediate parent, so the side-panel engine is untouched in code. childIndent is gone, folded into scalarInsertion's deepest-present-block rule; nestedSetting wraps the missing blocks at listIndent per level (sidePanelInsertion's spelling).
NOTES (2026-11-22): internal/config/projectwrite.go needed no edit — the project pair goes through writableKey, so dropping the depth refusal there covers it; the file is not in FILES.
NOTES (2026-11-22): consequential edit — internal/config/doc.go: made necessary by removing the scalar writer's depth limit (file map said the side-panel block sat "a level deeper than that writer reaches").
NOTES (2026-11-22): consequential edit — internal/config/configwrite_keysource.go: made necessary by removing the scalar writer's depth limit (header said "one level deeper than the scalar writer reaches").
NOTES (2026-11-22): consequential edit — cmd/apogee/wire_options.go: made necessary by removing the scalar writer's depth limit (settingsHost.Write comment said the side-panel keys sit past the scalar writer's reach).

**What:**
**Goal:** `SaveConfigSetting`/`ResetConfigSetting` (and the project pair) write and reset an Editable scalar at any registry path depth 1–3, creating absent parents and removing an emptied parent chain; `writableKey` has no depth refusal; the scalar round-trip sweep covers every Editable key including `ui.side-panel.show` and `ui.side-panel.width`.
**Approach (assumed at the header base):**
- `ScalarTarget.Parent string` becomes a parent chain plus the deepest existing ancestor and the missing suffix; `ScalarTargetIn` walks every segment, applying the flow-style / null / non-mapping refusals at each level.
- `scalarInsertion` emits the missing suffix as nested lines indented under the deepest existing ancestor (absent top level goes below `commentedExampleBlockEnd`); generalises `sidePanelInsertion`. `spliceScalarDelete` cascades upward while an ancestor body empties; generalises `spliceSidePanelDelete`.
- Delete `scalarPathDepth` and its stale "every registry row is within it" comment; drop the depth skip in `TestSpliceScalarSettingRoundTripsEveryEditableKey`.
- The side-panel engine (`configwrite_sections.go`) is untouched here; the host still routes panel keys to it.
**Regression guard.** `ScalarTarget` keeps `Parent`/`ParentKey`/`ParentBody`/`ParentNull` as the IMMEDIATE parent (the side-panel engine's `sidePanelSpotIn`, `sidePanelInsertion` and `spliceSidePanelDelete` build and read them); the chain, deepest-ancestor and missing-suffix fields are added beside them. Prose rule, not one site: no comment names `scalarPathDepth` or says the scalar writer stops at two segments — `grep -rn 'scalarPathDepth\|three segments deep\|one block down\|nested too deeply' --include='*.go' internal/config cmd/apogee` returns nothing (comment-only edits in the side-panel engine and its callers' tests).
**Files:** internal/config/configwrite_scalar.go, internal/config/configwrite_scalarsplice.go, internal/config/configwrite_scalar_test.go, internal/config/projectwrite.go, internal/config/configwrite_sections.go (comments only), internal/config/configwrite_sections_test.go (comment only), internal/config/registry.go (comment only), cmd/apogee/settingsedit.go (comment only), cmd/apogee/wire_options_test.go (comments only)
**Read first:** internal/config/configwrite_scalarsplice.go — ScalarTarget, ScalarTargetIn, scalarInsertion, childIndent; internal/config/configwrite_scalar.go — writableKey, scalarPathDepth, spliceScalarDelete; internal/config/configwrite_sections.go — sidePanelSpotIn, sidePanelInsertion, spliceSidePanelDelete;
  internal/config/configsplice.go — scalarLineParts, sameApartFrom, zeroConfigPath; internal/config/configwrite_scalar_test.go — TestSpliceScalarSettingRoundTripsEveryEditableKey, TestSpliceScalarSettingNestedBlockShapes, TestConfigWriteSettingRefusals; cmd/apogee/settingsedit.go — settingKeyLine, nestedKeyLine
**Tests:** `configwrite_scalar_test.go`: 3-deep set into (a) no `ui:` block, (b) `ui:` without `side-panel:`, (c) null `side-panel:`, (d) populated block, each keeping comments/neighbours byte-identical; reset of the last child removes `side-panel:` and then `ui:` when emptied; flow-style ancestor refused at depth 2 and 3; the side-panel engine's existing tests pass unchanged.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/config/ && test -z "$(grep -rn 'scalarPathDepth\|three segments deep\|one block down\|nested too deeply' --include='*.go' internal/config cmd/apogee)"`
**Commit:** `refactor(config): splice a scalar setting at any registry depth`

## 4. The writer admits gesture-written structured keys — ✅ DONE (2026-11-22)

NOTES (2026-11-22): validateSettingValue returns a render hook's refusal unwrapped (the domain parsers already lead with "apogee: invalid ui.side-panel.…"), so a refused sections/folded value is not led twice; every other kind keeps the "apogee: %w" wrap.
NOTES (2026-11-22): the four hook functions (renderSidePanelSections/Folded, readBackSidePanelSections/Folded) live in registry.go beside uiKeyValidator, reusing domain.Parse*/Spell*, sidePanelFoldedText and writtenSidePanel* — no new parsing.

Depends on item 3.

**What:**
**Goal:** a registry row can be marked gesture-written; `writableKey` admits a key that is Editable or gesture-written; a KindStructured gesture-written key is rendered as a one-line flow value and verified by read-back through the generic splice; `ui.side-panel.sections` and `ui.side-panel.folded` are gesture-written, the footer keys are not.
**Approach (assumed at the header base):**
- New `Key` field (e.g. `Gesture bool`); Editable keeps gating the pane only.
- Optional per-key render and read-back hooks on `Key`, reusing existing spellers (`domain.SpellSidePanelSections`, `sidePanelFoldedText`) and the `fileConfig` read the side-panel verifiers use — no new parsing. `valueFitsOneLine` accepts a one-line flow sequence/map for KindStructured; `verifyScalarEdit` uses the read-back hook where `scalarAtPath` reports a list of maps as not-set.
- Reset of sections/folded stays refused (as today).
**Regression guard.** Item 4 adds admission plus render/read-back only; its tests drive the whole-list write through the splice internals, and item 5 settles the public meaning.
Also: the render hook is consulted inside `renderSettingValue` — the one place both `validateSettingValue` (run before the file opens) and `scalarSetEdit` reach — so a KindStructured gesture key never falls through to "not a simple value". Yields to ADR 0043 amendment 0916 (`adr-0043-amend-0916`): who may write a key stays the registry row's question — `Gesture` is a second field on the row beside `Editable`, read by `writableKey`, never decided by `Set` or a render hook.
**Files:** internal/config/registry.go, internal/config/configwrite_scalar.go, internal/config/configwrite_scalarsplice.go, internal/config/configedit.go, internal/config/configwrite_scalar_test.go
**Read first:** internal/config/configwrite_scalar.go — writableKey, validateSettingValue, renderSettingValue, scalarSetEdit, verifyScalarEdit, ResetConfigSetting; internal/config/configwrite_scalarsplice.go — valueFitsOneLine; internal/config/configedit.go — scalarAtPath, scalarChangedOnlyAt;
  internal/config/configwrite_sections.go — sidePanelFoldedText, writtenSidePanelSections, verifySidePanelSections; internal/config/registry.go — Key, the ui.side-panel.sections/folded and ui.footer.* rows; internal/domain/sidepanelprefs.go — ParseSidePanelSections, SpellSidePanelSections, SpellSidePanelFolded
**Tests:** whole-value write of sections and folded through the splice internals (`renderSettingValue` + `scalarSetEdit` under the edit, not the public `SaveConfigSetting`) lands canonical one-line text and verifies; a footer key is refused; reset of sections refused with today's wording.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/config/`
**Commit:** `feat(config): write gesture-owned structured keys through the scalar splice`

## 5. Sections and folded merge inside the generic writer — ✅ DONE (2026-11-22)

NOTES (2026-11-22): the merge hook's signature is `merge func(before fileConfig, delta string) (text, landed string, err error)` rather than the approach's `(value string, err error)`: folded's landed (`""` for nothing folded) differs from the text the file carries (`[]`), so the hook answers both; the pre-open delta check is that same hook run against an empty fileConfig (reads nothing of the file, so only the delta can fail), not a second parse.
NOTES (2026-11-22): the delta spellings (`folded`/`open` and the two refusal forms) are constants in internal/config/registry.go beside the hooks; item 7's tui gestures cannot import config, so that item spells the delta on its own side or moves the words to domain.
NOTES (2026-11-22): the ported "a list one entry per line" refusal now reads "one entry per line" (the scalar splice's wording) where the old engine said "one item per line"; settingsHost.Write still routes the side-panel block to SaveSidePanelSetting and discards SaveConfigSetting's landed through a small closure, both left to items 6–7.

Depends on item 4.

**What:**
**Goal:** `SaveConfigSetting(path, key, value)` returns `(landed string, err error)`; for `ui.side-panel.sections` a value `"<section>: <mode>"` and for `ui.side-panel.folded` a value `"<section>: folded|open"` is merged against the file's list under the edit lock with exactly today's `SaveSidePanelSection`/`SaveSidePanelFold` semantics, and `landed` is the resolved canonical list text.
**Approach (assumed at the header base):**
- Optional per-key `merge func(before fileConfig, delta string) (value string, err error)` on `Key`, run inside the `edit()` splice callback. Sections hook = config's `withSectionMode` (file list names the section → that entry's mode only, order and partiality kept; else full resolved list via `ParseSidePanelSections`); folded hook = file names minus the section, plus it when folding, canonical spelling.
- Callers of `SaveConfigSetting` take the new result: `cmd/apogee/wire_verbs.go`, `cmd/apogee/wire_options.go`, config tests.
- Port every `TestSidePanelSections*`/`TestSidePanelFolded*` case in `configwrite_sections_test.go` to the generic entry (new cases in `configwrite_scalar_test.go` or a renamed file); the old engine stays in place until item 7.
**Regression guard.** After this item, a value passed to SaveConfigSetting for a key with a merge hook (ui.side-panel.sections, ui.side-panel.folded) is always the one-section delta; a whole-list spelling is refused with an error naming the expected delta form; any item-4 test that wrote a whole list through SaveConfigSetting is rewritten to deltas. Files must list every cmd/apogee caller (production and test) of SaveConfigSetting the report names, so the Acceptance compiles.
Also: for a merge-hook key the pre-open check validates only the delta (today's `SaveSidePanelSection`/`SaveSidePanelFold` check: parse the one-entry spelling), never the whole-value render — `ParseSidePanelSections` needs brackets; the merged value is rendered and verified inside the splice. Folded `landed` is `domain.SpellSidePanelFolded` of the parsed list (`""` when nothing is folded, as `SaveSidePanelFold` answers today), never the file text (`[]`). Every `SaveConfigSetting(` call site takes the new result — `grep -rn 'SaveConfigSetting(' --include='*.go' .`. The whole-list cases (`TestSidePanelSectionsWholeListWrite`, `TestSidePanelFoldedWholeListWriteAndRefusals`) port to item 4's splice-internal tests, not to `SaveConfigSetting`.
**Files:** internal/config/registry.go, internal/config/configwrite_scalar.go, internal/config/configwrite_scalar_test.go, internal/config/configedit_test.go, cmd/apogee/wire_verbs.go, cmd/apogee/wire_options.go, cmd/apogee/configwatch_apply_test.go, cmd/apogee/wire_settings_test.go
**Read first:** internal/config/configwrite_sections.go — SaveSidePanelSection, withSectionMode, SaveSidePanelFold, saveSidePanelSections, saveSidePanelFolded, SaveSidePanelSetting; internal/config/configwrite_scalar.go — SaveConfigSetting, saveScalar, scalarSetEdit, validateSettingValue; internal/config/configedit.go — edit, verifiedEdit;
  internal/config/configwrite_sections_test.go — TestSidePanelSectionsWriteRefusals, TestSidePanelSectionsReSetWritesNothing, TestSidePanelSectionsWholeListWrite, TestSidePanelFoldedWholeListWriteAndRefusals; cmd/apogee/wire_options.go — settingsHost.Write, SaveTo; cmd/apogee/wire_verbs.go — recordServerChoice;
  internal/config/configedit_test.go — TestConcurrentSaveConfigSettingLandsEveryKey
**Tests:** ported partial-list, absent-list, reorder-kept, fold/unfold, duplicate-name cases pass against `SaveConfigSetting`; `landed` equals the resolved list (folded: `""` when nothing is folded); a bracketed whole-list value for sections or folded is refused naming the delta form.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/config/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSettingsHost' ./cmd/apogee/`
**Commit:** `refactor(config): merge panel section and fold edits in the generic writer`

## 6. SettingsHost.Write returns the landed value — ✅ DONE (2026-11-22)

Depends on item 5.

**What:**
**Goal:** `tui.SettingsHost.Write(key, value string) (string, error)`; every implementation and caller compiles against it; behaviour of every existing write unchanged.
**Approach (assumed at the header base):**
- `settingsHost.Write` (`cmd/apogee/wire_options.go`) returns the `landed` text from config (the side-panel branch returns the value it wrote, until item 7).
- Callers in `internal/tui`: `settingsapply.go` (pane commit, `toggleTaskListFold`, `toggleToolsFold`), `colorscheme.go`, `sidepanel.go` (`setPanelWidth`, `setPanelShow`) take `_, err`.
- `fakeSettingsHost` (`internal/tui/settings_test.go`) returns its recorded value.
**Regression guard.** Files must list every cmd/apogee and internal/tui test caller or implementation of SettingsHost.Write the report names, so the Acceptance compiles.
Also: `fakeSettingsHost` keeps its field `write func(path, value string) error` (and `settingsWriteLog.write` its signature); its `Write` method returns `(value, h.write(path, value))`, so none of the ~17 constructions in other `internal/tui` test files changes. The cmd/apogee test callers that take `Write`'s result in a single-value context move to `_, err` — `grep -rn '\.Write(' cmd/apogee/*_test.go | grep -i 'settings\|h\.Write'`.
**Files:** internal/tui/tui.go, cmd/apogee/wire_options.go, internal/tui/settings_test.go, internal/tui/settingsapply.go, internal/tui/colorscheme.go, internal/tui/sidepanel.go, cmd/apogee/settingsrows_test.go, cmd/apogee/wire_options_test.go, cmd/apogee/configwatch_apply_test.go
**Read first:** internal/tui/tui.go — SettingsHost.Write; cmd/apogee/wire_options.go — settingsHost.Write, SaveTo; internal/tui/settings_test.go — fakeSettingsHost, settingsWriteLog.write; internal/tui/settingsapply.go — settingsPersist, toggleTaskListFold, toggleToolsFold;
  internal/tui/sidepanel.go — setPanelWidth, setPanelShow; internal/tui/colorscheme.go — switchColorScheme; cmd/apogee/settingsrows_test.go — TestSettingsHostRowsMarkFollowsAGlobalSaveAndReset; cmd/apogee/configwatch_apply_test.go — TestRunRootWiresTheConfigWatch
**Tests:** existing; a `wire_options_test.go`-style host test asserts `landed` for one scalar key.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSettings' ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSettingsHost' ./cmd/apogee/`
**Commit:** `refactor(tui): settings host Write returns the landed value`

## 7. Pin and fold gestures write through Write — ✅ DONE (2026-11-22)

NOTES (2026-11-22): the delta spelling lives on the tui side (`sidePanelDelta`, `sidePanelFoldedState`/`sidePanelOpenState` in sidepanel.go), since tui cannot import config; the words now stand in both packages. Both gestures share one `landPanelList` that lands `landed` through `domain.UIPrefs.Set` (the key's domain parser) and journals `landed`; a `Set` refusal of `landed` is reported like a failed write.
NOTES (2026-11-22): `fakeSettingsHost` gains a `merge` field that answers `Write` for the two side-panel list keys (the fake's `write` answers the handed value as landed, which a delta is not); `settingsSpy.sections`/`folds` now record the `Write` deltas as strings (`tasks: pinned`, `tasks: folded`/`tasks: open`).
NOTES (2026-11-22): consequential edit — internal/config/configwrite_sections.go: made necessary by settingsHost.Write/Reset no longer routing the side-panel block to that writer (file header and IsSidePanelKey doc)
NOTES (2026-11-22): consequential edit — internal/config/doc.go: made necessary by settingsHost.Write/Reset no longer routing the side-panel block to configwrite_sections.go
NOTES (2026-11-22): consequential edit — internal/config/registry.go: made necessary by the fold toggle writing a merge delta rather than through SaveSidePanelFold (folded row comment)
NOTES (2026-11-22): the side-panel engine (SaveSidePanelSetting/Section/Fold, ResetSidePanelSetting, IsSidePanelKey) now has no caller outside its tests; item 8 retires it.

Depends on item 6.

**What:**
**Goal:** `SettingsHost` has 6 methods (no `WriteSidePanelSection`/`WriteSidePanelFolded`); `settingsHost.Write`/`Reset` have no `config.IsSidePanelKey` branch and route panel keys through the generic writer; pinning, hiding and folding a panel section lands the same file bytes as at the header base.
**Approach (assumed at the header base):**
- `setPanelSectionMode` / `toggleSidePanelFold` (sidepanel.go) call `Write` with the delta spelling, parse `landed` with the domain parser and store it (they keep their optimistic session update first).
- `fakeSettingsHost` drops `writeSection`/`writeFolded`; `settingsSpy` (sidepanel_test.go) and `navmode_test.go` assert recorded `Write(key, delta)` calls.
- Prose rule: no comment or doc outside historical text names the two removed host methods — `{ grep -rn 'WriteSidePanel' --include='*.go' . ; grep -rn 'WriteSidePanel' docs/manual docs/design .capcom/knowledge/glossary --exclude-dir=archived ; }` returns nothing.
**Regression guard.** The prose-guard grep is scoped to live text only: `--include='*.go'` across the repo plus docs/manual, docs/design (excluding docs/design/archived) and .capcom/knowledge/glossary — never the active plan file, .capcom/issues, decision records, CHANGELOG, docs/reviews or archived plans.
Also: `TestSettingsHostWritesBothSidePanelSectionsBlockKeys` (wire_options_test.go), which today Writes the whole list `"[usage: hidden]"` and pins named-first bytes, is re-spelled as the delta `"usage: hidden"` (item 5 refuses a whole list) and pins the bytes and `landed` the merge produces.
**Files:** internal/tui/tui.go, internal/tui/sidepanel.go, internal/tui/settings_test.go, internal/tui/sidepanel_test.go, internal/tui/navmode_test.go, cmd/apogee/wire_options.go, cmd/apogee/wire_options_test.go
**Read first:** internal/tui/sidepanel.go — setPanelSectionMode, toggleSidePanelFold, withSectionMode; cmd/apogee/wire_options.go — settingsHost.Write, settingsHost.Reset, WriteSidePanelSection, WriteSidePanelFolded; internal/tui/tui.go — SettingsHost; internal/tui/settings_test.go — fakeSettingsHost;
  internal/tui/sidepanel_test.go — settingsSpy.host, separatorDragModelUnder, doubleClickModel; internal/tui/navmode_test.go — settingsSpy users; cmd/apogee/wire_options_test.go — TestSettingsHostSidePanelSectionsWriteRefreshesTheBaseline, TestSettingsHostSidePanelFoldedWriteRoundTrips,
  TestSettingsHostResetsSidePanelShow, TestSettingsHostWritesBothSidePanelSectionsBlockKeys
**Tests:** `TestSettingsHost*SidePanel*` (wire_options_test.go) re-pointed through `Write`, asserting file bytes and `landed`, `TestSettingsHostWritesBothSidePanelSectionsBlockKeys` included (delta spelling); the sidepanel spy tests pass with recorded deltas.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'Panel|NavMode|Settings' ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSettingsHost' ./cmd/apogee/`
**Commit:** `refactor(tui): pin and fold gestures write through the settings host Write`

## 8. Retire the side-panel splice engine

Depends on item 7.

**What:**
**Goal:** `internal/config/configwrite_sections.go` and `configwrite_sections_test.go` are gone; no production code calls `IsSidePanelKey`, `SaveSidePanelSetting`, `ResetSidePanelSetting`, `SaveSidePanelSection` or `SaveSidePanelFold`; every case they tested is covered through `SaveConfigSetting`/`ResetConfigSetting` (the whole-list cases through item 4's splice-internal tests).
**Approach (assumed at the header base):**
- Move `sidePanelFoldedText` (used by registry.go) before deleting the file; repoint `configsplice_windows_test.go`'s side-panel case at the generic writer.
- Prose rule: no comment, doc or manual page outside historical text names the removed functions or a second splice engine — `{ grep -rn "$P" --include='*.go' . ; grep -rn "$P" docs/manual docs/design .capcom/knowledge/glossary --exclude-dir=archived ; }` returns nothing, with ``P="SaveSidePanel\|ResetSidePanel\|IsSidePanelKey\|configwrite_sections\|block writer\|block's writer\|block's own reset\|side-panel:\` block as a whole"``.
**Regression guard.** Same grep scope as item 7.
Also: before deleting, move every symbol of `configwrite_sections.go` / `configwrite_sections_test.go` still referenced from outside them (rule, not a list — `go build ./... && GOOS=windows go vet ./internal/config/` names them: e.g. `withSectionMode`, `writtenSidePanelSections`, `writtenSidePanelFolded` reused by items 4–5, and the test helper `readSectionsConfig` used by `configsplice_windows_test.go`, moved to a surviving test file or replaced by `os.ReadFile`). `internal/config/doc.go`'s file-map line naming `configwrite_sections.go` goes; the reset cases (`TestSidePanelSectionsBlockResetsShow`, …`BlockResetNoOpAndRefusals`, …`BlockWritesAndResetsWidth`) port to `ResetConfigSetting`.
**Files:** internal/config/configwrite_sections.go, internal/config/configwrite_sections_test.go, internal/config/configsplice_windows_test.go, internal/config/registry.go, internal/config/doc.go, cmd/apogee/wire_options.go and cmd/apogee/wire_options_test.go (comments, unless item 7 rewrote them), internal/config/configwrite_scalar_test.go (comment, unless item 3 rewrote it)
**Read first:** internal/config/configwrite_sections.go — sidePanelFoldedText, withSectionMode, writtenSidePanelSections, writtenSidePanelFolded; internal/config/configsplice_windows_test.go — TestWriteConfigAtomicallySavesASidePanelSettingThroughAJunction;
  internal/config/configwrite_sections_test.go — readSectionsConfig, writeSectionsConfig; internal/config/registry.go — ui.side-panel.* rows; internal/config/doc.go — file map; internal/config/configwrite_scalar.go — SaveConfigSetting, ResetConfigSetting, writableKey
**Closes:** side-panel-writer-split
**Tests:** `GOOS=windows go vet ./internal/config/` compiles the windows test; ported cases from item 5 pass; the reset cases pass through `ResetConfigSetting`.
**Acceptance:** `go build ./... && GOOS=windows go vet ./internal/config/ && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/config/`
**Commit:** `refactor(config): retire the side-panel splice engine`

## 9. The side-panel width is a stated-or-unset value

Depends on items 1 and 8.

**What:**
**Goal:** `domain.UIPrefs.SidePanelWidth` is a comparable `domain.SidePanelWidth` value (stated n | unset) owning resolve (unset → `DefaultSidePanelWidth`) and stated-ness; no code outside it tests the width against `0` to mean unset; the file, journal and apply spellings are unchanged (reset journals `"44"`, applies `""`).
**Approach (assumed at the header base):**
- `internal/domain/sidepanelprefs.go`: the type with `IsStated()`, `Resolve()`, a text formatter; the domain table's width entry lands `""` as unset.
- Consumers: `uiSidePanelWidth`/`zeroWhenUnstated`/`sidePanelWidthText` (keyfield.go), the width row (registry.go), `settingsApplyValue` (settingsapply.go), `configuredPanelWidth`, `panelWidth`, `clampPanelWidth`, drag `startStored`, `setPanelWidth`, `resetPanelWidth` (sidepanel.go — tui keeps its layout clamp floors), `sidePanelWidthMoved` (cmd/apogee/settingsedit.go).
- Rule: `grep -rnE 'SidePanelWidth *[!=]= 0([^0-9]|$)' --include='*.go' .` returns nothing.
**Regression guard.** The width's default text is item 1's meaning (`"44"`, the registry row's text). Files is a rule, not a closed list: every `_test.go` that `grep -rln 'SidePanelWidth' --include='*_test.go' .` names and that assigns or compares the int width (today among them `internal/tui/{sidepanel,settings,sidepaneloverlay,settingsapply}_test.go`, `internal/config/config_test.go`, `cmd/apogee/{settingsedit,settingsrows}_test.go`, `internal/domain/sidepanelprefs_test.go`). The cmd run widens to `-run 'Setting|SidePanel|ExternalEdit'` so `TestExternalEditReloadReports…SidePanelWidth…` runs, and the Goal's no-`0`-test clause is in Acceptance (the grep is tightened: the loose `!= 0` form also matches `len(spy.writes) != 0` on lines naming the width).
**Files:** internal/domain/sidepanelprefs.go, internal/domain/uikeys.go, internal/domain/uiprefs_test.go, internal/config/keyfield.go, internal/config/registry.go, internal/tui/settingsapply.go, internal/tui/sidepanel.go, cmd/apogee/settingsedit.go, and every `_test.go` the guard's grep names (above)
**Read first:** internal/tui/sidepanel.go — configuredPanelWidth, panelWidth, clampPanelWidth, startPanelDrag, endPanelDrag, setPanelWidth, resetPanelWidth; internal/config/keyfield.go — uiSidePanelWidth, sidePanelWidthText, zeroWhenUnstated; internal/tui/settingsapply.go — settingsApplyValue;
  cmd/apogee/settingsedit.go — settingChanged, sidePanelWidthMoved; cmd/apogee/settingsedit_test.go — TestExternalEditReloadReportsASidePanelWidthWhoseRowDidNotMove; internal/tui/settings_test.go — TestSettingsPaneReReadMovesTheSidePanelWidthUnderAnUnmovedRow;
  internal/domain/sidepanelprefs_test.go — TestSidePanelWidthValidateAndSetJudgeTheStatedWidth
**Closes:** ui-pref-declared-thrice
**Tests:** domain: unset vs stated-44 differ, resolve both to 44, compare with `==`; tui: drag from unset then reset journals `"44"` with reset; cmd: reload diff still reports unset → stated 44 as moved (`TestExternalEditReloadReportsADeletedSidePanelWidthAsAReset`, `TestExternalEditReloadReportsASidePanelWidthWhoseRowDidNotMove`).
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/domain/ ./internal/config/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'Panel' ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'Setting|SidePanel|ExternalEdit' ./cmd/apogee/ && test -z "$(grep -rnE 'SidePanelWidth *[!=]= 0([^0-9]|$)' --include='*.go' .)"`
**Commit:** `refactor(domain): model the side-panel width as stated or unset`

## 10. Applying a UI pref on screen is one tui module

Depends on item 1.

**What:**
**Goal:** one `internal/tui` file owns applying `ui.*` keys: by default a key lands through `UIPrefs.Set`; a follow-up table covers only the keys that tell a widget (glyphs, task-list-open, tools-open, tools-fold-over, tools-auto-collapse, pointer-shapes, git-fetch, color-scheme); it exports its applied key set; the tui apply test sweeps `domain.AllUIKeys()` (structured keys included) and the binary's test reads the exported set instead of its hand-kept mirror.
**Approach (assumed at the header base):**
- New `internal/tui/uiprefapply.go`; `settingsApplyLocal`'s single-`Set` arms collapse into the default path. Follow-up rows take the full `(Model, note, tea.Cmd, error)` shape; color-scheme's widget call runs BEFORE `Set` (keep that order); `ui.inspector` stays start-up-only.
- Exported set (e.g. `tui.AppliedSettingKeys()`) covers every ui key it applies plus `auto-title` and `cursor-shape`.
- `settingKeysAppliedByTheRenderer` (cmd/apogee/wire_settings_test.go) is deleted; `TestEveryEditableSettingKeyHasAnApply` reads the export and also covers every `ui.*` registry row (closes the reload-diff gap → `cannotApply`).
**Regression guard.** The default path admits only `AllUIKeys()` minus `ui.inspector`; every other key returns `handled=false` — `ui.inspector` keeps reaching `Settings.Apply` → `applyMirror` (cmd/apogee/wire_settings.go). The sweep asserts `ui.inspector` handled=false and absent from the exported set. `settingsApplyLocal` keeps its name and 5-result shape (called directly by several tui tests). The glossary Note `settings-surface` names the exported tui set in place of `settingKeysAppliedByTheRenderer`.
**Files:** internal/tui/uiprefapply.go, internal/tui/settingsapply.go, internal/tui/settingsapply_test.go, internal/tui/doc.go, cmd/apogee/wire_settings_test.go, .capcom/knowledge/glossary/glossary-safety/settings-surface.md
**Read first:** internal/tui/settingsapply.go — settingsApplyLocal, settingsApplyLive, settingsApplyValue, applyColorScheme; internal/tui/settingsapply_test.go — TestSettingsApplyLocalLandsEveryUIKeyThroughSet; cmd/apogee/wire_settings_test.go — settingKeysAppliedByTheRenderer, TestEveryEditableSettingKeyHasAnApply;
  cmd/apogee/wire_settings.go — applySettingFor, settingsTable ui.inspector entry, cannotApply; internal/domain/uiprefs.go — UIPrefs.Set, uiKeys
**Tests:** tui sweep over `domain.AllUIKeys()` applies a documented value for every key but `ui.inspector`, which stays handled=false and outside the exported set; cmd test passes reading the export.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'Apply' ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestEveryEditableSettingKeyHasAnApply' ./cmd/apogee/`
**Commit:** `refactor(tui): apply ui prefs through one module with an exported key set`

## 11. One persist door for gesture write-backs

Depends on items 7 and 10.

**What:**
**Goal:** every TUI gesture that persists a `ui.*` key (`toggleTaskListFold`, `toggleToolsFold`, `setPanelWidth`, `resetPanelWidth`, `setPanelSectionMode`, `toggleSidePanelFold`, `setPanelShow`) goes through one method that writes or resets, reports a failure as `taskListNotSavedNote` + error, and journals via `recordSettingEdit`; no gesture restates that sequence; user-visible notes and journal entries unchanged.
**Approach (assumed at the header base):**
- The door lives in `uiprefapply.go` (item 10) and returns `landed` for callers that store it (sections, folded).
- `switchColorScheme` is out of scope (header).
**Files:** internal/tui/uiprefapply.go, internal/tui/settingsapply.go, internal/tui/sidepanel.go, internal/tui/settingsapply_test.go, internal/tui/sidepanel_test.go
**Read first:** internal/tui/settingsapply.go — toggleTaskListFold, toggleToolsFold, recordSettingEdit, taskListNotSavedNote; internal/tui/sidepanel.go — setPanelWidth, resetPanelWidth, setPanelSectionMode, toggleSidePanelFold, setPanelShow, sidePanel*Source consts;
  internal/tui/settings_test.go — fakeSettingsHost, noSettingsWriterNote; internal/tui/sidepanel_test.go — settingsSpy, "a failed write keeps the fold/pin and says so" subtests
**Closes:** ui-pref-apply-restated
**Tests:** a table test drives each gesture against a failing `fakeSettingsHost` and asserts the exact `not saved:` note text and journal entry; success path journals as before.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'Fold|Panel|Persist' ./internal/tui/`
**Commit:** `refactor(tui): one persist door for ui gesture write-backs`

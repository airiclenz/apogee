# Settings coverage + popup scrollbar plan

- **Goal:** give the shared popup module a scrollbar (all overflowing popups), add keyboard scrolling to `/usage`, and close the two ratified `/settings` coverage gaps: a per-mechanism toggle sub-list and an editable `validated-sets.enable` row.
- **Date:** 2026-08-14
- **Status:** unexecuted
- **Sized for:** ~200k-context host
- **Authoritative sources:**
  - `internal/tui/popup.go` (row-window primitives: `popupRowWindow` ~:1294, `popupRowWindowFrom` ~:1335, `popupRowLines` ~:640; `popupSpec` ~:305)
  - `internal/tui/model.go:2686` `renderScrollbar` (thumb math to mirror), `internal/tui/theme.go:82-83` (glyphs), `:209-210` (styles)
  - `internal/tui/model.go:1092-1098` (the `/usage` esc-only key claim), `internal/tui/mouse.go:1296-1354` (`usageWindow`, `usageWheel`)
  - `internal/config/registry.go` (KeyRegistry; `context-files.*` split precedent ~:192-207; `validated-sets` row ~:333), `internal/config/registry_test.go` (bijection walker)
  - `internal/config/configwrite.go:327` `SaveServerEntrySetting` (the per-entry writer precedent), `internal/config/configwrite_scalar.go`, `internal/config/configsplice.go:159` `zeroConfigPath`
  - `internal/mechanisms/catalogue.go` (`KnownIDs()` :155, `Descriptors()` :180), `cmd/apogee/wire_tools.go:189` `mechanismIDs`, `cmd/apogee/wire_settings.go:641/:844` (mechanisms apply arm / `reloadMechanisms`)
  - ADR 0035 (one key per deliberate edit), ADR 0037 (every settings edit applies live), ADR 0016 (an explicit `mechanisms:` block suppresses a validated set)
  - `layout.md`, `docs/layout/settings-screen-layout.md`
- **Ratified design calls** (owner, 2026-08-14, via AskUserQuestion in the plan-writing session):
  1. Coverage scope: per-mechanism toggles + `validated-sets.enable` row only; `servers[]`, `mcp-servers`, `model-profiles`, `system-prompt-models` stay `$EDITOR`.
  2. Scrollbar appears in ALL overflowing popups and honors `ui.show-scrollbar`.
  3. `/usage` gains ↑/↓ and pgup/pgdn scrolling alongside the wheel.
  4. Mechanisms sub-list: ⏎ (and space) toggles the highlighted mechanism, writes + live-applies immediately, list STAYS OPEN; esc closes. ⏎ on the `mechanisms` row consequently no longer opens `$EDITOR` (raw block edits happen in `config.yaml` by hand).
  5. Toggling a mechanism off writes `<id>: false` (never removes the line). ADR 0016 consequence accepted: a non-empty manual block suppresses a validated set; existing semantics, no new UI.
  6. The scrollbar column is reserved ONLY while the row window overflows; fitting popups keep full width.
  7. Mechanism rows show bare `id` + `on`/`off` cell — no descriptions added anywhere.
- **Standing requirements:** skills: coding-standards. Any authorized deviation from item text lands as a dated NOTES line under the item.
- **Out of scope:** server-entry sub-screen editing (collides with plan `2026-08-13 - 01` api-key sources, unexecuted); sub-editing for `mcp-servers` / `model-profiles` / `system-prompt-models`; editing `validated-sets.alias` values on-screen (row stays `$EDITOR`-pointed structured); home/end keys for `/usage`; mechanism descriptions or a `Desc` field on `MechanismDescriptor`; the `/confine`-gated rows (`confine-to-workspace`, `unconfined-hosts`, ADR 0012); any version bump.

## 1. Popup scrollbar primitive — ✅ DONE (2026-08-14)

NOTES (2026-08-14): the reserved column reuses the existing package const `scrollbarWidth` (model.go) rather than adding a popup-local one — same one-cell gutter, and model.go is outside this item's files.
NOTES (2026-08-14): the bar is not reserved when the pane is so narrow that the rows would be left ≤1 cell — `truncateToWidth` draws nothing at that budget, so a bar there would hide the very content it describes.

**What:** In `internal/tui/popup.go`, add scrollbar support to the row-window painter. `popupSpec` gains a `scrollbar bool` field (default false — callers opt in, item 2). When `scrollbar` is true AND the seated row window is smaller than the total row count (overflow), reserve exactly 1 column from the row block's inner width and paint a vertical bar spanning the row block's painted lines: thumb size `max(1, h*h/totalRows*shownRows…)` — mirror the transcript's two-line arithmetic at `internal/tui/model.go:2700-2701`, mapping the window position (`start` over `total-shown`) onto `h-thumb` where `h` is the number of painted row-block lines. Use `glyphScrollThumb` / `glyphScrollTrack` (`internal/tui/theme.go:82-83`) with styles `th.scrollThumb` / `th.scrollTrack` (`theme.go:209-210, :395-396`). No column is reserved when the rows fit (ratified call 6). The bar covers the ROW window only — the body block's elision markers (`popupElisionMarker`, ~:1164) are unchanged. Works for both windowing modes: selection-grown (`popupRowWindow`) and `rowTop`-anchored (`popupRowWindowFrom`). Wrapped rows (`wrapRows`): the bar spans the block's painted LINES while thumb position/size are computed from ROW counts. Apply mechanically to every spec that windows rows; a spec that never overflows simply never paints a bar.

**Files:** internal/tui/popup.go, internal/tui/popup_test.go

**Tests:** no bar and no reserved column when rows fit; bar present and 1 column narrower rows when overflowing; thumb at top when window at row 0, at bottom when window shows the last row, monotone in between; `scrollbar: false` never paints regardless of overflow; wrapped-row block still lines up.

**Acceptance:** `go build ./internal/tui/ && go test ./internal/tui/ -run 'Popup'`

commit: `feat(tui): popup row windows can paint a scrollbar`

## 2. Callers adopt the popup scrollbar

Depends on item 1.

**What:** Stamp `spec.scrollbar = !m.opts.HideScrollbar` at every popup spec construction site, via a one-line Model helper (e.g. `m.popupScrollbarOn()`): `/settings` key list (`settings.go` ~:2058), enum sub-list (~:2197), text field spec (~:2134, only if its spec windows rows — mechanical check), `/usage` (`usage.go` ~:136-159), `/sessions` (`sessions.go` ~:525), picker (`picker.go` ~:912), autocomplete (`autocomplete.go` ~:886), approval prompt (`approval.go` ~:180), ask prompt (`model.go` ~:3792). Reword the `ui.show-scrollbar` registry `Desc` (`internal/config/registry.go:296-299`) from transcript-only to cover popups (docmap tests may pin the wording — update in lockstep). Amend `layout.md`'s popup section to describe the overflow bar. Mouse handling needs no change (`usageWheel`/`settingsWheel` clamp against the drawn window, not column math).

**Files:** internal/tui/settings.go, internal/tui/usage.go, internal/tui/sessions.go, internal/tui/picker.go, internal/tui/autocomplete.go, internal/tui/approval.go, internal/tui/model.go, internal/config/registry.go, layout.md

**Tests:** one TUI test that an overflowing `/settings` (or `/usage`) render contains the thumb glyph and a non-overflowing picker render does not; `ui.show-scrollbar: false` (i.e. `HideScrollbar`) suppresses the bar in a popup.

**Acceptance:** `go build ./... && go test ./internal/tui/ ./internal/config/`

commit: `feat(tui): every overflowing popup paints the scrollbar`

## 3. Keyboard scrolling for /usage

**What:** Broaden the `/usage` key claim at `internal/tui/model.go:1092-1098`: while `m.usagePane.open`, claim `esc` (close, as today), `up`/`down` (step `m.usagePane.top` by ∓1) and `pgup`/`pgdown` (step by the drawn window height `win.end-win.start`), clamping exactly like `usageWheel` (`mouse.go:1338-1354`) via `m.usageWindow()` (`mouse.go:1305`); the paint-time clamp at `usage.go:155` backstops overshoot. The claim MUST stay above the global transcript `pgup`/`pgdown` interception at `model.go:1192-1194`. All other keys fall through — the pane stays non-modal; update the comment accordingly. Change `usageHint` (`usage.go:52`) to exactly `"↑/↓ scroll · esc close"`.

**Files:** internal/tui/model.go, internal/tui/usage.go, internal/tui/usage_test.go (create if absent)

**Tests:** with the pane open and overflowing: `down` moves the window by one row, `pgdown` by a page, both clamp at the ends; `pgup`/`pgdown` do NOT move the transcript while the pane is open; a printable key still reaches the input box; `esc` closes and resets `top`.

**Acceptance:** `go test ./internal/tui/ -run 'Usage'`

commit: `feat(tui): /usage scrolls from the keyboard`

## 4. validated-sets.enable and .alias registry rows

**What:** Replace the single `validated-sets` `KindStructured` row (`internal/config/registry.go:333-336`) with two rows in the same registry position, following the `context-files.*` precedent (registry.go:192-207 — no parent row remains, or the bijection test fails):
- `{ Path: "validated-sets.enable", Kind: KindBool, Default: "true", Editable: true }` — desc in the registry's house style.
- `{ Path: "validated-sets.alias", Kind: KindStructured }` — not editable; `externallyEdited` gives it the `⏎ opens $EDITOR` pointer for free.

In `cmd/apogee/settingsrows.go`: `settingValues` (~:130-141) drops `"validated-sets"` and gains both new keys — enable via `boolValue(o.ValidatedSetsEnable)`, alias via an `"N aliases"` count (split `validatedSetsSummary` ~:333); `settingSections` needs no edit (`validated-sets` is not an `Opens` value — verify). In `cmd/apogee/wire_settings.go`: rekey the apply arm `"validated-sets"` (~:648) to `"validated-sets.enable"`, both routing to `reloadValidatedSets()` (~:861); alias stays non-applyable (structured, `$EDITOR`); update the `unreachable(key)` guard arm so `TestApplySettingRefusesEveryKeyItCannotReach` passes. No writer change needed: `ValidatedSets` is pointer-to-struct so `zeroConfigPath` accepts the nested path, depth 2 passes `scalarPathDepth`, and the seeded template's commented `# validated-sets:` block (`internal/config/defaults/config.yaml:627-631`) anchors first-write insertion.

**Files:** internal/config/registry.go, cmd/apogee/settingsrows.go, cmd/apogee/wire_settings.go, cmd/apogee/settingsrows_test.go, cmd/apogee/wire_settings_test.go

**Tests:** registry bijection test passes with the split rows (no test edit expected — verify); `settingValues` coverage test passes; write + read-back of `validated-sets.enable` against a config file with (a) no block, (b) commented block, (c) live block; apply arm reaches `reloadValidatedSets` for the new key.

**Acceptance:** `go test ./internal/config/ ./cmd/apogee/`

commit: `feat(config): validated-sets.enable becomes an editable settings row`

## 5. Per-mechanism config writer

**What:** New `config.SaveMechanismSetting(path, id string, enabled bool) error` in a new file `internal/config/configwrite_mechanism.go`, mirroring the `SaveServerEntrySetting` shape (`configwrite.go:327`): addressed by `(id, value)` OUTSIDE the registry (no registry row — the `mechanisms` row stays `KindStructured`; `zeroConfigPath` and the scalar path stay untouched). It writes `<id>: true|false` inside the top-level `mechanisms:` mapping, handling all four parent states like the scalar splice does (`scalarInsertion`, `configwrite_scalarsplice.go:266`): parent absent → insert after the commented example block (`commentedExampleBlockEnd`; the template carries `# mechanisms:` at `defaults/config.yaml:610-616`), parent null, parent present + key absent, key present → replace the value. Toggling off writes `<id>: false`, never removes a line (ratified call 5). Verification mirrors `verifiedEntrySplice`: re-parse before/after and require the two `fileConfig`s to differ ONLY at `Mechanisms[id]`. The writer accepts any id string; id validation against `mechanisms.KnownIDs()` is the caller's job (item 6), same division as `mechanismIDs` (`cmd/apogee/wire_tools.go:189`).

**Files:** internal/config/configwrite_mechanism.go, internal/config/configwrite_mechanism_test.go

**Tests:** table over the four parent states plus replace-existing; false-write keeps the line; verification rejects a splice that changed anything else; round-trip via `LoadFileConfig`.

**Acceptance:** `go test ./internal/config/ -run 'Mechanism'`

commit: `feat(config): a per-mechanism writer splices one id into the mechanisms block`

## 6. Mechanisms toggle sub-list on /settings

Depends on item 5 (and item 1 for the sub-list's scrollbar, which arrives for free).

**What:**
- `internal/tui/tui.go`: `Options` gains `ListMechanisms func() []MechanismToggle` (new `type MechanismToggle struct { ID string; Enabled bool }`) and `WriteMechanism func(id string, enabled bool) error`. Nil callbacks ⇒ ⏎ on the row does nothing (same contract as `ListSchemes`).
- `cmd/apogee/wire_options.go`: wire both. `ListMechanisms` returns every id from `mechanisms.KnownIDs()` in sorted order, `Enabled` reflecting the config FILE's manual `mechanisms` map (absent id = off) — load fresh like `reloadMechanisms` does, so external edits show truthfully. `WriteMechanism` calls `config.SaveMechanismSetting` + `w.externalEdits.refresh()` (mirror `WriteSetting`, wire_options.go:190), then live-applies through the EXISTING `"mechanisms"` apply arm (`wire_settings.go:641` → `reloadMechanisms` :844) per ADR 0037.
- `internal/tui/settings.go`: new pane kind `settingsMechanismList` beside `settingsEnumList` (~:97-101). ⏎ on the `mechanisms` row (`settingsEnter` ~:514, path special-case like the `settingKeyColorScheme` precedent in `settingsVocabulary` ~:1532) opens the sub-list instead of `$EDITOR`. Renderer modeled on `renderSettingsEnum` (~:2180): one `popupRow{id, "on"|"off"}` per mechanism, `menuRows: true`, selection-window scrolling, hint exactly `"⏎/space toggle · esc back"`. Keys: ↑/↓ move (wrap like the enum list), ⏎ AND space toggle the highlighted id via `WriteMechanism` and the list STAYS OPEN with state refreshed (ratified call 4; each toggle is one deliberate edit per ADR 0035), esc returns to the key list, everything else swallowed (the settings pane is modal). Write failure surfaces on `m.settings.failure` like `settingsPersist` (~:1213).
- `cmd/apogee/settingsrows.go`: the `mechanisms` row's `EditPointer` becomes exactly `"⏎ opens toggle list"` (special-case beside `externallyEdited` ~:46); value cell stays the existing count summary.
- Docs: `docs/layout/settings-screen-layout.md` gains the sub-list; note in the item's CHANGELOG entry that a manual block created by the first toggle suppresses a validated set (ADR 0016, pre-existing semantics).

**Files:** internal/tui/tui.go, internal/tui/settings.go, internal/tui/settings_test.go, cmd/apogee/wire_options.go, cmd/apogee/settingsrows.go, cmd/apogee/settingsrows_test.go, docs/layout/settings-screen-layout.md

**Tests:** ⏎ on the mechanisms row opens the sub-list (not `$EDITOR`); toggle calls `WriteMechanism` with the highlighted id and flipped value and the list stays open; esc returns to the key list; nil callbacks ⇒ no-op; the 21-id list overflows and windows correctly; pointer text pinned.

**Acceptance:** `go test ./internal/tui/ ./cmd/apogee/`

commit: `feat(tui): /settings toggles individual mechanisms in a sub-list`

---

**Suggested version bump:** one feature-level micro-bump (per the house convention "VERSION micro-bumps per shipped feature") once the plan lands — the owner decides whether and when; no item changes VERSION or CHANGELOG release headings.

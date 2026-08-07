# Color schemes — implementation plan

- **Goal:** user-configurable color schemes for the Apogee TUI: built-in schemes embedded
  in the binary, user scheme files in `~/.apogee/schemes/` (YAML, shadowing built-ins by
  name), a `ui.color-scheme` config key with a live-apply settings picker, and a
  `/color-scheme` command with an `export` subcommand.
- **Date:** 2026-08-07
- **Status:** not started
- **Authoritative sources:**
  - Ratified design calls below (owner, grill-me session 2026-08-07) — the ground truth
    for every user-visible behavior in this plan.
  - `internal/tui/theme.go:22-54` — the current palette (pinned verbatim in the role
    table of item 1).
  - ADR 0037 (live settings apply), ADR 0035/0036 (config writes), ADR 0027 (skill vs
    file-ref colors are load-bearing), ADR 0011 (value-copied Model, no no-copy types),
    `internal/tui/paintcache.go:71-79` (paint cache must not survive a theme swap).
  - Precedents: `cmd/apogee/defaults.go` (seed-if-missing file writes),
    `internal/tui/settings.go:1094` (`settingsApplyLocal`), the `kindServer` dynamic
    enum (`cmd/apogee/registry.go:56-61,143`, `internal/tui/settings.go:1243`).
- **Skills:** coding-standards
- **Standing requirements:** run `make check` before every commit; one commit per item;
  never change VERSION / CHANGELOG release headings / tags (suggestion only — see the
  closing note); any authorized deviation from item text lands as a dated NOTES line
  under the item.

## Ratified design calls (owner, via AskUserQuestion, 2026-08-07)

1. Built-in schemes are **embedded in the binary** (`go:embed`). They are never written
   to disk, there is no boot-time presence check, and no network download — ever.
2. Scheme files are **YAML** (`gopkg.in/yaml.v3`, same as `config.yaml`); exported files
   carry a comment per key.
3. Schema = **one key per semantic color role** (23 roles, table in item 1), organized in
   commented sections. Every key optional; a missing key falls back to the built-in
   default ("dark") value for that role.
4. Two shipped schemes: **`dark`** (exactly today's palette, remains the default) and a
   new **`light`** scheme.
5. User-facing name is **color-scheme**: config key `ui.color-scheme` (file-only, in the
   `ui:` block), user folder `~/.apogee/schemes/` (`*.yaml`), command `/color-scheme`.
6. **Shadowing allowed:** `~/.apogee/schemes/<name>.yaml` overrides a built-in of the
   same name. Resolution order: user file, then built-in.
7. **`/color-scheme export <name>`** writes a fully-commented complete scheme file to
   `~/.apogee/schemes/<name>.yaml` and refuses to overwrite an existing file. Switching
   happens via a settings-screen picker row (built-ins + discovered user files) with
   live apply on the ADR 0037 `settingsApplyLocal` path, and via `/color-scheme <name>`.
8. **Forgiving validation:** bad hex or unknown key → that key falls back to the default
   value plus a warning naming file and key; unreadable file or unknown scheme name →
   default scheme plus a warning. Never crash, never lose styling.
9. **No full-screen background painting:** schemes recolor only what is colored today.
   The light scheme is documented as "for light terminals". In every shipped scheme,
   `skill` and `file-ref` must remain visually distinguishable (ADR 0027).
10. **Light scheme basis: GitHub-light equivalents** — same hue meaning per role,
    values pinned in item 3.
11. **Warnings surface: chat-transcript note** (`addEphemeralNote` styling — dim
    one-liner, not persisted to the session record), at boot and after a live switch;
    the settings pane row additionally shows its inline failure text while open.

## Out of scope

- Downloading schemes from the network, in any form.
- Painting the full screen background; light/dark terminal auto-detection
  (`lipgloss.AdaptiveColor` etc.).
- Theming glyphs, markers, or layout — colors only.
- Hot-reload when a scheme *file* is edited on disk (the config-watch plan
  `docs/plans/2026-08-06 - 04 - editor-key-and-config-watch-plan.md` is the natural
  future home; switching schemes re-reads the file, which covers the edit loop).
- Any VERSION/tag/release change.

## 1. `internal/scheme` package: types, YAML decode, forgiving validation, embedded `dark` — ✅ DONE (2026-08-07)

**What:** New package `internal/scheme` (no lipgloss or tui imports — plain data):

- `scheme.go`: `type Scheme struct` with 23 string fields (hex values, `#rrggbb`), one
  per role, with yaml tags exactly as in this table. This table is binding — it is
  today's palette from `internal/tui/theme.go:22-54`:

  | YAML key | theme.go var | dark value |
  |---|---|---|
  | `user-text` | colWhite | `#ffffff` |
  | `chrome` | colDarkGray | `#4a4a4a` |
  | `divider` | colDimGray | `#333333` |
  | `surface` | colBlack | `#000000` |
  | `muted` | colFaint | `#8a8a8a` |
  | `diff-add` | colDiffAdd | `#3fb950` |
  | `diff-del` | colDiffDel | `#f85149` |
  | `error` | colError | `#f85149` |
  | `code` | colCode | `#f0883e` |
  | `mode-plan` | colModePlan | `#2afefa` |
  | `mode-ask-before` | colModeAskBefore | `#3fb950` |
  | `mode-allow-edits` | colModeAllowEdits | `#58a6ff` |
  | `mode-auto` | colModeAuto | `#f0883e` |
  | `skill` | colSkill | `#b1baff` |
  | `file-ref` | colFileRef | `#cdffa4` |
  | `prompt-toggle` | colPromptToggle | `#b0d2ff` |
  | `tool-marker` | colToolMarker | `#8db4e6` |
  | `gauge` | colGauge | `#c396ff` |
  | `selection` | colSelection | `#3a5fcd` |
  | `spinner-1` | colSpinner1 | `#8668ff` |
  | `spinner-2` | colSpinner2 | `#19a946` |
  | `spinner-3` | colSpinner3 | `#ffbf00` |
  | `spinner-4` | colSpinner4 | `#ff4a81` |

- `schemes/dark.yaml`: the embedded default scheme — every key present with exactly the
  values above, organized in commented sections (base tones / semantic / autonomy modes
  / prompt tokens / chrome accents / misc / spinner), each line commented with what it
  colors (source the wording from the existing comments in `theme.go:22-54`). A header
  comment states: schemes recolor only what apogee already colors; this scheme is for
  dark terminals.
- `go:embed` of `schemes/*.yaml`; `Default() Scheme` returns the parsed embedded
  `dark` (parse once, package-private sync.Once or init — but no work at import time
  beyond the embed itself is required; implementer's call).
- `type Warning struct { File, Key, Reason string }` with a `String()` rendering like
  `color-scheme "dark.yaml": key "error": bad hex "#zz0000" — using default`.
- `Parse(name string, data []byte) (Scheme, []Warning)`: strict-ish YAML decode into a
  key map, then per-key: unknown key → warning, skip; value failing
  `^#[0-9a-fA-F]{6}$` → warning, keep default; missing key → silently keep default (no
  warning — partial files are the intended usage, design call 3). Never returns an
  error; a file that fails YAML decode entirely yields `(Default(), one warning)`.

**Tests** (`scheme_test.go`): full-file round-trip; partial file (2 keys) inherits the
other 21 from Default; bad hex → default value + warning naming the key; unknown key →
warning; unreadable YAML → Default + warning; embedded `dark.yaml` parses with zero
warnings and equals the pinned table (compare all 23 hex values literally in the test —
this test is the drift guard between `dark.yaml` and this plan).

**Acceptance:** `go test ./internal/scheme/ -count=1` passes; `make check` passes.

**Commit:** `feat(scheme): scheme type, forgiving YAML parse, embedded dark scheme`

## 2. Discovery, shadowing resolution, and export writer

Depends on item 1.

**What:** in `internal/scheme`:

- `Discover(userDir string) []string`: sorted unique names — built-in names plus the
  basename (minus `.yaml`) of every `*.yaml` in `userDir`. Missing/unreadable dir is
  not an error (empty contribution). No file contents are read here.
- `Resolve(name, userDir string) (Scheme, []Warning)`: user file
  `<userDir>/<name>.yaml` first (design call 6), else built-in, else
  `(Default(), warning "unknown color-scheme %q — using dark")`. File read errors →
  Default + warning; parse warnings pass through.
- `Export(name, userDir string) (path string, err error)`: only built-in names are
  exportable (the point is "get an editable copy", design call 7); unknown name →
  error listing built-ins. Writes the embedded YAML bytes verbatim (they are already
  fully commented, item 1) to `<userDir>/<name>.yaml` with `os.MkdirAll(userDir,
  0o700)` + `0o600` file mode, mirroring `seedConfig` (`cmd/apogee/defaults.go:45-58`).
  An existing file → error "already exists — edit it or delete it first"; never
  overwrite.

**Tests:** temp-dir based: Discover merges + sorts + dedups (shadow name appears once);
Resolve prefers the user file over a built-in of the same name; Resolve of unknown name
→ Default + warning; Export writes byte-identical embedded content, refuses a second
export, creates the dir when absent.

**Acceptance:** `go test ./internal/scheme/ -count=1` passes; `make check` passes.

**Commit:** `feat(scheme): user-dir discovery with shadowing, resolve, export writer`

## 3. The built-in `light` scheme

Depends on item 1.

**What:** `internal/scheme/schemes/light.yaml`, same commented structure as `dark.yaml`,
header comment "for light terminals — apogee does not paint the full background" (design
call 9). Values are binding (design call 10 — GitHub-light equivalents; non-GitHub hues
darkened for contrast on white, hue preserved):

| key | value | | key | value |
|---|---|---|---|---|
| `user-text` | `#1f2328` | | `mode-plan` | `#0f9b96` |
| `chrome` | `#d0d7de` | | `mode-ask-before` | `#1a7f37` |
| `divider` | `#eaeef2` | | `mode-allow-edits` | `#0969da` |
| `surface` | `#ffffff` | | `mode-auto` | `#bc4c00` |
| `muted` | `#656d76` | | `skill` | `#6639ba` |
| `diff-add` | `#1a7f37` | | `file-ref` | `#4d7c0f` |
| `diff-del` | `#cf222e` | | `prompt-toggle` | `#4a72b8` |
| `error` | `#cf222e` | | `tool-marker` | `#3b6ea5` |
| `code` | `#bc4c00` | | `gauge` | `#8250df` |
| `selection` | `#b3d1ff` | | `spinner-1` | `#6f42c1` |
| `spinner-2` | `#1a7f37` | | `spinner-3` | `#b08800` |
| `spinner-4` | `#d1245d` | | | |

Add cross-scheme guard tests that iterate over ALL built-ins (so future schemes are
covered automatically): every built-in parses with zero warnings and has all 23 keys
present in its YAML text; `skill` != `file-ref` (ADR 0027, design call 9); the four
`mode-*` values are pairwise distinct.

**Tests:** the guard tests above.

**Acceptance:** `go test ./internal/scheme/ -count=1` passes; `make check` passes.

**Commit:** `feat(scheme): built-in light scheme with cross-scheme guard tests`

## 4. `newTheme` consumes a Scheme; close the non-spinner palette leaks

Depends on item 1.

**What:** in `internal/tui`:

- `newTheme()` becomes `newTheme(s scheme.Scheme)` (`theme.go:206`); every style in it
  takes its color via `lipgloss.Color(s.<Field>)`. Delete the package-level palette
  vars at `theme.go:22-48` (`colWhite` … `colSelection`). **Exception:**
  `colSpinner1..4` (`theme.go:50-53`) stay untouched — item 5 owns them; the theme
  keeps using them for spinner styles exactly as today.
- The `theme` struct grows the few raw-color fields the leak sites need (e.g.
  `surface`, `errorFg`, `chrome`, plus a `modeColor(mode) lipgloss.Color` method or
  color fields for the four modes) — all plain `lipgloss.Color` values, copy-safe
  (ADR 0011).
- Close the known leak sites so no call site references a palette var:
  - `model.go:442-446` — textarea `Background(colBlack)` → theme field.
  - `model.go:3650` — `Foreground(colError)` → theme field.
  - `model.go:3801-3809` — `modeColor` → the theme's mode colors.
  - `model.go:4030` — `gaugeFill.Background(colDarkGray)` → theme field.
  - `popup.go:394` — `Background(colBlack)` → theme field.
- Production call site `model.go:283` becomes `newTheme(scheme.Default())` (item 6
  swaps in the configured scheme). Update every test call site of `newTheme()` and
  every test reference to a deleted palette var (`render_test.go`, `mouse_test.go`;
  `spinner_test.go` only if it touches non-spinner vars) — mechanical
  `newTheme(scheme.Default())` substitution.
- After this item, `grep -n 'col[A-Z]' internal/tui/*.go` (non-test) may match only
  `colSpinner` references.

**Tests:** existing suite is the safety net (rendering must be pixel-identical — same
values flow through); add one guard test asserting `newTheme(scheme.Default())`
produces the documented dark colors for a sample of roles (error fg, mode colors,
selection bg) so the scheme→style wiring can't silently cross wires.

**Acceptance:** `go test ./internal/tui/ -count=1` passes;
`grep -rn 'colWhite\|colDarkGray\|colDimGray\|colBlack\|colFaint\|colDiffAdd\|colDiffDel\|colError\|colCode\|colMode\|colSkill\|colFileRef\|colPromptToggle\|colToolMarker\|colGauge\|colSelection' internal/tui/ --include='*.go'`
returns nothing; `make check` passes.

**Commit:** `refactor(tui): newTheme takes a color scheme; close palette-var leaks`

## 5. Spinner stops move into the theme

Depends on item 4.

**What:** the init-time package var `spinnerStops = buildSpinnerStops(...)`
(`internal/tui/spinner.go:267`) cannot follow a runtime scheme switch. Move the stops
onto the `theme` struct: a field built inside `newTheme` from the scheme's four
`spinner-*` values (via `buildSpinnerStops`); the blend at `spinner.go:305` and every
other consumer reads `th`'s stops. Delete `colSpinner1..4` from `theme.go` (their
values now come from the scheme). Update `spinner_test.go` accordingly. Keep the
`theme` value copy-safe: a slice field built once and never mutated is acceptable
(same sharing discipline as `theme.measure`, ADR 0030) — no `strings.Builder`-class
no-copy types (ADR 0011).

**Tests:** existing spinner tests updated; guard from item 4 extended with one spinner
stop.

**Acceptance:** `go test ./internal/tui/ -count=1` passes;
`grep -n 'colSpinner' internal/tui/` returns nothing; `make check` passes.

**Commit:** `refactor(tui): spinner color stops become per-theme state`

## 6. Config key `ui.color-scheme` and boot-time resolution

Depends on items 2 and 4.

**What:**

- `cmd/apogee/config.go`: `uiConfig` (`config.go:1088-1101`) gains
  `ColorScheme string` (yaml `color-scheme`; empty = default `dark`); merge into the
  resolved `settings` exactly as the other `ui.*` keys (file-only — the `ui:` block
  never rides env/flag, `config.go:683`).
- `cmd/apogee/registry.go`: row `Path: "ui.color-scheme", Kind: kindEnum,
  Default: "dark", EnumValues: nil, Editable: true` with a `Desc` naming the folder and
  shadowing rule. `EnumValues` stays empty on purpose — the vocabulary is dynamic
  (built-ins + user files), following the `kindServer` precedent
  (`registry.go:56-61`); exempt the row from `TestRegistryEnumValuesMatchParseSites`
  the same way `kindServer` is. `Validate`: accept any non-empty name without path
  separators — existence is deliberately NOT checked (forgiving load, design call 8).
  The bijection guard (`registry.go:17-27`) forces this row; that is expected.
- `cmd/apogee/defaults/config.yaml`: document `color-scheme: dark` in the commented
  `ui:` block (lines ~367-403), naming `~/.apogee/schemes/`, shadowing, and
  `/color-scheme export`.
- `internal/tui/tui.go` `Options` (`tui.go:200`): add `ColorScheme scheme.Scheme` and
  `ColorSchemeName string` and `ColorSchemeWarnings []string`, next to the other
  presentation selections (`Spinner` etc.) — already-resolved values, the renderer
  never parses files for boot.
- `cmd/apogee/wire.go` (options literal, `wire.go:550-640`): compute
  `schemesDir = <apogee home>/schemes`, call `scheme.Resolve(name, schemesDir)`, set
  the three fields (warnings rendered via `Warning.String()`).
- `internal/tui/model.go:283`: `newTheme(opts.ColorScheme)`; on startup, each entry of
  `ColorSchemeWarnings` becomes one transcript ephemeral note
  (`transcript.addEphemeralNote`, `transcript.go:255`) — design call 11.

**Tests:** config-load test: `ui.color-scheme` parses and lands in settings; registry
tests stay green (bijection + enum pin exemption); a wire-level or unit test that an
unknown configured name yields Default + a warning string mentioning the name.

**Acceptance:** `go test ./cmd/apogee/ ./internal/tui/ -count=1` passes;
`go run ./cmd/apogee --help` exits 0; `make check` passes.

**Commit:** `feat(config): ui.color-scheme key with boot-time scheme resolution`

## 7. Settings picker row and live apply

Depends on item 6 (and 5).

**What:**

- `internal/tui/tui.go` `Options`: add two closures, wired from `cmd/apogee/wire.go`:
  `ListSchemes func() []string` (→ `scheme.Discover(schemesDir)` merged with
  built-ins — `Discover` already includes built-ins) and
  `ResolveScheme func(name string) (scheme.Scheme, []string)` (→ `scheme.Resolve`,
  warnings pre-rendered). Discovery stays out of the renderer's own code; the TUI only
  calls what it is handed.
- `cmd/apogee/settingsrows.go`: add the row to the "Interface" section
  (`settingsrows.go:66-77`) with a value reader like the other `ui.*` rows
  (`settingsrows.go:124-126`).
- `internal/tui/settings.go`: the row's enum vocabulary is dynamic — extend
  `settingsVocabulary` (`settings.go:1243-1262`) to call `opts.ListSchemes()` for this
  key, following the `SettingServer` divergence precedent. Current value marked
  `"(current)"` as today (`settings.go:1893`).
- Live apply: new constant `settingKeyColorScheme = "ui.color-scheme"`
  (`settings.go:1138-1144`); `settingsApplyLocal` (`settings.go:1094`) case: resolve
  via `opts.ResolveScheme`, set `m.th = newTheme(s)`, clear the block paint cache —
  `paintcache.go:71-79` names this exact obligation ("the cache has to be cleared when
  the theme changes"); update that comment to reflect that the switch now exists —
  then `m.layout()` and return `tea.ClearScreen` for a full repaint. This requires
  `settingsApplyLive`/`settingsApplyLocal` (`settings.go:1066-1128`) to grow a
  `tea.Cmd` return, threaded through the one call site (`settings.go:1040-1050`);
  other keys return a nil Cmd.
- Warnings from a live switch: each becomes a transcript ephemeral note; while the
  pane is open, the row's existing failure/answer slot shows
  `"applied with N warnings"` (design call 11). Persistence itself needs no new code —
  the registry row (item 6) makes the normal `settingsWrite` splice path work.

**Tests:** `settingsApplyLocal` with a stub `ResolveScheme`: theme actually changes (a
sampled style's foreground differs after applying a stub scheme), paint cache is
invalidated, a Cmd is returned; vocabulary test: picker lists stub `ListSchemes`
output; warning path produces the note.

**Acceptance:** `go test ./internal/tui/ ./cmd/apogee/ -count=1` passes; `make check`
passes.

**Commit:** `feat(tui): color-scheme settings picker with live apply`

## 8. `/color-scheme` command with export subcommand

Depends on item 7.

**What:** in `internal/tui`:

- `command.go`: add `commandSpec{name: "color-scheme", takesArgs: true, ...}` in
  alphabetical position (the order test pins it). Grammar, parsed on the `/confine`
  precedent (`parseConfine`, `command.go:338-355`):
  - bare `/color-scheme` → transcript note listing every scheme from
    `opts.ListSchemes()`, current one marked;
  - `/color-scheme <name>` → switch: reuse the item-7 apply path AND persist through
    the same write path the settings pane uses, so the config file and the live state
    never diverge;
  - `/color-scheme export <name>` → `Options` gains `ExportScheme func(name string)
    (string, error)` (wired to `scheme.Export` in `wire.go`); success → note with the
    written path; error (unknown name, file exists) → error-styled transcript entry
    (`addError`, `transcript.go:758`);
  - anything else → `unknown /color-scheme subcommand %q` error mirroring
    `command.go:350`.
- Dispatch: one `case "color-scheme":` in `runCommand` (`model.go:1516`).

**Tests:** parse tests for all four grammar branches; runCommand tests with stub
closures: list note content, switch applies + persists, export success and
refuse-overwrite error surfaces.

**Acceptance:** `go test ./internal/tui/ -count=1` passes; `make check` passes.

**Commit:** `feat(tui): /color-scheme command — list, switch, export`

## 9. Docs: ADR 0039, CONTEXT.md term, layout.md, CHANGELOG

Depends on items 1–8 (content describes the landed behavior).

**What:**

- `docs/adr/0039-color-schemes-are-embedded-roles-with-user-shadowing.md` (next free
  number; keep the house ADR format): record design calls 1–11 with the
  grill session as context, explicitly noting the rejected alternatives (boot-time
  file installation, network download, base-palette indirection, full-screen
  painting) and the ADR 0027 skill/file-ref constraint now enforced by test.
- `CONTEXT.md`: add a **Color Scheme** term to the domain language (the role file, the
  23 roles, shadowing, the dark default) — section placement per the file's existing
  structure; read only the relevant section.
- `layout.md`: where colors are described in prose (notably around the palette and
  spinner-color passages, lines ~653-659, ~747-766, ~853-859), state that colors now
  come from the active color scheme and name the role keys where a specific hex was
  previously stated as fixed. Keep it a touch-up, not a rewrite.
- `CHANGELOG.md`: entry under the current unreleased/latest section describing the
  feature (embedded dark+light, `ui.color-scheme`, `~/.apogee/schemes/` shadowing,
  `/color-scheme` with export, live apply). Do NOT add a new version heading and do
  NOT touch `VERSION` (see closing note).
- `README.md`: one feature-list line if the README carries a feature list; otherwise
  skip (note the skip in NOTES).

**Tests:** none (docs); `make check` still runs as the gate.

**Acceptance:** `make check` passes; `ls docs/adr/ | tail -1` shows the 0039 file;
`grep -n "Color Scheme" CONTEXT.md` and `grep -n "color-scheme" CHANGELOG.md` match.

**Commit:** `docs: ADR 0039 color schemes; CONTEXT/layout/CHANGELOG updates`

## Suggested version bump

This is a user-visible feature (new config key, new command, new files under
`~/.apogee`): suggest **minor → v0.13.0** once the plan lands. No item performs the
bump — owner's call, after review.

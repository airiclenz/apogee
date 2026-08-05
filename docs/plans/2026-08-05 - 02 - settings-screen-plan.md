# Settings screen — /settings full-height pane over a declarative key registry

- **Goal:** a `/settings` slash command opening a full-height pane that lists every
  config key with its resolved value, and edits simple keys by persisting them into
  `~/.apogee/config.yaml` through the comment-preserving splice writer. The screen is
  driven by a new declarative key registry so it can never drift from the schema.
- **Date:** 2026-08-05 · **Status:** not started
- **Baseline commit:** `066f7c0` — all file:line anchors below are as of this commit.
- **Authoritative sources:** ADR 0011 (thin renderer), ADR 0012 + 2026-07-21 amendment
  (config-write fence), ADR 0015/0016 (mechanisms & validated sets), ADR 0023 §8
  (upgrade never touches an existing config), ADR 0027 (one slash namespace),
  ADR 0030 (width authority), ADR 0031 (Driver invariants); `layout.md` §"What
  'height' means" (:87) and the Column contract (:1054); `cmd/apogee/config.go`
  (schema: `fileConfig` :685, `layer` :424, `settings` :38, `resolveSettings` :547),
  `cmd/apogee/configwrite.go` (splice writer), `cmd/apogee/defaults/config.yaml`
  (embedded template), `internal/tui/popup.go` / `picker.go` / `command.go`.
- **Skills:** coding-standards.

## Ratified design calls

Owner, 2026-08-05 (AskUserQuestion, this planning session):

1. **Surface = full-height pane.** The pane claims the entire transcript row budget;
   the status line, input box, and footer (layout.md's frame floor) stay intact. Not a
   true full-screen takeover, not the 8-row picker window.
2. **Persist per edit.** Each committed edit is spliced into `config.yaml` immediately
   (comment-preserving, verified, atomic). A settings-screen edit is the deliberate
   user act ADR 0012's fence requires. Live apply only where a runtime path already
   exists (see author calls: mode only); every other edited row shows a
   "(next launch)" marker.
3. **v1 edit scope = simple keys only.** Bools, enums, strings, ints are editable.
   Structured blocks (`servers`, `mcp-servers`, `mechanisms`, `validated-sets`,
   `unconfined-hosts`, `system-prompt-models`, `context-files.names`,
   `model-profile`, the system-prompt text/file entries) render read-only with an
   "edit in config.yaml" pointer.
4. **Declarative key registry first.** One table in `cmd/apogee` describing every
   config key (path, kind, default, sources, restart flag, editability, description);
   the screen renders from it and resolution reads its source metadata from it. A
   reflection guard pins registry ↔ `fileConfig` bijection.
5. **Inserted keys land below their commented example.** The splice finds the key's
   `# key: …` example block from the seeded template and inserts the active line
   directly after it; append-at-end is the fallback when no example matches.
6. **Reset-to-default is in v1.** A reset action deletes the key's active line via the
   same verified splice; the value returns to its default (next launch unless live).
7. **/settings is idle-only** (`whileRunning: false`, the default command policy).

Author-resolved, 2026-08-05 (convention/safety, no user-visible fork or single-homing
a safety flow):

- **`confine-to-workspace` and `unconfined-hosts` are display-only** in the pane, with
  a "use /confine" pointer — the acknowledgement interlock (ADR 0012: distinct
  affirmative act, no default-yes) stays single-homed in `/confine`.
- **The pane never triggers rebinds.** Live model/server switching stays with
  `/model` and `/server`; editing `endpoint`/`model` in the pane persists only. The
  sole live apply is `mode`, through the existing `SetMode` seam the Shift+Tab
  handler already uses (`internal/tui/model.go:913`).
- **`api-key` is masked** (`••••`) in the list; characters are visible while editing.
- **Row order = template order**, with faint section-header rows matching the
  template's comment sections. `/settings` is `noRecall: true` (pure-UI verb, the
  `/new`//`/clear` precedent).
- **Edit idioms:** ⏎ on a bool toggles and persists; ⏎ on an enum opens an in-pane
  value sub-list (`menuRows`, the `/schedule` two-step precedent :`schedule.go:204`),
  ⏎ commits, esc backs out; ⏎ on a string/int opens a caret edit buffer (the
  `/sessions` rename idiom, `sessions.go:264`), enter validates and commits, esc
  cancels; backspace on a selected active row starts a reset, confirmed by ⏎ (hint
  line switches to "⏎ confirm reset · esc cancel").

## Out of scope

- **Boot-time auto-sync of missing config keys — REJECTED, not deferred.** No such
  code exists (repo and `git log -S` swept); it contradicts ADR 0023 §8,
  `seedConfig`'s pinned never-overwrite invariant (`defaults_test.go:45`), README's
  "an upgrade never touches it", and ADR 0021's never-edits-config posture. The
  screen removes its motivation: it renders the full schema from the registry, so a
  key missing from the file still appears with its default, and a first edit inserts
  it. Item 1's ADR records the rejection.
- Structured-block editors of any kind (lists, maps, multi-line system-prompt text).
- Editing `confine-to-workspace`/`unconfined-hosts` from the pane (stays `/confine`).
- Rebinds or server/model switching from the pane.
- Rewriting the typed `layer`/`settings` copy chain into full table-driven resolution
  (the 2026-08-01 architecture-review follow-up; the registry + bijection guard is
  this plan's step toward it, the rewrite is its own future effort).
- Any version bump (see closing note).

Any authorized deviation from item text lands as a dated NOTES line under the item.

## 1. ADR 0035 + CONTEXT.md term: ratify the settings surface — ✅ DONE (2026-08-05)

**What:** Write `docs/adr/0035-the-settings-surface-persists-one-key-per-deliberate-edit.md`
recording the calls above: the full-height pane class (frame floor intact), persist-per-edit
through the splice writer as an ADR 0012-fenced deliberate act (extending the authorized
write set from `unconfined-hosts` to registry-declared editable scalars, each write naming
its entry), v1 simple-key scope, insert-below-example placement, reset-as-line-deletion,
idle-only, confine single-homing, mode-only live apply — and a "considered and rejected"
section for boot-time auto-sync (grounds: ADR 0023 §8, `seedConfig` invariant, ADR 0021
posture; the registry-driven screen supersedes the motivation). Add a **Settings surface**
entry to CONTEXT.md's glossary (the `## Language` section): the full-height pane over the
key registry; note it reconciles the existing "the tool never writes config on its own"
entries — never *unprompted*; a settings-screen edit is user-initiated and names its entry.

**Tests:** none (docs only).

**Acceptance:** `ls docs/adr/ | grep 0035` shows the file;
`grep -n "Settings surface" CONTEXT.md` hits the glossary; `make check` passes.

**Commit:** `docs(adr): ratify the /settings surface — full-height pane, persist-per-edit, auto-sync rejected`

## 2. Declarative key registry with schema-bijection guard — ✅ DONE (2026-08-05)

**What:** New file in `cmd/apogee` (suggest `registry.go`): a `configKey` row type and a
`keyRegistry` table with one row per leaf key of `fileConfig` (`cmd/apogee/config.go:685`),
dotted paths for nested keys (`ui.spinner`, `present.auto-open`, `context-files.enable`, …),
and one row per structured block (kind `structured` terminates descent: `servers`,
`mcp-servers`, `mechanisms`, `validated-sets`, `unconfined-hosts`, `system-prompt-models`,
`context-files.names`, `model-profile`, plus `system-prompt-text`/`system-prompt-file`
marked not editable in v1). Row fields: `Path`, `Kind` (bool/int/string/enum/structured),
`Default` (display string), `EnumValues`, `EnvVar`, `FlagName`, `GlobalOnly`,
`RestartRequired`, `Editable`, `Masked` (api-key only), `Desc` (one line, condensed from
the template's comments — template at `cmd/apogee/defaults/config.yaml`; `model-profile`
is the one key absent from the template, per `config.go:758`). Editability per ratified
call 3 and the author calls (`confine-to-workspace`/`unconfined-hosts` → `Editable: false`).

**Tests:** reflection guard walking `fileConfig`'s yaml tags recursively and asserting a
bijection with registry paths (structured rows terminate descent — the anti-drift core);
enum rows have non-empty `EnumValues` matching the parse sites
(`internal/tui/spinner.go:53` spinner names, `ParseCursorShape`, the mode ladder);
`Editable` ⇒ kind not structured; every row has a non-empty `Desc`; `Masked` only on
`api-key`.

**Acceptance:** `go test ./cmd/apogee/ -run Registry` passes; `make check` passes.

**Commit:** `feat(config): declarative key registry with schema-bijection guard`

## 3. Resolution reads source metadata from the registry — ✅ DONE (2026-08-05)

**NOTES (2026-08-05):** of the default literals at `config.go:548-550`, only `mode` was pointed at
its registry row — the one default whose row spelling (`"ask-before"`) IS the key's typed value.
The bool/aggregate defaults stay typed literals: their rows spell them as text (`"true"`), and
reaching `confine-to-workspace`'s default through a parse of a table entry would leave a safety
default one registry typo away from silently flipping to `false`. `TestRegistryModeDefaultIsTheLadderDefault`
pins the one row resolution now reads to the ladder constant, and `TestMultiSourceKeysBindDescribedKeys`
(added beyond the item's test list) guards the new binding table against advertising a source
nothing reads.

**What:** Depends on item 2. Rewire the multi-source precedence loop in
`resolveSettings` (`cmd/apogee/config.go:599-618` — endpoint, model, mode, host-alias,
bypass, api-key) to take each key's env-var and flag names from its registry row instead
of parallel string literals; point the default literals (`config.go:548-550`) at the
registry rows where the default is representable as the row's typed value. The ~19
file-only typed assignments (`:551-598`) stay as they are (out-of-scope note above); the
item's win is that source metadata now has exactly one home.

**Tests:** for each of the six multi-source keys, a table test asserting env-beats-file
and flag-beats-env resolution driven through the registry row (fails if a row's
`EnvVar`/`FlagName` is edited to nonsense); existing config tests stay green unchanged.

**Acceptance:** `go test ./cmd/apogee/` passes; `make check` passes.

**Commit:** `refactor(config): resolution reads source metadata from the key registry`

## 4. Scalar splice writer: insert-below-example, replace, delete — ✅ DONE (2026-08-05)

**NOTES (2026-08-05):** two deviations. (1) The golden files pin the **user-edited** variant only
(`cmd/apogee/testdata/settings-edited.yaml` × 8 ops); the **seeded template** cases are pinned
structurally instead — `TestSpliceScalarSettingInsertsBelowTheTemplateExample` asserts the result
differs from the 450-line template by exactly the inserted line(s) and that they landed under the
key's commented example, which is a stronger claim than byte-equality with seven 450-line goldens
no reviewer could read. (2) For a **nested** key whose block is absent, the created two-line block
lands after the END of the parent's commented example block rather than immediately below the leaf's
own commented line — the item's "within/below the parent's example block" latitude — so the new
`ui:` mapping is not wedged into the middle of its own documentation. A nested key whose block IS
already open joins that mapping (its commented example is not a candidate anchor: the child has to
land inside the block that is open).

**What:** Generalize the machinery in `cmd/apogee/configwrite.go` (today hard-coded to
`unconfined-hosts`, `:38`, `:225`) into scalar operations keyed by registry path, for
top-level and one-level-nested keys:

- **Replace:** an active `key: value` line present → rewrite that line's value.
- **Insert:** no active line → insert directly below the key's commented example block
  (match `# key:` at the expected indent; for nested keys, within/below the parent's
  example block), per ratified call 5; fall back to append-at-end when no example
  matches, creating the parent mapping line for a nested key whose parent is absent.
- **Delete (reset):** remove the key's active line; when that leaves a parent mapping
  with no remaining active children, remove the now-empty parent line too (an empty
  mapping key would change the parse).

Keep the existing writer's whole safety contract: guided by parsed `yaml.Node`
positions, textual line splice, result re-parsed and compared against the original
`fileConfig` apart from the target path (generalize `sameApartFromHosts`,
`configwrite.go:333`, to `sameApartFrom(path)`), refuse flow-style lists and
multi-document files, seed the template first when the file is absent
(`configwrite.go:89` precedent), atomic temp+rename write (`writeConfigAtomically:342`).
Value rendering follows the template's own style (strings quoted as the template quotes
them, bools/ints bare).

**Tests:** golden-file table tests under `cmd/apogee/testdata/`: seeded template and
user-edited variants × {key active → replace; key absent with example → insert below
it; no example → append; nested with absent parent → parent created; delete → line
gone; delete last child → parent gone}; a property check that after every op the result
parses and differs from the input only at the target path; loud-failure cases (flow
style, multi-doc, example-match ambiguity) refuse without writing.

**Acceptance:** `go test ./cmd/apogee/ -run 'Splice|ConfigWrite'` passes; existing
`/confine off --save` tests stay green; `make check` passes.

**Commit:** `feat(config): comment-preserving scalar splice writer with insert-below-example and delete`

## 5. Settings rows seam: registry → tui.Options — ✅ DONE (2026-08-05)

**NOTES (2026-08-05):** three deviations. (1) The `(env)`/`(flag)` markers needed a fact the resolved
`options` did not carry — precedence collapses the layers, so afterwards nothing says which source won.
`config.go` gained `configSource` + `overrideSources` (the same predicates `flagLayer`/`envLayer` are
built from, read off the same registry rows), `applyConfig` records the result on the new
`options.overrides`, and the builder reads it; guarded by `TestOverrideSourcesNameTheWinningSource` and
`TestApplyConfigRecordsOverrideSources`. (2) The builder itself lives in a new `cmd/apogee/settingsrows.go`;
`wire.go` carries only the one-line `SettingsRows:` wiring beside `SaveHostAcknowledgement`, as the item
says. (3) Section names are AUTHORED here rather than lifted from the template: the template's `# ===`
dividers are per-key, so a header per divider would be one header per row. `settingSections` is an
ordered table of section RUNS over registry order (ten sections), pinned to that order by
`TestSettingSectionsOpenInRegistryOrder` so a key added to the registry inherits its section.

**What:** Depends on item 2. A new plain-data row type in `internal/tui`
(suggest `SettingRow`: key path, display section, kind, current value string, default
string, source marker, restart/editable/masked/read-only-pointer fields, enum values,
a one-line description) and a provider seam `tui.Options.SettingsRows func() []SettingRow`
(`internal/tui/tui.go:198` Options block), populated in `cmd/apogee/wire.go` (beside the
`SaveHostAcknowledgement` wiring, `wire.go:513-518`) from the registry + the resolved
`options`: effective value formatting, `(env)`/`(flag)` source markers when an override
beat the file for that key this run, `api-key` masked to `••••`, structured rows
summarized ("3 servers — edit in config.yaml"), rows in template order carrying their
section names. The provider derives rows fresh on every call (the TUI calls it at
render, per the derive-at-render convention, `picker.go:23-27`). No UI in this item.

**Tests:** unit tests on the wire-side builder with a fabricated resolved config: row
count and order match the registry, masking, override markers, structured summaries,
section grouping.

**Acceptance:** `go test ./cmd/apogee/ ./internal/tui/` passes; `make check` passes.

**Commit:** `feat(tui): settings-row seam from the key registry into tui.Options`

## 6. /settings verb + full-height read-only pane — ✅ DONE (2026-08-05)

**NOTES (2026-08-05):** three deviations. (1) The row schema is FOUR cells, not the item's two:
`[key, value, "(env)"|"(flag)", "· edit in config.yaml"]`. The Column contract the item invokes
forbids pre-concatenated tiers (`layout.md`: "a row is a slice of CELLS, not a pre-concatenated
string"), and those two extra tiers are item 5's `Source` / `EditPointer` fields, which have no other
renderer in the plan — item 7's own text ("rows with an env/flag source marker") presupposes item 6
already draws the marker. Both columns collapse entirely on a config with no override and no
structured block, so the schema costs nothing where it says nothing. (2) The pane draws a ONE-LINE
body: the selected row's `Desc`, claimed through `popupFloor{body: 1}`. `popupBudget` keeps one line
for a body on every pane whether or not it draws one (`maxRows ≤ avail−1`), so a bodyless pane hands
that row straight back to the transcript and the item's own "the transcript gives way fully" would be
false by exactly one row at every window size; `SettingRow.Desc` is documented by item 5 as "the
one-line description shown for the selected row" and had no other renderer. It is truncated to the
inner width here, so the one budgeted line always holds the description rather than an elision
marker. (3) `TestOnlyTheSessionResetPairIsNotRecallable` is renamed
`TestOnlyResetAndPureUIVerbsAreNotRecallable`: `/settings` is `noRecall` per ratified call, so the
pinned set is three verbs and the old name stated something the test no longer asserts.

**What:** Depends on item 5. Add the `/settings` row to `commandSpecs`
(`internal/tui/command.go:126-142`, alphabetical between `/server` and `/skills`;
`takesArgs: false`, `whileRunning: false`, `noRecall: true`) and its `runCommand` case
(`internal/tui/model.go:1389`). New `internal/tui/settings.go`: a `settingsPane` struct
on the Model, zero value = closed (the `picker`/`sessionBrowser` shape, ADR 0011 rules
in `internal/tui/doc.go:444-453` — no no-copy types by value, slices replaced not
mutated). Wire it as the fifth `framePane` (`paneSettings` in `model.go:3901-3907`,
`openPanes` `:3922`, `frameRowPlan` `:3968`, `popupBudget` `:4081`) with the new budget
rule: while open, the settings pane is granted the entire transcript budget (the
transcript gives way fully); the existing floors are untouched — four-row pane minimum,
no pane below a 12-row window, and a pane that gives way entirely leaves its fact on the
status line (`layout.md:184`). Add a `frameOverlays` slot (`model.go:2844`, `:2876`) and
a key-routing branch beside browser/picker (`handleKey`, `model.go:840-860`): while
open the pane swallows every key; ↑/↓ move the ❯ selection with wrap
(`(sel±1+n)%n`, the `pickerKey` idiom `picker.go:519`), selection clamped against
re-derived rows, esc closes, ⏎ is a no-op in this item. Render via `renderPopup`
(`popup.go:263`): title "Settings", two-cell rows `[key, value]` from
`m.opts.SettingsRows()`, section-header rows as single-cell rows, scroll window sized
to the granted budget (not the 8-row cap), hint line "↑/↓ select · ⏎ edit · esc close";
every cell escape-stripped and measured through `th.measure` (ADR 0030). Amend
`layout.md` with a new section documenting the full-height pane class (this item owns
that amendment): its budget rule, floors, give-way fact, and its place in the give-way
order.

**Tests:** command-table alphabetical test updated; open/close/navigation/clamp key
tests; paint tests in the `paint_test.go` harness for the pane at full height, at a
short window (floor behavior), and for the give-way status-line fact;
`TestModelNoBuilderByValue` stays green.

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes.

**Commit:** `feat(tui): /settings opens a full-height read-only settings pane`

## 7. Editing v1a: bool toggle and enum sub-list, persisted per edit — ✅ DONE (2026-08-05)

**NOTES (2026-08-05):** five deviations. (1) `mode` DOES record its edit in the pane's pending state, and what
"shows no marker" buys is the VALUE cell: the rows provider closes over the resolution captured at wire time
(`wire.go`'s `settingsRows(opts)`), so after a live apply nothing in the pane would otherwise ever show the new
mode — the row would sit at its pre-edit value with no marker, an edit indistinguishable from one that failed.
`settingsValueCell` shows a live-applied edit's value; only restart-required rows get the "(next launch)" marker.
(2) The marker shares the row's LAST cell with item 6's `EditPointer` instead of adding a fifth column: a row
either cannot be written here (pointer) or was written by this pane (marker), never both, and at 80 columns a
fifth tier would cost every row's value about 25 cells. (3) The enum sub-list opens on the value the key HOLDS
rather than resetting the highlight to the first row — the item's "reset the sub-selection" via the `/schedule`
precedent — because on `mode` a reset-to-0 would make ⏎⏎ silently set the ladder's first rung; with the current
value highlighted, ⏎⏎ re-sets what is already there, which item 4's writer no-ops. (4) The override note is
ordered AFTER the live-apply case, so a live-applied row never claims to be "overridden … this run": the live
apply outranks the source that beat the file at resolution time for as long as the run lasts. (5) Two existing
tests changed shape with the code they cover: `TestSettingsPaneEscCloses` compares the closed pane through
`reflect.DeepEqual` (the pane now holds a slice of edits, so `==` no longer compiles), and
`settingsDisplayRows`/`settingRowCells` became Model methods (the value and note cells read the pane's own edit
state), so their two unit tests call them on a zero Model. `layout.md` also gained one paragraph — the value
sub-list is the one state in which the full-height pane is short — which item 6's amendment did not cover.

**What:** Depends on items 4 and 6. Writer seams into the pane: `tui.Options` gains
`WriteSetting func(path, value string) error` and `ResetSetting func(path string) error`,
wired in `wire.go` to item 4's splice operations (the renderer never touches YAML — the
`SaveHostAcknowledgement` seam precedent). In the pane: ⏎ on an editable bool row
toggles and persists immediately; ⏎ on an editable enum row switches the pane body to a
value sub-list (`menuRows: true`, the `/schedule` two-step precedent — flip a pane-state
kind and reset the sub-selection, `schedule.go:204`), ⏎ persists the highlighted value,
esc backs out without writing. After a successful write: `mode` also applies live
through the existing `SetMode` seam (`model.go:913-920`) and shows no marker; every
other row records the edit in the pane's pending state and renders a
"→ <value> (next launch)" marker (rows with an env/flag source marker instead render
"saved — overridden by <VAR> this run"). A failed write surfaces the error inline on
the row and changes nothing else. Pending state lives on the pane struct and is
display-only (no file re-reads; replaced wholesale per the value-model rules).

**Tests:** key-flow tests with fake writer funcs capturing (path, value): toggle
persists the right rendered value; enum sub-list commit/back-out; mode live-applies via
a captured engine call and skips the marker; pending markers render; override-note
path; write-error path leaves state unchanged.

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes.

**Commit:** `feat(tui): settings pane edits bools and enums, persisting per edit`

## 8. Editing v1b: string/int buffer, validation, reset-to-default — ✅ DONE (2026-08-05)

**NOTES (2026-08-05):** seven deviations. (1) The validate hooks are on the registry rows as the item says, but
the PANE never calls one — it cannot (ADR 0011: the registry is the binary's). `saveConfigSetting` runs
`configKey.Validate` before it opens the file, and the pane's half of "invalid input renders an inline error and
stays in the buffer" is the existing refusal path: a `WriteSetting` error keeps `settingsValueBuffer` open with the
reason in the row's note cell. (2) Validation sits in `saveConfigSetting` rather than inside the splice so its
message is NOT prefixed with `update config "<path>":` — the note cell truncates at the column, and the pane would
have shown the file path where the reason belongs (the validators are worded key-first for the same reason; the
splice's own kind check is left where it is). (3) A commit of an EMPTY buffer writes nothing and just closes (the
`/sessions` empty-rename precedent): `api-key`'s buffer seeds empty by design, so a committing-empty rule would
make ⏎⏎ wipe the key, and the deliberate way to take a value away is the reset. (4) A masked row's marker is now
`saved (next launch)` instead of item 7's `→ •••• (next launch)` — item 7's own doc says "'saved' is the whole of
what the row has to say about an api-key", and the mask fallback read `row.Value`, which is `""` for a key that was
unset at resolution time, so a first api-key write would have rendered a marker that trailed off. (5) A reset is
recorded as the edit it is: `settingEdit` gained `reset`, `recordEdit`/`editOf` carry the record rather than the
value alone, `settingsWrite` split into `settingsPersist` (which reports whether the write LANDED — the buffer is
the one caller whose next move depends on it) and `settingsApplied`, which now homes the `mode` live apply for both
a write and a reset. That is also item 7's carried-over follow-up answered: the reset of `mode` goes through the
same seam, and `TestModeIsTheOnlyKeyAppliedWithoutARestart` (beyond the item's test list) pins the
`settingsModeKey` ↔ `RestartRequired:false` equivalence the pane's show/apply split rests on. (6) "Active (file-set
or just-edited)" is read as `edited-by-this-pane || Value != Default`: the renderer cannot know what the file sets,
and a row already showing its default has no line worth removing — which is exactly the item's "reset on a
default-valued row is a no-op", implemented as backspace arming nothing. (7) Two existing tests changed shape with
the code they cover: `TestSettingsPaneEnterNeverWritesARowItMayNotEdit` lost its string/int rows (⏎ now opens their
buffer — the new buffer tests own them) and gained backspace coverage of the same refusal, and
`TestSettingsPaneNavigationWrapsAndSwallowsEveryOtherKey`'s "⏎ on a string row is a no-op" became "⏎ opens the
row's buffer". Registry-side accept/refuse tables and a hook-coverage guard are also beyond the item's list.

**What:** Depends on item 7. ⏎ on an editable string/int row opens a caret edit buffer
(the `/sessions` rename idiom: hand-rolled mini-editor, esc cancel, ⏎ commit,
backspace rune-pop, `▏` caret cell — `sessions.go:264`, `:478`); `api-key` characters
are visible while editing, masked again on commit. On commit, validate before writing:
registry rows carry a validate hook reusing the existing parse/validate sites
(`ParseCursorShape`, `ui.validate` `config.go:413`, mode parse, int parse for
`context-window`/`present.port`, URL parse for `endpoint`/`web-search-endpoint`);
invalid input renders an inline error and stays in the buffer. Reset: backspace on a
selected active (file-set or just-edited) row arms a reset, the hint line switches to
"⏎ confirm reset · esc cancel", ⏎ calls `ResetSetting` and clears the row's pending
marker (value returns to default — "(next launch)" marker unless the key is `mode`,
which also live-applies its default).

**Tests:** buffer key-flow tests (commit, cancel, api-key masking states); a validation
table per validator with an invalid input each, asserting no write and an inline error;
reset flow (arm, cancel, confirm) with fake writer funcs; reset on a default-valued row
is a no-op.

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes.

**Commit:** `feat(tui): settings pane edits strings and ints with validation and reset-to-default`

## 9. Closing docs: README, CHANGELOG, ISSUES

**What:** Depends on items 1–8. README: a `/settings` subsection in the command
documentation (what it shows, persist-per-edit, reset, the read-only structured blocks,
idle-only), keeping the existing "your edits are never overwritten" wording accurate
(unprompted writes still never happen). CHANGELOG: an entry under the unreleased
heading — no version identifier changes. ISSUES.md: remove the settings-screen entry
(line 5 at baseline; the grilling it asked for is this plan's ratified-calls record).
The template (`cmd/apogee/defaults/config.yaml`) header comment gains one line noting
`/settings` can view and edit these values in-app.

**Tests:** none (docs only).

**Acceptance:** `grep -n "settings" README.md CHANGELOG.md` show the additions;
`grep -c "full screen menu" ISSUES.md` returns 0; `make check` passes.

**Commit:** `docs: document /settings, close the ISSUES entry, changelog`

## Suggested version bump

Not performed by this plan. When the plan completes, a minor bump (0.8.x → 0.9.0) is
warranted: `/settings` is a new user-facing surface and the first general config-write
capability. The owner decides whether and when.

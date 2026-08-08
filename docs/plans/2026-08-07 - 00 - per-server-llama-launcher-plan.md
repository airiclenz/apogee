# Per-server llama-launcher key — implementation plan

- **Goal:** move the llama-launcher integration from the global top-level `llama-launcher:`
  key to an optional per-`servers:`-entry key, so `/model` offers Launch profiles only while
  the session is ON the launcher-fronted entry, and every other server (e.g. OpenRouter)
  keeps its advertised-model discovery. `/unload-model` and `/stop-server` follow the same
  per-entry enablement (they already refuse unmanaged endpoints; only their on/off source
  moves).
- **Date:** 2026-08-07
- **Status:** not started
- **Authoritative sources:** ADR 0029 (launcher integration; D4 is what this plan
  supersedes), ADR 0036 (servers list is the single definition; refusal-over-silence
  posture), ADR 0037 (settings live-apply; the llama-launcher row leaves that surface),
  `docs/design/` launcher facade contract as quoted in `cmd/apogee/launcher.go`'s file
  header. Code line numbers below are as of commit `74379d0`.
- **Skills:** coding-standards
- **Out of scope:** launcher rows in `/server` (ADR 0029 rejects), cross-server
  `/model <profile>` matching (rejected design call 4), auto-migration of the old key
  (rejected design call 3), any version identifier change (see closing note), any
  `internal/tui` change (the renderer's `ErrNoLauncher` → advertised-models fallback at
  `internal/tui/picker.go:397-405` already expresses the new behavior; item 2 verifies it
  from the wire side).

## Ratified design calls

Decided by the owner, 2026-08-07, via AskUserQuestion (calls 1–4); calls 5–8 bound at plan
write time from the existing documented postures they extend:

1. **Gate = per-server key.** Each `servers:` entry may carry its own launcher setting;
   `/model` shows Launch profiles only while the session is on that entry. Rejected:
   address-matching auto-detection.
2. **Key shape = path on the entry.** Entry field `llama-launcher:` with value `auto`
   (resolve the launcher's `DefaultConfigPath()`) or an explicit config path; key absent =
   launcher off for that server. The top-level `llama-launcher:` key retires from the
   schema; its `/settings` registry row and live-apply path are removed (the `servers:`
   block already reads "⏎ opens $EDITOR"). Rejected: split global-path + per-entry flag.
3. **Legacy handling = refuse with fix.** A config.yaml still carrying the top-level
   `llama-launcher:` key fails startup with a ready-to-paste per-entry example — never
   silent (ADR 0036 posture; pre-production, no shim owed). Rejected: auto-migration.
4. **Remote flow = two steps.** On a non-launcher server `/model` never offers profiles;
   coming home is `/server <name>`, then `/model`.
5. **A profile-load move preserves the launcher.** The resolved launcher path is installed
   by startup entry selection, `bindServer`, and `/server` switches to entries; a launcher
   profile load that MOVES the session (possibly to an endpoint no entry names) leaves the
   holder untouched, so the integration stays on for the session that just used it. The
   ephemeral `--endpoint` override entry carries no launcher key, so the integration is off
   there.
6. **`auto` resolves `DefaultConfigPath()` verbatim** — no `os.Stat`, no absoluteness
   check. The old empty-key auto-detect stat-gated because it lit up silently; `auto` is an
   explicit opt-in, so a missing config degrades at the first verb, naming the path (the
   same posture the named-path case already documents at `launcher.go:115-117`).
7. **Per-entry `off` is refused at validation** ("remove the key to disable") — absent is
   the off state, and two spellings of one state invite drift. Whitespace-only and
   URL-scheme values are refused with `validateLlamaLauncher`'s existing reasoning, adapted.
8. **`auto` matches case-insensitively, trimmed** — the posture the old key's `off`
   sentinel already had (`launcher.go:123-125`).

## 1. Per-entry key: schema field, validation, resolver — ✅ DONE (2026-08-08)

**What:** Additive only — the global key keeps working untouched this item.

- `cmd/apogee/config.go`: `serverEntry` (lines 928-933) gains
  `LlamaLauncher string` with yaml tag `llama-launcher,omitempty`. Doc comment states the
  three shapes (absent = off for this server, `auto`, path) and that this is ADR 0029 D4's
  key moved per-entry (cite this plan's date; the ADR amendment itself is item 5).
- `cmd/apogee/config.go`: `validateServers` (lines 946-964) gains per-entry checks on the
  new field per design calls 7–8: refuse whitespace-only ("set auto, a config path, or
  remove the key"), refuse any value containing `://` (adapt `validateLlamaLauncher`'s
  URL message, lines 988-996), refuse `off` in any casing ("remove the key to disable").
  Error messages name the entry index and name, matching the neighbouring checks' style.
- `cmd/apogee/launcher.go`: new resolver `entryLauncherPath(value string) (string, bool)`
  beside `launcherConfigPath` (lines 104-147, which stays for now): trimmed empty → off;
  `auto` (EqualFold) → `llamalauncher.DefaultConfigPath()` verbatim (design call 6); else
  `expandUserPath` with the same keep-as-written fallback the old ladder has (lines
  127-135). Doc comment carries design calls 5–6 in prose.

**Tests:** `cmd/apogee/launcher_test.go` — resolver table: empty, `auto`, `AUTO`, ` auto `,
`~/x.yaml`, absolute path, expand-failure fallback. `cmd/apogee/config_test.go` —
validateServers accepts absent/`auto`/path, refuses whitespace-only, `off`, `OFF`, and
`http://…` values with messages naming the entry.

**Acceptance:** `go build ./... && go test ./cmd/apogee/` (and `make check` before commit).

**Commit:** `feat(config): add per-server llama-launcher key with auto/path resolution`

## 2. The launcher path follows the session's server entry — ✅ DONE (2026-08-08)

Depends on item 1.

NOTES (2026-08-08): three deviations from the literal text. (a) The two-line install is a
`(*launcherPath).follow(entry serverEntry)` method in `launcher.go` that both closures call, rather
than the lines inlined twice — one place for design call 5's "and NOT the mover" rule, with the
short comment still at each install site as the item asks. (b) The holder construction
(`entryLauncherPath(opts.startupLauncher)` + `newLauncherPath`) moved UP to just above the
`switchServer`/`bindServer` closures, because Go resolves a captured local only if it is declared
first; the seams block keeps `launcherSeams` and its (updated) comment. (c)
`TestRunRootWiresTheLauncherSeamsForTheWholeSession` was rewritten onto `startupLauncher` — the
global key no longer reaches the holder, so its `off`-key case failed as of this item; item 3 still
owns deleting the key itself.

**What:** The `launcherPath` holder (`cmd/apogee/launcher.go:149-185`) keeps its type and
its four seams; what changes is who sets it.

- `cmd/apogee/config.go`: `options` gains `startupLauncher string` — the SELECTED startup
  entry's `LlamaLauncher` value as written, set where `resolveStartupEntry`'s result is
  folded into opts (around lines 1565-1581). The ephemeral override entry (ADR 0036 D6)
  never carries one, so it resolves to "" (design call 5). The global `llama-launcher:`
  key no longer feeds the holder after this item (its `/settings` live-apply case still
  compiles until item 3 removes it — transient, and stated in the commit body).
- `cmd/apogee/wire.go:500`: `startPath, _ := entryLauncherPath(opts.startupLauncher)`. A
  pre-bound start (no startup entry) leaves the holder empty; the verbs answer
  `tui.ErrNoLauncher` until a bind installs a value.
- `cmd/apogee/wire.go` `switchServer` (line 441) and `bindServer` (line 478): after a
  successful `mover.move(...)` / `binder.bind(entry)`, install
  `path, _ := entryLauncherPath(entry.LlamaLauncher); launcher.set(path)`. The install
  sits in these two closures and NOT in `sessionMover.move` — the profile-load move
  (`launcherWiring.load`, `launcher.go:556-641`) reaches the mover directly and must
  preserve the holder (design call 5). State that rule in a comment at the install sites.
- `cmd/apogee/launcher.go`: update the now-stale doc comments — `launcherPath`
  (149-162, "editable in the /settings pane" goes), `launcherWiring` (484-500, "ADR 0037"
  enablement story becomes "follows the session's server entry"), and the file-header
  mention if any.

**Tests:** `cmd/apogee/wire_test.go` — with a fake `launcherOps`: (a) startup on an entry
with `llama-launcher: auto` → `launcherSeams.on()` true; (b) startup on a plain entry →
`ErrNoLauncher` from a verb; (c) `switchServer` to a launcher entry turns the verbs on,
switching back to a plain entry turns them off; (d) `bindServer` from pre-bound installs;
(e) a move through `sessionMover.move` directly (the profile-load path) leaves the holder
unchanged. Reuse the existing wire-test scaffolding for closures.

**Acceptance:** `go build ./... && go test ./cmd/apogee/` (and `make check` before commit).

**Commit:** `feat(wire): launcher integration follows the session's server entry`

## 3. Retire the top-level `llama-launcher:` key — ✅ DONE (2026-08-08)

NOTES (2026-08-08): three deviations from the literal text. (a) Two test files the item's list does
not name also pinned the retired key and had to be adjusted: `configwrite_scalar_test.go` (three
splice cases keyed by the now-absent registry row, which `mustKey` panics on) and `launcher_test.go`
(`TestLauncherConfigPathLadder`, the deleted ladder's own test). (b) `defaults_test.go` needed NO
change — its template↔registry guards pass with the row and the teaching block gone — so it is
untouched rather than adjusted. (c) Doc comments on neighbouring, surviving declarations that named
the deleted field were adjusted where they would otherwise dangle: the `editor` key's three docs
(`config.go` resolved + layered, `root.go`, and `fileConfig.Editor`, all of which said "like
llamaLauncher above"), `registry.go`'s validate-hooks prose, `wire.go`'s `unreachable` doc, and
`launcher.go`'s `entryLauncherPath` header (which introduced itself as the successor to the deleted
ladder). Item 5 still owns every cross-cutting doc edit, including README's "Local servers —
llama-launcher" section, which still teaches the retired top-level key.

Depends on items 1 and 2. One item despite its file count: the registry bijection guard
(registry rows ↔ `fileConfig` yaml tags) and the template↔registry guard
(`defaults_test.go`) compile-couple every site below — split, and an intermediate commit
fails `make check`. Every edit is an enumerated deletion or a doc-prose move.

**What:**

- `cmd/apogee/config.go`: delete the top-level key's sites — resolved field + doc (59-68),
  layered field (437-443), merge line (661-662), `fileConfig.LlamaLauncher` (785-794),
  `validateLlamaLauncher` (966-996, its reasoning now lives in `validateServers` per
  item 1), layer fold (1201-1202), validate call (1547-1551), opts assignment (1581).
- `cmd/apogee/root.go`: delete the `llamaLauncher` options field + doc (85-91) — adjust to
  wherever item 2 left the struct.
- `cmd/apogee/registry.go`: delete the `llama-launcher` row (149-152).
- `cmd/apogee/settingsrows.go`: delete the formatter (line 92).
- `cmd/apogee/wire.go`: delete the applier's `launcher` field (1216-1217), the
  `"llama-launcher"` apply case (1307-1316), and the reaches case (1412-1413). The
  `launcherPath` holder itself stays — item 2's folds are its only writers now.
- `cmd/apogee/launcher.go`: delete the old `launcherConfigPath` ladder (104-147) — item 2
  left it callerless.
- `cmd/apogee/defaults/config.yaml`: the global-key teaching block (~lines 78-108) is
  replaced by per-entry documentation inside the `servers:` block prose: the three shapes
  (absent / `auto` / path), an example entry carrying `llama-launcher: auto`, and the
  note that the launcher's MCP adapter under `mcp-servers:` remains the remote answer
  (ADR 0029 D4's container case, which survives this change).
- Tests across `config_test.go`, `registry_test.go`, `settingsrows_test.go`,
  `wire_test.go`, `defaults_test.go`: drop cases pinning the deleted key; the bijection
  and template↔registry guards must pass with the row, tag, and template block gone.

**Tests:** the adjusted guard suites above are the tests; add none beyond them.

**Acceptance:** `go build ./... && go test ./cmd/apogee/` green;
`grep -rn "llamaLauncher\|launcherConfigPath" cmd/apogee --include="*.go" | grep -v _test`
finds no top-level-key survivors (the per-entry `LlamaLauncher` field and
`entryLauncherPath` remain); `make check` before commit.

**Commit:** `feat(config)!: retire the top-level llama-launcher key`

## 4. Legacy refusal for the retired key

Depends on item 3.

**What:** `cmd/apogee/configmigrate.go` — extend the legacy detection (the
`legacyFileConfig` sniff, lines 35-95) to the retired `llama-launcher:` top-level key,
refusing rather than migrating (design call 3): a file that sets it fails startup with a
message naming the line and a ready-to-paste fix — remove the top-level line, add
`llama-launcher: <its value, or auto>` to the `servers:` entry the launcher fronts, shown
as a complete example entry block. The check runs BEFORE any quadruple-key migration
write, so a file is never half-rewritten and then refused; when both are present, the
refusal wins and no backup is created. A file without the key never reaches any of it.

**Tests:** configmigrate/config tests: key present alone → error containing the paste-able
`llama-launcher:` entry line; key present + the legacy quadruple → error, and no
`config.yaml.bak-*` sibling was written; key absent → existing migration and clean-parse
paths unchanged (existing tests stay green).

**Acceptance:** `go build ./... && go test ./cmd/apogee/ -run 'Legacy|Migrat|Config'`
green; `make check` before commit.

**Commit:** `feat(config): refuse the legacy top-level llama-launcher key with a paste-able fix`

## 5. Docs: ADR amendments, CONTEXT.md, CHANGELOG

Depends on items 1–4. This is the single owning item for every cross-cutting doc edit.

**What:**

- `docs/adr/0029-…md`: dated amendment under decision 4 recording the supersession — the
  key moved into `servers:` entries (absent/`auto`/path), why (a global launcher captured
  `/model` on every server of a multi-server config, overriding advertised-model discovery
  on remote entries like OpenRouter), the owner's four calls of 2026-08-07, and design
  call 5's preserve-on-profile-load rule. The auto-detect-by-existence posture is
  superseded by explicit `auto`; the container/MCP composition note stands.
- `docs/adr/0037-…md`: dated note that the `llama-launcher` row left the settings surface
  with the key — enablement now follows `/server` moves, so the live-apply case is gone.
- `CONTEXT.md`: the llama-launcher / Launch profile entries (~lines 189-198) and any
  other mention of the top-level key (check ~line 916's move paragraph) updated to the
  per-entry form and the follows-the-entry enablement rule.
- `CHANGELOG.md`: entry under the unreleased heading describing the schema break, the new
  per-entry key, and the startup refusal for the old key. Do NOT touch `VERSION` or any
  release heading.

**Tests:** none (docs). **Acceptance:** `make check` still green;
`grep -rn "llama-launcher:" docs/adr/0029*.md CONTEXT.md` shows the per-entry form
documented; CHANGELOG diff touches only the unreleased section.

**Commit:** `docs: record the per-server llama-launcher supersession (ADR 0029/0037, CONTEXT, CHANGELOG)`

## Suggested version bump

Minor (0.x line): a config-schema break with a refusal path for old files. Suggestion
only — whether and when to bump is the owner's call; no item touches a version identifier.

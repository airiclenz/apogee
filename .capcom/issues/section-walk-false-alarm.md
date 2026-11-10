---
title: Unknown-key walk falsely flags every ui.side-panel.sections and sessions.watch-homes entry
status: done
type: bug
priority: 3
plan: 2026-11-10 - 01 - panel-and-tool-shape-fixes-plan
created: 2026-11-10T10:44:53Z
closed: 2026-11-10T12:51:54Z
reason: "Resolved by plan '2026-11-10 - 01 - panel-and-tool-shape-fixes-plan' item 1 (archived at f53b8f0d): the unknown-key walk no longer descends into entry structs that declare no yaml keys; misspelled keys beside the list are still reported."
---

## Symptom

At startup and on every live config re-read, the unknown-key walk (`internal/config/unknownkeys.go`) prints a false "unknown key" notice for a key that is legitimate, validated and loads correctly:

```
apogee: config /home/airic/.apogee/config.yaml: unknown key "ui.side-panel.sections[4].mcp" at line 917 is ignored
```

The line in question (line 917 of the home config) is the documented syntax for `ui.side-panel.sections` (docs/manual/configuration.md, "The side panel"):

```yaml
ui:
  side-panel:
    sections: [ usage: pinned, sub-agents: auto, tasks: auto, changed-files: auto, mcp: auto ]
```

The loader reads this line fine — `sidePanelSectionsConfig.UnmarshalYAML` (internal/config/config.go:3074) parses it and `domain.ParseSidePanelSections` validates every section name and mode. Nothing is actually ignored. The notice is pure noise, printed once per list entry: the five-entry list above produces five notices (`sections[0].usage` … `sections[4].mcp`). Reproduced directly against `unknownKeys()` with a throwaway probe in both spellings — the one-line flow list and the one-entry-per-line block list; both are flagged, so there is no formatting dodge for the user.

## Root cause

The walk derives the config schema by reflection from `fileConfig`'s yaml tags (`schemaKeys`, the reader shared with the registry bijection test) and descends only into struct, slice-of-struct and map-of-struct fields. `ui.side-panel.sections` is typed `sidePanelSectionsConfig []domain.SidePanelEntry` — a slice of structs, so the walk descends into each list entry and reads its keys against `domain.SidePanelEntry`'s fields. But `SidePanelEntry` (internal/domain/sidepanelprefs.go:142) is a value struct with **no yaml tags at all**:

```go
type SidePanelEntry struct {
    Section SidePanelSection
    Mode    SidePanelMode
}
```

The entry's single mapping key — `usage: pinned`, `mcp: auto` — is **data** (a section name), not a schema key, so the walk flags it as unknown. The specialised parser already validates exactly those names and refuses an unknown one with a named message (`unknown section "lsp"`), which is better than anything the generic walk could say.

**Second instance, same defect:** `sessions.watch-homes` is typed `watchHomesConfig []watchHomeConfig`, and its entry struct is also fully untagged (`watchHomeConfig{Label string; Path string}`, internal/config/config.go:3195). Every user-chosen watch-home LABEL is flagged: `unknown key "sessions.watch-homes[0].<label>"`. Labels are validated by `validateWatchHomes` (config.go:377).

**Confirmed not affected** (probed): `ui.footer.rows` (slice of strings), `ui.footer.colors`, and `servers[].request-extra` (custom-unmarshalled into a comparable string, so the walk does not descend into it — its only false-positive-free behaviour is correct). A probe file that put `request-extra:` under `mcp-servers:` was correctly flagged — the key belongs on `servers:` entries.

## Self-write loop

apogee itself writes the offending line: clicking a section header's pin glyph in the side panel toggles pinned/auto and saves `ui.side-panel.sections` to the global config through the settings host (cmd/apogee/wire_options.go:603; test `TestSettingsHostWritesBothSidePanelSectionsBlockKeys`). So a config apogee itself wrote draws the false notice at the next start or live re-read — apogee arguing with itself. Any user who ever pinned a section through the UI carries the trigger.

## Fix direction

Two sane shapes; pick one:

1. **Derived rule (preferred):** in `descendUnknownKeys`, do not walk a slice-of-struct (or map-of-struct) field whose element struct declares **no yaml keys at all** — an entry struct the schema gives no keys for cannot hold a mis-spelled key; its mapping keys are inherently data. This fixes both instances with no hand-maintained list and cannot regress normal structs (every configurable struct has at least one tagged field). Keep `schemaKeys` unchanged — the fix lives in the walk only, so the registry bijection test stays honest.
2. **Explicit exemptions:** extend the existing per-key exemption pattern in `walkUnknownKeys` (cf. `subAgentsKey` on `serverEntryType`, `stepBudgetNoticeKey` on `fileConfigType`) with the two fields. Simplest diff, but every future data-keyed setting must remember to add itself.

Do not silence the notice by weakening the walk generally — a misspelled key under `side-panel:` (`widht:`) must still be reported.

## Suggested tests

`internal/config/unknownkeys_test.go` has no side-panel or watch-homes coverage today. Add:

- a config with the documented `ui.side-panel.sections` list (mix of modes, flow style) → zero notices;
- the same in block style → zero notices;
- `sessions.watch-homes` with a label → zero notices;
- a genuinely unknown key beside them (`ui.side-panel.widht: 80`, `ui.side-panel.bogus: 1`) → still flagged, notice text unchanged.

Run scoped (slow-box rules: never a whole-package `-race` run): `GOMEMLIMIT=2GiB go test -race -count=1 -run TestUnknownKeys ./internal/config/`.

## Notes for whoever picks this up

- Read `internal/config/unknownkeys.go` in full first — the walk is deliberately strict about what it descends into, and its comments explain the two existing exemptions.
- `ui.side-panel.folded: []` is NOT affected (slice of plain strings).
- The notice is cosmetic: startup proceeds and the setting takes effect. Priority reflects annoyance, not breakage.

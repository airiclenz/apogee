# system-prompt-layers — an explicit additive prompt channel

**Goal:** Add an opt-in `system-prompt-layers:` config key whose entries append, in listed
order, to whatever prompt the existing ladder selects — if it is enabled it is sent, and
nothing the user did not enable is sent. The four-rung ladder, whole-entry replacement and
the embedded default's fallback-only role are unchanged (ADR 0023 §2, ADR 0064 §2–§3).

**Date:** 2026-09-01
**Status:** unexecuted
**Sized for:** ~200k-context host

**Sources:**
- docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md
- docs/adr/0064-the-system-prompt-ships-an-embedded-default.md
- docs/manual/configuration.md:837–931 · internal/agent/loop.go:895 (standingSystem — untouched)

**Ratified design calls** (owner, 2026-09-01):
- **Key name:** `system-prompt-layers:`, beside `system-prompt-text`/`system-prompt-file`.
- **Not a rung:** layers append after the selected prompt; they never trigger the embedded
  default — the default still fires only when nothing at all is configured.
- **Order:** selected prompt first, then layers in listed YAML order, joined by "\n\n".
- **Layer shape:** each entry states exactly one of `text:`/`file:`; a `file:` resolves like
  `system-prompt-file` (ExpandUserPath; relative paths against the config home).
- **Settings row:** registered under "System prompt", external-edit like `system-prompt-models`.

**Regression check (2026-09-01):** 2 — recast; 2 — guard folded (registry bijection); 4 — recast; 4 — recast (2026-09-01, seed-from-resolution: liveSettings.promptEditorSeed seeds only when the whole resolution IS the embedded default — Global.Text=="" AND Global.File=="" AND len(Layers)==0 AND ResolveSystemPrompt equals DefaultSystemPrompt; len(Models)==0 dropped; previous "2 — recast; 4 — recast" lines kept).

**Standing requirements:** skills: coding-standards.

**Out of scope:** engine changes (internal/agent untouched); rung/`use-default-prompt`
semantic changes; per-model layers; export command; multi-line editor editing of layers;
CHANGELOG (lands at closeout via item sidecars).

---

## 1. ADR 0067 — system-prompt-layers + CONTEXT.md vocabulary — ✅ DONE (2026-09-01)

NOTES (2026-09-01): the ADR's front matter carries `Amends: ADR 0023 (decision 1's key count)`; ADR 0023 §1 and ADR 0064 §2 are deliberately NOT edited — the item's regression guard exempts "0023/0064 historical quotes", so both counts stay as historical record with 0067 named as the current one.

NOTES (2026-09-01): regression-guard sweep run — `grep -rn "system-prompt-text" docs/ CONTEXT.md ISSUES.md | grep -v layers`. Live-doc hits fall in three classes, all resolved: CONTEXT.md:1043 amended by this item; docs/manual/configuration.md is item 5's; ADR 0037's live-apply list and ADR 0035's persist list are named in 0067's Consequences (`system-prompt-layers` joins `system-prompt-models`'s external-edit class in both) rather than rewritten. ADR 0018:199 and ADR 0026:48/157/205/209 quote the seeded-template premise, not the key set, so they are not stale.

NOTES (2026-09-01): docs/layout/settings-screen-layout.md:56 shows an illustrative settings frame listing `system-prompt-text`; it is a rendering sample, not a key-set listing, and the golden settings frame is item 4's to regenerate — left untouched here.

**What:** Write `docs/adr/0067-system-prompt-layers-are-an-explicit-additive-channel.md` in
house style (read 0023 and 0064 for form): context — the one-flag-two-meanings surprise and
the paste-into-the-editor workaround; decision — the key, append-only placement after the
selected prompt, the default's fallback-only role unchanged, composition order, layer shape;
the ADR names the full top-level key set (`system-prompt-text`, `-file`, `-models`,
`-layers`, `use-default-prompt`), superseding ADR 0023 §1's three-key count; consequences —
Budget and KV-cache inherit through the existing prompt channel, zero engine change;
rejected — additive-by-default (two-persona stacking, floor shift per 0064 §6), merge into
`system-prompt-text`, editor-only composition. Update CONTEXT.md's System prompt entry
(1033–1052) with the layers term.
**Files:** docs/adr/0067-system-prompt-layers-are-an-explicit-additive-channel.md, CONTEXT.md
**Tests:** none (prose).
**Acceptance:** `grep -n "system-prompt-layers" docs/adr/0067-*.md CONTEXT.md`
**Commit:** `docs(adr): add 0067 — system-prompt-layers record`

**Regression guard.** Rule: any doc listing the top-level system-prompt keys without the new
key is stale. `grep -rn "system-prompt-text" docs/ CONTEXT.md ISSUES.md | grep -v layers` —
hits outside 0023/0064 historical quotes are amended by this item or named in its prose.

## 2. Parse system-prompt-layers into the config schema + settings row — ✅ DONE (2026-09-01)

NOTES (2026-09-01): the golden settings frame cmd/apogee/testdata/frames/t16-settings-rows.txt now lacks the new row, so TestE2ELiveStateFollowsTheRunningSession fails until item 4 — which owns that file and its regeneration — lands. Every other test in ./cmd/apogee and ./internal/... passes.

NOTES (2026-09-01): everyKeyFileConfig (config_test.go) gained a SystemPromptLayers entry so the "sets EVERY key of the schema" fixture stays honest; no assertion depends on it today.

NOTES (2026-09-01): layer entries use the BARE yaml keys `text:`/`file:` (the plan's spelling), unlike systemPromptEntryConfig which repeats the full `system-prompt-text`/`-file` spellings; the reason is recorded on systemPromptLayerConfig.

NOTES (2026-09-01): SystemPromptLayer is a type of its own rather than a reuse of PromptSource — a source is a rung the ladder selects between, a layer is never selected. Reason recorded on the type.

**What:** Recast at the regression check (2026-09-01). internal/config: type
`SystemPromptLayer{Text, File string}` beside PromptSource (config.go:86); `Layers
[]SystemPromptLayer` on SystemPromptSettings (config.go:103). fileConfig gains
`SystemPromptLayers []systemPromptLayerConfig` (near config.go:1223; yaml key
`system-prompt-layers`, per-entry tags `text:`/`file:`), mapped in the fileSystemPrompt
closure (config.go:756, carrier rows at config.go:445–454) and toSystemPromptSettings
(config.go:1864); the key-accessor row joins its siblings at config.go:445–453. Structural rule: a layer states exactly one of
text/file — zero or both is a validation error naming `system-prompt-layers[N]` (mirrors
TestSystemPromptSettingsValidate's checks). A `file:` entry is stored raw; resolution is
item 3's. internal/config/registry.go: a KindStructured Key row for
`system-prompt-layers` between the system-prompt-models row and the use-default-prompt
row — settingsRows iterates config.KeyRegistry (settingsrows.go:110), so the Key entry
is what makes the row exist at all; EditPointer/ExternalEdit derive from Editable=false
via editPointer/externallyEdited (settingsrows.go:288–312).
**Files:** internal/config/config.go, internal/config/config_test.go, internal/config/registry.go, cmd/apogee/settingsrows_test.go
**Tests:** extend TestApplyConfigSystemPrompt (config_test.go:3202) — layers parse, entries
carry text/file, the key-accessor row round-trips; extend TestSystemPromptSettingsValidate
(3260) — the table gains a single layer entry with neither text nor file, rejected, and a
single layer entry with both text and file, rejected (each asserts
`system-prompt-layers[0]`); TestSettingsRowsFormatEffectiveValues (settingsrows_test.go:337)
gains `"system-prompt-layers": "none"` in its want map; the external-edit row's affordance
is pinned in the existing byPath loop in settingsrows_test.go (~line 498).
**Acceptance:** `go test ./internal/config -run 'TestApplyConfigSystemPrompt|TestSystemPromptSettingsValidate' && go test ./cmd/apogee -run 'SettingsRows' && go test ./internal/config -run TestRegistryIsBijectionWithFileConfig`
**Commit:** `feat(config,tui): parse system-prompt-layers entries and register settings row`

**Regression guard.** Both producers of SystemPromptSettings (fileSystemPrompt,
toSystemPromptSettings) must carry Layers — a parse path that drops the list makes the key
silently inert. `grep -n "SystemPromptLayers" internal/config/config.go` shows the field
reaching the resolved settings struct. Guarded by the master's tree verification (2026-09-01),
folded in place of a reviewer report: internal/config/registry_test.go:20–34
(TestRegistryIsBijectionWithFileConfig) walks fileConfig's yaml tags by reflection and fails
unless the new system-prompt-layers fileConfig key and its KindStructured registry row land
together — `go test ./internal/config -run TestRegistryIsBijectionWithFileConfig` is added
to the item's Acceptance.

## 3. ResolveSystemPrompt composes the layers

**What:** In ResolveSystemPrompt (config.go:3112), after rung selection: for each layer in
order, resolve `file:` (ExpandUserPath; relative joined to `home`; unreadable → error naming
`system-prompt-layers[N]` and the path) or `text:`; validate every layer's template with
prompt.Validate (unknown placeholder → error naming `system-prompt-layers[N]` and the known
four). Join: selected template + layers, "\n\n" between parts, layers in listed order.
Binding calls (header): layers present ⇒ the embedded default never fires — template=="" with
layers yields the layers alone; useDefault is consulted only when template=="" AND no layers.
The resolved value's shape is unchanged (one rendered string): both consumers
(cmd/apogee/wire_boot.go:131, cmd/apogee/wire_settings.go:1879) need no edits.
**Depends on item 2.**
**Files:** internal/config/config.go, internal/config/config_test.go
**Tests:** extend TestResolveSystemPrompt (config_test.go:3325): layers append after the
selected prompt in order; layers alone with `use-default-prompt` unset (no default, layers
only); text + layers with `use-default-prompt: false` sends both; an empty layer list is
byte-identical to today's resolution; a layer file relative→home and `~`→user home; an
unreadable layer file names the layer index + path; an unknown placeholder in a layer names
`system-prompt-layers[N]` + the known four.
**Acceptance:** `go test ./internal/config -run TestResolveSystemPrompt`
**Commit:** `feat(config): compose system-prompt-layers in ResolveSystemPrompt`

**Regression guard.** The fallback-only rule of ADR 0064 §3 must survive: a layer must never
co-send with the embedded default, and an empty layer list must not change any existing
resolution (the rung-3/4 anchors in TestResolveSystemPrompt). `grep -n "useDefault"
internal/config/config.go` shows the flag's single consultation point.

## 4. Editor seed guard + settings-frame regeneration

**What:** Recast at the re-check round (2026-09-01, seed-from-resolution — see the header block): the seed decision lives in liveSettings.promptEditorSeed (wire_settings.go:449–456), not seedPromptEditor (settingsrows.go:216, an applier only). The guard is replaced — liveSettings.promptEditorSeed (wire_settings.go:449–456)
seeds the embedded default's bytes only when the whole resolution IS the embedded default:
Global.Text=="" AND Global.File=="" AND len(Layers)==0 AND config.ResolveSystemPrompt over
the holder's current settings for the session's bound model (upstreamBinding.Model, what
rebindSpecFor resolves with) equals config.DefaultSystemPrompt() — a per-model entry that
MATCHES the bound model means the run does not send the default, so the editor opens empty. The
len(Models)==0 clause is dropped: a per-model entry that matches nothing still resolves the
embedded default — the run sends it, so the editor shows it — and use-default-prompt:false
with nothing configured no longer seeds, because the run sends an empty prompt. Every
explicitly-configured resolution — global text, file, per-model match, layers-only — opens
the editor empty, matching what the run sends. ADR 0064 §7's embedded-default pre-fill seam
is preserved (default-only resolutions still seed). Regenerate the golden
frame cmd/apogee/testdata/frames/t16-settings-rows.txt so the new row's announced string
`system-prompt-layers` appears (per the frame test's own write mode).
**Depends on item 2.**
**Files:** cmd/apogee/wire_settings.go, cmd/apogee/wire_settings_test.go, cmd/apogee/settingsrows_test.go, cmd/apogee/e2e_livestate_test.go, cmd/apogee/testdata/frames/t16-settings-rows.txt
**Tests:** pin the new cases in TestPromptEditorSeedAnswersOnlyAnEmptyGlobalPrompt
(wire_settings_test.go:2397): a models entry with no match still seeds; a models entry that
matches the bound model opens empty; use-default-prompt:false
with nothing configured seeds nothing; layers-only opens empty; with layers present the editor
opens empty; a settingsrows or wire_settings applier-path guard — layers configured ⇒ the text
row stays empty; the frame test drives the exact announced row string from the regenerated frame.
**Acceptance:** `go test ./cmd/apogee -run 'Settings'`
**Commit:** `feat(tui): settings row for system-prompt-layers (external edit)`

**Regression guard.** The settings screen announces the key: a registered config key with no
row is invisible to the user, and a row whose ExternalEdit Flag lies sends users into a text
editor for a list key. `grep -n "system-prompt" cmd/apogee/settingsrows.go`. Guarded by the
seed-from-resolution decision (2026-09-01 — see the What and the header block).

## 5. Manual amendment (owning item)

**What:** docs/manual/configuration.md "The system prompt" (837–931): after the rungs list, a
short "Layering" passage — layers append after the selected prompt in listed order; they are
not a rung and never trigger the embedded default; an example with one `text:` and one
`file:` entry; a pointer to ADR 0067. One owning item: no other doc names the key's
user-facing behaviour (README stays the front door).
**Depends on item 1.**
**Files:** docs/manual/configuration.md
**Tests:** none (prose).
**Acceptance:** `grep -n "system-prompt-layers" docs/manual/configuration.md`
**Commit:** `docs(manual): document system-prompt-layers`

**Regression guard.** Rule: the manual's ladder text (842–860) and the new passage must not
contradict — the four rungs and `use-default-prompt`'s one meaning stand. Re-read 837–931
after the edit; `grep -n "use-default-prompt" docs/manual/configuration.md`.

---

**Suggested version bump:** minor — new config key + ADR; existing configs resolve
byte-identically (an empty layer list is inert). The user decides; never part of item work.

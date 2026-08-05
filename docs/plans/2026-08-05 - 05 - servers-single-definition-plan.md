# Servers list as the single definition — retire the top-level endpoint/api-key/host-alias/model quadruple

- **Goal:** `servers:` becomes the single definition of what upstream servers exist.
  The redundant top-level `endpoint:`/`api-key:`/`host-alias:`/`model:` keys are
  retired; a session starts on the **last-chosen** entry recorded in a new `server:`
  scalar key; first boot asks via the `/server` picker; an empty list guides into
  `/settings`; legacy configs are auto-migrated once, with a backup.
- **Date:** 2026-08-05 · **Status:** not started
- **SEQUENCING (hard gate):** do not execute until the settings-screen plan
  `docs/plans/2026-08-05 - 02 - settings-screen-plan.md` is fully DONE (items 4–9).
  Items 7–9 here consume its splice writer, `/settings` pane, and row seam; the pane
  then follows the registry changes here automatically.
- **Baseline:** HEAD `efe99ca` at write time. The settings plan's remaining items will
  land first, so file:line anchors below WILL drift — symbol names are authoritative
  over line numbers everywhere in this plan.
- **Authoritative sources:** ADR 0036 (written by item 1 — the ratified calls below are
  its content); ADR 0028 (superseded in part), ADR 0035 + settings plan (pane scope,
  splice writer), ADR 0029 (launch profiles never ride in `servers:` — stands),
  ADR 0024 (`errMissingEndpoint` stands — construction is deferred, never
  endpoint-less), ADR 0031 (driver invariants); `cmd/apogee/registry.go`,
  `cmd/apogee/config.go` (`fileConfig` :802, `serverEntry` :960, `multiSourceKeys`
  :555, `resolveSettings` :671), `cmd/apogee/upstream.go` (`upstreamChoices` :211,
  `upstreamHolder` :40), `cmd/apogee/wire.go`, `cmd/apogee/configwrite.go`.
- **Skills:** coding-standards.

## Ratified design calls

Owner, 2026-08-05 (AskUserQuestion, two rounds, this planning session):

1. **`servers:` is the single definition.** One entry = `name` (label, `/server`
   argument, and host alias — the old `host-alias:` job), `endpoint`, optional
   `api-key`, optional `model` hint. The top-level quadruple is retired from the
   schema. Loaded models keep being discovered by the heartbeat / llama-launcher
   linkage, one Monitor per bound server, unchanged.
2. **Startup = the last-chosen server, persisted in a `server:` scalar key in
   `config.yaml`** (ADR 0028's rejected-for-now spelling, now ratified). Every
   `/server` switch to a *configured* entry splice-writes `server: <name>` through the
   ADR 0035 writer. Moves to unlisted endpoints — launcher profile loads, ephemeral
   override entries — have no name and never write it. This supersedes ADR 0028 D5's
   "a switch is session-scoped, nothing is written back".
3. **First boot asks.** `server:` unset with a non-empty list → the TUI starts with no
   upstream bound and auto-opens the `/server` picker; the choice constructs the
   engine and records `server:`. Engine construction is *deferred*, never performed
   without an endpoint — ADR 0024's `errMissingEndpoint` posture is not re-opened.
4. **Override story: raw overrides + name selection.** `--endpoint`/`APOGEE_ENDPOINT`
   construct an **ephemeral unnamed startup entry** (api-key from `APOGEE_API_KEY`,
   hint from `--model`/`APOGEE_MODEL`) that wins over any name selection and is never
   persisted. `--server`/`APOGEE_SERVER` select a configured entry by name, riding the
   existing flag > env > file precedence on the `server:` key. When only
   `APOGEE_API_KEY` or `--model`/`APOGEE_MODEL` is set, it overlays that field of the
   selected entry.
5. **Empty list + no overrides → guide into `/settings`.** The TUI starts pre-bound
   and opens the `/settings` pane; pane scope stays exactly the settings plan's
   ratified v1 (owner pointed this question back at that plan): the `servers` row is
   read-only with its "edit in config.yaml" pointer. No structured add-form in this
   plan.
6. **Migration = one-time verified auto-rewrite, backup + announce.** A config with
   any legacy top-level key is rewritten once: quadruple folded into a `servers:`
   entry plus a `server:` pointer, comments and unrelated lines preserved, a
   timestamped `.bak` sibling written first, one startup line announcing the backup
   and what moved. If the verify step finds any other difference, refuse and fall
   back to a hard error carrying a paste-able replacement block.

Author-resolved, 2026-08-05 (convention/consequence of the calls above):

- **`server:` is `kindString`, not `kindEnum`** — the registry's `EnumValues` are
  static and `TestRegistryEnumValuesMatchParseSites` recovers vocabularies from parse
  sites; server names are config-dependent. Validity is checked at selection time.
- **A stale `server:`** (names no entry) is state, not intent: startup notice + the
  first-boot picker, not a hard error — the picker fixes in one keystroke what a hard
  error would send to file surgery.
- **Non-interactive drivers can't picker:** `apogee headless`, `probe`, `probe model`,
  bench paths get the friendly hard error (with example block) whenever no startup
  server is determinable. Only the TUI gets the pre-bound flows.
- **Empty-list guidance says edit `config.yaml` and restart.** The pane's servers row
  is read-only (call 5) and `opts.servers` is frozen at wire time; a live config
  rescan is deliberately out of scope (below).
- **The override env/flag names detach from the registry.** `APOGEE_ENDPOINT`,
  `APOGEE_API_KEY`, `APOGEE_MODEL`, `--endpoint`, `--model` no longer describe config
  file keys, so they leave `multiSourceKeys` and live in the startup-override
  resolver with their own binding test.
- **Migrated entry name** = old `host-alias:` value if set, else
  `hostFromEndpoint(endpoint)`. A name collision with an existing `servers:` entry →
  refuse the rewrite (fall back to the paste-able hard error).

## Out of scope

- Structured-block editors in the pane (settings plan's explicit out-of-scope; a
  server add-form is a future plan).
- Live config rescan mid-session (restart after hand-editing `servers:`).
- Launch profiles riding in `servers:` (ADR 0029 D-"never" stands).
- Heartbeat observing more than the bound server (ADR 0028 D4 stands).
- The pre-existing launch-endpoint drift in `rebindSpecFor` (`cmd/apogee/wire.go`) and
  `scheduleWiring.fire` (`cmd/apogee/schedule.go`) — both keep using launch `opts`
  after a switch. Not created by this plan; item 11 records it in ISSUES.md.
- Any version bump (see closing note).

Any authorized deviation from item text lands as a dated NOTES line under the item.

## 1. ADR 0036 + amendments + CONTEXT.md — ✅ DONE (2026-08-05)

**What:** Write
`docs/adr/0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md`
recording every ratified call above as decisions: single-definition schema; the
`server:` last-chosen key written per configured switch (supersedes ADR 0028 D5's
session-scoped-write-back half and its startup-endpoint-synthesis wording; converts
0028's rejected "`server:` startup key" into ratified); the deferred-construction
pre-bound TUI phase (extends, does not supersede, ADR 0024 — `errMissingEndpoint`
stands); the override story (ephemeral entry + `--server` name selection); the
empty-list `/settings` guidance within ADR 0035 D3's scope; the one-time verified
migration rewrite with backup (a narrow, announced exception to `seedConfig`'s
never-overwrite invariant and an extension of ADR 0035's authorized write set — the
ADR owns both statements explicitly). Append an amendment note to ADR 0028 pointing
at 0036 (house style: the 0029 amendment block in that file); append a write-set
amendment note to ADR 0035. Update CONTEXT.md: the **Upstream** entry and the
Heartbeat/`/server` section (drop "session-scoped … config.yaml is not rewritten",
describe `server:` recording and the first-boot ask), and note that a server's `name`
is the host alias (the standalone `host-alias:` key is gone).

**Tests:** none (docs only).

**Acceptance:** `ls docs/adr/ | grep 0036` shows the file;
`grep -n "0036" docs/adr/0028-*.md docs/adr/0035-*.md` hit the amendment notes;
`grep -n "server:" CONTEXT.md` reflects the new wording; `make check` passes.

**Commit:** `docs(adr): ratify the servers-list schema — single definition, last-switch startup, one-time migration`

## 2. The `server:` startup-choice key (additive) — ✅ DONE (2026-08-05)

**What:** Depends on item 1. `fileConfig` gains `Server string
\`yaml:"server"\`` (beside `Servers`, `cmd/apogee/config.go:819`). New registry row in
`cmd/apogee/registry.go`: `Path: "server"`, `Kind: kindString`, `EnvVar:
"APOGEE_SERVER"`, `FlagName: "server"`, `RestartRequired: true`, `Editable: true`,
Desc naming it the entry a session starts on, recorded automatically by `/server`.
New `multiSourceKeys` entry (`config.go:555`) overlaying into a new
`settings.startupServer` string; register the `--server` flag beside `--endpoint`
(`cmd/apogee/root.go:230-243`); copy through to `options`. No selection logic yet —
the value is resolved and carried, unused until item 3. No validation beyond the
string itself (stale names are handled at selection time, per the author-resolved
call).

**Tests:** registry guard suite stays green (the bijection guard *requires* the new
row); the source-metadata table test (`config_test.go:383-437`) gains a `server` row
asserting flag-beats-env-beats-file; `TestMultiSourceKeysBindDescribedKeys` covers
the new entry.

**Acceptance:** `go test ./cmd/apogee -count=1` passes; `make check` passes.

**Commit:** `feat(config): server: startup-choice key with APOGEE_SERVER/--server sources`

## 3. Retire the quadruple — selection comes from the list

**What:** Depends on item 2. Delete the `Endpoint`, `Model`, `HostAlias`, `APIKey`
fields from `fileConfig` (`config.go:803-814`) and their four registry rows
(`registry.go:87-108`); shrink `multiSourceKeys` to `server`/`mode`/`bypass`. New
selection step in `applyConfig` (suggest `selectStartupServer`): resolve the chosen
name (the post-precedence `server:` value) against `opts.servers` — hit → that entry
feeds `opts.endpoint`/`opts.apiKey`/`opts.model`/`opts.hostAlias` (alias = entry
`Name`; the `hostFromEndpoint` fallback at `config.go:1495-1497` is kept for item 4's
ephemeral entry). Add a **legacy sniff**: unmarshal the same bytes into a small
`legacyFileConfig` struct holding only the four retired keys (plain unmarshal ignores
unknown keys — without the sniff an old config's endpoint would vanish silently);
any legacy key present → hard error printing a ready-to-paste `servers:` +
`server:` block built from the old values (item 9 upgrades this error into the
auto-rewrite). **Transitional behavior, replaced by items 6–7:** name set but absent,
name unset (first boot), or empty list → the friendly hard error naming
`~/.apogee/config.yaml` and showing an example block. That error remains the
permanent behavior for the non-interactive drivers (headless/probe/bench, per the
author-resolved call).

**Tests:** selection table (named entry selected; entry's fields land in `opts`;
alias = `Name`); stale-name, unset-name, and empty-list error paths with the
guidance text; legacy-sniff table (each legacy key alone triggers; the paste-able
block carries the old endpoint/key/alias/model; a new-style config does not
trigger); registry bijection green with the shrunken schema.

**Acceptance:** `go test ./cmd/apogee -count=1` passes;
`grep -cE 'yaml:"(endpoint|api-key|host-alias|model)"' cmd/apogee/config.go` counts
only `serverEntry`'s nested tags; `make check` passes.

**Commit:** `feat(config): the servers list is the single definition — top-level endpoint/api-key/host-alias/model retired`

## 4. Override story — ephemeral entry and per-field overlays

**What:** Depends on item 3. A startup-override resolver in `cmd/apogee/config.go`
owning the detached constants (`APOGEE_ENDPOINT`, `APOGEE_API_KEY`, `APOGEE_MODEL`;
flags `--endpoint`, `--model`; flag beats env per pair): an endpoint override present
→ build an ephemeral `serverEntry{Name: "", Endpoint: <override>, APIKey: <key
override or empty>, Model: <hint override or empty>}` and select it as the startup
entry, ignoring any `server:`/`--server` name and never writing anything; no endpoint
override → the selected entry is used with `APOGEE_API_KEY` and `--model`/
`APOGEE_MODEL` overlaying its `api-key`/`model` fields. Ephemeral alias =
`hostFromEndpoint`. The empty-list error from item 3 does not fire when an override
rescues startup. A binding test (the `TestMultiSourceKeysBindDescribedKeys` spirit)
pins the resolver's env/flag names so nothing advertises a source nothing reads.

**Tests:** matrix — env endpoint alone; flag beats env; key overlay onto a named
entry; hint overlay; ephemeral + empty list starts; ephemeral ignores `server:`;
nothing is splice-written in any override path.

**Acceptance:** `go test ./cmd/apogee -count=1` passes; `make check` passes.

**Commit:** `feat(config): raw endpoint overrides build an ephemeral startup entry`

## 5. `upstreamChoices` — synthesize only the ephemeral startup

**What:** Depends on item 4. Rework `upstreamChoices`
(`cmd/apogee/upstream.go:211-222`): choices are the `servers:` list verbatim; a
synthesized row is prepended **only** when the startup entry is ephemeral (override
case), so ADR 0028's "the startup server is always offered" invariant holds while
the configured-startup duplicate-name edge (synthesized alias colliding with a
configured `name`) dissolves. `findServer`, `serverChoices`, and the picker's
current-marking by endpoint equality (`internal/tui/picker.go:734-745`) are
unchanged.

**Tests:** `upstreamChoices` table — configured startup by name → no synthesized row,
list order preserved; ephemeral startup → one prepended unnamed-alias row; empty list
+ ephemeral → exactly one row.

**Acceptance:** `go test ./cmd/apogee ./internal/tui -count=1` passes; `make check`
passes.

**Commit:** `refactor(upstream): server choices come from the list; only an ephemeral startup is synthesized`

## 6. Late-bound engine construction

**What:** Depends on item 5. In `cmd/apogee/wire.go`, extract engine construction
(`buildAgent` + `upstreamHolder` + Monitor wiring) into a bind step invoked with a
`serverEntry`. When item 3/4's selection determines a startup entry, bind immediately
before the TUI starts — behavior byte-identical to today. When it does not (TUI path
only: first boot, stale `server:`, empty list), start the TUI **pre-bound**: no agent
constructed, a `BindServer` seam on `tui.Options` that performs the bind on first
selection, engine-facing seams routed through a late holder that is nil-safe until
bound, and a startup-reason value (`firstBoot` | `staleChoice(name)` | `noServers`)
passed to the TUI. Non-interactive drivers keep item 3's hard errors. The engine
itself is untouched — construction still requires an endpoint (ADR 0024), it merely
happens later; ADR 0031's wire-silent/benchable invariants are unaffected (this is
driver-side wiring).

**Tests:** wire-level — determined startup binds before TUI start; each undetermined
reason starts pre-bound with the right reason; headless with no determinable server
errors; `BindServer` constructs and flips the seams exactly once.

**Acceptance:** `go test ./cmd/apogee -count=1` passes; `make check` passes.

**Commit:** `feat(wire): engine construction defers until a startup server is determined`

## 7. TUI pre-bound state — first boot asks, empty list guides

**What:** Depends on item 6 and on the settings plan's pane (items 6–8 there). In
`internal/tui`: a pre-bound state entered from the startup reason. Reasons
`firstBoot`/`staleChoice` → auto-open the existing `/server` picker over the
configured entries with a notice line (stale: names the missing entry); ⏎ calls
`BindServer` and, for a configured entry, records the choice through item 8's seam;
esc closes the picker and leaves a status-line fact ("no server bound — /server to
choose"); submitting input while pre-bound reopens the picker with the notice instead
of reaching the (absent) engine. Reason `noServers` → auto-open the `/settings` pane;
the servers row's existing "edit in config.yaml" pointer plus a status-line fact
("no servers configured — add one to ~/.apogee/config.yaml and restart") carry the
guidance; the pane behaves exactly as the settings plan shipped it. Amend `layout.md`
with the pre-bound status-line facts (this item owns that amendment). ADR 0011 value
rules apply (no no-copy types by value; `TestModelNoBuilderByValue` stays green).

**Tests:** key-flow tests per reason (auto-open, choose→bind+record, esc→fact,
submit→reopen); paint tests in the `paint_test.go` harness for the notice line and
both status-line facts; command-table and `TestModelNoBuilderByValue` stay green.

**Acceptance:** `go test ./internal/tui -count=1` passes; `make check` passes.

**Commit:** `feat(tui): first boot asks — the pre-bound state opens the server picker, or /settings when none configured`

## 8. A switch records the startup choice

**What:** Depends on item 7 (and the settings plan's splice writer). Wire a
`recordServerChoice(name)` seam (composition root, beside the `switchServer` closure
in `cmd/apogee/wire.go:388-394`) that splice-writes `server: <name>` via
`saveConfigSetting` for every successful `/server` switch **to a configured entry**
and for the first-boot picker choice; the fold/announce line notes the recording
("server: saved"). Moves that land on unlisted endpoints — launcher profile loads
(`cmd/apogee/launcher.go`), the ephemeral override row — skip the write silently. A
failed write surfaces as a status-line warning and does not block the switch (the
session move already happened; the recording is best-effort persistence).

**Tests:** fake-writer capture — configured switch writes the name; first-boot choice
writes; launcher move and ephemeral row do not; write failure warns and the switch
stands.

**Acceptance:** `go test ./cmd/apogee ./internal/tui -count=1` passes; `make check`
passes.

**Commit:** `feat(server): a /server switch records the startup choice in config.yaml`

## 9. One-time migration — verified rewrite, backup, announce

**What:** Depends on items 3 and 8 (splice machinery in `cmd/apogee/configwrite.go`).
Upgrade item 3's legacy-sniff hard error into the ratified auto-rewrite, TUI and
non-interactive drivers alike: copy `config.yaml` to a timestamped sibling
(`config.yaml.bak-YYYYMMDD-HHMMSS`); rewrite the file deleting the four legacy key
lines and inserting a `servers:` entry (name per the author-resolved rule, endpoint,
api-key/model only if the legacy keys set them) plus a `server: <name>` line,
comments and unrelated lines preserved via the existing splice + `sameApartFrom`
verify machinery (the whole-file re-parse must equal the original with exactly the
quadruple folded); print one startup line naming the backup and what moved. Any
verify failure, or a migrated-name collision with an existing entry → no write, fall
back to item 3's paste-able hard error. A config already in the new schema is never
touched (the sniff is the only trigger; `seedConfig` behavior unchanged).

**Tests:** golden-style table — legacy-only config rewritten (comments intact, entry
+ pointer present, backup file exists); legacy + existing `servers:` list appends the
entry; name collision refuses with the paste-able error; verify-failure path refuses
without writing; new-schema config untouched; the announce line renders.

**Acceptance:** `go test ./cmd/apogee -run 'Migrat|Legacy|Splice' -count=1` passes;
`go test ./cmd/apogee -count=1` passes; `make check` passes.

**Commit:** `feat(config): legacy top-level keys auto-migrate once into the servers list, with backup`

## 10. The seeded template teaches the new schema

**What:** Depends on item 3. Rewrite `cmd/apogee/defaults/config.yaml`: the four
quadruple blocks (:16-47 at baseline) are replaced by a reworked `servers:` section —
now "the single definition of the servers you run models on", first-boot-picker note,
per-field docs (name = label/argument/alias, endpoint, optional api-key, optional
model hint) — and a `server:` block with a commented example (`# server: my-box`)
placed so the settings-plan splice writer's insert-below-example rule lands recorded
choices under it. Update the precedence header comment (:7) for the surviving
multi-source keys and the override story (raw `--endpoint`/`APOGEE_ENDPOINT` build an
unlisted startup server). Seed tests / template-referencing tests updated; the
template stays the row-order and Desc source the registry condenses.

**Tests:** seeding tests stay green; a grep-style test (or assertion in the existing
template tests) that no retired top-level key example remains.

**Acceptance:** `go test ./cmd/apogee -count=1` passes;
`grep -cE '^# (endpoint|api-key|host-alias|model):' cmd/apogee/defaults/config.yaml`
returns 0; `make check` passes.

**Commit:** `docs(config): the seeded template teaches the servers-list schema`

## 11. Closing docs

**What:** Depends on items 1–10. README: config section rewritten for the
servers-list schema (example block, `server:` recording, first-boot ask, override
story — keep the `APOGEE_API_KEY=… apogee` example, now described as overlaying the
startup entry), migration note (one-time rewrite + backup). CHANGELOG: an unreleased
entry marking the schema change as breaking-with-auto-migration — no version
identifier changes. ISSUES.md: remove the config-schema entry (line 7 at baseline);
add a new entry for the pre-existing launch-endpoint drift (`rebindSpecFor` and
`scheduleWiring.fire` keep using launch `opts` after a switch — more visible now that
switches persist). `layout.md` needs nothing beyond item 7's amendment.

**Tests:** none (docs only).

**Acceptance:** `grep -n "servers:" README.md` hits the new section;
`grep -c "host-alias" README.md` returns 0;
`grep -c "retire the redundant top-level" ISSUES.md` returns 0;
`grep -n "rebindSpecFor" ISSUES.md` hits the new entry; `make check` passes.

**Commit:** `docs: servers-list schema — README, changelog, ISSUES entries`

## Suggested version bump

Not performed by this plan. When it completes, a minor bump (0.x → next 0.(x+1).0) is
warranted: a breaking config-schema change with auto-migration and a new first-boot
flow. The owner decides whether and when.

# Sub-agent routing by root key: `sub-agents-server` + `/sub-agents-server`

**Goal:** Replace ADR 0045's `sub-agents: true` entry flag with a root config key naming the
delegation target, changeable mid-session through a `/sub-agents-server` picker. Every
`servers:` entry becomes an eligible target; posture keys stay on entries and follow the target.

**Date:** 2026-08-31 · **Status:** unexecuted
**Sized for:** ~200k-context host

**Sources:** `docs/adr/0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md` ·
`docs/adr/0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md` ·
`internal/config/config.go:1046-1051` (the `server:` key precedent) ·
`internal/config/registry.go:170-191` (`KindServer` row) · `internal/tui/keymigration.go` (offer) ·
`cmd/apogee/delegation.go` (wiring, latch, notices) · `docs/design/test-drivers.md` (TUI driving)

**Ratified design calls** (owner unless dated):
- **Target model (owner, 2026-08-31):** every `servers:` entry is an eligible target; the
  `sub-agents:` flag is REMOVED; a new root key `sub-agents-server:` names the target and a
  `/sub-agents-server` picker changes it in a running session; each delegation spawned after the
  change routes to the new target.
- **Migration (owner, 2026-08-31):** a config carrying the old flag gets a startup migration
  offer (keymigration precedent).
- **Mid-run (owner, 2026-08-31):** retargeting is allowed while the agent runs; already-spawned
  sub-agents keep their server.
- **Model's say (owner, 2026-08-31):** human-only this plan; ADR 0045's model-chosen routing
  stays deferred. Keep ONE consultation point (the wiring's target resolution) so a future
  per-call parameter slots in without re-touching the recursion path.
- **Posture (owner, 2026-08-31):** `bypass:`/`mechanisms:` are valid on ANY entry and apply
  whenever that entry is the delegation target, never to the session itself.
- **Empty key (owner, 2026-08-31):** delegations fall back to the parent's upstream — the
  pre-ADR-0045 behavior; the feature is opt-in.
- **Stale name (author, per the `server:` precedent):** a key naming no entry is not refused;
  one startup notice lists the configured names and routing falls back to the parent's upstream.
- **Offer rows (author):** two rows — "move it" / "not now" (esc re-offers next start-up); no
  "never" row, since the flag is dead weight the owner removes once.

**Regression check (2026-08-31, 89a2e525):**
- 1 — recast: the registry row ships `Editable: false` (ratified); guard folded, and the item
  yields to ADR 0037 decision 4 rather than reversing it.
- 2 — guard folded: the target name is a plumbed parameter, `relist` takes it, and the stale-name
  notice fires from `land`, not the constructor.
- 3 — guard folded: `Retarget` forgets the latch and moves the name `relist` resolves against.
- 3 — rejected: "give `sub-agents-server` a settingsTable apply or exempt it beside
  `settingKeyServer`, because TestEveryEditableSettingKeyHasAnApply drives every editable key";
  the ratified row is `Editable: false` and that test skips non-editable keys
  (cmd/apogee/wire_settings_test.go:478).
- 4 — guard folded: watcher exemption for the self-made write; picker rows from the configured
  names; the Rows assertion moves to item 1.
- 5 — guard folded: the two-flag answer, `Retarget` on accept, the real detection and offer sites,
  and the two-path write verify.
- 6 — guard folded: the doc rule replaces the closed-string grep; layout.md's accept-a-command
  paragraph joins the overlay section; the Tests grep is scoped.
- 1 — re-checked: the ratified `Editable: false` now stands in **What** with the folded guards as
  ordinary item text, and the seeded template gains the commented `# sub-agents-server:` example
  beside `# server: my-box` so item 4's recorded choice lands under it (ADR 0035 placement); the
  guard paragraph keeps only the reason and the yield to ADR 0037 decision 4.
- 2 — guard folded: the retired flag's documentation leaves the seeded template with the field, so
  `internal/config/defaults/config.yaml` has one owner per edit — item 1 adds, item 2 removes.

**Standing requirements:** skills: coding-standards. Deviations from item text land as dated
NOTES lines under the item.

**Out of scope:** model-chosen routing / any `sub_agent` tool parameter change; a CLI flag or
env var for the new key; launcher actuation for the target (ADR 0045 deferred item); sub-agent
auto-naming (separate IDEA); the second heartbeat monitor's shape (stays).

---

## 1. Config: the `sub-agents-server:` root key (additive) — ✅ DONE (2026-08-31)

NOTES (2026-08-31): consequential edit — cmd/apogee/testdata/frames/t16-settings-rows.txt: made necessary by the new registry row, which the settings pane renders (golden re-recorded with `go test ./cmd/apogee -run TestE2ELiveStateFollowsTheRunningSession -update`; the only change is the added row and the value column widening for it).
NOTES (2026-08-31): the item's Files list names cmd/apogee/wire_settings_test.go, but the ratified `Editable: false` row is skipped by TestEveryEditableSettingKeyHasAnApply (cmd/apogee/wire_settings_test.go:478) — the file needed no edit and is left untouched, exactly as the item's regression check predicted.
NOTES (2026-08-31): `Options.SubAgentsServer` lands in internal/config/options.go, which the item's Files list does not name — that is where the Options struct lives, so the item's own "Add `Options.SubAgentsServer`" instruction requires it.
NOTES (2026-08-31): the read-only row falls through settingsrows.go's existing editPointer ladder to `⏎ opens $EDITOR`, which keeps TestSettingsRowsPointReadOnlyKeysAtTheirEditor (a non-editable row must carry a pointer) green without a new pointer constant. Item 4 may want to repoint it at /sub-agents-server.
NOTES (2026-08-31): the ADR-0035 placement guard was verified with a throwaway probe (deleted): spliceScalarSet lands `server:` under `# server: my-box` and `sub-agents-server:` under `# sub-agents-server: my-box`, neither at EOF.

**What:** Recast at the regression check (2026-08-31). Add `SubAgentsServer string
\`yaml:"sub-agents-server"\`` to the root Config beside `Server`
(`internal/config/config.go:1051`), with a comment stating the semantics: the name of
the `servers:` entry that takes delegations; absent/empty ⇒ delegations run on the session's own
upstream; a name no entry carries is not refused (selection answers it — the `server:` comment).
Add the lookup `SubAgentsServerTarget(entries []ServerEntry, name string) (ServerEntry, bool)`
beside the existing `SubAgentServer` (config.go:1715). Add the KeyRegistry row
(`internal/config/registry.go`): `Path: "sub-agents-server"`, `Kind: KindServer`, `Editable:
false`, `Read` returns the current name or "auto (session server)"; Desc: "Which servers: entry
takes delegations; /sub-agents-server records the choice." `/settings` DISPLAYS the target
read-only and `/sub-agents-server` (item 4) is the only way to change it, so the row needs no
`settingsTable` apply entry and no TestEveryEditableSettingKeyHasAnApply exemption. Add
`Options.SubAgentsServer` and a `keyAccessors` entry (fromFile only), the key name in the want map
(`internal/config/config_test.go:509`) and a value in `everyKeyFileConfig` (config_test.go:545), or
TestKeyAccessorsBindDescribedKeys and TestEveryConfigKeyReachesTheOptions go red. Give the seeded
template a commented `# sub-agents-server: my-box` example beside the existing `# server: my-box`
(`internal/config/defaults/config.yaml:194`), so that item 4's recorded choice lands under its
commented example instead of being appended at EOF (ADR 0035 placement,
`internal/config/configwrite_scalar_test.go:169`) — no test binds registry keys to the template, so
this is a placement guard rather than a red test. No behavior change yet — the key parses, the
routing still consults the flag.

**Regression guard.** The row is `Editable: false` (RATIFIED, owner 2026-08-31) because the
settings pane routes Enter by row Kind and never by Path: an editable KindServer row would call
settingsSwitchServer and move the SESSION's upstream, and no later item flips this. The item
therefore YIELDS to the documented decision it looked like it reversed —
internal/config/registry.go:184-190 (ADR 0037 decision 4), where `server` is Editable BECAUSE that
edit is the `/server` switch itself; a read-only row leaves that rule intact.

**Files:** internal/config/config.go, internal/config/registry.go, internal/config/defaults/config.yaml, internal/config/config_test.go, cmd/apogee/settingsrows_test.go, cmd/apogee/wire_settings_test.go

**Tests:** YAML round-trip (key set → field; absent → empty); `SubAgentsServerTarget` finds the
named entry and reports not-found for a stale name; the registry row renders in the pane's Rows as
a read-only `KindServer` row — that test lives in cmd/apogee/settingsrows_test.go, where Rows are
built, and it adds `"sub-agents-server": "auto (session server)"` to that file's
TestSettingsRowsFormatEffectiveValues want map (cmd/apogee/settingsrows_test.go:392) —
fabricatedSettings sets no target, so the Read fallback is the pinned value and the closing count
check goes red without the entry; the accessor and want-map additions keep
TestKeyAccessorsBindDescribedKeys and TestEveryConfigKeyReachesTheOptions green.

**Acceptance:** `go test ./internal/config/... ./cmd/apogee/...` · `go vet ./internal/config/...`

**Commit:** feat(config): add the sub-agents-server root key

**Depends on:** nothing.

## 2. Routing consults the key; the flag retires — ✅ DONE (2026-08-31)

NOTES (2026-08-31): the item's "retire the flag's documentation from the seeded template" had nothing to remove — `internal/config/defaults/config.yaml` never documented the `sub-agents:` entry flag (verified against 6d3d7522, item 1's only template edit), so the file is untouched and not in FILES.
NOTES (2026-08-31): the missing-name notice's "(configured: …)" tail reads `none` when the `servers:` list is empty — a case the item's sentence did not spell.
NOTES (2026-08-31): the item's citation of `routingNotice` is wrong (no such function); the notice is rendered by `missingNameNotice` and emitted through `land`/`stateChange` on the first observe, as the item's own Regression guard prescribes.
NOTES (2026-08-31): `config.posturedKeys` was deleted with the refusal that was its only caller (dead-code rule); `TestSubAgentServer` was deleted with the function it tested, superseded by item 1's `TestSubAgentsServerTarget`.
NOTES (2026-08-31): consequential edit — cmd/apogee/doc.go: made necessary by retiring the `sub-agents:` entry flag (the package map's delegation.go line named it).
NOTES (2026-08-31): consequential edit — internal/agent/delegationtarget.go: made necessary by retiring the `sub-agents:` entry flag (the latch's doc comment named it twice).
NOTES (2026-08-31): consequential edit — internal/mechanisms/retired.go: made necessary by retiring the `sub-agents:` entry flag (two comment lines called the per-server posture the `sub-agents:` posture).

**What:** `newDelegationWiring` (`cmd/apogee/delegation.go:139`) resolves its entry by
`cfg.SubAgentsServer` via `SubAgentsServerTarget` instead of `config.SubAgentServer`; empty key ⇒
the wiring holds no server and no target is ever pushed (the ADR 0045 §4 floor, now the DEFAULT).
Delete `ServerEntry.SubAgents` (config.go:1429), `config.SubAgentServer` (config.go:1715), the
two-flag refusal and the posture-on-unflagged refusal in `ValidateServers` (config.go:1633) —
`bypass:`/`mechanisms:` are now valid on every entry. The stale-name case: wiring builds with no
server and one startup notice "sub-agents: no servers entry named "X" — delegations run on the
session server (configured: a, b)" through the existing notify path. Retire the flag's
documentation from the seeded template with the field: the `sub-agents:` entry-key text in
`internal/config/defaults/config.yaml` (the `servers:` entry-key block around :149) goes when
`ServerEntry.SubAgents` does — one owner per template edit, item 1
adding the new key's commented example and item 2 removing the retired flag's, so no template edit
is orphaned. Keep the consultation in ONE place (the wiring's entry resolution) so a future
model-chosen parameter reuses it.

**Regression guard.** The delegation target NAME is a new `newDelegationWiring` parameter plumbed
from its call site (cmd/apogee/wire_live.go:285-286 — add to Files); it is NOT read off the
wiring's `base`, which is apogee.Config and carries no such field. `relist` is the second caller of
the deleted config.SubAgentServer and must take the name — `relist(name string, entries
[]config.ServerEntry)` with reloadServers passing `file.SubAgentsServer` (add
cmd/apogee/wire_settings.go to Files) — so the mid-session re-read documented at
cmd/apogee/delegation.go:20-22 keeps working. The stale-name notice is emitted on the FIRST
observe/beat through `land` (delegation.go:279-300), never inside the constructor: notify is
bridge.NotifyRouting -> programRef.send, a no-op before Run binds the program, and
newDelegationWiring runs at composition time. The item's citation of `routingNotice`
(delegation.go:307-318) is wrong — no such function exists; the notice sentences come from
stateChange (delegation.go:315) and dialectAdvice (delegation.go:350), emitted by land, and the
tests drive those. Deleting the two-flag refusal means a file with two flagged entries now loads;
item 5 answers that case. The seeded template's flag documentation retires with the field
(internal/config/defaults/config.yaml); no test binds the template to the schema, so that edit is a
documentation guard rather than a red test.

**Files:** internal/config/config.go, internal/config/defaults/config.yaml, internal/config/config_test.go, cmd/apogee/delegation.go, cmd/apogee/delegation_test.go, cmd/apogee/wire_live.go, cmd/apogee/wire_settings.go

**Tests:** rewrite the flag fixtures (config_test.go:2570, 2962) to the root key; routing tests:
key set ⇒ target latches from that entry's beat; absent ⇒ parent fallback; stale name ⇒ the EXACT
notice sentence above plus fallback; posture builds from the named entry whatever the flag era
refused. Drive the notice strings through `land` (delegation.go:279) — the first observe/beat, not
the constructor — where `stateChange` (delegation.go:315) and `dialectAdvice` (delegation.go:350)
render them; and a `servers:` re-read still re-resolves the target, `reloadServers` passing
`file.SubAgentsServer` into `relist`.

**Acceptance:** `go test ./internal/config/... ./cmd/apogee/...`

**Commit:** feat(sub-agents): route delegations by sub-agents-server; retire the entry flag

**Depends on:** item 1.

## 3. Runtime retarget: the wiring seam

**What:** Add `Retarget(name string) error` on `delegationWiring`: resolve NAME against the live
entries, build its `subAgentServer` via `newSubAgentServer` (a defective `mechanisms:` map fails
the call back to the caller — the reload rule, delegation.go:164), swap `wiring.server`; the next
beat latches the new target and the notify path reports the routing change once. In-flight
children are untouched by construction (targets snapshot at spawn — ADR 0045 decision 3's
mutex-read). Expose the seam to the TUI layer as a new member of the host seams the composition
root assembles (`cmd/apogee/wire.go` / wherever `tui.Options` is populated), nil-safe for Drivers:
a bench or headless run without the seam retargets nothing and no pane offers it.

**Regression guard.** `Retarget` mirrors relist's stale branch (delegation.go:411-423) under the
lock — bump `generation`, reset `routed/stated/dialectAdvised`, push
`engine.SetDelegationTarget(nil)` — or the engine stays latched on the old target until the new
server's first beat (10s) and `stateChange` returns "" while `routed` stays true
(delegation.go:316-322), so the once-per-change notice never fires. It must also move the name
`relist` resolves against (a live-target field on the wiring), or the next `servers:` re-read —
the watcher fires one on any save (cmd/apogee/wire_settings.go:1695, ADR 0041) — silently reverts
routing to the configured name and re-latches the old server. The seam is plumbed at
cmd/apogee/wire_live.go:285 and reaches the TUI through cmd/apogee/wire_options.go:60, where
`tui.Options` is assembled — not wire.go.

**Files:** cmd/apogee/delegation.go, cmd/apogee/delegation_test.go, cmd/apogee/wire_live.go, cmd/apogee/wire_options.go

**Tests:** retarget at idle swaps the target the next spawn sees; retarget mid-run (beat in
flight) likewise; retarget to a stale name errors and leaves the old target live; retarget to an
entry with a broken `mechanisms:` map errors with the catalogue message; the routing notice fires
once per change, not per spawn — the retarget forgets the latch (`routed/stated/dialectAdvised`
reset, nil target pushed) so the next beat re-announces; a `servers:` re-read after a retarget
keeps the retargeted server rather than reverting to the configured name.

**Acceptance:** `go test ./cmd/apogee/...`

**Commit:** feat(sub-agents): retarget delegations mid-session via the wiring seam

**Depends on:** item 2.

## 4. `/sub-agents-server`: the picker, the recording, the mid-run posture

**What:** New commandSpecs row (`internal/tui/command.go:202`): `{name: "sub-agents-server",
summary: "pick the servers: entry that takes delegations (bare = pick)", takesArgs: true,
runsBareAtAccept: true, whileRunning: true}` — mid-run-capable per the ratified call, unlike
`/server`. Commandrun case opens the shared picker with a new kind (`pickerSubAgentsServer`,
title "sub-agents server", hint like keyMigrationHint's) whose rows come from the same
configured-names seam `/server` uses (`ServerHost.List`, picker.go:435). A named argument skips
the picker (the `/server NAME` idiom, picker.go:468). Acceptance: drive the retarget seam (item
3) and record the choice through the same path `/server` uses for `server:` — the Options seam
`switchToServer` drives (`RecordServerChoice`) — mirrored as a twin for the new key; esc
dismisses, changing nothing. Add the `tui.Options` seam member (nil-safe: command reports the
standing "not wired" answer). Update the pinned parser tables: `command_test.go` wantParsed
(command.go:62), the runsBareAtAccept list (minilang_test.go:954); the verb must NOT join the
latch-blocked list (command_test.go:408) — that is the ratified mid-run posture.

**Regression guard.** Exempt `sub-agents-server` beside settingKeyServer in `changed()`
(cmd/apogee/settingsedit.go:233 — ADR 0041 decision 8's own rule: a change apogee itself made is
not a change by the time the watcher looks), or the pick the human just made comes back one poll
later as "config changed on disk" plus a failed-apply refusal; add cmd/apogee/settingsedit.go to
Files. The picker's rows come from the CONFIGURED entry names (the list `configuredServer` asks,
internal/tui/upstream.go:361-365) through a seam of its own — NOT `ServerHost.List`, which prepends
the synthesized ephemeral --endpoint entry (upstream.go:323-333) that names no `servers:` entry,
and whose `serverRows` marks "· current" on HostAlias, the session's server rather than the
delegation target. The recording seam is an explicit twin of `ServerHost.RecordChoice`
(internal/tui/tui.go:375, helper picker.go:404-413); `Options.RecordServerChoice` does not exist.
Drop "the recorded key reaches the settings Rows" from item 4's Tests — SettingsHost.Rows is a fake
under ./internal/tui and the registry row is the binary's; item 1's cmd/apogee registry test covers
it.

**Files:** internal/tui/command.go, internal/tui/commandrun.go, internal/tui/picker.go, internal/tui/tui.go, internal/tui/upstream.go, internal/tui/command_test.go, internal/tui/picker_test.go, internal/tui/minilang_test.go, cmd/apogee/settingsedit.go

**Tests:** drive the EXACT verb string "/sub-agents-server" end-to-end (test-drivers): opens the
picker over the configured names — never the synthesized `--endpoint` entry, and with no "· current"
marker borrowed from the session's server — ⏎ retargets and records, esc closes unchanged,
"/sub-agents-server NAME" skips the picker; whileRunning lets it run mid-turn; the recording does
not come back from the watcher as an external change (the `changed()` exemption).

**Acceptance:** `go test ./internal/tui/...`

**Commit:** feat(tui): /sub-agents-server picker records and applies the target

**Depends on:** item 3.

## 5. The migration offer for configs still carrying the flag

**What:** Detect retired `sub-agents: true` lines by scanning the resolved config file's raw YAML
under `servers:` entries (the struct no longer holds the key) in the composition root, and offer
the keymigration-style pane at startup (`internal/tui/keymigration.go` posture: opens unasked
under a notice, gives way to an already-open prebound surface, esc = "not now", re-offered next
start-up; two rows per the ratified call — no "never" row). Taking the row calls a new Options
seam (`MigrateSubAgentsServer(entryName) (string, error)`) that rewrites the file: drop the flag
line from that entry, add the root key naming it — one deliberate edit, validate→persist→apply
(ADR 0037), and the apply half routes through the already-landed configwatch refresh so the
watcher sees nothing (configwatch_apply_test.go:121's self-write rule). The renderer never sees
file text; it is handed entry names only.

**Regression guard.** RATIFIED (owner, 2026-08-31) — when TWO entries carry the retired
`sub-agents: true` flag (a config the deleted refusal used to reject, so it never ran), the offer
names the FIRST flagged entry in `servers:` order, and "move it" writes that one name as the root
key while dropping the flag line from BOTH entries. Fold these guards too: the detection site is
cmd/apogee/keymigrate.go:71, called from cmd/apogee/wire.go:127 — cmd/apogee/configwatch.go does
not exist, and creating a new file turns TestDocMapNamesEveryFile red unless cmd/apogee/doc.go is
listed with it. The new offer runs AFTER openKeyMigration and gives way to it exactly as that offer
gives way to a prebound surface (ADR 0047, internal/tui/keymigration.go:53-67), so the two unasked
start-up panes never race; pin the both-offers config (a literal `api-key:` plus the retired flag)
in keymigration_test.go. "move it" calls item 3's `Retarget` once the write lands (add
cmd/apogee/delegation.go to Files) — the seam as written only rewrote the file, and
`sub-agents-server` has no settingsTable row, so applySettingFor would refuse it and the live
wiring would keep its old target. The writer must use `sameApartFrom(before, after, serversKey,
"sub-agents-server")` (the two-path precedent at internal/config/configmigrate.go:227):
verifyEntryEdit zeroes only serversKey (configwrite_keysource.go:317) and would refuse the root-key
addition as "the edit would have changed more than the servers: list". Add internal/tui/picker.go
(kind const :87, pickerHintFor :145, acceptPicker :711, pickerTitle :874, pickerOfferingRows :909),
internal/tui/model.go (the openKeyMigration call site :643), cmd/apogee/wire.go and
cmd/apogee/wire_options.go to Files.

**Files:** cmd/apogee/keymigrate.go, cmd/apogee/wire.go, cmd/apogee/wire_options.go, cmd/apogee/delegation.go, internal/tui/keymigration.go, internal/tui/picker.go, internal/tui/model.go, internal/tui/tui.go, internal/tui/keymigration_test.go

**Tests:** drive the offer with a seeded file carrying the flag: the pane names the entry, "move
it" rewrites the file (flag line gone, root key added) and the session routes to it through item
3's `Retarget`, esc persists nothing; a file without the flag offers nothing; a file with TWO
flagged entries names the first and drops both flag lines; a file carrying a literal `api-key:` AND
the retired flag opens the key migration first and this offer after it, neither lost; the rewrite
survives the read-back verification the seam already performs (`sameApartFrom` over both paths).

**Acceptance:** `go test ./internal/tui/... ./cmd/apogee/...`

**Commit:** feat(tui): offer the sub-agents flag migration at startup

**Depends on:** item 2 (files overlap item 4's — serial regardless).

## 6. Docs and the ADR

**What:** Write `docs/adr/0066-sub-agent-routing-follows-the-sub-agents-server-root-key.md`:
records this grill, amends ADR 0045's decisions 1-2 (flag → root key; posture valid on any
entry), leaves decisions 3-7 standing, notes model-chosen routing remains deferred and now has a
single consultation point. Amend CONTEXT.md's Sub-agent server term, docs/manual/configuration.md
(`servers.sub-agents` section → the root key, the pane row, the migration), docs/manual/commands.md
(the new verb), and layout.md's overlay section (the "which one?" overlay gains the second
server-kind question, layout.md:1781 area).

**Regression guard.** The rule is EVERY doc mention of the retired entry flag, not one closed
string: sweep `grep -rniE 'sub-agents:|flagged' docs/manual CONTEXT.md layout.md README.md` plus the
optional-keys list at docs/manual/configuration.md:561 that the pattern misses; the live hits are
configuration.md:561,583-601 and CONTEXT.md:113,182-193 — including CONTEXT.md:184's "only one entry
may carry the flag (a second is a startup error)", which item 2 deletes. layout.md's accept-a-command
paragraph (1775-1781) is amended beside the overlay section: `/sub-agents-server` is takesArgs +
runsBareAtAccept, so it belongs there next to `/model` and `/server`. ADRs and archived plans keep
their historical wording — they are the closed trail.

**Files:** docs/adr/0066-sub-agent-routing-follows-the-sub-agents-server-root-key.md, CONTEXT.md, docs/manual/configuration.md, docs/manual/commands.md, layout.md

**Tests:** none (docs-only). Verify the ADR links resolve and every removed key spelling is gone
from the three trees the Acceptance names — `grep -rn "sub-agents: true" docs/manual docs/design CONTEXT.md`
returns nothing; docs/adr/ and docs/plans/archived/ are NOT swept (the closed trail keeps its wording).

**Acceptance:** `grep -rn "sub-agents: true" docs/manual docs/design CONTEXT.md | wc -l` is 0 · ADR filenames referenced exist

**Commit:** docs: ADR 0066 and manual for sub-agents-server routing

**Depends on:** items 4 and 5.

---

**Suggested version bump (user decides; no item performs it):** a micro bump at closeout — the
flag's removal is a user-visible config change even though the migration offer eases it.

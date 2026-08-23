# Per-profile tool rosters — the Model profile's third axis

**Goal:** land ADR 0057 — a `model-profiles:` entry carries a `tools: {disabled, enabled}` delta
axis against the default roster; tools gain a build-level default-off state with a global
`tools.enabled:` lift; precedence is profile > global > build; profile resolution becomes
axis-wise across all three axes; the resolved roster rides Rebind and announces non-empty deltas
in one line at a switch.

**Date:** 2026-08-23 · **Status:** unexecuted · **sized for:** ~200k-context host

**Authoritative sources** (an item that disagrees with these follows these):
- [ADR 0057](../adr/0057-the-tool-roster-is-a-third-model-profile-axis-resolved-axis-wise.md)
  — the ratified decision, all eight calls plus the stated bounds.
- [ADR 0044](../adr/0044-model-profiles-are-per-model-and-mostly-shipped.md) — the profile map,
  match rule and Rebind ride; its decision 4 is superseded axis-wise per ADR 0057.
- [ADR 0031](../adr/0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
  — the engine never reads config; resolution stays in the composition root.
- `docs/design/tool-surface-findings.md` — the promotion rationale and the bench-arm landing
  zone.

**Ratified design calls (owner, 2026-08-23, grill session via AskUserQuestion):**

- **Home:** third axis on the Model profile — not a separate `tool-rosters:` map, not
  per-server (Q1).
- **Shape:** delta lists `tools: {disabled: [...], enabled: [...]}` against the default roster —
  not disabled-only, not a full replacement list (Q2).
- **Default-off:** build-level default-off flag on tool registration + global `tools.enabled:`
  counterpart; ships with NO tool default-off (Q3).
- **Precedence:** specificity ladder profile > global > build default; a tool in both lists of
  one scope = startup NOTICE, disabled wins; unknown names = NOTICE, never refusal (Q4).
- **Resolution:** axis-wise for ALL three axes — absent axis defers user ▸ shipped ▸ zero,
  explicit zero overrides; supersedes ADR 0044 decision 4 (Q5).
- **Shipped table:** carries no tools axis; a future shipped roster needs its own ratification
  (Q6).
- **Rebind:** the resolved roster is a per-model binding riding every switch (Q7).
- **Notice:** one-liner at a switch when the roster deltas are non-empty; silent otherwise (Q8).
- **Stated bounds:** deltas apply to the DEFAULT tool set only (injected `Config.Tools` is the
  host's authority verbatim); built-in tools only, MCP untouched; plain config, not a Mechanism;
  a sub-agent resolves its own model's roster.

**Standing requirements:** skills: `coding-standards`. `make check` once at closeout; per-item
acceptance below. No `VERSION`/CHANGELOG-release-heading change — close with a
VERSION-SUGGESTION line instead.

**Out of scope (deliberately):** any tool actually shipping default-off (the state ships empty);
shipped-table rosters; MCP tool gating; the unified-git and PTY grill topics; the bench arms
(a)–(f) themselves — this plan builds their landing zone, not their verdicts.

---

## 1. `domain`: the roster axis and the default-off state — ✅ DONE (2026-08-23)

NOTES (2026-08-23): the item's "`domain.Tool` … gains a `DefaultOff bool`" landed as the optional
marker interface `DefaultOffTool` + the `IsDefaultOff` helper, not a struct field: `domain.Tool` is
an INTERFACE and every other per-tool attribute (ReadOnlyTool, SubprocessTool, ReadSourceTool,
PromptTool) already uses exactly this shape, including the implement-yet-report-false carve-out. It
is still a `DefaultOff bool` for item 2, which reads it through `domain.IsDefaultOff(tool)`.

NOTES (2026-08-23): the item's stated test — "Config field presence pinned by the existing
Config-shape test (`config_test.go`'s field census gains `EnabledTools`)" — had no such census to
extend; `internal/domain/config_test.go` holds only mode-ladder and effort tests, and no field
census over `domain.Config` exists anywhere in the repo. Pinned instead with
`TestConfigCarriesBothGlobalRosterLists`: a zero Config asks for neither list, and the two lists are
independent (a copy-paste aliasing one to the other fails).

NOTES (2026-08-23): the slice-typed axis makes `domain.ModelProfile` non-comparable, which broke six
existing `==`/`!=` comparisons in tests outside the item's named files (production code compares
none). Converted mechanically to `reflect.DeepEqual` in `internal/agent/rebind_test.go` (×3),
`internal/agent/setprofile_test.go`, `internal/profiles/match_test.go` (×2, one of them on
`profiles.Decision`, which embeds a profile) and `cmd/apogee/delegation_test.go`; assertions and
messages are otherwise unchanged. The non-comparability is documented on the `ModelProfile` doc
comment.

**What:** `domain.ModelProfile` gains a third axis, `Tools ToolRosterDelta`
(`Disabled, Enabled []string`; zero value = no deltas). `domain.Tool` (or the registration
surface `internal/tools` feeds, whichever the struct owns) gains a `DefaultOff bool` marking a
tool present in the build but absent from the default menu. `domain.Config` gains
`EnabledTools []string` beside `DisabledTools`, documented with the same default-set-only
contract. Doc comments state the ladder (profile > global > build) and that the zero profile
stays the byte-identical anchor.

**Files:** `internal/domain/config.go` (ModelProfile, Config), the tool type's home, and
`internal/domain/doc.go`'s map lines.

**Tests:** zero-value `ToolRosterDelta` means no deltas; Config field presence pinned by the
existing Config-shape test (`config_test.go`'s field census gains `EnabledTools`).

**Acceptance:** `go test ./internal/domain/...`

**Commit:** `feat(domain): the Model profile carries a tool-roster delta axis`

## 2. `tools`: roster computation over the registry — ✅ DONE (2026-08-23)

NOTES (2026-08-23): the item's "injected tool set untouched" test is pinned in-package as
`TestEffectiveRoster_LeavesTheGivenSetUntouched` (the ladder never mutates or reorders the set it is
handed, and returns the identical slice when it subtracts nothing) plus the contract on
`EffectiveRoster`'s doc comment. The construction-path half — `resolveTools` returning
`Config.Tools` before any roster is composed — lives in `internal/agent`, which is item 5's file
set and its `Config.Tools never rebinds` test; item 2 stayed inside its named files and Acceptance
command.

NOTES (2026-08-23): `KnownToolNames` had to stop reading the composed menu (it called
`DefaultToolsWithHost`, which now applies the ladder) or a default-off tool's name would be reported
to the user as a typo in the very `tools.enabled:` list that exists to lift it. The assembly was
split: unexported `builtinTools` is the build rung both `DefaultToolsWithHost` and `KnownToolNames`
read; no tool construction moved or changed. `withoutDisabled` is superseded by `EffectiveRoster`
and deleted (it was unexported and had no other caller); its trimming and empty-list no-op survive
in the new pass, and its three existing `HostTools{Disabled: …}` tests still pass unchanged.

**What:** one pure function computes the effective tool set: start from the default set minus
build default-off tools, then apply global deltas, then profile deltas — later scope wins per
tool; within one scope disabled wins and the conflict is reported to the caller (for the item-4
NOTICE). Applies to the DEFAULT set only — an injected `Config.Tools` bypasses it untouched.
`NewDefaultRegistry`/`DefaultTools` honour `DefaultOff` (no tool sets it yet, so today's menu is
byte-identical).

**Files:** `internal/tools/registry.go` and tests.

**Tests:** table-driven — default-off tool absent; global `enabled` lifts it; profile `disabled`
beats global `enabled` and vice versa; same-scope conflict yields disabled + a reported
conflict; injected tool set untouched; empty everything = today's exact menu.

**Acceptance:** `go test ./internal/tools/...`

**Commit:** `feat(tools): the effective roster composes build, global and profile deltas`

## 3. `config`: the spellings — `tools.enabled:` and the profile `tools:` axis — ✅ DONE (2026-08-23)

NOTES (2026-08-23): the item's "records axis PRESENCE per entry ... for the axis-wise resolver" is
recorded at the YAML seam and goes no further, per the run's DECISION that the resolver is item 4 and
has not landed: `modelProfileConfig.Tools` is a `*toolsConfig`, so `tools: {}` and an absent key are
distinguishable, and the fact is asked for through the documented `spellsToolsAxis` predicate
(pinned by `TestProfileEntryRecordsWhetherItSpellsTheToolsAxis`, which also pins that both project to
the same zero delta — which is why presence cannot live in `domain.ModelProfile`). Nothing reads the
predicate in production yet; item 4's resolver is its caller.

NOTES (2026-08-23): the new `tools.enabled` registry row is NOT `Editable`, unlike `tools.disabled`
beside it. An editable row needs an apply case in `cmd/apogee/wire_settings.go` plus a
`liveTools.setEnabled` swap door (`TestEveryEditableSettingKeyHasAnApply` enforces it), which is
`cmd/apogee` work this item's Files exclude, and the enable direction reaches only a tool registered
default-off — a state no tool ships in yet. The row reads its value and sends ⏎ to the file, the
`mcp-servers`/`model-profiles` posture. Pinning the new row's rendered value in
`cmd/apogee/settingsrows_test.go` was unavoidable: that table asserts one entry per registry key.

NOTES (2026-08-23): `unknownToolNotice` gained a `key` parameter (`unknownToolNotice(key, names)`)
so the same complaint can name any of the four lists; exported `UnknownToolNames` keeps its
signature, so `cmd/apogee/wire_settings.go`'s caller is untouched. The conflict notice asks
`tools.RosterConflicts` (item 2) rather than comparing lists here, so the line can never describe a
different set than the ladder acts on.

NOTES (2026-08-23): README's tool-roster paragraph was updated beside the seeded template though the
item's Files name only the template — the two spellings are user-facing, no later item of this plan
touches README, and the paragraph's "all on by default … `disabled:` is how you take one off" would
otherwise be the only account of a block that now has two keys.

**What:** parse the global `tools.enabled:` list and
`model-profiles.<pattern>.tools.{disabled,enabled}`. Unknown tool names in ANY of these lists
produce the existing one-line NOTICE (extend `UnknownToolNames`/`unknownToolNotice` to name the
offending key); a tool in both lists of one scope produces a NOTICE naming the tool and the
winning side (disabled). The YAML layer records axis PRESENCE per entry (tools axis key present
vs absent) for item 5's axis-wise resolver — presence is a file-config fact, never a domain one.
The seeded config template documents both new spellings as commented examples beside
`tools.disabled:`.

**Files:** `internal/config/config.go`, `internal/config/registry.go`,
`cmd/apogee/defaults/config.yaml`, tests.

**Tests:** round-trip both spellings; unknown-name NOTICE fires for each list; same-scope
conflict NOTICE; registry docmap test covers the new key rows.

**Acceptance:** `go test ./internal/config/...`

**Commit:** `feat(config): tools.enabled and the per-profile tools axis are spelled`

## 4. `config`: profile resolution goes axis-wise — ✅ DONE (2026-08-23)

NOTES (2026-08-23): the item's Files point at "wherever the resolver lives today (composition-root
side, `internal/config` / `cmd/apogee`)" — that is `internal/profiles` (`Resolve`, `Entry`,
`Decision`), which only `cmd/apogee` imports, so the resolution itself changed there and
`internal/config`/`cmd/apogee` changed as its seam and its caller. `profiles.Entry` gained
`SpellsTools` because axis presence cannot live in `domain.ModelProfile` (item 3's finding): it is
the file fact `toProfileEntries` now carries across, and the shipped table leaves it false (ADR 0057
decision 6). The other two axes need no flag — `""` vs `native` and `""` vs `none` are already
distinct, so `spellsToolCall`/`spellsThinking` read the domain value.

NOTES (2026-08-23): per the run's DECISION this item also owns the two facts item 3 left behind: the
degenerate `tools:` NULL spelling is pinned as ABSENT
(`TestProfileEntriesCarryTheToolsAxisPresence`, alongside `tools: {}`, a listed axis and no key at
all), and the seeded template's "replaces the WHOLE profile, every axis" sentence — false the moment
this item landed — is rewritten to the axis-by-axis rule with
`TestEmbeddedDefaultConfigDocumentsModelProfiles` re-pinned on "AXIS BY AXIS".

NOTES (2026-08-23): two stale doc comments in files this item already edits were corrected with it:
`TestApplyConfigModelProfiles`'s "keeps the whole block it was given — both axes" (the projection
does keep every axis; which one WINS is now the resolver's business) and
`cmd/apogee/modelprofile.go`'s two-axis header plus `modelProfileNotice`'s "a user match is silent"
rule, which is now "silent when the user's tier answered everything the table had to say".

**What:** replace the whole-entry pick with axis-wise resolution per ADR 0057 §5: for each of
tool-call format (+pattern), thinking, and tools, take the nearest layer (user ▸ shipped ▸ zero)
whose entry SPELLS that axis; an explicitly spelled zero (`tool-call-format: native`,
`thinking: {style: none}`, `tools: {}` with a key present) overrides deeper layers. Pattern
matching (case-insensitive substring, longest-wins, user-beats-shipped) is unchanged. The
resolver still hands the engine one resolved `domain.ModelProfile` — the engine sees no
layering. The shipped shape table gains no tools axis.

**Files:** wherever the resolver lives today (composition-root side, `internal/config` /
`cmd/apogee`), tests.

**Tests:** the ratifying trap — a tools-only user entry over a shipped gpt-oss match keeps
harmony thinking; explicit `style: none` user axis still silences a shipped one; absent axes
fall through to zero exactly as before; existing resolution tests updated where rule-4
whole-replacement was pinned.

**Acceptance:** `go test ./internal/config/... ./cmd/...`

**Commit:** `feat(config): model-profile resolution is axis-wise across the three layers`

## 5. `agent`: the roster rides Rebind — ✅ DONE (2026-08-23)

NOTES (2026-08-23): the item's "`RebindSpec` gains the resolved roster (spelled as the per-model deltas or the composed effective set — follow the profile field's idiom)" landed as NO new field: the profile field's idiom, taken literally, already carries it — ADR 0057 makes the roster the third AXIS of `domain.ModelProfile`, which item 1 landed, so `RebindSpec.Profile.Tools` IS the resolved per-model delta pair. A second field would duplicate that axis and let the two disagree about one model. A `*domain.ToolRegistry` on the spec was rejected outright: it would be the second registry-mutation path `SwapTools` exists to prevent (ADR 0037 binding F), and it would break the item's own "construction composes the same way (item 2's function), so startup and switch agree" — the caller would compose at a switch and the engine at startup. `RebindSpec`'s exclusion doc line, its `Profile` field, the `Rebind`/`SetProfile`/`SwapTools` "what stands" lines and the `setprofile.go` package doc all say the roster is per-model now.

NOTES (2026-08-23): the swap is `applyRoster`, called from INSIDE `applyProfile` rather than from `Rebind` alone. The item asks for "the same one-internal-swap shape as `applyProfile`", and the roster is an axis of the profile: leaving `SetProfile` out would let a `model-profiles:` edit under a stable model move `cfg.Profile.Tools` while the live tool set stayed behind, and the NEXT rebind would then apply an edit the user made long before. `applyProfile` is documented as the ONE place profile replacement exists (ADR 0044 decision 6); the roster now lives there with the other two axes. It commits in place (assembling the registry has no error path) after the parse seam has committed.

NOTES (2026-08-23): the item's Files are `rebind.go`/`construct.go`/`agent.go`, and `setprofile.go` + `swaptools.go` were edited with them because the one-swap shape lives in the first and the swap's BOUNDARY in the second. `Agent` gains `ownsToolSet` (seeded by the new `composesDefaultRoster`, cleared by `SwapTools`): without it a rebind would re-run the default assembly over a registry the host swapped in — every MCP tool folded into a live TUI session among it — because `resolveTools` would hand back the immutable construction seed rather than the live set. That guard is what makes "the roster rides Rebind" safe to land before the composition root wires anything.

NOTES (2026-08-23): the item's "a sub-agent with a different model gets its own roster … pin it with a test, not new code" is pinned as what is actually true by construction, which is narrower than the plan's phrasing: `newChildAgent` sets `childCfg.Tools = a.defaultSubAgentTools()`, an INJECTED set, so the child's roster axis is not the engine's to apply there. `TestSubAgentRosterIsItsOwnModels` pins both halves — a routed child's Config carries the TARGET model's roster axis and never the orchestrator's, and ADR 0005's narrowing is the ceiling that keeps a profile `enabled:` entry from handing a child a tool its parent lacked. An unrouted child runs the parent's model, so "its own model's roster" is the parent's, already in the set it inherits.

NOTES (2026-08-23): `internal/agent/doc.go`'s half-line "setprofile.go is the separate, explicit door for changing the model PROFILE, which Rebind deliberately leaves alone (ADR 0037)" is stale — ADR 0044 put the profile ON Rebind long before this item — and was left untouched rather than fixed drive-by; it is on the run's NOTES, not in the issue register.

**What:** `RebindSpec` gains the resolved roster (spelled as the per-model deltas or the
composed effective set — follow the profile field's idiom); a model switch applies it atomically
at the ADR 0024 boundary through the same one-internal-swap shape as `applyProfile`. Rewrite
`RebindSpec`'s exclusion doc line: the ROSTER is per-model; mode, approvals, confinement and the
conversation remain session state. Construction composes the same way (item 2's function), so
startup and switch agree. Sub-agents resolve against their own model by construction — pin it
with a test, not new code.

**Files:** `internal/agent/rebind.go`, `internal/agent/construct.go`, `internal/agent/agent.go`
doc comments, tests.

**Tests:** a rebind to a profiled model swaps the tool set at the boundary; switching back
restores; injected `Config.Tools` never rebinds; a sub-agent with a different model gets its own
roster.

**Acceptance:** `go test ./internal/agent/...`

**Commit:** `feat(agent): the resolved tool roster is a per-model rebind binding`

## 6. `tui`: the switch notice — ✅ DONE (2026-08-23)

NOTES (2026-08-23): the item's Files ("`internal/tui` / composition root") and its Acceptance (`go test ./internal/tui/...`) point at different packages; the notice PATH the `model profile: … (built-in)` line uses is the composition root's — `cmd/apogee/modelprofile.go` composes the string, `rebindSpecFor` appends it to `RebindResult.Notices`, and `internal/tui` only surfaces notice strings as transcript notes (`heartbeat.go`'s `applyRebind`, pinned generically by the existing `TestRebindNoticesSurfaceAsNotes`). So the line and its tests landed in `cmd/apogee` and the acceptance run was `go test ./internal/tui/... ./cmd/apogee/...` — both pass. No `internal/tui` change was needed; a tui-side test would have exercised a fake, not this line.

NOTES (2026-08-23): the notice fires at the SWITCH seam only (`rebindSpecFor`), per the item's and ADR 0057 decision 8's wording. Startup's stderr profile line and the `model-profiles:`-edit door (`reloadModelProfiles`, which already drops the shape notice deliberately) were left silent — neither is a model switch, and a pre-bound start gets the line from the first beat's rebind anyway.

NOTES (2026-08-23): two renderings the item did not spell, both fixed to the ladder's own verdict so the line describes the roster the session gets rather than the lists it was spelled from: a name written in BOTH directions of the axis renders once as a removal (disabled wins, `tools.EffectiveRoster`; the clash itself is already reported at load time by `config.rosterConflictNotice`), and names are trimmed and deduplicated exactly as `internal/tools` trims them before comparing.

NOTES (2026-08-23): `rebindSpecFor`'s doc comment said "What it deliberately does NOT touch: the endpoint, the mode, the tools, and the conversation" — stale since item 5 put the roster on Rebind, and directly contradicted by the notice this item adds three lines above it. Refreshed in place ("the approvals" replaces "the tools", and the profile bullet now names the third axis) rather than left for a follow-up, because the contradiction would have shipped inside the function this item edits. `cmd/apogee/doc.go`'s half-line map entry for `modelprofile.go` was extended for the same reason: the file now produces two notices, not one.

**What:** a switch whose roster deltas are non-empty renders one line —
`tools: +a +b −c (profile)` — through the same notice surface as the shipped-profile line;
silent when the entry has no tools axis or the deltas resolve empty. Names sorted for
determinism, additions before removals.

**Files:** the notice path the `model profile: … (built-in)` line uses (`internal/tui` /
composition root), tests.

**Tests:** golden line for mixed deltas; silence for no-axis and empty-delta switches.

**Acceptance:** `go test ./internal/tui/...`

**Commit:** `feat(tui): a model switch announces non-empty roster deltas`

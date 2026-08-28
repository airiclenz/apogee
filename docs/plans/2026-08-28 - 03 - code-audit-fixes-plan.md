# Code-audit 2026-08-28 fixes — implementation plan

**Goal:** close the 20 findings of `docs/reviews/code-audit-2026-08-28.md` (6 High, 14 Medium
over `cmd/apogee`, `internal/config`, `internal/judge`, `internal/skills`, `internal/tui`,
`internal/tuitest`, `internal/filewatch`, `Makefile`/CI, `scripts/`). Every finding was
re-verified against `main` at `8b5aa8dd` on 2026-08-28 before this plan was written: 17 hold as
reported, 2 hold with a corrected consequence (F1 judge key, F8 watcher baseline — see the
ratified calls), 1 cites a wrong path (F12 — `cmd/apogee/wire_settings.go`, not
`internal/tui/…`). None of the 20 is recorded in `ISSUES.md`, and none overlaps a finding of
plan `2026-08-28 - 02` (the deferred-residuals sweep, in flight); the two plans do share files
(`cmd/apogee/wire_test.go`, `internal/skills/parse.go`), which is why item 0 gates this plan on
that one being archived. 16 items, each sized to one sub-agent; CHANGELOG entries land at the
closeout from the sidecars, per the skill.

**Date:** 2026-08-28
**Status:** unexecuted
**sized for:** ~200k-context host

**Authoritative sources:**
- `docs/reviews/code-audit-2026-08-28.md` — the findings (each item names its finding by the
  audit's own heading). Where re-verification corrected a finding, the item says so and the
  corrected fact is binding.
- `AGENTS.md` (Bypass floor, ADR 0031 north star, regressions-never-deferred, ISSUES/CHANGELOG
  convention); `docs/design/test-drivers.md` (tuitest drivers, stubllm, e2e support helpers);
  ADR 0036 (`APOGEE_API_KEY` startup overlay), ADR 0037 (settings apply: validate-then-commit,
  binding A), ADR 0041 (the config file is watched), ADR 0047 (per-entry key sources), ADR 0062
  (test drivers are Drivers).
- Line numbers below were verified on 2026-08-28 against `main` at `8b5aa8dd`; the symbol name
  is the anchor, the number a hint.

**Ratified design calls (owner, 2026-08-28, via the plan-writer's questions):**
- **Scope:** all 20 findings, with two recasts — F1 (judge key) is a trim plus a header test,
  `unset = keyless` stays; F8 (watcher zero baseline) is a test-only pin, no watcher change.
- **Padded `name:` / `endpoint:` (F6/F7):** trimmed on decode; the canonical form is what is
  stored, deduped, selected and dialled. Never a refusal.
- **Pre-bound `/clear` and `/new` (F11):** view-only reset — no engine call, no idle-save; the
  pre-bound reason survives.
- **MCP after a url-safety edit (F12):** the live connection reconnects under the new guard; a
  server whose endpoint the new list denies is dropped and the apply note names it.
- **actionlint (F18):** always `go run …@$(ACTIONLINT_VERSION)`; the PATH short-circuit goes;
  CI calls the Makefile target so the two literally run one command.
- **Newcomer container (F19):** a private bridge network with the stub bound on the docker
  bridge gateway address; `--pids-limit`, `--memory`, `--security-opt no-new-privileges`. Root
  stays (the reader's `apt` needs it); `--network host` goes.
- **Skills description cap (F4):** `maxDescriptionLen = 4096` runes per description, clamped at
  the two `Description:` assignments; no corpus-wide cap.
- **Judge key (F1):** `APOGEE_API_KEY` is the ADR 0036 overlay, not an ADR 0047 source —
  `unset` stays keyless at every endpoint; the value is trimmed like its sibling env reads.
- Plan-author calls (mechanical, following existing precedent) are stated inline in each item's
  **What** as binding text.

**Regression check (2026-08-28, against `main` at `836af8c7`; owner asked for the plan to be
amended accordingly):** every item was checked for behaviour that works today and would stop
working, tests that would go red, and announced values that would change. Five items regressed
as first written and carry a `**Regression guard.**` paragraph that is binding text; the calls it
makes are these:
- **Item 15 (release smoke):** archive bytes are never reproducible across two `make dist` runs
  (fresh mtimes inside tar/zip), so the SHA256SUMS diff would fail every correct release. The
  cross-check is recast onto the embedded build stamp (`go version -m`: `vcs.revision` must be
  the tagged commit); `vcs.modified=true` warns, never fails.
- **Item 2 (padded name/endpoint):** the config-edit transaction is a second reader of the file
  and matches entries by the raw on-disk name; it canonicalises the same way.
- **Item 3 (`context-window`):** the type is exported (`TokenCount`) because `cmd/apogee` assigns
  into the field; whole floats (`65536.0`, `1e3`) that load today become a load error — a stated,
  accepted behaviour change; the pinned entry-level refusal row is rewritten.
- **Item 7 (pre-bound `/clear`):** the view-only reset is refused, with today's note, when the
  session was started with `--resume`/`--continue` — the later bind would replay a conversation
  the view no longer shows.
- **Item 8 (url-safety → MCP):** the reconnect fires only when the admitted server set changes;
  a reconnect failure lands in the note, never as the row's error; a dormant holder (`a.mcp ==
  nil`, embedder/test Drivers) skips the MCP half.
Six more items gained a correction that is not a design call (a missing interface implementer, a
pinned test wording, a helper's real name, an un-passable assertion, a flaky equality, a skip
path) — each is stated in its item.

**Standing requirements:**
- `skills: coding-standards`
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- Per-item Acceptance is targeted; `make check` runs once, at the closeout.
- No item changes `VERSION`, a CHANGELOG release heading, or a tag.
- Each item's sidecar CHANGELOG entry names the audit finding it closes (the audit's heading)
  so the closed trail is greppable.
- Nothing in this plan touches `ISSUES.md` — no finding was ever registered there.

**Out of scope:**
- A `working-window` load-time scalar check (`internal/config/config.go:588-595`) — same shape
  as item 3, not an audit finding; a follow-on if the owner wants the pair.
- A shared cross-package `captureStderr` test helper (item 9 fixes the three copies in place).
- A corpus-wide index cap for skills (ratified: per-description only).
- Non-root or `--cap-drop` for the newcomer container (ratified: root stays for `apt`).
- Verifying a published release byte-for-byte against a rebuilt tagged tree — the archives
  are not reproducible (item 15 checks each binary's embedded `vcs.revision` only); making
  `make dist` reproducible is a separate release-hygiene question.
- The audit's "What Looked Good" refutations (mode fail-open, footer race, renderer shrink,
  double identity notice) — nothing to do.

---

## 0. Plan `2026-08-28 - 02` is archived before this plan starts — ✅ DONE (2026-08-28)

**What.** This plan and the deferred-residuals sweep both edit `cmd/apogee/wire_test.go`
(sweep item 22 / this plan item 9) and `internal/skills/parse.go` (sweep item 27 / this plan
item 5). Confirm the sweep's closeout has archived it — `docs/plans/archived/` holds
`2026-08-28 - 02 - deferred-residuals-sweep-plan.md` and `docs/plans/` no longer does — and
that the tree is clean. No code change. If the sweep is not archived, report BLOCKED with the
sweep's remaining items; do not proceed.

**Files:** none

**Tests.** None.

**Acceptance.** `ls "docs/plans/archived/" | grep -q '2026-08-28 - 02 - deferred-residuals-sweep-plan.md' && ! ls docs/plans/ | grep -q '2026-08-28 - 02' && [ -z "$(git status --porcelain | grep -v 'docs/plans/2026-08-28 - 03')" ]`

**Commit.** none — a gate item; the verifier marks it done without a commit when the check passes

## 1. The judge reads the verdict past a stray brace and sends a trimmed key — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the "prose quote before the object" row places the quote after a stray `{`, because a quote before the first brace is never scanned — the swallow the row pins can only happen inside a candidate span.

NOTES (2026-08-28): `useStub` gained a variadic `...stubllm.Option` tail so the key test can gate the stub; existing call sites are unchanged.

**What.** Audit: *the live judge gate rejects valid model verdicts on a stray brace in prose*
(High) and *the judge keys its requests through a raw env read* (Medium, recast). (1)
`firstJSONObject` (`internal/judge/judge.go:339`) anchors on `strings.IndexByte(text, '{')`
once. Change `parseVerdict` (`:317`) to walk every `{` in order: balance the span from that
brace (the existing walk, extracted to `balancedObjectAt(text, start) (end int, ok bool)`),
try `json.Unmarshal` into the verdict struct, and accept the FIRST span that decodes to a
verdict whose `verdict` value is one of the accepted words; a span that fails to balance or to
decode moves the anchor to the next `{`. No candidate → an error (see the guard). Binding: the
walk is bounded by the reply length (each candidate starts after the previous anchor), and the
string-skipping state resets per candidate.

**Regression guard.** `TestAskRefusesAnUnreadableReply` (`judge_test.go:117-119`) pins THREE
distinct wordings that must all survive: a reply with no `{` at all still says `no JSON object`
(`judge.go:316`); a span that decoded but carried a third verdict word, with nothing better
after it, still says `verdict "maybe"` (`:331` — remember the last such rejection across the
walk and return it when no candidate wins); every other failure keeps the `decode` wording. "No
candidate → the decode error" alone would collapse the first two into the third and turn that
test red.

(2) Both client constructions (`judge.go:93`, `:177`) read
`os.Getenv(apiKeyEnv)` raw while `endpoint()` (`:198-203`) and `resolveModel` (`:220-226`)
trim. Add `apiKey() string` beside `endpoint()` returning `strings.TrimSpace(os.Getenv(apiKeyEnv))`
and use it at both sites. The `apiKeyEnv` const doc (`:24`) states that this is the ADR 0036
overlay — unset means keyless, as for every endpoint apogee dials — and that it is not an ADR
0047 per-entry source. Binding: no `KeyResolver` seam, no hard error on unset (ratified).

**Files:** `internal/judge/judge.go`, `internal/judge/judge_test.go`

**Tests.** (1) Rows in `TestAskReadsTheVerdict` (`judge_test.go:58`): prose carrying a stray
`{wrote 3 files}` before the object; an unbalanced `{` in prose before the object; a prose
`"` before the object (the in-string state must not swallow it). A row in
`TestAskRefusesAnUnreadableReply` (`:109`): braces with no verdict object anywhere still
fails with the decode error. (2) A new test using `useStub` (`:22`) with
`stubllm.WithAPIKey("k")` (`internal/stubllm/server.go:44`, gate at `:165`) and
`t.Setenv(apiKeyEnv, " k ")`: `Ask` succeeds — the trimmed Bearer header reached the stub; with
the variable unset the stub's 401 surfaces as an `Ask` error, never a verdict.

**Acceptance.** `go test ./internal/judge/`

**Commit.** `fix(judge): the verdict is read past a stray brace and the key is sent trimmed`

## 2. A padded server name or endpoint is canonicalised on decode — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the duplicate-name row was added beside the existing "two entries sharing one name" row in `TestApplyConfigServersInvalid` rather than replacing it — the plain-scalar case still has to hold.

**What.** Audit: *config validation passes whitespace-padded server names* (High) and *a
whitespace-padded endpoint passes validation and reaches the wire* (High). `ServerEntry`
(`internal/config/config.go:1333`) is decoded by the plain `yaml.Unmarshal` at `:2281` inside
`parseConfigFile` (`:2260`) and nothing trims `Name` or `Endpoint`; `ValidateServers`
(`:1422`) dedups on the raw name (`:1430-1434`), `selectStartupServer` trims the CHOICE
(`:2826`) but compares against the raw entry (`:2843`), `ApplyConfig` seeds `HostAlias` from
the raw name (`:2600`), and `provider.NewClient` trims only `/` (`internal/provider/client.go:244`).
Changed representation — the stored form becomes the trimmed form. In `parseConfigFile`,
directly after the unmarshal and before `validateModelProfiles` (`:2284`), trim every entry's
`Name` and `Endpoint` with `strings.TrimSpace` (one loop, binding — the canonical form is
stored once, so no comparer changes). Consumers that now see the canonical form (enumerated;
none needs a code change): `ValidateServers` dedup `:1430`; `selectStartupServer` `:2843`;
`aliasFromEndpoint` `:2924`; `HostAlias` `:2600`; `cmd/apogee/upstream.go:354,368,490`;
`cmd/apogee/wire_settings.go:395,474`; `cmd/apogee/launcher.go:901`; `cmd/apogee/daemon.go:553`;
`cmd/apogee/daemonfire.go:269`; `cmd/apogee/keymigrate.go:120`;
`internal/config/configmigrate.go:165`; `internal/config/configwrite_keysource.go:178`;
`internal/tui/picker.go:334,348,392,483,951`; `internal/tui/settings.go:739,1124`. Producers:
`parseConfigFile` (every Driver loads through it — `LoadFileConfig`, `ResolveOptions`,
the settings write path's re-validation at `cmd/apogee/wire_settings.go:1514`) AND the
config-edit transaction (guard below). Also, as an embedder guard, `provider.NewClient`
applies `strings.TrimSpace` before its `TrimRight("/")`. Update the `ValidateServers` doc to
say the entries it sees are already canonical.

**Regression guard.** The edit transaction is a second reader of the file that does NOT go
through `parseConfigFile`: `verifiedEdit` (`internal/config/configedit.go:100-103,119-120`)
parses `before` and `after` with a plain `yaml.Unmarshal`, and `serverEntryAt`
(`internal/config/configwrite_keysource.go:178`) and `serverEntryNode` (`:288`) match the
entry by the RAW struct name / node value — while every caller passes the in-memory, now
canonical, name (`cmd/apogee/wire_verbs.go:202,234` for `/model` remember and the launch-profile
record; `cmd/apogee/keymigrate.go:185,224` for key migration). Without a matching trim, a file
whose entry is `name: " box "` — which works today at every layer — would be refused with `it
has no servers: entry named "box"`. Binding: extract the trim loop into one unexported helper
`canonicaliseServers(*fileConfig)` called by `parseConfigFile` AND by `verifiedEdit` on both
`before` and `after` right after each unmarshal (so `verifyEntryEdit`'s DeepEqual of
`after.Servers[at]` against `want` compares canonical to canonical), and make
`serverEntryNode` compare `strings.TrimSpace(value.Value) == name` so the padded on-disk node
is still found. The splice never rewrites the node's own `name:` scalar — the file keeps what
the user wrote; only the match is canonical. The parse-only checks at
`configwrite_keysource.go:107` and `configmigrate.go:132` and the legacy fold
(`configmigrate.go:126-131,217-231`, raw space end to end, result reloaded through
`parseConfigFile`) need no change.

**Files:** `internal/config/config.go`, `internal/config/config_test.go`,
`internal/config/configedit.go`, `internal/config/configwrite_keysource.go`,
`internal/config/configwrite_keysource_test.go`, `internal/provider/client.go`,
`internal/provider/client_test.go`

**Tests.** `internal/config`: a file with `name: " box "` and `endpoint: "  http://x:1 "`
loads with `Name == "box"`, `Endpoint == "http://x:1"`; `selectStartupServer` with the choice
`box` selects it (beside `TestApplyConfigStartupServerOverrideSelects`, `config_test.go:947`);
`HostAlias` is `box`; two entries `" box "` and `"box"` are refused as duplicates (row in
`TestApplyConfigServersInvalid`, `:2544`). `internal/provider`: `NewClient("  http://x/  ")`
dials `http://x` — assert the request URL the fake transport sees. Guard tests in
`configwrite_keysource_test.go`: an entry edit (the cheapest `entryEdit` caller the file
already drives) on a file whose entry is `name: " box "`, called with the name `box`, succeeds
and leaves the node's `name:` scalar as written; the same edit against a file with no such
entry still yields the `it has no servers: entry named` refusal. Bite check: the first of
these fails against the tree with only the `parseConfigFile` trim applied.

**Acceptance.** `go build ./... && go test ./internal/config/ ./internal/provider/`

**Commit.** `fix(config): a padded server name or endpoint is stored trimmed, so it selects and dials`

## 3. `context-window` refuses a fractional or negative value at load — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the item's regression guard (a) named `cmd/apogee/wire_options.go:77` and `cmd/apogee/launcher.go:295,599` as `ServerEntry.ContextWindow` assignment sites; they are not — they assign into `tui.Options`, the local `launchProfile` row and `tui.LaunchProfileChoice` respectively — so neither file changed. `delegation.go:425` needed `int(entry.ContextWindow)` rather than a `config.TokenCount(...)` conversion. Three compile sites the item did not list did need touching: `cmd/apogee/upstream.go` and `cmd/apogee/wire_firing.go` (`int(...)` at `config.ResolveContextWindow`) and `cmd/apogee/wire_firing_test.go` (`int(...)` in a comparison).

NOTES (2026-08-28): two `internal/config/configedit_test.go` fixtures used `context-window: lots` as their generic "a file that is settings-shaped but holds a value the schema's type cannot take" case and asserted on `cannot unmarshal`; the key was switched to `working-window:` (still a plain `int`) so those two tests keep pinning the DECODER's own error rather than `TokenCount`'s sentence.

NOTES (2026-08-28): the new table's entry-scope YAML carries a `server: box` line — `ApplyConfig` refuses a `servers:` list that records no startup server, so the entry-level rows cannot be written without it.

NOTES (2026-08-28): `docs/manual/configuration.md` gained a sentence on the refusal beside the `context-window:` pin, because the accepted `65536.0`/`1e3` change is user-facing.

**What.** Audit: *`context-window` is validated only on the settings write path* (Medium).
`validateContextWindow` (`internal/config/registry.go:659`) guards only the `/settings` row;
the file decodes `ContextWindow int` (`internal/config/config.go:1040`, entry-level `:1347`)
through `yaml.v3`, which silently truncates `3.5` to `3` (decode.go `case float64`, no
round-trip guard) and `applyFile` (`:577-587`) silently drops `<= 0`. Add an EXPORTED
`TokenCount int` type in `internal/config/config.go` implementing `yaml.Unmarshaler`: it accepts
only a node whose tag is `!!int` with a value `>= 0`, and refuses everything
else with the same wording `validateContextWindow` uses (`want a token count of 0 or more`),
naming the key. Use it for BOTH `fileConfig.ContextWindow` (`:1040`) and
`ServerEntry.ContextWindow` (`:1347`); the entry-level `< 0` check at `:1496` becomes
redundant and is removed.

**Regression guard.** (a) The type must be exported: `cmd/apogee` assigns `int` VARIABLES into
`ServerEntry.ContextWindow` — `wire_server.go:44` (`opts.StartupContextWindow`),
`wire_settings.go:703` (`s.entryWindow`), `launcher.go:295,599`, `delegation.go:441`,
`wire_options.go:77`, and `wire_test.go:5090` — and an unexported type cannot be named for the
conversion outside the package; those sites take `config.TokenCount(...)`. (b) Decode the
value with `node.Decode(&n)` (after the tag check), never `strconv.Atoi(node.Value)`: yaml.v3
tags `0x10000`, `1_000`, `0o17`, `+5` as `!!int` and they load today. (c) yaml.v3 never invokes
the Unmarshaler for an absent key or an explicit `null` (`prepare` short-circuits), so "`!!null`
→ 0" is moot — the field stays 0 as today; do not write that branch. (d) **Stated behaviour
change, accepted at the regression check:** a whole-number float (`65536.0`, `1e3`) that loads
today as 65536/1000 becomes a load error — the audit's own intent (the decoder cannot tell
`3.5` from `1e3` without refusing the `!!float` tag), named in the sidecar CHANGELOG entry.
A quoted `"65536"` is already refused today (`cannot unmarshal !!str into int`) — unchanged. Binding: precedent is `validateResponseReserveFraction` called from
`parseConfigFile` (`:2287`) — the refusal is a load error every Driver already surfaces
loudly; a custom type rather than a second pass because the truncation happens INSIDE the
decoder and cannot be seen after it. The `int` consumers (`applyFile` `:580-585`, the
entry-level twin, any `fc.ContextWindow` read — grep at implement time) take `int(...)`.
Marshalling (the settings write path re-encodes the file) is unchanged: a named int marshals
as an int. Update the comment at `:577-579`.

**Files:** `internal/config/config.go`, `internal/config/config_test.go`,
`cmd/apogee/wire_server.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/launcher.go`,
`cmd/apogee/delegation.go`, `cmd/apogee/wire_options.go`, `cmd/apogee/wire_test.go`

**Tests.** A table beside the existing `context-window` file tests (`config_test.go:1571`):
`context-window: 3.5` → load error containing `context-window` and `0 or more`;
`context-window: -1` → the same error; `context-window: 0` → unpinned (`Options.ContextWindow
== 0`); `context-window: 65536` → 65536; `context-window: lots` → still an error. The same for
a server entry's `context-window:`; `context-window: 0x10000` → 65536 (hex still loads);
`context-window: 1e3` → the load error (the accepted change, pinned so it is deliberate).
`TestApplyConfigServersInvalid`'s negative entry-level row (`config_test.go:2709-2712`) pins
`servers: entry 1`, `box`, `context-window: -8192 is negative`, `1 or more` — a scalar
Unmarshaler cannot name the entry index or name, so REWRITE that row to want `context-window`
and `0 or more` (the user-visible locator moves from `entry 1 ("box")` to the yaml line the
decoder reports; state that in the sidecar entry).

**Acceptance.** `go build ./... && go test ./internal/config/`

**Commit.** `fix(config): a fractional or negative context-window is refused at load instead of floored`

## 4. The `/skills` report reads one catalog snapshot — ✅ DONE (2026-08-28)

NOTES (2026-08-28): `Provider.Skipped`'s doc comment claimed it "reads the SAME snapshot List does" — the claim this finding disproves — so the correction the item asks for at `provider.go:113-116` rewrites it to say the opposite (it reads the snapshot in force when it is called) and points a both-halves reader at `Report`, rather than merely adding a mention of `Report`.

NOTES (2026-08-28): the `SkillCatalog` doc block (`tui.go:24-31`, inside the item's cited `24-43` range) enumerates the interface's members and attributed the `/skills` report to `Skipped`; one clause was added naming `Report` as the pair the report takes. The item's doc-correction list named only `provider.go` and `catalog.go`.

**What.** Audit: *`/skills` can report two catalog snapshots as one* (Medium). Every
`Provider` accessor loads `p.cur` independently (`internal/skills/provider.go:105-129`), and
`noteSkillCatalog` (`internal/tui/skills.go:82-91`) calls `List()` then `Skipped()` with a
`Reload` able to land between them (the `/skills` rescan runs off the Update loop,
`skills.go:290`). Add ONE combined accessor, `Report() (list []Skill, skipped []SkipError)`,
to `Provider`, served from a single `p.current()` load; add `Report()` to the `SkillCatalog`
interface (`internal/tui/tui.go:24-43`) and to BOTH test implementers — `fakeSkillCatalog`
(`internal/tui/skill_test.go:29-60`) and `reloadableCatalog` (`skill_test.go:579-594`, returns
`*f.skills, nil`; missing it fails the whole `internal/tui` test package's compile);
`noteSkillCatalog` calls `Report()`. `Catalog.Report()` delegates to `c.List()` (sorted by
DisplayName then ID, `catalog.go:98-110` — `skillCatalogNote` relies on that order) and a clone
of `c.Skipped()`. Binding: a combined
accessor rather than an exported snapshot getter — the interface stays behavioural (ADR 0031:
a Driver reads answers, not the engine's internals). Correct the doc comments at
`provider.go:113-116` and `catalog.go:17-20` to name `Report` as the one-snapshot read; `List`
and `Skipped` stay for their single-accessor callers (`internal/tui/autocomplete.go:553`).

**Files:** `internal/skills/provider.go`, `internal/skills/catalog.go`,
`internal/skills/provider_test.go`, `internal/tui/tui.go`, `internal/tui/skills.go`,
`internal/tui/skill_test.go`

**Tests.** `internal/skills`: `TestProviderReportIsOneSnapshot` — swap the catalog with
`p.cur.Store` from a goroutine between two `Report` calls in a loop and assert every returned
pair is internally consistent (a skill id never appears in both halves; the pair's sizes
match one of the two seeded catalogs). `internal/tui`: `TestSkillCatalogNote*`
(`skill_test.go:897`, `:1127`) and `TestSkillsCommandReportsSkipped` (`:1214`) still pass
through `Report`.

**Acceptance.** `go build ./... && go test ./internal/skills/ && go test ./internal/tui/ -run 'Skill'`

**Commit.** `fix(skills): the /skills report is served from one catalog snapshot`

## 5. Skill descriptions are capped for the index and an unreadable entry is recorded — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the new unreadable-entry test skips as root (a 0-mode dir stays readable), per the item's text; verified passing under an unprivileged user and failing without the `load.go` fix.

**What.** Audit: *the skills BM25 index is built from the full, uncapped description* (Medium,
Security) and *an unreadable skills-dir entry is silently dropped* (Medium, Security). (1)
`Description` is set uncapped at `internal/skills/parse.go:190` (frontmatter path) and `:338`
(no-frontmatter path) and indexed whole by `buildIndex` (`internal/skills/suggest.go:249`) on
every `Load`/`Reload` (`load.go:71`). Add `maxDescriptionLen = 4096` (runes, ratified) beside
`maxSummaryLen` (`parse.go:16`) and clamp `Description` at both assignments with the existing
rune-safe `clampSummary` generalised to `clampRunes(s string, max int) string` (binding — one
clamp, two limits; `clampSummary` becomes `clampRunes(s, maxSummaryLen)`). The `parse.go:14-15`
and `skill.go:25-29` comments state the two caps: 200 for the menu, 4096 for the matcher.
(2) The `fs.WalkDir` callback (`load.go:209-211`) returns `nil` on `walkErr != nil` without
recording. Before returning, when `p != "."`, record
`cat.addSkip(SkipError{Path: absSkillPath(dir, p), Err: fmt.Errorf("skill dir entry %s was not scanned: %w", p, walkErr)})`
— the trio the four sibling branches use (`:219-252`). The root `p == "."` case stays silent
(the anchor's own failure is already recorded at `:195-201`).

**Files:** `internal/skills/parse.go`, `internal/skills/load.go`, `internal/skills/skill.go`,
`internal/skills/parse_test.go`, `internal/skills/load_test.go`,
`internal/skills/suggest_test.go`

**Tests.** (1) A skill whose `description:` is 10 000 runes loads with
`len([]rune(Description)) == 4096`; `TestSuggestIndexesTheDescriptionPastTheMenuClamp`
(`suggest_test.go:365`) still passes; a term placed past rune 4096 is NOT suggested. (2)
`TestLoadSymlinkEscapeRefused` (`load_test.go:402`) unchanged — the walk never errors on a
symlink entry (over `os.Root.FS()` the escapee comes back as a non-dir entry with `err == nil`
and is dropped at `load.go:238` as a non-`SKILL.md`), so no `Skipped()` assertion is added
there; a new test makes a sub-directory unreadable (`chmod 0`, skipped on Windows
and when running as root — `os.Geteuid() == 0`) and asserts one `SkipError` naming it while
the sibling skill still loads.

**Acceptance.** `go test ./internal/skills/`

**Commit.** `fix(skills): descriptions are capped before indexing and an unreadable entry is reported`

## 6. A file-name row in the dropdown is one line and inserts exactly what it shows — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the item asked for one sentence added to the `fileSuggestions` doc block and to
the `doc.go` trade record; each also had its existing claim corrected from "escape-stripped"/"strips
the workspace path" to "escape-stripped and flattened"/"sanitizes the workspace path", because the
old wording would have described only half of what the code now does. The surrounding lines were
re-wrapped only where those edits changed the fill.

**What.** Audit: *a workspace file name containing a newline or tab forges extra dropdown
rows* (Medium, Security). `fileSuggestions` (`internal/tui/autocomplete.go:654-662`) strips
escapes at `:658` but never flattens, unlike `skillRow` (`:611-615`); a `\n` paints extra rows
through `popupRowBlocks` (`popup.go:896-903`) and lands in the composer as a real line the
`@`-ref scanner cuts at (`command.go:747-770`); a `\t` is expanded to spaces by the popup
(`popup.go:588`) and by the textarea on insert, so the row and the value differ and
`autocompleteExactMatch` never matches. Change `:658` to `p = flattenField(stripEscapes(p))`
— the `skillRow` idiom — so the row cell, the value and the ref token all derive from one
flattened string. Extend the doc block at `:622-653` (and the trade recorded at
`doc.go:949-958`) with one sentence: a name carrying a line or tab break is flattened like an
escape and becomes unreferenceable through the dropdown, the same trade ESC already takes.

**Files:** `internal/tui/autocomplete.go`, `internal/tui/doc.go`, `internal/tui/transcript_test.go`

**Tests.** Beside `TestAutocompleteRowsStripEscapes` (`transcript_test.go:840`): seed
`fileCache` with `"docs/no\ntes.md"` and `"docs/a\tb.md"`; assert each yields exactly one
rendered row with no `\n`/`\t`, and that `acceptAutocomplete` splices a token equal to
`fileRefToken(row value)`. Extend `TestAcceptedFileRowMatchesItsValue` (`:884`) with the tab
seed — it must fail against the pre-item tree (bite check).

**Acceptance.** `go test ./internal/tui/ -run 'Autocomplete|FileRow|Escape'`

**Commit.** `fix(tui): a file name with a line or tab break is one dropdown row and inserts as shown`

## 7. `/clear` and `/new` in a pre-bound session reset the view without an engine call — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the view-reset block was extracted into a new `resetSessionView` method so
the pre-bound branch and the bound path run the one block rather than a copy; the item's text
said to "run the view reset block" without naming the mechanism.

NOTES (2026-08-28): three comments inside the moved block named control flow that stayed behind
in `startNewSession` ("A refused clear returns above and never reaches this line", "The clear
above was a session boundary", "The Rotate queued above") — each was reworded to stay true when
read from either caller, and the pre-bound case was named in the two where it differs.

NOTES (2026-08-28): the reset block holds `noteContextFiles`, which reads
`Engine.ContextFilesReport`, so the pre-bound path is not literally engine-call-free as the
item's title says; the **What**'s binding enumeration (skip the save, the exchange check and
`ClearContext`) is what was followed, and the unbound holder answers that read with a zero
report (`cmd/apogee/wire_engine.go:256-261`), so nothing is noted.

**What.** Audit: *in a pre-bound session, `/clear` and `/new` report a misleading error and
skip the reset* (Medium). `startNewSession` (`internal/tui/commandrun.go:126`) calls
`saveAtIdle`, `InExchange` and `ClearContext` on the `lateEngine`, whose unbound answers are
`errNoServerBound` (`cmd/apogee/wire_engine.go:116`, `:221-226`), and returns at `:135-139`
before `transcript.reset()` (`:151`); `submit` dispatches commands (`model.go:1536`) before
the `prebound()` gate (`:1551`). Ratified: view-only reset. At the top of `startNewSession`,
when `m.prebound()` (`internal/tui/prebound.go:55`): skip the save, the exchange check and
`ClearContext`, run the view reset block (`:145-182` — transcript, spent skills, live stats,
title, whatever the block holds at implement time) and return with no note. `Options.Prebound`
is left as it is — the reason and its footer/startup box survive the reset. Binding: no new
note text; the pre-bound refusal wording (`preboundRefusal`) is not reused here because nothing
was refused.

**Regression guard.** Pre-bound + resume is the case the view-only reset must NOT take:
`cmd/apogee/wire_live.go:181` resolves `--resume`/`--continue` regardless of `Prebound`, the
host starts active on that record (`:196-203`), the TUI replays its scrollback
(`model.go:574`), and the binder later seeds the agent with `b.resumed` (`wire_server.go:149`)
— nothing the TUI does changes that pointer. A view-only `/clear` there, followed by `/server`,
binds an engine that still holds the whole resumed conversation: exactly the fresh-looking
view lying about an engine that remembers which `commandrun.go:123-125` forbids, and today's
path is honest (the refusal note, view intact). Binding: the pre-bound branch takes the
view-only reset only when `m.opts.Resumed == nil` (`tui.go:1148`, set at
`wire_options.go:265`); with a resume pending it falls through to today's behaviour unchanged
(`ClearContext` on the late engine → the existing `could not clear context: … no server is
bound yet` note). `prebound()` is `Options.Prebound.Reason != ""` (`prebound.go:55`), cleared
only by a committed bind (`:166`), so it is true exactly while no engine is bound — no other
state reaches this branch.

**Files:** `internal/tui/commandrun.go`, `internal/tui/minilang_test.go`

**Tests.** Beside `TestClearCommandSurfacesEngineError` (`minilang_test.go:111`): a model with
`Options.Prebound.Reason` set and a `fakeEngine` whose `clearFn` fails with an
`errNoServerBound`-shaped error — `/clear` adds no `could not clear` note, `clearFn` is never
called, the transcript is the startup box, and `m.prebound()` is still true; `/new` the same.
Guard row: the same model with `Options.Resumed` set — `/clear` keeps today's `could not
clear` note and the transcript (bite check: this row passes before and after; the two above
fail before).

**Acceptance.** `go test ./internal/tui/ -run 'Clear|New|Prebound'`

**Commit.** `fix(tui): /clear in a pre-bound session resets the view instead of asking for a server`

## 8. A url-safety edit re-applies to the live MCP connection — ✅ DONE (2026-08-28)

NOTES (2026-08-28): `mcpGuard` de-methodised from `*rootWiring` to a package-level function in
`wire_live.go` (name and body unchanged, both call sites updated). The item's text says the apply
partitions "under `w.mcpGuard(…)`", but `applyURLSafetyHosts` is composed from the root's members
rather than from the root and holds no `w`; building the guard through a second call to
`security.NewURLGuard` would have been exactly the drift `mcpGuard` exists to prevent.

NOTES (2026-08-28): the e2e step-6 rewrite does not read the disconnect NOTE off the pty. An apply
note lands on the settings row alone (`settingsapply.go` → `settingEdit.note`, rendered only in the
`/settings` pane); it never reaches the transcript or the session record, so "the session record
carries the disconnect note" is not observable there. The note's exact wording is pinned by
`TestApplySettingURLSafetyHostsDropsAnMCPServerTheNewListDenies` instead, and step 6 observes the
other announced half — the tool is gone, the ask comes back as an unknown tool, and the refused
rename reconnect brings nothing back.

NOTES (2026-08-28): `docs/manual/configuration.md` gained two sentences under "Both lists are
live" — beyond the item's Files list, but the behaviour it documents is user-facing (a host-list
edit can now disconnect a connected MCP server and says so on the row) and the section stated the
old behaviour as the whole story.

NOTES (2026-08-28): a config file that no longer parses at the re-admission is reported with
`liveMCP`'s own reconnect-failure sentence rather than a new wording, and never as the row's error
— the item states guard (b) for a failed dial and is silent on a failed re-read; the same reasoning
(the tool rebuild has already committed) applies to both.

**What.** Audit: *after a `/settings` url-safety edit, network tools and the MCP connection
disagree about which hosts are allowed* (Medium, Security; the audit's path is wrong — the
code is `cmd/apogee/wire_settings.go`). `applyURLSafetyHosts` (`wire_settings.go:1250-1272`)
rebuilds only the tool set; the MCP guard is consumed at connect time and never retained
(`internal/mcp/transport.go:201-255`), so the only way the connection follows a list is a
reconnect (`liveMCP.reconnect`, `cmd/apogee/wire_mcp.go:83`, validate-then-commit,
"previous connections kept" on failure), and `mcp.Connect` is all-or-nothing
(`internal/mcp/client.go:75-96`). Ratified: reconnect under the new guard; denied servers are
dropped and named. (1) `internal/mcp`: add `Admit(servers []ServerConfig, guard security.URLGuard) (admitted []ServerConfig, denied []Denied)`
where `Denied{Name string; Err error}`; it runs the endpoint vet `checkEndpoint` (`:244`)
performs — the same `guard.DisableIPFloor().CheckContext` — on every SSE/streamable server and
admits every stdio server (no endpoint). (2) `applyURLSafetyHosts`, after the tool rebuild
succeeds: re-read the file's `mcp-servers:` exactly as `reconnectMCP` does (`:1576-1582`),
partition with `Admit` under `w.mcpGuard(spec.allowHosts, spec.denyHosts)` over the tool set's
current spec (`wire_live.go:97-100` reads it the same way), and call
`a.mcp.reconnect(admitted, a.tools, a.engine)`. The note: `toolRosterNote` plus, when `denied`
is non-empty, `; mcp server <name> disconnected — its endpoint is denied` per server (binding
wording; one note, no second row). A reconnect failure is returned as the row's error the way
`mcpReconnectFailed` already words it — the previous connections are kept and the note says so
(guard (b) below: in the NOTE, not as the row's error).
A busy engine refuses the whole url-safety edit today already, at the first `SwapTools`
(`internal/agent/swaptools.go:67`, idle-only), before any reconnect is reached — the plan
adds no new busy refusal (ADR 0037 binding A holds as it is). (3) Correct the comment at
`wire_live.go:92-96`: the two surfaces agree because a host-list edit reconnects when a
verdict changed, not only because a later reconnect reads the live spec; amend the two places
that document the OLD behaviour as design — `docs/design/test-drivers.md:738` ("read off the
tool that STILL answers under its old alias") and a one-line `[Unreleased]` CHANGELOG note
that the F-40 entry (`CHANGELOG.md:794-798`) is superseded by this item (the sidecar carries
it). (4)
`cmd/apogee/e2e_egress_test.go` steps 5–6 (`:140-174`) pin the OLD behaviour; rewrite step 6:
after the deny lands in step 5, `docs__echo` is gone — a submit that asks for it gets the
model's fallback / an unknown-tool refusal, and the session record carries the disconnect
note; the rename reconnect is refused as before (endpoint still denied) and nothing comes back.
ADR 0037 decision 7 ("a refused reconnect keeps the old connections") loses its e2e
observation here (after step 5 the previous set is empty); it stays unit-covered by
`TestMCPReconnectUsesTheLiveURLSafetyLists` — say so in the test's step comment.

**Regression guard.** Three guards, all binding. (a) **Reconnect only when the admitted set
changed.** `liveMCP.reconnect` always dials the new set — a NEW process per stdio server
(`internal/mcp/transport.go:141`), initialize + `tools/list` per HTTP server (10 s timeout each,
`:284`) — then closes the old one; it runs synchronously on the Update goroutine
(`internal/tui/settingsapply.go:195`). Unconditional, every `allow-hosts`/`deny-hosts` edit
(the common case: a web-tool host while a stdio server is connected) would freeze the TUI for
the dial, lose every server's state, and a stdio server holding a lock/port would fail its
second launch. Today that edit is instant and leaves MCP alone. So: run `Admit` under the OLD
spec's guard and under the NEW one over the same `file.MCPServers`; when the two admitted name
sets are equal, skip the reconnect entirely (the connection is already under an equivalent
verdict; the tool set follows the new lists as today). The plan's own "a deny that covers
nothing reconnects the same set" test is INVERTED: a deny that covers nothing does NOT
reconnect (assert the holder's client identity is unchanged). (b) **A reconnect failure is
note text, never the row's error.** The tool rebuild is already committed (`wire_tools.go:172-
183`) when the reconnect runs; returning an error makes the pane say `saved — live apply
failed` (`settingsapply.go:161-164`) about an edit whose primary effect IS in force. Return
`nil` with the note `toolRosterNote + "; mcp reconnect failed: <err> — previous connections
kept"`. (c) **A dormant holder skips the MCP half.** url-safety rows reach through
`reachesTheSwapDoor` (`wire_settings.go:1024-1030,1385`: `tools != nil && engine != nil`), and
`liveMCP.reconnect` on a nil receiver derefs (`wire_mcp.go:84`); `TestApplySettingURLSafetyHostsSwapTheSet`
(`wire_test.go:1390`) and `schedule_test.go:557-564` build the applier with no `mcp` and no
`configPath`, as the documented embedder Driver may (`wire_mcp.go:109-110`, ADR 0031). When
`a.mcp == nil` or `a.configPath == ""`, the MCP half is skipped and the note is
`toolRosterNote` alone — those two tests stay green unchanged. Minor: `Admit`'s denial wording
for an empty/unparseable endpoint says `its endpoint is denied` only when the guard denied it;
a parse failure names the parse error.

**Files:** `internal/mcp/client.go`, `internal/mcp/transport.go`, `internal/mcp/mcp_test.go`,
`cmd/apogee/wire_settings.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_live_test.go`,
`cmd/apogee/e2e_egress_test.go`, `docs/design/test-drivers.md`

**Tests.** `internal/mcp`: `Admit` splits an SSE server on a denied host from a stdio server
and an allowed SSE server. `cmd/apogee`: beside `TestMCPReconnectUsesTheLiveURLSafetyLists`
(`wire_live_test.go:76`) — applying `url-safety.deny-hosts` that covers a connected server's
host reconnects without it and the note names it; **a deny that covers nothing does NOT
reconnect** (the holder's client is the same object before and after; no connect call on the
fake transport); a reconnect failure (fake transport refuses the second dial) leaves the row
error-free with the `previous connections kept` phrase in the note; `TestApplySettingURLSafetyHostsSwapTheSet`
(`wire_test.go:1390`) and the `schedule_test.go:557` apply pass UNCHANGED (the nil-holder
guard). The e2e step-6 rewrite observes the announced surface (the note text and the absence
of the tool). `TestE2EEgressDeniedMCPEndpointStopsTheLaunch` unchanged.

**Acceptance.** `go build ./... && go test ./internal/mcp/ && go test ./cmd/apogee/ -run 'MCP|URLSafety|Egress'`

**Commit.** `fix(cmd): a url-safety edit reconnects MCP under the new host lists and drops denied servers`

## 9. `captureStderr` restores the process stderr even when the wrapped call bails — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the new test's subtest body carries one line the item did not specify — an
unreachable `sub.Fatal` after the `captureStderr(sub, func() { sub.Skip("bail") })` call — so a
helper that ever stopped Goexiting would be caught rather than silently making the test vacuous.

NOTES (2026-08-28): `internal/agent/library_corrupt_store_test.go` gained the `runtime` and `time`
imports the new test needs; the other two files already had both.

**What.** Audit: *`captureStderr` leaks the process stderr pipe and a reader goroutine if the
wrapped call panics* (Medium). Three copies share the defect — `cmd/apogee/wire_test.go:76-95`
(callers `:853`, `confinement_e2e_test.go:153`), `internal/library/store_test.go:345` (caller
`:379`), `internal/agent/library_corrupt_store_test.go:25` (caller `:94`); none defers, and a
`t.Fatal` inside `f()` (a `runtime.Goexit`) skips the restore as a panic does. In each copy:
register `t.Cleanup(func() { _ = w.Close(); os.Stderr = orig })` immediately after the swap
(idempotent with the happy-path restore, which stays so the captured string is returned in
order), and `defer r.Close()` for the read end, which today is never closed. Binding: fixed in
place, no shared helper package (out of scope).

**Files:** `cmd/apogee/wire_test.go`, `internal/library/store_test.go`,
`internal/agent/library_corrupt_store_test.go`

**Tests.** In each package, one test `TestCaptureStderrRestoresOnGoexit`: a `t.Run` subtest
calls `captureStderr(sub, func() { sub.Skip("bail") })` — `Skip` is the `runtime.Goexit` path
with no failure to swallow — and after `t.Run` returns the outer test asserts `os.Stderr` is
the original file and `runtime.NumGoroutine()` is `<=` its pre-subtest count within a
short poll (the reader goroutine ended because the write end was closed; `<=`, not `==` — in
`cmd/apogee` other tests' httptest idle-conn goroutines wind down asynchronously and an
equality flakes). Bite check: against the pre-item helper the `os.Stderr` assertion fails.

**Acceptance.** `go vet ./cmd/apogee/ ./internal/library/ ./internal/agent/ && go test ./cmd/apogee/ -run 'CaptureStderr' && go test ./internal/library/ ./internal/agent/ -run 'CaptureStderr|Corrupt'`

**Commit.** `test: captureStderr restores os.Stderr and closes its pipe on every exit path`

## 10. The watcher's "absent at Start, appears later" case is pinned — ✅ DONE (2026-08-28)

**What.** Audit: *the config watcher's zero baseline re-applies the whole config when a
previously-absent file first appears* (Medium) — re-verification corrects the consequence:
`ReloadConfig` (`cmd/apogee/settingsedit.go:222`) diffs the appearing file against the
defaults baseline `newExternalEdit` took from the same missing file (`:152`), so only keys
that actually differ apply, and apogee's own seed write re-takes the baseline (`refresh`,
`:257`). The documented behaviour (`internal/filewatch/filewatch.go:113-115`) is correct;
what is missing is a test — `TestWatchSurvivesADeleteAndRecreate` (`filewatch_test.go:146`)
covers delete-then-recreate, nothing covers absent-at-Start. Ratified: test-only. Add
`TestWatchReportsAFileThatAppearsAfterStart`: Start on a missing path (`startWatcher`, `:26`,
does not require the file; `sample()` returns the zero state, `filewatch.go:186-191`);
`expectNoChange` for `testQuiet`; create the file; exactly one report; then silent. No
production change.

**Files:** `internal/filewatch/filewatch_test.go`

**Tests.** The new test, using the file's own helpers — `expectNoChange` (`:58`) and
`awaitChange` (`:45`; there is no `expectChange`).

**Acceptance.** `go test ./internal/filewatch/ -run 'Watch'`

**Commit.** `test(filewatch): a file absent at Start is reported once when it appears`

## 11. The egress test proxy refuses an unmapped host, and its branches are unit-tested — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the hop-by-hop test uses a test-local header-recording httptest destination
(`recordHeaders`, in `netfix_test.go`) rather than `PageServer` as the item's text says — `Page`
records hits only and exposes no request headers, and growing the shipped fixture's API for one
call site would be the speculative generality the standards forbid. The routed happy-path test does
use `PageServer`, as the item asks.

NOTES (2026-08-28): `netfix_test.go` is not named in `doc.go` — `docmap.Check` skips `_test.go`
files, so it does not demand it (the item made that conditional).

**What.** Audit: *the egress test proxy can dial real hosts* (Medium, Security) and *the
egress-instrument half of T-18 has no unit test* (High). `DialContext`
(`internal/tuitest/netfix.go:73-80`) passes an unmapped `addr` through to a real dial, and
`serve` (`:137-176`) accepts any absolute URI from any loopback client, against the file's own
guarantee (`:3-7`, `:20`); nothing in `internal/tuitest` tests `netfix.go` (no `netfix_test.go`),
and `cmd/apogee/e2e_egress_test.go` maps every host it names (`:71-75`) — no test relies on the
pass-through. (1) `DialContext` returns
`fmt.Errorf("tuitest: no route for %s — every host a driven run may reach is mapped", addr)`
when `routes[host]` is absent OR `net.SplitHostPort` fails; `serve`'s existing dial-error
branch (`:154-157`) turns that into the 502 it already sends. The doc at `:64-66` flips: a
destination with no route is refused, so the header's guarantee is enforced rather than
assumed. (2) New `internal/tuitest/netfix_test.go`: the 400 on a non-absolute request URI
(`:138-142`); the 502 on an unmapped host, with the proxy log recording the attempt (binding:
the log is an access log of what reached the proxy, refused or not); hop-by-hop stripping
(`:150-152`) — a `PageServer` handler records the request headers, and `Proxy-Connection`,
`Connection`, `Keep-Alive` are absent; the routed happy path through `PageServer`, `Saw(host)
== 1`. Name the test file in `doc.go` only if `docmap.Check` demands it (it lists sources).

**Files:** `internal/tuitest/netfix.go`, `internal/tuitest/netfix_test.go`

**Tests.** The four unit tests above; `TestE2EEgress` (PTY, `cmd/apogee/e2e_egress_test.go:65`)
unchanged and green where it runs.

**Acceptance.** `go test ./internal/tuitest/ -run 'Proxy|Forward|Netfix' && go vet ./cmd/apogee/`

**Commit.** `fix(tuitest): the forward proxy refuses an unmapped host and its branches are unit-tested`

## 12. `ReplayTrace`'s error branches and all twelve F-keys are pinned

**What.** Audit: *`ReplayTrace`'s reader error branches are unpinned by any test* (High) and
*eight of twelve F-key constants are pinned by no test* (Medium). (1) `ReplayTrace`
(`internal/tuitest/screen.go:224-247`) `t.Fatalf`s on an unreadable file (`:227-230`), an
unquotable line (`:238-240`) and a failed write (`:241-243`); its only test is under
`//go:build !windows` (`pty_test.go:122-145`). Add tests in `screen_test.go` (no build tag) with
a `fatalRecorder` test double — embeds `testing.TB`, overrides `Helper`/`Fatalf` to record the
message and `runtime.Goexit()`; `ReplayTrace` runs in a goroutine the test joins (binding
shape; the double lives in `screen_test.go`, unexported): a missing path names the path; a
crash-truncated final line (`"\x1b[2Jhel` with no closing quote) names the line number and
`not a quoted trace write`; a valid trace with a trailing partial line still fails (the
tail is not silently dropped — pin that, it is the killed-run case). The write branch is
unreachable on a fresh screen (`Screen.Write` errors only when closed) — state that in a
comment beside the branch rather than testing it. (2) `TestKeysDecodeAsIntended`
(`driver_test.go:117-141`) gains rows for F2, F3, F6, F7, F8, F9, F10, F11 with their
`tea.KeyF*` codes, so the `keys.go:3-12` claim is true for every constant (ratified plan-author
call: pin all twelve rather than drop eight).

**Files:** `internal/tuitest/screen_test.go`, `internal/tuitest/driver_test.go`

**Tests.** As above; `TestReplayTraceRebuildsTheScreen` (`pty_test.go:122`) unchanged.

**Acceptance.** `go test ./internal/tuitest/ -run 'Replay|Keys'`

**Commit.** `test(tuitest): ReplayTrace's failure branches and every F-key are pinned on all platforms`

## 13. `make check` and CI run one actionlint

**What.** Audit: *`make check` can run a different actionlint than CI* (High). `Makefile:47`
short-circuits to any `actionlint` on PATH with no version check; `ci.yml:36` always runs the
pinned `go run`. Ratified: always `go run`. (1) `ACTIONLINT = go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)`
— the `command -v` goes; the comment at `:41-45` drops its "PATH wins" sentence and says the
module cache serves the offline case after the first run. (2) A `.PHONY: actionlint` target
running `$(ACTIONLINT) .github/workflows/*.yml`; `check` (`:244-246`) calls it. (3)
`ci.yml:36` becomes `run: make actionlint`, so the version literal lives once, in the
Makefile; the ci.yml comment (`:32-35`) says so. `scripts/check-pins.sh` is unaffected (it
vets `uses:` SHAs only).

**Files:** `Makefile`, `.github/workflows/ci.yml`

**Tests.** None new; the target runs.

**Acceptance.** `! grep -n 'command -v actionlint' Makefile && grep -n '^actionlint:' Makefile && grep -n 'run: make actionlint' .github/workflows/ci.yml && make actionlint`

**Commit.** `build: make check and CI run the one pinned actionlint`

## 14. The newcomer container leaves the host network namespace

**What.** Audit: *the live judge runs as root in the host network namespace in the newcomer
e2e container* (Medium, Security). `cmd/apogee/e2e_newcomer_test.go:160-161` runs
`docker run --detach --network host --volume <kit>:/kit:ro debian:stable-slim sleep 3600`; the
container needs exactly two things from the network — the in-process stubllm (today
`httptest` on 127.0.0.1, `:67`) and the internet to fetch a release — and root for `apt`.
Ratified: private bridge, stub on the bridge gateway. (1) Resolve the bridge gateway with
`docker network inspect bridge --format '{{(index .IPAM.Config 0).Gateway}}'`; empty or
unparsable → `t.Skipf` naming it (a fourth soft gate beside `:54-62`). (2) Start the stub with
`stubllm.Serve(ctx, gateway+":0", loadScript(t, "newcomer"))` (`internal/stubllm/server.go:93`,
sets `URL` from the bound listener at `:104`) instead of `stubllm.New`; a bind ERROR on the
gateway address (rootless docker, a remote `DOCKER_HOST` — the gateway is not a host
interface there) is also `t.Skipf`, naming the address, never a failure (regression-check
correction: those setups could not reach a loopback stub under `--network host` either, so
nothing that passes today is lost). Its URL is what the task prompt announces (`:210`, threaded
from `:71`) — unchanged wiring, a different address. (3) The run becomes
`docker run --detach --network bridge --pids-limit 512 --memory 2g --security-opt no-new-privileges --volume <kit>:/kit:ro <image> sleep 3600`;
the tool description at `:186-188` keeps "as root" and adds "on a private network: the model
server and the internet are reachable, nothing else on this machine is"; the user turn at
`:292` drops its "no internet" (it was never true under `--network host` and is not now). (4) The Linux gate
comment (`:58-61`) and `docs/design/test-drivers.md:813` say why Linux: the host can bind on
the docker bridge address there only. Binding: the image tag stays as it is (pinning a digest
is a release-hygiene question, not this finding).

**Files:** `cmd/apogee/e2e_newcomer_test.go`, `docs/design/test-drivers.md`

**Tests.** `TestNewcomerFollowsTheDocs` itself (env-gated: judge + docker + Linux); the
verifier runs it when the gates are met and otherwise proves the skip path names the gateway
gate. `go vet ./cmd/apogee/` in every case.

**Acceptance.** `go vet ./cmd/apogee/ && ! grep -n '"--network", "host"' cmd/apogee/e2e_newcomer_test.go && grep -n 'no-new-privileges' cmd/apogee/e2e_newcomer_test.go && go test ./cmd/apogee/ -run 'TestNewcomerFollowsTheDocs' -count=1`

**Commit.** `test(e2e): the newcomer container runs on a private bridge with the stub on the gateway`

## 15. The release smoke proves each published binary was built from the tagged commit

**What.** Audit: *the release smoke gate verifies published assets against checksums fetched
from the release itself* (Medium, Security). `scripts/release-smoke.sh:84-95` verifies
`dist/SHA256SUMS` locally and `:100-127` verifies `$work/SHA256SUMS` remotely; nothing ties
the published assets to the tree.

**Regression guard (this item was recast at the regression check).** The first shape —
diff the published `SHA256SUMS` against a fresh local `make dist` — fails EVERY correct
release: `make dist` (`Makefile:210-229`) `rm -rf dist`, rebuilds, `cp`s LICENSE/README with
fresh mtimes and packs with `tar -czf` / `zip -qr`, which store those mtimes, so two runs never
hash alike (verified 2026-08-28: same content, re-tarred → different sha); the release is cut
on macOS (`Makefile:36`) while the smoke may run on Linux — different tar/toolchain too. Archive
bytes are not the stable fact. The embedded build stamp is: the dist build is `-trimpath`,
`CGO_ENABLED=0`, default `-buildvcs`, so every published binary carries `vcs.revision` and
`vcs.modified`, readable with `go version -m <binary>` on any host for any GOOS (verified
2026-08-28 on a linux/arm64 cross-build: `build vcs.revision=<sha>`, `build vcs.modified=…`).
Binding: the cross-check reads that stamp; nothing diffs archive bytes.

After the remote verification succeeds (inside the `missing -eq 0` branch, `:122-124`): (1)
`want="$(git rev-parse -q --verify "$VERSION^{}")"` — unresolvable → `skip "vcs cross-check:
tag $VERSION is not in the local clone — git fetch --tags"` and stop this step. (2) For each
downloaded archive, extract only the binary into `$work/<name>/` — `tar -xzf … --strip-components=1 apogee_${BARE}_${t}/apogee`
for the four tarballs; `unzip -p … apogee_${BARE}_${t}/apogee.exe > …` for the two zips when
`unzip` is on PATH, else `skip` naming each zip (the header's "Needs:" line gains "`unzip` for
the two Windows archives' stamp check"). (3) `go version -m <binary>`: no `vcs.revision` line →
`fail "asset <name> carries no build stamp — was it built with -buildvcs=false?"`; revision ≠
`$want` → `fail "asset <name> was built from <rev>, not the tagged commit <want>"`;
`vcs.modified=true` → `echo "    warning: asset <name> was built from a modified tree"` — a
warning, never a fail (plan-author call: untracked files flip that flag, and a plan doc in
`docs/plans/` at cut time is routine); all six match → `echo "    6 assets carry
vcs.revision <short want> = the tag"`. (4) Every check is `if ! …; then fail …; fi` (or
`|| fail`): under `set -euo pipefail` (`:19`) a bare non-zero `diff`/`grep` aborts the script
before `fail` runs. The local `make dist` half (`:84-95`) and its skip rule are unchanged.

**Files:** `scripts/release-smoke.sh`

**Tests.** The verifier runs `bash -n`, then proves the stamp reader on a local build without
the network: `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o <scratch>/apogee ./cmd/apogee`
and confirms the script's stamp-reading function (factor it as `binary_revision <path>` so it
is callable) prints `git rev-parse HEAD`.

**Acceptance.** `bash -n scripts/release-smoke.sh && grep -n 'vcs.revision' scripts/release-smoke.sh && grep -n 'not the tagged commit' scripts/release-smoke.sh && ! grep -n 'published SHA256SUMS differs' scripts/release-smoke.sh`

**Commit.** `build(release): the smoke gate proves each published binary was built from the tagged commit`

---

## Suggested version bump

Patch (the next `0.x.y` after whatever the sweep's own suggestion lands as): items 2, 3, 6, 7
and 8 are user-visible behaviour fixes (a padded config now works, a fractional window is
refused, the dropdown, `/clear`, MCP after a url-safety edit) and items 5 and 11 close two
hostile-input holes — corrections, not features. The owner decides; no item bumps anything.

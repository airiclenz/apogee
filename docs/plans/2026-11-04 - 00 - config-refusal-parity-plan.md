# Config refusal parity — the three Seqcom docs-walk bugs

**Goal:** every config value the manual says is refused is refused, in the same words, by startup, a live re-read and `apogee probe config`. Closes Seqcom's docs-a3, docs-a5 and docs-a7 findings (apogee v0.31.0) by fixing the code to match the manual.
**Date:** 2026-11-04
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 8febcc90

**Regression check (2026-11-04, 8febcc90):**
- 1: guard folded (decision on the notice de-duplication; ConfigDir/LiveReread/ProjectRoot pins and the prose rule checked and folded).
- 2: guard folded; supersedes `validateSearchEndpoint`'s "deliberately loose" doc comment (`internal/config/registry.go`).
- 3: guard folded (decision on the checked-text key enumeration; file-pass parity case and template file checked and folded).
- 4: guard folded (decision on the checked-text key enumeration; parse-func refusal, project-layer skip, test recasts and comment rule checked and folded); supersedes the `workflow-retries` row comment (`internal/config/registry.go`, "FILE-ONLY, so no Validate hook … a negative count resolves to the default").

**Sources:**
- `/srv/seqcom-feedback/.capcom/issues/apogee-feature-requests/docs-a3-v0310.md`, `docs-a5-v0310.md`, `docs-a7-v0310.md` (untrusted reports; the repro steps are the useful part)
- `docs/manual/probe.md` §Config; `docs/manual/configuration.md` §"Where web_search looks — web-search-endpoint:", §"Delegated sub-agents — what one may spend"
- ADR 0021 (probe free half), ADR 0041 §7 (last good config kept), ADR 0043 amend 0916/0924 (row projections; checked-text keys)

**Ratified design calls** (owner, 2026-11-04):
- **probe config scope:** it refuses everything startup refuses — every block validator and `ValidateHost` — and reports the notices startup's later pass announces; only `StartupUndetermined` stays tolerated.
- **web-search-endpoint:** refused at load (startup, live re-read, probe config). After the `https://` heal it must be an `http`/`https` URL with a non-empty host, or a disable sentinel (`off`/`none`/`disabled`, any case).
- **Blank web-search-endpoint:** whitespace-only text means unset — the built-in provider — and the manual says so.
- **Count keys:** `delegate-max-depth` below 1 is refused at load; a negative `delegate-max-steps`, `delegate-fanout-rounds`, `delegate-max-tokens`, `workflow-retries`, `workflow-continuations` or `working-window` is refused at load. Each key's meaning for 0 is unchanged.

**Standing requirements:**
- skills: coding-standards
- A refusal names the key and the offending value, in the sentence the key's existing `Validate` hook already uses where one exists.
- Any deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Replying to Seqcom (an apply file after the plan ships, outside the repo).
- Line numbers in refusals for keys that are not decoder types.
- `sessions.max-count`, `ui.tools-fold-over` and other block keys moving to the file pass — item 1 closes their probe gap through the validators.
- `/settings`' per-block `settingsApplier.fileConfig` re-read, which deliberately skips block validators.

## 1. probe config refuses what startup refuses — ✅ DONE (2026-11-04)

NOTES (2026-11-04): `configReport` now takes the apogee home (`configDir`) rather than the config file path and derives the path with `config.FilePath`, so the startup pass and the reader read from the same home; the new pass lives in a helper, `startupRefusal`, which reuses `noFlagChanged`/`noEnvironment` from settingsedit.go.
NOTES (2026-11-04): the "server with no endpoint" case uses `servers: [{name: no-endpoint}]` plus `server: no-endpoint`; every case also asserts that `apogee probe host` really refuses the file, so a case that startup accepts cannot pass silently.


**What:**
**Goal:** `apogee probe config` exits non-zero with startup's own sentence for every config value `config.ApplyConfigOffer` refuses (a `StartupUndetermined` result excepted), and its `notices` section lists every notice that pass announces, each once. Fix for Seqcom docs-a3-v0310: `sessions: {max-age: 30d}` printed `0s`, `notices (none)`, exit 0, while `apogee probe host` failed with `invalid sessions.max-age "30d": …`.
**Approach (assumed at the header base):** `configReport` (`cmd/apogee/probeconfig.go`) reads through `config.LoadLayeredOffer`, which stops at `applyFile` and never reaches the block validators `resolveOptions` runs (`Present.Validate`, `UI.Validate`, `Sessions.Validate`, `Sessions.ValidateHost`, `SystemPrompt.Validate`, `contextFiles().validate()`, `ValidateServers`) nor the notice producers only `ApplyConfigOffer` runs (`resolveConfineToWorkspace`, `rosterNotices`, `mcpEnvAllowlistNotices`, `mcpStdioHeaderNotices`). Have `configReport` also run the same `ApplyConfigOffer` pass the watcher's `externalEdit.projection` (`cmd/apogee/settingsedit.go`) runs — no flags, no env — returning its error as the command's failure, tolerating `StartupUndetermined` as `projection` does, and folding its notices into the report de-duplicated against the reader's. Do not add validation inside `LoadLayeredOffer` (used by `internal/project` and pinned by `TestFieldLoadFileConfigKeepsItsRefusals`). Correct the `configReport` doc comment that says it reads the way the watcher does through `LoadLayeredConfig`. Update `docs/manual/probe.md` §Config only where its wording no longer matches.
**Regression guard.** The notice de-duplication is load-bearing — the reader's notices (unknown key, project-layer notices) are emitted by both LoadLayeredOffer and the ApplyConfigOffer pass, so the test must assert an unknown-key notice prints exactly once; the printed resolved values stay on the LoadLayeredOffer read so confine-to-workspace shows the file's value.
Thread the RunE's resolved `configDir` (`--config` over `APOGEE_CONFIG`) into `configReport` and build the pass's `Options{ConfigDir: configDir, LiveReread: true, ProjectRoot: projectRoot, ProjectRootPinned: true}` — never derive the dir from `path`. Run the pass only after the reader returned no error (past the `migration` early return), so `TestProbeConfigRefusesToMigrateALegacyFile` stays green and nothing is migrated. Fix by rule every comment in `probeconfig.go` (`probeConfigCommand`'s doc included) and sentence in `probe.md` §Config claiming the command fails only on the reader's refusals, that a project-file defect is always a notice, or that host identity is unread (`grep 'reader\|acknowledgement\|project file'` in both).
**Files:** cmd/apogee/probeconfig.go; cmd/apogee/probeconfig_test.go; docs/manual/probe.md
**Read first:** cmd/apogee/probeconfig.go — configReport, probeConfigCommand; cmd/apogee/settingsedit.go — externalEdit.projection; internal/config/config.go — ApplyConfigOffer, resolveOptions;
internal/config/layer.go — LoadLayeredOffer, layerProjectRoot; cmd/apogee/probeconfig_test.go — TestProbeConfigRefusesToMigrateALegacyFile
**Closes:** probe-config-skips-validators
**Tests:** in `cmd/apogee/probeconfig_test.go`, beside `TestProbeConfigFailsOnAMalformedFile`: a table driving `apogee probe config` on a scratch `--config` home for `sessions: {max-age: 30d}`, a negative `sessions.max-count`, an unknown `ui.spinner`, and a server with no endpoint — each exits non-zero and prints the same sentence `apogee probe host` prints for that file; a file that only triggers an `ApplyConfigOffer` notice (a bad `unconfined-hosts` entry) lists it under `notices` exactly once and exits 0; a file with an unknown top-level key prints that key's notice exactly once and exits 0; a home whose model cannot be determined still reports and exits 0; `TestProbeConfigRefusesToMigrateALegacyFile` still passes.
**Acceptance:**
- `go build ./cmd/apogee/`
- `go test -race -count=1 -run 'TestProbeConfig' ./cmd/apogee/`
- `go vet ./cmd/apogee/`
**Commit:** `fix(probe): make probe config refuse what startup refuses`

## 2. web-search-endpoint is refused at load

**What:**
**Goal:** a `web-search-endpoint` that, after trimming and the `https://` heal, is not an `http`/`https` URL with a non-empty host — and is not blank or a disable sentinel (`off`/`none`/`disabled`, any case) — is refused when the config is read, so startup, a live re-read and `apogee probe config` all fail naming the key and value; blank text loads as unset (the built-in provider). The tool and the validator decide through one parse. Fix for Seqcom docs-a5-v0310: `x y z`, `://`, `http://[` started fine and failed at call time.
**Approach (assumed at the header base):** extract the trim/sentinel/heal/parse logic of `resolveSearchEndpoint` (`internal/tools/web_search.go`) into one exported function in `internal/tools` (e.g. `ParseSearchEndpoint`) returning the endpoint kind and URL or an error; `resolveSearchEndpoint` calls it, and so does `validateSearchEndpoint` (`internal/config/registry.go`) — no second copy of the rules. Turn the `web-search-endpoint` registry row from `stringField` into a `checkedField`, the `re-stream-budget` / `delegate-timeout` pattern, so `Key.admit` runs the hook on the file pass. Whitespace-only text must reach the row as unset, not as a refusal. Rewrite the manual paragraph's last two sentences to the shipped rule (blank = built-in provider; a value with no http(s) host is refused at startup) and align `internal/config/defaults/config.yaml`'s comment for the key if it states the old rule.
**Regression guard.** Heal only a value with no `://` in it (not `Scheme == ""`: `localhost:8080/s` parses with scheme `localhost` and heals today), then require scheme `http`/`https` and a non-empty `u.Hostname()`, not `u.Host` (`https://http://[` has Host `http:`). On a parse error `resolveSearchEndpoint` keeps today's fall-through — trimmed text, `providerCustom`, not disabled — so an embedder's unvalidated `domain.Config.WebSearchEndpoint` still fails in `Execute` through the redacted error. Fix by rule every comment that lists the checked-text keys or says the endpoint loads unjudged (`grep -rn "lands through the row\|checked-text\|search endpoint" internal/config/*.go`), `keyfield.go`'s enumeration and count among them. Supersedes `validateSearchEndpoint`'s "deliberately loose" doc comment (`internal/config/registry.go`): rewrite it to the new rule.
**Files:** internal/tools/web_search.go; internal/tools/web_search_test.go; internal/config/registry.go; internal/config/registry_test.go; internal/config/keyfield.go; internal/config/keyfield_test.go; docs/manual/configuration.md; internal/config/defaults/config.yaml
**Read first:** internal/tools/web_search.go — resolveSearchEndpoint, NewWebSearch; internal/config/registry.go — validateSearchEndpoint, web-search-endpoint row; internal/config/keyfield.go — checkedField;
internal/config/keyfield_test.go — TestFieldLoadFileConfigKeepsItsRefusals; internal/tools/web_search_redaction_test.go — TestWebSearch_UnparseableEndpointDoesNotLeakAPIKey; internal/tools/network_test.go — TestWebSearch_SchemeLessEndpointSelfHeals
**Closes:** web-search-endpoint-unchecked
**Tests:** `internal/tools`: table test for the shared parse — `""`, `"   "`, `off`, `NONE`, `example.com/search`, `localhost:8080/s` (heals), `https://h/s?q=`, `x y z`, `://`, `http://[`, `http://`, `:8080/s`, `ftp://h` — with the existing `TestWebSearchSetEndpointResolvesLikeConstruction`, `TestWebSearch_SchemeLessEndpointSelfHeals`, `TestWebSearch_EmptyEndpointDefaultsToDuckDuckGo` and `TestWebSearch_UnparseableEndpointDoesNotLeakAPIKey` still passing. `internal/config`: move `"web-search-endpoint: not a url\n"` in `keyfield_test.go` from the loads list to the refused-at-load list; add `x y z`, `://` and `ftp://h` to `TestSettingKeyValidatorsRefuseWhatStartupWouldRefuse` and blank/sentinel/scheme-less shapes to `…AcceptTheirDocumentedShapes`; `TestApplyConfigRefusesUnusableWebSearchEndpoint`: `web-search-endpoint: "x y z"` fails naming the key.
**Acceptance:**
- `go test -race -count=1 -run 'WebSearch|SearchEndpoint' ./internal/tools/`
- `go test -race -count=1 -run 'SettingKeyValidators|LoadFileConfig|WebSearch' ./internal/config/`
- `go build ./...`
**Commit:** `fix(config): refuse an unusable web-search-endpoint at load`

## 3. delegate-max-depth below 1 is refused at load

**What:**
**Goal:** `delegate-max-depth: 0` or a negative value in a config file is refused when the config is read — startup, live re-read and `apogee probe config` fail naming the key and value — while an absent key still resolves to the default `1`. Fix for Seqcom docs-a7-v0310: `0` resolved silently to `1` (first half of `delegate-count-silent-clamp`; item 4 closes it).
**Approach (assumed at the header base):** `fileConfig.DelegateMaxDepth` is a plain `WholeCount` clamped by `atLeast(1, countPtr(…))` in its registry row, so absent and `0` are indistinguishable. Make it `*WholeCount` and bind the row as a `checkedField` exactly as `re-stream-budget` is bound, so `validateDelegateMaxDepth` refuses on the file pass. The fractional-value decode refusal (`WholeCount.UnmarshalYAML`) must still fire. Update every comment that states the clamp (grep `DelegateMaxDepth` and `delegate-max-depth` in comments across `internal/` and `cmd/`) and keep the manual and template sentence "It must be at least `1`" — add that a smaller value is a startup error if the paragraph does not already say so.
**Regression guard.** every comment that enumerates or counts the checked-text keys whose file pass lands through the row's Set (grep keyfield.go and registry.go for that enumeration and its count) gains delegate-max-depth and its count is corrected in this item; add internal/config/keyfield.go to Files
Add a `delegate-max-depth: 0` case to `TestFilePassRefusesThroughTheRows` (startup and `LoadFileConfig` give one sentence). The template (`internal/config/defaults/config.yaml`) takes the same added startup-error wording as the manual.
**Files:** internal/config/config.go; internal/config/registry.go; internal/config/keyfield.go; internal/config/config_test.go; internal/agent/subagent.go; cmd/apogee/wire_config.go; cmd/apogee/probeconfig_test.go; docs/manual/configuration.md; internal/config/defaults/config.yaml
**Read first:** internal/config/registry.go — delegate-max-depth row, re-stream-budget row, validateDelegateMaxDepth; internal/config/keyfield.go — checkedField;
internal/config/config.go — fileConfig.DelegateMaxDepth, WholeCount.UnmarshalYAML; internal/config/config_test.go — TestApplyConfigDelegateMaxDepth, TestFilePassRefusesThroughTheRows
**Tests:** `TestApplyConfigDelegateMaxDepth`: replace the case "a 0 is not a value here and resolves as absent" with `0` and `-1` refused naming `delegate-max-depth`, absent → `1`, `2` → `2`; `TestFilePassRefusesThroughTheRows` gains a `delegate-max-depth: 0` case; `TestApplyConfigCountKeysRefuseAFraction` still passes; fix `fileConfig` literals that set `DelegateMaxDepth` to use a pointer. `cmd/apogee/probeconfig_test.go`: `apogee probe config` on a home with `delegate-max-depth: 0` exits non-zero naming the key.
**Acceptance:**
- `go test -race -count=1 -run 'DelegateMaxDepth|CountKeysRefuseAFraction|FilePassRefusesThroughTheRows' ./internal/config/`
- `go test -race -count=1 -run 'TestProbeConfig' ./cmd/apogee/`
- `go build ./...`
**Depends on:** item 1 (shares `cmd/apogee/probeconfig_test.go`).
**Commit:** `fix(config): refuse a delegate-max-depth below 1 at load`

## 4. Negative count keys are refused at load

**What:**
**Goal:** a negative `delegate-max-steps`, `delegate-fanout-rounds`, `delegate-max-tokens`, `workflow-retries`, `workflow-continuations` or `working-window` in a config file is refused when the config is read, naming the key and value; `0` and an absent key resolve exactly as before for each. Fix for the same silent-default class as Seqcom docs-a7-v0310.
**Approach (assumed at the header base):** these rows use `atLeast(0, …)` (`working-window`: `atLeast(1, …)` with default `"0"`) over plain `WholeCount`/`TokenCount` fields, so a negative silently becomes the default. Apply item 3's mechanism — pointer field plus `checkedField` — to each, reusing each key's existing `validate*` hook where it already refuses negatives; give `workflow-retries` and `workflow-continuations` a hook refusing negatives. Check each hook keeps the key's own 0 meaning (unbounded / no ceiling / never / whole window). Update the manual's paragraphs for these keys only where they state a negative is ignored or clamped.
**Regression guard.** the same enumeration and count gain this item's six keys in this item; add internal/config/keyfield.go to Files
Refuse negatives for `workflow-retries` and `workflow-continuations` in the `checkedField` parse func with no `Validate` hook (the `parseHeadlessMaxTurns` / `ParseWorkflowWake` pattern): both rows are non-editable, and `TestRegistryValidateHooksSitOnEditableKeys` fails a hook there. Both keys are `ClassProjectParam`, so a project file's negative no longer resolves to the default but skips the whole project layer with `projectSkippedNotice` (workflow-wake's posture); startup does not fail. Fix by rule every comment saying a negative of these keys resolves to or reads as the default (`grep -rn "negative.*\(default\|resolves\)\|lands through the row's Set" internal/ cmd/`). Supersedes the `workflow-retries` row comment (`internal/config/registry.go`, "FILE-ONLY, so no Validate hook … a negative count resolves to the default") and its echoes in `fileConfig.WorkflowRetries` and `TestApplyConfigWorkflowKeys`'s doc.
**Files:** internal/config/config.go; internal/config/registry.go; internal/config/keyfield.go; internal/config/config_test.go; internal/config/registry_test.go; internal/config/layer_test.go; docs/manual/configuration.md
**Read first:** internal/config/registry.go — workflow-retries row, working-window row, parseHeadlessMaxTurns; internal/config/registry_test.go — TestRegistryValidateHooksSitOnEditableKeys;
internal/config/config_test.go — TestApplyConfigWorkflowKeys, TestFilePassRefusesThroughTheRows, everyKeyFileConfig; internal/config/layer.go — applyProjectRows
**Tests:** one table in `internal/config/config_test.go`: for each of the six keys, `-1` refused naming the key, `0` and absent resolve to today's values (assert the current resolved values, read from the tree before the change); `TestApplyConfigCountKeysRefuseAFraction` still passes; the two workflow keys' refusals are pinned as `TestFilePassRefusesThroughTheRows` cases, not in the `k.Validate` tables; `TestApplyConfigWorkflowKeys`'s case "a negative count resolves to the default" is recast as a refusal case carrying the new wantErr; `everyKeyFileConfig`'s `WorkingWindow` takes `countptr(32000)`; `internal/config/layer_test.go`: `TestLayeredConfigSkipsANegativeWorkflowCount` pins the skip notice naming the key and value.
**Acceptance:**
- `go test -race -count=1 -run 'CountKeys|Delegate|Workflow|WorkingWindow|SettingKeyValidators|FilePassRefusesThroughTheRows|ValidateHooksSitOnEditableKeys|LayeredConfigSkips' ./internal/config/`
- `go build ./...`
**Depends on:** item 3.
**Closes:** delegate-count-silent-clamp
**Commit:** `fix(config): refuse negative count keys at load`

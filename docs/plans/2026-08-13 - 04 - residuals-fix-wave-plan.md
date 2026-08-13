# Residuals fix wave — security-floor gaps, key-source polish, hygiene, and the SetSampling merge

**Goal:** close the ten owner-ratified residual items from `ISSUES.md`: the `rm -rf /Users/...`
dangerous-rule blind spot, the keystore stderr secret leak, the README key-source doc gap, the
false migration-notice claim on headless, the `ScopeEnv` PATH-fold duplication, the missing
`destination` path-agreement pin, the symlink-fragile writer-disclosure test roots, the
tab-width leak through `flattenField`, the stale `internal/domain/doc.go` marker list, and the
`SetSampling` wholesale-replace defect.

**Date:** 2026-08-13
**Status:** unexecuted
**Sized for:** ~200k-context host
**Skills:** coding-standards

**Authoritative sources:**
- `ISSUES.md` "Open defects" + the hostile-bytes and API-key-sources residual sections (as of
  this plan's write session) — the ten entries this plan closes are marked `[P]` there.
- Explore scout report 2026-08-13 (this plan's write session) — every file:line below was
  verified there; where an ISSUES citation was off by a line, the corrected location is used.
- [ADR 0047](../adr/0047-api-keys-resolve-through-a-per-entry-key-source.md) — the key-source
  contract item 5 documents.
- [ADR 0046] / `internal/agent/loop.go:686-691` — the output-cap rationale item 11 protects.
- Embedded config template `internal/config/defaults/config.yaml:45-88` — canonical key-source
  names and doc wording (NOT `cmd/apogee/defaults/`; item 12 corrects that stale citation).

**Ratified design calls (owner + plan author, 2026-08-13):**
1. **Scope** (owner, via AskUserQuestion at plan-write time): the nine residuals plus the
   `SetSampling` field-wise merge — the owner's 2026-08-13 deferral of the merge is reversed by
   this ratification.
2. **SetSampling semantics** (plan author, binding): field-wise merge exactly as the existing
   contract sentence at `internal/domain/hooks.go:619-620` promises — a non-nil field
   overwrites, a nil field leaves the current value untouched. There is NO clearing surface: a
   hook cannot reset a field back to nil; that would be new API and is out of scope.
   `revision++` stays unconditional.
3. **Keystore redaction** (plan author, binding): redact by substitution — every occurrence of
   the key value in captured stderr is replaced with `[redacted]` before `said(...)` folds it
   into the error; the rest of the tool's words are kept (diagnostic value preserved). Both
   error paths in `Store.Write` are covered.
4. **Migration-notice wording** (plan author, binding): `plaintextKeyNotice` gains a
   caller-supplied reason sentence; the TUI probe-failed path keeps today's sentence ("this
   machine has no secret store apogee can move it into" — true there), the headless path says
   "headless runs never prompt, so apogee cannot offer to move it into a secret store" (the
   current claim is false when the machine does have a store).
5. **Tab folding** (plan author, binding): `flattenField` folds `\t` to a single space alongside
   `\n`; `stripEscapes` keeps `\t` unchanged (body text legitimately contains tabs — the
   one-row field seams all route through `flattenField`, so that is the single fix point).
6. **Symlink-safe roots** (plan author, binding): one new test helper `tempRoot(t)` —
   `filepath.EvalSymlinks` over `t.TempDir()`, same resolution rule as the existing `realPath`
   helper (`internal/tools/path_safety_test.go:113`) — placed beside `realPath`; the named
   bare-sentence tests switch their roots to it. No production code changes.

**Standing constraint (owner, mid-session 2026-08-13):** nothing in this plan may narrow
apogee's read access to the skills folder — apogee must be able to read skills at all times.
No item below touches read-path resolution, confinement, or the skill loader; an implementer
who finds their change drifting into those surfaces must stop and report BLOCKED rather than
proceed.

**Out of scope:**
- The approved-out-of-workspace-write Execute gap (pending owner call; needs a grill).
- The `configwrite.go` ~1631-line split (wants its own plan).
- `/server` overlay resolution (pre-existing, ADR 0036 decision 6).
- Windows home patterns in ssh/credential rules beyond item 2's scope (declared ADR 0020 debt),
  the darwin seatbelt live test (needs hardware), the EXDEV fallback test (unreachable on a
  single-device tmpdir), `run_tests` unscoped PATH (plausibly deliberate).
- Any clearing/reset surface on `SamplingParams` (design call 2).
- Version bumps (see the closing note).

---

## 1. Verify the open-defects fix-wave plan is archived — ✅ DONE (2026-08-13)

**What:** Confirm `docs/plans/2026-08-13 - 03 - open-defects-fix-wave-plan.md` no longer exists
at that path and `docs/plans/archived/2026-08-13 - 03 - open-defects-fix-wave-plan.md` does —
that plan is executing as this one is written, touches `internal/tui` and `ISSUES.md` (files
this plan also touches), and its closeout removes three `ISSUES.md` entries this plan's item 12
must not collide with. If the plan is not yet archived, report BLOCKED — this plan waits.

**Files:** none (verification only).

**Tests:** none.

**Acceptance:** `ls "docs/plans/archived/2026-08-13 - 03 - open-defects-fix-wave-plan.md"`
succeeds and `ls "docs/plans/2026-08-13 - 03 - open-defects-fix-wave-plan.md"` fails.

**Commit:** none (no changes; the item is a gate).

---

## 2. `rm -rf /Users/...` joins the hard-refuse floor — ✅ DONE (2026-08-13)

NOTES (2026-08-13): The item's premise is factually wrong and the plan/ISSUES.md entry it cites
(`ISSUES.md:102-105`) with it — `rm -rf /Users/alice` did NOT pass before this change. Both `rm`
patterns open their target alternation with a bare `/`, which matches ANY absolute target, so the
`/(?:etc|usr|…|home|opt)` enumeration is unreachable in these two rules today. Verified by probe
before editing: `rm -rf /Users/alice` → hard-refuse `rm-rf-root-home-system`; `rm -fr /Users/alice`
→ hard-refuse `rm-fr-root-home-system`. The edit was made as written anyway (it is behaviour-
neutral, corrects the enumeration's documented intent, and pre-empts a real hole if the bare `/`
branch is ever narrowed), and the prescribed tests were added as regression pins. Item 12 should
still remove the ISSUES entry, but the closed trail must not claim a gap was closed.
NOTES (2026-08-13): Acceptance ran as `go build ./internal/security/ && go test
./internal/security/` instead of the item's `go build ./...` — a concurrently-implemented item's
in-flight tree breaks the repo-wide build (`internal/keystore/keystore.go:177: undefined:
redactKey`, item 3). Nothing in this item's packages is affected; `go vet` and `gofmt -l` are clean.

Depends on item 1.

**What:** In `DefaultDangerousRules()` (`internal/security/rules.go:17`), add `users` to the
system-path alternation of both `rm-rf-root-home-system` (`rules.go:25-29`) and
`rm-fr-root-home-system` (`rules.go:33-37`) — the alternation currently lists `home` but not
`users`, so `rm -rf /Users/alice` passes where `rm -rf /home/alice` hard-refuses. Lowercase
`users` (the guard's `normalize` in `dangerous.go` lower-cases inspected text), matching the
`/users/` spelling already used by `write-ssh-keys` (`rules.go:50`) and
`write-credential-persistence`.

**Files:** `internal/security/rules.go`, `internal/security/dangerous_test.go`,
`internal/security/rules_test.go`

**Tests:**
- New rows in `TestDangerousActionGuard_Tier1HardRefuse` (`dangerous_test.go:30`):
  `rm -rf /Users/alice` and `rm -fr /Users/alice` hard-refuse.
- New rows in `TestDefaultDangerousRules_HomeAnchoredRulesMatchTheMacOSHome`
  (`rules_test.go:283`) — the macOS-home table is the natural home, per its own comment.
- Existing precision floor stays green: `TestDangerousActionGuard_PrecisionNearMissesNotBlocked`
  (`dangerous_test.go:93`) — `rm -rf ./build` and friends still pass.

**Acceptance:** `go build ./... && go test ./internal/security/`

**Commit:** `fix(security): rm -rf /Users/... hard-refuses like the linux home`

---

## 3. Keystore errors redact the secret from store-tool stderr — ✅ DONE (2026-08-13)

NOTES (2026-08-13): design call 3 says "every occurrence of the key value"; the helper also replaces the `securityWord`-escaped spelling of the key, since a key needing quotes does not appear literally in a `security -i` echo — a superset of the ratified rule, pinned by its own test row.
NOTES (2026-08-13): the fake-tool fixture gained an echo-stdin mode (`APOGEE_KEYSTORE_FAKE_ECHO` + `fakeTools.leakStdin`; `fakeFailure` now takes the stdin it echoes) — the plan's "fake store tool that echoes its stdin to stderr". The spawn/exec-failure path cannot be staged with a tool that runs, so it is driven through the package's existing `runner` seam.

Depends on item 1.

**What:** In `Store.Write` (`internal/keystore/keystore.go:150`), both error constructions that
quote captured stderr through `said(...)` — the spawn/exec failure at `keystore.go:175-176` and
the refusal at `:182-183` — must first redact the secret: replace every occurrence of the key
value in the stderr text with `[redacted]` (design call 3). On darwin a failing `security -i`
can echo its scripted `add-generic-password ... -w <key>` line to stderr; on linux
`secret-tool` receives the raw key on stdin and could echo it likewise. Implement as a small
unexported helper in `keystore.go` applied to `outcome.stderr` before `said(...)` at both
sites; `said` itself (`run.go:115`) is unchanged.

**Files:** `internal/keystore/keystore.go`, `internal/keystore/keystore_test.go`

**Tests:**
- New: a fake store tool that echoes its stdin to stderr and exits non-zero — the returned
  error contains `[redacted]` and does NOT contain the key value; same for the exits-zero-but-
  complains shape (`security -i` behavior).
- Existing stays green: `TestWriteReportsWhatTheStoreSaid` (`keystore_test.go:681`),
  `TestWriteSendsTheSecretOnStdinAndNeverInArgv` (`:512`), `TestWriteRefusesWhatItCannotStore`
  (`:713`).

**Acceptance:** `go build ./... && go test ./internal/keystore/`

**Commit:** `fix(keystore): store-tool stderr is redacted before it reaches the error`

---

## 4. The migration notice names the real reason per driver — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the two reason sentences are unexported constants (`reasonNoStore`,
`reasonHeadless`) beside `plaintextKeyNotice` rather than literals at the call sites — the clause is
grammar-coupled to the notice template (lower-case, no trailing period) and the constants document
that at one place; the tests pin the sentences as literals, so the coupling stays checked.
NOTES (2026-08-13): the plan listed `TestPrepareKeyMigrationNoticesWithoutAStore` as "existing stays
green — asserts the TUI sentence", but it did not assert any sentence; added one assertion line
there so the session path's reason is pinned too (without it nothing would catch both callers
passing the headless reason).

Depends on item 1.

**What:** `plaintextKeyNotice` (`cmd/apogee/keymigrate.go:115`) hard-codes "this machine has no
secret store apogee can move it into" (`:117`) — true on the TUI path (its caller at
`keymigrate.go:68` fires only after `probeKeyStore()` said no store) but false on the headless
path (`cmd/apogee/headless.go:254` prints the notice for ANY plaintext key without probing, per
its own comment at `:247-252`). Add a `reason string` parameter carrying that one sentence
(design call 4): the TUI call passes today's sentence verbatim; the headless call passes
"headless runs never prompt, so apogee cannot offer to move it into a secret store". The rest
of the notice body (`:116-121` — the `api-key-env:`/`api-key-cmd:` alternatives, `chmod 600`,
`plaintext-key-ok: true`) is unchanged.

**Files:** `cmd/apogee/keymigrate.go`, `cmd/apogee/headless.go`,
`cmd/apogee/keymigrate_test.go`

**Tests:**
- Extend `TestPlaintextKeyNoticeNamesTheEntriesAndTheAlternatives` (`keymigrate_test.go:118`)
  to pass and assert the reason sentence.
- Extend `TestHeadlessNoticesPlaintextKeysAndNeverPrompts` (`:326`) to pin the headless
  sentence and assert the no-store claim is absent.
- Existing stays green: `TestPrepareKeyMigrationNoticesWithoutAStore` (`:267`) — asserts the
  TUI sentence, `TestPrepareKeyMigrationRaisesTheOfferWithAStore` (`:236`).

**Acceptance:** `go build ./... && go test ./cmd/apogee/`

**Commit:** `fix(keymigrate): the plaintext-key notice states the real reason per driver`

---

## 5. README documents the key sources — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the schema example carries the two alternative keys as a comment on the
existing `api-key:` line rather than as extra YAML keys — an entry may name only one source,
so spelling all three as live keys in one entry would show a config that is a startup refusal.

Depends on item 1.

**What:** `README.md` documents only `api-key` — the `servers:` schema example (`:547`), the
optional-keys enumeration (`:555`), the "### The upstream API key" section (`:604-612`), and
the second example block (`:624`). Add `api-key-cmd` and `api-key-env` to the schema example
and the enumeration, and give "The upstream API key" a short paragraph covering: the three
sources, the exactly-one-per-entry rule, `api-key-cmd` runs with no shell and takes the
command's trimmed stdout, `api-key-env` names a variable rather than holding a key. Source of
truth for names and semantics: the embedded template
`internal/config/defaults/config.yaml:45-88` and ADR 0047 — keep the README wording consistent
with the template's doc comments, shorter not longer.

**Files:** `README.md`

**Tests:** none (docs-only).

**Acceptance:** `grep -c "api-key-cmd" README.md` and `grep -c "api-key-env" README.md` both
return ≥ 2 (schema example + prose), and `git diff --stat` for the item touches only
`README.md`.

**Commit:** `docs(readme): document api-key-cmd and api-key-env key sources`

---

## 6. `ScopeEnv` reuses `hostRules.isPathName` — ✅ DONE (2026-08-13)

Depends on item 1.

**What:** `ScopeEnv`'s `add` closure (`internal/platform/host.go:120-135`) inlines the PATH
name-fold rule (`fold == "PATH"` at `:133`, with `fold` upper-cased on Windows) that
`hostRules.isPathName` (`host.go:175-180`) already expresses; the two agree today and drift if
the fold rule ever changes. Replace the `if fold == "PATH"` test with `r.isPathName(key)`. The
`fold` variable stays — the `seen` dedup map still needs it.

**Files:** `internal/platform/host.go`

**Tests:** existing suite stays green unmodified — it pins both sides:
`TestScopeEnvKeepsTheCallersAllowlistAndAddsThePlatformFloor` (`host_test.go:131`, including
the Windows `PATH`/`Path` dedup and POSIX `Path`-is-not-PATH subtests) and
`TestScopeInheritedEnvScopesOnlyPATH` (`:226`). No new tests — this is a pure refactor with
behavior pinned on both paths.

**Acceptance:** `go build ./... && go test ./internal/platform/`

**Commit:** `refactor(platform): ScopeEnv reuses the isPathName fold rule`

---

## 7. `flattenField` folds tabs — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the substitution is a package-level `strings.NewReplacer` (`fieldBreaks`, beside `flattenField`) rather than a second chained `strings.ReplaceAll` — one pass, and byte-for-byte the same semantics as the `ReplaceAll` it replaces, including leaving an invalid UTF-8 byte alone (which `strings.Map`, the file's other folding idiom, would have normalised to U+FFFD). House precedent: `internal/security/dangerous.go:220`.

Depends on item 1.

**What:** `flattenField` (`internal/tui/transcript.go:1517`) folds only `\n`, and
`stripEscapes` (`:1455`) deliberately keeps `\t` — so a tab in any one-row field (popup titles
via `popupTitleLine`, approval fields, argument labels, `resolvedPathNote`, autocomplete and
skill rows) survives to the width math, where lipgloss measures it as width 1 while the
terminal expands it to the next tab stop. Fold `\t` to a single space in `flattenField`
alongside `\n` (design call 5): update the containment fast-path and the replacement, and the
doc comment at `:1502-1516`. `stripEscapes` is untouched — body text keeps its tabs.

**Files:** `internal/tui/transcript.go`, `internal/tui/transcript_test.go`

**Tests:**
- New: a `flattenField` unit test — a string with `\t` and one with both `\t` and `\n` come
  back with single spaces; a string with neither is returned unchanged.
- Existing stays green unmodified: `TestStripEscapesDropsControlCharacters`
  (`transcript_test.go:665` — pins that stripEscapes KEEPS the tab, still true),
  `TestRenderPopupTitleFoldsNewlines` (`popup_test.go:1171`),
  `TestModelApprovalFlattensFieldsThatCouldForgeRows` (`model_test.go:1055`).

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `fix(tui): flattenField folds tabs so field width math holds`

---

## 8. The `destination` key gets its path-agreement pin — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the plan offered "extend the test or add a sibling in the same file" — a sibling was chosen: the existing test's three subtests are per-scenario with a denied approval, while this one is a per-tool table that must ALLOW the call to reach a result string.
NOTES (2026-08-13): the redirect is a leaf symlink pointing INSIDE the workspace (`docs/notes.md` → `store/notes.md`), not out of it as in the existing symlinked-directory subtest — an out-of-workspace target refuses at the tools' own fence, so no success sentence would exist to compare against, and the resolution divergence (not the fence) is what this item pins. Ask-Before is the mode for the same reason: it gates every write, so the approval carrier is populated while the call still executes.

Depends on item 1.

**What:** `TestResolvedPathRidesTheCallAndTheApproval`
(`internal/agent/resolvedpath_test.go:19`) drives only `write_file` (the `path` key), so
nothing pins that the approval pane and the result string agree on the resolved path for the
`destination`-keyed tools. Extend the test (or add a sibling in the same file) to drive
`copy_file` and `move_file` (resolved `destination`) and `delete_file` (resolved `path`):
assert `domain.ApprovalRequest.ResolvedPath`, `domain.ToolCallEvent.ResolvedPath`, and the
tool's result string (`file_ops.go:157` "copied … resolves to", `:206` "moved …", `:363`
"deleted …") all carry the same `filepath.EvalSymlinks`-resolved target. The file's existing
`realPath` helper (`resolvedpath_test.go:108`) is the oracle. Test-only — no production code
changes.

**Files:** `internal/agent/resolvedpath_test.go`

**Tests:** the item IS the test (above).

**Acceptance:** `go test ./internal/agent/`

**Commit:** `test(agent): pin pane/result path agreement for copy, move, and delete`

---

## 9. Writer-disclosure test roots survive a symlinked temp dir — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the item's enumeration of affected roots is incomplete — proved by running the
suite with `TMPDIR` pointed at a symlinked directory, which is also how the fix was verified. Three
further roots in the same class (bare-sentence disclosure assertion over a raw `t.TempDir()`) failed
and were switched too: `write_file_test.go:61` (`TestWriteFile_Execute_ReportsBytesWritten`),
`read_file_test.go:166` (`…_ReportsTheSpanItRendered`) and `:234` (`…_LocatesASubstring`). Leaving
them would have missed the item's own goal.
NOTES (2026-08-13): the two `read_file_test.go` roots the item names by their bare-sentence lines
(`:495`, `:531`) already resolved symlinks inline as `realPath(t, t.TempDir())`; they now call
`tempRoot(t)`, which is the same expression behind the helper's name. `file_edit_test.go:298`
(`TestEditExistingFile_ToolErrors`) carries no disclosure sentence, but the item names the line, so
it was switched as written — behaviour-neutral.

Depends on item 1.

**What:** The bare-sentence (no-note) disclosure assertions build workspace roots from raw
`t.TempDir()`; on a host whose temp dir is reached through a symlink (macOS `/tmp`) the writer
would emit a resolution note and the exact-string assertions break. Add `tempRoot(t)` beside
`realPath` in `internal/tools/path_safety_test.go` (design call 6) and switch the roots of the
affected tests to it: `file_ops_test.go:850`, `:887`, `:924` (bare sentences at `:877`, `:913`,
`:948`), `write_file_test.go:97` (bare sentence `:134`), `file_edit_test.go:217`, `:298` (bare
sentence `:263`), plus the same shape in `find_replace_test.go` (bare sentence `:122`) and
`read_file_test.go` (`:495`, `:531`). Test-only — no production code changes; do not touch
tests whose assertions already route through `realPath`.

**Files:** `internal/tools/path_safety_test.go`, `internal/tools/file_ops_test.go`,
`internal/tools/write_file_test.go`, `internal/tools/file_edit_test.go`,
`internal/tools/find_replace_test.go`, `internal/tools/read_file_test.go`

**Tests:** the item IS the hardening; the whole `internal/tools` suite stays green.

**Acceptance:** `go test ./internal/tools/`

**Commit:** `test(tools): disclosure test roots resolve symlinks like the writers do`

---

## 10. `internal/domain/doc.go` names `ApprovalScoper` — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the added clause pushed the sentence past the file's ~95-column comment width,
so the `tools.go`/`toolsummary.go`/`confinement.go` sentences were re-wrapped across the same
lines. Wording and order are untouched — only the line breaks moved (the pre-existing 119-column
line 59 is gone as a side effect).

Depends on item 1.

**What:** The `tools.go` sentence in the package map (`internal/domain/doc.go:57-60`)
enumerates the marker interfaces — `ReadOnlyTool, SubprocessTool, ExternalEffectTool,
ReadSourceTool, PromptTool` — without `ApprovalScoper` (`internal/domain/tools.go:146`). Add it
to the enumeration with a short qualifier that it is read on the approval path rather than by
the dispatch disposition (the scout's note). One-sentence edit; nothing else in the file moves.

**Files:** `internal/domain/doc.go`

**Tests:** `TestDocMapNamesEveryFile` (`docmap_test.go:12`) stays green (it checks file names,
not sentences — informational).

**Acceptance:** `go build ./... && go test ./internal/domain/`

**Commit:** `docs(domain): the marker-interface map names ApprovalScoper`

---

## 11. `SetSampling` merges field-wise

Depends on item 1.

**What:** `Request.SetSampling` (`internal/domain/hooks.go:589-592`) replaces `r.sampling`
wholesale, so a hook setting only `Temperature` nils the `MaxTokens` the loop stamped at
`internal/agent/loop.go:693` and `:1056` (ADR 0046) — silently un-capping the reply. Make it
the field-wise merge the contract at `hooks.go:619-620` already promises (design call 2):
non-nil `Temperature`/`MaxTokens` overwrite, nil fields leave the current value untouched,
`revision++` unconditional, no clearing surface. Also refresh the stale doc comment at
`:587-588` ("no current Mechanism mutates these" — the loop and `internal/agent/compact.go:319`
now do) to state the merge semantics.

**Files:** `internal/domain/hooks.go`, `internal/domain/hooks_test.go`

**Tests:**
- New: stamp `MaxTokens`, then `SetSampling` with only `Temperature` — the drained
  `State().Sampling` keeps both; the mirror case (stamp `Temperature`, set only `MaxTokens`)
  likewise.
- Existing stays green unmodified: `TestRequestSetToolsAndExtraAndSampling`
  (`hooks_test.go:173`), the budget suite (`internal/agent/budget_test.go` —
  `TestPreRequestHookBeatsTheOutputCap` at `:180` still passes: the hook's non-nil `MaxTokens`
  overwrites the loop's under merge semantics exactly as under replacement).

**Acceptance:** `go build ./... && go test ./internal/domain/ ./internal/agent/`

**Commit:** `fix(domain): SetSampling merges field-wise so a partial set cannot un-cap the reply`

---

## 12. `ISSUES.md`: remove the ten closed entries and fix the stale template citation

Depends on items 2–11.

**What:** Remove from `ISSUES.md` the ten `[P]`-marked entries this plan closed: the
`SetSampling` open defect (item 11); the hostile-bytes residuals for `isPathName` reuse
(item 6), the `rm` rules blind spot (item 2), the path-agreement test (item 8), the
symlinked-tempdir test roots (item 9), the `stripEscapes`/`flattenField` tab entry (item 7),
and the `domain/doc.go` marker entry (item 10); the API-key residuals for README (item 5), the
keystore stderr redaction (item 3), and the migration-notice wording (item 4). Also in the
API-key residuals section: remove the stale-citation entry after confirming no remaining text
says `cmd/apogee/defaults/` (the real home is `internal/config/defaults/`), and remove the
"untracked plan doc `2026-08-13 - 03`" entry — item 1 verified that plan is committed and
archived. Nothing else in the file moves; the closed trail is the per-item `CHANGELOG.md`
entries the run's verifiers wrote (house rule: `ISSUES.md` holds open work only).

**Files:** `ISSUES.md`

**Tests:** none (docs-only).

**Acceptance:** `grep -c "cmd/apogee/defaults" ISSUES.md` returns 0,
`grep -c "SetSampling" ISSUES.md` returns 0 outside the parked request-side-knobs entry's
cross-reference (update that cross-reference to say the merge has landed), and
`git diff --stat` for the item touches only `ISSUES.md`.

**Commit:** `docs(issues): close the residuals-fix-wave entries`

---

**Suggested version bump:** items 2–5 are user-facing fixes (a security-floor tightening, a
secret-redaction fix, doc coverage, honest notice wording) and item 11 hardens a documented
API contract; per the house per-feature micro-bump policy one micro-bump after execution is
warranted — the owner decides; no plan item changes `VERSION`.

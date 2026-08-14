# ISSUES sweep plan — the ratified 9-defect batch

**Goal:** close the owner-ratified set of open defects out of `ISSUES.md` — the /server
entry-identity defect, the unreachable permitted branch for a workspace-internal symlink escape,
the vestigial `verifiedEntrySplice` parameter, four stale cross-file doc references, the Windows
home in the dangerous-rule patterns, the input-width mirror's partial sanitizer parity, the
`tempRoot` test inconsistency, and the two over-limit `internal/tools` files — then prune the
closed entries per the house convention (ISSUES holds open work only; the changelog is the
closed trail).

**Date:** 2026-08-14
**Status:** not started
**Sized for:** ~200k-context host

**Authoritative sources:**
- `ISSUES.md` "Run residuals — open" sections (2026-08-13 and both 2026-08-14 sections) — the
  defect statements with file:line evidence.
- ADR 0049 + `docs/design/confinement-execution-contract.md` §4 ("The approved escape executes")
  — the permit model item 2 extends.
- ADR 0036 (server entries; decision 1: an alias is the bound entry's own name) — item 1's
  identity rule.
- ADR 0043 — file-split-by-concern precedent and the ~400-line smell threshold, item 8.
- `internal/security/doc.go` — the dangerous-action guard is "NOT a security boundary" (bounds
  item 5's over-match tolerance).

**Ratified design calls** (owner via AskUserQuestion, 2026-08-14):
1. **Scope:** this 9-defect sweep; the restream hold-off cancel seam and the /server
   key-source overlay are deferred (stay in ISSUES.md); the parked url-safety config key stays
   parked.
2. **/server identity is the entry NAME:** all five current-entry comparisons switch from
   endpoint equality to `choice.Name == m.opts.HostAlias`; picking a same-endpoint sibling entry
   performs a real switch.
3. **Symlink-escape route is permit-match-first:** `openMutationRoot` routes to the permitted
   branch when the input's resolved path equals the permitted target, BEFORE the lexical
   in-workspace branch; with no permit, behavior is byte-identical to today.
4. **tempRoot conversion covers the write-family suites only** (~60 sites); incidental roots
   (registry, terminal, git, python, grep, …) stay raw; the when-to-use rule lands in
   `tempRoot`'s doc comment.
5. **Windows-home mechanism is a normalize fold:** `normalize` folds `\` → `/` so every rule
   gains separator robustness; the anchors then add only the forms folding does not cover
   (`%userprofile%`).

**Standing requirements:**
- skills: coding-standards
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- The restream hold-off cancel seam (`internal/agent/loop.go:315`) and the /server
  startup-entry key-source overlay (ADR 0036 decision 6) — deferred by owner call, their
  ISSUES.md bullets stay.
- The parked url-safety config key (AllowHosts/DenyHosts surfacing).
- Blanket `t.TempDir()` conversion outside the write-family suites.
- Any VERSION / CHANGELOG-release-heading change (see the closing note).

---

## 1. /server identifies the session's entry by name, not endpoint — ✅ DONE (2026-08-14)

NOTES (2026-08-14): the item lists five comparison sites by pre-item line number; located by
function, they are picker.go `currentServerRow`/`switchToServer`/`serverRows` and settings.go
`settingsSwitchServer`/`settingsCurrentValue` — the same five, at picker.go:325/366/1036 and
settings.go:700/1697 in the current tree.
NOTES (2026-08-14): beyond the four doc sites the item names, one paragraph of `switchToServer`'s
own doc comment gained the sibling-entry rule, since that function is where the already-on branch
now means "the same ENTRY" rather than "the same URL".

**What:** Replace endpoint equality with entry-name equality at the five sites that decide
"which configured entry is the session on": `switchToServer`'s already-on branch
(`internal/tui/picker.go:366`), `currentServerRow` (`picker.go:325`), `serverRows`' current-row
mark (`picker.go:1035`), `settingsSwitchServer`'s already-on branch
(`internal/tui/settings.go:609`), and `settingsCurrentValue`'s `(current)` mark
(`settings.go:1560`). The comparison is `choice.Name == m.opts.HostAlias` (or the local
equivalent): `HostAlias` is exactly the bound entry's name — set at launch
(`internal/config/config.go:2052`), kept in sync by `foldServerSwitch`
(`internal/tui/model.go:2019-2022`) and the pre-bound bind (`internal/tui/prebound.go:183`), and
the ephemeral-override start synthesizes its row with `Name: opts.HostAlias`
(`cmd/apogee/upstream.go:291-302`), so a name match holds on every start shape. Consequence
(ratified call 2): picking a same-endpoint sibling entry no longer takes the already-on branch —
it performs the full switch, so the key source rebinds and the recorded pin is truthful. Update
the doc prose that states the endpoint contract: `internal/tui/tui.go:905-916` (`ServerChoice`),
`internal/tui/doc.go:433-435`, `picker.go:320-322`, `settings.go:1552-1556`.
**Files:** internal/tui/picker.go, internal/tui/settings.go, internal/tui/tui.go,
internal/tui/doc.go, internal/tui/picker_test.go, internal/tui/settings_test.go
**Tests:** new coverage for two configured entries sharing one endpoint — picking the *other*
entry performs a switch (SwitchServer called with that name, pin records that name) and the
current-row mark sits only on the bound entry's row; re-picking the bound entry still takes the
already-on branch and records the pin. Existing suites
(`TestServerNamingTheActiveServerRecordsThePin` `picker_test.go:811`,
`TestServerCommandAnswersWithoutSwitching` `:973`,
`TestSettingsServerRowSaysWhenItIsAlreadyOnTheChosenServer` `settings_test.go:904`,
`TestServerPickerListsTheConfiguredServers` `picker_test.go:635`) updated where they pinned
endpoint identity.
**Acceptance:** `go build ./internal/tui/ && go test ./internal/tui/ -run 'Server|Settings'`
**Commit:** `fix(tui): the session's server entry is identified by name, not endpoint`

## 2. A permitted escape through a workspace-internal link reaches its target — ✅ DONE (2026-08-14)

NOTES (2026-08-14): the existing `TestSafeWriteFile_PermitLeavesWorkspaceWritesUnchanged` asserted the old (now-fixed) behaviour for its symlinked-hop half — that half was re-pointed at a hop the permit does NOT name, which is the floor case that remains a refusal; the landing case moved to the new `TestSafeWriteFile_PermittedTargetThroughAWorkspaceLink`, and the "identical to no-permit" comparison the item asked for was added to the floor test.
NOTES (2026-08-14): one file beyond the item's list — `internal/tools/path_safety.go` (doc-only): `escapeTargetPin`'s doc claimed its workspace branch was checked first and unconditional "exactly as security's mutation root checks it", which this item's routing change falsified. Repaired to name the resolved-path equality both sides route on; no code change.

**What:** In `openMutationRoot` (`internal/security/writepermit.go:62-75`), check the permit
BEFORE the lexical in-workspace branch (ratified call 3): when `permitted != ""`, join `input`
to `root` exactly as `openPermittedRoot` does and, if `EvalRealPath(named) ==
filepath.Clean(permitted)`, delegate to `openPermittedRoot`; otherwise fall through to today's
branches unchanged. This makes the Gate's Allow executable for a workspace-internal symlink
whose target lies outside the workspace: the write lands on the disclosed resolved target
through the permitted ancestor root, never through the link. The floor holds by construction —
permits are minted only for escape targets (`classifyWriteTarget`,
`internal/agent/dispatch.go:828`), so an unpermitted call and any in-workspace target that is
not the disclosed escape resolve as today. Binding semantics, stated so the tests pin them: the
whole family lands on the *disclosed* target uniformly — including `delete_file`, which for this
shape now removes the resolved outside target (what the approval pane showed) rather than the
in-workspace link, leaving the link dangling. Rewrite `openMutationRoot`'s doc comment (the
"checked FIRST and is unconditional" paragraph) to the refined rule: the lexical branch stays
unconditional for every call except the one whose resolved path IS the permitted target. Amend
the contract's §4 escape block (`docs/design/confinement-execution-contract.md`, block at
`:591`) to state the permitted branch is decided by resolved-target match and is reachable for
a workspace-internal link (this item owns that amendment; the `:622` reads sentence belongs to
item 4).
**Files:** internal/security/writepermit.go, internal/security/writepermit_test.go,
internal/tools/write_permit_test.go, docs/design/confinement-execution-contract.md
**Tests:** in `writepermit_test.go`: a workspace-internal symlink to an outside target with the
matching permit — `SafeWriteFile` lands the bytes at the resolved target and the link is
untouched; the same shape with a wrong or absent permit is refused exactly as today;
`SafeRemove` with the matching permit removes the outside target, not the link; an in-workspace
regular-file write under an unrelated escape permit behaves identically to no-permit (the
never-worse floor, extending `TestSafeWriteFile_PermitLeavesWorkspaceWritesUnchanged:158`). In
`internal/tools/write_permit_test.go`: one per-verb case for the link shape driven through the
existing `escapeCases()` table (`:58`).
**Acceptance:** `go build ./internal/security/ ./internal/tools/ && go test ./internal/security/ -run 'Permit|SafeWrite|SafeRemove|SafeCopy' && go test ./internal/tools/ -run 'Escape|Permit'`
**Commit:** `fix(security): a permitted escape through a workspace-internal link reaches its target`

## 3. Drop verifiedEntrySplice's vestigial data parameter — ✅ DONE (2026-08-14)

NOTES (2026-08-14): the item's doc sweep named three neighbouring comments; two of them —
`keySourceNoun` (`configwrite_keysource.go:42`) and `spliceEntrySetting`'s closing sentence
(`configwrite.go:405`) — narrate only the noun the caller passes and the shape the gate catches,
both unaffected by the signature, so they were read and left byte-identical. The third
(`serverEntryAt`, `:157-158`) was sharpened to "the sole before-state", and the comment that DID
narrate the old shape — `verifiedEntrySplice`'s own, which said the result "must agree with the
original" while the function also received the original bytes — was rewritten to name `before` and
to state that every comparison is between parsed states.

**What:** Remove the unused first parameter `data []byte` from `verifiedEntrySplice`
(`internal/config/configwrite_keysource.go:291`) — the body reads only `updated`/`before`/
`at`/`want`/`what`, and `before` already carries the parsed before-state. Update the three call
sites (`configwrite_keysource.go:134`, `:153`, `internal/config/configwrite.go:392`) and the
direct test call (`configwrite_keysource_test.go:321`), and sweep the neighbouring doc comments
that narrate the old shape (`configwrite_keysource.go:42`, `:157-158`, `configwrite.go:405`).
**Files:** internal/config/configwrite_keysource.go, internal/config/configwrite.go,
internal/config/configwrite_keysource_test.go
**Tests:** existing suite only — `TestVerifiedEntrySpliceNamesWhatTheEditFailedToPlace`
(`configwrite_keysource_test.go:309`) compiles against the new signature and passes.
**Acceptance:** `go build ./internal/config/ && go test ./internal/config/`
**Commit:** `refactor(config): drop verifiedEntrySplice's vestigial data parameter`

## 4. Cross-file doc references name their file and function — ✅ DONE (2026-08-14)

NOTES (2026-08-14): item text lists `configwrite.go`, `configwrite_scalarsplice.go` and
`configwrite_keysource.go` as where "the splices below" moved. The tree contradicts two thirds of
that: the key-source entry edits deliberately do NOT start from `ReadConfigForWrite` (they read
through `readConfigForEntryEdit`, whose own doc says so), and `configwrite_scalarsplice.go` holds a
text-block helper rather than a writer. Naming those would have reinstated the same class of
misleading reference the item repairs, so the replacement names the four writers that do start from
this read — `SaveConfigSetting`/`ResetConfigSetting` (configwrite_scalar.go), `SaveMechanismSetting`
(configwrite_mechanism.go), `SaveServerEntrySetting` (configwrite.go) — plus the key-source
exception.
NOTES (2026-08-14): the item's `go build ./internal/config/` guard could not run against the live
tree: a concurrent item-3 dispatch has `internal/config/configwrite.go` + `configwrite_keysource.go`
mid-edit there (`verifiedEntrySplice` signature changed, one call site not yet updated), which is
another item's work and was left untouched. The guard was run instead on a throwaway worktree at
HEAD carrying only this item's three config diffs — BUILD_OK — and `gofmt -l` is clean on all three.

**What:** Repair the four positional references left stale by earlier file splits, under the
same rule the prior repair item used — name the file, and the function where one is meant:
`internal/config/configsplice.go:232` ("the state every splice below starts from" → the splices
now live in `configwrite.go`, `configwrite_scalarsplice.go` and `configwrite_keysource.go`);
`internal/config/configwrite_scalarsplice.go:50` ("the flow-style list refusal above" →
`spliceHostAcknowledgement` in `configwrite.go`); `internal/config/configwrite_scalar.go:96`
("the seeding read below" → `ReadConfigForWrite` in `configsplice.go`); and
`docs/design/confinement-execution-contract.md:622` — extend "Reads are not widened:
`security.SafeReadFile` takes no permit and none may be added" to also name the read the
read-modify-write verbs DO perform through the permitted parent (`readWriteTarget` /
`statWriteTarget`, `internal/tools/path_safety.go:139`, `:153` — their doc comments carry the
rationale), so a reader cannot infer that an approved `file_edit` is unable to read its own
target. Wording stays consistent with item 2's §4 amendment (item 2 owns the escape-block
change; this item owns only the `:622` sentence).
**Files:** internal/config/configsplice.go, internal/config/configwrite_scalarsplice.go,
internal/config/configwrite_scalar.go, docs/design/confinement-execution-contract.md
**Tests:** none (comment/prose only) — `go build ./internal/config/` as a compile guard.
**Acceptance:** `go build ./internal/config/`
**Commit:** `docs(config): cross-file references name their file and function`

## 5. Dangerous-rule patterns recognise the Windows home — ✅ DONE (2026-08-14)

NOTES (2026-08-14): the item left the shared-anchor extraction to the implementer — both anchors
were extracted, `homeAnchor` (the three write-* rules) and `deleteTargetAnchor` (the two rm rules),
since each was byte-identical across its users and this item edits every copy. The anchor-mechanics
prose moved from the rule comments onto the constants; the rule comments keep their
precision/tier rationale and now point at the constant.
NOTES (2026-08-14): `rm -rf C:\Users\alice` folds to `rm -rf c:/users/alice`, which the rm anchor
could not match at all — its alternation starts at the character after the flags, and `c` is not
`/`. Recognising the Windows home there therefore needed one branch beyond `%userprofile%`: a drive
root `[a-z]:/`, the Windows spelling of the bare `/` "any absolute target" branch. Without it the
item's own required test case (`rm -rf C:\Users\alice` trips the rm rules) cannot pass.
NOTES (2026-08-14): two doc comments beyond the item's list were corrected because the fold
falsified them — `Rule.Pattern`'s authoring contract (`dangerous.go`), which told rule authors the
text is only "whitespace-collapsed and lower-cased", and `DefaultDangerousRules`' header sentence
naming the same normalized shape. Prose only.

**What:** Two changes (ratified call 5). (a) `normalize`
(`internal/security/dangerous.go:294-296`) additionally folds `\` → `/` (alongside the existing
lowering and whitespace collapse), so `c:\users\alice\.ssh` matches the existing
`/users/[^/\s]+` anchor as a substring; extend `normalize`'s doc comment (`dangerous.go:288-291`)
— the fold exists for Windows-path matching, and the guard remains deliberately not
obfuscation-resistant. (b) Add `%userprofile%` — the one home form folding does not produce —
to the home-anchor alternation: the byte-identical anchor
`(?:~|/home/[^/\s]+|/users/[^/\s]+|/root|\$home)` shared by `write-ssh-keys`
(`internal/security/rules.go:65`), `write-credential-persistence` (`rules.go:75-76`) and
`write-apogee-control-plane` (`rules.go:114`), and the different anchor shared by the two rm
rules (`rules.go:40-41`, `:48-49`). Patterns stay lower-case (the haystack is pre-lowered).
Whether the shared anchor is extracted into a named constant is the implementer's call under the
forwarded standards.
**Files:** internal/security/dangerous.go, internal/security/rules.go,
internal/security/dangerous_test.go, internal/security/rules_test.go
**Tests:** a Windows block in `rules_test.go` mirroring the macOS `/Users/<name>` block
(`rules_test.go:285-305`): `C:\Users\alice\.ssh\authorized_keys` write trips `write-ssh-keys`;
`%USERPROFILE%\.npmrc` write trips `write-credential-persistence`;
`C:\Users\alice\.apogee\config.yaml` trips `write-apogee-control-plane`;
`rm -rf C:\Users\alice` and `rm -rf %userprofile%` trip the rm rules. A `normalize` table case
in `dangerous_test.go` pinning the backslash fold. A negative case showing an ordinary
non-path backslash string does not trip a home rule.
**Acceptance:** `go build ./internal/security/ && go test ./internal/security/ -run 'Rules|Dangerous|Normalize'`
**Commit:** `feat(security): dangerous-rule patterns recognise the Windows home`

## 6. The input-width mirror matches the widget sanitizer in full — ✅ DONE (2026-08-14)

NOTES (2026-08-14): one file beyond the item's list — `internal/tui/chromelayout.go` (doc-only): the
`inputContentRows` contract named `expandInputTabs` by name, which the item's authorized rename
(`sanitizeInputLine`) falsified; repaired to the new name and its widened rule. No code change.
NOTES (2026-08-14): the item asks for oracle cases "carrying `utf8.RuneError`", and the obvious ones
pass against the OLD code by coincidence — a kept `U+FFFD` is one cell wide, so a row still holds the
same number of runes and the row starts agree. The RuneError cases were therefore chosen for shapes
where the extra rune actually moves a boundary (a space-group break past it, and a word too wide for
the row); each new case was verified to FAIL with the drop rule disabled and pass with it on.

**What:** Extend `expandInputTabs` (`internal/tui/inputaccent.go:286`) into a full per-line
mirror of the widget's default `runeutil.NewSanitizer()` (bubbles v2.1.0,
`internal/runeutil/runeutil.go:26-29`, `:56-95`): drop `utf8.RuneError`, drop control runes
other than `\t`, and expand `\t` to `inputTabCells` spaces as today. `\r`/`\n` need no per-line
handling — the widget folds `\r` → `\n` BEFORE splitting into logical rows
(`textarea.go:504`, `:519-529`), so neither can appear inside a line handed to the mirror;
record that argument where the function's contract lives rather than only at
`internal/tui/mouse.go:254-261`. Rename the function to match its widened job (mechanical,
implementer's choice under the standards) and update its caller `wrapRowStarts`
(`inputaccent.go:222`) and the contract paragraph at `:212-218`.
**Files:** internal/tui/inputaccent.go, internal/tui/inputaccent_test.go,
internal/tui/mouse.go, internal/tui/render_test.go
**Tests:** extend `TestWrapRowStartsMirrorsTheWidget` (`inputaccent_test.go:79`) — whose oracle
reads runes back off the widget, so parity is asserted rather than assumed — with cases
carrying `utf8.RuneError` and a non-tab control rune (e.g. `\x07`); extend the
`TestInputContentRowsMirrorsTheWidget` table (`render_test.go:5133`) with one such case.
**Acceptance:** `go build ./internal/tui/ && go test ./internal/tui/ -run 'WrapRowStarts|InputContentRows|InputCellSpans|CellToRuneOffset'`
**Commit:** `fix(tui): the input-width mirror matches the widget sanitizer in full`

## 7. Write-family test suites adopt the symlink-resolved temp root — ✅ DONE (2026-08-14)

NOTES (2026-08-14): the item's line numbers are pre-item-2 evidence, so the sites were located by
grep over each named file rather than by offset; the converted set is exactly the item's — 12 in
`path_safety_test.go` (`:61`, `:90-91`, and the ten between `:145` and `:381`; `tempRoot`'s own body
keeps `t.TempDir()`, being what it wraps), 12 in `write_permit_test.go` (verified against
`41cfb1d^` to be the same 12 that stood inside the pre-item-2 `:233-335` `escapeFixtures` group —
item 2 added no new root), 4 / 5 / 9 / 19 / 12 in the remaining five, matching the item's counts
exactly. 73 roots in total, against the ratified call's "~60" estimate.

**What:** (Ratified call 4.) Convert the raw `t.TempDir()` roots in the write-family suites —
where a future exact-string assertion on a tool's success sentence is plausible — to `tempRoot`
(`internal/tools/path_safety_test.go:131`): `write_permit_test.go` (`:233-335`, the
`escapeFixtures` group), `write_file_test.go` (`:16`, `:40`, `:146`, `:182`),
`file_edit_test.go` (`:26`, `:50`, `:90`, `:129`, `:163`), `find_replace_test.go` (`:47`,
`:129`, `:178`, `:209`, `:234`, `:266`, `:298`, `:322`, `:359`), `file_ops_test.go` (the 19
raw sites incl. `extraRootFixture:247`), `read_file_test.go` (the 12 raw sites incl.
`escapesUnderComponentSwap:40`), and `path_safety_test.go`'s own raw sites (`:61`, `:90-91`
`scopeFixture`, `:145-381`). Line numbers are pre-item-2 evidence — locate by function, not
offset. Incidental roots (registry, terminal, git, python, grep, find_files, list_dir, diff,
network, exec, present_document, workspace_scoped, sub_agent) stay raw. Sharpen `tempRoot`'s
doc comment into the package rule: suites whose paths can reach a bare success sentence or the
safety fence use `tempRoot`; incidental workspace roots need not.
**Files:** internal/tools/path_safety_test.go, internal/tools/write_permit_test.go,
internal/tools/write_file_test.go, internal/tools/file_edit_test.go,
internal/tools/find_replace_test.go, internal/tools/file_ops_test.go,
internal/tools/read_file_test.go
**Tests:** the converted suites themselves — behavior-neutral by construction.
**Acceptance:** `go test -race -count=1 ./internal/tools/`
**Commit:** `test(tools): write-family suites adopt the symlink-resolved temp root`

## 8. Split path_safety.go and file_ops.go by concern — ✅ DONE (2026-08-14)

NOTES (2026-08-14): two doc references beyond the item's file list were repaired because the moves falsified them — `file_ops.go`'s header cited `path_safety.go` as `readScope`'s home (now `path_read.go`), and `internal/agent/resolvedpath_test.go`'s `TestResolvedPathAgreesWithTheResultForEveryWriteKey` comment cited `file_ops.go` alone for three writers' success sentences (now also `delete_file.go`). Prose only, no code change.
NOTES (2026-08-14): the header paragraph narrating delete_file sat inside the item's "keep lines 1–309" range of `file_ops.go`; it moved with the tool and became `delete_file.go`'s file doc, its first sentence re-anchored to stand alone ("joins them as the family's remove-bytes half" → named beside the move-bytes half). Leaving it behind would have left `file_ops.go` narrating a tool it no longer holds — the stale-reference class item 4 exists to repair.
NOTES (2026-08-14): `doc.go`'s two group counts were corrected alongside the map entries — "Four files register no tool" → "Five" (falsified by `path_read.go`), and "Twenty-two files carry the built-ins" → "Twenty-nine". The latter was already stale by six before this item (the tree holds 28 carriers, 29 with `delete_file.go`), and it is the same sentence whose "three-tool file_ops.go" clause the item requires reworded to "two-tool".

**What:** Two mechanical splits back under the ~400-line threshold (ADR 0043: a smell
threshold, not a rule — but both files are tracked in ISSUES.md as split candidates).
`internal/tools/path_safety.go` (407): keep lines 1–192 — the security aliases, the fenced
primitives, and the ADR-0049 approved-escape section — and move the read side (lines 193–407:
`workspaceRelative`, `readWorkspaceFileBounded`, `readAllBounded`, `readFileErrorMessage`,
`escapeOrMessage`, the `readScope` family, `matchRoot`, `rootUsable`) into a new
`internal/tools/path_read.go` with a file doc stating its concern.
`internal/tools/file_ops.go` (418): keep copy+move and their shared schema/args/pre-flight
(lines 1–309) and move the delete family (lines 310–409: `deleteFileSpec`, `deleteFileArgs`,
`DeleteFile`, `checkDeletePath`) into a new `internal/tools/delete_file.go`, matching the
tool-per-file convention (`write_file.go`, `file_edit.go`); split the trailing compile-time
assertion block (`:410-418`) per type. Pure moves — no signature or behavior change. Update
`internal/tools/doc.go`'s file map (`docmap.Check` fails on any unlisted file): add the two new
files, reword the `path_safety.go` description (`doc.go:200-215`) and the "three-tool
file_ops.go" wording (`doc.go:150`, `:160-163`). Line ranges are pre-items-2/7 evidence —
carve by declaration list, not offset.
**Files:** internal/tools/path_safety.go, internal/tools/path_read.go,
internal/tools/file_ops.go, internal/tools/delete_file.go, internal/tools/doc.go
**Tests:** existing suite only — the docmap test and the full package prove the moves.
**Acceptance:** `go build ./internal/tools/ && go test -race -count=1 ./internal/tools/`
**Commit:** `refactor(tools): split path_safety.go and file_ops.go by concern`

## 9. Close the swept entries out of ISSUES.md — ✅ DONE (2026-08-14)

NOTES (2026-08-14): `CHANGELOG.md` is in the item's Files list but is untouched — the item edits it
only to fill a gap, and the `[Unreleased]` section already carries an entry for all eight closed
items (Added: the Windows-home guard; Changed: the `internal/tools` file split, the `tempRoot` test
conversion, the cross-file doc references, `verifiedEntrySplice`, the contract's escape section;
Fixed: `/server` name identity, the permitted symlink escape, the prompt-box width mirror).
NOTES (2026-08-14): removing the four bullets of "Run residuals — open (2026-08-14)" emptied that
section, so its heading and its intro paragraph went with them, per the item's "a section left empty
is removed entirely".

**What:** Depends on items 1–8. Per the house convention (ISSUES holds open work only; the
changelog is the sole closed trail): remove from `ISSUES.md` the bullets items 1–8 closed —
from "Run residuals — open (2026-08-13)" the Windows-home bullet and the input-width-mirror
bullet; from "Run residuals — open (2026-08-14)" the `/server` endpoint-match bullet, the
`verifiedEntrySplice` bullet, the cross-file doc-references bullet and the `tempRoot` bullet;
and the whole "Run residuals — open (2026-08-14, approved-escape write)" section (all three
bullets: the symlink-escape write, the contract `:622` sentence, the file splits). The
2026-08-13 section KEEPS its two deferred bullets (the `/server` key-source overlay and the
restream hold-off cancel seam) — deferred by owner call 1, not closed. A section left empty is
removed entirely. Verify `CHANGELOG.md` `[Unreleased]` carries an entry for each of items 1–8
(the per-item verifiers write them; this item only fills a gap if one slipped) — no release
heading is added.
**Files:** ISSUES.md, CHANGELOG.md
**Tests:** none (register pruning) — inspection against the item list is the check.
**Acceptance:** `grep -c 'verifiedEntrySplice\|expandInputTabs\|already-on early return' ISSUES.md` returns 0 matches; the two deferred bullets still present (`grep -c 'restreamHoldoff' ISSUES.md` returns 1).
**Commit:** `chore(issues): close the swept entries out of ISSUES.md`

---

**Suggested version bump:** v0.14.2 (micro) — user-visible fixes (/server identity, the
permitted symlink escape, Windows-home guard coverage) with no new surface. The bump is the
owner's call; no item changes VERSION or the changelog heading.

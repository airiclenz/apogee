# ISSUES residuals — best-fitting open items

**Goal:** close the plan-fit items from `ISSUES.md`: the mechanical run residuals (stale
comments/prose, the `\r` fold gap, test hardening, helper relocation), the two small feature
gaps (`/model`–`/server` re-pin; the auto-title entry, which turned out to be stale and is
retracted), and the `configwrite_scalar.go` split.

**Date:** 2026-08-14 · **Status:** unexecuted · **sized for:** ~200k-context host

**Authoritative sources:** `ISSUES.md` at commit `ffc559e` (the entries each item quotes);
code facts pinned at the same commit. Where an ISSUES entry disagrees with the code as found,
the code as found governs and the deviation lands as a dated NOTES line under the item.

**Ratified design calls (owner, 2026-08-14, via AskUserQuestion):**

- Scope: all three clusters — mechanical residuals, small feature gaps, scalar split — are in.
- Re-pin fixes **both twins**: `/model` (`bindPickedModel`) and `/server` (`switchToServer`).
- The auto-title ISSUES entry is **retracted as stale** (live-apply already ships via the
  renderer-owned key path); no dispatcher case is added.
- `configwrite_scalar.go` splits into **two files, pure move**: the writer core stays, the
  targeting/text-block/insertion machinery moves to a new `configwrite_scalarsplice.go`.
- `verifiedEntrySplice`'s refusal takes a **caller-passed noun**: key-source writers keep
  "the key source", `setEntrySetting` names the actual key being written.

**Standing requirements:** skills: coding-standards. Every item that resolves an `ISSUES.md`
entry removes that entry in the same item (this repo's register holds open work only) and
records the close under `CHANGELOG.md` `[Unreleased]`.

**Out of scope (deliberately):** the approved-out-of-workspace-write defect (pending owner
call); the Windows-home ssh-key/credential patterns (declared ADR 0020 debt); the
restream-holdoff cancel seam (cancel-semantics grill); the input-width mirror `\r`/`\n` gap
(unreachable by construction); the `/server` early-return matching on **endpoint** rather than
entry name (two entries sharing an endpoint both take the early return — a distinct quirk
noticed while scouting item 7; a candidate for its own ISSUES entry at closeout, not fixed
here); any change to `VERSION` or a `CHANGELOG` release heading.

---

## 1. Drop the stale rejection count from the prefix-once comments — ✅ DONE (2026-08-14)

NOTES (2026-08-14): the two comment edits were already present in the working tree from a prior
attempt at this item (dispatch `RETRY: yes`); they match the item's text exactly and were kept.
`ISSUES.md` was restored (`git checkout --`) per the dispatch DECISION and only this item's bullet
was removed. `internal/tui/render_test.go` is item 2's work and was left untouched.

**What:** `internal/agent/construct.go:273` and `internal/agent/enable_mechanisms_test.go:88`
both say "Add's three rejections already carry the `apogee: ` prefix"; `MechanismRegistry.Add`
(`internal/domain/mechanism.go:236–254`) now has four gates (empty ID, reserved sentinel,
duplicate ID, no hook interface). Drop the count from both comments — say "Add's rejections" —
rather than writing "four", so the next gate does not re-stale them. The substantive claim
(every rejection arrives prefixed, so the enable path appends context rather than wrapping)
stays as written. Remove the matching ISSUES.md bullet ("Two comments still count
`MechanismRegistry.Add`'s gates as three…", small-guards run); CHANGELOG `[Unreleased]` gets
one line for the comment fix.

**Files:** `internal/agent/construct.go`, `internal/agent/enable_mechanisms_test.go`,
`ISSUES.md`, `CHANGELOG.md`

**Tests:** none new — comment-only change; the existing
`TestEnableMechanisms_MergeRejectionCarriesOnePrefix` still passes.

**Acceptance:**
- `grep -rn "three rejections" internal/agent/` → no matches
- `go test ./internal/agent/ -run TestEnableMechanisms_MergeRejectionCarriesOnePrefix`

**Commit:** `docs(agent): the prefix-once comments stop counting Add's rejection gates`

## 2. The pop-up-fold comment stops citing an ISSUES entry that is not there — ✅ DONE (2026-08-14)

NOTES (2026-08-14): retry dispatch — the `render_test.go` reword was already in the working tree
from the prior attempt at this item, matched the item's text, and was kept; the `ISSUES.md` bullet
removal had been undone when item 1 restored that file, so it was reapplied here.

**What:** the comment block at `internal/tui/render_test.go:383–389` (above
`TestWrappedSurfacesBreakInThePaintersMeasure`) ends "…it is tracked in ISSUES.md with the
rest of the ADR 0030 residue", but the register's ADR 0030 entry holds only `hangingPrefixes`.
Reword the final sentence to state the fact without the citation: the fold is the lipgloss
pane's own, deliberate, and not separately tracked. Everything before that sentence stays.
Remove the matching ISSUES.md bullet (`render_test.go:389` stale citation, small-guards run);
CHANGELOG `[Unreleased]` gets one line.

**Files:** `internal/tui/render_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** none new — comment-only change.

**Acceptance:**
- `grep -n "tracked in ISSUES.md" internal/tui/render_test.go` → no matches
- `go test ./internal/tui/ -run TestWrappedSurfacesBreakInThePaintersMeasure`

**Commit:** `docs(tui): the pop-up-fold comment stops citing a nonexistent ISSUES entry`

## 3. Retract the stale auto-title ISSUES entry — ✅ DONE (2026-08-14)

**What:** the ISSUES bullet "`auto-title` has no case in `applySettingFor`
(`cmd/apogee/wire_settings.go:495`)…" (remember-model run) is factually stale: `auto-title` is
a renderer-owned key — `settingsApplyLive` (`internal/tui/settings.go:1274`) tries
`settingsApplyLocal` first, whose `settingKeyAutoTitle` case (`internal/tui/settings.go:1307–1308`)
sets `m.opts.AutoTitle` live, and the runtime gate reads that field per prompt
(`internal/tui/autotitle.go:96`). The binary dispatcher is never consulted for this key, so
its missing case is by design (same as the other renderer-owned keys), and committing
`auto-title` in `/settings` applies to the running session. Covered by
`TestSettingsPaneRendererOwnedKeysApplyWithoutTheSeam` (`internal/tui/settings_test.go:1073`,
auto-title sub-test at `:1081`). No code change: remove the entry, and record the retraction
under CHANGELOG `[Unreleased]` (one line stating the entry was stale — live-apply shipped
2026-08-06 with the live-apply dispatcher, `056583d`).

**Files:** `ISSUES.md`, `CHANGELOG.md`

**Tests:** none new — run the existing coverage as the verification.

**Acceptance:**
- `go test ./internal/tui/ -run TestSettingsPaneRendererOwnedKeysApplyWithoutTheSeam`
- `grep -n "applySettingFor" ISSUES.md` → no matches

**Commit:** `docs(issues): retract the stale auto-title entry — live-apply already ships`

## 4. `flattenField` folds `\r` like the line fold does — ✅ DONE (2026-08-14)

NOTES (2026-08-14): beyond the item's literal guard/replacer edit, `flattenField`'s doc comment and
`fieldBreaks`' were reworded (they read "each newline and each tab" / "both characters" and named the
tab a widening over the input fold — all made false by the third character), and the extended test's
header comment plus the first table case's name ("a field with neither" → "none of the three") were
updated for the same reason. No behavior beyond the `\r` fold changed; the pre-existing table cases,
the rune-count and the idempotence assertions are untouched.

**What:** `flattenField` (`internal/tui/transcript.go:1522–1527`) guards on
`strings.ContainsAny(s, "\n\t")` and `fieldBreaks` (`:1532`) replaces only `\n` and `\t`,
while its sibling `lineBreaks` (`internal/tui/lineeditor.go:184`) also folds `\r`; the display
seam `flattenField` guards takes model bytes no sanitizer touches. Add `\r` to both the guard
and the replacer (`"\r", " "` — one rune for one space, preserving the rune-count invariant
the existing test asserts). Remove the matching ISSUES.md bullet (`flattenField` folds `\n`
and `\t` but not `\r`, residuals sweep run); CHANGELOG `[Unreleased]` gets one line.

**Files:** `internal/tui/transcript.go`, `internal/tui/transcript_test.go`, `ISSUES.md`,
`CHANGELOG.md`

**Tests:** extend `TestFlattenFieldFoldsNewlinesAndTabs` (`internal/tui/transcript_test.go:744`):
add table cases carrying `\r` and `\r\n` (the latter folds to two spaces), and widen the
invariant loop's `!strings.ContainsAny(got, "\n\t")` assertion (`:763`) to include `\r`. The
rune-count and idempotence assertions stay.

**Acceptance:**
- `go test ./internal/tui/ -run TestFlattenField`

**Commit:** `fix(tui): flattenField folds carriage returns like the line fold does`

## 5. A direct `lineBreaks.Replace` unit test pins the input fold — ✅ DONE (2026-08-14)

**What:** `flattenLine`'s widened `\t`/`\r` branches (`internal/tui/lineeditor.go:171`, `:184`)
are unreachable through any in-package door — the bubbles runeutil sanitizer maps both before
the fold sees them — so today only end-state tests exist (`settings_test.go:2158` pins the
widget's output, not the replacer's). Add a new `internal/tui/lineeditor_test.go` with a
direct unit test of the package-level `lineBreaks` replacer: each of `\n`, `\t`, `\r` folds to
a single space; one rune for one rune (the caret arithmetic at `lineeditor.go:176–178` depends
on it — assert rune-count preservation); and the fold is idempotent. Remove the matching
ISSUES.md bullet (`flattenLine`'s widened branches unreachable, residuals sweep run);
CHANGELOG `[Unreleased]` gets one line.

**Files:** `internal/tui/lineeditor_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** the new test IS the change — suggested name
`TestLineBreaksFoldsNewlineTabAndCarriageReturn`, table-driven, calling `lineBreaks.Replace`
directly (same package, no `lineEditor` construction needed).

**Acceptance:**
- `go test ./internal/tui/ -run TestLineBreaks`

**Commit:** `test(tui): a direct lineBreaks.Replace test pins the input fold`

## 6. Diagnostics tests take symlink-resolved temp roots — ✅ DONE (2026-08-14)

**What:** `internal/tools/diagnostics_test.go` keeps 15 raw `t.TempDir()` roots — lines 64,
85, 97, 119, 120, 171, 193, 212, 237, 255, 276, 318, 382, 422, 473 — the same
symlinked-TMPDIR hazard (macOS `/tmp`) that already bit bare-sentence assertions elsewhere.
The package's own remedy exists: `tempRoot(t)` (`internal/tools/path_safety_test.go:131`,
`t.TempDir()` with symlinks resolved by the same rule `realPath` uses), already adopted at
`diagnostics_test.go:442` and across `exec_fence_test.go`, `read_file_test.go`,
`file_ops_test.go`, `write_file_test.go`, `file_edit_test.go`, `find_replace_test.go`.
Replace all 15 raw calls with `tempRoot(t)`. Remove the matching ISSUES.md bullet
(diagnostics_test raw TempDir roots, residuals sweep run); CHANGELOG `[Unreleased]` gets one
line.

**Files:** `internal/tools/diagnostics_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** the change is test-only; the whole package suite is the check.

**Acceptance:**
- `grep -n "t.TempDir()" internal/tools/diagnostics_test.go` → no matches
- `go test ./internal/tools/`

**Commit:** `test(tools): diagnostics tests take symlink-resolved temp roots`

## 7. Re-selecting the bound model or the active server records the pin

**What:** both pickers' early returns skip the remember write, so a user cannot pin what the
heartbeat (or startup) already put them on:

- `/model <id>` — `Model.bindPickedModel` (`internal/tui/picker.go:693–698`): when
  `id == m.opts.Model` it adds the "already bound" note and returns before the record chain at
  `:700–710`. In that branch, after the "already bound" note, attempt the record exactly as the
  normal path does: `record := recordModelChoice(m.opts.RecordModelChoice, id)`; on
  `record.saved` add `modelSavedNote`; then `record.warn(&m.transcript)`. (The wiring's silent
  skips — remember off, no bound entry, launcher-fronted — already live inside the seam,
  `cmd/apogee/wire_verbs.go:174–188`, and need no guard here.)
- `/server <name>` — `Model.switchToServer` (`internal/tui/picker.go:359–367`): the
  non-prebound early return (`choice.Endpoint == m.opts.Endpoint`) adds the "already on" note
  and returns before the record at `:379`. Mirror `:379`'s record + saved-note + warn handling
  inside that branch, after the "already on" note, using `choice.Name`. The `m.prebound()`
  branch (`bindToServer`) is untouched, and the endpoint-vs-name matching of the early return
  itself is out of scope (see header).

Remove the matching ISSUES.md bullet (`/model <id>` naming the already-bound model records
nothing, remember-model run); CHANGELOG `[Unreleased]` gets one line covering both twins.

**Files:** `internal/tui/picker.go`, `internal/tui/picker_test.go`, `ISSUES.md`,
`CHANGELOG.md`

**Tests:** extend `internal/tui/picker_test.go` (the recording coverage around `:341` shows
the house pattern): one test per twin asserting that re-selecting the already-bound
model/server invokes the record seam and, on a saved record, adds the saved note after the
"already …" note; plus one asserting a nil seam still yields only the "already …" note (no
panic, no extra note).

**Acceptance:**
- `go test ./internal/tui/`

**Commit:** `feat(tui): re-selecting the bound model or active server records the pin`

## 8. The entry-splice refusal names what the edit failed to place

**What:** `verifiedEntrySplice`'s refusal (`internal/config/configwrite_keysource.go:286–289`)
reads "the edit did not put the key source on the %q entry where a reader would look for it;
edit the file by hand" — but the function is also reached from `setEntrySetting`
(`internal/config/configwrite.go:391`), the model / launch-profile writer, where "key source"
is wrong. Ratified: the caller passes the noun. Add a `what string` parameter to
`verifiedEntrySplice`; `setEntryKeyCommand` (`configwrite_keysource.go:127`) and
`setEntryPlaintextKeyOK` (`:146`) pass `"the key source"` (message unchanged for them);
`setEntrySetting` passes the actual key it is writing (derive from its `entrySetting` row —
"the model" / "the launch profile"). Message shape:
`"the edit did not put %s on the %q entry where a reader would look for it; edit the file by hand"`.
Remove the matching ISSUES.md bullet (`verifiedEntrySplice`'s refusal message, remember-model
run); CHANGELOG `[Unreleased]` gets one line.

**Files:** `internal/config/configwrite_keysource.go`, `internal/config/configwrite.go`,
`internal/config/configwrite_keysource_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** no existing test asserts the refusal string (verified at scout time). Add a direct
unit test of `verifiedEntrySplice` in `configwrite_keysource_test.go`: hand it a before/after
pair whose edit did NOT land on the target entry (failing `serversChangedOnlyAt`) and assert
the returned error carries the caller's noun — one case with `"the key source"`, one with
`"the model"`.

**Acceptance:**
- `go test ./internal/config/`

**Commit:** `fix(config): the entry-splice refusal names what the edit failed to place`

## 9. The scalar writer's splice machinery moves to its own file

**What:** `internal/config/configwrite_scalar.go` is 803 lines, double the coding-standards
~400-line guide. Ratified: two files, **pure move** — no signature, logic, or doc-comment
changes beyond a banner for the new file. `configwrite_scalar.go` keeps the writer core:
entry points / key admission (`SaveConfigSetting`, `ResetConfigSetting`,
`validateSettingValue`, `writableKey`, `scalarPathDepth`), value rendering
(`renderSettingValue`, `ParseSettingList`, `renderScalarList`, `renderScalar`), the splice
drivers (`setScalarSetting`, `deleteScalarSetting`, `spliceScalarSet`, `spliceScalarDelete`),
and verification (`verifiedSplice`, `scalarAtPath`) — ~445 lines. A new
`internal/config/configwrite_scalarsplice.go` takes the machinery: targeting (`ScalarTarget`
+ `IsSet` + `childIndent` + `ScalarTargetIn` + `valueFitsOneLine`), text/block-scalar
rendering (`spliceTextBlock`, `textLineParts`, `blockScalarEnd`, `textBlockBody`,
`blockScalarHeader`), and insertion placement / comment scanning (`scalarInsertion`,
`settingLines`, `indentLines`, `CommentedExampleLine`, `commentedExampleBlockEnd`,
`commentedKey`, `isCommentLine`, `deleteLines`) — ~370 lines plus a short banner stating it
is the scalar writer's splice machinery, split out of `configwrite_scalar.go`. Update the
matching ISSUES.md bullet's resolution (the 803-line entry, configwrite split run) — remove
it; CHANGELOG `[Unreleased]` gets one line.

**Files:** `internal/config/configwrite_scalar.go`,
`internal/config/configwrite_scalarsplice.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** none new — a pure move inside one package; the existing scalar suite
(`configwrite_scalar_test.go`, golden-file based) is the behavioral check.

**Acceptance:**
- `go test ./internal/config/`
- `wc -l internal/config/configwrite_scalar.go internal/config/configwrite_scalarsplice.go`
  → both ≤ 500

**Commit:** `refactor(config): the scalar writer's splice machinery moves to its own file`

## 10. Shared splice plumbing lands beside its callers

Depends on item 9.

**What:** the configwrite split left three helpers stranded away from their callers; move
each to where its callers live (pure moves, doc comments travel and are adjusted only where
they describe location):

- `appendBlock` (`internal/config/configwrite.go:241`) is called from three files
  (`configwrite.go:174`, `configwrite_scalar.go` — the `spliceScalarSet` call — and
  `configmigrate.go:344`): move it to `internal/config/configsplice.go`, the machinery every
  config writer shares.
- `listValue` (`internal/config/configwrite_keysource.go:328`): its only callers are
  `renderSettingValue` and `scalarAtPath`, both in the post-split `configwrite_scalar.go` —
  move it there.
- `lineCount` (`configwrite_keysource.go:330`): its only caller is `spliceTextBlock`, in the
  post-split `configwrite_scalarsplice.go` — move it there. The pair's shared doc comment
  (`:319–326`, the deliberate-duplication note about `cmd/apogee/settingsrows.go`) splits
  with them, each half keeping the duplication rationale that applies to it.

Remove the matching ISSUES.md bullet (shared plumbing outside `configsplice.go`, configwrite
split run); CHANGELOG `[Unreleased]` gets one line.

**Files:** `internal/config/configwrite.go`, `internal/config/configsplice.go`,
`internal/config/configwrite_keysource.go`, `internal/config/configwrite_scalar.go`,
`internal/config/configwrite_scalarsplice.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** none new — pure moves inside one package; the package suite is the check.

**Acceptance:**
- `go test ./internal/config/`
- `grep -n "func appendBlock" internal/config/configsplice.go` → one match;
  `grep -n "func listValue\|func lineCount" internal/config/configwrite_keysource.go` → no
  matches

**Commit:** `refactor(config): shared splice plumbing lands beside its callers`

## 11. The configwrite prose names files, not positions

Depends on items 9 and 10.

**What:** the configwrite split left cross-file prose pointing at neighbours that are no
longer "above" or "below". Replace every positional reference with a file-qualified one (the
rule: name the file, and the function where one is meant; never "above"/"below" across file
boundaries):

- `internal/config/configwrite_keysource.go:22` — "the same contract the two writers above
  are" → name `configwrite.go`'s two writers.
- `internal/config/configwrite.go:273` — "Each writer above spells its own key…" → name them.
- `internal/config/configwrite.go:319` — "the writers above's contract" → same treatment.
- `internal/config/configwrite.go:404` — "the verification below is what catches that" → name
  `verifiedEntrySplice` in `configwrite_keysource.go`.
- `internal/config/configwrite_scalar.go:19` — "the acknowledgement writer above" → name
  `configwrite.go`'s acknowledgement writer.

(Line numbers are pre-split anchors from commit `ffc559e`; find each phrase by its quoted
text after items 9–10 shift lines.) Remove the matching ISSUES.md bullet (prose pointing
across files, configwrite split run); CHANGELOG `[Unreleased]` gets one line.

**Files:** `internal/config/configwrite.go`, `internal/config/configwrite_keysource.go`,
`internal/config/configwrite_scalar.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** none new — comment-only change.

**Acceptance:**
- `grep -n "writers above\|writer above\|verification below" internal/config/*.go` → no
  matches
- `go build ./internal/config/`

**Commit:** `docs(config): the configwrite prose names files, not positions`

---

**Suggested version bump:** micro (item 7 is a small shipped feature; the rest are fixes and
refactors). Whether and when to bump is the owner's call — no version identifier changes in
this plan.

# Residuals sweep + configwrite split — implementation plan

**Goal:** close seven small open residuals from the 2026-08-13 runs (one real redaction defect, one
input-fold gap, one README omission, two test-coverage gaps, one contradicting security comment,
three drifted doc comments) and split the 1631-line `internal/config/configwrite.go` into four
files by writer concern, behaviour-preserving.

**Date:** 2026-08-13
**Status:** unexecuted
**Sized for:** ~200k-context host.

**Authoritative sources:**
- `ISSUES.md` — the "Residuals fix wave run", "In-band retry and prompt-asset sweep run", and
  "API-key sources run" residual entries this plan resolves (each item names its entry).
- ADR 0043 (`docs/adr/0043-files-split-by-concern-and-config-gets-a-package.md`) — the
  split-by-concern decision; it names `configwrite.go` as a follow-on candidate.
- ADR 0035 — the textual-splice config-writer contract the split must preserve.
- `docs/plans/archived/2026-08-13 - 04 - residuals-fix-wave-plan.md` design call 6 + item 9 — the
  `tempRoot(t)` symlink-resolution pattern item 5 extends.

**Ratified design calls (owner, 2026-08-13, via AskUserQuestion in the plan-writing session):**
1. **Scope selection:** this batch = residuals sweep + configwrite split. The
   out-of-workspace-write Execute defect stays open (fence call deferred); the `rm` rules' bare-`/`
   hard-refuse is **documented as intended**, behaviour unchanged (item 6).
2. **Keystore leak fix = tail-prefix trim:** when the capped stderr buffer filled to its cap, trim
   any trailing bytes that form a proper prefix of the key before redaction; no cap or
   memory-bound change (item 1).
3. **configwrite layout = 4 files:** `configsplice.go` (shared plumbing), `configwrite.go`
   (acknowledgement writer only), `configwrite_scalar.go`, `configwrite_keysource.go`; exported
   API unchanged (items 8–10).
4. **flattenLine folds tabs AND carriage returns**, each to one space, rune-for-rune (item 2).

**Standing requirements:**
- skills: coding-standards
- Behaviour-preserving throughout items 8–10: no exported-name change, no logic change — the
  external callers in `cmd/apogee` (`wire_verbs.go`, `wire_options.go`, `wire_settings.go`,
  `keymigrate.go`, `settingsedit.go`) must compile untouched.
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- No version identifier changes (see the closing note).

**Out of scope:** the approved out-of-workspace write defect (owner call deferred); the
`/server`-overlay residual (ADR 0036 decision 6); the cancel-in-holdoff seam; the
`internal/agent/prompts/` README (design call 9 of its plan scoped it out); the README accept-
paragraph wrap cosmetic; Darwin/Windows hardware-gated residuals; the EXDEV fallback test
(unreachable on a single-device tmpdir); `run_tests` PATH scoping; Windows-home dangerous-rule
patterns (ADR 0020 declared debt); the `listValue` duplication between `configwrite_keysource`
and `settingsrows.go` (pre-existing, noted only).

---

## 1. Keystore stderr redaction survives the 4 KiB cut — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the trim checks the QUOTED spelling as well as the raw key — the item text names
only "a proper prefix of the key", but `redactKey` already redacts both spellings for the same
reason, and a key carrying `"` or `\` diverges from its escaped form (a cut after `"sk\"l` leaves a
tail that is a prefix of no raw key). One extra spelling in the loop; covered by the quoted-key
subtest.

**What:** `internal/keystore/run.go` caps captured store-tool stderr at `maxToolStderr` (4096
bytes) with a raw byte cut (`cappedBuffer`, `run.go:124-157`), and redaction runs only afterwards
(`redactKey`, `keystore.go:228-240`, whole-value `strings.ReplaceAll`). A key straddling the cut
leaves an unredacted key PREFIX in the refusal message. Ratified fix (design call 2): after
capture and before `redactKey` is applied (`keystore.go:177`), when the buffer filled to its cap,
trim the longest buffer suffix that is a proper prefix of the key (any length ≥ 1); the trimmed
text never reaches `said()`. Implement it where the redaction seam already lives so both error
paths (`keystore.go:178-181` and `:185-188`) inherit it; keep `cappedBuffer` itself unchanged
(its never-error Write contract stands). Remove the resolved ISSUES entry via item 11, not here.

**Files:** `internal/keystore/run.go`, `internal/keystore/keystore.go`,
`internal/keystore/keystore_test.go`

**Tests:** a new test beside `TestWriteRedactsTheSecretFromWhatTheStoreSaid`
(`keystore_test.go:732`): a fake tool whose stderr places the key so it straddles the 4096-byte
boundary; assert the message carries no prefix of the key (probe several prefix lengths, e.g. the
first 8 bytes). A companion case where the buffer is NOT full pins that no trimming happens then.

**Acceptance:** `go build ./... && go test ./internal/keystore/`

**Commit:** `fix(keystore): trim a capped stderr tail that is a key prefix before redaction`

## 2. `flattenLine` folds tabs and carriage returns — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the item's premise — that a pasted `\t` or `\r` "survives into a one-line settings
field" — does not hold against the widget: EVERY write into a bubbles textarea (`SetValue`,
`InsertString`, the `tea.PasteMsg` arm) runs through `insertRunesFromUserInput` → `runeutil.Sanitizer`,
whose defaults map `\t`→four spaces and `\r`→`\n` before `flattenLine` sees the value. The ratified
fold (design call 4) is implemented exactly as specified and is now the field's own invariant rather
than a borrowed one, but it is unreachable through any door today. The two new subtests therefore pin
the observable END STATE — no control rune reaches the row, caret at the flattened value's rune count
— rather than asserting each control rune became ONE space: the pasted tab lands as the widget's four
spaces, the pasted CRLF as two. The doc comment and the test comment both state which layer does what.

**What:** `lineEditor.flattenLine` (`internal/tui/lineeditor.go:157-165`) folds only `\n` to a
space, so a bracketed paste carrying `\t` or `\r` survives into a one-line settings field.
Ratified (design call 4): fold `\n`, `\t`, and `\r` each to one space — rune-for-rune so the
caret-offset preservation (`caretRune`/`caretToRune`) keeps working; update the early-return
guard to check all three, and extend the doc comment at `:148-156` to name the widened fold
(the display-seam sibling is `flattenField`, `internal/tui/transcript.go:1503`, which already
folds tabs).

**Files:** `internal/tui/lineeditor.go`, `internal/tui/settings_test.go`

**Tests:** extend `TestSettingsPasteLandsInTheOpenField` (`settings_test.go:2152`) with subtests
pasting a tab and a `\r\n` into the open field; assert each control rune became a space and the
caret lands where the rune count says.

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `fix(tui): flattenLine folds tabs and carriage returns like newlines`

## 3. README names the empty-variable key-source failure — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the amended sentence is ten characters longer, so the paragraph's last two lines
were re-wrapped to absorb it — 89 columns, inside the paragraph's existing maximum of 90. No other
line of the paragraph moved.

**What:** `README.md:662` lists the key-resolution failure cases as "A non-zero exit, a
60-second timeout, empty output or an unset variable" — but `resolveEnvKey` also refuses a
variable that is set and *empty*. Amend the sentence to include the empty-variable case (e.g.
"empty output, or an unset or empty variable"), keeping the paragraph's existing line width.

**Files:** `README.md`

**Tests:** none (prose).

**Acceptance:** `grep -n "unset or empty" README.md` shows the amended sentence; the paragraph
still reads as one sentence.

**Commit:** `docs(readme): key-source failure list includes an empty variable`

## 4. `ScopeEnv` Windows Path-scoping subtest with a real root — ✅ DONE (2026-08-13)

**What:** no `ScopeEnv` subtest exercises `windowsRules()` with a non-empty workspace root — the
"windows folds duplicate names" subtest (`internal/platform/host_test.go:166`) passes root `""`,
so Path-spelling scoping on the allowlist path is pinned only via `ScopeInheritedEnv`'s test
(`:251`). Add one subtest to `TestScopeEnvKeepsTheCallersAllowlistAndAddsThePlatformFloor`:
`windowsRules().ScopeEnv` with a real root (e.g. `C:\work\repo`) and a `Path` value mixing
in-workspace and system entries; assert the in-workspace entries are dropped, the system entries
survive, and the folded duplicate keeps the scoping. Mirror the fixture shape of the
`ScopeInheritedEnv` subtest at `:251`.

**Files:** `internal/platform/host_test.go`

**Tests:** the item IS the test.

**Acceptance:** `go test ./internal/platform/`

**Commit:** `test(platform): ScopeEnv pins Windows Path scoping under a real workspace root`

## 5. Exec-fence and diagnostics test roots survive a symlinked TMPDIR

**What:** four `internal/tools` tests still build roots from raw `t.TempDir()` and fail under a
symlinked `TMPDIR`; convert each to the canonical `tempRoot(t)` helper
(`internal/tools/path_safety_test.go:131-135`, the `filepath.EvalSymlinks` wrapper the
2026-08-13 residuals-fix-wave item 9 established). Sites: `exec_fence_test.go:110`
(`TestEveryExecSiteRefusesAProgramInsideTheWorkspace`), `:131`
(`TestPythonExecRefusesAnInRepoVirtualenvByName`), `:154-155`
(`TestExecFenceCoversTheConfinementBoxNotOnlyTheRoot`, both `root` and `extra`), and
`diagnostics_test.go:442` (`TestDiagnostics_ApprovalScopeNamesTheSamePackageAsTheResult`).

**Files:** `internal/tools/exec_fence_test.go`, `internal/tools/diagnostics_test.go`

**Tests:** the item IS the test change. Verify the fix the way the prior item did: run the two
files' tests once normally and once with `TMPDIR` pointed at a symlink to a real directory
(create both under a scratch dir), both passing.

**Acceptance:**
`go test ./internal/tools/ -run 'TestEveryExecSiteRefusesAProgramInsideTheWorkspace|TestPythonExecRefusesAnInRepoVirtualenvByName|TestExecFenceCoversTheConfinementBoxNotOnlyTheRoot|TestDiagnostics_ApprovalScopeNamesTheSamePackageAsTheResult'`
run twice — once as-is, once with `TMPDIR` set to a symlinked directory — both green.

**Commit:** `test(tools): exec-fence and diagnostics roots resolve symlinks like the writers do`

## 6. `rm` rules' comment documents the absolute-path hard-refuse as intended

**What:** the bare `/` branch of the two recursive-delete rules
(`internal/security/rules.go:31-32` and `:40`, pattern alternation
`(?:/|~|\$home|/\*|/(?:etc|usr|…)\b)`) matches EVERY absolute target, making the system-path
enumeration dead as a discriminator — while the comment at `:20-25` claims "destructive
recursive deletes of project files stay allowed". Owner call (design call 1): behaviour stands —
every absolute recursive delete hard-refuses; only relative/`./` targets stay allowed. Rewrite
the comment block at `:20-25` (and reconcile the package-level near-miss claim at `:11-12` if it
also implies absolute project paths pass) to state that: the precision boundary is
relative-vs-absolute, the bare `/` branch deliberately catches all absolute targets, and the
system-path enumeration is retained as documentation of the worst cases, not as the
discriminating branch. No regex change, no test-behaviour change.

**Files:** `internal/security/rules.go`

**Tests:** none new — `go test ./internal/security/` pins existing behaviour unchanged.

**Acceptance:** `go build ./... && go test ./internal/security/`

**Commit:** `docs(security): rm rules' comment owns the absolute-path hard-refuse`

## 7. Comment-drift sweep: promptFS, /color-scheme, /schedule

**What:** three drifted doc comments, each corrected in place:
- `internal/mechanisms/toolloop.go:116-124` — the `promptFS` comment still narrates `prompts/`
  as "the fixed sentence fragments of the loop-breaking directive"; `prompts/` now holds 17
  assets spanning several mechanisms. Rewrite the first sentence to describe the directory as
  the package's prompt text generally (keep the go:embed / never-read-from-disk /
  never-overridable clauses and the design-call-2 sentence).
- `internal/tui/command.go:62-64` — the takesArgs bullet names only `/confine` as keeping a
  dedicated parse; `/color-scheme` has one too (`parseColorScheme`, `:446`, dispatched at
  `:202`). Name both, consistently with the registry prose at `:143-147`.
- `internal/tui/doc.go:124` — "every other row is handed a plain token list" glosses
  `/schedule`, whose prompt form reads the raw tail (`parsedInput.rest`,
  `internal/tui/command.go:128-134`). Qualify the sentence so /schedule's raw-tail read is
  acknowledged (the /color-scheme exception is already named in the same sentence's opening).

**Files:** `internal/mechanisms/toolloop.go`, `internal/tui/command.go`, `internal/tui/doc.go`

**Tests:** none (comments only).

**Acceptance:** `go build ./... && go vet ./internal/mechanisms/ ./internal/tui/`

**Commit:** `docs(comments): promptFS, takesArgs, and token-list prose match the code`

## 8. configwrite split I — the key-source writer moves out

**What:** move the key-source section of `internal/config/configwrite.go` (banner at
`:1329-1347` through end of file, `:1348-1631`: the four yaml-tag consts,
`SaveServerKeyCommand`, `SaveServerPlaintextKeyOK`, `saveEntryEdit`, `readConfigForEntryEdit`,
`setEntryKeyCommand`, `setEntryPlaintextKeyOK`, `serverEntryAt`, `spliceEntryKeyCommand`,
`spliceEntryPlaintextKeyOK`, `serverEntryNode`, `verifiedEntrySplice`, `serversChangedOnlyAt`,
`listValue`, `lineCount`) verbatim into a new `internal/config/configwrite_keysource.go`,
carrying the section banner as the new file's header comment. Pure move: no renames, no logic
edits. Add the new file to `internal/config/doc.go`'s file map (the docmap test gates this) and
adjust doc.go's `configwrite.go` sentence only as far as this item's move makes it false —
item 10 owns the final wording.

**Files:** `internal/config/configwrite.go`, `internal/config/configwrite_keysource.go`,
`internal/config/doc.go`

**Tests:** existing `configwrite_keysource_test.go` unchanged and green.

**Acceptance:** `go build ./... && go test ./internal/config/` (includes the docmap test);
`git diff --stat` shows no file outside `internal/config/`.

**Commit:** `refactor(config): key-source writer moves to configwrite_keysource.go`

## 9. configwrite split II — the scalar writer moves out

Depends on item 8.

**What:** move the scalar-setting section (banner `:450-471` through `:1328` of the pre-split
file: `scalarPathDepth` through `scalarAtPath`, including the `ScalarTarget` type and methods)
verbatim into a new `internal/config/configwrite_scalar.go`, banner as header. The shared
helpers embedded in that span that item 10 will re-home (`rootMapping`, `isNullNode`,
`scalarLineParts`, `indentLine`, `indentLines`, `isCommentLine`, `deleteLines`) move WITH the
section here — one move each, item 10 takes them from there. Pure move, no renames. Add the file
to `internal/config/doc.go`'s map.

**Files:** `internal/config/configwrite.go`, `internal/config/configwrite_scalar.go`,
`internal/config/doc.go`

**Tests:** existing `configwrite_scalar_test.go` unchanged and green.

**Acceptance:** `go build ./... && go test ./internal/config/`;
`git diff --stat` shows no file outside `internal/config/`.

**Commit:** `refactor(config): scalar-setting writer moves to configwrite_scalar.go`

## 10. configwrite split III — shared splice plumbing gets its own file

Depends on item 9.

**What:** extract the concern-neutral plumbing into a new `internal/config/configsplice.go`
(ratified layout, design call 3), leaving `configwrite.go` as the acknowledgement writer alone.
Move: from `configwrite.go` — `Document`, `mappingEntry`, `maxNodeLine`, `insertAt`,
`SplitConfigLines`, `joinConfigLines`, `sameApartFrom`, `zeroConfigPath`, `fieldByYAMLTag`,
`writeConfigAtomically`, `ReadConfigForWrite`; from `configwrite_scalar.go` — `rootMapping`,
`isNullNode`, `scalarLineParts`, `indentLine`, `indentLines`, `isCommentLine`, `deleteLines`.
Give `configsplice.go` a header comment naming its role: the line/node splice machinery all
three writers and `configmigrate.go` share (ADR 0035's verified textual-splice contract). Pure
moves, no renames, exported API unchanged. Update `internal/config/doc.go`: one line per file —
`configsplice.go`, `configwrite.go` (acknowledgement), `configwrite_scalar.go`,
`configwrite_keysource.go` — replacing the old single `configwrite.go` sentence. Resulting
`configwrite.go` ≈ ack section only (~400 lines); note in the sidecar if any helper turns out
to be used by exactly one writer (leave it with that writer, dated NOTES line).

**Files:** `internal/config/configwrite.go`, `internal/config/configwrite_scalar.go`,
`internal/config/configsplice.go`, `internal/config/doc.go`

**Tests:** whole-package suite green, unchanged test files.

**Acceptance:** `go build ./... && go test ./internal/config/ && go test ./cmd/apogee/` (external
callers compile and pass untouched); `git diff --stat` shows no changes under `cmd/apogee/` or
`internal/tui/`.

**Commit:** `refactor(config): shared splice plumbing moves to configsplice.go`

## 11. ISSUES.md sweep — the resolved entries leave the register

Depends on items 1–10.

**What:** remove from `ISSUES.md` exactly the entries this plan resolved, per the file's own
convention (resolved work leaves; `CHANGELOG.md` is the closed trail — the per-item commits
already carry the changelog entries):
- "Residuals fix wave run — residuals (2026-08-13)": the keystore-stderr entry (item 1), the
  README key-source entry (item 3), the `ScopeEnv` Windows-Path entry (item 4), the
  `flattenLine` tab entry (item 2), the symlinked-`TMPDIR` four-tests entry (item 5), and the
  `rm` rules dead-regex entry (item 6) — that section then holds no bullets: remove its heading
  and intro line too.
- "In-band retry and prompt-asset sweep run — residuals (2026-08-13)": the `promptFS` doc-comment
  entry, the `command.go:62-64` /color-scheme entry, and the `tui/doc.go` plain-token-list entry
  (all item 7). The section's other bullets (cancel-in-holdoff, `internal/agent/prompts/` README,
  README wrap cosmetic) stay.
- "API-key sources run — residuals (2026-08-13)": the `configwrite.go` 1631-line split entry
  (items 8–10). The `/server`-overlay bullet stays.

No other ISSUES edits; open entries and section conventions untouched.

**Files:** `ISSUES.md`

**Tests:** none (register maintenance).

**Acceptance:** `grep -c "cappedBuffer" ISSUES.md` returns 0;
`grep -n "Residuals fix wave run" ISSUES.md` returns nothing;
`grep -n "cancel landing inside" ISSUES.md` still finds the kept in-band entry (spot-check that
surviving bullets survived).

**Commit:** `docs(issues): resolved residuals leave the register for the changelog`

---

**Suggested version bump:** micro (`v0.13.17` → `v0.13.18`) once executed — a shipped redaction
fix plus a user-visible paste fix warrant the changelog line; the owner decides, no item touches
`VERSION` or `CHANGELOG` release headings.

# The deferred register — the residues, the manual gaps, and the two defects

**Goal:** close the eighteen open beads that no other plan owns — the Mechanism-retirement residue, the
ten manual gaps, the tool-row error defect, the hero tape's clock, and the git-hook pair — so the
register holds only work that is genuinely parked. No new config key, no new verb.

**Date:** 2026-09-03 · **Status:** unexecuted · **Base:** `94c36e88` · **sized for:** ~200k-context host

**Sources**
- issue register (`bd`) → `apogee-5qe.1`, `apogee-5qe.2`; `apogee-i5h.1`–`.4`; `apogee-b3u`; `apogee-h64`; `apogee-t57.1`–`.10`
- ADRs `0012` (blast radius, the dangerous-action guard), `0016` (Validated sets), `0030` (the width authority), `0039` (parallel sub-agents), `0044`/`0050`/`0057`/`0058` (the profile axes), `0052` (split diffs), `0062` (test drivers), `0071` (Floor guards, the empty catalogue)
- `docs/layout/tool-layout.md` and `docs/layout/split-diff-layout.md` (canon for the surfaces items 7 and 15 touch)
- `graphics/demo/README.md` (the knobs) · `CONTEXT.md` (the *Validated set* definition item 9 quotes)

**Ratified design calls** (owner, 2026-09-03)
- **Catalogue seam:** `internal/mechanisms` exports a swap-and-restore test seam; items 4 and 6 register a row through it.
- **Floor superset:** `internal/floor` exports the predicate and the name set; the coverage test lives in `internal/agent`, the one package importing both floor and `internal/tools`.
- **Failed tool rows:** the collapsed slot is the bare word `error` (a `failure` hook's own short word where one exists); the whole message always becomes the expandable body.
- **Hero tape:** knob 3 becomes `Wait+Screen@<timeout> /\+1 −1/` — the fix card's diffstat, which is what tells it from the CHANGELOG card.
- **Git hooks:** `.git/hooks/commit-msg` stays as a dormant fallback; `AGENTS.md` records `.beads/hooks/` as the live path.
- **Pre-commit sweep:** reproduced empirically in a scratch clone, and the finding recorded — not guarded speculatively.
- **Manual shape:** a keyed `##` section per substantial topic; a paragraph where the gap is one key.
- **Drift gate:** a mechanical registry↔manual check with a named allowlist ships as item 17.

**Standing requirements:** `skills: coding-standards`. Any authorized deviation lands as a dated NOTES line under its item. Plan `docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md` is independent and unexecuted but owns `cmd/apogee/{headless,daemon,daemonfire,wire,wire_boot,wire_firing,root}.go`, `internal/run/`, `internal/session/`, `internal/mcp/`, `internal/tui/reportpane*.go` and `docs/manual/{headless,daemon}.md` — run one plan at a time.

**Out of scope:** the twelve beads plan `2026-09-03 - 01` owns (`apogee-kk0.*`, `apogee-1ov.*`, `apogee-4w7`) · the bench arms (`apogee-304.*`) · the owner-run hardware passes (`apogee-2uh.*`) · code-signing (`apogee-3p1`) · every parked feature bead (`apogee-09k`, `-9d8`, `-fsx`, `-m3p`, `-54f`, `-1bf`, `-37s`, `-rms`, `-2sj`, `-96u`, `-p5j`, `-3b3`, `-8wy`) · re-recording the hero GIF · any new config key.

**Regression check (2026-09-03, `94c36e88`):**
- 1: guard folded — the clone's register is hydrated before the sweep is judged, the fix stays hook-scoped, and the `commit-msg` check is made exact.
- 2: guard folded — `register`'s curation comment names the exported test seam as its one exception.
- 5: guard folded — the private predicate stays and is wrapped, only the two tables come over, and the acceptance greps the import path.
- 6: guard folded — two rows registered, members out of canonical order, and no `t.Parallel()`.
- 7: recast — the failure vocabulary and `failedSummary` change with `absorbFailure`, the ORPHAN branch is reworded with them, four more pinned tests join the item, and it supersedes the floor-line contract comment at `internal/tui/toolview.go:1155-1158`.
- 8: recast — the wait pattern carries the card's real U+2212 sign, and the ASCII stat is fixed where the prose quotes the card.
- 9: recast — the new section lands before `## Keeping the session store bounded`, and every later manual item re-locates by heading text.
- 10: guard folded — `ui.show-scrollbar` is a live flip, not process-constant, and 428-448 is not the section's extent.
- 12: guard folded — the fence's `custom-regex` half is written fresh, and the heading goes after the effort-dialect prose.
- 13: recast — the ruleset is built-in in this build with no config key; the item yields to `docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md:91-92`.
- 14: guard folded — the acceptance greps are pinned to phrases that do not already pass at BASE.
- 15: guard folded — six blocks render a diff (or the regions rule), and the `## Reading a tool …` heading is named in the **What**.
- 17: recast — the predicate accepts the leaf and fenced spellings and is verified against `config.KeyRegistry`; the new file reuses `manualConfigPath` and `firstSentence`.
- 18: guard folded — `.beads/issues.jsonl` is flushed, staged and verified closed for all 22 ids.
- Re-check round (`0be6c920`), second pass over the amended items:
- 5: guard extended — the coverage test reads the tool roster live, so `task_list` (added at `805fda78`) and anything after it cannot go unclassified.
- 7: guard extended — every site pinning the failed wording moves by rule-plus-grep rather than by list (six more `subagentblock_test.go` goldens named), the relocated message keeps the tool's own spelling under the quoted-body rule, and both doc edits are scoped to the fallback branch.
- 8: guard extended — the tape path is corrected to `graphics/demo/tapes/hero.tape`, and every ASCII diffstat quoted off a card moves by rule-plus-grep, the `+3 -0` comment line included.
- 9: guard folded — the section's heading is named verbatim in the **What** and the 448/449 placement clause is struck in favour of "immediately before `## Keeping the session store bounded`".
- 13: guard folded — `~/.apogee` moves to the Tier-2 examples (`TierForceApproval`), and the "editable in global config" sentence leaves the **What** for the ADR-0012 framing.
- 17: guard extended — both back-tick arms accept a trailing colon, `context-files.enable` leaves the must-pass list, and the undocumented keys are documented by the item rather than allowlisted.

---

## 1. The repo's hook path is reproduced and recorded — ✅ DONE (2026-09-04)

NOTES (2026-09-04): reproduction transcript — clone of this repo hydrated with `bd init --prefix apogee` (bootstrapped the Dolt DB from the beads remote; `.beads/embeddeddolt` created) plus `bd import .beads/issues.jsonl` for the 11 JSONL-only records, then the hook was observed rewriting `.beads/issues.jsonl` (76 issues exported) before any verdict was taken. Three commits through `.beads/hooks/pre-commit` (`core.hooksPath=.beads/hooks`, bd shim 1.2.2) — staged deletion + staged edit with no `.beads/` path staged; a staged `.beads/config.yaml` + edit; staged `.beads/issues.jsonl` + edit + deletion — each carried exactly the index in `git show --stat HEAD`, leaving an unstaged edit and an untracked file untouched. Verdict: CLEAN, no sweep, so no fix was landed and `.beads/config.yaml` is untouched; the `--no-verify` convention is therefore not written into the bullet.

NOTES (2026-09-04): the reported 22-deletion incident WAS reproduced, from a different cause: in a second clone with a deletion and an edit already staged, `bd init`'s own auto-commit ("bd init: initialize beads issue tracking") swallowed both. Recorded in the bullet as "never run `bd init` with a dirty index".

NOTES (2026-09-04): two further observations recorded in the bullet — `bd hooks run pre-commit` exports only when a `.beads/` path is already staged (it prints `pre-commit: skipping JSONL export — no staged .beads paths` otherwise), and because it re-exports after git snapshots the index, committing a staged `.beads/issues.jsonl` commits the pre-hook content while the fresh export stays unstaged.

NOTES (2026-09-04): AGENTS.md carried no `--no-verify` convention at BASE, so nothing had to be removed; the item's "drop it" clause applied to the new bullet's own wording.

**What.** Closes `apogee-5qe.1` and `apogee-5qe.2`. Two facts are already true at this base and the item
starts by confirming them, not assuming them: `bd config show` reports `export.git-add = false` (bd's
default), so `bd hooks run pre-commit` writes `.beads/issues.jsonl` but stages nothing; and
`.beads/hooks/commit-msg` is tracked, carries no `BEGIN BEADS INTEGRATION` markers, and is absent from
`bd hooks list` — bd does not manage it, so the attribution stripper survives a `bd hooks install`.
**Reproduce the sweep** in a throwaway clone of this repo (never in the working tree): set
`core.hooksPath` to `.beads/hooks`, stage a file deletion plus one edit, commit through the hook, and
compare `git show --stat HEAD` against what was staged. Record the observed result — swept or clean —
in a new `AGENTS.md` bullet under *Conventions not derivable from the code*, which also states that
`.beads/hooks/` is the live hook path, that `.git/hooks/commit-msg` is a deliberate dormant fallback
kept for a `core.hooksPath` unset, and that beads' section markers preserve anything written outside
them. If the sweep DOES reproduce, that is a live defect: fix it in this item (the narrowest fix is
`BD_GIT_HOOK`-scoped, e.g. `bd config set export.auto false`) and say so in the bullet — never defer it.
Drop the `--no-verify` convention from the bullet only if the reproduction came back clean.

**Regression guard.** A fresh clone carries no Dolt DB — `git ls-files .beads/` tracks only `.gitignore`,
`README.md`, `config.yaml`, `hooks/*`, `interactions.jsonl`, `issues.jsonl` and `metadata.json`, never
`embeddeddolt/` — so `bd hooks run pre-commit` there exits 0 writing nothing and a "clean" verdict is
non-evidence: hydrate the clone's register first (`bd import` / `bd init`, until `.beads/embeddeddolt`
exists), assert the hook actually rewrites `.beads/issues.jsonl` in it, and only then judge the sweep,
recording the hydration step in the transcript. If the sweep reproduces, keep the fix `BD_GIT_HOOK`-scoped
(`export.git-add` is already `false`) rather than setting `export.auto false` — that key lives in the
TRACKED `.beads/config.yaml:68-69`, and turning it off leaves the committed export stale against the DB,
reversing AGENTS.md:16 ("committed so a clone without `bd` can still read the register"); if it is turned
off anyway, the bullet must say the export is thereafter exported and staged by hand.

**Files:** `AGENTS.md`; `.beads/config.yaml` only if the reproduction lands a config-scoped fix

**Tests.** None — this item's evidence is the reproduction transcript, quoted in the commit body.

**Acceptance.** `bd config show | grep 'export.git-add'` prints `false`;
`bd hooks list | grep -v prepare-commit-msg | grep -c 'commit-msg'` is 0 — the bare count is 1 today,
from `prepare-commit-msg`, so only the exact form proves bd manages no stripper;
`git ls-files .beads/hooks/` names all six hooks; `grep -c 'beads/hooks' AGENTS.md` is ≥ 1;
`diff .beads/hooks/commit-msg .git/hooks/commit-msg` is silent.

**Commit:** `docs(agents): the live git-hook path and what beads' pre-commit actually stages`

---

## 2. `mechanisms.SwapCatalogue` — a seam a test can register a row in — ✅ DONE (2026-09-04)

NOTES (2026-09-04): the swapped row's descriptor field asserted for pass-through is `Capability` (`domain.MechanismDescriptor` has no `BypassDisables` field) — the assertion the plan's intent asks for, on the field that exists.

NOTES (2026-09-04): added a fourth test beyond the three the item names — `SwapCatalogue` reaches the catalogue through `registerIn`, so its empty-ID and duplicate-ID panics now have an exported caller; the test pins that they still panic and that the shipped catalogue is left unswapped.

**What.** The shipped catalogue is empty by design (`internal/mechanisms/catalogue.go:54`, ADR 0071) and
`catalogue`, `row`, `register`, `registerIn`, `buildFrom` and `knownIDs` are all unexported, so nothing
outside the package can reach the config → `EnableMechanisms` → engine-build path with a real row —
which is what leaves items 4 and 6 unpinnable. Export one seam and nothing else:
`func SwapCatalogue(rows []Row) (restore func())`, plus the exported `Row` a caller needs to describe one
(id, descriptor fields, constructor). It swaps the package-level `catalogue` map and returns the
restore closure; callers defer it. `Build`, `KnownIDs` and `Descriptors` keep reading the package var,
so they see the swapped table with no other change. Document it in `internal/mechanisms/doc.go` as a
TEST seam whose only production use is none — the existing `registerIn`/`buildFrom` comments
(`catalogue.go:71-72`, `:130-131`) already state that intent for the private twins, and this is the
exported half of the same argument. It is deliberately NOT concurrency-safe: a swapping test does not
call `t.Parallel()`, and the doc comment says so.

**Regression guard.** `register`'s own comment (`internal/mechanisms/catalogue.go:61-63`) states that
"register is deliberately unexported: the catalogue is curated (ADR 0002 / ADR 0015 §6), so there is no
public way to add a Mechanism to it" — a sentence `SwapCatalogue` makes false. Amend it in this item, in
the file **Files:** already lists, to name the exported swap-and-restore seam as the one, test-only
exception, so the curation claim stays true.

**Files:** `internal/mechanisms/catalogue.go`, `internal/mechanisms/doc.go`, `internal/mechanisms/catalogue_test.go`

**Tests.** A swap makes `KnownIDs()`, `Descriptors()` and `Build()` see the registered row; the restore
closure puts the empty shipped catalogue back; a nested swap restores in order.

**Acceptance.** `go test ./internal/mechanisms/`; `go vet ./internal/mechanisms/`;
`grep -c 'func SwapCatalogue' internal/mechanisms/catalogue.go` is 1.

**Commit:** `feat(mechanisms): a swap-and-restore catalogue seam for tests`

---

## 3. A shed Validated-set member names the guard that governs it now — ✅ DONE (2026-09-04)

NOTES (2026-09-04): the test's two wordings are asserted as two blocks inside the existing
`TestResolveValidatedSetDropsARetiredIDWithANotice` rather than by converting it to a table — the
`grammar` assertions are untouched byte-for-byte, and the promoted case reuses the same decision
because `retiredSetMemberNotice` is pure.

**What.** Closes `apogee-i5h.1`. `retiredSetMemberNotice` (`cmd/apogee/validatedsets.go:247-251`) names the
entry, the ID and `mechanisms.RetiredRelease(id)`, but never `mechanisms.Successor(id)`
(`internal/mechanisms/retired.go:158-163`) — so a member shed because it became a **Floor guard** (six of
the twenty-one rows: `tool_loop_interceptor`, `validate`, `empty_response_recovery`, `tool_use_enforcer`,
`cached_content_intercept`, `tool_result_cap`, `retired.go:105-110`) tells the reader the row is gone
without saying the behaviour survives. Split the wording on `Successor(id)`, matching the phrasing
`ResolveEnabled` already uses for the same IDs (`retired.go:264-266`, `is the %q floor guard since %s`):
an empty successor keeps today's sentence byte-for-byte; a non-empty one reads
`apogee: validated-set entry %q names mechanism %q, the %q floor guard since %s — it is dropped from the set and the rest applies; the behaviour is on by default.`
The function stays pure and table-testable. No caller changes (`validatedsets.go:211` is the only one).

**Files:** `cmd/apogee/validatedsets.go`, `cmd/apogee/validatedsets_test.go`

**Tests.** Extend `TestResolveValidatedSetDropsARetiredIDWithANotice`
(`cmd/apogee/validatedsets_test.go:270-300`): its existing `grammar` case (empty successor) keeps the
current substrings unchanged; a new case over a promoted ID asserts the successor key and the
floor-guard phrasing appear, and that the retired-outright sentence does NOT.

**Acceptance.** `go test ./cmd/apogee/ -run 'ValidatedSet'`; `go vet ./cmd/apogee/`;
`grep -c 'floor guard since' cmd/apogee/validatedsets.go` is ≥ 1.

**Commit:** `fix(validated): a shed set member names the floor guard that governs it now`

---

## 4. The Bypass construct test builds a real catalogued row

**What.** Closes `apogee-i5h.2`. Depends on item 2.
`TestMechanismIDsConstructsUnderBypass` (`cmd/apogee/wire_tools_test.go:281-296`) enables `"validate"`,
which retired in this wave (`internal/mechanisms/retired.go:106`), so `ResolveEnabled` drops it at
`retired.go:242` and the test builds an Agent with an EMPTY `EnableMechanisms` — it proves only that an
empty enable list constructs. Its comment (`:277-280`) still calls `validate` "a real catalogued
Mechanism" and is false. Rewrite it over a row registered through `mechanisms.SwapCatalogue`: swap in one
test row, resolve it through `ResolveEnabled` with `KnownIDs()`, assert the resolved ids are non-empty,
set them on a `validCfg(t)` with `Bypass = true`, and build through `apogee.New` — the end-to-end claim
the comment makes, now true. Assert the returned notices are empty for a known live id. Drop
`t.Parallel()` (item 2's seam is not concurrency-safe) and say why in one line. The stale comment is
this item's to correct.

**Files:** `cmd/apogee/wire_tools_test.go`

**Tests.** The rewritten test itself; plus a second case pinning that a RETIRED id still resolves to an
empty list and earns its floor-guard notice, so the vacuous shape cannot come back unnoticed.

**Acceptance.** `go test ./cmd/apogee/ -run 'MechanismIDs'`; `go vet ./cmd/apogee/`;
`grep -c 'SwapCatalogue' cmd/apogee/wire_tools_test.go` is ≥ 1.

**Commit:** `test(wire): the bypass construct check builds a real catalogued row`

---

## 5. `internal/floor`'s write-tool superset is pinned against the real roster again

**What.** Closes `apogee-i5h.3`. `wave4WriteTools` (`internal/floor/toolnames.go:14-19`) decides whether a
call invalidates the read cache and whether an empty reply followed a write, but the test that
cross-checked it against every workspace-writing builtin died with
`internal/mechanisms/writedetection_test.go` (deleted in `6ba1322c`); `internal/floor/toolnames_test.go`
pins only a hand-written 12-row table naming 7 of the map's 13 entries. Export two symbols from floor —
`IsFileMutatingTool(name string) bool` (the existing private predicate at `toolnames.go:50`) and
`FileMutatingToolNames() []string` (a sorted copy, never the map) — and re-home the coverage test in
`internal/agent`, the ONLY package that imports both `internal/floor` and `internal/tools`. floor's
"never imports internal/tools" rule (`internal/floor/doc.go:13-15`) is untouched and this item says so in
the new test's doc comment. The test walks `tools.DefaultToolsWithHost("", tools.HostTools{Enabled:
tools.KnownToolNames(), …})`, skips `domain.IsReadOnly` tools, and classifies the rest through two
hand-maintained tables carried over verbatim from the deleted file (`workspaceWritingBuiltins`,
`writeCapableNonFileBuiltins`), failing on any built-in in neither.

**Regression guard.** The private predicate at `toolnames.go:50` STAYS under its own name and
`IsFileMutatingTool` merely wraps it — renaming `isFileMutatingTool` breaks its in-package callers
(`internal/floor/conversation.go:65,116`, `readcache.go:77`, `loopbreak.go:184`, `toolnames_test.go:38`).
Only the two classification TABLES come over verbatim: `stubAsker`, `stubPresenter` and `stubSkillLookup`
are already declared in package `agent` (`internal/agent/planmenu_test.go:26,32`,
`construct_test.go:376`), so the re-homed test fills `tools.HostTools`'s Asker/Presenter/SkillLookup from
those existing stubs rather than carrying its own — redeclaring any of them fails the whole
`internal/agent` test build. Item 5 lands a new file in `internal/agent`, whose ~85 `package agent` test
files share one test-identifier namespace, so every helper, fixture and test name it carries over is
grepped against that whole package first — not only against the two tables the item names. The
write-tool roster is read live, never hand-copied: `internal/tools` gained `task_list` after this plan's
base (registry.go at commit `805fda78`), so item 5's coverage test enumerates the roster from the
registry at run time and fails when a new tool is added without a floor verdict — a pinned list would
have gone stale within a day of this plan being written.

**Files:** `internal/floor/toolnames.go`, `internal/floor/doc.go`, `internal/floor/toolnames_test.go`, `internal/agent/writedetection_test.go`

**Tests.** The re-homed coverage test; plus the existing floor table extended to name all 13 map entries
rather than 7, so the exported predicate is pinned in its own package too.

**Acceptance.** `go test ./internal/floor/ ./internal/agent/ -run 'Write|ToolName'`;
`go vet ./internal/floor/ ./internal/agent/`;
`grep -c '"github.com/airiclenz/apogee/internal/tools"' internal/floor/*.go` is 0 — the IMPORT path, not
the prose: a bare `internal/tools` count is 1 in each of `conversation.go`, `doc.go` and `toolnames.go`
at this base and would fail before and after.

**Commit:** `test(floor): the write-tool superset is checked against the real tool roster again`

---

## 6. The Validated-set applying rung gets an end-to-end test

**What.** Closes `apogee-i5h.4`. Depends on item 2. With both catalogues empty (`shipped.json` is `[]`;
the Mechanism catalogue registers nothing), `validated.Validate` (`internal/validated/validate.go:79`)
rejects every non-empty member set, so `setApplied` (`cmd/apogee/validatedsets.go:169`), `appliedNotice`
(`:257-265`) and the canonical member sort (`:209`) are unreachable by any shipped configuration and are
pinned only as hand-built decisions in `internal/probe/model_test.go:99-131`. Add ONE test in
`cmd/apogee` that walks the whole user path: swap in a catalogue row (item 2), write a probe record with
`library.SaveProbeRecord` so the fingerprint resolves at `domain.ConfidenceMedium`
(`internal/library/fingerprint.go:90-92`), write a user entry naming that row with `writeLabEntry`
(`cmd/apogee/validatedsets_test.go:305-322`), then call `resolveValidatedSet` and assert three things
the unit pins cannot: the returned set is the entry's members in sorted order, the notices carry
`appliedNotice`'s sentence with the right count and source, and `apogee.BuildMechanisms(validCfg(t), set)`
constructs. Reuse the existing helpers; add no new fixture shape.

**Regression guard.** Swap in TWO rows, not one, and write the entry with its members OUT of canonical
order: a one-member set makes the canonical sort at `cmd/apogee/validatedsets.go:209` unobservable, so the
test could not fail against an unsorted implementation it claims to pin. The new test defers the restore
before any `t.Cleanup`, and a parallel swap would race
`TestResolveValidatedSet_IdentityAliasReplacesTheConfidenceGate` (`cmd/apogee/validatedsets_test.go:80-102`),
which asserts the EMPTY catalogue skips an entry naming `lab_row`. Item 6's test must not call
`t.Parallel()` — it registers its row through `mechanisms.SwapCatalogue`, which swaps a package-level map
that item 2 documents as deliberately not concurrency-safe.

**Files:** `cmd/apogee/validatedsets_test.go`

**Tests.** The walk described above; plus one negative case pinning that the SAME entry with the
catalogue un-swapped lands on `setSkipped`, so the test proves the seam is what opened the rung.

**Acceptance.** `go test ./cmd/apogee/ -run 'ValidatedSet'`; `go vet ./cmd/apogee/`;
`grep -c 'appliedNotice\|AUTO-APPLIES\|applied' cmd/apogee/validatedsets_test.go` is ≥ 1.

**Commit:** `test(validated): the applying rung is walked from record to enable list`

---

## 7. A failed tool call says one word and keeps its whole message

**What.** Recast at the regression check (2026-09-03). Closes `apogee-b3u`. A failed call's summary is `errorSummaryPrefix + firstLine(content)`
(`internal/tui/toolview.go:1171`) with no bound, and `leaderRowIn` reserves the slot at full width BEFORE
the target gets a budget (`internal/tui/toolleader.go:168-177`), so a long error eats the tool name down
to ` …` or drops it entirely (`:183-185`) — no floor protects it, because the ~15-cell `promoteMinTargetCells`
guard only fires for `quoted` summaries (`toolleader.go:88-95`, `toolview.go:945-947`) and a
`namedSummary` error is never quoted. Expanded is no better: an expanded member's first row is still a
leader row (`toolblock.go:311`), and a failure that took the fallback branch sets no `Details` at all, so
`hides` is false (`toolblock.go:274`) and there is nothing behind the clip. Change the fallback branch of
`absorbFailure` (`toolview.go:1163-1172`) only: the summary becomes the bare word `error`, and the whole
content — not `firstLine` — becomes the body via `tv.Details.with(outputBody(content))`, which wraps at
paint time and is never clipped (`toolbody.go:104-113`). The three per-tool `failure` hooks keep their
short words (`error: exit 3`) and their existing bodies untouched. `denied`, `cancelled` and
`interrupted — the run did not finish` (`toolview.go:151-153`) are not `absorbFailure`'s and do not change.
A failed message that moves into the body is spelled as the TOOL wrote it, per the quoted-body rule
(`toolview.go:845-858`): `shortenPaths` respells the summary against the workspace (`:868-870`) and
deliberately never touches a body, so `read_file` failing on `/ws/missing.go` reads `error: missing.go`
today and carries the absolute path in the body after — the item's **What** states that, it is not a
defect to fix here. Amend the canon spec in the same commit, scoped to the FALLBACK branch alone:
`docs/layout/tool-layout.md:80` keeps its red `error: …` for the three hooked tools and gains the bare
verdict word beside it, and `:111` keeps its aspirational "the right slot always prints whole" wording
with the failure slot named as the one case that now always does.

**Regression guard.** Add the bare word `error` to the failure vocabulary beside
`deniedSummary`/`cancelledSummary` (internal/tui/toolview.go:150-154) and match it in `failedSummary`
(toolleader.go:325-331), so a failed row keeps its red tone, a faulted delegation keeps its ✗ instead of
gaining the done ✓, and `failedCalls` keeps counting "N errors"; changing `absorbFailure` alone is the
regression. The transcript's ORPHAN branch (transcript.go:1255-1260) is reworded identically in this
item — one failure wording on screen — and `TestTranscriptToolResultError` is edited with it. The
per-line 160-rune flood cap in `clipDetail` (toolregistry.go:1517-1522, `detailClipRunes` in
textutil.go:42-46) stays: the **What** names it as a known limit on a single long error line, and the
item's new test uses multi-line short-line output so it passes as described. `**Files:**` gains
`cmd/apogee/e2e_outcome_test.go`, `internal/tui/toolbranch_test.go`, `internal/tui/toolshape_test.go` and
`internal/tui/transcriptbridge_test.go`, and **Tests** names each pinned sentence re-anchored on the new
slot word: the faulted-delegation row and `assertErrorTone` (e2e_outcome_test.go:76-79, :230-244),
toolbranch_test.go:1219-1225, toolshape_test.go:135-141, the live/replayed "2 errors" equality
(transcriptbridge_test.go:500-551) and both fallback cases of
`TestPresentToolCallFailedSubprocessNamesItsExitCode` (toolpresent_test.go:604-612). `absorbFailure`'s
contract comment (toolview.go:1155-1158) and toolpresent_test.go:552-556 record the failure sentence as
the floor line; this item supersedes that explicitly under the 2026-09-03 ratified call on failed tool
rows, and rewrites that comment to say so. The enumerated test goldens are examples, not the scope
boundary: the rule is that EVERY site pinning a failed call's wording moves with this item, found by
grepping the tree for the failure sentence and its prefix (`grep -rn 'error: ' internal/tui cmd/apogee
--include='*_test.go'` plus the golden/testdata trees) before the item is closed — a site the plan did
not list is in scope, never a deferral. `delegationFailure` declines when `steered == 0`
(internal/tui/toolregistry.go:816-820), so every unsteered failed delegation takes the fallback branch
and six further `subagentblock_test.go` goldens are already known to be inside that rule (:124, :353,
:473, :682, :736, :1520).

**Files:** `internal/tui/toolview.go`, `internal/tui/toolleader.go`, `internal/tui/transcript.go`, `internal/tui/toolpresent_test.go`, `internal/tui/transcript_test.go`, `internal/tui/subagentblock_test.go`, `internal/tui/toolbranch_test.go`, `internal/tui/toolshape_test.go`, `internal/tui/transcriptbridge_test.go`, `cmd/apogee/e2e_outcome_test.go`, `docs/layout/tool-layout.md`

**Tests.** `TestPresentToolCallErrorResult` (`toolpresent_test.go:534-553`) and
`TestTranscriptToolResultError` (`transcript_test.go:1041-1047`, through the reworded ORPHAN branch) move
from the sentence to the word plus a body carrying the sentence whole;
`TestSpanlessSubAgentHeadsGroupWithEachOther`
(`subagentblock_test.go:546-576`), which today pins the dropped-target shape as intended, asserts the
target survives; a new case drives a multi-line error of SHORT lines and asserts every line reaches the
expanded body. Every remaining pinned failure sentence is re-anchored on the new slot word: the
faulted-delegation row and `assertErrorTone` (`cmd/apogee/e2e_outcome_test.go:76-79`, `:230-244`),
`toolbranch_test.go:1219-1225`, `toolshape_test.go:135-141`, the live/replayed "2 errors" equality
(`transcriptbridge_test.go:500-551`), and both fallback cases of
`TestPresentToolCallFailedSubprocessNamesItsExitCode` (`toolpresent_test.go:604-612`). The six further
`subagentblock_test.go` goldens that pin the fallback slot move with them — `:124`, `:353`, `:473`,
`:682`, `:736`, `:1520` — and the guard's grep is what finds any the plan missed.

**Acceptance.** `go test ./internal/tui/ -run 'Error|Failure|Tool'`; `go test ./cmd/apogee/ -run 'E2E'`;
`go vet ./internal/tui/`; `grep -c 'error: ' docs/layout/tool-layout.md` reflects the amended text.

**Commit:** `fix(tui): a failed tool row says error and keeps the message in its body`

---

## 8. Knob 3 waits for the fix's card instead of the clock

**What.** Recast at the regression check (2026-09-03). Closes `apogee-h64`. `graphics/demo/tapes/hero.tape:95` is a fixed `Sleep 10s` that must land in
the sub-second gap between the fix's `Replace` card painting and the queued interjection being delivered,
while the run varies ~19s–29s end to end and slides that window by ~8s — measured hit rate about 1 take in
7, and the early-miss shape CANCELS the run. VHS 0.11.0 (the version installed on the recording machine)
has a screen-state conditional: `Wait[+Screen][@<timeout>] /<regexp>/`, default scope `Line`, default
timeout `15s`. Replace the `Sleep 10s` with `Wait+Screen@40s /\+1 -1/` — the fix card's own diffstat,
which is exactly what the knob comment already uses to tell the right card from the CHANGELOG one
(`+3 -0`). `+Screen` is load-bearing: the card row is not the last line. The timeout is generous because a
timeout is a LOUD failure (vhs exits non-zero and the take dies) where the old miss was silent. Rewrite the
knob-3 comment block (`:49-94`) accordingly: what it waits on, why `+Screen`, why the timeout, and that a
changed edit size means a changed pattern. Update `graphics/demo/README.md:116-125` — "Knob 3 is a coin
toss, not a setting" is superseded and the measurement paragraph becomes the record of why the wait
exists. Pin the tool: `README.md:13`'s bare `brew install vhs` gains "0.11.0 or newer — `Wait+Screen`
is required by `hero.tape`". Knobs 1 and 2 are NOT touched; neither is anything else in the tape.

**Regression guard.** Write the pattern with the card's real sign — `Wait+Screen@<timeout> /\+1 −1/`
using U+2212 (or `/\+1 [-−]1/`) — because `diffCounts` spells the stat with U+2212
(internal/tui/toolview.go:295-297, pinned as "+1 −1" at toolsummary_pin_test.go:143); the ASCII `-`
matches nothing on screen, so every take times out and exits vhs non-zero where today about one in seven
lands. Fix the same ASCII stat wherever the prose quotes the card — the rule, not a site list: every
diffstat the demo prose quotes off a card is spelled with U+2212, found by
`grep -n '[+-][0-9] -[0-9]' graphics/demo/tapes/hero.tape graphics/demo/README.md` (tape :50, :89 — the
`+3 -0` line in the same comment block — and :90; README :121, :122, :128). The tape's path is
`graphics/demo/tapes/hero.tape`, as `**Files:**` has it; `graphics/demo/hero.tape` does not exist. The
enumerated quotes are examples, not the scope boundary: the rule is that every place in the repo
spelling the card's diffstat in ASCII moves to U+2212 with this item, found by grepping for the stat
itself (`grep -rn '+1 -1' .` over `graphics/`, `docs/` and in-code comments) before the item is closed —
comment lines included.

**Files:** `graphics/demo/tapes/hero.tape`, `graphics/demo/README.md`

**Tests.** None — a VHS tape has no test surface in this repo, and re-recording the GIF is out of scope.

**Acceptance.** `grep -c 'Sleep 10s' graphics/demo/tapes/hero.tape` is 0;
`grep -c 'Wait+Screen@40s' graphics/demo/tapes/hero.tape` is 1;
`grep -c 'coin toss' graphics/demo/README.md` is 0; `grep -c '0.11.0' graphics/demo/README.md` is ≥ 1.
The owner-run check, recorded as a NOTES line rather than blocking the item: `vhs validate hero.tape`
exits 0 on the recording machine.

**Commit:** `fix(demo): knob 3 waits for the fix card rather than guessing at a clock`

---

## 9. `validated-sets:` gets a section that says what a Validated set is

**What.** Recast at the regression check (2026-09-03). Closes `apogee-t57.1`. The key parses `enable:` (bool, default true) and `alias:` (map, runtime
fingerprint label → entry key) at `internal/config/config.go:877-894`, carries two `/settings` rows
(`internal/config/registry.go:644-659`), and the manual uses "the Validated set" as a known term in four
places — `configuration.md:187`, `commands.md:261`, `:286`, `probe.md:39`, `:41` — while defining it
nowhere. (The bead cites `configuration.md:172-173`; that is the `APOGEE_MODEL` paragraph. Line 187 is the
real site.) Add a Pattern-1 section headed `## Per-model Mechanism sets — `validated-sets:`` — that
heading verbatim, which is what the acceptance grep counts — immediately BEFORE
`## Keeping the session store bounded — `sessions:``: what a Validated set IS, in the reader's terms and drawn from
`CONTEXT.md:1646-1669` — a per-model enable set proven non-inferior to Bypass on that model, applied
whole or not at all, automatically at ≥ medium fingerprint confidence and merely offered below it — that
the shipped roster is empty since v0.20.0 so a stock install sees none, that a user's own
`~/.apogee/validated/*.json` entry still resolves and sheds retired members rather than being skipped
whole, that a non-empty `mechanisms:` is manual control and suppresses the set, and that a dangling alias
target is a startup error. One `yaml` fence in the house form showing `enable:` and one `alias:` pair.
Link `[ADR 0016](../adr/0016-*.md)` by its real filename, read from the tree.

**Regression guard.** Insert the new `validated-sets:` section immediately BEFORE
`## Keeping the session store bounded — `sessions:`` (docs/manual/configuration.md:653 at BASE), never
at 448/449: a heading there re-parents the skill `triggers:` prose (449-473) and the compaction, pruning,
`context-window`, effort, `ui.stall-after`, `cursor-shape` and `editor` paragraphs (475-652) under
"Per-model Mechanism sets". Plan-wide: every later item editing `docs/manual/configuration.md` or
`docs/manual/commands.md` (items 10, 12, 13, 14, 15, 16, 17) re-locates its insertion point by heading
text, never by the line numbers this plan cites at BASE `94c36e88` — item 9's insertion shifts them all.
The unheaded 200-line run at configuration.md:449-652 stays under `## Skill suggestions` unless an item
carving a heading into it states in its own text which paragraphs travel with the new heading and which
stay above it.

**Files:** `docs/manual/configuration.md`

**Tests.** None — prose. Item 17 is what pins it mechanically.

**Acceptance.** `grep -c 'validated-sets' docs/manual/configuration.md` is ≥ 2;
`grep -c '^## Per-model Mechanism sets' docs/manual/configuration.md` is 1;
`go test ./cmd/apogee/ -run 'Docs|Manual'`.

**Commit:** `docs(manual): validated-sets: gets a section that says what a Validated set is`

---

## 10. The `ui:` block's four undocumented keys

**What.** Closes `apogee-t57.2`. Four keys parse, carry `/settings` rows and are documented nowhere:
`ui.spinner` (enum `snake` | `glitter` | `classic`, default `snake`; an unknown name is a startup error),
`ui.spinner-color` (bool, default true, deliberately independent of `ui.spinner` so all six combinations
are valid), `ui.show-scrollbar` (bool, default true; gates the bar AND the column it hangs in, in the
transcript and every popup, and is process-constant), and `ui.color-scheme` (name, default `dark`;
built-ins `dark` and `light` plus any `~/.apogee/schemes/<name>.yaml`, which shadows a built-in of the same
name; an unknown NAME warns and falls back rather than refusing, but a path is refused). Parse sites
`internal/config/config.go:2152-2227`, defaults `:335-347`, registry `internal/config/registry.go:526-577`.
Promote the existing `## Skill suggestions — `ui.skill-suggestions:`` section (428-448) to
`## The terminal UI — `ui:``, keep its skill-suggestions prose intact as the first sub-topic, and add the
four keys plus a cross-reference to `ui.stall-after` (620-624, which stays where it is). Give
`ui.inspector` one sentence too — the manual names it once in passing (`commands.md:32`) and never says it
is off by default or that it takes effect at the next start.

**Regression guard.** `ui.show-scrollbar` is NOT process-constant: a `/settings` commit applies it live —
`internal/tui/settingsapply.go:261-266` sets `m.opts.HideScrollbar` and calls `m.layout()` — and
`registry.go:538-542` marks it Editable with none of the next-start note `ui.inspector` carries at
`:562-579`. Drop "process-constant", say the flip is live like `ui.skill-suggestions`, and keep "takes
effect at the next start" for `ui.inspector` alone. Do not state 428-448 as the section's extent either:
at BASE `## Skill suggestions` runs 428-**652**, and 428-448 becomes a section only once item 9 inserts a
heading — phrase this item as "rename the heading at 428 and add the four keys after the ADR 0061
paragraph (448)", re-located by heading text after item 9 lands.

**Files:** `docs/manual/configuration.md`

**Tests.** None — prose.

**Acceptance.** For each of `ui.spinner`, `ui.spinner-color`, `ui.show-scrollbar`, `ui.color-scheme`,
`ui.inspector`, `grep -c '`<key>`' docs/manual/configuration.md` is ≥ 1;
`grep -c 'skill-suggestions' docs/manual/configuration.md` is unchanged or higher;
`go test ./cmd/apogee/ -run 'Docs|Manual'`.

**Commit:** `docs(manual): the ui: block's four undocumented keys join the reference`

---

## 11. `auto-title:` and the two delegation-posture entry keys

**What.** Closes `apogee-t57.3` and `apogee-t57.4`. (a) `auto-title:` (bool, default true, file-only,
`config.go:732-735`) is described in `sessions.md:20-42` and `headless.md:50-52` but absent from the
configuration reference, which is where a reader looks a key up by name. Add a short paragraph beside
`## Keeping the session store bounded — `sessions:`` (653-677) that names the key, its default and its
two jobs — naming a new session from its first prompt, and naming an unnamed delegation — and links
`sessions.md` for the fallback ladder rather than restating it. (b) The `servers:` entry inventory at
`configuration.md:698-709` omits `bypass:` and `mechanisms:`, which both parse per entry
(`config.go:1664-1665`) and are named in the starter template
(`internal/config/defaults/config.yaml:212-213`). Add them as a bolded lead paragraph in the run of them,
after the `sub-agents-server:` block that ends at 786: both describe what DELEGATIONS to that server run
as and apply only while that entry is the `sub-agents-server:` target; an absent `bypass:` leaves the
child inheriting the parent's live flag; a PRESENT `mechanisms:` map is the child's ENTIRE catalogue
(replace-whole, so a map of all-false arms nothing), and an unknown id is a startup error naming the
entry. Both keys are legal on any entry by design — a posture on an entry nothing delegates to is a
description, not a defect.

**Files:** `docs/manual/configuration.md`

**Tests.** None — prose.

**Acceptance.** `grep -c 'auto-title' docs/manual/configuration.md` is ≥ 1;
`grep -c 'replace-whole\|entire catalogue' docs/manual/configuration.md` is ≥ 1;
the entry inventory names `bypass` and `mechanisms`; `go test ./cmd/apogee/ -run 'Docs|Manual'`.

**Commit:** `docs(manual): auto-title: and a servers: entry's delegation posture join the reference`

---

## 12. The `model-profiles:` tool-call and thinking axes

**What.** Closes `apogee-t57.5`. `tool-call-format:` and `tool-call-pattern:` are undocumented, and
`thinking:`'s `style:` / `start:` / `end:` are alluded to at `configuration.md:579` ("orthogonal to
`style:` beside it") without ever being specified — so a profile for a non-native-tool-call model cannot
be written from the manual. Add a `##` section after the effort prose (ends 581) covering the axes as the
code defines them: `tool-call-format:` takes exactly `native` (the default, and what an omitted key
means), `markdown-fenced` or `custom-regex` (`internal/domain/config.go:540-546`); a text format is parsed
from the model's VISIBLE content rather than the wire's structured `tool_calls`; `tool-call-pattern:` is a
Go regexp, mandatory under `custom-regex` and refused under every other format, needing named capture
groups `name` and `args` with dot-matches-newline already applied, and accepting the JavaScript
`(?<name>…)` spelling; `thinking.style:` takes `none` (default), `delimited` or `harmony` (the gpt-oss
channel form), and `delimited` requires BOTH `start:` and `end:` set to the literal tokens the model
emits. Say that each axis resolves independently, and that every one of these is a startup error when
malformed — quoting no error strings, which belong to the code. One `yaml` fence showing a `custom-regex`
profile and a `delimited` one, drawn from `internal/config/defaults/config.yaml:1002-1015`.

**Regression guard.** `internal/config/defaults/config.yaml:1002-1015` holds a `delimited` profile and a
`markdown-fenced` one and NO `custom-regex` profile — `tool-call-pattern:` appears in that file only as
prose at `:957-958` — so cite that range for the `delimited` half alone and write the `custom-regex` half
fresh against `validateToolCallPattern` (`internal/config/config.go:2466-2481`). The effort prose does not
end at 581: the dial-detection/picker prose runs 583-594 and `effort-dialect:` 596-620, so a `##` at 582
would also swallow `ui.stall-after` (620-624), `cursor-shape:` (627-634) and `editor:` (636-651) — the
very keys item 10 has just cross-referenced as living elsewhere. Place the `## Model profiles` heading
after 620 (equivalently, immediately before `## Keeping the session store bounded`), re-located by heading
text after item 9 lands.

**Files:** `docs/manual/configuration.md`

**Tests.** None — prose.

**Acceptance.** For each of `tool-call-format`, `tool-call-pattern`, `markdown-fenced`, `custom-regex`,
`delimited`, `harmony`, `grep -c` in `docs/manual/configuration.md` is ≥ 1;
`grep -c '^## Model profiles' docs/manual/configuration.md` is 1; `go test ./cmd/apogee/ -run 'Docs|Manual'`.

**Commit:** `docs(manual): the model-profiles: tool-call and thinking axes are specified`

---

## 13. The dangerous-action guard reaches the manual

**What.** Recast at the regression check (2026-09-03). Closes `apogee-t57.6`. The two-tier guard runs ahead of the mode disposition in EVERY mode and
can only tighten a call (`internal/security/dangerous.go:16-31`, `guard.go:109-130`), yet `docs/manual/`
has no occurrence of "dangerous" — it lives only in `docs/adr/0012` and `docs/design/`. Add a `###`
subsection under `## Auto mode's blast radius` (1254): Tier 1 hard-refuses outright in every mode with no
per-call override (`rm -rf` of a root, home or system path; fork bombs; writes to `~/.ssh`, credential
and persistence files, `.git/hooks`), and Tier 2 forces the approval prompt even in Auto for idioms that
are sometimes legitimate (`curl … | sh`, `sudo`, a terminal command writing under apogee's own
`~/.apogee` control plane) — and a forced gate carries no cache key, so
"Always allow this session" cannot remember it. State ADR 0012's own framing without softening it: this is
a footgun-guard, not a security boundary — it catches a small model's obvious catastrophic mistakes, is
trivially bypassable by anything determined, and is never what makes `confine-to-workspace: false` safe;
only a VM is. Say the ruleset is built into this build with no config key yet, and attribute the
add-only/no-remove asymmetry — global config may add or remove, project config may only add — to
ADR 0012 as the shape a merge takes when keys land, not as shipped wiring. Quote no rule patterns beyond the illustrative handful
above; the ruleset is `internal/security/rules.go` and moves.

**Regression guard.** Do not tell the reader the ruleset is editable: `internal/security/guard.go:43`
always seeds `DefaultDangerousActionGuard()`, `MergeDangerousRules` (internal/security/rules.go:260) has
no caller outside `internal/security`, and no config key or `/settings` row names it. The manual says the
dangerous-action ruleset is built into this build with no config key yet, and attributes the
add-only/no-remove asymmetry to ADR 0012 as the shape a merge takes when keys land — citing
docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md:91-92, which records
those semantics as a decision, not as shipped wiring. Item 13 yields to that ADR; it does not supersede
it.

**Files:** `docs/manual/configuration.md`

**Tests.** None — prose.

**Acceptance.** `grep -ci 'dangerous-action' docs/manual/configuration.md` is ≥ 1;
`grep -c 'footgun\|not a security boundary' docs/manual/configuration.md` is ≥ 1;
`go test ./cmd/apogee/ -run 'Docs|Manual'`.

**Commit:** `docs(manual): the dangerous-action guard is documented where the blast radius is`

---

## 14. What an approval actually covers

**What.** Closes `apogee-t57.7`. `commands.md:87-93` documents only the arming delay; three facts a user
must know to use the pane are stated nowhere. Add a `##` section to `commands.md` after the fold prose
(ends 114): (a) **"Always allow this session" is scoped to the CALL, not the tool** — the memory keys on
the tool name plus a digest of the call's arguments (`internal/agent/resolution.go:630-648`), so allowing
`npm test` does not clear `npm run build`, which is deliberate; (b) **it is honoured across the whole
sub-agent tree** — there is one memory per agent tree hanging off the approver seam
(`internal/agent/approvalcache.go:9-23`), so an allow granted inside a sub-agent clears the prompt for its
parent and siblings and outlives the child that earned it, and the memory is in-process only, never
persisted, so a resumed session starts empty; (c) **MCP grants are server-grain** — approving one of a
server's tools clears its siblings for the session (`resolution.go:624-642`), which the pane says on the
frame in its own words. Keep the existing arming-delay paragraph where it is and cross-reference it rather
than moving it. Cross-link from `configuration.md:309-311` (the MCP approval sentence).

**Regression guard.** The item's only content-bearing acceptance grep already passes at BASE:
`grep -c 'sub-agent\|server-grain\|arguments' docs/manual/commands.md` is 12 today (`commands.md:95-108`
says "sub-agent" repeatedly), so it certifies nothing about the new section. Pin the facts by phrase
against their real baselines instead — `server-grain` is 0 at BASE and must reach ≥ 1, and `arguments` is
already 1 at BASE so the new section must take it to ≥ 2 — or scope each count to the new section.

**Files:** `docs/manual/commands.md`, `docs/manual/configuration.md`

**Tests.** None — prose.

**Acceptance.** `grep -c '^## Approving a call' docs/manual/commands.md` is 1;
`grep -c 'server-grain' docs/manual/commands.md` is ≥ 1 (0 at BASE);
`grep -c 'arguments' docs/manual/commands.md` is ≥ 2 (1 at BASE);
`go test ./cmd/apogee/ -run 'Docs|Manual'`.

**Commit:** `docs(manual): what an approval actually covers`

---

## 15. How a diff and a near-miss suggestion read

**What.** Closes `apogee-t57.8` and `apogee-t57.9`. (a) `docs/manual/commands.md` contains no occurrence of
"diff", though five blocks render one — `edit_existing_file`, `single_find_and_replace`,
`multi_find_and_replace`, `view_diff`, `git_diff_range`. Say that the same regions paint two ways: two
panes side by side when each pane can give the code ~40 columns after its number gutter and marker
(in practice a terminal around 100 columns wide), and a stacked reading below that — the information is
identical, only the arrangement differs, the answer is asked again at every paint from THIS body at THIS
width, so a resize can flip the reading, and a body never paints half one way and half the other.
Removed rows wear `-` on a red band, added rows `+` on a turquoise band — turquoise, not green, so the
pairing survives red-green-weak vision — and the marker, never the colour alone, is what says which way a
line went. Long lines wrap rather than clip. Link `docs/layout/split-diff-layout.md` as the canon.
(b) `read_file`, `list_dir`, `grep` and `find_files` answer a miss with a `did you mean:` clause listing
up to five siblings of the missing name that share its prefix, case-insensitively, sorted, spelled the
way the caller wrote the path; a fence refusal never gains one, because a suggestion there would read as
absence and hide the refusal.

**Regression guard.** SIX blocks render a diff, not five: `write_file` attaches EditRegions at apply time
exactly like the edit tools (`internal/tools/write_file.go:63-67,113`; `regions.go:24`, "the four writing
tools"), so an overwrite or a create paints the same split diff and the manual's closed list would say it
does not. Add it, or state the RULE instead of a list — every block whose tool recorded or printed
regions (the four writing tools, plus `view_diff` and `git_diff_range`,
`internal/tui/toolregistry.go:282,377`). Name the heading in this **What** as items 14 and 16 do ("Add a
`## Reading a tool …` section"), since the acceptance requires
`grep -c '^## Reading a tool' docs/manual/commands.md` to be 1 and the What never says to add one.

**Files:** `docs/manual/commands.md`

**Tests.** None — prose.

**Acceptance.** `grep -ci 'diff' docs/manual/commands.md` is ≥ 3;
`grep -c 'did you mean' docs/manual/commands.md` is ≥ 1;
`grep -c '^## Reading a tool' docs/manual/commands.md` is 1; `go test ./cmd/apogee/ -run 'Docs|Manual'`.

**Commit:** `docs(manual): how a diff and a near-miss suggestion read`

---

## 16. What the status line reports while a run is live

**What.** Closes `apogee-t57.10`. The manual names the status line only for the esc-stop hint
(`commands.md:71-73`) and the `quiet` qualifier (`configuration.md:620-624`). Add a `##` section to
`commands.md` beside the stop-hint prose covering what the line actually reports while a run is live:
the left slot is an activity phrase plus an elapsed clock (`thinking · 4s`), the phrase vocabulary being
thinking, responding, the tool's own label, retrying, compacting, stopping and working; with delegations
running the row is THEIRS and not the parent's, because a parent inside a delegation is doing nothing of
its own to report — exactly one live delegate keeps its own phrase under its own name
(`repo-scout · reading`), and two or more merge into one count (`2 sub-agents · working`) counted from the
oldest live child's clock, so the number never restarts when a sibling emits (`internal/tui/model.go:3386-3421`).
Then the `· N tok/s` readout: the last completion's server-reported token count over that turn's
generation window, shown as a whole number and hidden entirely below one token per second — which is also
how an unmeasurably short window reads, deliberately shown as no reading rather than an invented one
(`model.go:3246-3257`, `fold.go:29-34`). Cross-reference `ui.stall-after` rather than restating it.

**Files:** `docs/manual/commands.md`

**Tests.** None — prose.

**Acceptance.** `grep -c '^## The status line' docs/manual/commands.md` is 1;
`grep -c 'tok/s' docs/manual/commands.md` is ≥ 1;
`grep -c 'sub-agents' docs/manual/commands.md` is ≥ 1; `go test ./cmd/apogee/ -run 'Docs|Manual'`.

**Commit:** `docs(manual): what the status line reports while a run is live`

---

## 17. The manual gains a drift gate over the settings registry

**What.** Recast at the regression check (2026-09-03). Depends on items 9–16. Nothing cross-checks `internal/config`'s settings registry against
`docs/manual/configuration.md`, which is how ten keys drifted at once. Add
`TestManualDocumentsEverySettingsKey` in `cmd/apogee`, modelled on the two gates that already exist —
`TestManualListsEveryKnownToolName` (`internal/tools/manual_drift_test.go:18`) and
`TestManualListsEveryEnvironmentOverride` (`cmd/apogee/docs_env_test.go:66`), whose path-constant and
failure-message shapes it copies. It walks every `Path` in the registry's settings table and asserts the
key appears back-ticked somewhere in `configuration.md`, with a single named allowlist for keys
deliberately documented on another page — each allowlist entry carrying a one-line reason naming that
page, so the list is auditable rather than a silencer. The allowlist is populated from what is actually
true after items 9–16 land, never speculatively: if a key outside those items turns out undocumented,
document it here in one sentence rather than allowlisting it, and note that in the item's NOTES. The
keys expected to arrive undocumented are `ui.inspector` (item 10 covers four `ui:` keys, not this one),
`auto-compact`, `prune-tool-results`, `remember-model` and `context-files.enable` — item 17 writes each
into `docs/manual/configuration.md` beside its own block, so this is doc-writing plus a gate, never
gate-only work.

**Regression guard.** The gate's predicate accepts the spelling the page actually uses, not only the
dotted `Path`: a key counts as documented when its dotted path appears back-ticked, OR its leaf segment
appears back-ticked, OR it is spelled `<leaf>:` inside a fenced block beneath its own block heading.
BOTH back-tick arms accept an optional trailing `:` inside the back-ticks (match `` `<path>` `` or
`` `<path>:` ``, and the same for the leaf) — that colon is the manual's house spelling
(`` `mcp-servers:` `` occurs 7 times at configuration.md:146,203,237,…; `` `mcp-servers` `` occurs 0
times), so without it 14 correctly documented keys fail every arm and the gate goes red on a correct
tree. Verify the predicate against `config.KeyRegistry` (internal/config/registry.go:174) over today's
`configuration.md` before the item is closed — the 13 keys already documented under another spelling
(`read-cache`, `tool-result-cap`, `tool-call-repair`, `tool-loop-breaker`, `tool-use-enforcer`,
`empty-response-recovery` at :62-63; the four `present.*` keys at :1240-1244; `sessions.max-age`/`max-count`
at :667-668; `context-files.names` at :1191) must pass without an allowlist entry, since the
allowlist is only for keys documented on another page. `context-files.enable` is NOT on that must-pass
list: its only spelling is the back-ticked span `` `enable: false` `` (configuration.md:1193), which
matches no arm, so item 17 spells the key out in the `context-files:` paragraph rather than widening the
predicate for it. Where the gate finds a registry key genuinely undocumented after items 9-16 land —
`ui.inspector`, `auto-compact`, `prune-tool-results`, `remember-model` and `context-files.enable` are the
candidates a round-2 reviewer named — item 17 documents it in one sentence in
`docs/manual/configuration.md` rather than allowlisting it; the allowlist stays reserved for keys
documented on another manual page. The item's own **What** and `**Files:**` say so, so an implementer
does not read the gate as gate-only work. The new `cmd/apogee/docs_settings_test.go` REUSES
`manualConfigPath` and `firstSentence` from `docs_env_test.go` (same `package main`); redeclaring either
fails to compile the whole test package.

**Files:** `cmd/apogee/docs_settings_test.go`, `docs/manual/configuration.md`

**Tests.** The gate itself, plus a negative case proving it fails on a key absent from both the manual and
the allowlist (a fabricated path fed to the same predicate, never a mutation of the real registry).

**Acceptance.** `go test ./cmd/apogee/ -run 'ManualDocumentsEverySettingsKey'`; `go vet ./cmd/apogee/`;
`go test ./internal/tools/ -run 'Manual'` still passes.

**Commit:** `test(docs): every settings key is named in the configuration reference`

---

## 18. The closed entries leave the issue register

**What.** Depends on every item above. The register (`bd`) holds OPEN work: a resolved item is CLOSED
there, and its record lives in `CHANGELOG.md` under `[Unreleased]` — closing a bead writes no changelog
entry, so do both. Close `apogee-5qe` with `apogee-5qe.1` and `apogee-5qe.2`; `apogee-i5h` with
`apogee-i5h.1`–`.4`; `apogee-t57` with `apogee-t57.1`–`.10`; and the two standalone beads `apogee-b3u`
and `apogee-h64`. Give every `bd close` a `--reason` naming this plan. Beads are addressed by ID and never
by list position: `bd show <id>` each one first and confirm the title matches the entry this plan claims
to close — the register was migrated out of the deleted `ISSUES.md` on 2026-09-03 and a stale ID would
close the wrong work. Closing an epic does not close its children; close each child explicitly. If any
bead was NOT delivered by items 1–17, it stays OPEN and this item says so in a dated NOTES line — in
particular, if item 1's reproduction found the pre-commit sweep still live and fixed it, say that in the
`CHANGELOG.md` entry as a fix rather than as a note.

**Regression guard.** `bd close` rewrites the TRACKED export `.beads/issues.jsonl` (auto-export is on,
`.beads/config.yaml:68-69`, and `export.git-add` is `false`, so the hook stages nothing), so with
`**Files:** none` this item would commit `CHANGELOG.md` alone and leave the committed register showing
every bead open. Name `.beads/issues.jsonl` in **Files:**, flush and stage it in this commit (`bd export`,
then `git add .beads/issues.jsonl`), and add the acceptance line
`jq -r 'select(.id=="apogee-t57.10").status' .beads/issues.jsonl` prints `closed`. Item 18 verifies that
the committed `.beads/issues.jsonl` really carries all 22 ids with a closed status before it commits, and
refreshes the export explicitly when it does not — item 1 may have turned beads' automatic export off, and
a silently stale committed register defeats the file's only purpose.

**Files:** `.beads/issues.jsonl`, plus the `CHANGELOG.md` `[Unreleased]` entry the closeout writes; the rest is register state.

**Tests.** None.

**Acceptance.** For each of `apogee-5qe`, `apogee-5qe.1`, `apogee-5qe.2`, `apogee-i5h`, `apogee-i5h.1`,
`apogee-i5h.2`, `apogee-i5h.3`, `apogee-i5h.4`, `apogee-t57`, `apogee-t57.1`–`apogee-t57.10`, `apogee-b3u`
and `apogee-h64`, `bd show <id> --json | jq -r '.[0].status'` prints `closed`;
`jq -r 'select(.id=="apogee-t57.10").status' .beads/issues.jsonl` prints `closed`, and the same check over
all 22 ids in the committed export passes.

**Commit:** `chore(issues): the residues, the manual gaps and the two defects close in the register`

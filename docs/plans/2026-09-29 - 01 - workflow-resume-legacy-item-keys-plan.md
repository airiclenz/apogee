# Workflow resume survives item-key formula changes — plan

**Goal:** A workflow folder written by any earlier apogee build resumes its finished items after
an upgrade, a rebuild or a session reload. A future change to the item-key formula cannot break
this silently. When finished work is redone because its inputs changed, the stage says so.
**Date:** 2026-09-29
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 1e3efe44

**Regression check (2026-09-29, 1e3efe44):**
- 1: guard folded (verify/merge suffix on the item draft; doc.go file-map line; Goal restated against the golden hex).
- 2: recast (stale guard; `prior` capture moved here; narrowed to fanouts; supersedes CHANGELOG.md:45 Unreleased sentence).
- 3: recast — new item inserted by ratified design call (merge/verify keys rebuilt under every older scheme).
- 4 (was 3): guard folded from decision (takes `prior` from item 2; redid count uses every scheme's key; renumbered).
- 2 (round 2): guard folded (manual `prompt:` sentence and changelog sidecar name the staleness exception; supersedes docs/manual/workflows.md:230-232 @1e3efe44).
- 3 (round 2): guard folded from decision (older-scheme output paths are pure joins, never Store.Path/MkdirAll) plus guards folded (fixture uses `prompt:` files and a source fanout with no `out:`; scheme 1's manifest keeps v0.23.4's verdict rule).
- 4 (round 2): guard folded from decision (note text `redid <n> finished item(s): their inputs changed since they ran`, covering upstream-result changes such as a merge after RerunFailed; header Notice call updated).

**Diagnosis (evidence):** session `20260929T163940Z-86f6c197`, folder
`20260929-183951-audit`: run 1 stored ground-truth's receipt under key `3887c0f2…`, the formula
without the prompt body (v0.23.4 and every build before `5f1cba20`). After a reload on a binary
rebuilt with `5f1cba20`, runs 2–3 keyed the same item `260381b0…`, found no receipt and re-ran
every stage. The plan hash still found the same folder; only the item keys moved.

**Sources:**
- `internal/workflow/runner.go` — `stageKeyBrief`, `prepareItems`, `openStatus`, `endStage`
- `internal/workflow/store.go` — `ItemKey`, `ReadReceipt`, `isValidKey`
- `git show v0.23.4:internal/workflow/runner.go` — scheme 1's `stageKeyBrief` (no `prompt_body`)
- commit `5f1cba20` — the change that orphaned scheme-1 receipts
- `docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md` D4
- `docs/manual/workflows.md` — the item-key paragraphs

**Ratified design calls** (user, 2026-09-29):
- **Robustness:** a numbered key-scheme registry, tried newest first on a miss, with a golden test
  per scheme. The formula-free item manifest was rejected.
- **Adoption:** an older scheme's folder is renamed to the current key. A current-key folder
  without an ok/partial receipt holds only unfinished work and is removed first. A current-key
  ok/partial receipt is never overwritten.
- **Staleness:** a receipt found under an older scheme is adopted even where that scheme did not
  cover an input the current one does (scheme 1: prompt-file contents).
- **Notice:** stage Note `redid <n> finished item(s): their inputs changed since they ran`, shown
  by `Format` and the `/workflows` detail.

**Standing requirements:**
- skills: coding-standards
- This machine (Raspberry Pi 5): run tests only as
  `GOMEMLIMIT=2GiB go test -count=1 -run <TestName> ./internal/workflow/`. Never race or cover a
  package pattern, and never run two test runs at once. Pass these rules to every sub-agent.

**Out of scope:**
- Session identity or scratch-dir movement on reload: verified correct.
- Plan-hash changes, background `ResumeWorkflows`, TUI rendering changes.
- Migrating folders outside a Run (no startup sweep).

## 1. Put the item-key formula behind a numbered, pinned scheme registry — ✅ DONE (2026-09-30)

NOTES (2026-09-30): itemDraft carries the round and the verify/merge key suffix but not the stage — prepareItems keys with the stage it is already handed (the same value each caller used to pass to stageKeyBrief, the verify `child` included).
NOTES (2026-09-30): the prompt file is now read while keying each item, not once per stage up front, so a verify or fanout stage with zero items and an unreadable prompt file no longer errors; error text for every item-bearing case is unchanged.
NOTES (2026-09-30): TestKeySchemeGolden pins two cases per scheme (fanout, and a verify-style suffix); scheme 1 values computed in a v0.23.4 worktree, scheme 2 in a HEAD (=1e3efe44 code) worktree. Extra TestKeySchemesAreNewestFirst guards the registry order.
NOTES (2026-09-30): store.go change is ItemKey's doc comment only (every scheme ends in ItemKey, so its encoding moves every golden key).

**What:** A behaviour-neutral refactor that makes every key formula ever shipped reproducible.
**Regression guard.** Verify and merge append `source.Key`+claim / the manifest to the brief
(stages.go:193,265), so the `itemDraft` carries the stage, the round and that suffix instead of a
finished `keyBrief`, and each scheme func takes the suffix. `doc.go`'s file map gains a
`keyscheme.go is …` line, or `TestDocMapNamesEveryFile` goes red.
**Goal:** `internal/workflow` has an ordered registry of key schemes, newest first:
- scheme 2: the brief with `prompt_body`, pinned by `TestKeySchemeGolden`'s hex;
- scheme 1: v0.23.4's key, the brief without `prompt_body`, pinned by `TestKeySchemeGolden`'s hex.

Each scheme computes a full item key. A golden test per scheme pins its exact hex output for a
fixed stage, item, context file and prompt file. Its failure message says: "the item-key formula
changed: add a new scheme and keep this one".
**Approach (assumed at the header base):**
- One unexported `keyScheme` type (id, and a func from stage, round, item, context files, prompt
  and workspace FS to a key). The package-level `keySchemes` slice is newest first. The current
  scheme is `keySchemes[0]`, and `prepareItems` keys every item through it.
- `stageKeyBrief` stays as scheme 2's brief. Scheme 1's brief is a separate func: the same struct
  without `PromptBody`.
- A scheme must never call anything a later change could alter without failing its golden test.
  The golden tests guard that.
- Record the rule in `internal/workflow/doc.go` (store.go paragraph): a change to any key input or
  encoding adds a scheme and never edits an existing one.
**Files:** internal/workflow/runner.go; internal/workflow/stages.go; internal/workflow/store.go; internal/workflow/keyscheme.go; internal/workflow/keyscheme_test.go; internal/workflow/doc.go
**Read first:** internal/workflow/runner.go — prepareItems, itemDraft, stageKeyBrief; internal/workflow/stages.go — verifyDraft, runMerge;
internal/workflow/store.go — ItemKey; internal/workflow/doc.go — file map; internal/workflow/docmap_test.go — TestDocMapNamesEveryFile
**Tests:**
- `TestKeySchemeGolden`: one sub-test per scheme with its hard-coded key.
  - Scheme 1's value is computed once with v0.23.4's code (`git show v0.23.4:…`).
  - Scheme 2's value is computed with the base tree's code.
- A test that `keySchemes[0]` equals the key `prepareItems` writes into status.json for the same
  fixture.
- `TestDocMapNamesEveryFile` stays green with `keyscheme.go` in `doc.go`'s file map.
**Acceptance:**
- `go build ./... && go vet ./internal/workflow/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'KeyScheme' ./internal/workflow/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/workflow/`
**Commit:** refactor(workflow): pin every item-key formula as a numbered scheme

## 2. Resume an item from the receipt an older key scheme stored — ✅ DONE (2026-09-30)

NOTES (2026-09-30): the stale guard treats a prior status line naming any key other than the older one, not just a current-scheme key, as admitting only when that key's folder holds no ok/partial receipt; the older-scheme walk continues past a receipt the guard rejects instead of stopping, so a line naming an even older scheme's key still adopts that one
NOTES (2026-09-30): prepareItems now resolves the receipt (and any adoption) before the output path, since outputPath's Store.Path pre-creates items/<key>/; a prior line whose key is not a valid item key counts as the item having finished elsewhere (redo, never adopt)
NOTES (2026-09-30): the docs/manual/workflows.md sentence says a pre-upgrade folder keeps its finished items; until item 3 lands this holds for fan-out stages only, not verify or merge

**What:** Recast at the regression check (2026-09-29). Depends on item 1. Fixes a regression from
`5f1cba20`: a folder written by v0.23.4 or any earlier build loses every finished item on resume.
This item's changelog sidecar replaces the Unreleased CHANGELOG.md:45 sentence ("items of a stage
that names a `prompt:` file … get new keys once across this upgrade, so re-running or resuming a
workflow folder started before it redoes those stages' finished items"): folders from before the
upgrade now resume their finished items.
**Regression guard.** Ratified design call (user, 2026-09-29) — **Stale guard:** an older scheme's
receipt is adopted only when the folder's prior status.json line for the same stage name, round and
label is absent, names that older key, or names a current-scheme key whose folder holds no
ok/partial receipt; otherwise the item last finished under a current-scheme key and older receipts
are known stale, so it is redone. The `prior []StageStatus` capture moves here from the notice item
(now item 4). Supersedes CHANGELOG.md:45 @1e3efe44 (Unreleased), via the changelog sidecar above.
**Staleness exception in the docs:** the `prompt:` key sentence in docs/manual/workflows.md and the
changelog sidecar both say a folder from before the upgrade keeps its finished items even when a
`prompt:` file was edited before its first run on this build. Supersedes docs/manual/workflows.md:230-232
@1e3efe44 ("edit it and a re-run of the recipe redoes that stage's finished items") for such folders.
**Goal:** An item of a stage whose key brief does not depend on upstream item keys (a fanout) with
no ok/partial receipt under the current scheme's key, but one under an older scheme's key that the
stale guard admits, is resumed (Resumed, no child spawned). Afterwards its folder is under the
current key. Merge and verify stages are item 3's.
**Approach (assumed at the header base):**
- `Runner.openStatus` also returns the found folder's stages from before it resets them.
  `runState` keeps them as `prior []StageStatus` (nil for a new folder).
- In `runState.prepareItems`, on a miss, walk `keySchemes[1:]` in order. For each distinct key,
  call `ReadReceipt`. The first ok/partial hit calls a new `Store.AdoptItem(id, fromKey, toKey)`.
- `AdoptItem` validates both keys (`isValidKey`). It removes a `toKey` folder with no ok/partial
  receipt, refuses one that has such a receipt, and renames `fromKey` to `toKey`.
- An adopt error is returned like any store error. The item's output path is computed from the
  current key after the rename.
- Before adopting, apply the stale guard against `prior`'s line for the same stage name, round and
  label.
**Files:** internal/workflow/runner.go; internal/workflow/store.go; internal/workflow/runner_test.go; internal/workflow/store_test.go; docs/manual/workflows.md
**Read first:** internal/workflow/runner.go — prepareItems, openStatus, outputPath; internal/workflow/store.go — ReadReceipt, isValidKey, itemDir;
internal/workflow/runner_test.go — TestRunnerRedoesItemsWhenThePromptFileChanges; docs/manual/workflows.md — `prompt:` key paragraph
**Tests:**
- Runner test: an ok receipt under the scheme-1 key of a prompt-file fanout item. `Run` spawns no
  child, marks it Resumed, and leaves `items/<scheme2Key>/receipt.json` present and
  `items/<scheme1Key>` gone.
- Same, plus an `items/<scheme2Key>/` folder holding only `transcript.jsonl` and a prior status
  line naming that scheme-2 key (the diagnosis case): adopted.
- A folder with an ok scheme-1 receipt AND an ok scheme-2 receipt recorded on the prior status
  line, prompt then edited: the item is redone, not adopted.
- Store tests: `AdoptItem` refuses invalid keys and never overwrites an ok/partial receipt.
- Docs: in `docs/manual/workflows.md`, the `prompt:` key paragraph says a folder from an older
  apogee still resumes its finished items, even when a `prompt:` file was edited before its first
  run on this build; the changelog sidecar says the same.
**Acceptance:**
- `go build ./... && go vet ./internal/workflow/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Scheme|Adopt' ./internal/workflow/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/workflow/`
**Commit:** fix(workflow): resume items whose receipt an older key scheme stored

## 3. Rebuild merge and verify keys under every older scheme — ✅ DONE (2026-09-30)

NOTES (2026-09-30): the per-scheme verdict rule is a `verdict` field on each keyScheme (scheme 1: `verdictOfAnyStatus`, v0.23.4's rule; scheme 2: the current `verdictOf`), since the verdict a merge manifest names is a key input; `ItemResult` keeps older schemes in an unexported `older []schemeItem` (key, output, verdict), read through `underScheme`
NOTES (2026-09-30): the four new goldens (verify item and merge item under schemes 1 and 2) were cross-checked by running the equivalent computation against throwaway worktrees of v0.23.4 and 1e3efe44 — all four matched; the older-scheme adoption walk now runs for every child-running stage kind, not just fanouts

**What:** Recast at the regression check (2026-09-29) — new item, inserted by a ratified design
call. Depends on item 2. Every item's key is computed under every registered scheme and kept on the
item result (per-scheme keys and output paths). A verify or merge stage's older-scheme key is
rendered from the upstream items' same-scheme keys and output paths: the manifest and the
`source.Key` lead are rendered per scheme in the `stages.go` verify/merge draft builders. After a
key-formula change nothing — including the shipped audit's `report` merge — is redone.
**Regression guard.** Ratified design call (user, 2026-09-29) — **Merge/verify full chain:** every
item's key is computed under every registered scheme (kept on the item result, e.g. per-scheme keys
and output paths), and a verify or merge stage's older-scheme key is rendered from the upstream
items' same-scheme keys and output paths (the manifest / source.Key lead rendered per scheme,
stages.go verify/merge draft builders), so after a key-formula change nothing — including the
shipped audit's `report` merge — is redone.
An older scheme's output path is computed as a pure path (join of the workflow folder,
`items/<olderKey>/output.md`, or the stage's `out:` rendered) and never through anything that
creates a directory (no Store.Path / MkdirAll for older keys), so item 2's rename and its "older
folder gone" assertion hold.
**Fixture keys must differ:** the runner test's stages name `prompt:` files through
`Runner.Prompts` (as `promptPlan` does), and the merge's source fanout sets no `out:`, so every
scheme-1 key differs from its scheme-2 key (an inline `task:` omits `prompt_body` and would not).
**Scheme 1's verdict rule:** scheme 1's manifest is rendered with v0.23.4's `verdictOf` (the
receipt's verdict field read even on a partial receipt), not the unreleased 4cdec143 rule.
**Goal:** A folder whose fanout, verify and merge receipts all sit under scheme-1 keys resumes
every one of them with no child spawned.
**Approach (assumed at the header base):**
- `ItemResult` gains the item's key and output path under every scheme in `keySchemes`, filled in
  `prepareItems` (an older scheme's output path is a pure join, as `ItemOutputPath` builds it —
  never `outputPath` or `Store.Path`).
- `renderManifest` and `verifyDraft` render one suffix per scheme from `source.Items`' same-scheme
  keys and output paths; the draft carries them, and item 2's adoption walk (stale guard included)
  keys each older scheme with its own suffix.
**Files:** internal/workflow/runner.go; internal/workflow/stages.go; internal/workflow/keyscheme.go; internal/workflow/keyscheme_test.go; internal/workflow/runner_test.go
**Read first:** internal/workflow/stages.go — verifyDraft, renderManifest, verdictOf; internal/workflow/runner.go — prepareItems, itemDraft, ItemResult;
internal/workflow/store.go — ItemOutputPath; internal/workflow/runner_test.go — promptPlan
**Tests:**
- Runner test: fanout → verify → merge, every receipt under scheme-1 keys, stages naming `prompt:`
  files through `Runner.Prompts` and the fanout setting no `out:`. `Run` spawns no child, marks
  every item Resumed, leaves each folder under its current key, and no `items/<scheme1Key>` folder
  exists afterwards.
- Same chain with a verify item whose scheme-1 receipt is partial with `verdict: confirmed`: the
  merge is still resumed.
- `TestKeySchemeGolden`: golden keys for a verify item and a merge item under both schemes.
**Acceptance:**
- `go build ./... && go vet ./internal/workflow/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'KeyScheme|Scheme' ./internal/workflow/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/workflow/`
**Commit:** fix(workflow): rebuild merge and verify keys under every older scheme

## 4. Say when a resumed stage redid finished work

**What:** Depends on item 3. Today, when a re-issued workflow redoes an item an earlier run of the
same folder had finished, nothing says so. That covers the same stage, round and label with an
ok/partial receipt under another key that item 2 did not adopt. After this item, the stage says so.
**Regression guard.** The notice item keeps its content but takes `prior` from item 2 instead of
adding it, and becomes item 4 (Depends on item 3); its "redid" count uses every scheme's key.
The note text becomes `redid <n> finished item(s): their inputs changed since they ran` (item for
1, items otherwise) — it covers a brief, prompt or context-file edit and a changed upstream result
(e.g. a merge after RerunFailed) alike.
**Goal:**
- Such a stage's `StageResult.Note` and its `status.json` line carry
  `redid <n> finished item(s): their inputs changed since they ran`.
- `Format` prints it as `<kind> <stage>: redid …`.
- A stage with nothing redone carries no such note.
**Approach (assumed at the header base):**
- `runState.prior` comes from item 2.
- `prepareItems` counts the items that miss under every scheme while `prior` has an ok/partial
  item with the same stage name, round and label.
- `endStage` sets the note on the StageResult and the stage's status line when the count is above
  0: `item` for 1, `items` otherwise.
**Files:** internal/workflow/runner.go; internal/workflow/runner_test.go; internal/workflow/format_test.go; docs/manual/workflows.md
**Read first:** internal/workflow/runner.go — prepareItems, endStage, runState, openStatus; internal/workflow/format.go — noteLine;
internal/agent/background.go — RerunFailed; internal/agent/workflowcall.go — workflowDetail;
internal/workflow/runner_test.go — TestRunnerRedoesItemsWhenThePromptFileChanges
**Tests:**
- Runner test: run a prompt-file fanout, edit the prompt, run again. The stage Note is
  `redid 1 finished item: their inputs changed since they ran`, and `Format`'s
  output contains `fanout <stage>: redid 1 finished item: …` exactly.
- No note on a plain resume, and none for an item adopted by item 2.
- Docs: `docs/manual/workflows.md` documents the line beside the prompt-file key paragraph.
**Acceptance:**
- `go build ./... && go vet ./internal/workflow/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Redid|Redo' ./internal/workflow/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/workflow/`
**Commit:** feat(workflow): note the finished items a resumed stage redid

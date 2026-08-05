# Remove the obsolete `/skill` command

- **Goal:** delete the `/skill` picker verb — redundant since skills are invoked
  directly (`/<skill-id>`, ADR 0027) or picked from the merged `/` menu — together
  with the `menuOnly` machinery and the `acSkill` dropdown that exist solely for it,
  then sweep code comments and living documentation so nothing still describes the
  removed entry point.
- **Folded in (2026-08-05):** item 4 — `/new` (and its alias `/clear`) must no longer
  be recorded as a recallable prompt (`ISSUES.md:9`). Independent of the `/skill`
  removal; folded here on the owner's instruction rather than opening a fourth
  active plan.
- **Date:** 2026-08-04 · **Status:** TODO (no item executed) · Amended 2026-08-05
  (item 4 folded in)
- **Ratified design calls:**
  - Recall exclusion scope: only the session-reset pair `/new` + `/clear` stops
    being recorded, via a spec-driven `commandSpec` flag (`noRecall`); every other
    sent line — ordinary prompts, Interjections, all other whole-line `/command`
    invocations (`/version` stays test-pinned as recorded) — remains recallable.
    Rejected: excluding all slash commands; excluding all zero-arg commands.
    (Owner via AskUserQuestion, 2026-08-05.)
- **Authoritative sources:**
  - `ISSUES.md:12` — the owner directive: "`/skill` is not needed anymore — remove it."
  - `ISSUES.md:9` (working tree at fold-in time) — the owner directive: "`/new` should
    not be recorded as a recallable prompt." Item 4's `file:line` refs are indexed
    against `ff55ff8`; the same drift rule applies.
  - `docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md` — the decision that
    made `/skill` redundant; it explicitly kept the two-step picker as an alternate
    entry point, so it receives a dated amendment note (item 3), never a rewrite.
  - Pinned commit `5b75c1b` — every `file:line` below is indexed against it. Line
    numbers are locators only; the named symbols and quoted phrases are authoritative
    if lines drift (the menu-layout plan `2026-08-04 - 03` lands first and touches
    some of the same files).
  - If any doc named below disagrees with an item, the code at `5b75c1b` plus
    `ISSUES.md:12` win.
- **Standing requirements:**
  - skills: coding-standards
  - Execute only on a clean tree, after plan `2026-08-04 - 03`
    (user-questions-menu-layout) has landed — its in-flight work touches
    `autocomplete.go` / `popup.go`.
  - `make check` green before every commit.
- **Out of scope — must NOT be touched:**
  - `/skills` (the listing command), direct `/<skill-id>` invocation, inline skill
    tokens, and the skills subsystem (`internal/skills/*`, `Options.Skills`,
    `Options.ReloadSkills`) — all stay.
  - Shared autocomplete plumbing the merged `/` menu still uses: `skillSuggestions`,
    `insertSkillToken`, `spliceCompletion`, `outsideRegion`, the `skillRegion`
    field, popup chrome — stays.
  - The `whileRunning` flag and the `— idle only` tag machinery
    (`autocomplete.go:269,292-295`) — only `menuOnly` is removed.
  - Historical records are never rewritten: CHANGELOG entries at or below the
    `## [0.10.4]` heading (line ~2383), earlier `[Unreleased]` narrative entries
    (they describe their own change at its time; the new `### Removed` entry
    supersedes them chronologically), `TODO.md` done-list lines (`:77`, `:777`),
    passing mentions in ADR 0025 (`:159`) and ADR 0032 (`:29`), and everything under
    `docs/plans/archived/` and `docs/handoffs/`.
  - The active plan `2026-08-04 - 04 - slash-menu-relevance-ranking-plan.md:53`
    mentions the `/skill <arg>` picker inheriting the ranking; that clause becomes
    inert after this removal. Saved plans are never rewritten — flagged here for the
    owner, not edited.
  - No version identifier changes (see closing note).
  - Item 4 additions: no retroactive scrub of already-recorded `/new`//`/clear`
    lines in `~/.apogee/prompts/*.jsonl` — compaction ages them out; the
    `internal/recall` store stays command-agnostic (the filter is TUI-side policy);
    the `acceptAutocomplete` path (which records no command today,
    `autocomplete.go:671`) is untouched.

## 1. Remove the `/skill` verb, the `menuOnly` flag, and the `acSkill` picker — ✅ DONE (2026-08-05)

NOTES (2026-08-05): `TestSlashMenuShadowsCollidingSkillID`'s `/skill clea` path could not be
"reworked to reach the shadow via the merged menu" — `slashSuggestions` drops a shadowed skill from
the merged rows by construction, so the menu is not a route to it. Repointed instead to the
surviving entry point: the shadowed id typed as an inline `/id` token mid-message, which
`submitParse` still resolves.

NOTES (2026-08-05): the wholesale deletions listed below would have dropped the last coverage of
three SURVIVING behaviours — the merged menu's edge-triggered catalog reload, its nil-`ReloadSkills`
safety (both only pinned by the deleted `TestSkillPickerReloads*` trio) and `skillSuggestions`'
already-invoked exclusion (only pinned by the deleted
`TestSkillPickerExcludesTokensAlreadyInTheBuffer`). Deleted as instructed, then replaced by three
merged-menu tests: `TestSlashMenuReloadsTheCatalogOnOpen`, `TestSlashMenuReloadNilSafe`,
`TestSlashMenuExcludesSkillsAlreadyInTheBuffer`.

NOTES (2026-08-05): `internal/tui/doc.go:122-125` (item 2's list) was rewritten here, because item
1's own acceptance grep for `acSkill` covers that line. Item 2 will find nothing left there.

**What:** Delete the behaviour and every test that pins it, in one green commit.

Production — `internal/tui/command.go`:

- The registry row `{name: "skill", summary: "pick a skill by name (writes its
  /token)", menuOnly: true}` (`:139`).
- The `menuOnly` field on `commandSpec` (`:77`) and its defining doc comment
  (`:69-71`). `/skill` is its only setter and nothing else changes: every other
  spec leaves it false, so each reader below reduces to its false arm.
- The `c.menuOnly` disjunct in `matchCommand` (`:285`, `if !ok || c.menuOnly` →
  `if !ok`).
- `skillPickerUsage` (`:222`) and its `unknownSlashNote` branch (`:231-233`) — a
  bare `/skill` now earns the same generic `unknown command or skill` refusal as
  any other unknown verb.
- The ordering-dependency paragraph "`/skill` must precede `/skills`…" (`:122-126`).
- In-file doc-comment mentions of the flag at `:81`, `:201`, `:219`, `:238`, `:268`.

Production — `internal/tui/autocomplete.go`:

- The `acSkill` kind (`:46`), its `computeAutocomplete` branch (`:107-113`), and its
  `autocompleteTitle` case (`:703-704`).
- `skillArgToken` (`:308-323`) and the `inPicker` half of `recomputeAutocomplete`'s
  reload edge trigger (`:163`) — the `inMenu` half survives (the merged menu still
  reloads the catalog on open).
- The `spec.menuOnly` early-return in `autocompleteExactMatch` (`:563`) and the
  `acSkill` early-return above it (`:547-549`).
- The `|| spec.menuOnly` disjunct in `acceptAutocomplete` (`:600`) — `takesArgs`
  completion behaviour (`/confine`) is untouched.
- Comment mentions of the menu-only `/skill` at `:30`, `:277`, `:579`.

Tests (all in `internal/tui/`):

- `skill_test.go` — delete the picker-behaviour tests wholesale:
  `TestSkillArgToken`, `TestSkillArgTokenAtTheCaret`,
  `TestComputeAutocompleteSkillDropdown`, `TestCommandDropdownOffersSkill`,
  `TestSkillArgWinsOverBareCommand`, `TestAcceptSkillSplicesInlineToken`,
  `TestSkillPickerExcludesTokensAlreadyInTheBuffer`,
  `TestSkillCommandChainsIntoPicker`, `TestEnterOnSkillCommandDoesNotSubmit`,
  `TestSkillPickerReloadsOnOpenByTyping`, `TestSkillPickerReloadsViaCommandChain`,
  `TestSkillPickerReloadNilSafe`. `TestSlashMenuShadowsCollidingSkillID` (`:366`)
  keeps its subject but its `/skill clea` path (`:378`) must be reworked to reach
  the shadow via the merged menu; `TestNilCatalogGuards` (`:606`) drops its
  `"/skill "` case, keeps the rest.
- `command_test.go` — `TestCommandTableDrivesParserAndMenu` (`:41`): the
  `menuOnly` assertions go, `wantParsed` becomes all 15 remaining verbs;
  `TestCommandSpecsReadAlphabetically` (`:100-115`): drop the skill<skills index
  assert (`:112`); `TestParseInputSoleUnknownSlash` (`:245`): the `/skill` case
  (`:255`) now expects the generic unknown handling; `TestUnknownSlashNote`
  (`:284`): the `skillPickerUsage` assert (`:288`) and the menuOnly drift-guard
  loop (`:291-298`) go; the `"/skill foo"` sent-verbatim case (`:229`) keeps its
  expectation (a multi-token unknown-verb line still travels as text) — verify,
  don't assume.
- `minilang_test.go` — delete `TestBareSkillVerbTeachesThePicker` (`:532`); update
  the caret-region comment naming the `"/skill "` picker exception (`:1031`).
- `autocomplete_test.go` — `TestAutocompleteSkillDropdownChrome` (`:31`) and
  `TestAutocompleteDropdownSpansFullWidth` (`:115`) open their dropdown via
  `"/skill "`: repoint both to a surviving dropdown (the merged `/` menu or `@`)
  so the chrome coverage is kept, not deleted.
- `prompteditor_test.go` — the reset fixture (`:63-76`) uses
  `"half-typed /skill go"` and `kind: acSkill`: swap to a surviving kind.
- `interject_test.go` — `TestAutocompleteOpensWhileRunning` (`:845`): the
  `/skill re` mid-run case (`:879`) repoints to a surviving mid-run flow; the
  `idleOnlyTag`-on-`/clear` assert (`:872`) stays untouched.
- `transcript_test.go:676-677` — uses the surviving `skillSuggestions`; relabel
  the `"a /skill picker row"` comment only if it still reads wrong.

**Tests:** the suite itself is the deliverable above; additionally keep (or add,
if the reworked `TestParseInputSoleUnknownSlash` case doesn't already pin it) one
assertion that a sole `/skill` is refused with the generic unknown note.

**Acceptance:**
- `make check` → green.
- `grep -rn "menuOnly\|acSkill\|skillArgToken\|skillPickerUsage" internal/ cmd/ apogee.go` → no hits.
- `grep -n '"skill"' internal/tui/command.go` → no hits.

**Commit:** `feat(tui): remove the /skill picker — direct /<id> invocation and the merged menu replace it`

## 2. Sweep the code comments that still name the `/skill` picker — ✅ DONE (2026-08-05)

Depends on item 1.

NOTES (2026-08-05): the acceptance grep cannot reach zero and was not made to. Six hits are
irreducible: `internal/tui/command_test.go:212,214,240,268,271` are item 1's own required deliverable
(the assertions pinning that a sole `/skill` now earns the generic unknown-slash refusal — lines 214,
240 and 271 are executable test data, 212 and 268 the comments documenting exactly those cases), and
`internal/agent/loop.go:830` is the `</skill>` closing tag inside the skill-injection format string —
executable code, and a false positive of the pattern rather than a mention of the verb. Refined grep
that IS clean, for the verifier:
`grep -rnE '/skill([^s-]|$)' --include='*.go' . | grep -v docs/ | grep -vE 'command_test\.go|</skill>'`
→ no hits.

NOTES (2026-08-05): swept beyond the enumerated bullets, under the item's own general rule ("every
in-code comment that presents the `/skill` picker as a live consumer"). The bullet list was derived
from the string `/skill`, so it named none of the comments that call the removed feature "the
picker" by the bare word. The full bare-word set, all repointed to the surviving consumers (the
merged `/` menu, `/skills`, inline `/id` tokens): `internal/tui/autocomplete.go:353` ("not the
picker's" — a contrast that no longer denotes anything), `:376` ("a skill picker row's cells") and
`:392-396` (`skillSuggestions`' doc comment — "the picker's two cells", "the picker is dark", plus
the column claim in the same sentence, which was the picker's layout and is now the merged menu's
flattened cell); `internal/tui/skills.go:38` ("the same live refresh the picker edge-triggers when
it opens" — false since item 1: the merged menu is what edge-triggers it), `:59` ("the order the
picker shows"), `:174` ("identical from the picker"); `internal/skills/parse.go:26`, `:57`, `:58`,
`:187`, `:245` ("nonsense in the picker", "the picker label", "the picker hint", "a stray character
in the picker", "contentless entry in the picker"); `internal/skills/skill.go:30`, `:41` ("the
picker just does not offer it", "never appears in the picker"); and in tests
`internal/skills/parse_test.go:173` ("the picker's summary"), `internal/skills/load_test.go:219`
("the snapshot the picker and the loop share"), `internal/tui/skill_test.go:673` ("reads like the
picker"), `:680` ("the picker edge-triggers on open"), `:753` ("identical from the picker").
Comment-only in every case — the test files' assertions are untouched. Bare-word "picker" comments
that denote the SURVIVING shared single-select overlay (`picker.go` — `/model`, `/server`,
`/schedule`, the launcher rows) or the `/sessions` browser were left alone, as was
`command_test.go:268` ("the retired picker verb"), which is item 1's own deliverable and presents
the feature as removed. Also reworded the slash-list phrase `command/file/skill` → `command, file
and skill` (`popup.go:16,89`, `autocomplete.go:665`, `autocomplete_test.go:18`): those are pattern
false positives, not picker mentions, but clearing them is what lets the acceptance grep stand as a
permanent guard. `internal/tui/doc.go:122-124` needed nothing — item 1 already rewrote it, as its
NOTES predicted.

**What:** One-line comment rewordings only — no behaviour, no test changes. Every
in-code comment that presents the `/skill` picker as a live consumer is repointed
to the surviving consumers (the merged `/` menu, `/skills`, inline `/<id>` tokens):

- `internal/tui/doc.go:122-124` — the mini-language paragraph describing the
  `/skill` → `acSkill` chain.
- `internal/tui/tui.go:18`, `:282`, `:290` — `Options.Skills` / `ReloadSkills`
  described as feeding "the /skill picker".
- `internal/tui/skills.go:15`, `:18` — the file header's verb map.
- `internal/tui/prompteditor.go:39`, `:43` — `skillRegion` described as tracking a
  "`/skill <partial>` region" (it now serves the merged-menu reload trigger).
- `internal/tui/model.go:505`, `:991` — "(reloads on /skill open)".
- `cmd/apogee/wire.go:85`, `:89`, `:520` — the catalog-refresh comments.
- `apogee.go:239` — "attached /skill IDs".
- `internal/skills/doc.go:9`, `:12`; `catalog.go:13`, `:79`; `provider.go:14`,
  `:54`; `skill.go:10`; `parse.go:14`; `load.go:26`; `provider_test.go:10`.
- `internal/domain/config.go:327`.

`/skill-id` and `/<skill-id>` mentions are the direct-invocation syntax and stay.

**Tests:** none new — `make check` proves the sweep is comment-only.

**Acceptance:**
- `make check` → green.
- `grep -rnE '/skill([^s-]|$)' --include='*.go' . | grep -v docs/` → no hits.
- `git diff HEAD~1 --stat` shows comment-only line counts (no hunk touches
  executable code — verifier eyeballs the diff).

**Commit:** `docs(tui): repoint code comments from the removed /skill picker to its surviving consumers`

## 3. Living documentation, changelog, ADR amendment, and issue closure

Depends on item 1.

**What:** every cross-cutting doc amendment for this removal, owned here:

- `README.md:158` — delete the `/skill` table row (the surrounding prose describes
  the merged menu and needs no change — verify).
- `CONTEXT.md:638` — drop the "plus the `/skill <name>` picker that writes the
  token for you" clause from the Skill entry.
- `layout.md:848` — remove the `/skill` exception clause ("plus `/skill`, which
  chains into the picker over the catalog") from the "Accepting a command RUNS
  it" spec; re-read `:845-849` so the surviving sentence stays true.
- `cmd/apogee/defaults/config.yaml:280` — reword "attach one in chat with /skill"
  to the direct form (type `/<skill-id>` in your message).
- `CHANGELOG.md` — add a `### Removed` entry under `[Unreleased]` (create the
  section if absent): `/skill` is gone — invoke a skill by typing its `/<id>`
  directly or pick it from the merged `/` menu; the `menuOnly` flag left with it,
  which also retires the wrong `— idle only` tag that row wore (ISSUES.md:23).
  Earlier `[Unreleased]` entries are left untouched (see out-of-scope).
- `docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md` — add a short
  dated amendment note (at the status/header area): the alternate two-step
  `/skill` entry point this ADR kept was removed 2026-08-04 as redundant
  (ISSUES.md directive); the core decision — one namespace, inline tokens — is
  unchanged. Do not rewrite the ADR body or its rejected-alternatives section.
- `ISSUES.md` — delete both entries: `:12` (the removal directive — executed) and
  `:23` (the `— idle only` tag bug — moot: the tag, the flag, and both proposed
  fixes left with the feature).

**Tests:** none (markdown + an embedded yaml comment; the config template is
covered by item 2's `make check` having proven the embed still builds — re-run it
here anyway since `config.yaml` changes in this item).

**Acceptance:**
- `make check` → green (the edited template re-embeds).
- `grep -nE '/skill([^s-]|$)' README.md CONTEXT.md layout.md ISSUES.md cmd/apogee/defaults/config.yaml` → no hits.
- `awk '/## \[Unreleased\]/,/## \[0\.10\.4\]/' CHANGELOG.md | grep -c '### Removed'` → ≥ 1.
- `grep -n '2026-08-04' "docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md"` → ≥ 1 (the amendment note).

**Commit:** `docs: retire /skill from the living docs, amend ADR 0027, close both ISSUES entries`

## 4. Never record `/new` and `/clear` as recallable prompts

Independent of items 1–3 (any order; the shared files — `command.go`,
`command_test.go`, `CHANGELOG.md`, `ISSUES.md` — are touched in disjoint regions).
Authoritative source: `ISSUES.md:9` plus the ratified design call in the header.

**What:** Today every sent line is recorded: `submit()` calls
`recordSend(sent)` (`internal/tui/model.go:1150`) one line before `runCommand`
dispatches the parsed command, and `recordSend` (`internal/tui/recall.go:198-208`)
filters only an unwired host, the empty string, and consecutive duplicates — so a
sent `/new` lands in the walk and on disk, and a later ↑ + ⏎ replays a session
wipe. Change, per the ratified design call:

- `internal/tui/command.go`: add a `noRecall bool` field to `commandSpec`
  (`:72-78`) with a doc comment stating the meaning — a sent invocation of this
  verb is never recorded as a recallable prompt, in memory or on disk — and set
  `noRecall: true` on exactly two registry rows: `clear` and `new` (`:133`; the
  rows are one `case "clear", "new":` handler, `model.go:1448`).
- `internal/tui/model.go` `submit()` (`:1149-1152`): in the `kindCommand` branch,
  skip the `recordSend` call (which owns both the in-memory list and the disk
  append Cmd) when the matched spec carries `noRecall`.
- `internal/tui/interject.go` (`:171`): guard the same way, so the flag means
  "never recorded" independent of state. Today this site is unreachable for the
  pair (both are idle-only and refused before the record) — the guard is
  future-proofing, not behaviour; note this in a code comment or commit body,
  not with a test.
- `CONTEXT.md:273-277` (Prompt recall entry): the "What is recorded is what was
  *sent*" sentence gains the carve-out — the session-reset pair `/new`//`/clear`
  is deliberately never recorded, so a walk cannot hand back a line whose ⏎
  wipes the session. Use the phrase "session-reset" (acceptance greps for it).
- `layout.md:1064-1080` (Prompt recall paragraph): one added clause/sentence with
  the same fact, same "session-reset" phrase.
- `CHANGELOG.md`: entry under `[Unreleased]` → `### Fixed` (create the heading if
  absent): `/new` and `/clear` are no longer recorded as recallable prompts.
- `ISSUES.md`: delete the `/new` recallable-prompt entry (`:9` at fold-in time).

**Tests:**

- `internal/tui/recall_test.go` — extend `TestRecallRecordsEverySendPath`
  (`:353`) or add a sibling: a sent `/new` and a sent `/clear` produce no
  in-memory entry and no `AppendPrompt` call on the fake host; the existing
  `/version`-is-recorded assertion (`:380-386`) stays as the recorded-command
  control.
- `internal/tui/command_test.go` — drift guard: iterate the spec table and assert
  exactly {`clear`, `new`} carry `noRecall` (the same shape as the `menuOnly`
  drift guard item 1 removes; independent of whether item 1 has run).

**Acceptance:**

- `make check` → green.
- `go test ./internal/tui/ -run 'TestRecall' -v` → all pass, including the new
  exclusion coverage.
- `grep -c 'noRecall: true' internal/tui/command.go` → exactly 2.
- `grep -n 'session-reset' CONTEXT.md layout.md` → ≥ 1 hit in each file.
- `grep -n 'recallable' ISSUES.md` → no hits.
- `awk '/## \[Unreleased\]/,/## \[0\.10\.4\]/' CHANGELOG.md | grep -c 'recallable'` → ≥ 1.

**Commit:** `fix(tui): never record /new and /clear as recallable prompts`

---

**Suggested version bump (not performed):** minor — `v0.11.0` — a user-visible
command is removed, which under 0.x SemVer practice is a minor-line event; the Go
API is untouched; item 4's recall fix rides the same bump. Owner decides whether
and when.

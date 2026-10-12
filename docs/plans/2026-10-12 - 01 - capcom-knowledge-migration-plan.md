# capcom Knowledge migration: the glossary and the decision records become Notes

**Goal:** move apogee's glossary (`CONTEXT.md`) and its 98 decision records (`docs/adr/0001`–`0098`)
into capcom Knowledge as the `glossary` and `decisions` Note trees, in capcom ADR 0010's shape;
point every live reference at the Notes; leave pointer stubs where the files were.
**Date:** 2026-10-12
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** fbd6ef01

**Regression check (2026-10-12, fbd6ef01):**
- 1: guard folded
- 2: guard folded
- 3: guard folded
- 4: guard folded
- 5: guard folded
- 6: guard folded (item 5's guard)
- 7: guard folded (item 5's guard)
- 8: guard folded (item 5's guard)
- 9: guard folded (item 5's guard)
- 10: guard folded
- 11: guard folded
- 12: guard folded
- 13: guard folded (owner decision)

**Sources:**
- capcom ADR 0010 (`cd ../capcom && capcom know show adr-0010-decision`) — the target shape
- `../capcom/docs/plans/archived/2026-10-12 - 03 - glossary-and-adr-migration-plan.md` — capcom's own cut of the same shape
- `../capcom/docs/format.md` — Note file format, `aliases:`, `retired:`, `superseded-by:`
- `docs/handoffs/2026-10-12 - 01 - refocus-findings-and-capcom-knowledge-migration.md` — Tier 3
- `CONTEXT.md`, `docs/adr/*.md` at the base — the ground truth every `--verify` compares against

**Ratified design calls (owner, 2026-10-12):**
- **Old files:** `CONTEXT.md` becomes a pointer stub of at most 10 lines, a new `docs/adr/README.md` pointer stub of at most 10 lines is created (none exists at the base), and the 98 ADR files are `git rm`'d.
- **Scope:** `layout.md` and `docs/design/` stay files; only their links into the migrated files are rewritten.
- **Global skills:** `~/.claude/skills/*` (grill-with-docs, improve-codebase-architecture, implement-plan) are out of scope.
- **AGENTS.md:** only its knowledge pointers change; no shrink.
- **ADR children:** one child Note per `##` section, verbatim.
- **Child titles:** derived by the cutting script from the heading; hand work is the 98 record summaries and the 109 term gists.
- **Retired glossary terms:** Mechanism, Hook point, Post-response decision, Mechanism descriptor and Experimental hook are `--retire`d.

**Standing requirements:**
- skills: coding-standards
- Pi 5 rules (`AGENTS.local.md`): never `go test` a whole heavy package or a pattern; one package and one test, `GOMEMLIMIT=2GiB`; never two test runs at once. Pass these to every sub-agent.
- Throwaway scripts live in `docs/skill-runs/migrate-knowledge/` (gitignored); an item that finds one gone rewrites it from the owning item's Approach.
- After every item: `capcom check` exits 0 with no output and `capcom fmt --check` exits 0.
- Never hand-edit `format:` in `.capcom/config.yaml`, `retired:` or `superseded-by:`: `capcom know update` writes them and raises the store to format 3 itself.

**Out of scope:**
- Historical text: `CHANGELOG.md`, `docs/plans/archived/`, `docs/reviews/`, `docs/design/archived/`, `docs/handoffs/`, closed capcom issues — their links into `docs/adr/` may dangle.
- The ~5,300 bare `ADR NNNN` citations in Go and docs: they resolve as typed against `adr-NNNN`.
- Synthetic test fixtures naming `docs/adr/…` or `CONTEXT.md` (`internal/tools/*_test.go`, `internal/security/dangerous_payload_test.go`, `internal/workflow/*_test.go`).
- Tier-6 doc cleanups of the handoff; global skills; AGENTS.md shrink.

## 1. Glossary cut into a Note tree — ✅ DONE (2026-10-12)

NOTES (2026-10-12): gate passed — capcom 0.3.0; `capcom know update --help` lists `--retire` and `--add-superseded-by`.
NOTES (2026-10-12): the intro body of `glossary.md` drops CONTEXT.md's `# Apogee` H1 (the Note title replaces it; capcom's own cut did the same); everything else above `## Language` is verbatim after link rewriting.
NOTES (2026-10-12): a term Note's body is the whole term paragraph, the bold term line included, so the gloss (e.g. `(and its **Beat**)`) survives verbatim; `--verify` compares whole stripped bodies (frontmatter keys tolerated) and also fails on any Note under `glossary/` not cut from CONTEXT.md.
NOTES (2026-10-12): the two explicit anchors (`floor-guard`, `tool-result-capping`) equal their term ids, so no anchor alias was needed; glossary-context's body is its two-line intro verbatim, Self-regulation's and Retired terms' bodies are their prose verbatim, the other seven sections carry one descriptive line; root and section titles are one sentence (root lower-cases "apogee" per house spelling).
NOTES (2026-10-12): split_glossary.py exposes `rewrite_links(text, source_path, note_path)`, which resolves targets against the source file's directory, for item 3's split_adr.py to import.

**What:**
**Goal:** `.capcom/knowledge/glossary.md` exists with ten section children and 109 term grandchildren holding `CONTEXT.md`'s text verbatim (after link rewriting); `docs/skill-runs/migrate-knowledge/split_glossary.py --verify` exits 0; `CONTEXT.md` is byte-identical to the base.
**Approach (assumed at the header base):** first the gate: `capcom version` is ≥ 0.3.0 and `capcom know update --help` lists `--retire` and `--add-superseded-by`; stop the item if not. Write `split_glossary.py`:
- Intro (everything above `## Language`) → body of `glossary.md`, followed by one line per section citing its id. Title: one sentence.
- Each `### <Section>` → `glossary/glossary-<s>.md` with `aliases: [<the heading's GitHub anchor slug>]`. Ids, binding: `glossary-identity`, `glossary-turns`, `glossary-safety`, `glossary-reactions`, `glossary-self-regulation`, `glossary-context`, `glossary-deliverables`, `glossary-probing`, `glossary-validation`, `glossary-retired`. Body: the section's prose that belongs to no term (Self-regulation's two paragraphs, Retired terms' 102 lines verbatim), else one line on what the section covers.
- Term paragraph = a line matching `^\*\*[^*]+\*\*( \([^)]*\)| / \*\*[^*]+\*\*)?:` whose previous line is blank or an `<a id="…"></a>` line; it runs to the next term, anchor line or heading. The anchor line belongs to the term after it, and is dropped. Term file `glossary/glossary-<s>/<id>.md`, body verbatim incl. `_Avoid_:`.
- Term id: the bold name lowercased, an outer parenthetical gloss dropped, non-alphanumerics → `-`. Binding exceptions: `Session / Session record` → `session` (alias `session-record`); `Split diff / Stacked diff` → `split-diff` (alias `stacked-diff`); `Present / Presentation` → `present` (alias `presentation`); `Stop (a delegation)` → `stop-delegation`; `Spend (money)` → `spend`; `File reference (@file)` → `file-reference`; `The loop` → `loop`; `The bench` → `bench`; `Deferred-response action vs request-prep hint` → `deferred-response-action`. A term whose anchor id differs from its id carries the anchor id as an alias.
- Provisional term title: the bold name with its gloss, written double-quoted; item 2 replaces it.
- Link rewriting (inside every Note): `[text](…NNNN-slug.md…)` into `docs/adr` → `text`, plus ` (ADR NNNN)` when `text` lacks `ADR NNNN`; `[text](#anchor)` → `text`; any other relative link is re-pathed relative to the Note's own file.
- `--verify`: re-reads `CONTEXT.md`, applies the same rewrite, and checks every term body and section prose equals its Note body, and that the term count is 109.
Then `capcom fmt`, `capcom agents`.
**Regression guard.** Term ids trim leading and trailing `-` and collapse runs of `-` (`**`confine-to-workspace`**`, CONTEXT.md:1418 → `confine-to-workspace`); exception keys match the bold name ignoring case and hyphens (CONTEXT.md:2010 writes `**Deferred Response Action vs Request-prep Hint**`).
Context and history's two-line intro (CONTEXT.md:2055-2056, "These are distinct operations — …") is section prose kept verbatim in `glossary-context`'s body, and `--verify` compares it.
The link regex matches newlines inside link text and normalises whitespace before testing the text for `ADR NNNN` (CONTEXT.md:1788-1789 `[ADR⏎0095](docs/adr/…)`, 2566-2567 `[the⏎bench](#…)`).
**Files:** .capcom/knowledge/glossary.md, .capcom/knowledge/glossary/ (new tree), AGENTS.md
**Read first:** CONTEXT.md — `### Context and history`, `**`confine-to-workspace`**`, `**Deferred Response Action vs Request-prep Hint**`, `<a id="floor-guard">`, `### Retired terms`;
../capcom/docs/format.md — Ids, Aliases, Note files
**Tests:** no Go change; the Acceptance commands are the check.
**Acceptance:**
- `capcom know update --help | grep -c -E -- '--retire|--add-superseded-by'` prints 2 or more
- `python3 docs/skill-runs/migrate-knowledge/split_glossary.py --verify`
- `find .capcom/knowledge/glossary -mindepth 2 -name '*.md' | wc -l` prints 109
- `git diff --quiet fbd6ef01 -- CONTEXT.md`
- `capcom check && capcom fmt --check`
- `capcom know show floor-guard`, `capcom know show "Floor guard"` and `capcom know show identity-and-shape` each resolve
- `capcom know show confine-to-workspace` and `capcom know show deferred-response-action` resolve
**Commit:** `docs(knowledge): move the glossary into Knowledge`

## 2. Glossary titles and retired terms — ✅ DONE (2026-10-12)

NOTES (2026-10-12): `glossary` and the ten section Notes already carried one-sentence titles from item 1, so `.capcom/knowledge/glossary.md`, the section Notes and AGENTS.md's managed block are unchanged (capcom check confirms the block is current).
**What:** Depends on item 1.
**Goal:** every term Note's title reads `<Term>: <gist>` on its own (gist one clause, from the definition's first sentence); every section Note and `glossary` has a one-sentence title; the five retired terms (Mechanism, Hook point, Post-response decision, Mechanism descriptor, Experimental hook) carry `retired: true`.
**Approach (assumed at the header base):** read each term Note's first paragraph only; write titles with `capcom know update <id> --title …`. Retire with `capcom know update <id> --retire` (this raises the store to format 3). No body changes.
**Regression guard.** Retitling the top-level `glossary` Note refreshes AGENTS.md's managed block on its own (`../capcom/docs/format.md`, The Managed block in AGENTS.md), so AGENTS.md is in this item's files and `capcom check` (which runs the agents-block check) stays in its Acceptance.
**Files:** .capcom/knowledge/glossary.md, .capcom/knowledge/glossary/ (titles, five retired marks), .capcom/config.yaml, AGENTS.md
**Read first:** ../capcom/docs/format.md — Supersession, Note files (title quoting), The Managed block in AGENTS.md;
CONTEXT.md — `**Mechanism**`, `**Hook point**`, `**Post-response decision**`, `**Mechanism descriptor**`, `**Experimental hook**`
**Tests:** no Go change; the Acceptance commands are the check.
**Acceptance:**
- `grep -L '^title: .*: ' $(find .capcom/knowledge/glossary -mindepth 2 -name '*.md')` prints nothing
- `grep -l '^retired: true' -r .capcom/knowledge/glossary | wc -l` prints 5
- `python3 docs/skill-runs/migrate-knowledge/split_glossary.py --verify`
- `capcom check && capcom fmt --check`
**Commit:** `docs(knowledge): title the glossary terms and retire the retired ones`

## 3. Decision records cut into a Note tree — ✅ DONE (2026-10-12)

NOTES (2026-10-12): split_adr.py imports `note` and `rewrite_links` from item 1's split_glossary.py; banners before the H1 (most ADRs carry a `> Note (2026-10-0x)` there) and prose between the H1 and the first `##` both go into the record body, in file order, before `<!-- summary pending -->`; frontmatter lines keep their file order (Status, Supersedes, Amends).
NOTES (2026-10-12): derived child titles extend the plan's amendment form to every dated heading (`ADR 0013 clarification of 2026-07-02: …`, `ADR 0016 runtime-surface realisation of 2026-07-19: …`, a `, second` date tail as `(second)`), name numbered headings `ADR 0083 part N: <rest>`, and split a `—` tail only outside parentheses (0011's `Phase-2 realisation (P2.2 — …)` stays whole, lowercased); 498 children in all, clash letter suffixes on 11 ids: 0011 `phase-2b` and same-day amendments/addenda in 0018, 0019, 0022, 0043, 0059, 0077, 0078.
**What:** Depends on item 1.
**Goal:** `.capcom/knowledge/decisions.md` lists all 98 records; `decisions/adr-NNNN.md` exists for 0001–0098 with one child per `##` section; `split_adr.py --verify` exits 0; `docs/adr/` is unchanged.
**Approach (assumed at the header base):** write `docs/skill-runs/migrate-knowledge/split_adr.py` (reuse item 1's link rewriter; `CONTEXT.md#anchor` links → `text`):
- Number from the filename; record title = the H1 verbatim. Record body: `Status: <value>` and any `Amends: …` / `Supersedes: …` lines from the frontmatter verbatim, a blank line, the `> Note …` banners and any prose between the H1 and the first `##` verbatim, then the line `<!-- summary pending -->`.
- Split at `##` lines outside code fences (ADR 0018 has `##` inside a fence). Child body = the section below its heading, verbatim. Child id `adr-NNNN-<kind>`: Context → `context`; Decision → `decision`; `Decision N — …` → `decision-N`; Consequences → `consequences`; Considered options / Considered and rejected / Rejected / Alternatives rejected → `rejected`; `Amendment (YYYY-MM-DD…)` → `amend-MMDD`; `Addendum (…)` → `addendum-MMDD`; `Note (date)` → `note-MMDD`; a numbered `## N. …` → `part-N`; any other heading → its first word slugged. A clash inside one record gets a letter suffix on the last token (`amend-0802b`), keeping ids ≤ 4 words.
- Child title, derived: `ADR NNNN <heading lowercased>`; for a heading with a `—` tail, `ADR NNNN <kind>: <tail>`; amendments read `ADR NNNN amendment of YYYY-MM-DD: <tail>`. Double-quoted.
- `decisions.md`: one-sentence title; body one line per record, `adr-NNNN` — its title.
- `--verify`: for each ADR, frontmatter values, banners, pre-`##` prose and each section body (after link rewriting) equal the Note text, and the record count is 98.
Then `capcom fmt`, `capcom agents`.
**Regression guard.** The rewriter resolves each link target against the source file's directory: bare sibling `NNNN-slug.md` targets (~577 in `docs/adr`, e.g. 0071:10 `[ADR 0076](0076-….md)`) and multi-line link text included, so no Note keeps an ADR filename target.
`--verify` reads Note bodies tolerating any frontmatter keys (item 4 adds `retired:`/`superseded-by:`) and checks the record body as a prefix: Status/Amends/Supersedes lines, banners and pre-`##` prose verbatim, then exactly one trailing slot holding the placeholder or a summary; it also asserts `decisions.md` lists all 98 records.
`(Amendment|Addendum) (\(|— )YYYY-MM-DD` gives `amend-MMDD` / `addendum-MMDD` (0016:171, 0076:261,372,411); the title tail is the text after the first `) —`, `):` or `YYYY-MM-DD:` (0077:105,125 carry a `—` inside the tail).
**Files:** .capcom/knowledge/decisions.md, .capcom/knowledge/decisions/ (new tree), AGENTS.md
**Read first:** docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md — frontmatter, `## Decision N —` headings, sibling links; docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md — `## Amendment — date:` headings;
docs/adr/0018-*.md — `##` inside a code fence; ../capcom/docs/skill-runs/migrate-knowledge/split_adr.py — rewrite, verify; ../capcom/.capcom/knowledge/decisions/adr-0010.md — record body shape
**Tests:** no Go change; the Acceptance commands are the check.
**Acceptance:**
- `python3 docs/skill-runs/migrate-knowledge/split_adr.py --verify`
- `ls .capcom/knowledge/decisions/adr-*.md | wc -l` prints 98
- `grep -oE 'adr-[0-9]{4}' .capcom/knowledge/decisions.md | sort -u | wc -l` prints 98
- `grep -rnE '\]\([^)]*[0-9]{4}-[a-z0-9-]+\.md' .capcom/knowledge/decisions` prints nothing
- `git diff --quiet fbd6ef01 -- docs/adr`
- `capcom check && capcom fmt --check`
- `capcom know show "ADR 0076"` and `capcom know show adr-0071-decision-1` resolve
**Commit:** `docs(knowledge): move the decision records into Knowledge`

## 4. Supersession links between decision records — ✅ DONE (2026-10-12)

NOTES (2026-10-12): the Status-line parser behind `split_adr.py --verify-supersession` retires a record whose Status opens "superseded" and, in each `;`-clause that supersedes, retires or moves something, takes the ADRs named after "by"/"and" (so 0063's parenthetical "(ADR 0013 D5)", 0036's "widened by ADR 0078" and 0076's "amended by ADR 0077" name no successor); it reproduces the item's table exactly (asserted), and also checks each `Supersedes:` line (0012, 0071, 0088) against its targets' successors.
NOTES (2026-10-12): `.capcom/config.yaml` unchanged — the store was already `format: 3` after item 2, so no `capcom know update` raised it.
**What:** Depends on item 3.
**Goal:** every record whose frontmatter `Status:` says it is superseded in whole carries `retired: true` and `superseded-by:` naming its successors; every record whose `Status:` names a superseding ADR for part of it carries `superseded-by:` and stays current; records amended only by themselves, by an "amended by" ADR, or by non-ADR events carry neither.
**Approach (assumed at the header base):** the ADR `Status:` lines are the ground truth; this table, read from them at the base, is the cross-check (a mismatch follows the Status line and leaves a dated NOTES line). Apply with `capcom know update adr-NNNN --add-superseded-by adr-MMMM` (repeat) and `--retire`.
- Whole (retire): 0003→0076; 0004→0012; 0014→0071; 0015→0076; 0016→0076; 0070→0071, 0076.
- Part: 0002→0076; 0005→0012, 0013; 0006→0076; 0007→0088; 0009→0071; 0010→0076; 0013→0088; 0021→0076; 0023→0064, 0067, 0076; 0025→0063; 0028→0036, 0076; 0034→0076; 0035→0037, 0043; 0036→0037, 0041; 0037→0041, 0076; 0041→0029; 0045→0066, 0076; 0051→0074; 0061→0065; 0063→0088; 0069→0039; 0071→0076; 0073→0076; 0082→0088; 0086→0088.
- Neither: 0001, 0029, 0043, 0048, 0076, 0078.
**Regression guard.** The counts alone pass a wrong or missing successor, so `split_adr.py --verify-supersession` (added here) builds the expected `retired:` / `superseded-by:` set per record from the ADR `Status:` lines, compares it with each record Note's frontmatter (`grep -H '^superseded-by:\|^retired:'` shape), and exits non-zero on any missing, extra or wrong id.
**Files:** .capcom/knowledge/decisions/ (record frontmatter only), .capcom/config.yaml
**Read first:** docs/adr/*.md — frontmatter `Status:` / `Supersedes:` lines (table matches them at fbd6ef01); ../capcom/docs/format.md — Supersession; .capcom/config.yaml — format (already 3 after item 2);
../capcom/.capcom/knowledge/decisions/adr-0002.md — superseded-by example
**Tests:** no Go change; the Acceptance commands are the check.
**Acceptance:**
- `grep -l '^retired: true' .capcom/knowledge/decisions/adr-*.md | wc -l` prints 6
- `grep -l '^superseded-by:' .capcom/knowledge/decisions/adr-*.md | wc -l` prints 31
- `grep -n '^format:' .capcom/config.yaml` shows 3
- `python3 docs/skill-runs/migrate-knowledge/split_adr.py --verify-supersession`
- `capcom check && capcom fmt --check`
**Commit:** `docs(knowledge): link superseded decision records to their successors`

## 5. Record summaries, ADRs 0001–0020

**What:** Depends on item 4.
**Goal:** in `adr-0001.md` … `adr-0020.md` the `<!-- summary pending -->` line is replaced by a 3–5 line summary that states the decision, names its children in reading order, and — for a superseded record — says what still holds.
**Approach (assumed at the header base):** per record read the record body, its `-context` first paragraph and its `-decision` child(ren); write the summary with `capcom know update … --body-replace-old '<!-- summary pending -->' --body-replace-new …`. No other text changes.
**Regression guard.** Item 3's `--verify` checks each record body as a prefix ending in one free summary slot (the placeholder or the text that replaced it), so replacing `<!-- summary pending -->` keeps this item's `--verify` green; a `--verify` that pins the placeholder line is fixed in the script, never worked around in the Notes.
**Files:** .capcom/knowledge/decisions/adr-0001.md … adr-0020.md
**Read first:** docs/adr/0001-*.md … 0020-*.md — frontmatter `Status:`, `## Context`, `## Decision`; docs/skill-runs/migrate-knowledge/split_adr.py — --verify (record-body comparison, item 3);
../capcom/.capcom/knowledge/decisions/adr-0001.md — record summary shape; ../capcom/internal/check/check.go — checkNote;
`capcom know update --body-replace-old/--body-replace-new` — works on a retired Note (adr-0003/0004/0014/0015/0016 retired by item 4)
**Tests:** no Go change; the Acceptance commands are the check.
**Acceptance:**
- `grep -l 'summary pending' .capcom/knowledge/decisions/adr-00[01][0-9].md .capcom/knowledge/decisions/adr-0020.md` prints nothing
- `python3 docs/skill-runs/migrate-knowledge/split_adr.py --verify`
- `capcom check && capcom fmt --check`
**Commit:** `docs(knowledge): summarise decision records 0001 to 0020`

## 6. Record summaries, ADRs 0021–0040

**What:** Depends on item 4. Item 5's Goal and Approach, for `adr-0021` … `adr-0040`.
**Goal:** no `<!-- summary pending -->` line remains in `adr-0021.md` … `adr-0040.md`; each holds a summary per item 5's rule.
**Regression guard.** Item 5's guard (item 3's `--verify` checks the record body as a prefix with one free summary slot) covers this item; no change of its own.
**Files:** .capcom/knowledge/decisions/adr-0021.md … adr-0040.md
**Read first:** docs/adr/0021-*.md … 0040-*.md — frontmatter `Status:`, `## Context`, `## Decision`; docs/skill-runs/migrate-knowledge/split_adr.py — --verify;
../capcom/.capcom/knowledge/decisions/adr-0001.md — record summary shape; docs/adr/0037-every-settings-edit-applies-to-the-running-session.md — 33-line pre-`##` prose (summary lands after it)
**Tests:** no Go change; the Acceptance commands are the check.
**Acceptance:**
- `grep -l 'summary pending' .capcom/knowledge/decisions/adr-00{2[1-9],3[0-9],40}.md` prints nothing
- `python3 docs/skill-runs/migrate-knowledge/split_adr.py --verify`
- `capcom check && capcom fmt --check`
**Commit:** `docs(knowledge): summarise decision records 0021 to 0040`

## 7. Record summaries, ADRs 0041–0060

**What:** Depends on item 4. Item 5's Goal and Approach, for `adr-0041` … `adr-0060`.
**Goal:** no `<!-- summary pending -->` line remains in `adr-0041.md` … `adr-0060.md`; each holds a summary per item 5's rule.
**Regression guard.** Item 5's guard (item 3's `--verify` checks the record body as a prefix with one free summary slot) covers this item; no change of its own.
**Files:** .capcom/knowledge/decisions/adr-0041.md … adr-0060.md
**Read first:** docs/adr/0041-*.md … 0060-*.md — frontmatter `Status:`, `## Context`, `## Decision`; docs/skill-runs/migrate-knowledge/split_adr.py — --verify;
../capcom/.capcom/knowledge/decisions/adr-0001.md — record summary shape
**Tests:** no Go change; the Acceptance commands are the check.
**Acceptance:**
- `grep -l 'summary pending' .capcom/knowledge/decisions/adr-00{4[1-9],5[0-9],60}.md` prints nothing
- `python3 docs/skill-runs/migrate-knowledge/split_adr.py --verify`
- `capcom check && capcom fmt --check`
**Commit:** `docs(knowledge): summarise decision records 0041 to 0060`

## 8. Record summaries, ADRs 0061–0080

**What:** Depends on item 4. Item 5's Goal and Approach, for `adr-0061` … `adr-0080`.
**Goal:** no `<!-- summary pending -->` line remains in `adr-0061.md` … `adr-0080.md`; each holds a summary per item 5's rule.
**Regression guard.** Item 5's guard (item 3's `--verify` checks the record body as a prefix with one free summary slot) covers this item; no change of its own.
**Files:** .capcom/knowledge/decisions/adr-0061.md … adr-0080.md
**Read first:** docs/adr/0061-*.md … 0080-*.md — frontmatter `Status:`, `## Context`, `## Decision`; docs/adr/0070-off-ramp-mechanisms-ship-on-by-default.md — wholly superseded (retired by item 4; body edit still allowed);
docs/skill-runs/migrate-knowledge/split_adr.py — --verify; ../capcom/.capcom/knowledge/decisions/adr-0001.md — record summary shape
**Tests:** no Go change; the Acceptance commands are the check.
**Acceptance:**
- `grep -l 'summary pending' .capcom/knowledge/decisions/adr-00{6[1-9],7[0-9],80}.md` prints nothing
- `python3 docs/skill-runs/migrate-knowledge/split_adr.py --verify`
- `capcom check && capcom fmt --check`
**Commit:** `docs(knowledge): summarise decision records 0061 to 0080`

## 9. Record summaries, ADRs 0081–0098

**What:** Depends on item 4. Item 5's Goal and Approach, for `adr-0081` … `adr-0098`.
**Goal:** no `<!-- summary pending -->` line remains anywhere under `.capcom/knowledge/`.
**Regression guard.** Item 5's guard (item 3's `--verify` checks the record body as a prefix with one free summary slot) covers this item; no change of its own.
**Files:** .capcom/knowledge/decisions/adr-0081.md … adr-0098.md
**Read first:** docs/adr/0081-*.md … 0098-*.md — frontmatter `Status:`, `## Context`, `## Decision`; docs/skill-runs/migrate-knowledge/split_adr.py — --verify;
../capcom/.capcom/knowledge/decisions/adr-0001.md — record summary shape; CONTEXT.md — holds no "summary pending" text, so item 9's tree-wide grep is safe
**Tests:** no Go change; the Acceptance commands are the check.
**Acceptance:**
- `grep -rl 'summary pending' .capcom/knowledge` prints nothing
- `python3 docs/skill-runs/migrate-knowledge/split_adr.py --verify`
- `capcom check && capcom fmt --check`
**Commit:** `docs(knowledge): summarise decision records 0081 to 0098`

## 10. Live docs link to the Notes

**What:** Depends on items 2 and 4.
**Goal:** no live doc links into `docs/adr/` or `CONTEXT.md`: the shared reference grep below prints nothing, and `check_links.py` finds every relative link in the touched files resolving.
**Approach (assumed at the header base):** write `docs/skill-runs/migrate-knowledge/relink.py` and `check_links.py`. Rewrites, each target re-pathed relative to the linking file: a link to `docs/adr/NNNN-slug.md[#…]` in any relative form (`docs/adr/`, `../adr/`, `../../adr/`) → `.capcom/knowledge/decisions/adr-NNNN.md`; `CONTEXT.md#<anchor>` → the section or term Note file whose id or alias is the anchor; bare `CONTEXT.md` → `.capcom/knowledge/glossary.md`. Prose naming `CONTEXT.md` as the glossary reads "the glossary (`capcom know show glossary`)". `.apogee/skills/release-notes-apogee/prompts/_apogee.md`: decision records live in `.capcom/knowledge/decisions/`, the glossary in `.capcom/knowledge/glossary/`.
The shared reference grep, defined here and reused by items 11–13: `git grep -n -E 'CONTEXT\.md|docs/adr|adr/0[0-9]{3}' -- . ':!docs/plans' ':!docs/handoffs' ':!docs/reviews' ':!docs/skill-runs' ':!docs/design/archived' ':!CHANGELOG.md' ':!CONTEXT.md' ':!docs/adr' ':!.capcom' ':!AGENTS.md' ':!*_test.go'`. This item makes it print only `*.go` lines (item 11's).
**Regression guard.** `check_links.py` skips `docs/design/archived/` (45 links already dangle there at the base) and URL-decodes targets (`%20` in reaction-core-greenfield.md) before resolving.
A directory link to `docs/adr/` (SECURITY.md:88) → `.capcom/knowledge/decisions.md`, re-pathed; prose naming `docs/adr/` (docs/manual/README.md:21, building.md:356) reads "the decision records (`capcom know show decisions`)".
docs/manual/commands.md:438's fenced sample becomes an illustrative non-ADR path in the real refusal format (`file not found: <given> — did you mean: <sibling>`); item 11's path_suggest.go:21 comment uses the same path.
The doc-drift tests run are those that read touched files, one package and one test per run (below); `TestReadmeStatesTheToolCounts` reads README.md, which this item never touches.
**Files:** docs/manual/ (configuration.md, README.md, building.md, commands.md, headless.md, probe.md, reactions.md, updating.md), docs/design/ (the seven live docs), docs/layout/ (settings-screen-layout.md, split-diff-layout.md, tool-layout.md), layout.md, SECURITY.md, .apogee/skills/release-notes-apogee/prompts/_apogee.md
**Read first:** docs/manual/commands.md — near-miss suggestion fence; SECURITY.md — decision-records paragraph; docs/manual/README.md — "Working on the codebase itself?"; docs/manual/configuration.md — ADR links (some split across lines);
.apogee/skills/release-notes-apogee/prompts/_apogee.md — "Docs & manual" surface row; internal/tools/manual_drift_test.go — TestManualListsEveryKnownToolName;
internal/stubllm/script_test.go — designDocExample; internal/domain/reaction_test.go — TestManualStatesTheMomentCount
**Tests:** the doc-drift tests that read the touched files (`docs/manual/`, `docs/design/`, `.apogee/skills/`), one package and one test per run.
**Acceptance:**
- the shared reference grep prints only lines from `*.go` files
- `python3 docs/skill-runs/migrate-knowledge/check_links.py docs/manual docs/design layout.md docs/layout SECURITY.md .apogee/skills`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestManualListsEveryKnownToolName' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestShippedDangerousRuleIDsAreDocumented' ./internal/config/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestManualListsEveryEnvironmentOverride' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestManualListsEveryEventLineKind' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestDesignDocExampleScriptParses' ./internal/stubllm/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestRepoReleaseSkillsLoad' ./internal/skills/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestManualStatesTheMomentCount' ./internal/domain/`
**Commit:** `docs: link the decision records and glossary as Notes`

## 11. Go names the Notes, never the old files

**What:** Depends on item 4.
**Goal:** no non-test Go file names a `docs/adr/` path or `CONTEXT.md`; the message `cmd/release` prints names `.capcom/knowledge/decisions/adr-0098.md`, and a test asserts that the path the message names exists relative to the repo root.
**Approach (assumed at the header base):** comments naming `docs/adr/NNNN-slug.md` → `ADR NNNN`; comments naming `CONTEXT.md` → "the glossary" (keep any term name). In `cmd/release/main.go` the `"See docs/adr/0098-the-repo-owns-its-release-pipeline.md."` string becomes `"See ADR 0098 (.capcom/knowledge/decisions/adr-0098.md)."`, held in one named constant the test reads. Synthetic fixtures stay.
**Regression guard.** internal/domain/config.go:1369's Go doc-link definition (`// [ADR 0065]: docs/adr/0065-…md`, for SkillLookup's `[ADR 0065]`) points at `.capcom/knowledge/decisions/adr-0065.md`; the comment-path rule would leave `[ADR 0065]: ADR 0065`, which is no link target.
internal/tools/path_suggest.go:21's `(docs/adr/0025)` is a filename-prefix example, not a citation: it takes the same illustrative path item 10 picks for docs/manual/commands.md:438.
**Files:** apogee.go, cmd/apogee/launcher.go, cmd/release/main.go, cmd/release/main_test.go, internal/adoption/doc.go, internal/agent/orientation.go, internal/agent/resolution.go, internal/config/allow.go, internal/domain/{config,doc,exchange}.go, internal/floor/loopbreak.go, internal/heartbeat/heartbeat.go, internal/projectroot/doc.go, internal/refs/doc.go, internal/run/run.go, internal/tools/path_suggest.go, internal/tui/{recall,suggestband,thinking,tui}.go, internal/workflow/{items,plan}.go
**Read first:** cmd/release/main.go — newRootCommand (Long); cmd/release/main_test.go — execute, TestBadCommandLineIsAUsageError; internal/domain/config.go — SkillLookup doc-link definition;
internal/tools/path_suggest.go — suggestSiblings; internal/tools/path_suggest_test.go — synthetic docs/adr/0025 fixtures (stay); .github/workflows/ci.yml — Windows `go test ./cmd/release/...` job
**Tests:** new test in `cmd/release` that reads the message constant, extracts the path, and stats it from the module root.
**Acceptance:**
- the shared reference grep (item 10) prints nothing
- `go build ./... && go vet ./cmd/release/`
- `GOMEMLIMIT=2GiB go test -count=1 ./cmd/release/`
**Commit:** `refactor: cite decision records as Notes in Go`

## 12. Open issues link to the Notes

**What:** Depends on item 4.
**Goal:** no open (todo, doing or waiting category) capcom issue names a `docs/adr/` path or links `CONTEXT.md`; closed issues are untouched.
**Approach (assumed at the header base):** list open issues with hits (`git grep -l -E 'CONTEXT\.md|docs/adr' -- .capcom/issues`, then filter on status via `capcom show`); rewrite links as item 10 does, plain path mentions to `ADR NNNN` / "the glossary". Edit with `capcom update <id>` body flags where they exist, else in place, then `capcom fmt`.
**Regression guard.** In issue bodies a link rewrites to the root-relative `.capcom/knowledge/decisions/adr-NNNN.md` (all 23 existing `](docs/…)` issue links are root-relative), or to plain `ADR NNNN`, never re-pathed relative to the issue file; `--body-replace-old` refuses text that occurs more than once (code-affordance-parity.md cites ADR 0044 twice), so such a file is edited in place.
**Files:** .capcom/issues/ (open issues with hits only)
**Read first:** .capcom/config.yaml — statuses (parked → waiting); .capcom/issues/code-affordance-parity.md; .capcom/issues/adaptive-prompt-complexity.md; .capcom/issues/phase5-owner-verification/windows-below-floor-notice.md;
.capcom/issues/skill-auto-attach.md; .capcom/issues/model-schedule-tool.md, instruction-block-override.md
**Tests:** no Go change; the Acceptance commands are the check.
**Acceptance:**
- for every issue `capcom list` shows as open, `git grep -E 'CONTEXT\.md|docs/adr'` on its file prints nothing
- `capcom check && capcom fmt --check`
**Commit:** `docs(issues): cite decision records as Notes in open issues`

## 13. Pointer stubs and AGENTS.md

**What:** Depends on items 10, 11 and 12.
**Goal:** `CONTEXT.md` and `docs/adr/README.md` are pointer stubs of at most 10 lines and `docs/adr/` holds nothing else; AGENTS.md sends readers to the `glossary` and `decisions` Notes; the managed block lists both; the shared reference grep without its `CONTEXT.md`, `docs/adr` and `AGENTS.md` exclusions prints only stub lines.
**Approach (assumed at the header base):**
- `CONTEXT.md` stub: the glossary is the `glossary` Note tree (`.capcom/knowledge/glossary/`, plain markdown); read a term with `capcom know show "<Term>"`; edit the files in place.
- `docs/adr/README.md` stub: records are `adr-NNNN` Notes under `decisions` (`capcom know show "ADR 0076"`); a new record takes the next number (`adr-0099`) in capcom's decision-record shape — a record Note plus one child per section, `capcom know add --parent decisions`. Never cite apogee's own ADR 0010 for that shape.
- `git rm` the 98 ADR files.
- AGENTS.md: every line naming `CONTEXT.md` or `docs/adr` (intro, §Where knowledge lives bullets incl. the `grep -n '^\*\*Term'` instruction, the "docs/adr/ keeps settled decisions" clause, ADR links) names the Notes instead: terms via `capcom know show "<Term>"` and `capcom know tree glossary --depth 1`, records via `capcom know show "ADR NNNN"`. Then `capcom agents`.
**Regression guard.** (a) links inside the Notes are checked too — item 13's Acceptance runs check_links.py over `.capcom/knowledge` as well, and adds a grep over `.capcom/knowledge` for markdown link targets into `docs/adr/`, a bare `NNNN-slug.md` ADR filename, or `CONTEXT.md`, which must print nothing (verbatim prose that merely names CONTEXT.md is allowed; only link targets fail); (b) the `CONTEXT.md` stub also states that code comments cite glossary terms as `CONTEXT: <Term>` and that `capcom know show "<Term>"` resolves them, so the ~40 `(CONTEXT: <Term>)` Go comments stay valid; (c) `docs/adr/README.md` does not exist at the base — item 13 creates it, and the header's "Old files" ratified call is reworded to say CONTEXT.md becomes a stub and a new docs/adr/README.md stub is created.
**Files:** CONTEXT.md, docs/adr/ (README.md new, 98 files removed), AGENTS.md
**Read first:** AGENTS.md — "Where knowledge lives" bullets for CONTEXT.md and docs/adr/, the "What capcom owns" clause, "Be context-greedy", ADR 0006/0071/0076/0096/0097/0098 links;
docs/design/archived/technical-design.md — the `../adr/` links that will dangle; internal/domain/doc.go — package doc that names the glossary;
cmd/release/main.go — message constant (item 11) that must still resolve after the rm
**Tests:** no Go change; the Acceptance commands are the check.
**Acceptance:**
- `ls docs/adr` prints only `README.md`; `wc -l < CONTEXT.md` and `wc -l < docs/adr/README.md` ≤ 10
- the shared reference grep minus `':!CONTEXT.md' ':!docs/adr' ':!AGENTS.md'` prints only lines of the two stubs
- `! grep -q 'no Notes yet' AGENTS.md`
- `python3 docs/skill-runs/migrate-knowledge/check_links.py AGENTS.md CONTEXT.md docs/adr docs/manual docs/design layout.md docs/layout .capcom/knowledge`
- `grep -rnE '\]\([^)]*(docs/adr/|[0-9]{4}-[a-z0-9-]+\.md|CONTEXT\.md)' .capcom/knowledge` prints nothing
- `capcom check && capcom fmt --check`
**Commit:** `docs: leave pointers where the glossary and ADRs were`

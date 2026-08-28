# Plan: make `skills.Catalog.Suggest` predictable — raw-word gate, safer stemmer, prefix match, id-weighted ranking

**Goal:** the skill-suggestion band (plan `2026-08-27 - 01`, archived; ADR 0061) works, but three
of the matcher's rules behave differently from what a person typing expects, so the band reads as
"spotty". Probed against the owner's real 24-skill library on 2026-08-27:

| Draft | Today | Why |
|---|---|---|
| `grill me on this plan` | nothing | 5 words, but `me`/`on`/`this` are stopwords → 2 content terms < 3-term gate |
| `get me up to speed on this project` | nothing | `speed` stems to `spe`, `up`/`to`/`on`/`this` dropped → 2 terms, one mangled |
| `audit the parser for security holes` | ok | but `holes` → `hol`: the summary's "holes" only matches because it is mangled the same way |
| `cut a release for homebrew` | `/test-checklist` first | "release" repeats in test-checklist's summary; a hit in brew-release's **id** weighs the same as one buried in a summary |

This plan fixes exactly those three rules — the gate's unit, the stemmer's guards plus a prefix
match, and a bonus for id/display-name hits — and pins the real-library behaviour with a committed
fixture so future tuning stays honest. Everything else in the matcher (BM25, the ≥ 2-term evidence
gate, the trigger boost, the spent-at-send rule, the band) is unchanged.

**Date:** 2026-08-27 · **Status:** unexecuted · **Sized for:** ~200k-context host

**Authoritative sources:**

- `internal/skills/suggest.go` at `59edeb9a` — the matcher: `tokenize` / `stem` / `stemSuffixes`
  (~line 300–345), `minContentWords = 3` (~line 29), `minMatchedTerms = 2`, `minStemRunes = 3`,
  `index.score` (~line 260), `document{id, terms, length, triggers}`, `Suggest` (~line 112).
- `internal/skills/suggest_test.go` — `suggestFixture()` (8 skills), `TestTokenize` table
  (~line 250), `BenchmarkSuggest`.
- `internal/skills/load.go` — `Load(Sources{Home, Workspace, UseProjectSkills})`; `Home`'s
  `skills/` subdir is the global library (line 119: `skillAnchor{base: src.Home, rel: "skills"}`).
- ADR 0061 `docs/adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md` —
  Decision 1 (~line 46–50: "≥ 2 distinct non-stopword draft terms … top 3") and the Consequences
  bullet "nothing is computed at all until the draft holds three content words" (~line 93).
- CONTEXT.md *Skill* entry, the **Suggestion** paragraph (~line 1066): "BM25 + evidence gate" —
  still true after this plan; no edit.
- `docs/manual/commands.md` "Suggested skills" (~line 86–130) and `docs/manual/configuration.md`
  "Skill suggestions" (~line 276–310) — both already say "enough words" / "the closest three";
  neither names the stemmer or the gate's unit, so neither changes.
- The owner's library, the source of item 3's fixture: `~/.apogee/skills/` (a symlink to
  `/workspace/repos/skills/` on the dev box; 24 skill directories, each `<id>/SKILL.md`).

**Ratified design calls (owner, 2026-08-27, via AskUserQuestion during plan writing):**

1. **Gate unit:** the "enough draft" gate counts **raw whitespace-separated words, ≥ 3**, stopwords
   included — `grill me on this plan` qualifies. At least one content term must survive tokenising
   or nothing is scored (a draft of pure stopwords never shows a row).
2. **Fuzzy match:** beyond exact equality, a draft term matches an indexed term when either is a
   **prefix of the other and the shorter is ≥ 4 runes** (`releas` ↔ `release`, `release` ↔
   `releases`, `plan` ↔ `plann`). No substring matching — `homebrew` does NOT match `brew`.
3. **Field weight:** id / display-name hits add an **additive bonus** of `1.0 × idf(matched term)`
   per draft term whose matched document term appears in the id or display name — on top of BM25,
   never inside its term frequency. (BM25F-style tf weighting rejected: tf saturation at k1=1.2
   blunts it and it lengthens the document.)
4. **Regression home:** a **committed testdata fixture** — the real library's id + description
   frontmatter, no bodies — under `internal/skills/testdata/library/skills/<id>/SKILL.md`, loaded
   through the ordinary `Load`. (Env-gated test against `~/.apogee/skills` rejected: no CI
   coverage.)

**Standing requirements:**

- `skills: coding-standards`
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- Never change `VERSION`, a CHANGELOG release heading, or a tag (see closing note).
- Per-item acceptance is targeted; `make check` runs once at closeout.
- `suggest.go` stays ONE deep module (tokeniser, index, scoring, gate) — no new file for the
  matcher, no new package, no dependency. Every new constant sits beside the existing ones with a
  one-line rationale, as `minContentWords` and `triggerBoostIDFs` are written today.
- The index stays immutable and lock-free (built once in `finalize`); nothing here may add a
  per-keystroke allocation that is not O(draft terms × document terms) over a corpus of dozens.
- The `Suggest` signature and the `Suggestion` struct do not change — the TUI (`suggestband.go`)
  and the Provider delegate are untouched by this plan.

**Out of scope:**

- Substring / infix matching, edit-distance, synonyms, or any stemmer library (the in-house rules
  gain guards; they are not replaced).
- Any change to the ≥ 2-distinct-term evidence gate, the trigger boost, `defaultSuggestLimit`, or
  the ordering rule (score desc, id asc).
- `triggers:` on any real skill — the fixture copies what the library declares today (none).
- The band, Tab handling, the spent rule, `ui.skill-suggestions` — all `internal/tui`, untouched.
- Changing what the model sees (ADR 0061 Decision 2 stands as written).

---

## 1. Stemmer guards and the raw-word gate — ✅ DONE (2026-08-28)

NOTES (2026-08-28): guards are an `applies func(word, stemmed string) bool` field on the `stemSuffixes` rows plus a small `hasAnySuffix` helper, all inside `suggest.go`; the length floor is checked before the guard so a floor refusal still keeps the whole word (`goes`, `ties`) while a guard refusal falls through to the next rule (`holes` → `hole`).
NOTES (2026-08-28): the `TestTokenize` `speed need agreed` row uses `feed` in place of `need` — `need` is a stopword and never reaches the stemmer; `feed` is on the item's own list of protected words.
NOTES (2026-08-28): deviation — `docs/manual/configuration.md` line 319 ("at least three content words") contradicted the new gate; the plan said the manual needs no edit, but the sentence names the gate's unit, so it now reads "at least three words". No other manual text changed.
NOTES (2026-08-28): `internal/tui/suggestband_test.go` line 56–57 comment still says "under three content words … minContentWords" — a comment in the TUI, which the plan leaves untouched; harmless (compiles), worth a one-word tidy some day.

**What:** two rule fixes inside `internal/skills/suggest.go`; no signature changes.

- **Stemmer guards** (`stem`, `stemSuffixes`, `minStemRunes`) — keep the one-pass,
  first-rule-wins shape and the ≥ 3-rune stem floor, add these guards as binding rules, each with
  a one-line comment naming the word it protects:
  - `ies` → `y`: unchanged.
  - `es`: strip only when the stem would end in `x`, `z`, `ch`, `sh` or `ss` (`boxes` → `box`,
    `wishes` → `wish`, `classes` → `class`); otherwise fall through to the `s` rule
    (`holes` → `hole`, `releases` → `release`, `changes` → `change`). This is the one place the
    "first rule that applies" order changes: a rule that declines by guard (not by the length
    floor) lets the next rule try. The length-floor behaviour is unchanged: a rule refused by
    the floor still returns the whole word.
  - `s`: never strip when the word ends in `ss` or `us` (`stress`, `process`, `status`,
    `focus`).
  - `ed`: never strip when the stem would end in `e` (`speed`, `need`, `feed`, `agreed`); so
    `planned` → `plann`, `speed` → `speed`.
  - `ing`: unchanged (`planning` → `plann`, `thing` → `thing` by the floor).
- **Raw-word gate** — replace `minContentWords = 3` with `minDraftWords = 3` (rationale: "counts
  the words a person sees, stopwords included — the band must not go dark because `me`, `on` and
  `this` were dropped"). In `Suggest`: `if len(strings.Fields(draft)) < minDraftWords || len(queryTerms) == 0 { return nil }`.
  The TUI already strips resolving `/tokens` and `@refs` before calling; the count is over what it
  passes.
- Update the comment on the `Suggest` doc block ("returns nil — never a partial guess — when the
  draft holds fewer than three words or no content term at all").

**Files:** `internal/skills/suggest.go`, `internal/skills/suggest_test.go`

**Tests:** `TestTokenize` table gains/changes rows: `holes` → `hole`; `releases` → `release`;
`boxes wishes classes` → `box wish class`; `stress process status focus` unchanged; `speed need
agreed` unchanged; `planned planning` → `plann plann`; existing `goes ties` row still holds. A new
`TestSuggestGateCountsRawWords`: `grill me on this plan` (5 words, 2 terms) over the existing
fixture returns `grill-me`; `grill plan` (2 words) returns nil; `the and of to` (4 words, 0
terms) returns nil. Rename/retarget `TestSuggestBelowTheContentWordFloorReturnsNothing` to the
raw-word rule.

**Acceptance:** `go build ./... && go test ./internal/skills/ -count=1 && go vet ./internal/skills/`

**Commit:** `fix(skills): Suggest — raw-word gate and guarded stemmer`

---

## 2. Prefix matching and the id / display-name bonus

Depends on item 1.

**What:** ranking changes inside `internal/skills/suggest.go`; index gains one precomputed set.

- **Document name terms:** `document` gains `nameTerms map[string]bool` — the distinct tokens of
  `tokenize(s.ID + " " + s.DisplayName)`, built in `buildIndex` beside `terms`.
- **Term matching:** add `func (d document) match(q string) (term string, tf int)`. Exact hit
  first (`d.terms[q]`). Otherwise scan `d.terms` for terms where `strings.HasPrefix(long, short)`
  with the shorter side ≥ `minPrefixRunes = 4` (rationale: "four runes is the shortest stem that
  names a topic — `plan`, `test`, `code` — below it `run` would claim `running` and `rune`");
  among candidates take the highest `tf`, ties broken by the lexicographically smallest term so
  the result is deterministic. Returns `("", 0)` on no match. A query term matches at most one
  document term, so `matched` still counts distinct draft terms and the evidence gate's meaning
  is unchanged.
- **Scoring:** `index.score` calls `doc.match(q)` per query term; the BM25 contribution uses the
  matched term's `tf` and `idf(matchedTerm)` (the document term's IDF — the draft's spelling has
  no document frequency of its own). Then the bonus: when `doc.nameTerms[matchedTerm]`, add
  `nameBonusIDFs × idf(matchedTerm)` with `const nameBonusIDFs = 1.0` (rationale: "a word the
  author put in the skill's NAME is the skill's topic; a summary mention is a use of it — one
  IDF's worth keeps an id hit ahead of any single summary repeat without drowning a two-term
  summary match"). The trigger boost stays as it is, added after.
- The prefix scan is O(document terms) per query term per document; the corpus is dozens of
  documents of ~30 terms, so per keystroke it is a few thousand string comparisons. Re-run
  `BenchmarkSuggest` and record the new µs/op in a NOTES line under this item (informational).

**Files:** `internal/skills/suggest.go`, `internal/skills/suggest_test.go`

**Tests:** on the existing fixture: `TestSuggestPrefixMatchesAStemEitherWay` — `cut a releas for
the tap` admits `brew-release` (query prefix of document term) and `compile release checklists
now` admits `test-checklist` (`checklists` → `checklist` exact, plus `release`); a 3-rune term
(`cut`) never prefix-matches `cutting` (below `minPrefixRunes`).
`TestSuggestNameHitsOutrankSummaryRepeats`: `cut a release for homebrew` ranks `brew-release`
first (its id holds `brew` + `release`) over `test-checklist` even with `brew-release`'s
`Triggers` set to nil for the case; with the fixture's triggers intact `TriggerHit` still wins
(existing `TestSuggestPutsATriggerHitFirst` unchanged). `TestSuggestBreaksScoreTiesByIDAscending`
and `TestSuggestLimitCapsTheResult` still pass unchanged — if a fixture score changes under the
bonus, adjust the fixture's summaries, not the assertions' intent.

**Acceptance:** `go build ./... && go test ./internal/skills/ -count=1 && go vet ./internal/skills/`

**Commit:** `feat(skills): Suggest — prefix term matching and an id/display-name bonus`

---

## 3. Real-library fixture and regression cases

Depends on item 2.

**What:** pin the behaviour the owner actually sees against a catalog-shaped copy of the real
library.

- Create `internal/skills/testdata/library/skills/<id>/SKILL.md` for every directory in
  `~/.apogee/skills/` that holds a `SKILL.md` (24 on 2026-08-27; skip anything that is not a
  `<dir>/SKILL.md`, e.g. loose files at the top level). Each fixture file is the source's
  frontmatter block (`---` … `---`, with `name:` and `description:` exactly as written, any
  `triggers:` kept) followed by one body line `Fixture body — see testdata/library/README.md.`
  Bodies are NOT copied (they are large, some are private-ish, and `Suggest` never reads them).
  Write `internal/skills/testdata/library/README.md` (5–8 lines): what this is, that it mirrors
  the owner's library frontmatter as of 2026-08-27, how to refresh it (the one-liner below), and
  that only id/name/summary/triggers matter to the test.
  Refresh one-liner to record in the README:

  ```bash
  for d in ~/.apogee/skills/*/; do id=$(basename "$d"); f="$d/SKILL.md"; [ -f "$f" ] || continue;
    mkdir -p internal/skills/testdata/library/skills/$id;
    { awk 'NR==1&&$0!="---"{exit} {print} NR>1&&$0=="---"{exit}' "$f";
      echo; echo "Fixture body — see testdata/library/README.md."; } \
      > internal/skills/testdata/library/skills/$id/SKILL.md; done
  ```

- New file `internal/skills/suggest_library_test.go`: loads the fixture via
  `Load(Sources{Home: "testdata/library"})`, asserts the catalog holds ≥ 20 skills (so a broken
  fixture cannot pass vacuously), then a table of drafts with the binding expectation per row:

  | Draft | Expect |
  |---|---|
  | `grill me on this plan` | first = `grill-me` |
  | `grill me about this design plan` | first = `grill-me`; result contains `grill-with-docs` |
  | `audit the parser for security holes` | result contains both `code-audit` and `security-audit` |
  | `cut a release for homebrew` | first = `brew-release` |
  | `compact this conversation into a handoff` | first = `handoff` |
  | `what changed since the last release and how do I test it` | first = `test-checklist` |
  | `get me up to speed on this project` | first = `refocus` |
  | `fix the parser` | nil (2 words) |
  | `the and of to` | nil (no content term) |

  A row that cannot be met without editing the matcher is NOT patched around: the implementer
  reports it (BLOCKED with the row named) rather than weakening the expectation or the fixture.
- Add the fixture directory to `internal/skills/doc.go`'s narration in one sentence next to
  `suggest.go` (the package's docmap, if it has one, must name it — check `doc.go` first).

**Files:** `internal/skills/testdata/library/README.md`,
`internal/skills/testdata/library/skills/*/SKILL.md` (24 new files),
`internal/skills/suggest_library_test.go`, `internal/skills/doc.go`

**Tests:** the new table test itself, plus `go test ./internal/skills/ -run Library -v` printing
each row's top-3 (t.Logf) so a reviewer can see the ranking, not only the pass.

**Acceptance:** `go build ./... && go test ./internal/skills/ -count=1 -run 'Library|Suggest' -v 2>&1 | grep -c "^--- PASS" && go test ./internal/skills/ -count=1`

**Commit:** `test(skills): real-library fixture pins Suggest's ranking on the phrases people type`

---

## 4. ADR 0061 and package doc wording

Depends on item 2 (the wording must describe what landed).

**What:** docs only — bring the two places that describe the gate and the matching rule in line.

- `docs/adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md`:
  - Decision 1 (~line 46–50): after "BM25 over a document of id + display name + summary + …
    `triggers:` phrases (bodies are **not** indexed)", insert ", a draft term matching a document
    term exactly or by a ≥ 4-rune prefix either way, an id/display-name hit adding one IDF's worth
    of bonus," before "a trigger-phrase hit adding a fixed boost". Keep "≥ 2 distinct non-stopword
    draft terms" as is (still true).
  - Consequences (~line 93): "until the draft holds three content words" → "until the draft holds
    three words (stopwords included) and at least one content term".
  - Append a dated one-line `Amended 2026-08-27:` note at the end of the Decision section naming
    the three rule changes and this plan (an ADR amendment, not a supersession — the decision's
    substance is unchanged).
- `internal/skills/doc.go` (~line 42, the `suggest.go` sentence): mention prefix matching and the
  name bonus in the same breath as BM25 if the sentence currently enumerates the rules; leave it
  if it does not.
- `CHANGELOG.md` is NOT edited here — the entry text goes in this item's sidecar (closeout
  applies it): "Skill suggestions: the band now counts the words you typed (stopwords included),
  the stemmer no longer mangles `holes`/`speed`/`stress`, a term matches by ≥ 4-rune prefix
  (`releas` finds `release`), and a hit in a skill's id or name outranks a summary repeat —
  `cut a release for homebrew` now names `/brew-release` first."

**Files:** `docs/adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md`,
`internal/skills/doc.go`

**Tests:** none (docs). `grep -n "prefix" docs/adr/0061-*.md` finds the amendment;
`go vet ./internal/skills/` still passes (doc.go compiles).

**Acceptance:** `grep -q "Amended 2026-08-27" docs/adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md && grep -q "stopwords included" docs/adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md && go build ./internal/skills/`

**Commit:** `docs(adr): 0061 amended — raw-word gate, prefix match, name bonus`

---

**Suggested version bump:** micro (`0.18.x` → next micro) after closeout — a user-visible
behaviour fix to a shipped feature; the owner decides.

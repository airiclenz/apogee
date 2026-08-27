# Plan: skill suggestions — a host-side band that names the skills fitting the draft

**Goal:** as skill libraries grow, apogee must help the user find the fitting skill WITHOUT
advertising the catalog to the model. Today the model learns a skill exists only when the user
types `/id` (CONTEXT.md "Skill", ADR 0027); nothing about skills reaches the prompt otherwise. This
plan keeps that contract and adds a Driver-side suggestion: while the user types, an engine-level
matcher (BM25 over id + name + summary + an optional author-declared `triggers:` list) ranks the
catalog against the draft, and the TUI paints the top matches in a one-row band above the input
box. Tab opens the `/` menu filtered to those rows; a skill shown at the moment a message is sent
is spent for the session and never suggested again. Model-facing discovery (auto-attach, a
`load_skill` tool) is deferred and recorded, not built.

**Date:** 2026-08-27 · **Status:** unexecuted · **Sized for:** ~200k-context host

**Authoritative sources:**

- CONTEXT.md "Skill" glossary entry (~line 1042) — a skill is turn-local prompt text the USER
  invokes; "_Avoid_: 'tool'". This plan does not change what reaches the model.
- ADR 0027 (one slash namespace, inline `/token`), ADR 0032 (library precedence), ADR 0053 (popup
  surfaces embed one list surface), ADR 0037 (every settings edit applies live), ADR 0031 (the
  engine stays sufficient for any Driver — the matcher lives in `internal/skills`, presentation in
  the TUI).
- `internal/skills/parse.go` (`frontmatter` struct ~line 58, strict-then-lenient
  `parseFrontmatterFields`, `maxSummaryLen = 200`), `internal/skills/catalog.go` (`Catalog`,
  keep-first `set`), `internal/skills/provider.go` (atomic snapshot swap).
- `internal/tui/autocomplete.go` (`acKind`, `acItem`, `autocompleteState`, `skillSuggestions`
  ~line 553, `autocompleteKey` ~line 655 — Tab is the overlay's second accept key,
  `insertSkillToken`, `autocompleteTitle`, `maxAutocompleteItems = 8`).
- `internal/tui/interject.go` (`bandPlan`, `bandShape` ~line 446, `renderPendingInterjections`
  ~line 471, `queuedRow`) and `internal/tui/model.go` `frameRowPlan` (~line 3318: `plan.band =
  bandShape(len(m.pendingInterjections), left)`), `submit()` (~line 1480), `stageInterjection`
  (`interject.go` ~line 155), `runClear` path (`commandrun.go` ~line 135, after `ClearContext`).
- `internal/tui/settingsapply.go` `settingsApplyLocal` (~line 226) — the pattern for a TUI-local
  `ui.*` key applied without an engine seam; `internal/config/registry.go` (`ui.spinner` row
  ~line 422) and `internal/config/config.go` (`use-project-skills` fromFile ~line 533).
- layout.md "The staged-interjection band" (~line 1378) — the painting and give-way rules the new
  band copies.
- Precedent survey (2026-08-27 brainstorm): Anthropic tool search (`tool_search_tool_bm25` /
  `_regex`, top-5, prefix untouched), Codex skills list (2 % context cap, shortens then omits),
  OpenCode `skill` tool, Cursor "Auto Attached" globs, community Claude Code `UserPromptSubmit`
  keyword hooks.

**Ratified design calls (owner, 2026-08-27, via AskUserQuestion during /grill-me):**

1. **Scope:** Option A (host-side suggestion) ships now. Option B (model-facing discovery — B1
   auto-attach, B2 a `load_skill` tool) is NOT built; the matcher is written as an engine-level
   `skills` API so B1 could reuse it, and B is recorded in ISSUES.md pointing at a future grill.
2. **Surface:** a one-row suggestion band above the input box, sibling of the staged-interjection
   band, same painting rules (black field, full width, `bodyIndent`, `…` clipping). Never modal,
   never steals Enter.
3. **Accept key:** Tab, with the band showing and no `/`/`@` overlay open, opens the existing
   selector popup filtered to the suggested skills (top match highlighted); Tab/Enter accept as
   today, Esc closes. Tab with nothing to suggest stays inert.
4. **Corpus:** BM25 over id + display name + summary + `triggers:`; bodies are NOT indexed.
5. **`triggers:`:** optional top-level SKILL.md frontmatter key; strict path accepts a YAML list
   or a comma-separated string, lenient path comma-splits the value. A trigger phrase hit adds a
   fixed boost on top of BM25 and is never the sole admission gate.
6. **Admission:** a skill is admitted when a trigger phrase hits OR ≥ 2 distinct non-stopword
   draft terms match its fields; admitted skills rank by BM25 (+ trigger boost); top 3 shown; the
   draft must hold ≥ 3 content words before anything shows.
7. **Dedup:** the skills in the band at the moment a message is SENT (submit or staged
   interjection) are spent for the session — never suggested again; skills already invoked
   (`/id` in the draft) are always excluded; the spent set resets on `/clear` and `/new`. Before
   sending, the band may change freely as the draft changes.
8. **Config:** a bool knob, default true, live via `/settings` (ADR 0037). Placed in the `ui.`
   group as `ui.skill-suggestions` because it is a TUI-local presentation key applied through
   `settingsApplyLocal` exactly like `ui.spinner` — the group is the plan-writer's routine
   placement; the knob, its default and its live behaviour are the owner's call. Off = the band
   never paints and Tab stays inert.
9. **ADR:** a short ADR 0061 records that suggestion is Driver-side UI over an engine-level
   matcher, never a Mechanism, and that model-facing discovery is deferred with the invariant
   reason.

**Standing requirements:**

- `skills: coding-standards`
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- Never change `VERSION`, a CHANGELOG release heading, or a tag (see closing note).
- Per-item acceptance is targeted; `make check` runs once at closeout.
- `internal/tui` and `internal/config` narrate every file in their `doc.go` (docmap test): a new
  file in either package must be named there in the same item that creates it.
- The Bubble Tea `Model` is value-copied on every Update (ADR 0011): no `strings.Builder` or other
  no-copy type by value on anything the Model reaches.
- Every string that reaches the screen from a SKILL.md (display name, summary, trigger) is
  escape-stripped and flattened where the row is built (`stripEscapes`, `flattenField`), as
  `skillSuggestions` already does.

**Out of scope:**

- Auto-attaching a skill body without a `/id` in the message (B1) and any model-callable skill
  loader (B2) — deferred to a future grill, recorded in item 1.
- Indexing skill bodies, embeddings, or any matcher needing a model or a network.
- Headless / daemon presentation of suggestions (the engine API is available to them; no Driver
  consumes it yet).
- Persisting the spent set across a session resume (`/sessions`) — a restored session starts
  with an empty spent set.
- Live-while-typing recompute of the `/skills` LISTING; `/skills` only gains a triggers column.
- Any change to what the agent loop prepends for an invoked skill (`loop.go` `resolveSkillRefs`).

---

## 1. ADR 0061, CONTEXT.md glossary addendum, ISSUES.md deferral entry — ✅ DONE (2026-08-27)

NOTES (2026-08-27): the Suggestion sentence is appended INSIDE the *Skill* entry's block (no blank
line before it) — in CONTEXT.md a blank line followed by a bold term starts a NEW glossary entry,
and Suggestion is defined as part of *Skill*; it sits after the entry's `See …` sentence, which
trails "the agent resolves." so that line could gain the ADR 0061 link the item also asks for.

NOTES (2026-08-27): the paragraph's closing "see ADR 0061" is plain text rather than a second
link, since the See line two lines above already links the ADR.

**What:** docs only.

- Write `docs/adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md` in the house
  ADR shape (`Status: accepted` frontmatter, `# <title>`, `## Context`, `## Decision`,
  `## Consequences`). Context: the catalog is host-side; the model sees a skill only through a
  `/id`; libraries grow; the industry default (Claude Code, Codex) advertises every skill's name +
  description to the model, which apogee refuses because it costs context on every request and
  would have to be a gated Mechanism under the Bypass invariant. Decision: (1) suggestion is a
  Driver concern painted from an engine-level matcher `skills.Catalog.Suggest` (BM25 over id +
  name + summary + `triggers:`, evidence gate, top 3); (2) nothing about the catalog reaches the
  model — a suggestion becomes model-visible only when the user accepts it into a `/id`; (3) the
  spent-at-send dedup rule; (4) model-facing discovery (auto-attach B1, `load_skill` tool B2) is
  deferred: either is a Mechanism that must be benched against Bypass and B2 reopens CONTEXT.md's
  "a skill is not a tool" — a later ADR must supersede this one explicitly to build them. Cite
  ADR 0027, 0031, 0053, 0037 and the precedent survey (Anthropic tool search, Codex 2 % cap,
  OpenCode `skill` tool, Cursor auto-attach) in one short paragraph.
- CONTEXT.md, the "Skill" glossary entry: append one paragraph after "The TUI parses and offers
  …; the agent resolves." defining **Suggestion**: "a Driver-side hint naming the catalog skills
  whose id, name, summary or `triggers:` best match the draft (`skills.Catalog.Suggest`, BM25 +
  evidence gate); painted by the TUI in the suggestion band above the input box, accepted via Tab
  into a `/token`, and spent for the session once a message is sent with it showing. A suggestion
  never reaches the model — see ADR 0061." Add `triggers:` to the frontmatter list in the entry's
  first sentence ("id, display name, summary, optional triggers"). Add the ADR to the entry's See
  line.
- ISSUES.md, under `## Parked / deferred work`, add a `### Model-facing skill discovery (B1
  auto-attach / B2 load_skill tool) — deferred by ADR 0061` section (3–8 lines): what each would
  be, why deferred (Mechanism under the Bypass invariant; B2 contradicts the "Skill" glossary),
  what must precede it (a grill + superseding ADR, a bench arm), and that the matcher from this
  plan is the reusable half.

**Files:** `docs/adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md`,
`CONTEXT.md`, `ISSUES.md`

**Tests:** none (docs). `grep -n "0061" CONTEXT.md ISSUES.md` finds both references.

**Acceptance:** `test -f "docs/adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md"
&& grep -q "Suggestion" CONTEXT.md && grep -q "ADR 0061" ISSUES.md`

**Commit:** `docs(adr): 0061 — skill suggestions are Driver-side over an engine matcher`

---

## 2. `triggers:` frontmatter field on `skills.Skill` — ✅ DONE (2026-08-27)

NOTES (2026-08-27): the item names `internal/tui/skills_test.go`; the package's test file is
`internal/tui/skill_test.go` (singular) and the existing `skillCatalogNote` tests live there, so the
two new tests were added beside them rather than in a second file.

NOTES (2026-08-27): `frontmatter` stopped being comparable once it gained the `triggersField` slice,
so `scanFrontmatterFields`' `fm != (frontmatter{})` ok-check became `frontmatter.hasNamingField()`.
It deliberately EXCLUDES `Triggers`: triggers alone can never make a skill loadable, so a block that
recovered nothing else still reports the strict parser's YAML error (which names the offending line)
rather than the vaguer "missing a required field" — the pre-existing diagnosis is unchanged.

**What:** teach the loader an optional author-declared trigger list.

- `internal/skills/skill.go`: add `Triggers []string` to `Skill` with a doc comment: lowercase,
  whitespace-normalised phrases the author expects in a prompt that this skill fits; optional;
  read only by `Suggest` (item 3); never shown to the model.
- `internal/skills/parse.go`:
  - `frontmatter` gains `Triggers triggersField `yaml:"triggers"``, where `triggersField` is a
    `[]string` with a custom `UnmarshalYAML` accepting a YAML sequence of scalars OR one scalar
    split on commas. Any other node kind is a soft field error: the skill still loads, triggers
    empty (do not sink a skill over its triggers).
  - Lenient scan (`scanFrontmatterFields` / `recognisedKeys`): recognise `triggers` and
    comma-split the folded value.
  - `normalizeTriggers`: trim, lowercase, collapse internal whitespace, drop empties, dedupe
    (first wins), cap each phrase at 64 runes and the list at 32 entries (extra ones dropped —
    the caps are constants next to `maxSummaryLen` with a one-line rationale each).
  - `parseWithFrontmatter` sets `Triggers`; `parseFallback` leaves it empty; `validate` does not
    require it.
- `internal/tui/skills.go` `skillCatalogNote`: when a skill has triggers, add one indented
  `triggers: a, b, c` line under its row (escape-stripped, flattened, clipped at 120 runes with
  `…`). No column change when a skill has none.

**Files:** `internal/skills/skill.go`, `internal/skills/parse.go`, `internal/skills/parse_test.go`,
`internal/tui/skills.go`, `internal/tui/skills_test.go`

**Tests:** table cases in `parse_test.go` — YAML list; comma string; list with duplicates and
mixed case; a mapping node (soft error, skill loads with empty triggers); lenient path with an
unbalanced quote elsewhere in the block still yields triggers; caps (33 entries → 32, a 70-rune
phrase → 64). `skills_test.go`: the `/skills` note shows the triggers line for a skill with
triggers and no line for one without.

**Acceptance:** `go build ./... && go test ./internal/skills/ ./internal/tui/ -run
'Trigger|Frontmatter|Parse|SkillCatalog|Skills' -count=1`

**Commit:** `feat(skills): optional triggers: frontmatter list on a skill`

---

## 3. The matcher: `skills.Catalog.Suggest` (BM25 + triggers + evidence gate) — ✅ DONE (2026-08-27)

NOTES (2026-08-27): `internal/skills/load.go` is edited although the item's Files list names only
catalog.go/provider.go/doc.go — the item text puts the index build "in `Load` after the walk", and
`Load` lives in load.go; the change there is the single `cat.finalize()` call, with `finalize()`
itself in catalog.go as the item specifies.

NOTES (2026-08-27): the plan's stemmer order (`ies`→`y`, `es`, `s`, `ing`, `ed`, first match wins)
is implemented literally, so "releases" stems to "releas" while "release" stays "release". Left as
specified rather than upgraded to a Porter-style rule; it costs a match only on that one inflection
pair and the tokeniser table test records the behaviour.

NOTES (2026-08-27): `BenchmarkSuggest` measures ~244 µs/op over 1000 synthetic skills on this host
(informational, nothing asserted) — a real library is one to two orders of magnitude smaller.

Depends on item 2.

**What:** one new file `internal/skills/suggest.go` holding the whole matcher as one deep module;
no new package, no dependency.

- **Tokeniser** `tokenize(s string) []string`: lowercase; split on any rune that is not a letter
  or digit (`unicode.IsLetter`/`IsDigit`, so hyphenated ids split into their words); drop tokens
  shorter than 2 runes; drop English stopwords from a fixed `stopwords` set (~60 entries: articles,
  pronouns, auxiliaries, prepositions, "please", "want", "need", "can", "could", "would", "should",
  "make", "just", "like", "use", "also"); light stemming — strip a trailing `ies`→`y`, `es`, `s`,
  `ing`, `ed` when the remaining stem is ≥ 3 runes (one pass, in that order, no library).
- **Index** `type index struct` built ONCE per catalog: per skill a term-frequency map over the
  document = id + display name + summary + every trigger phrase (triggers are ordinary document
  text for BM25; the phrase boost below is separate), document length, corpus average length,
  document frequency per term. Built in
  `Load` after the walk (`catalog.go`: a `finalize()` step that builds `c.idx`), so the atomic
  snapshot the Provider serves is index-complete and immutable. Index cost is O(total tokens);
  no lazy build, no locks.
- **Scoring** `bm25(k1=1.2, b=0.75)` in the standard form over the draft's distinct tokens.
- **Trigger hit**: a trigger phrase hits when its tokenised sequence appears contiguously in the
  draft's token sequence (whole tokens, after the same normalisation). A hit adds
  `triggerBoost = 2.0 × the corpus's maximum single-term IDF` to the score so a hit outranks any
  purely lexical match of the same skill but two lexical skills still order among themselves by
  BM25. Record `TriggerHit` on the result.
- **Admission** (call 6): `contentWords := len(distinct(tokenize(draft)))`; when
  `contentWords < 3` return nil. A skill is admitted when `TriggerHit` OR the number of distinct
  draft tokens present in its document ≥ 2. Rank admitted skills by score desc, then id asc
  (deterministic); return at most `limit`.
- **API**:

  ```go
  type Suggestion struct {
      ID, DisplayName, Summary string
      Score      float64
      TriggerHit bool
  }
  // Suggest ranks the catalog against draft. exclude, when non-nil, drops a skill by id before
  // ranking (the caller's invoked/spent sets); limit caps the result (≤ 0 → 3).
  func (c *Catalog) Suggest(draft string, exclude func(id string) bool, limit int) []Suggestion
  ```

  `Provider` gains `Suggest(...)` delegating to the current snapshot exactly as its `List`/`Get`
  do (the binary passes the `*skills.Provider` as `tui.Options.Skills`, `cmd/apogee/wire_boot.go`
  ~line 221, so the Provider is what must satisfy the interface item 5 extends).
- `internal/skills/doc.go`: name `suggest.go` and state the contract in two sentences (host-side
  only; the model never sees a suggestion).
- Coding-standards calls made here: one deep module (`suggest.go` owns tokeniser, index, scoring,
  gate); the index is built at load and immutable — no mutex; constants (`k1`, `b`,
  `defaultSuggestLimit = 3`, `minContentWords = 3`, `minMatchedTerms = 2`) are named with a
  one-line rationale each.

**Files:** `internal/skills/suggest.go`, `internal/skills/suggest_test.go`,
`internal/skills/catalog.go`, `internal/skills/provider.go`, `internal/skills/doc.go`

**Tests:** `suggest_test.go` builds a fixture catalog of ~8 skills (ids such as `code-audit`,
`security-audit`, `brew-release`, `grill-me`, `handoff`, `refocus`, plus one with triggers
`["cut a release", "homebrew"]` and one with a summary that reuses common words). Cases: fewer
than 3 content words → nil; "please audit the parser for security holes" admits `security-audit`
and `code-audit` with `security-audit` first; "cut a release for homebrew" → the triggered skill
first with `TriggerHit`; a draft matching a single term of many skills admits none (gate);
`exclude` drops the top hit and the next fills in; `limit` respected; ordering deterministic on
ties (id asc); tokeniser table (stemming, stopwords, hyphen splitting, digits). A benchmark
`BenchmarkSuggest` over 1000 synthetic skills documents the cost (informational, not asserted).

**Acceptance:** `go build ./... && go test ./internal/skills/ -count=1 && go vet ./internal/skills/`

**Commit:** `feat(skills): Catalog.Suggest — BM25 + triggers matcher with an evidence gate`

---

## 4. The `ui.skill-suggestions` knob — ✅ DONE (2026-08-27)

NOTES (2026-08-27): the key resolves onto `UISettings.SkillSuggestions` (internal/config/config.go)
rather than a flat `Options.SkillSuggestions` in options.go as the item's text spells it. Every
`ui:` key of the block travels in that one struct through the shared `fileUI` projection
(`o.UI = fc.ui()`, "every key of a block writes the whole block"), and the registry/schema bijection
test walks `uiConfig` for the `ui.` paths — so the item's own headline, "register the bool exactly as
`ui.spinner-color` is", is what was followed. internal/config/options.go is therefore untouched and
the registry row reads `o.UI.SkillSuggestions`; the UISettings literals in the config tests gained
the new default.

NOTES (2026-08-27): cmd/apogee/wire_settings.go is untouched. `ui.spinner` has NO entry in
`settingsTable` — a renderer-owned key never reaches the binary's dispatcher at all (it is
intercepted by `settingsApplyLocal`) — so mirroring it exactly means naming the key in
`settingKeysAppliedByTheRenderer` (cmd/apogee/wire_test.go), the list
TestEveryEditableSettingKeyHasAnApply reads. Adding a `settingsTable` entry would have been dead
code.

NOTES (2026-08-27): the pass-through is cmd/apogee/wire_options.go, not wire_boot.go/wire_firing.go
— `tui.Options{...}` is composed in exactly one place in the binary.

NOTES (2026-08-27): internal/tui has no settingsapply_test.go; the renderer-owned-key table
(TestSettingsPaneRendererOwnedKeysApplyWithoutTheSeam) lives in settings_test.go, so the case was
added there. The table gained an optional `seed` hook because testOpts leaves the field at its zero
value — without it the case would have passed over an apply that did nothing (mutation-checked: the
case fails when the `settingKeySkillSuggestions` branch is removed).

NOTES (2026-08-27): the registry assertions (row present, editable, default "true", Read projects
the session's value) went into internal/config/registry_test.go as a focused test rather than into
a round-trip test — TestSpliceScalarSettingRoundTripsEveryEditableKey already walks every editable
key automatically, so the new row's write/reset round trip is covered without an edit.

NOTES (2026-08-27): the template ships the key as a COMMENTED example (`# skill-suggestions: true`)
as the item asks, which keeps the seeded config behaviour-neutral
(TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt).

**What:** register the bool exactly as `ui.spinner-color` is, then apply it locally in the TUI.

- `internal/config/config.go`: `fileConfig.UI` gains `SkillSuggestions *bool
  `yaml:"skill-suggestions"`` (pointer: absent ≠ false); `fromFile` sets
  `o.SkillSuggestions = fc.UI.SkillSuggestions == nil || *fc.UI.SkillSuggestions`.
- `internal/config/options.go`: `Options.SkillSuggestions bool`.
- `internal/config/registry.go`: row `Path: "ui.skill-suggestions", Kind: KindBool, Default:
  "true", Editable: true, Desc: "Show the skills that fit the message you are typing in a band
  above the input box; Tab opens the / menu on them."`, `Read` via `boolValue`.
- `internal/config/defaults/config.yaml`: a commented block in the `ui:` section next to
  `spinner-color`, same wording style, `# skill-suggestions: true`.
- `cmd/apogee/wire_settings.go`: add the key to the apply table with `reaches:
  reachesWithoutAMember` and the same "write alone" apply the other TUI-local `ui.` keys use
  (find the entry `ui.spinner` uses and mirror it exactly).
- `internal/tui/tui.go`: `Options.SkillSuggestions bool` (doc: mirrors `ui.skill-suggestions`);
  `cmd/apogee/wire_boot.go` (and `wire_firing.go` if it composes `tui.Options`) passes
  `opts.SkillSuggestions` through.
- `internal/tui/settingsapply.go` `settingsApplyLocal`: case `"ui.skill-suggestions"` sets
  `m.opts.SkillSuggestions = value == settingTrue` and returns handled (the band item reads it).

**Files:** `internal/config/config.go`, `internal/config/options.go`,
`internal/config/registry.go`, `internal/config/defaults/config.yaml`,
`internal/config/config_test.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_boot.go`,
`internal/tui/tui.go`, `internal/tui/settingsapply.go`, `internal/tui/settingsapply_test.go`

**Tests:** config: absent key → true; `ui: {skill-suggestions: false}` → false; registry row
present, editable, default "true" (extend the existing registry round-trip test). TUI:
`settingsApplyLocal("ui.skill-suggestions", "false")` flips `m.opts.SkillSuggestions` and reports
handled.

**Acceptance:** `go build ./... && go test ./internal/config/ ./cmd/apogee/ -count=1 && go test
./internal/tui/ -run 'Settings|DocMap' -count=1`

**Commit:** `feat(config): ui.skill-suggestions knob, live-applied in the TUI`

---

## 5. The suggestion band — ✅ DONE (2026-08-27)

NOTES (2026-08-27): the recompute is FOLDED into `recomputeAutocomplete` (internal/tui/autocomplete.go)
rather than added beside each call site, which is the choice the item's own text offers ("pick the fold
if every site is the same edit path") — all five call sites are the edit path (a typed key, a paste, a
splice, an undo, a withdrawn interjection put back in the box). autocomplete.go is therefore edited
although the item's Files list does not name it.

NOTES (2026-08-27): internal/tui/skill_test.go is edited for the same reason: widening `SkillCatalog`
with `Suggest` obliges its two fakes (`fakeSkillCatalog`, `reloadableCatalog`) to implement it, and
`fakeSkillCatalog` is where the canned-suggestion hook the band's tests drive belongs — the item's
"a fake SkillCatalog returning canned suggestions" is that fake, which lives in skill_test.go.

NOTES (2026-08-27): the item states both that `bandShape` "grants the hint row only when at least one
row remains after the queued band's own claim" AND that "the queued band's lower framing row is
shared: the hint row replaces it". Those cannot both be literal — a row that REPLACES the framing row
costs nothing. The sharing rule was kept (it is what "View stacks the hint row directly above the
input box" needs: the group closes on the hint, not on a blank), and the gate is the honest
generalisation of the sentence: the hint is offered the budget only after the queue has taken its
rows, and is granted when the plan WITH the hint still fits (`withHint.height() <= budget`). The two
readings differ only where the queue is seated, and there the literal gate would refuse a free row.
The give-way order is unchanged and tested: the queue claims first, and a hint is never granted a
budget the queue asked for and did not get.

NOTES (2026-08-27): the escape-strip test is written against the suggested skill's ID rather than the
DisplayName the item's Tests line names — the band's row shows ids (`/code-audit`), so the id is what
becomes screen here and what the seam invariant applies to.

NOTES (2026-08-27): two tests beyond the item's list, both about the surface the item adds to the
frame: `hasSkillHints` is the one predicate the frame's allocation and the render share, so switching
`ui.skill-suggestions` off takes the row off the very NEXT frame (ADR 0037) instead of the next
keystroke, and `TestBandNeverOverflowsTheFrame` sweeps 8..26 rows × 0/2/5 staged to hold D2 with the
new row on the frame.

NOTES (2026-08-27): a send does not yet clear `m.skillHints` — the band keeps its row over an emptied
box until the next edit. That is item 7's work ("mark each id spent … then clear `m.skillHints`"),
which is not yet done; `spentSkills` is declared here as the item asks and stays empty until then.

Depends on items 3 and 4.

**What:** new file `internal/tui/suggestband.go` — state, recompute and render; the frame budget
learns the row.

- `SkillCatalog` (`internal/tui/tui.go` ~line 30) gains
  `Suggest(draft string, exclude func(string) bool, limit int) []skills.Suggestion`.
- Model state: `skillHints []skills.Suggestion` (the rows currently shown) and `spentSkills
  map[string]bool` (item 7 fills it; declared here so the exclude closure is complete from the
  start — a nil map reads as empty).
- `recomputeSkillHints()` runs wherever `recomputeAutocomplete` runs (the editor's edit path —
  find the call sites of `recomputeAutocomplete` and add the hint recompute beside each, or fold
  it into the same function; pick the fold if every site is the same edit path). Inputs: the
  draft with every resolving `/token` and `@ref` removed (reuse `outsideRegion` /
  `extractSkillRefs` / the file-ref extractor so a `/code-audit` token does not match itself);
  exclude = attached ids ∪ `spentSkills`; limit 3. Skipped entirely (hints nil) when
  `!m.opts.SkillSuggestions`, when `m.opts.Skills == nil`, or when a `/` or `@` overlay is
  active (`m.autocomplete.active` with kind `acCommand`/`acFile`). Cost: one `Suggest` per
  keystroke — the index is prebuilt (item 3); no debounce, no Cmd.
- Render `renderSkillHints() string`: one row, painted exactly as `queuedRow` paints (same style,
  `bodyIndent`, black field to full width, ANSI-aware clip with `…`):
  `  ✦ skills: /grill-me · /code-audit · /handoff   tab to pick` — `glyphSkill`, the ids as
  `/id` in `skillToken`'s violet on the band's field, separator ` · `, and the trailing legend
  `tab to pick` in the band's faint text. Empty string when there are no hints.
- Frame budget: `bandPlan` (`interject.go`) gains a `hint` row (0 or 1); `bandShape` takes
  `hints bool` and grants the hint row only when at least one row remains after the queued
  band's own claim (the hint gives way FIRST — before the queued rows — because a staged message
  is a fact and a hint is advice). `frameRowPlan` passes `len(m.skillHints) > 0`; `height()`
  counts it; View stacks the hint row directly above the input box, BELOW the queued band's rows
  (the hint is about the draft, the draft sits in the box). With both bands showing the queued
  band's lower framing row is shared: the hint row replaces it so the group still reads as one
  block (state this in layout.md).
- layout.md: add `## The skill-suggestion band` after the staged-interjection section: what it
  shows and when, the one-row shape, painting (same as the queued band), stacking with the queued
  band, give-way order (first of all bands), the knob, Tab's role (item 6), and that it is never
  drawn while a `/` or `@` dropdown is open.
- `internal/tui/doc.go`: name `suggestband.go`.

**Files:** `internal/tui/suggestband.go`, `internal/tui/suggestband_test.go`,
`internal/tui/tui.go`, `internal/tui/model.go`, `internal/tui/interject.go`,
`internal/tui/interject_test.go`, `internal/tui/doc.go`, `layout.md`

**Tests:** `suggestband_test.go` with a fake `SkillCatalog` returning canned suggestions: hints
recompute on an edit and clear when the draft drops below the gate; a `/id` already in the draft
is excluded (exclude closure receives it); knob off → no hints, no row; overlay open → no row;
render has no ANSI leak past the row's width and ends padded to `m.width`; a SKILL.md display
name carrying an ESC byte is stripped. `interject_test.go`: `bandShape` grants the hint row only
after the queued rows, drops it first under a tight budget, and `height()` counts it; frame plan
with queued rows + hint stacks hint nearest the box.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run
'Suggest|Hint|Band|Interject|Frame|DocMap|NoBuilder' -count=1`

**Commit:** `feat(tui): skill-suggestion band above the input box`

---

## 6. Tab opens the `/` menu filtered to the suggestions — ✅ DONE (2026-08-27)

NOTES (2026-08-27): `skillRow` is extracted as the item asks, but it builds the row the MERGED menu
actually renders (`["✦ /id · source", DisplayName · Summary]`, `skill: true`) rather than the
intermediate two cells `skillSuggestions` used to hand `slashSuggestions`. That is what the item's own
reason for the extraction requires — "so both menus share one row shape" — and the accept path needs
it besides: `acceptAutocomplete` branches on `it.skill`, which only the finished row carries. The "/"
menu's rendered rows are byte-identical to before; `slashSuggestions` now appends the row it is handed.

NOTES (2026-08-27): `acItem.source` is deleted with that move. It existed only to carry the source dir
from `skillSuggestions` to the cell `slashSuggestions` composed; the cell is now composed where the
source is in hand, leaving the field written and never read.

NOTES (2026-08-27): `spliceCompletion` gained a LEADING separator, written only when the region's text
does not already end in whitespace. The item's acceptance ("inserts `/id ` at the caret and leaves the
rest of the draft intact") cannot hold without it: the suggestion menu's region is the empty one at the
caret, which typically stands at the end of a word, and `head + token` there fuses "parser/code-audit"
into a token no parse resolves. It is a no-op for the "/" and "@" regions, whose start is 0 or preceded
by whitespace by construction (`caretToken`).

NOTES (2026-08-27): `hasSkillHints` (suggestband.go, item 5's file) gained `!m.autocomplete.active`.
The item states item 5's overlay rule already keeps the band off the frame while the suggest menu is
up, but that rule lives in `recomputeSkillHints`, and Tab opens the menu WITHOUT re-deriving the hints
— so the reachable combination is one this item creates. Enforcing it in the one predicate the frame
allocation and the render share keeps them from disagreeing.

NOTES (2026-08-27): the `"tab"` case is additionally gated on `m.state.live()`. Hints are re-derived
only at idle and while a worker runs, so at an approval or an ask they are whatever the last edit left;
without the gate Tab would open a menu over a decision surface, which is precisely what
`dismissAutocomplete` exists to prevent.

NOTES (2026-08-27): layout.md's band section is edited although the item's Files list does not name it
— the "when it says nothing" silences now include the suggestion menu, and the "Tab" paragraph now
states the pane's title, the insert-at-the-caret rule and the two ways it closes.

Depends on item 5.

**What:**

- `autocomplete.go`: new `acKind` `acSuggest` with title `"suggested skills"`
  (`autocompleteTitle`). `openSuggestMenu()` builds an `autocompleteState{active: true, kind:
  acSuggest, tokenStart: caret, tokenEnd: caret}` whose items are the band's `skillHints` mapped
  through the same row builder `skillSuggestions` uses (extract that builder into
  `skillRow(sk skills.Skill, rank int, source string) acItem` so both menus share one row
  shape — one reason to change); the `Skill` for each hint is looked up via `Get(id)` (a hint
  whose skill vanished between recompute and Tab is skipped). Selection starts at row 0.
- `model.go` `handleKey`: a `"tab"` case reached only when no overlay is active and
  `len(m.skillHints) > 0` (and the knob is on): open the suggest menu, `m.layout()`. Every other
  Tab keeps its current meaning (the overlay's accept when open; otherwise whatever the editor
  does today — do not change that).
- Accept path: `acceptAutocomplete` on an `acSuggest` row goes through `insertSkillToken` with the
  empty completion region at the caret, so the token is INSERTED (`/id `) rather than replacing a
  typed partial. `autocompleteExactMatch` must return false for `acSuggest` (there is no typed
  token to be exact against), so Enter accepts the highlighted row.
- Lifetime: `recomputeAutocomplete` on the next edit re-derives the overlay from the box as
  today, which closes the suggest menu (no `/` token at the caret) — that is the intended
  "typing dismisses" behaviour; ↑/↓ are swallowed by `autocompleteKey` before any recompute, so
  arrowing keeps it open; Esc closes via the existing `listCloses` path. While the suggest menu
  is open the band row is not painted (item 5's overlay rule already covers it, since the state
  is active).
- The band's legend `tab to pick` is the only hint text; `autocompleteHint` is reused unchanged
  as the popup's legend.

**Files:** `internal/tui/autocomplete.go`, `internal/tui/autocomplete_test.go`,
`internal/tui/model.go`, `internal/tui/keyclaim_test.go`

**Tests:** Tab with hints and no overlay opens `acSuggest` with the hint rows in order and row 0
selected; Tab with no hints leaves the model unchanged; Tab while a `/` overlay is open still
accepts (existing test stays green); Enter on the suggest menu inserts `/id ` at the caret and
leaves the rest of the draft intact; Esc closes; typing a character closes; a hint whose skill is
no longer in the catalog is skipped. Key-claim test names `tab` at idle with hints.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run
'Autocomplete|Suggest|KeyClaim|Tab' -count=1`

**Commit:** `feat(tui): Tab opens the / menu filtered to the suggested skills`

---

## 7. Spent-at-send dedup — ✅ DONE (2026-08-27)

NOTES (2026-08-27): the marking is factored into one method, `spendSkillHints` (suggestband.go),
rather than open-coded at each of the two send sites the item names. Both sites do the identical two
things (mark every id, then clear the row), and the rule's whole reason for existing — advice is made
once, at the send — belongs stated in the band's own file next to the set it retires.

NOTES (2026-08-27): layout.md's band section already carried the rule (item 5 wrote it) so this item
only sharpened it: the sentence now names WHICH acts spend (a send or a staged row) and which do not
(a refusal, a `/command` line), which is the part item 7 actually settles.

Depends on item 5.

**What:**

- `model.go` `submit()`: on every path that actually SENDS (the plain send and the
  "⏎ sends the held queue" path — not a refusal, not a `/command` line), mark each id in
  `m.skillHints` spent (`m.spentSkills[id] = true`, allocating the map on first use) BEFORE
  `promptEditor.reset()` clears the box; then clear `m.skillHints`.
- `interject.go` `stageInterjection()`: the same marking — a staged message is a send from the
  user's side.
- `commandrun.go`, the `/clear` | `/new` path: after `ClearContext` succeeds, `m.spentSkills =
  nil` (before `m.transcript.reset()`); a refused clear leaves it untouched.
- The exclude closure of item 5 already reads `spentSkills`; no other consumer.
- Document the rule in one sentence in `suggestband.go`'s file comment and in layout.md's band
  section ("a skill shown when a message is sent is not suggested again this session").

**Files:** `internal/tui/model.go`, `internal/tui/interject.go`, `internal/tui/commandrun.go`,
`internal/tui/suggestband.go`, `internal/tui/suggestband_test.go`, `layout.md`

**Tests:** with a fake catalog that always returns `grill-me`: after a submit with the band
showing, a new draft that would match again yields no `grill-me` hint (exclude closure sees it);
a submit with the band empty spends nothing; a staged interjection spends; `/clear` resets so the
hint returns; a refused `/command` line (unknown verb) spends nothing.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'Spent|Suggest|Clear|Interject'
-count=1`

**Commit:** `feat(tui): a suggested skill is spent for the session once a message is sent`

---

## 8. Manual pages and the changelog entry — ✅ DONE (2026-08-27)

NOTES (2026-08-27): README.md is untouched. The item makes that edit conditional ("only if the README has a skills paragraph; otherwise skip and note it") and the README has none — skills appear only in the documentation table's `Commands` row and in the Status paragraph's feature list, neither of which is a paragraph a sentence about suggestion could join without inventing a section the item did not ask for.

NOTES (2026-08-27): the `## Suggested skills` subsection sits after the intro prose and the command table, immediately before `## {{SKILL_DIR}} in skill bodies`, rather than directly under the `/skills` table row — the paragraphs following the table (unknown `/word`, `@` references, the key legend) belong to the page's opening material, and a heading wedged between the table and them would have orphaned them under it.

NOTES (2026-08-27): commands.md's `/skills` table row gained ", any declared `triggers:`," although the item's What names only the new subsection and the authoring example. The row's description ("id, name, summary and where each came from") is the one sentence in the manual that item 2's landed `/skills` triggers line makes wrong, and this item's own CHANGELOG text announces that change to users — leaving the row as it stood would have contradicted the section added two screens below it.

NOTES (2026-08-27): in configuration.md the new `## Skill suggestions — ui.skill-suggestions:` heading is placed at the end of the skills prose, so the unrelated paragraphs that already trailed the `use-project-skills:` section without a heading of their own (Compaction, `delegate-max-steps:`, `context-window:`, effort, `ui.stall-after`, `editor:`) now trail the new one instead. No content was moved or reworded; the pre-existing drift is unchanged in kind, only in which heading it hangs under.

Depends on items 5, 6 and 7.

**What:** docs only.

- `docs/manual/commands.md`: after the `/skills` row's paragraph, add a short subsection
  `## Suggested skills` — the band (what it shows, when it appears, that it never sends anything
  to the model), Tab to open the menu on them, the spent-once rule and `/clear`, the
  `ui.skill-suggestions` knob (link to configuration.md). Add `triggers:` to the frontmatter
  description in the `{{SKILL_DIR}}` / skill-authoring subsection with a 3-line example.
- `docs/manual/configuration.md`: in the "Skills a repository ships" section (~line 245) add a
  `## Skill suggestions — ui.skill-suggestions:` subsection: default, live apply, what the matcher
  reads (id, name, summary, `triggers:` — never the body), and the `triggers:` authoring advice
  (front-load the words a user would type; phrases match whole words).
- README.md: one sentence in the skills paragraph ("apogee suggests fitting skills as you type;
  nothing about your library reaches the model until you invoke one") — only if the README has a
  skills paragraph; otherwise skip and note it.
- CHANGELOG entry text (in the sidecar, per the skill's convention): one `### Added` bullet for
  the band + Tab + knob + `triggers:`, one `### Changed` bullet for `/skills` showing triggers,
  citing ADR 0061.

**Files:** `docs/manual/commands.md`, `docs/manual/configuration.md`, `README.md`

**Tests:** none (docs). `grep -n "ui.skill-suggestions" docs/manual/commands.md
docs/manual/configuration.md` finds both.

**Acceptance:** `grep -q "Suggested skills" docs/manual/commands.md && grep -q
"ui.skill-suggestions" docs/manual/configuration.md`

**Commit:** `docs(manual): suggested skills band, Tab, ui.skill-suggestions, triggers:`

---

**Suggested version bump:** minor-line micro bump (`0.17.x` → next micro, per the repo's
per-feature policy) after closeout — a user-visible feature (band, Tab, knob, `triggers:`) and a
new ADR. Not performed by this plan; the owner decides.

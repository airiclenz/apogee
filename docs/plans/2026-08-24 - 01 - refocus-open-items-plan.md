# Refocus open items — plan

**Goal:** close the four open defects in `ISSUES.md` and the two documentation claims that are
actually false in tracked files, as surfaced by the refocus run of 2026-08-24
(`docs/skill-runs/refocus/2026-08-24/briefing.md`).

**Date:** 2026-08-24
**Status:** ready
**sized for:** ~200k-context host

## Authoritative sources

- `ISSUES.md` § "Open defects" — the four defect statements, each with its own `file:line` evidence.
  **If this plan disagrees with `ISSUES.md`, `ISSUES.md` wins.**
- `docs/adr/0057-the-tool-roster-is-a-third-model-profile-axis-resolved-axis-wise.md` — decisions
  4 (the specificity ladder), 5 (axis-wise resolution), 7 (the roster rides Rebind), 8 (the
  announcement), and the "Bounds" section (an injected `Config.Tools` is the host's authority, so
  the HOST folds the deltas in where it builds).
- `docs/adr/0050-thinking-effort-is-a-profile-axis-with-one-canonical-wire-mapping.md` — effort is
  orthogonal to style and its zero value is the wire anchor.
- `docs/adr/0044-model-profiles-are-per-model-and-mostly-shipped.md` — the user ▸ shipped ▸ zero
  tiering and the announce-what-changes-behaviour rule.
- `docs/adr/0037-*` binding F — which tool changes go through `SwapTools` rather than a write on a
  tool already in the set.
- `CONTEXT.md` § "Model profile" and § "Thinking effort" — the domain language these items must
  keep true.

## Ratified design calls

Decided by the repo owner (Airic Lenz) on 2026-08-24, in answer to the questions this plan raised
before it was saved:

1. **The thinking axis splits into two sub-axes — channel style and effort — resolved
   independently** (item 3). Rejected: keeping the axis atomic and merely announcing the drop;
   documenting the trap without fixing it.
2. **The Windows Auto toolchain-cache gap is OUT OF SCOPE** for this plan. It needs its own design
   session (box-local `%TEMP%` via `ScopeEnv` versus labelling the user's cache trees) and a Windows
   host to verify on; its `ISSUES.md` entry stays exactly as written.
3. **`AGENTS.md` drops the ADR count rather than updating it** (item 6) — a number that re-breaks
   on every ADR is a recurring defect, and the directory listing is the count.
4. **A `SwapTools` refusal during a model switch is a notice, not a failed rebind** (item 2). The
   binding has already committed by then; failing the rebind would report a switch that did happen
   as one that did not.

## Standing requirements

- **skills:** `coding-standards`
- Every authorized deviation from an item's text lands as a dated `NOTES (YYYY-MM-DD):` line under
  that item in this file.
- Comment prose in this repo is load-bearing: when an item changes behaviour a neighbouring comment
  describes, the comment moves with it. Do not leave a doc comment asserting the old contract.
- No version identifier is touched by any item — see "Suggested version bump" at the end.

## Out of scope

- The Windows Auto box-local `%TEMP%` / toolchain-cache work (design call 2 above).
- Every entry under `ISSUES.md` § "Parked / deferred work" other than the four open defects — they
  are deferred by decision, not by oversight.
- The seven "documentation fixes" listed in `docs/skill-runs/refocus/2026-08-24/corrections.md`.
  All seven target `docs/skill-runs/refocus/2026-08-24/docs.md`, which is the refocus run's own
  read-back of the docs and is **gitignored** (`.gitignore:23` — `/docs/skill-runs/`). Six of the
  seven claims do not appear in any tracked file at all: `docs/manual/building.md:22-23` already
  describes `make check` and `make dist` correctly, `AGENTS.md:26` already names
  `APOGEE_LIVE_ENDPOINT`, and no tracked file mentions a `--prompt` flag, `-ldflags -s -w`, or
  `internal/bench/`. Only the ADR count is a real stale claim in a tracked file; item 6 fixes it.
- The three `TODO` comments named in `docs/skill-runs/refocus/2026-08-24/planned.md`.
- Phase-5 verification leftovers (need Windows/macOS hosts).

---

## 1. Hand the roster ladder to the tool set the composition root builds

**What:** `registryWithMCP` builds the session's tool registry but passes only the `Disabled:` rung
of the roster ladder into `tools.HostTools` (`cmd/apogee/wire_tools.go:191` and the literal that
follows it). The `Enabled:` and `ProfileRoster:` rungs — the global `tools.enabled:` key and the
bound model's `tools:` profile axis — are never handed over, so ADR 0057's specificity ladder is
inert in every live TUI session. Separately, `domain.Config.EnabledTools` is never populated by any
of the four Config assemblies in the composition root, even though the config layer already parses
the key into `config.Options.ToolsEnabled` (`internal/config/config.go:506-508`), so the global
enable rung is dead on every Driver.

Make both rungs live at construction:

- In `registryWithMCP` (`cmd/apogee/wire_tools.go:191`), add two fields to the `tools.HostTools`
  literal beside the existing `Disabled: cfg.DisabledTools`:
  - `Enabled: cfg.EnabledTools` — the global enable rung.
  - `ProfileRoster: cfg.Profile.Tools` — the bound model's roster axis, the most specific rung.

  Give each a comment in the voice of the ones already in that literal: this hand-assembly must not
  be the one path on which a configured roster quietly stops applying, or connecting an MCP server
  would silently re-broaden the menu in every session without MCP.
- Add `EnabledTools: w.opts.ToolsEnabled` to the `apogee.Config` literal in
  `cmd/apogee/wire_boot.go` (beside `DisabledTools: w.opts.ToolsDisabled` at `:178`),
  `EnabledTools: opts.ToolsEnabled` in `cmd/apogee/headless.go` (beside `:390`), and
  `EnabledTools: w.opts.ToolsEnabled` in `cmd/apogee/daemonfire.go` (beside `:297`). One
  configuration, four Drivers — the same reason the existing comments give for `DisabledTools`.

`w.cfg.Profile` is already resolved before `wireSession` runs (`cmd/apogee/wire_boot.go:130`
resolves it, `:201` puts it on the Config; `wire_live.go:60` builds the registry afterwards), so no
ordering change is needed.

**Binding scope note:** this item makes the roster correct **at construction only**. Making a
mid-session model switch re-apply it is item 2's job and must not be attempted here — the two
seams commit separately and each gets its own test.

**Files:** `cmd/apogee/wire_tools.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/headless.go`,
`cmd/apogee/daemonfire.go`, `cmd/apogee/wire_test.go`

**Tests:** in `cmd/apogee/wire_test.go`, alongside the existing `registryWithMCP` tests
(`:886`, `:904`, `:935`, `:967`):

- A profile roster whose `Enabled` list names a globally disabled tool puts that tool back on the
  set `registryWithMCP` returns (the ratifying use case of ADR 0057 decision 4).
- A profile roster whose `Disabled` list names a tool the global config allows takes it off.
- `cfg.EnabledTools` alone lifts a tool the global `DisabledTools` names — asserting the fail-closed
  rule of ADR 0057 decision 4 holds where the ladder puts it: within ONE scope disabled wins, so
  use a tool named ONLY in `EnabledTools` to prove the rung is read at all, and a separate case for
  the both-lists conflict asserting disabled wins.
- A table case per Driver asserting `EnabledTools` reaches the assembled `apogee.Config` — extend
  whatever existing Config-assembly assertion covers `DisabledTools` rather than writing four new
  ones.

**Acceptance:**

```bash
go build ./... && go test ./cmd/apogee/ -count=1
```

**Commit:** `fix(wire): hand the enabled and profile roster rungs to the built tool set`

---

## 2. Re-compose the tool set when the bound model's roster changes

**Depends on item 1.**

**What:** ADR 0057 decision 7 makes the roster a per-model binding that rides `Rebind` — "`/model`
to the big model and its enabled tools appear; switch back and they are gone". The engine's own
re-compose seam declines under an injected `Config.Tools` by design (`internal/agent/setprofile.go`
`applyRoster`, gated on `ownsToolSet`, which `composesDefaultRoster` leaves false whenever the host
injects a set — `internal/agent/construct.go:430-432`). ADR 0057's Bounds section states the
consequence: the host folds its deltas in where it builds. The composition root already owns
exactly the seam for that — `liveTools`, whose `rebuildWith` builds a fresh set and installs it
through `Agent.SwapTools` (`cmd/apogee/wire_tools.go:143-159`). It is simply never driven by a
profile change today, so a switch announces its deltas (`rosterDeltaNotice`,
`cmd/apogee/wire_settings.go:1117`) and none of them take effect.

Drive it from both doors onto the profile:

- Add a `roster domain.ToolRosterDelta` field to `toolSetSpec` (`cmd/apogee/wire_tools.go:56`),
  documented the way `allowHosts`/`denyHosts` are: the roster belongs to the SET's identity because
  which tools exist is what the set IS, and no tool exposes a setter for it. Seed it from
  `w.cfg.Profile.Tools` in the `built := toolSetSpec{...}` literal at `cmd/apogee/wire_live.go:66`,
  and apply it in that literal's rebuild closure (`wire_live.go:73-81`) by setting
  `host.Profile.Tools = spec.roster` before the `registryWithMCP` call, beside the existing
  `host.DisabledTools = spec.disabled`.
- Add `setProfileRoster(roster domain.ToolRosterDelta, engine settingsEngine) error` to `liveTools`,
  written exactly like `setDisabled` (`cmd/apogee/wire_tools.go:106-110`): take `t.built()`, set
  `spec.roster`, call `t.rebuildWith`. Its doc comment says why this is the swap door rather than a
  write on a tool: the roster decides which tools EXIST, which is the set's identity (ADR 0037
  binding F).
- **Door one — a model switch.** In `rootWiring.rebind` (`cmd/apogee/wire_verbs.go`), after
  `w.engine.Rebind(spec)` succeeds and before `w.host.SetModel(spec.Model)`, call
  `w.toolSet.setProfileRoster(spec.Profile.Tools, w.engine)`. Per ratified design call 4, a
  non-nil error is **appended to `notices` as one line and the rebind still succeeds** — the
  binding has already committed, `SwapTools` can only refuse mid-Exchange, and the ADR 0024
  boundary the TUI rebinds at rules that out. Word the notice so a human reading the transcript
  knows the tools did not move: name the failure and say the roster applies at the next switch.
- **Door two — a `model-profiles:` edit under a stable model.** In
  `settingsApplier.reloadModelProfiles` (`cmd/apogee/wire_settings.go:997-1011`), replace the
  trailing `return a.engine.SetProfile(profile)` with: commit `SetProfile` first (it is
  validate-then-commit, so a profile this build cannot parse must not move the tool set either),
  and on success `return a.tools.setProfileRoster(profile.Tools, a.engine)`. Here the error IS
  returned — this path already reports failures onto the settings row, which is the surface the
  human is looking at.

**Binding scope note:** do not change `composesDefaultRoster`, `applyRoster`, `ownsToolSet`, or
anything else under `internal/agent/`. ADR 0057's Bounds section puts this work in the composition
root by decision; moving the seam into the engine is a different ADR.

**Files:** `cmd/apogee/wire_tools.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_verbs.go`,
`cmd/apogee/wire_settings.go`, `cmd/apogee/wire_test.go`

**Tests:** in `cmd/apogee/wire_test.go`, reusing the `applySettingSpy` `SwapTools` recorder at
`:2900` and the `rebind` harness at `:133`/`:4462`:

- A model switch onto a profile whose `tools.enabled:` names a tool absent from the current set
  installs a new registry through `SwapTools` that HAS that tool.
- A model switch back onto a model with no roster axis installs a set WITHOUT it.
- A `SwapTools` error during a switch leaves `RebindResult.Model` and `ContextWindow` as the switch
  resolved them and adds one notice line — the rebind is not reported as failed.
- `reloadModelProfiles` with a roster-bearing entry for the bound model swaps the set; a
  `SetProfile` failure swaps nothing.

**Acceptance:**

```bash
go build ./... && go test ./cmd/apogee/ -count=1
```

**Commit:** `fix(wire): re-compose the tool set when the bound model's roster changes`

---

## 3. Resolve the thinking axis as two sub-axes: channel style and effort

**What:** `Entry.spellsThinking` is true for any non-zero `ThinkingProfile`
(`internal/profiles/entry.go:45`), and `Resolve` then takes that entry's WHOLE thinking half
(`internal/profiles/match.go:92`). A user entry spelling only `effort:` over the shipped gpt-oss
profile therefore wins the axis outright and drops harmony parsing with it — `Style` resolves to
`""` and nothing is announced. That is the same whole-replacement trap ADR 0057 decision 5 closed
one level up, reappearing inside the thinking axis. Per ratified design call 1, close it the same
way.

Split the axis in two, each resolved independently through user ▸ shipped ▸ zero:

- **The channel-style sub-axis** is `{Style, Start, End}` — `Start`/`End` are the delimiter tokens
  `ThinkingDelimited` reads and are meaningless without a `Style`, so they travel with it and never
  resolve on their own. It is self-describing exactly as the tool-call axis is: `""` is the
  unwritten style and `none` is the spelled zero (`internal/domain/config.go:408-437`).
- **The effort sub-axis** is `Effort` alone. Also self-describing: `""` is the ABSENCE of the
  setting and the wire anchor (ADR 0050), and any of the four words is a spelled value. There is no
  spelled zero to distinguish, so no config-layer `spells…` field is needed — unlike the roster
  axis, whose `SpellsTools` exists precisely because it has none.

Concretely, in `internal/profiles/entry.go` replace `spellsThinking` with `spellsThinkingStyle`
(`e.Profile.Thinking.Style != ""`) and `spellsThinkingEffort` (`e.Profile.Thinking.Effort != ""`),
carrying the same self-describing-predicate reasoning in their comments plus the new sentence: an
entry that spells only `effort:` says nothing about how the reasoning ARRIVES, so the layer below
keeps that word. In `internal/profiles/match.go`, compose the resolved `Thinking` from two supplier
calls instead of one:

```go
style := supplier(Entry.spellsThinkingStyle).Profile.Thinking
Thinking: domain.ThinkingProfile{
    Style:  style.Style,
    Start:  style.Start,
    End:    style.End,
    Effort: supplier(Entry.spellsThinkingEffort).Profile.Thinking.Effort,
},
```

`shippedSpoke` bookkeeping is unchanged — it is set inside `supplier`, so a shipped tier that
supplies either sub-axis still earns `SourceShipped` and its notice, which is the correct outcome:
an `effort:`-only user entry over the shipped gpt-oss row now resolves to harmony-from-shipped plus
effort-from-user, and the human is told the shape came from the table.

Then update the prose that asserts the old contract:

- `internal/profiles/doc.go:4-8` and `internal/profiles/match.go:53-66` — both narrate "each of the
  three axes"; say that the thinking axis itself resolves as two sub-axes and why (the spelled-zero
  reasoning above), naming ADR 0058.
- `CONTEXT.md` § "Model profile" (the three-orthogonal-axes paragraph around `:246`) and § "Thinking
  effort" (around `:328`) — the thinking axis resolves in two halves, so an entry that dials effort
  keeps the layer's channel style. Amend in place; do not restructure either entry.
- `docs/adr/0057-…md` decision 5 — add a dated parenthetical in the house style used at
  `docs/adr/0044-…md:74` and `:123` (`*(**Amended 2026-08-24 by [ADR 0058](…):** …)*`) noting that
  the thinking axis it names as one axis itself resolves as two sub-axes since 0058. Do not rewrite
  the decision — ADRs are historical records.

**Write `docs/adr/0058-the-thinking-axis-resolves-as-two-sub-axes-style-and-effort.md`** with
`Status: accepted`, following the structure of ADR 0057 (Context / Decision / Bounds / Considered
and rejected / Consequences). It must record: the trap (an `effort:`-only entry is a taught idiom —
`README.md:150` teaches "reasoning effort set per model" — and it silently wiped harmony); the
decision (two sub-axes, style carrying `Start`/`End`, each self-describing from the domain value);
the bound (this is resolution only — `ThinkingProfile` stays one struct on `Config`, the engine
still sees one resolved profile and no layering); the rejected alternatives (keep atomic and
announce the drop; document the idiom without fixing it — both were weighed and declined on
2026-08-24); and the consequence that ADR 0057 decision 5's "three axes" reads as three axes, one
of which resolves in two halves.

**Files:** `internal/profiles/entry.go`, `internal/profiles/match.go`, `internal/profiles/doc.go`,
`internal/profiles/match_test.go`, `CONTEXT.md`,
`docs/adr/0057-the-tool-roster-is-a-third-model-profile-axis-resolved-axis-wise.md`,
`docs/adr/0058-the-thinking-axis-resolves-as-two-sub-axes-style-and-effort.md`

**Tests:** in `internal/profiles/match_test.go`, beside the existing axis-wise resolution table:

- The ratifying case — a user entry spelling ONLY `Effort` over a shipped entry carrying
  `ThinkingHarmony` resolves to `Style: ThinkingHarmony` + the user's `Effort`, with
  `Source == SourceShipped`.
- The mirror — a user entry spelling only a `Style` over a shipped entry carrying only an `Effort`
  keeps the shipped effort.
- The spelled zero still overrides: a user `Style: ThinkingNone` over shipped harmony resolves to
  `ThinkingNone`, and does NOT clear a shipped `Effort`.
- `Start`/`End` travel with `Style` and never alone: a user entry spelling `Style` +
  `Start`/`End` replaces all three; an entry spelling only `Effort` leaves all three at the
  shipped values.
- The pre-existing whole-axis cases keep passing unchanged (an entry spelling both halves is
  still that entry's word on both).

**Acceptance:**

```bash
go build ./... && go test ./internal/profiles/ -count=1
```

**Commit:** `fix(profiles): resolve thinking style and effort as independent sub-axes`

---

## 4. Correct the profile doc surfaces that still teach pre-ADR-0057 resolution

**Depends on item 3.**

**What:** three tracked surfaces still describe whole-profile replacement and a two-axis Model
profile. Each is a comment or config narration; no behaviour changes in this item.

- `internal/config/config.go:1024-1030` — the `ModelProfiles` field doc opens correctly ("its
  tool-call format and inline thinking-channel style … and, since ADR 0057, the tool roster") but
  still ends "A matching entry replaces the WHOLE profile, every axis, and outranks every shipped
  entry." Replace that closing sentence with the axis-wise rule: a matching entry outranks every
  shipped entry **on each axis it spells**, and an axis it leaves out defers to the shipped table
  (ADR 0057 decision 5) — noting, per item 3, that the thinking axis defers in two halves, so an
  entry dialling only `effort:` keeps the shipped channel style (ADR 0058). Keep the existing
  retired-`model-profile:`-block paragraph that follows.
- `internal/domain/config.go:230-232` — `Config.Profile`'s doc says the profile describes "its
  tool-call format and inline thinking-channel style". Name the third axis: the tool roster it is
  equipped with (ADR 0057 decision 1), matching the widened glossary term in `CONTEXT.md`.
- `internal/config/defaults/config.yaml:733-738` — the "Model profiles:" header still frames the
  concept as wire shape alone ("how a model speaks the wire — the shape its tool calls arrive in,
  and the inline thinking channel apogee strips out"). Widen it to how apogee **equips and speaks
  to** a model — the third axis, the tool roster, is already documented further down the same block
  at `:785` and `:820`, so the header is the only thing out of step. In the same block, add the one
  sentence the thinking axis now needs: `thinking:` resolves its style and its effort
  independently, so an entry spelling only `effort:` keeps whatever channel style the shipped table
  carries for that model.

**Binding scope note:** comment and YAML-comment prose only. Do not change a struct field, a tag, a
default value, or any live YAML key in this item.

**Files:** `internal/config/config.go`, `internal/domain/config.go`,
`internal/config/defaults/config.yaml`

**Tests:** none new — this item changes no behaviour. The existing embedded-default parse and
round-trip tests in `internal/config/` are the guard that the YAML comment edits did not break the
file, and they must keep passing.

**Acceptance:**

```bash
go build ./... && go test ./internal/config/... ./internal/domain/... -count=1
grep -n 'replaces the WHOLE profile' internal/config/config.go   # must print nothing
```

**Commit:** `docs(config): correct the profile surfaces that still teach whole-entry replacement`

---

## 5. Distinguish a zero-page PDF from a scanned one

**What:** `extractPDFText` decides the no-text case on `hasText` alone
(`internal/tools/pdf_text.go:97`). That flag is false both for a document whose pages carry only
images and for one whose page tree yields nothing to walk at all — `pages = reader.NumPage()` comes
back `0` (`:80`) and the loop body never runs. A structurally broken file that `pdf.NewReader`
accepts therefore returns `pdfNoTextMessage` — "likely scanned images; OCR is not supported … ask
the user for a text version" (`:26`) — which tells the model the document is a scan when in fact
nothing was read from it.

Add a `pages <= 0` guard immediately after `pages = reader.NumPage()` and before the walk, routing
that case to `pdfUnreadableFormat` ("could not extract text from this PDF: %v — the file may be
corrupted or encrypted; ask the user for a text version", `:31`) instead. `pdfUnreadableFormat`
takes a `%v` cause and its comment says the cause is quoted verbatim because "corrupted or
encrypted" is a guess; supply the literal cause `"the document has no pages"` so the message stays
evidence plus guess rather than guess alone. Give the guard a short comment in the file's voice:
zero pages is the reader accepting bytes it could not walk, which is a document-level failure and
not the scan `pdfNoTextMessage` describes.

**Binding scope note:** do not touch the `hasText` logic, the per-page failure path
(`pdfPageFailedFormat`), the recover, or any message constant's wording. The single change is one
guard and its comment.

**Files:** `internal/tools/pdf_text.go`, `internal/tools/pdf_text_test.go`

**Tests:** in `internal/tools/pdf_text_test.go`:

- A PDF the reader accepts whose page count is zero returns the `pdfUnreadableFormat` message
  (assert on the "may be corrupted or encrypted" substring) and NOT the "likely scanned images"
  one. Build the fixture the way the existing tests in this file build theirs; if none of them
  constructs a reader-accepted-but-empty document, the cheapest honest fixture is a minimal PDF
  whose catalog has an empty `/Pages` kids array — verify it actually reaches the guard rather than
  `pdf.NewReader`'s error path, and if it cannot, say so in the sidecar rather than asserting on a
  path the test does not exercise.
- The genuine image-only case still returns `pdfNoTextMessage` — extend or reuse the existing
  no-text case so the two are pinned apart.

**Acceptance:**

```bash
go build ./... && go test ./internal/tools/ -run 'PDF|Pdf' -count=1
```

**Commit:** `fix(tools): report a zero-page PDF as unreadable rather than as a scan`

---

## 6. Correct the two false claims in the top-level docs

**What:** two claims in tracked top-level docs are false as of this commit.

- `AGENTS.md:9` reads "`docs/adr/` — 54 architectural decision records." There are 57
  (`ls docs/adr/*.md | wc -l`). Per ratified design call 3, **drop the count** rather than update
  it: "`docs/adr/` — architectural decision records." The rest of the line ("Settled questions live
  here; check for an ADR before re-opening one.") stays exactly as written.
- `README.md:197` § Status opens "**`v0.15.x` on `main` — pre-production.**" while `VERSION` reads
  `v0.16.5`. Change the version band to `v0.16.x`. Change nothing else in that section — the
  SemVer sentence, the six-target claim and the Homebrew claim are all still accurate, and the
  `VERSION` file itself is NOT touched by this item or any other (see "Suggested version bump").

**Files:** `AGENTS.md`, `README.md`

**Tests:** none — prose only.

**Acceptance:**

```bash
grep -n '54 architectural' AGENTS.md   # must print nothing
grep -n 'v0.15' README.md              # must print nothing
grep -n 'v0.16.x' README.md            # must print the Status line
```

**Commit:** `docs: drop the stale ADR count and match the README status to VERSION`

---

## 7. Retire the closed defects from ISSUES.md

**Depends on items 1, 2, 3, 4 and 5.**

**What:** `ISSUES.md` holds OPEN work only — its own conventions section says a resolved item is
REMOVED from it and recorded in `CHANGELOG.md`, with no "done" narration left behind. Items 1–5
close all four entries under § "Open defects", so remove all four in full, including the `---`
separators between them, leaving the § "Open defects" heading immediately followed by
§ "Parked / deferred work":

- "The composition root never hands the roster to the tool set it builds" (items 1 and 2).
- "Two doc surfaces still teach the pre-ADR-0057 profile" (item 4; item 3 covered its
  `internal/domain/config.go:230` half's sibling in `CONTEXT.md`).
- "An `effort:`-only profile entry silently drops the shipped thinking style" (item 3).
- "A PDF that parses to zero pages reads to the model as a scan" (item 5).

Leave § "Parked / deferred work" untouched — every entry in it, the Windows Auto toolchain-cache
entry included, is deferred by decision and stays open.

**Binding scope note:** this item is the ONE owner of `ISSUES.md` in this plan. No earlier item
edits it, so nothing here is a duplicate removal; if an entry is already gone, that is a deviation
an earlier item must have logged, and this item records it as a dated `NOTES` line rather than
silently proceeding.

**Files:** `ISSUES.md`

**Tests:** none — the register is prose.

**Acceptance:**

```bash
grep -n 'never hands the roster' ISSUES.md          # must print nothing
grep -n 'pre-ADR-0057 profile' ISSUES.md            # must print nothing
grep -n 'silently drops the shipped thinking' ISSUES.md   # must print nothing
grep -n 'parses to zero pages' ISSUES.md            # must print nothing
grep -n 'Windows Auto: box-local' ISSUES.md         # must STILL print
```

**Commit:** `docs(issues): retire the four defects this plan closed`

---

## Suggested version bump

No item in this plan changes `VERSION`, and none should. When the plan lands, a **patch** bump
(`v0.16.5` → `v0.16.6`) is the level the work argues for: four defect fixes and two doc
corrections, no new surface and no removed one. Item 2 is the borderline case — it makes a
per-model tool roster take effect on a live model switch for the first time, which a user will
experience as new behaviour even though ADR 0057 already ratified it and the announcement already
shipped. If that reads as a feature rather than a fix, **minor** (`v0.16.5` → `v0.17.0`) is the
defensible call. Whether and how to bump is the user's decision, after the run.

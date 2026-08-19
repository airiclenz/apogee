# TUI architecture review — 2026-08-19

Deepening review of `internal/tui` (improve-codebase-architecture skill + coding-standards
skill), run at commit `030ab021` with the ADR 0052 / split-diff docs uncommitted in the
working tree. Method: three parallel read-only explorers — coordinator & event/paint flow,
transcript & block rendering, interactive surfaces & input — consolidated into the twelve
candidates below. A visual rendering sits beside this file
(`2026-08-19 - 00 - tui-architecture-review.html`, CDN-dependent); **this file is the
canonical record**. Line numbers are as of that commit and will drift; the function names
and duplication counts are the durable evidence.

**Status: no candidate started. The owner has not yet picked one.** A future session can
pick any candidate from this file alone; each card carries its evidence, its deletion-test
verdict, its ADR constraints, and its sequencing notes.

## Vocabulary

Glossary of the reviewing skill, used exactly throughout: **module** (anything with an
interface and an implementation), **deep** (small interface hiding much behaviour) vs
**shallow** (interface nearly as complex as the implementation), **seam** (where an
interface lives), **deletion test** (delete the module: complexity vanishes → it was a
pass-through; complexity reappears across callers → it earned its keep), **locality**
(change and bugs concentrate in one place), **leverage** (what callers gain from depth).

## Verdict

The package's deep modules are genuinely deep and must not be "improved": `popup.go`, the
tool registry, the event fold, the width authority all pass the deletion test decisively
(see §Verified healthy). The friction concentrates in three shapes:

1. **State machinery written N times** where one module should exist (list/selection state,
   the report pane, the rendezvous bodies).
2. **Facts re-derived** that a composer already computed (pane geometry, stat strings
   parsed back into the integers they were formatted from).
3. **Clusters still inside oversized files** after ADR 0043's split stopped early
   (`model.go` 4,031 lines / 14 clusters, `toolpresent.go` 2,514 / 4 concerns,
   `settings.go` 2,481 / 6 surfaces).

Nothing proposed loses functionality. Most candidates delete or relocate code.

Package scale, measured: 71 non-test files, ~33k non-test lines (≈55% comment — raw line
counts overstate size; the structural counts below do not). `Model`: **63 fields**, **355
methods** spread over 20+ files (largest holders: settings.go 67, model.go 66, mouse.go
35). `m.transcript` touched in 22 non-test files, `m.opts` in 19, `m.th` in 15.

## Binding constraints (do not violate; re-read before starting any candidate)

- **ADR 0011** — thin renderer over a worker-goroutine engine. `Model` is a **value type**
  copied on every `Update`: no mutex, no self-pointer, no no-copy type held by value
  (`TestModelNoBuilderByValue` guards). Engine calls from the Update goroutine only in the
  three documented classes (idle-only under the state machine / `SetMode`-class mutex /
  between-Steps by the driving goroutine).
- **ADR 0030** — one width authority, `theme.measure`. `lipgloss.Width`, `ansi.StringWidth`,
  `runewidth` are banned outside declared widget mirrors (inputaccent.go, mouse.go's
  `cellToRuneOffset`, chromelayout.go's `inputContentRows`). Verified holding — keep it so.
- **ADR 0043** — `internal/tui` stays **flat** (sub-packages explicitly rejected);
  coordinator concern clusters get own files in the same package; `doc.go` must name every
  non-test file (test-enforced, `docmap_test.go`). Every new file below costs one doc.go
  line in the same commit.
- **ADR 0035/0037/0041** — the /settings *display projection* (`settingsrows.go`,
  `settingsedit.go`) lives in `cmd/apogee` deliberately. Candidate 12 does not move it back.
- **ADR 0052** + `docs/plans/2026-08-19 - 03 - split-diff-display-plan.md` — in flight,
  touches `toolpresent.go`, `toolbranch.go`, `toolblock.go`. Candidates 4 and 5 are
  sequenced against it (see cards).

---

## Candidate 1 — One list surface behind every popup pane — **Strong**

**Files:** picker.go, sessions.go, settings.go, autocomplete.go, approval.go, model.go
(ask pane), usage.go, inspector.go; painter stays popup.go.

**Friction.** Eight popup-painted surfaces (`framePane`, model.go:3578-3588) and, counting
sub-modes, ~21 list variants (picker 7 `pickerKind`s, settings 6 `settingsKind`s, sessions
3 modes, autocomplete 2, ask single/multi). `popup.go` deepened the *painting* of all of
them; nothing deepened the *state*. Measured duplication:

- `clampSelection` written three times, verbatim-equivalent: picker.go:781,
  sessions.go:208, settings.go:1843.
- The wrap-around arrow idiom (`sel = (sel-1+n)%n` on up / mirror on down) written
  **eight** times: picker.go:619/624, sessions.go:249/254, settings.go:426/431, :598/601,
  :633/636, autocomplete.go:637/641 — plus two non-wrapping variants (approval.go:66/69,
  model.go:1278/1283) and two scroll-only variants (usage.go:142/144, inspector.go via
  `usageScrollStep`). Four different answers to "what does ↓ do at the bottom", none
  documented as a decision.
- The type-to-filter block (printable `msg.Text` append, backspace **by rune**, re-clamp)
  written twice byte-for-byte, picker.go:632-651 and sessions.go:288-305, including the
  same five-line comment.
- The budget→render boilerplate (`popupFloor` claim → `popupBudget` → `if !seated return
  ""` → `renderPopup`) written **seven** times: picker.go:930-955, sessions.go:565-574,
  settings.go:2378-2396, :2428-2447, autocomplete.go:895-908, usage.go:195-210,
  inspector.go:269+.

**Deletion test.** popup.go passes hard (delete it → eight painters reappear). The state
half has no module to delete — the complexity is already spread. The proof the deepened
shape works exists in-package: `filterPopupRows`/`pickerView` (picker.go:827-865) has two
callers (sessions.go:184-199 re-wraps it) and kills the accept-against-unfiltered-list bug
class.

**Deepened shape.** One value-typed `listSurface` (rows + selection + filter + window +
key verdict) every pane embeds, owning clamp/wrap/filter/esc and the budget→render call.
Each pane keeps rows, accept, hint.

**Win.** ~200-300 duplicated lines deleted across 6 files, ~35 call sites. Marginal cost
of the next pane drops to rows+accept. Testability: today "↓ at the bottom of a filtered
list" is only reachable through `Model.Update` via the `step` helper (model_test.go:52) —
~13,400 test lines over these surfaces pay that tax; a `listSurface.key()` is directly
unit-testable (see Candidate 9's evidence).

**Sequencing.** Prerequisite/first slice: unify the raw text buffers (three fields never
reach `lineEditor`: `picker.filter`, `sessionBrowser.filter`, `sessionBrowser.renameBuf` —
the package answers "what does backspace do to a rune buffer" in **five** places and draws
a caret **three** ways: `lineeditor.go:216` `textWithCaret`, picker.go:873 `"▌"`,
settings.go:262 + sessions.go:600 `"▏"`). The `lineEditor` → `promptEditor` layering
itself is sound; do not merge those. Candidate 2 is a natural first slice (scroll-only
variant); Candidate 12's sub-lists mostly fall out of this one.

**ADR fit.** New file `listsurface.go` + one doc.go line (0043); plain value, no lock
(0011); measurement stays in `theme.measure` (0030). Watch: `popupSpec`'s
`bodyPadAbove`/`bodyPadBelow` pair exists solely to carry the picker's filter line and both
callers set it identically from `filter != ""` — that is list-surface state leaked into the
painter's spec; it should ride the new module (see Smaller findings, popupSpec).

---

## Candidate 2 — The read-only report pane exists twice — **Strong**

**Files:** usage.go (324), inspector.go (369), mouse.go:1303-1508.

**Friction.** `/usage` and `/inspect` are one concept — "scrolled report, esc closes,
click-outside dismisses, wheel scrolls" — implemented twice, near-verbatim:

| concern | /usage | /inspect |
|---|---|---|
| state struct (`open bool; top int`, same comment) | usage.go:44-47 | inspector.go:69-72 |
| key contract | usage.go:112 | inspector.go:215 |
| dismiss | usage.go:163 | inspector.go:245 |
| render + spec | usage.go:170,190 | inspector.go:252,267 |
| pane rect | mouse.go:1303-1321 | mouse.go:1421-1439 (identical, one more `above` element) |
| window | mouse.go:1338-1349 | mouse.go:1458-1467 (same body) |
| click / wheel | mouse.go:1358-1367 / :1374-1390 | mouse.go:1478-1487 / :1494-1508 (copies) |

Only `usageScrollStep` was shared; eight of nine functions were copied. The copies say so
in comments ("It is usagePaneRect one slot further down"). The pane stacking order is
hardcoded in both `above` slices — a latent bug when the next pane lands.

**Deletion test.** Delete inspector.go's pane half → complexity vanishes into the usage
pane's identical body: **pass-through**. Only `usageRows`/`inspectorRows` (the content) is
real per-pane code.

**Deepened shape.** One `reportPane` value owning `{open, top}` + keys + the
rect/window/click/wheel family parameterised by what stacks above; usage.go and
inspector.go become row builders.

**Win.** ~130-150 lines deleted across 3 files; 9 duplicated functions → 1 set; stacking
order stated once. Value type, trivially ADR 0011-safe.

---

## Candidate 3 — The frame publishes its geometry once — **Strong**

**Files:** model.go:2546-2643 (`View`), model.go:2460-2528 (`frameOverlays`/
`transcriptRows`), mouse.go:755-773 (`settingsPaneRect`), mouse.go:1303-1321
(`usagePaneRect`), mouse.go:1421-1439 (`inspectorPaneRect`), model.go:2720
(`contentLineAt`).

**Friction.** `View` appends blocks in a fixed order (transcript, gap, prompt, browser,
picker, settings, usage, inspector, topRule, status, dropdown, queued, input, footer,
bottomRule — model.go:2572-2636), knows every block's row span while composing, and
discards all of it. Three near-identical `*PaneRect` functions then reconstruct a prefix
sum of that order, differing only in a slice literal. Adding a pane in the transcript-side
slot costs **six** edit sites (`View` order, `frameOverlays` field + `height()` +
builder, `framePane` consts, `openPanes`, every `*PaneRect` below the slot), three silent
on omission. Cost side effect: one click runs `frameOverlays()` 4×; with /settings open a
drag in the settings text field re-renders the pane twice per `MouseMotionMsg`
(mouse.go:937-952, `settingsTextPaint`).

The transcript half already does this right: hit-testing reads the painter's own
`lineTargets`. Only the pane half re-derives.

**Deletion test.** Individually each `*PaneRect` earns its keep (2 callers each). Against
the counterfactual — the composer publishes spans — all three become a map lookup:
**pass-through by re-derivation**.

**Deepened shape.** `View`'s composer returns the frame string together with each block's
`[y0, y0+h)` span; painter and mouse read the same value.

**Win.** ~90 lines of arithmetic deleted from mouse.go; stacking order stated once; 3-6
fewer full renders per mouse event; new pane = one place.

---

## Candidate 4 — One body painter for the five block frames — **Strong, time-critical**

**Files:** toolbranch.go:58 (`renderDetails`, expanded flat), :65 (`clipDetails`,
collapsed), :77 (`renderSubDetails`, marker indent), toolblock.go:351-360
(`renderExpandedMember`, `│` gutter), subagentblock.go:414-430
(`renderSubAgentMemberRows`, `│` gutter).

**Friction.** Which physical frame a tool body (`toolView.Details`) is laid out in —
gutter, indent, clip, wrap — is decided independently at five sites. The last two are
near-verbatim duplicates (`leaderRow` → `indicatorRow(glyphExpanded)` → `gutteredWrap`
loop → closing row, differing only in the closer). The shared primitive chain underneath
(`toolRowCells` → `leaderRow` → `indicatorRow` → `gutteredWrap` → `seeLessRow`,
blockstate.go predicates, wrap.go rails) is healthy — the duplication is one level up.

**Why now.** The split-diff plan (ADR 0052) item 7 wires its composer into **two** of the
five paths (toolbranch.go:77 and toolblock.go:351, named in the plan) and not
toolbranch.go:58 or subagentblock.go:424. After item 7 lands as written, an expanded
*targetless* diff block and an expanded diff inside a sub-agent member silently keep the
stacked reading while their siblings split — the same diff renders differently depending
on where it sits. That is the deletion test failing in advance.

**Deepened shape.** One body painter: (detail lines, frame spec, width) → rows. The
split-vs-stacked decision, the wrap, and the tone are made once; the five sites call it.

**Win.** ~40 duplicated lines converge; ADR 0052's rendering rule lands in 1 place instead
of 2-of-5; body-layout bugs get locality.

**Sequencing.** Do **before or inside** the split-diff plan. Afterwards it becomes a
repair. If the plan has already run when you read this: check whether toolbranch.go:58 and
subagentblock.go:424 render regions as split diffs; if not, this candidate is now a bug
fix, not just a deepening.

---

## Candidate 5 — Stats stop round-tripping through their own prose — **Strong, time-critical**

**Files:** toolpresent.go:945-1051 (`sumStats`, `sumDiffCounts`, `parseDiffCounts`,
`sumCountPhrases`, `countPhrase`) vs the producers `diffCounts` (:1271) and `plural`
(:2225).

**Friction.** A stat hook holds typed integers (`pairCounts` returns `(added, removed
int)`), formats them into `"+8 −3"` / `"12 lines"`, stores only the string on
`toolView.Summary.Text`, and the run-aggregate row **re-parses the string back into
integers** to sum a run. `parseDiffCounts` is a hand-written inverse of the format,
"deliberately anchored on that function's exact spelling (the ASCII `+` and the U+2212
minus)" (comment at :981). `sumCountPhrases` (:1010) parses, sums, then re-scans members a
second time to borrow a plural spelling. This is the exact shape ADR 0011 removed at the
package seam (view no longer parses tool prose), surviving intra-file.

**Deletion test.** Delete the parsers → the aggregate slot goes blank: they earn their
keep today. The *string leg* is the pass-through — nothing between `pairCounts` and
`sumDiffCounts` needs the text form.

**Deepened shape.** A small typed stat value (count+noun / added+removed sum type that
knows `add()` and `spell()`) carried beside the rendered text on `toolView`; group rows
add values.

**Win.** ~65 lines and two hand-written format inverses deleted; a wording change can no
longer silently break sums. **ADR 0052 item 5 adds a sixth stat producer**
(`EditRegions.Stat()`, also `+A −R` shaped) — do this before, or the inverse parser gains
another input.

---

## Candidate 6 — model.go finishes the split ADR 0043 started — **Strong**

**Files:** model.go (4,031 lines, 66 Model methods, 14 concern clusters mapped below).

**Friction.** Three self-contained clusters remain inside after ADR 0043's extraction of
sessionsave.go / approval.go / commandrun.go:

1. **Heartbeat / rebind / server switch**, model.go:1701-2193 (**493 lines** — larger than
   any file the ADR already extracted): `heartbeatState`, `rebindIntent`, `offlineNote`,
   `beatCmd`/`armBeat`/`beatTick`, `foldBeat`, `observeBinding`, `applyRebind`,
   `applyPendingRebind`, `rebindNote`, `foldServerSwitch`, `foldBeatFailure`,
   `blockedUpstream`, … `m.hb` is touched in exactly two non-test files (model.go 31
   sites, picker.go 6). Own state (11 fields), own generation guard, own debounce, own
   note vocabulary.
2. **The ask_user pane**, ~370 lines spread over model.go:204-224 (fields), 721-746
   (Update arm), 1226-1300 (keys, inline in `handleKey` — not even a method), 1486-1576
   (`submitAnswer`, `checkedLabels`, `restoreAskDraft`), 3818-4031 (layout). It is
   approval.go's un-extracted twin — the approval file's stated rationale ("both halves in
   one file … so a row can never be paintable and unreachable") applies verbatim.
   `askChecked` alone is written at three distant sites (730-733, 1293-1298, 1611).
3. **Box/square/join painting primitives**, model.go:2795-2953 (159 lines): `squareLine`,
   `squareOnField`, `drawBox`, `drawTitledBox`, `joinScrollbar`, `joinFrame`. Four of six
   have no Model receiver and no Model dependence; callers are popup.go, userblock.go,
   startupbox.go. This is the painting substrate wrap.go stands beside, misfiled in the
   coordinator.

**Deletion test.** All three earn their keep as behaviour; the friction is filing.
"Where is the heartbeat?" answers model.go — the same as no answer (ADR 0043's own words).

**Deepened shape.** Pure same-package file moves: `heartbeat.go`, `ask.go` (mirroring
approval.go's both-halves layout), primitives beside wrap.go. Zero call-site churn.
doc.go gains three lines (docmap test enforces).

**Win.** model.go 4,031 → ~3,000. Zero behaviour change. Ask/approval symmetry becomes
visible, which Candidate 1 later exploits.

**Full cluster map of model.go as of this review** (for whoever splits further):
struct+enum 22-345 · construction/seed 347-592 · `Update` 594-1070 · keys/scroll
1072-1376 · submit/ask-answer 1378-1576 · worker lifecycle 1577-1700 · heartbeat cluster
1701-2193 · layout arithmetic 2194-2439 · `View`+scrollbar 2440-2793 · paint primitives
2795-2953 · input/footer/startup views 2955-3255 · status line 3256-3552 · frame row
allocation 3553-3817 · ask pane layout 3818-4031.

---

## Candidate 7 — toolpresent.go splits along its four seams — **Strong**

**Files:** toolpresent.go (2,514 lines / 1,262 code), transcript.go:1558-1692.

**Friction.** Four modules share one file name (measured spans):

| span | concern | ~lines |
|---|---|---|
| :55-358 | card value type (`toolView`, `toolBody`, `detailLine`, …) — consumed by 7 files | 300 |
| :359-670 | **the presenter registry** — `toolPresenter` (8 hooks) + `toolRegistry` (27 entries) | 310 |
| :697-1200 | view lifecycle (`presentToolCall`, `enrichWithResult`, sanitize/shorten, run aggregation) | 500 |
| :1198-2203 | per-tool stat/target/body hooks (incl. the whole diff-body cluster) | ~1,000 |
| :2204-2514 | **two homeless modules**: generic text utils (`clipDetail`, `plural`, `firstLine`, … — called from 7 files) and the JSON-argument display module (`argumentDetails` etc. — **approval.go:359 needs it**) | 310 |

Also misfiled: `stripEscapes`/`bidiControl`/`stripEscapesAll` — **the package's security
seam, referenced from 19 files** (doc.go's second invariant) — live in transcript.go, a
file named for the scrollback.

**What is healthy and must not change:** the registry is genuinely deep. Adding a tool =
**one** edit (one `toolRegistry` entry, :471). Verified: all 27 tool-name literals occur
inside the registry; only `subAgentToolName` (subagentblock.go:15) and `askUserToolName`
(:441) escape as constants, both for *layout*, not wording; the registry is read at 3
sites (:698, :1080, :1103). Zero per-tool switches elsewhere in the package.

**Deepened shape.** Pure moves along the existing section banners: `toolview.go` (card
type + lifecycle), `toolregistry.go` (presenter + table + hooks), `toolargs.go` (JSON
argument display), `sanitize.go` (the escape seam out of transcript.go), diff bodies
beside the split-diff plan's `splitdiff.go`. Each new file = one doc.go line.

**Win.** ~1,400 lines relocated, 0 call sites changed; the security seam gets a findable
home; ADR 0052 (which grows the diff cluster past 2,800 lines in-place) lands in a file
named for it.

---

## Candidate 8 — Every Update arm delegates, like the six that already do — **Worth exploring**

**Files:** model.go:594-1070 (`Update`, 477 lines, 33 case arms), model.go:1089-1360
(`handleKey`, 272 lines).

**Friction.** The six arms that delegate to a named fold (`beatMsg`→`foldBeat`,
`actuationMsg`, `restoreMsg`, `settingsEditedMsg`, `configChangedMsg`, `sessionListMsg`)
are 3 lines each; the arms that inline are 25-40 (`askReqMsg` 721-746, `compactDoneMsg`
833-855, `spinnerTickMsg` 949-971). The winning shape exists and is applied
inconsistently. `handleKey` one level down: 8 sequential "does overlay X claim this key?"
guards (1111-1177) whose order is load-bearing and stated only across ~70 lines of
comment, then two `msg.String()` switches, then three state-gated blocks.

**Deepened shape.** Mechanical: each inline arm becomes `return m.foldX(msg)` in its
concern file (the pattern `foldEvent` proved); the overlay key-claim order becomes an
ordered list of claimants — data, not eight hand-written ifs.

**Win.** `Update` ~477 → ~120; `handleKey` ~272 → ~90; claim order readable and testable.
No external churn. Combines naturally with Candidate 6 (the ask and heartbeat arms move
with their clusters).

---

## Candidate 9 — The Options seam: 63 fields become ~10 named interfaces — **Worth exploring**

**Files:** tui.go:237-972 (735 lines for one struct — the whole interface between
`cmd/apogee` and `internal/tui`).

**Friction.** `Options` carries 63 fields, ~30 of them one-purpose bare `func` values
(SaveHostAcknowledgement, WriteSetting/ResetSetting/ApplySetting, Heartbeat/Rebind,
Servers/SwitchServer/BindServer/RecordServerChoice, the launcher family, the scheme
family, …). The seam grows one field per host capability — exactly as fast as the
implementation it hides: **shallow**. The same file already proves the deep shape: the
named interfaces `Engine`, `SessionHost`, `SkillCatalog`, `RecallHost`, `Scheduler`.
Four func families are obviously interfaces not yet made (settings ×4, server ×4,
launcher ×7, scheme ×3). Testability: `m.opts` is read in 19 non-test files; every test
constructs a whole Model — no family can be faked alone.

**Deletion test.** The struct earns its keep as a container (the alternative is 63
parameters); the grouping is what makes the seam deep. Two adapters per family already
exist: production wiring and the tests' fakes.

**Deepened shape.** Fold the func families into ~5 new named host interfaces beside the 5
existing, keeping the nil-means-unwired contract each family already assumes.

**Win.** tui.go 1,554 → ~900; per-family fakes in tests; churn concentrated in
`cmd/apogee`'s wiring (which ADR 0043 already split into `wire_<seam>.go` files — the
seams line up).

---

## Candidate 10 — The command table absorbs its satellite lists — **Worth exploring**

**Files:** command.go:187-209 (`commandSpecs`, 21 verbs — the real registry),
command.go:229-238, command.go:48-62 (`parsedInput`), commandrun.go:224,
commandrun.go:229-393 (`runCommand`), actuation.go:177-183 (`actuationBlocked`).

**Friction.** The registry is deep (parser, dropdown, mid-run policy, recall policy,
accept rule all read it; `TestCommandSpecsReadAlphabetically` pins ordering). But three
behavioural policies live as string-literal lists elsewhere:

- `commandrun.go:224` — `parsed.command == "continue" || parsed.command == "compact"`
  ("opens an Exchange", the upstream gate);
- `actuation.go:179` — hardcoded six-name list `"unload-model","stop-server","model",
  "server","continue","compact"` ("touches the server", the latch block list);
- `command.go:229-238` — a second switch for the four verbs with their own grammar, plus
  the `parsedInput` union growing one typed field per arg-taking verb (`confine`,
  `colorScheme`, `effort`, `undo`).

Cost to add a verb today: plain report 3 places; arg-taking 8; one that opens an Exchange
or moves the server 9-10 — and forgetting the ninth fails nothing visibly (the verb runs
into a dead upstream or a held latch). Nothing pins the satellite lists.

**Deepened shape.** Fold `opensExchange`, `touchesServer`, and a
`parseArgs func([]string) (any, error)` hook onto `commandSpec`; `parsedInput` carries one
opaque args value.

**Win.** ~40 lines net; two silent-drift lists deleted; new verb = 3 places. Full literal
scatter recorded in Appendix B for whoever does this.

---

## Candidate 11 — entryKind answers for itself, with the exhaustiveness test Events already have — **Worth exploring**

**Files:** transcript.go:95-106 (enum), :389 (`isHostNote`), :1165 (`hasBlockState`);
render.go:493 (paint switch), :404-405 (tail classification); transcriptcodec.go:213
(`entryKindNames`), :317-321, :416; paintcache.go:124 (`cacheable`).

**Friction.** Adding one entry kind edits **9 sites in 5 files**; six kind-keyed rules in
three files; only the codec name-map has a structural test. The package already solved
this exact problem for Events: `fold_test.go:175` `TestFoldEventCoversEveryEventVariant`
parses `internal/domain/events.go` and fails on an unanswered variant. No equivalent
exists for entry kinds. (The codec itself is well designed — name map + documented
unknown-kind skip, not a switch; don't touch its degrade path.)

**Deepened shape.** A behaviour table on the kind (`persistedName`, `carriesBlockState`,
`isHostNote`, `cacheable`) collapsing the six predicates, plus a fold-style completeness
test. Edits per new kind: enum row + painter case.

**Win.** 9 edit sites → 2; ~40 lines consolidated; a proven in-package pattern.

**Related (do together):** the "is this a sub-agent run head" predicate is spelled inline
**12 times** (`e.kind == entryToolCall && e.tool.name == subAgentToolName` + varying
conjuncts) — transcript.go:327, :426, :910, :1024, :1121; subagentblock.go:29, :58, :103,
:119-120, :367; usage.go:260; transcriptcodec.go:490 re-derives it a third way — while the
named module `headsSubAgentRun` (transcript.go:1395) has **one** caller. Extracted at the
wrong granularity; promote to the questions the sites ask (`headsRun()`, `opensRun()`,
`headsRunFor(callID)`). The `!done`-vs-phase distinction documented at subagentblock.go:329
currently must be remembered at each site.

---

## Candidate 12 — settings.go merges its twins, then splits its file — **Worth exploring**

**Files:** settings.go (2,481 lines / 1,221 code, 67 Model methods), mouse.go:755-1240
(485 settings-only pointer-geometry lines).

**Friction.** `settingsKind` (:97-107) names six surfaces sharing one file, one pane
struct, one key router — and two of the six are written twice:

- `renderSettingsEnum` (:2366-2396) vs `renderSettingsMechanisms` (:2418-2447) — a 30-line
  function and its copy; the second's comment admits it ("It is the value sub-list's shape
  … with the one difference the content forces").
- `settingsBufferKey`/`settingsCommitBuffer` vs `settingsTextKey`/`settingsCommitText` —
  a 25-line pair differing in the commit key (⏎ vs ctrl+s) and `TrimSpace` vs `TrimRight`.

Deleting the enum sub-list would make the mechanism sub-list's complexity vanish too — the
tell that one module is written twice. The rest of the file (watcher :945-1041, external
editor :752-924, live-apply router :1383-1520, edit journal, armed reset) is 4+ unrelated
clusters hanging off the same pane.

**Deepened shape.** Merge the twin pairs (sub-list with a content parameter; value editor
with a commit-key parameter), then split along the `settingsKind` seams into 2-3 files.

**Win.** ~150-200 duplicated lines deleted; the file heads toward the ~400-line guideline.
**Constraint:** display projection stays in `cmd/apogee` (ADR 0035/0037) — only the pane's
own machinery is touched. If Candidate 1 lands first, the sub-list halves mostly fall out
of it.

---

## Smaller findings — cheap, independent

| finding | where | move |
|---|---|---|
| "Reset the session" is a hand-kept checklist in 4 places; `finishWorker` resets 11 fields of 8 concerns; `startNewSession` vs `resumeLoaded` differ on `usage` (not reset by /clear while `ctxUsed` is) and `titleTouched` (not reset on resume while `autoTitleFired` is) — decision or omission, unreadable. Plus one byte-for-byte replay block in 2 files (model.go:457-466 vs sessions.go:482-494). | model.go:1604-1653, :352-427, :443-467 · commandrun.go `startNewSession` · sessions.go:452-506 | per-lifetime state values with their own reset; one shared replay function |
| The session title has no owner: 8 write sites in 5 files (`pendingTitle`/`pendingSource` written in autotitle.go:187,243,251; commandrun.go:163; sessions.go:479; sessionsave.go:319,349-355); the stash/restash invariant lives in ~60 lines of comment (autotitle.go:37-94) with no code home; untestable without a whole Model. | autotitle.go · sessionsave.go · sessions.go · commandrun.go · model.go:105-106 | a small title value with adopt/stash/flush/restash verbs |
| `uiState` is a bare int with 34 open-coded comparisons across 9 files; the idle-or-running set is named in prose ("BOTH live states", model.go:1126) but spelled inline 8 times; the state↔payload invariants (`pending != nil` exactly in approval state, `pendingAsk` likewise) are maintained by hand in `finishWorker` and re-asserted defensively at reads (model.go:2500, :2503, :3606-3608). | model.go:29-37 + 9 files | predicates on the state: `editable`, `live`, `busy`, `decisionPending` |
| `toolView.sanitize()` (toolpresent.go:839) is a security obligation enforced by memory: the wire structs have an exact member-list test (transcriptcodec_test.go:1024) but nothing pins that a new `toolView` field reaches the strip. ADR 0052 item 5 adds `Regions` through this exact hole — region text is tool-recorded file content and needs stripping. | toolpresent.go:839, :801 | structural (reflective) test: every string/detailLine field is reached by the strip |
| `paintKey` completeness ("a field missing here is a stale paint on screen", paintcache.go:73) is a comment-level contract across two files; `TestPaintCacheMatchesAColdRenderThroughEveryMutation` enumerates mutations by hand — same failure mode one level up. | paintcache.go:72-194 · 5 painter files | painters take a stated input record instead of raw `entry` |
| `renderView` is a 245-line function: a 5-branch block-shape chain (render.go:277, :325, :352, :373, :401) locked to a 5-value enum in another file (paintcache.go:65-69), with hand-written index advancement per branch (`i += span` / `i += calls-1` / `i = grp[end].at`) — the one place an off-by-one silently skips or double-paints a block. Well covered by `TestTranscriptLayoutGolden` + paintcache tests. | render.go:169-414 | one "what block starts at i" resolver returning shape+span+closure together |
| The click-map is a module only its test file names: `blocktarget_test.go` (737 lines) exists with no `blocktarget.go`; the implementation is spread over render.go (41 refs), toolblock.go (26), subagentblock.go (9), toolbranch.go (6), userblock.go (5), consumed by mouse.go and blockcursor.go. | 8 files | name it: `blocktarget.go` for the primitives (~60 lines out of render.go) |
| `uiApprover.Approve` and `uiAsker.Ask` are structurally identical 10-line rendezvous bodies (approver.go:32-41, asker.go:33-42). Four host delegates use three cross-goroutine idioms — rendezvous (approver/asker), mailbox (`interjectBox`), fire-and-forget (`uiPresenter`, `Bridge.Notify*`) — and no file states the taxonomy, which is what makes ADR 0011's legality rules hard to check by reading. | approver.go · asker.go · interject.go:57-137 · presenter.go:101-129 | one generic parked-call helper + a doc paragraph naming the three idioms |
| Scrollbar thumb arithmetic exists twice; popup.go:679 names the relationship ("the transcript's with the two counts it conflates pulled apart") — the popup version is the general case. | model.go:2770-2793 · popup.go:685-705 | one thumb-geometry function (window, total, painted-height) |
| `chromelayout.go` (72 lines) holds an ADR 0030 widget mirror (`inputContentRows`) and a generic `clampInt` (~15 call sites) — nothing in common; the file fails the deletion test as a file. | chromelayout.go | rehome both (mirror beside the other mirrors, clamp wherever), delete the file |
| **Watch item, not a rewrite:** `popupSpec` is at 20 fields; `titleFromBody`, `rowGap`, `rowPadBelow` have exactly one caller (the ask prompt), `bodyPadAbove`/`bodyPadBelow` exist solely for the picker filter line — the deep painter's interface creeping toward its implementation. | popup.go:318-343 | fold the ask trio into one named row-style; filter pads ride Candidate 1's module |

## Verified healthy — do not re-litigate

- **The tool registry** (toolpresent.go:471): 27 tools, one edit site each, zero per-tool
  switches elsewhere. Deep and open (ADR 0002). Candidate 7 moves it whole, never reshapes it.
- **The event fold** (fold.go + transcript.apply's 9-variant type switch) with
  `TestFoldEventCoversEveryEventVariant` — the package's model pattern (Candidates 8, 11
  copy it).
- **The width authority** (ADR 0030): zero banned measurement calls outside declared
  mirrors; markdown.go/mdtable.go route everything through `th.measure`/`wrapText`, each
  direct `Hardwrap` commented. Holding.
- **theme.go**: data + one 151-line constructor (the honest cost of 29 scheme roles, ADR
  0040's single seam). Not logic.
- **The paint cache** (paintcache.go): documented 10× win (render.go:423-437: 95% CPU /
  0.48s click → 0.05-0.07s). Earns its keep emphatically; only its key contract is a
  finding (above).
- **inputaccent.go's widget mirrors**: look like duplication of wrap.go, are deliberately
  not — each mirror's oracle is its widget, not the painter (ADR 0030 §6). Consolidating
  would reintroduce the caret-off-by-one class. The real risk is a bubbles bump desyncing
  the mirror — a pinning problem, not architecture.
- **`lineEditor` → `promptEditor` layering**: sound. `prompteditor.go` is thin (77 code
  lines) but deliberate field-grouping per ADR 0043; do not "deepen" it.
- **commandrun.go**: a file split (locality by file), not a module — it has no interface
  and needs none. Do not invent a seam for it.
- **The four small verb files** (confine.go, effort.go, colorscheme.go, undo.go): correct
  ADR 0043 shape — pure note builders, real per-verb wording. Their only tax is Candidate
  10's arrival path.
- **Prose-parsing residue in six stat hooks** (testVerdictStat etc., toolpresent.go:
  1373-1482, five package regexes): a documented trade (design call 14 — presentation must
  not grow the engine), every hook total (returns false on unrecognised shapes). One doc
  nit: doc.go's ADR 0011 narration reads as if prose parsing was fully eliminated;
  toolpresent.go:1183-1189 is honest that it was not. Fix the doc.go sentence, not the code.

## Recommended sequence

1. **Candidate 4 + 5 now** — small, and time-critical against the in-flight split-diff
   plan (2026-08-19 - 03). Together: one body painter + typed stats, then the plan's items
   5/7/8/9 land in one place. Add the sanitize structural test (Smaller findings row 4) in
   the same pass — ADR 0052 item 5 adds a field through that hole.
2. **Candidates 6 + 7** — zero-risk pure file moves (model.go and toolpresent.go splits +
   the sanitize.go rehome). Land any quiet afternoon; every later candidate benefits from
   the navigation.
3. **Candidate 2, then 1** — the report pane as the first slice of the list surface, then
   the full list-surface module (with the text-buffer unification as its first step).
   Candidate 12's twin-merges fall out at the end of this.
4. **Candidates 3, 8, 10, 11, 9** in any order — each independent.

Every step: behaviour-preserving, one commit per coherent move, `make check` green, doc.go
updated in the same commit (docmap test enforces). ADR 0043 is the precedent for the file
moves; a new ADR is warranted only if Candidate 1 (a shared surface module) or 9 (the
Options interface regrouping) is taken up, since both shape how future surfaces are built.

## Appendix A — file sizes (non-test lines / code lines where measured)

model.go 4031/1703 · toolpresent.go 2514/1262 · settings.go 2481/1221 · transcript.go
1692/734 · tui.go 1554 · mouse.go 1510/834 · popup.go 1449/572 · picker.go 1051/530 ·
autocomplete.go 908/401 · doc.go 777 · command.go 762/365 · schedule.go 749/393 ·
sessions.go 685/383 · subagentblock.go 580/235 · actuation.go 548 · render.go 537/237 ·
interject.go 527/258 · transcriptcodec.go 516/265 · mdtable.go 486/288 · toolblock.go
464/199 · toolbranch.go 455/170 · theme.go 427/226 · spinner.go 407/173 · lineeditor.go
402/153 · commandrun.go 396/150 · sessionsave.go 386/208 · skills.go 370 · inspector.go
369/188 · approval.go 365/135 · toolleader.go 348/133 · inputaccent.go 343 · autotitle.go
335/150 · usage.go 324/178 · paintcache.go 324/144 · activity.go 304/131 · wrap.go 296/122
· diagnostics.go 292/141 · markdown.go 279/177 · blockcursor.go 269 · prebound.go 261 ·
prompteditor.go 242/77 · userblock.go 232/127 · worker.go 228 · messages.go 225 ·
recall.go 211 · workspacepath.go 198 · presenter.go 198 · startupbox.go 194/90 ·
keymigration.go 192 · confine.go 184 · syncoutput.go 183 · colorscheme.go 179 ·
blockstate.go 168/63 · sink.go 163 · bridge.go 158 · undo.go 151 · width.go 144 · fold.go
140 · filecache.go 131 · effort.go 81 · chromelayout.go 72 · asker.go 42/10 · approver.go
41/10 · logo.go 22/7. Test totals worth knowing: model_test.go 5163 · mouse_test.go 3476 ·
settings_test.go 3311 · transcript_test.go 2904 · picker_test.go 2257 · popup_test.go 2106
· sessions_test.go 2012.

## Appendix B — command-name literal scatter (for Candidate 10)

Registry: command.go:188-208 (21 rows). Satellites (drift risk): command.go:230
(`confine`), :232 (`color-scheme`), :234 (`effort`), :236 (`undo`) — the grammar switch;
commandrun.go:224 (`continue`/`compact` Exchange gate); commandrun.go:231-388 — the
21-case `runCommand` switch; actuation.go:179 (six-name latch list); actuation.go:58-59
(`verbUnload`/`verbStop` constants, also display text). Usage-string literals (harmless,
listed for completeness): command.go:417, :479, :496, :532, :549, :586, :597;
commandrun.go:251; confine.go:95; undo.go:39, :112; picker.go:162-163, :254;
schedule.go:40; sessions.go:78. Outside the package: cmd/apogee/headless.go:148;
internal/config/configwrite.go:79. There is no `/help` command, so the dropdown summaries
in `commandSpecs` are the only help surface — nothing else to keep in sync.

## Appendix C — entry-kind decision sites (for Candidate 11)

Formal switches: render.go:493 (paint, 10 cases + default), transcript.go:1166
(`hasBlockState`). Kind-keyed maps/predicates: transcriptcodec.go:213 (`entryKindNames`),
:228 (inverse), paintcache.go:124 (`cacheable`). Inline comparisons (24): render.go:404,
:405; transcript.go:327, :390×2, :426, :650, :705, :717, :729, :748, :910, :1024, :1073,
:1121, :1143, :1230, :1321, :1327, :1396; transcriptcodec.go:317×2, :320, :416;
blockstate.go:108; subagentblock.go:29, :58, :103, :119, :367, :497, :572; usage.go:260;
schedule.go:444.

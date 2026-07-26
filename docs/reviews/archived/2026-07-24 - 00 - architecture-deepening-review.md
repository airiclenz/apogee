# Handoff — Architecture deepening review: 7 candidates (all landed; ledger closed 2026-07-26)

Date: 2026-07-24
Session type: **review only** (`/improve-codebase-architecture`). No code changed, no plan
written, no ADR/CONTEXT edit made. Deliverable is this handoff + the HTML report beside it.

**Companion artifact (the visuals):** `docs/reviews/archived/2026-07-24 - 00 - architecture-deepening-review.html`
— before/after diagrams for every candidate. This markdown is the pick-up-the-work doc; the
HTML is the illustrated version. (The original was written to a session scratchpad, which is
ephemeral — this copy is the durable one.)

## What this session did

Ran a depth review of the whole engine (~34k LOC, 18 `internal/` packages) via five parallel
`Explore` agents (agent-loop, mechanisms, tools/security/platform, tui/present, supporting
packages), read CONTEXT.md and the seam-defining ADRs (0002, 0003, 0010, 0011, 0012, 0020),
and cross-checked the headline claims at file level. Output: **7 ranked deepening candidates +
a smaller-deepenings list**, framed in the depth glossary (module / interface / deep / shallow
/ seam / leverage / locality).

**Ledger (updated as candidates land).** **01 landed 2026-07-24**
(`docs/plans/archived/2026-07-24 - 00 - turn-lifecycle-owner-plan.md`); **02 landed 2026-07-25**
(`docs/plans/archived/2026-07-25 - 00 - url-safety-choke-point-plan.md`); **04 and 07 landed
2026-07-25**, both under `docs/plans/archived/2026-07-25 - 01 - mechanism-registration-collapse-plan.md` — 07
was folded into 04's plan as item 2, because a shared stack-validity checker is nearly free once the
Mechanism metadata is row-shaped, but it was built and committed standalone; **05 landed 2026-07-25**
(`docs/plans/archived/2026-07-25 - 02 - windows-label-module-plan.md`). Of the smaller
deepenings, **session store lifecycle landed 2026-07-24** (absorbed by the session system, ADR 0022).
**03 and 06 landed 2026-07-25** and the four remaining
smaller deepenings landed 2026-07-25/26, all via
`docs/plans/archived/2026-07-25 - 03 - architecture-review-closeout-plan.md` (its item 8 —
`read_file`/`open_file` — via that plan's own follow-on,
`2026-07-26 - 00 - land-item8-close-size-window-plan.md`; see the cards and the
smaller-deepenings list for what was built). **The ledger is empty as of 2026-07-26**, and as of the
same day both parked items have their disposition: the `/code-audit` on the live url-safety gap
**ran** (`docs/reviews/2026-07-26 - 00 - url-safety-live-gap-audit.md`), and
`Request.InjectContext` is **parked in `TODO.md`** as a grill brief (see *Recommended next step*).
This doc is a closed record, not anyone's to-do list.

## The spine is already deep — leave it alone

Reported so the next agent does **not** "fix" it: `resolve()` (the entire Resolution verdict
behind one pure, table-tested function — `internal/agent/resolution.go`), `runSubprocess` (every
subprocess tool funnels Confinement through one point — `internal/tools/exec_common.go`), the
`Confiner` seam (3 genuinely-different OS backends behind 2 methods), the Mechanism
**dispatch + self-regulation** core (`internal/domain/{registry,mechanism,hooks}.go`,
`internal/agent/{hookrun,selfreg}.go`), `provider.Responder`, and fingerprint identity
(`domain.ModelFingerprint.Label` as the single key across library/validated/probe — a *refuted*
hypothesis; it is deep, not scattered). Several candidates below are literally "make X follow
the deep pattern these already set."

## The candidates

Evidence is file-level; line numbers marked ✓ were verified this session, others come from the
Explore agents and should be spot-checked before acting.

### 01 — Give the Turn a lifecycle owner · **Strong · TOP RECOMMENDATION** · ✅ **LANDED 2026-07-24**
- **Files:** `internal/agent/loop.go` (`step`, `closeExchange`✓L754, `completeTurn`✓L763,
  `abandonTurn`✓L781, `cancelTurn`✓L807), `compact.go` (`emergencyFold`✓L205), `selfreg.go`
  (`endTurn`/`discardTurn`), `state.go` (`exchangeStart`).
- **Problem:** The Turn — begin / overflow-recover / end — is the loop's central concept and has
  **no module**. Each of the 3 end-helpers touches a different subset of {tracker, turnIndex,
  rollback, deferred, inExchange}; the rules relating them live in 15-line defensive comments.
  The overflow re-derive ritual (`restoreDeferred → emergencyFold → rebuild req/rollback/deferred`)
  is copy-pasted 3×; `buildRequest` is reconstructed **7×** in loop.go (✓ grep); `exchangeStart`
  is written in **5** places. `emergencyFold` returns a bare `bool` but leaks 4 stale-locals +
  "check ctx.Err() yourself" onto `step()`.
- **Deepening:** a small `turn` module owning the state tuple, the 3 exits as a table, and one
  `fold()` that returns fresh working values. `step()` reads as a loop again.
- **Why first:** highest leverage + locality, no ADR conflict (engine-internal), and the target
  shape already exists **in the same package** — `resolve()` is the yardstick. Also removes the
  worst test friction: self-reg/turn state is today only assertable by reaching through the Agent
  (163 `a.conv` + 22 `a.tracker` reach-ins across tests).
- **Related (smaller):** split `loop.go` (1172 LOC) — the construction cluster and the
  domain→wire translation cluster are deep modules trapped in the god-file. ✅ **Landed with 01**:
  both clusters moved out (`construct.go` 223 LOC, `wire.go` 121 LOC) and `loop.go` is **773 LOC**.
  Still over the ~400-line house guideline, so a further split remains available — but the two
  modules the card named are out.

### 02 — One choke point for url-safety · **Strong** · ✅ **LANDED 2026-07-25**
- **Files:** `internal/agent/resolution.go` (`classNetwork → resolveRun`), `dispatch.go`,
  `internal/tools/{web_fetch,http_request,web_search}.go` (✓ the only 3 with `CheckContext`),
  `internal/security/urlsafety.go`.
- **Problem:** url-safety is the **one** Guardrail applied per-tool, not once. Each network tool
  re-runs the same `CheckContext` + rebuild-client + render-error trio. The Auto ladder's
  "url-filtered" is a promise enforced only by each tool's discipline — a third-party network
  tool (or a forgetful built-in) runs with **zero** url-safety and dispatch can't tell.
  Contrast: Confinement is impossible to forget because all subprocess tools route through
  `runSubprocess`.
- **Deepening:** apply `URLGuard` once — in dispatch for `classNetwork`, or a mandatory
  `runNetwork` helper every network tool routes through. Makes "url-filtered" true by
  construction; deletes the copied trio.
- **Note:** strengthens ADR 0012's url-safety floor (no conflict). The **live** gap is a
  correctness matter — this card fixes the *shape*; run `/code-audit` to confirm/close the hole
  itself independently.
- **Landed 2026-07-25** via `docs/plans/archived/2026-07-25 - 00 - url-safety-choke-point-plan.md`, as the
  *funnel* variant (a `networkTool` all three built-ins embed, not a dispatch pre-flight — a
  dispatch check keyed on a declared URL list would still be a declaration). The funnel carries an
  **unexported url-filter marker**, and the ladder split `classNetwork` (marked ⇒ auto-runs in Auto)
  from `classThirdPartyNetwork` (unmarked ⇒ gates, reason `unfiltered network reach`), so
  "url-filtered" is now true by construction. Recorded in **ADR 0012 amendment 2026-07-25** +
  `confinement-execution-contract.md` §4. The separate `/code-audit` on the *live* gap **ran
  2026-07-26** — `docs/reviews/2026-07-26 - 00 - url-safety-live-gap-audit.md`. Verdict: the hole
  this card named is **closed at the funnel**, but the "url-filtered" promise is defeated one rung
  up, in `classifyTool`'s ordering (a self-declared `ReadOnly()` is consulted before any unfakeable
  marker). 4 High + 10 Medium findings, each awaiting an owner decision in that report; nothing
  from them is owed to this card.

### 03 — Hand the view structured tool results · **Strong** · ✅ **LANDED 2026-07-25**
- **Files:** `internal/tui/toolpresent.go` (regexes ✓L243–246: `reReadRange`, `reWriteBytes`,
  `reListEntries`, `reGrepMatches`), `internal/tools/{read_file,write_file,list_dir,grep}.go`,
  `internal/domain/tools.go` (`ToolResult`).
- **Problem:** a 24-entry registry in the view reconstructs *what each tool did* by regex-matching
  the free-text result string meant for the model. Stringly-typed cross-package contract with no
  type — the TUI doesn't even import `internal/tools`. A format tweak silently degrades the card
  to a verbatim first line.
- **Deepening:** tools emit a small structured summary alongside `Content` (typed field on
  `ToolResult`); the view reads fields, keeps only labels/verbs. Deepens `internal/tools`, thins
  the view's leakiest file, honours ADR 0011 by construction, and feeds a future headless/bench
  host.
- **Note:** additive to public `ToolResult` (minor bump under ADR 0010 stability; back-compatible
  with ADR 0002's open extension point — summary optional, prose fallback stays).
- **Shape resolved 2026-07-25** →
  `docs/plans/archived/2026-07-25 - 03 - architecture-review-closeout-plan.md`, items **1–5**. The summary is a
  **sealed sum** in `domain` (`ToolSummary` + seven variants), sealed the way `Event` is, so an
  embedder can *read* every variant and *add* none. **Seven** tools carry one — the ones whose
  outcome the view re-derives (`read_file`, `write_file`, `list_dir`, `grep`, `view_diff`,
  `web_search`, `open_file`); the `firstLineDetail` and `outputDetail` families stay on prose,
  because quoting a fixed sentence or compressing free-form stdout is *rendering*, not scavenging.
  The acceptance oracle is that **rendered output does not change byte for byte**. Two figures in
  this card, corrected while planning: the registry has **21** entries, not 24, and the view
  re-derives **seven** facts (four regexes plus three prefix/count sniffers in `grepDetail`,
  `diffDetail`, `openFileDetail`/`searchDetail`).
- **LANDED 2026-07-25** — plan items **1–5**, each on its own green gate (`d964460` · `05aa0a0` ·
  `5463257` · `551abac`, plus this documentation commit). What was actually built, against the
  card's sketch:
  - **The typed field is a SEALED SUM, not a struct of loose fields.** `internal/domain/toolsummary.go`
    holds `ToolSummary` (unexported marker method, the `Event` precedent) and its **seven** variants;
    `domain.ToolResult` gained one optional `Summary` field; `apogee.go` re-exports all eight names.
    A flat `struct{Kind; A, B int; Text string}` and a `map[string]string` were rejected on the
    record (plan D1) — both would have kept the view *interpreting* rather than reading. Deliberately
    **no exported base struct** (unlike `EventBase`): an embeddable base would re-open the sum, so an
    embedder reads every variant and adds none.
  - **Seven tools attach one, and only seven** (plan D2): `read_file`, `write_file`, `list_dir`,
    `grep`, `view_diff`, `web_search`, `open_file` — each through one new `okSummary` constructor
    (`internal/tools/tools.go`, 7 call sites), each fact taken from the computation the tool already
    ran for its own header rather than re-read from its output. The `firstLineDetail` and
    `outputDetail` families stay on prose: quoting a fixed sentence or compressing free-form stdout
    is *rendering*, not scavenging. An error result never carries a summary.
  - **The view stopped parsing.** `summaryLine` (one exhaustive type switch) and `diffBody` replaced
    the four anchored regexes, `reSearchHit`, and the five prose sniffers (`detailFromPattern`,
    `grepDetail`, `searchDetail`, `openFileDetail`, `diffDetail`); the `regexp` import is gone from
    `toolpresent.go`. The seven entries keep `firstLineDetail` as the **floor** (plan D6), so a
    summary-less result degrades to its own first line rather than to a raw dump.
  - **The acceptance oracle was byte-for-byte identical output** (plan D4), and it held: every
    `wantDetail` literal in `toolpresent_test.go` is unchanged in the diff, and
    `transcriptcodec_test.go` passed **untouched** — the session wire form is the *rendered* view, so
    no summary reaches disk. The new cross-package pin `internal/tui/toolsummary_pin_test.go`
    executes all **seven** real tools against a temp workspace and asserts the rendered card line;
    that is the test the old regexes never had, and it is what now catches a tool that stops
    attaching its summary.
  - **Two aggravating facts found while planning, both now moot:** `clampToolResult` rewrites
    `Content` *before* the `ToolResultEvent` is emitted, and a `PostToolResult` Mechanism may rewrite
    it too (`errorenrich.go`) — so the old extractors' correctness rested on a coincidence between a
    compression policy and a display regex. A summary describes what the tool *did*, not what the
    text *says*, so neither seam can invalidate it.
  - **Item 5 is the documentation** — CONTEXT.md gains the **Tool summary** term (the structured
    half, sealed, optional, never persisted); ADR 0002 carries a dated note that a tool *may* attach
    one and that **omitting it is fully supported**, so the open extension point is unchanged; ADR
    0011 carries a dated note that the thin renderer now reads a typed value and **owns its own
    wording** (plan D5); the CHANGELOG names the change in the user's terms and calls it
    **additive**; and `internal/tools/doc.go` + `internal/tui/doc.go` state the two halves and the
    seven-tool rule at each end of the seam.
- **The line-count figures, reported straight** (the plan expected this card to ADD lines; the
  figures below cover the four code commits — item 5 adds doc comments only):
  **production** is net **+213** — `internal/domain` **+94** (a new file: the sum, its variants and
  the header explaining sealing/optionality/non-persistence), `apogee.go` **+32** (eight aliases),
  `internal/tools` **+76** (+123/−47, the render helpers now returning their facts), and
  `internal/tui` **+11** (+131/−120 — `summaryLine`/`diffBody` and a rewritten file header cost
  almost exactly what ten regexes and sniffers gave back). **Tests +886** (`domain` +80, `tools`
  +489, `tui` +317), of which the seven-tool pin is 163. **Not a finding:** the win here is one
  typed seam replacing a stringly-typed cross-package contract — the compiler now knows the view
  and the tools are talking about the same fact — plus a public surface an embedder can *read*.
  It is not a line count, and the TUI's near-flat production delta is the honest shape of it.

### 04 — Collapse the 21× Mechanism registration ritual · **Strong** · ✅ **LANDED 2026-07-25**
- **Files:** `internal/mechanisms/*.go` (~20 `Descriptor()` methods ✓), `catalogue.go` (the two
  parallel maps `catalogue` + `descriptors`), `internal/domain/mechanism.go`,
  `internal/agent/loop.go` (`buildEnabledMechanisms`, `libraryMechanismID` duplicate const).
- **Problem:** every Mechanism repeats a 7-part ritual of near-constant size (ID const · two-map
  `init()` · struct · constructor · descriptor var · `Descriptor()` · `Ordering()`). For the ~8
  trivial Mechanisms the ritual **is** the module (shallow). Two maps hand-synced; an ID
  re-declared as a literal in a second package; dependency-bearing Mechanisms special-cased in
  `buildEnabledMechanisms`.
- **Deepening:** one `Register(descriptor, ordering, constructor)` → single
  `{constructor, descriptor, ordering}` table, ID derived from `descriptor.ID`, methods
  synthesized. Let a catalogue row declare its deps so the engine loops uniformly.
- **Note:** preserves ADR 0003 — registry ordering/stacking semantics unchanged; only the
  **authoring** seam deepens. Deep dispatch/self-reg core untouched.
- **Grilled 2026-07-25** → `docs/plans/archived/2026-07-25 - 01 - mechanism-registration-collapse-plan.md`
  (6 items). The owner chose the **deeper** of the two shapes: the metadata leaves the Mechanism
  entirely and `MechanismRegistry` stores `RegisteredMechanism{Descriptor, Ordering, Hook}` rows,
  rather than staying on the instance behind an embedded helper. The card's "methods synthesized"
  sketch is **not buildable as written** — Go cannot decorate a value with `Descriptor()`/`Ordering()`
  while preserving which of the five hook interfaces it satisfies (embedding a type parameter is
  illegal; a concrete wrapper would make every Mechanism claim every hook point). Accepted cost: a
  public break (`apogee.Mechanism` removed in favour of `apogee.RegisteredMechanism`;
  `MechanismRegistry.Add`/`.Ordered` change shape) plus an ADR 0003 amendment moving its clauses
  (2)/(3) from *method* to *registration argument* — cheap at `v0.8.3`, and locality is preserved.
- **LANDED 2026-07-25** — all six plan items, each on its own green gate
  (`a924ef1` · `f00cd6e` · `b651dc3` · `26bf987` · `4803bdb`, plus this documentation commit). What
  was actually built, against the card's sketch:
  - **Item 1** (`a924ef1`) — the two hand-synced maps (`catalogue` + `descriptors`) are **one `row`
    table**; each Mechanism file's `init()` is a single `register(row{descriptor, ordering,
    construct})` call (19 Mechanism files carry a `func init()`, 21 rows — `cot.go` registers 3, so
    18+3=21); all **13** ordering literals moved onto their rows with their why-comments intact (the
    review's "11" undercounted — `autofix.go` and `syntax.go` have multi-line returns); the
    `toolfilter.go` raw-`"decompose"` drift this card flagged is fixed.
  - **Item 2** (`f00cd6e`) is candidate **07** in full — see its card below.
  - **Item 3** (`b651dc3`) — the registry holds `domain.RegisteredMechanism{Descriptor, Ordering,
    Hook}` and `domain.Mechanism` is **deleted**. The card's *"methods synthesized"* deepening was
    not buildable (see the grill note above), so the metadata left the instance entirely: `Build` is
    now the single place a descriptor and a behaviour are joined. Public break as forecast —
    `apogee.Mechanism` removed, `apogee.RegisteredMechanism` added, `Add`/`Ordered` re-shaped.
  - **Items 4–5** (`26bf987`, `4803bdb`) — the 21 `Descriptor()` + 21 `Ordering()` methods are gone
    (−188 production lines in `internal/mechanisms` from item 4 alone); `libraryMechanismID` and
    the `slices.Contains` special case in `internal/agent/construct.go` are gone with them, replaced
    by `row.needs` / `mechanisms.DepsNeeded` + a `deriveDeps` helper, so the engine's build loop is
    uniform for every ID.
  - **Item 6** — ADR 0003 carries a 2026-07-25 **amendment** (clauses (2)/(3) move from *method* to
    *registration argument*, with the registry semantics preserved byte-for-byte and the Go reason
    the alternative was impossible on the record); ADR 0015 carries a dated realisation note (§3's
    instance-match parenthetical is now vacuous, §5's "stable v1 API" is read against the 0.x reset,
    §2 reaffirmed); CONTEXT.md's **Mechanism descriptor** entry says the descriptor is catalogue data
    supplied at registration; the CHANGELOG names the break in the user's terms; `TODO.md` parks the
    two non-goals (no empty-`Descriptor.ID` guard on `Add`; `internal/mechanisms` does not construct
    its Deps).
- **The line-count figure, reported straight** (plan verification step 8, which expected ~−200):
  across the whole plan `internal/mechanisms` **production** files are net **−15** lines
  (+358/−373), and the package's non-comment code lines went **3,948 → 3,943**. The reduction is real
  but concentrated where it should be: the **19 registering Mechanism files shed 95 net lines** (18
  shrank; `library.go` grew +1), while `catalogue.go` **grew +79** absorbing the shared `row` type,
  `register`, `DepNeeds`/`DepsNeeded` and their doc comments. Two things kept the total from matching the
  estimate, both deliberate: the ordering/descriptor rationale comments attached to the deleted
  methods were **moved onto the rows rather than deleted** (nothing explaining *why* an edge exists
  was allowed to be lost), and item 5 added a new declaration surface. **Not a finding** — the plan's
  own trip-wire was a net *increase*, meaning "reshaped, not collapsed" — but worth stating plainly:
  the win here is structural (one table, drift unrepresentable, no engine-side special case), not a
  line count.

### 05 — Split the Windows Confiner into three deep sub-modules · **Worth exploring · owner-flagged** · ✅ **LANDED 2026-07-25**
- **Files:** `internal/platform/winguard.go` (spelled `winconfine` until this card landed — 581
  lines at review time, **804 as of 2026-07-25**), `confiner_windows.go` (572 → **777**).
- **Problem:** two 570+ line files split by **build tag, not concern** — each carries 2–3 of
  {label journal, SDDL label-walk, notice wording, token construction}. Past one-pass navigability.
  **Both grew ~40% since the review** (nothing in 01/02 touched them — the Phase-5 follow-ups did),
  so this card is now the most degraded of the outstanding cards. `TODO.md`'s entry still quotes the
  old 581/572 figures and should be refreshed when the card is picked up.
- **Deepening:** extract the **journal** (record + atomic r/w/list + retention + revert), the
  **label-walk** (read/set/clear SDDL over a tree + reparse-skip), the **notice wording** as
  their own modules; leave the Confiner as the composer. Keep decision logic in untagged files so
  it stays table-testable on Linux (the property Phase 5 bought).
- **Note:** the owner already recorded this exact refactor in `TODO.md` ("`## internal/platform`'s
  two Windows confinement files exceed the file-size guideline", explicitly flagged for
  `/improve-codebase-architecture`, naming these three seams). Consistent with ADR 0020.
- **Shape resolved 2026-07-25** → `docs/plans/archived/2026-07-25 - 02 - windows-label-module-plan.md`
  (7 items). The card's *three sub-modules* became **one new package**,
  `internal/platform/winlabel`, with the confiner as composer. Two alternatives were rejected on
  the record: more files inside `package platform` — which is what `TODO.md` literally asked for,
  and which gets every file under ~400 lines but hides **nothing**; and two packages (journal
  untagged, walk tagged), which is not buildable as a deepening at all, because the walk and the
  journal must be **co-located** for the journal-before-label invariant to be internal to a module
  rather than a rule three call sites remember. The package is a **leaf** — standard library plus
  `golang.org/x/sys/windows`, nothing from apogee — so it returns plain errors and `labelBox`
  wraps `domain.ErrConfinementUnavailable` once at the call site, leaving every rendered message
  byte-for-byte identical and `errors.Is` true everywhere it was before.
- **LANDED 2026-07-25 — plan complete and archived.** All **seven** plan items are ✅ DONE, each
  committed on its own green gate: six code items (`9a2a074` · `e165d79` · `9d4adb4` · `6a95e8d` ·
  `7b3e593` · `c7c3b7b`) and the documentation item (`281f36d`), followed by one follow-up commit
  (`0ec4c0c`) and the archive commit (`ba570ba`). What was actually built, against the card's
  sketch:
  - **The three seams the card named are all inside `winlabel`**, as files rather than packages:
    the *journal* (`journal.go` — record, atomic write, list/siblings, `session.go` — the stateful
    `Journal`, `retire.go` — retention and revert-split), the *label walk* (`walk_windows.go`,
    with `walk_other.go` stubbing it off Windows so no Linux test can pass over a label that was
    never written), and the *notice wording* (`notice.go`). The untagged/table-testable-on-Linux
    property the card insisted on is preserved: only `walk_windows.go` carries a build tag.
  - **The deepening is the invariant, not the file split.** ADR 0020 §2's rule — the one disk
    mutation apogee performs is only ever made against a record of how to undo it — was enforced
    by three cooperating call sites in two files and ~40 lines of defensive comment. It is now
    `winlabel.LabelTree`: it reads a root's prior, records it, labels it, unwinds its own
    just-added entry if that write fails, and repeats read → record → label per descendant. The
    `rootLabelled` bool that existed only to tell `labelBox` whether to unwind is **deleted**, and
    the backend's label pass is a bare loop over the box's roots — there is no fourth call site
    that could label without journalling.
  - **The composer got genuinely thinner.** `tokenConfiner` held eight fields, five of which were
    the journal's state (`journal`, `journalHome`, `journalPath`, `labelled`, `mu`); it now holds
    four plus one `*winlabel.Journal`, and its four wrapper methods (`journalLabel`,
    `unwindRootLabel`, `flushJournal`, `restoreLabels`) are gone. The backend keeps **no** mutex:
    serialization moved inside `Journal`, which holds its lock across the whole of `LabelTree`
    (read prior → record → label → walk → mark) and of `Retire`. One deliberate narrowing versus
    the old `c.mu`: `labelBox` loops root-by-root, so the lock is taken **once per root** rather
    than once per `Confine`, and two concurrent multi-root `Confine` calls can interleave *between*
    roots. Each root's pass stays atomic and the labelled-memo is keyed per folded root, so the
    journal-before-label invariant holds — checked against the diff at close, not assumed.
  - **The guardrails deliberately did not move.** `windowsProtectedRoots`, `windowsBoxRoots`,
    `windowsLabelGuardrail` and `windowsNetworkDenyDecision` need `hostRules`, and they are the
    *host's* path rules applied to a box, not part of the mechanism they veto. They stayed in
    `platform`, which is why the old `winconfine` file was renamed `winguard.go` rather than
    emptied. The token mint went to its own `wintoken_windows.go`.
  - **Nothing observable changed** — no error string, no label, no journal byte, no walk order, no
    public API. `confiner_windows_test.go` (the real-SACL lifecycle suite) diffs as identifier
    renames only, which was the plan's acceptance oracle.
  - **Still owed:** the Windows-tagged half is *compiled* by the gate (`GOOS=windows go vet` +
    two `go test -c`) and has never been *run* on this machine. The plan's manual verification
    step 4 — a real Windows host at build ≥ 17763 — is the owner's and remains outstanding.
- **The line-count figures, reported straight.** Across the four files the card named, the total
  went **UP**, 4,254 → 5,113 lines (+859):
  - **Production: 1,581 → 1,974 (+393).** The 804-line `winconfine` file and `confiner_windows.go`
    at 777 became `confiner_windows.go` 328 + `winguard.go` 208 + `wintoken_windows.go` 101 (637 in
    `platform`) plus eight `winlabel` files totalling 1,337 (largest: `walk_windows.go` 345,
    `journal.go` 339). Every file the plan created or touched is now under the ~400-line
    guideline. The increase is doc comments **moved rather than deleted** (nothing explaining why
    a rung is tolerated was allowed to be lost), eight new file headers, and a 51-line package doc
    that did not exist before.
  - **Tests: 2,673 → 3,139 (+466)**, of which **415 are four genuinely new test files** the
    boundary made possible: `deps_test.go` (69 — the leaf-dependency guard, parsing the tagged
    files too), `journal_state_test.go` (201), `journal_race_test.go` (62 — two goroutines
    recording under `-race`, proving the concurrency claim instead of asserting it in a comment)
    and `walk_other_test.go` (83).
  - The win is a boundary and an invariant made structural, plus **two ~800-line files gone**; it
    is not a line count. Three files under `internal/platform` remain over the guideline and are
    recorded in `TODO.md` as *not* findings: `confiner_windows_test.go` (1,377, by plan decision
    D7), `host.go` (434, pre-existing and out of scope) and `winlabel/journal_test.go` (433, an
    owner call at close — recorded rather than split). That `TODO.md` entry — the one this
    card was raised against — is now closed, with its stale 581/572 figures corrected.

### 06 — Decode each engine Event once · **Worth exploring** · ✅ **LANDED 2026-07-25**
- **Files:** `internal/tui/model.go` (`foldStats`), `transcript.go` (`apply`), `activity.go`
  (`foldActivity`).
- **Problem:** one `domain.Event` folded through 3 independent type-switches in 3 files, with a
  **comment-only** ordering dependency (`foldActivity` reads `transcript.hasOpenToolCall()`, so it
  must run after `apply`). Event set grew 8→11 additively; a new variant needing 2 folders needs 2
  edits with no compiler nudge.
- **Deepening:** decode each Event once into a typed view-delta the 3 consumers read; ordering
  becomes data flow, exhaustive switch makes a missed fold a compile nudge. Strengthens ADR 0011.
- **Shape resolved 2026-07-25** →
  `docs/plans/archived/2026-07-25 - 03 - architecture-review-closeout-plan.md`, item **6**. The owner chose the
  **narrower** of the two shapes: one `foldEvent` owner (a new `internal/tui/fold.go`, taking
  `foldStats` out of the 1,772-line `model.go`) that runs the three folds in order and **passes**
  `hasOpenToolCall()` into `foldActivity` as a parameter — the ordering becomes a data dependency
  instead of a comment. The card's **typed view-delta is rejected on the record**: the three folds
  produce genuinely different things (Model scalars, transcript entries, an activity phrase), so a
  delta struct would mirror all three consumers and hide nothing. The compile nudge comes instead
  from a variant-coverage test that parses `domain/events.go` for every `EventBase`-embedding type
  (the `winlabel/deps_test.go` idiom) and fails when one is missing from the fold table.
- **LANDED 2026-07-25** (`6c1f458`, plan item 6). What was actually built, against the card's
  sketch: a new `internal/tui/fold.go` owns the fold — `foldStats` moved out of `model.go`
  (1,772 → 1,728 lines) and every engine Event enters the view through `m.foldEvent(e)`, which
  runs `foldStats → transcript.apply → foldActivity` in one place and **passes**
  `m.transcript.hasOpenToolCall()` into `foldActivity` as a parameter — the comment-only ordering
  is now a data dependency, and `foldActivity` no longer reaches into the transcript at all. The
  compile-adjacent nudge exists as planned: `fold_test.go`'s variant-coverage test parses
  `domain/events.go` for every `EventBase`-embedding type, refuses to pass over zero parsed types,
  fails when one is missing from its fold table, and drives **every** variant through `foldEvent`
  with its expected effect — or its deliberate inertness — stated per row. One discovery worth
  keeping (item 6's NOTES): `activity_test.go` already had a package-level `foldEvent` helper
  duplicating the fold with a *different* fold set (no `foldStats`); it was deleted and its 14
  call sites now go through the production owner, so no shadow fold survives to drift.

### 07 — One home for the Mechanism-stack validity rule · **Worth exploring** · ✅ **LANDED 2026-07-25**
- **Files:** `internal/domain/registry.go` (`detectIncompatibility`, `detectRequirements`),
  `internal/validated/validate.go`.
- **Problem:** the "valid Mechanism stack" invariant (IncompatibleWith-absent, Requires-present)
  is implemented twice — domain over constructed Mechanisms, validated over descriptors — two
  sites that can drift.
- **Deepening:** one shared checker over `[]MechanismDescriptor` that both call. Keep the timing
  split (validated pre-build/soft-degrade, domain post-build) — share only the rule.
- **Landed 2026-07-25** (`f00cd6e`) as item 2 of
  `docs/plans/archived/2026-07-25 - 01 - mechanism-registration-collapse-plan.md`, folded into candidate 04's
  plan because it is nearly free alongside that work — but built and committed **standalone**, so it
  stands on its own regardless of 04's remaining items. `internal/domain/stack.go` is the one home:
  `CheckStack([]MechanismDescriptor) []StackDefect` returns **structured defects**, not formatted
  errors, because the two call sites' messages differ on purpose — `domain`'s are loud startup
  errors wrapping matchable sentinels, `validated`'s are soft skip-and-warn prose. Each caller
  renders its own wording, so `registry_ordered_test.go` and `validate_test.go` passed
  **unchanged** — that was the acceptance oracle for "the rule moved, the messages did not".
  `CheckStack`'s walk order (input order; per member, `Requires` defects before `IncompatibleWith`)
  is load-bearing: it is what makes all three call sites report the same first defect they did
  before. `detectIncompatibility` kept its pre-existing `len < 2` fast path — dropping it would have
  started failing a lone Mechanism that names *itself* incompatible, a behaviour change the plan
  forbade. No root alias: `CheckStack` is an internal cross-package seam, not public surface.

## Smaller deepenings (lower leverage; see HTML for the full list)

The four entries that were still open all **landed 2026-07-25/26** as items 7–10 of
`docs/plans/archived/2026-07-25 - 03 - architecture-review-closeout-plan.md` (item 8 via that
plan's follow-on, `2026-07-26 - 00 - land-item8-close-size-window-plan.md`); each entry below
carries its ✅ note.

- **Self-regulator read model** *(Speculative, test-only)* — `selfreg.go` has no accessors; **32**
  test sites (re-counted 2026-07-25; the review said 22) poke
  `strikes`/`suppressed`/`budgetTripped`/`harmfulStreak`. Add an observed-state accessor.
  → **plan item 10**, recorded there as the plan's weakest and most droppable item.
  ✅ **LANDED 2026-07-26** (`b9578a0`): `observed()` returns a `selfRegView` **snapshot** — copied
  maps, `Suppressed` sorted for a deterministic read — and the assertion-side reach-ins migrated
  to it; the 10 arrange-side writes that seed fixtures deliberately stay direct.
- **Session store lifecycle** *(Speculative)* — ✅ **LANDED 2026-07-24**, absorbed by the session
  system (ADR 0022) rather than picked up as a deepening: `internal/session/store.go` (277 LOC) now
  owns `Save/List/Load/LoadPath/Delete/Rename` over id-addressed records. Nothing left to do.
- **`workspaceWriteTarget` helper** — marker body copied across `write_file/file_edit/find_replace`
  (✓ 4 methods in 3 files); one path-arg helper collapses them. → **plan item 7** (the *methods*
  stay — the marker is a method set; only the bodies collapse, guarded by a test that all four
  agree on the same path).
  ✅ **LANDED 2026-07-25** (`13681f4`, follow-up `c218670`): one `pathArgWriteTarget` body behind
  the four marker methods; the follow-up rebuilt the rename guard to marshal each probe call from
  the tool's **own** args struct, plus a schema/tag agreement test and a `DefaultTools` coverage
  test, because the original hand-written table would not have caught a renamed path field.
- **`read_file` → `SafeStat`/`SafeReadFile`** — the TOCTOU-safe primitive exists and is documented
  *for* read_file, but read_file still does `resolveInRoot → os.Stat → os.ReadFile`. → **plan item
  8**, deliberately widened to `open_file`, which carries the identical trio (`open_file.go`
  L65–84).
  ✅ **LANDED 2026-07-26** (`bd83326` + `c39e856`, via item 8's follow-on plan
  `2026-07-26 - 00 - land-item8-close-size-window-plan.md`, after two attempts failed verification
  on over-claiming doc sentences and the owner skipped the item): both tools — the D9 widening to
  `open_file` held — now read through the fence with the claims scoped honestly, and the
  follow-through went **past** the card: `security.SafeOpen` gives ONE pinned handle for the size
  check and the read (fstat the opened descriptor, drain it through `io.LimitReader(cap+1)`), so
  the check/use window a `SafeStat`-shaped fix would have kept is closed at all three pair call
  sites (both read tools and the agent's @ref reader), and `SafeStat` itself is **deleted** with
  zero callers. The racing component-swap probe measured **0** escapes after, against thousands
  through the old trio; an in-root symlink spelled with an absolute target is now refused (a
  narrowing the owner kept on the record).
- **POSIX `Confine` argv-wrap helper** — landlock + seatbelt share a verbatim cmd-rewrite skeleton;
  `wrapArgvUnderLauncher` + `setConfinedPgid` absorbs both. → **plan item 9**, in a `!windows`
  file (the only tag both backends compile under).
  ✅ **LANDED 2026-07-26** (`f013d4d`): `internal/platform/confine_posix.go` holds both helpers
  with the resolved-path and process-group rationale moved onto them; each backend keeps a bare
  empty-argv guard so its error *ordering* is unchanged (the message itself is now the shared
  `errNoArgv` sentinel), and the existing argv assertions in both backend tests passed unchanged.
- **`Request.InjectContext` placement** *(Speculative — reopens an ADR-0010 line)* — encodes
  chat-template role-safety policy inside a `domain` data type; the engine/`context` layer owns
  role-alternation. Flagged, **not** recommended without a grill; the current placement is
  defensible.
  **PARKED 2026-07-26** — carried into `TODO.md` (§ *`Request.InjectContext` placement*) as a
  grill brief, so this record stops being the question's only home. The verdict above is unchanged:
  the grill (`grill-with-docs`) is a future owner session, and nothing moves before it.

## State of the tree

*At the review session (2026-07-24):* clean tree at start; the session added exactly two untracked
files under `docs/reviews/` (this doc + the `.html`) and built, tested and committed nothing.

*As of 2026-07-25:* the landed candidates are committed on `main` (01: `5997ce8`…`cd39e23`;
02: `2f881f9`…`a6e39db`, incl. the follow-up `76ec91c` making a url-safety block state itself once
rather than twice; 04 + 07: `a924ef1`…`85a9f6a`; 05: `9a2a074`…`ba570ba`, incl. the follow-up
`0ec4c0c`), every landed candidate's plan doc archived. Per
the standing owner directive Apogee commits directly to `main` (pre-production).

**Candidate 04's plan is complete and archived.** All six items of
`docs/plans/archived/2026-07-25 - 01 - mechanism-registration-collapse-plan.md` are ✅ DONE, each committed on
its own green gate, and the plan doc was archived in `20110c8`. The whole-plan
verification greps all come back empty as specified: no `domain.Mechanism`, no `descriptors[`, no
`Descriptor() domain.MechanismDescriptor` / `Ordering() domain.OrderingConstraints`, no
`libraryMechanismID`, and no `IncompatibleWith`/`Requires` in `internal/validated/validate.go`. The
one item left for the owner is the plan's **manual** step 9 (build the TUI and drive one Turn with
`guided_decomposition` + `tool_result_cap`, then confirm the three loud failure paths still fail
loudly) — the automated suite pins all of it, but the plan asked for eyes on it.

**Candidate 05's plan is complete and archived.** All seven items of
`docs/plans/archived/2026-07-25 - 02 - windows-label-module-plan.md` are ✅ DONE, each committed on its
own green gate, and the plan doc was archived in `ba570ba`. Every item was verified by an
independent agent before its commit, and three follow-ups raised during the run were settled at
close (`0ec4c0c`): a stale `journalLabelEntry` comment renamed, `winlabel/journal_test.go` recorded
in `TODO.md` as a deliberate size exception, and the transitional locking gap opened when the
confiner's mutex was removed confirmed **closed** by `LabelTree`. The one item left for the owner is
the plan's **manual** step 4: the Windows-tagged half is *compiled* by the gate (`GOOS=windows go
vet` + two `go test -c`) but has never been *run* — that needs a real Windows host at build ≥ 17763.

Candidates 03 and 06 and the four remaining smaller deepenings have had **no code written for
them** — their evidence below is still review-session evidence and, apart from the ✓-marked and
2026-07-25-refreshed figures, should be spot-checked before acting.

**Re-verified 2026-07-25 as still outstanding:** `workspaceWriteTarget` (4 methods across 3 files),
`read_file` (still `resolveInRoot → os.Stat → os.ReadFile`), self-regulator accessors (none), the
POSIX `Confine` argv-wrap duplication (landlock + seatbelt; untouched by 05, which is the Windows
backend), and candidates 03 and 06 exactly as described.

*As of 2026-07-26 (close):* **every candidate and every smaller deepening is landed and the tree
is clean.** The close-out plan
(`docs/plans/archived/2026-07-25 - 03 - architecture-review-closeout-plan.md`) executed
2026-07-25/26: items 1–7, 9 and 10 each on its own green gate (`d964460` … `b9578a0`), four
follow-up fixes (`a73eed8` · `c218670` · `f45a30a` · `cfcb2a4`), and item 8 — after two failed
attempts and an owner SKIP — landed 2026-07-26 via its follow-on plan
(`2026-07-26 - 00 - land-item8-close-size-window-plan.md`) as `bd83326` + `c39e856`. Still owed to
the owner, recorded in the close-out plan's verification results: the `VERSION` minor-bump call
for the additive `ToolSummary` surface, and the manual TUI drive of the seven summary-bearing
tools.

## Recommended next step

*Originally:* grill **candidate 01** (Turn lifecycle owner) first — highest leverage, the `resolve()`
yardstick is in the same package, no ADR conflict. Then 02 and 04 as the strongest lower-risk
follow-ons ("make X follow the deep pattern the codebase already trusts"). If the owner would rather
start narrow, 05 is owner-pre-blessed and self-contained.

*As of 2026-07-25:* **01, 02, 04, 05 and 07 have all landed** (see the ledger above), and the
ledger is clean — no plan is mid-flight.

*As of 2026-07-25 (later the same day):* everything still outstanding — **03, 06 and all four
remaining smaller deepenings** — is now written up as one close-out plan,
`docs/plans/archived/2026-07-25 - 03 - architecture-review-closeout-plan.md` (10 items, to be executed with
`/implement-plan`). Nothing in it has been built yet. When it lands this review's ledger is empty
except for the two items it deliberately parks: the `/code-audit` below, and
`Request.InjectContext` (still un-grilled). The reasoning that follows is what that plan was
written against.

The outstanding cards are 03 and 06, and **03 is the strongest pick**:

- **03** (structured tool results) — the strongest remaining *Strong* on leverage: it retires the
  view's 24-entry regex registry, deepens `internal/tools`, honours ADR 0011 by construction and
  feeds a future headless/bench host. It is an additive public `ToolResult` change (ADR 0010 minor
  bump) and wanted a clean ledger to start from, which it now has. The reason to prefer 05 first
  (its degradation) is spent — 05 has landed.
- **06** (decode each Event once) is *Worth exploring* and touches the same TUI surface, so it is
  the natural follow-on rather than a competitor: 03 first gives 06 a typed delta to fold.

Also still open from candidate 02: the separate **`/code-audit`** on the *live* url-safety gap. The
shape fix landed; whether any currently-registered path reaches the network unfiltered is a
correctness question the audit answers, and it now has one place to look.

*As of 2026-07-26 (close):* **the ledger is empty — nothing from this review is left to pick up.**
All seven candidates and the four smaller deepenings are landed and their plan docs archived. The
two items this review parked are now dispositioned, which is what let this doc move to
`docs/reviews/archived/`:

- **The `/code-audit` on the *live* url-safety gap — RAN 2026-07-26.** Report:
  `docs/reviews/2026-07-26 - 00 - url-safety-live-gap-audit.md`. Verdict in one line: the hole
  candidate 02 named is **closed at the funnel** (`networkTool.do` is the single path to the
  network and the marker is genuinely unfakeable), but the Auto ladder's "url-filtered" promise is
  **defeated one rung up** — `classifyTool` consults a self-declared `ReadOnly()` before any
  unfakeable marker. 4 High + 10 Medium findings, each an open owner decision (fix now / accept /
  defer) recorded in that report, not here.
- **`Request.InjectContext` — PARKED 2026-07-26 in `TODO.md`** (§ *`Request.InjectContext`
  placement*) as a grill brief carrying the whole design, so nothing is re-derived. The verdict is
  unchanged and is *not* decided here: *Speculative*, reopens an ADR 0010 line, still not
  recommended without an owner grill (`grill-with-docs`); the current placement is defensible.

Neither item lives in this doc any more. Nothing in `docs/reviews/archived/` is anyone's to-do
list, and that now holds for this review.

## Suggested skills

- **`/improve-codebase-architecture`** — re-enter at step 3 (the grilling loop) on the chosen
  candidate. Side-effects land inline: a new CONTEXT.md term if a deepened module names a new
  concept; an ADR if the owner rejects a candidate for a load-bearing reason.
- **`grill-with-docs`** — if you want to stress-test the chosen deepening against the domain model
  and sharpen terminology before designing the interface.
- **`handoff` → plan** — a candidate that graduates to implementation must be **saved as a plan
  doc, not implemented in-session**: `docs/plans/YYYY-MM-DD - NN - slug-plan.md` in the
  `/implement-plan` house format (numbered `## N.` H2 items with What/Tests/Acceptance/commit).
  Then `/implement-plan` executes it item-by-item.
- **`/code-audit`** — for candidate 02's live url-safety gap (correctness), separate from the
  shape fix. ✅ **Ran 2026-07-26** —
  `docs/reviews/2026-07-26 - 00 - url-safety-live-gap-audit.md`; nothing left to run from here.

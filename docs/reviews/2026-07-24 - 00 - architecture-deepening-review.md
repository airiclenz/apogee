# Handoff — Architecture deepening review: 7 candidates (01, 02, 04 and 07 landed; 03, 05, 06 outstanding)

Date: 2026-07-24
Session type: **review only** (`/improve-codebase-architecture`). No code changed, no plan
written, no ADR/CONTEXT edit made. Deliverable is this handoff + the HTML report beside it.

**Companion artifact (the visuals):** `docs/reviews/2026-07-24 - 00 - architecture-deepening-review.html`
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
2026-07-25**, both under `docs/plans/2026-07-25 - 01 - mechanism-registration-collapse-plan.md` — 07
was folded into 04's plan as item 2, because a shared stack-validity checker is nearly free once the
Mechanism metadata is row-shaped, but it was built and committed standalone. Of the smaller
deepenings, **session store lifecycle landed 2026-07-24** (absorbed by the session system, ADR 0022).
**03, 05 and 06 are still outstanding and un-grilled** — for each of
those the next session's job is step 3 of the skill: pick a candidate,
walk its design tree, and land side-effects inline (CONTEXT.md term if a deepened module names a new
concept; an ADR if the owner rejects a candidate for a load-bearing reason; a saved plan doc if it
graduates to implementation — see *Suggested skills*).

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
  `confinement-execution-contract.md` §4. The separate `/code-audit` on the *live* gap is still
  worth running; the shape now gives it one place to look.

### 03 — Hand the view structured tool results · **Strong**
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
- **Grilled 2026-07-25** → `docs/plans/2026-07-25 - 01 - mechanism-registration-collapse-plan.md`
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
  but concentrated where it should be: the **21 Mechanism files shed ~96 net lines** (every one of
  them shrank), while `catalogue.go` **grew +79** absorbing the shared `row` type, `register`,
  `DepNeeds`/`DepsNeeded` and their doc comments. Two things kept the total from matching the
  estimate, both deliberate: the ordering/descriptor rationale comments attached to the deleted
  methods were **moved onto the rows rather than deleted** (nothing explaining *why* an edge exists
  was allowed to be lost), and item 5 added a new declaration surface. **Not a finding** — the plan's
  own trip-wire was a net *increase*, meaning "reshaped, not collapsed" — but worth stating plainly:
  the win here is structural (one table, drift unrepresentable, no engine-side special case), not a
  line count.

### 05 — Split the Windows Confiner into three deep sub-modules · **Worth exploring · owner-flagged**
- **Files:** `internal/platform/winconfine.go` (581 at review time — **804 as of 2026-07-25**),
  `confiner_windows.go` (572 → **777**).
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

### 06 — Decode each engine Event once · **Worth exploring**
- **Files:** `internal/tui/model.go` (`foldStats`), `transcript.go` (`apply`), `activity.go`
  (`foldActivity`).
- **Problem:** one `domain.Event` folded through 3 independent type-switches in 3 files, with a
  **comment-only** ordering dependency (`foldActivity` reads `transcript.hasOpenToolCall()`, so it
  must run after `apply`). Event set grew 8→11 additively; a new variant needing 2 folders needs 2
  edits with no compiler nudge.
- **Deepening:** decode each Event once into a typed view-delta the 3 consumers read; ordering
  becomes data flow, exhaustive switch makes a missed fold a compile nudge. Strengthens ADR 0011.

### 07 — One home for the Mechanism-stack validity rule · **Worth exploring** · ✅ **LANDED 2026-07-25**
- **Files:** `internal/domain/registry.go` (`detectIncompatibility`, `detectRequirements`),
  `internal/validated/validate.go`.
- **Problem:** the "valid Mechanism stack" invariant (IncompatibleWith-absent, Requires-present)
  is implemented twice — domain over constructed Mechanisms, validated over descriptors — two
  sites that can drift.
- **Deepening:** one shared checker over `[]MechanismDescriptor` that both call. Keep the timing
  split (validated pre-build/soft-degrade, domain post-build) — share only the rule.
- **Landed 2026-07-25** (`f00cd6e`) as item 2 of
  `docs/plans/2026-07-25 - 01 - mechanism-registration-collapse-plan.md`, folded into candidate 04's
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

- **Self-regulator read model** *(Speculative, test-only)* — `selfreg.go` has no accessors; 22
  tests poke `strikes`/`suppressed`/`budgetTripped`. Add an observed-state accessor.
- **Session store lifecycle** *(Speculative)* — ✅ **LANDED 2026-07-24**, absorbed by the session
  system (ADR 0022) rather than picked up as a deepening: `internal/session/store.go` (277 LOC) now
  owns `Save/List/Load/LoadPath/Delete/Rename` over id-addressed records. Nothing left to do.
- **`workspaceWriteTarget` helper** — marker body copied across `write_file/file_edit/find_replace`
  (✓ 4 files); one path-arg helper collapses them.
- **`read_file` → `SafeStat`/`SafeReadFile`** — the TOCTOU-safe primitive exists and is documented
  *for* read_file, but read_file still does `resolveInRoot → os.Stat → os.ReadFile`.
- **POSIX `Confine` argv-wrap helper** — landlock + seatbelt share a verbatim cmd-rewrite skeleton;
  `wrapArgvUnderLauncher` + `setConfinedPgid` absorbs both.
- **`Request.InjectContext` placement** *(Speculative — reopens an ADR-0010 line)* — encodes
  chat-template role-safety policy inside a `domain` data type; the engine/`context` layer owns
  role-alternation. Flagged, **not** recommended without a grill; the current placement is
  defensible.

## State of the tree

*At the review session (2026-07-24):* clean tree at start; the session added exactly two untracked
files under `docs/reviews/` (this doc + the `.html`) and built, tested and committed nothing.

*As of 2026-07-25:* the landed candidates are committed on `main` (01: `5997ce8`…`cd39e23`;
02: `2f881f9`…`a6e39db`, incl. the follow-up `76ec91c` making a url-safety block state itself once
rather than twice; 04 + 07: `a924ef1`…`4803bdb` plus this documentation commit), 01's and 02's plan
docs archived. Per the standing owner directive Apogee commits directly to `main` (pre-production).

**Candidate 04's plan is complete.** All six items of
`docs/plans/2026-07-25 - 01 - mechanism-registration-collapse-plan.md` are ✅ DONE, each committed on
its own green gate, and the plan doc is ready to move to `docs/plans/archived/`. The whole-plan
verification greps all come back empty as specified: no `domain.Mechanism`, no `descriptors[`, no
`Descriptor() domain.MechanismDescriptor` / `Ordering() domain.OrderingConstraints`, no
`libraryMechanismID`, and no `IncompatibleWith`/`Requires` in `internal/validated/validate.go`. The
one item left for the owner is the plan's **manual** step 9 (build the TUI and drive one Turn with
`guided_decomposition` + `tool_result_cap`, then confirm the three loud failure paths still fail
loudly) — the automated suite pins all of it, but the plan asked for eyes on it.

Candidates 03, 05 and 06 and the four remaining smaller deepenings have had **no code written for
them** — their evidence below is still review-session evidence and, apart from the ✓-marked and
2026-07-25-refreshed figures, should be spot-checked before acting.

**Re-verified 2026-07-25 as still outstanding:** `workspaceWriteTarget` (4 methods across 3 files),
`read_file` (still `resolveInRoot → os.Stat → os.ReadFile`), self-regulator accessors (none), the
POSIX `Confine` argv-wrap duplication, and candidates 03, 05 and 06 exactly as described.

## Recommended next step

*Originally:* grill **candidate 01** (Turn lifecycle owner) first — highest leverage, the `resolve()`
yardstick is in the same package, no ADR conflict. Then 02 and 04 as the strongest lower-risk
follow-ons ("make X follow the deep pattern the codebase already trusts"). If the owner would rather
start narrow, 05 is owner-pre-blessed and self-contained.

*As of 2026-07-25:* **01, 02, 04 and 07 have all landed** (see the ledger above), and the ledger is
clean — no plan is mid-flight.

The outstanding cards are 03, 05 and 06. The strongest next pick is:

- **05** (Windows Confiner split) — owner-pre-blessed in `TODO.md`, self-contained, and the only
  card that has **got worse since the review** (both files ~40% larger, and nothing since has
  touched them). `TODO.md` still quotes the stale 581/572 figures and should be refreshed when the
  card is picked up.
- **03** (structured tool results) is the strongest remaining *Strong* on leverage. It is an
  additive public `ToolResult` change (ADR 0010 minor bump) and wanted a clean ledger to start
  from — which it now has, so the only reason to prefer 05 first is 05's degradation, not
  sequencing.

Also still open from candidate 02: the separate **`/code-audit`** on the *live* url-safety gap. The
shape fix landed; whether any currently-registered path reaches the network unfiltered is a
correctness question the audit answers, and it now has one place to look.

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
  shape fix.

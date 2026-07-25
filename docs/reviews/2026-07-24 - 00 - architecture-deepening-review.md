# Handoff — Architecture deepening review: 7 candidates (01 and 02 landed; 03–07 outstanding)

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
(`docs/plans/2026-07-25 - 00 - url-safety-choke-point-plan.md`). **03–07 are still outstanding and
un-grilled** — for each of those the next session's job is step 3 of the skill: pick a candidate,
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
  domain→wire translation cluster are deep modules trapped in the god-file.

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
- **Landed 2026-07-25** via `docs/plans/2026-07-25 - 00 - url-safety-choke-point-plan.md`, as the
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

### 04 — Collapse the 21× Mechanism registration ritual · **Strong**
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

### 05 — Split the Windows Confiner into three deep sub-modules · **Worth exploring · owner-flagged**
- **Files:** `internal/platform/winconfine.go` (581), `confiner_windows.go` (572).
- **Problem:** two 570+ line files split by **build tag, not concern** — each carries 2–3 of
  {label journal, SDDL label-walk, notice wording, token construction}. Past one-pass navigability.
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

### 07 — One home for the Mechanism-stack validity rule · **Worth exploring**
- **Files:** `internal/domain/registry.go` (`detectIncompatibility`, `detectRequirements`),
  `internal/validated/validate.go`.
- **Problem:** the "valid Mechanism stack" invariant (IncompatibleWith-absent, Requires-present)
  is implemented twice — domain over constructed Mechanisms, validated over descriptors — two
  sites that can drift.
- **Deepening:** one shared checker over `[]MechanismDescriptor` that both call. Keep the timing
  split (validated pre-build/soft-degrade, domain post-build) — share only the rule.

## Smaller deepenings (lower leverage; see HTML for the full list)

- **Self-regulator read model** *(Speculative, test-only)* — `selfreg.go` has no accessors; 22
  tests poke `strikes`/`suppressed`/`budgetTripped`. Add an observed-state accessor.
- **Session store lifecycle** *(Speculative)* — `internal/session` (72 LOC) is write-only `Save`;
  read path lives in `domain.DecodeSession` + a caller's `ReadFile`. Give it `Save/List/Load`.
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

Clean working tree at session start; this session added exactly two untracked files under
`docs/reviews/` (this doc + the `.html`). Nothing built, tested, or committed. Per the standing
owner directive Apogee commits directly to `main` (pre-production) — but committing these docs is
the owner's call; they are not yet staged.

## Recommended next step

*Originally:* grill **candidate 01** (Turn lifecycle owner) first — highest leverage, the `resolve()`
yardstick is in the same package, no ADR conflict. Then 02 and 04 as the strongest lower-risk
follow-ons ("make X follow the deep pattern the codebase already trusts"). If the owner would rather
start narrow, 05 is owner-pre-blessed and self-contained.

*As of 2026-07-25:* 01 and 02 have landed (see the ledger above), so the next pick is **04**
(the Mechanism registration ritual), with 05 as the narrow self-contained alternative.

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

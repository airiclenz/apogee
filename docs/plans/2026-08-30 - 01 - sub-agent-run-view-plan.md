# Sub-agent run view — open a delegation full-screen, walk back up, message a running child

**Goal.** Expanding a delegation opens it as a **run view** that takes the whole transcript slot (status line, prompt box and footer stay), follows the latest output, and carries a clickable breadcrumb / `esc` back up one level. Inside the view the prompt box addresses that child: a message queues into an engine-side per-child mailbox and lands at the child's next between-Steps boundary as an interjection. The activity line tracks one slot per run, so the top level stops flickering between concurrent children.

**Date:** 2026-08-30 · **Status:** unexecuted · **sized for:** ~200k-context host

**Authoritative sources:** `IDEAS.md:14,18` · `docs/adr/0013-…recursion-point…md` (D5: child runs atomically inside the parent Turn — unchanged) · `docs/adr/0025-*` (interjections commit between Steps) · `docs/adr/0031-*` (wire-silent engine, benchable all the way up) · `docs/adr/0035-*` (alternate-screen takeover rejected) · `docs/adr/0039-*` (spawn call-ID identifies a child's stream) · `docs/adr/0005-*` (child privileges ≤ parent) · `layout.md:109-230,742-1000` · `docs/layout/tool-layout.md:96-110,207-230` · `docs/design/test-drivers.md:709-800` · base commit `595b2f10`.

**Ratified design calls** (owner, 2026-08-30):
- **Scope:** running children only. A finished or scheduled child opens read-only; child persistence / prompting a finished child stays the `ISSUES.md:197` non-goal.
- **Entry:** expand = open the run view. The inline expanded sub-agent shape (`expandedSubAgentView`) is removed; a run has two shapes: collapsed row ↔ run view. The `✦ Sub-Agent (N)` umbrella still expands inline to list its members (collapsed rows).
- **Up:** `esc` goes one level up, plus a clickable breadcrumb header row `← main › <name>`. Inside a view `esc×2` no longer stops the run — back out first (3 presses), the status right slot says `esc back`.
- **Status line:** one activity slot per run; top level shows `N sub-agents · working` while ≥2 children act, the run view shows its child's own slot (closes `IDEAS.md:18`).
- **Stop:** whole-run only (`esc×2` at top level); per-child stop deferred (own grill).
- **Parent notice:** a child that received user messages returns its result with the trailer `(the user sent N message(s) to this sub-agent while it ran)`.
- **ADR:** yes — item 1 records the decision as ADR 0063.
- **Engine seam (ADR 0031):** the mailbox lives in `internal/agent` (`Agent.InterjectChild`), drained by the child's own Step-driving goroutine; the TUI only addresses it. No Driver-only shortcut.
- **Double print (ISSUES.md:40):** folded in — closed by item 8 (owner, 2026-08-30).

**Regression check (2026-08-30, 595b2f10):** three independent reviewers against base `595b2f10`; no item SAFE.
- 1: guard folded — no ADR index exists; the "mirror the 0062 sites" clause and its count-equality acceptance are dropped; ADR 0063 explicitly supersedes ADR 0025's rejected Run-drain / interjection-Event section (`docs/adr/0025-*.md:204-210`) for child agents only.
- 2: guard folded — drain and `ChildInterjectionEvent` are depth > 0 only; supersedes `docs/adr/0025-*.md:208` for children by name (item 1); `Run`'s doc (`agent.go:531-533`) rewritten; test (b) observes through a request-recording responder.
- 3: guard folded — the delegation result is clamped in `appendToolResult` after `runSubAgent` returns (line elision, tail kept), the trailer survives by shape; human-visible in the report row; the rule is "every result that is not `dispatchCancelled`".
- 4: guard folded — entries commit through `transcript.place`; depth > 0 `addUserAt` entries register no sticky `userBlock` (yields to the sticky-header rule at `transcript.go:614-619`); the not-landed notice is a transcript note; `cmd/apogee/wire_engine.go` forwards `InterjectChild`.
- 5: guard folded — stopping is run-wide and a depth-0 event drops every child slot (ADR 0013 D5); yields to the sticky "stopping" of `activity.go:264-266` / `model.go:1704-1706`.
- 6: guard folded — a rooted paint collects no `userBlock`; the breadcrumb is the only sticky header inside a view; `internal/tui/model.go` joins Files.
- 7: guard folded — `openRun` exits the block cursor; item 7 owns the cmd/apogee follow-through (`openLastRun`, retargeted e2e tests, t04 golden); `keyclaim_test.go` want list; the claimant stays out of the ask/approval states (yields to `approval.go:41-43`); one reset rule for the stack; the two named existing tests stay as they are.
- 8: guard folded — the retire grep covers every `internal/tui/*_test.go`; refuse/redirect on `span > 0 || !head.done`; `doc.go` reworded; `collapsedSubAgentView` inlined.
- 9: guard folded — only the `stateIdle`/`stateRunning` arms of `enter` consult the viewed child; `stageChildMessage` keeps the command / unknown-slash branches.
- 10: guard folded — scene (1) uses `openLastRun`; the parallel cap is pinned; goldens use `goldenRedactions(sess)`.
- 11: guard folded — the acceptance grep is scoped to the doc targets; the guard rule covers every sentence or sketch that draws the delegation frame.
- 8 (second pass, owner decision): recast — the item is a FIX closing `ISSUES.md:40` (double print): the early, badly formatted copy IS `expandedSubAgentView` painting the delegation's tool-result body above `subAgentPromptRows`; bite test on `setExpanded` + rooted-paint once-only test added; commit reworded to `fix(tui)`.
- 11 (second pass, owner decision): `ISSUES.md` joins Files — the `## Open defects` entry at `:40` is removed (closed by item 8; the closeout records it in CHANGELOG); acceptance gains the absence grep.
- 8 (re-check round): guard folded — `renderSubAgentGroup` (`subagentblock.go:383-384`, `:398-400`) and `renderSubAgentMemberRows` (`:521-532`) lose their `spanned && expanded` arms with `expandedSubAgentView`; `blocktarget_test.go` joins Files (its "expanded sub-agent head" case rewritten to the collapsed row's mark or deleted); item 8 owns the rewrite of `TestPerChildBlockExpandsAlone` against item 6's rooted paint (item 7's "stays as it is" holds only through item 7).

**Standing requirements:** `skills: coding-standards` · deviations land as dated NOTES lines under the item · no version identifier changes (closing note only).

**Out of scope:** child conversation persistence / resuming a finished child · per-child stop · auto-naming children (`IDEAS.md:16`) · headless/daemon addressing of children (the engine seam is enough for them; no CLI surface) · grouping/ordering changes to the `✦ Sub-Agent (N)` umbrella · `ISSUES.md:85-95` (never-ran ▶) beyond what item 8 touches.

---

## 1. ADR 0063 — sub-agent runs are user-addressable views — ✅ DONE (2026-08-30)

NOTES (2026-08-30): none — the item's regression guard was already folded into its What (no ADR index exists, so no index clause or index grep); the ADR names `docs/adr/0025-…:204-210` in both its Decision and its Consequences and scopes the supersession to depth > 0.

**What.** Write `docs/adr/0063-sub-agent-runs-are-user-addressable-views.md` (format of `docs/adr/0062-test-drivers-are-drivers.md`: `Status: accepted` frontmatter, Context / Decision / Consequences). Decisions to record, each as a numbered D-line: (D1) a child is addressed by its spawn call-ID through `Agent.InterjectChild(spawnCallID, domain.UserInput) error`, an engine-side mailbox drained by the goroutine driving the child's Steps — this supersedes ADR 0025's rejected Run-drain / interjection-Event section (`docs/adr/0025-*.md:204-210`) for child agents (depth > 0) only, named in Decision and Consequences; the top-level contract stands; ADR 0013 D5 unchanged (parent Turn still atomic, cancel still rolls the whole Turn back, nothing new persists). (D2) `domain.ErrNoSuchChild` names a child that is not running; the engine emits `domain.ChildInterjectionEvent{Landed}` for every queued message so any Driver can paint delivery honestly. (D3) a steered child's result carries the parent-notice trailer. (D4) the run view is a transcript-slot takeover like `/settings` (`frameRowPlan` reserve = 0), never an alternate screen — ADR 0035 stands; `esc` and the breadcrumb go up one level; the view is Driver state, never persisted. (D5) expand on a framed delegation opens the view; `layout.md:797` "nothing expands by itself" is amended: a run view opens at its latest line — it is a view, not a fold state, so `tool-layout.md:98` "two states per call" keeps holding for the row. (D6) privileges: a user message to a child is a plain interjection; tool set, mode, confinement stay the child's (ADR 0005).

**Regression guard.** No ADR index exists in the tree — drop the "add to whatever index / mirror the 0062 sites" clause and its Acceptance grep. ADR 0063 EXPLICITLY SUPERSEDES ADR 0025's section at docs/adr/0025-*.md:204-210 (Run-side drain rejected; no interjection Event) FOR CHILD AGENTS (depth > 0) ONLY — the top-level contract (Driver drains between Steps, no event) stands; the ADR names that section in its Decision and Consequences.

**Files:** `docs/adr/0063-sub-agent-runs-are-user-addressable-views.md`.

**Tests.** None (docs).

**Acceptance.** `test -f "docs/adr/0063-sub-agent-runs-are-user-addressable-views.md"` · `grep -c '^(D[1-6])\|D[1-6]' docs/adr/0063-*.md` ≥ 6.

**Commit.** `docs(adr): 0063 — sub-agent runs are user-addressable views`

## 2. Engine: child registry, `InterjectChild`, mailbox drain, delivery event — ✅ DONE (2026-08-30)

NOTES (2026-08-30): consequential edit — internal/agent/doc.go: made necessary by adding internal/agent/children.go; TestDocMapNamesEveryFile (docmap_test.go) fails when a non-test file is missing from doc.go's file map, so children.go joins the "mid-session doors" paragraph.

NOTES (2026-08-30): consequential edit — internal/agent/subagent_test.go: made necessary by adding domain.ChildInterjectionEvent; eventBaseOf enumerates every Event variant and t.Fatals with "teach it the new variant" on an unknown one, so the new variant gains its case.

NOTES (2026-08-30): the mailbox CLOSES when the child's run ends (childMailbox.close, called from runSubAgent's defer beside the unregister) and InterjectChild answers ErrNoSuchChild for a closed one. Not in the item's literal text; it is what makes D2's "one event per queued message" hold — without it, a message added in the window between a successful registry lookup and the flush would sit in a mailbox nothing drains, with no event to account for it.

NOTES (2026-08-30): the drain runs AFTER Run's step-cap check, so a boundary that ends the Exchange is not treated as a delivery point; whatever stays queued is reported Landed:false by runSubAgent's defer.

NOTES (2026-08-30): item 3's `steered` counter is deliberately NOT added here — item 2's What never names it and an unused field is dead code. Item 3 adds `steered int` to Agent and one increment beside the `Landed: true` emit in `(*Agent).drainMailbox` (internal/agent/children.go), which therefore joins item 3's Files.

NOTES (2026-08-30): the request-recording responder the item names is `requestLogResponder` in children_test.go — `recordingResponder` was already taken by harness_test.go:54.

**What.** In `internal/domain`: add `ErrNoSuchChild = errors.New("no running sub-agent with that call-ID")` beside `ErrNoOpenExchange`, and `ChildInterjectionEvent{EventBase; Input UserInput; Landed bool}` in `internal/domain/events.go` next to `SubAgentPhaseEvent` (`:141`) — `EventBase.Depth` = the child's depth, `CallID` = the spawn call-ID, exactly as the child's other events. In `internal/agent`: a `childRegistry` (mutex-guarded `map[string]*Agent`, spawn call-ID → child) on `Agent`; `runSubAgent` (`internal/agent/subagent.go:98`) registers `sub` under `call.ID` before `sub.Run(ctx)` (`:136`) and unregisters in the same defer that `Close`s it (`:131`). Each `Agent` gets a `mailbox` (mutex-guarded `[]domain.UserInput`); `(*Agent).InterjectChild(spawnCallID string, in domain.UserInput) error` appends to the named child's mailbox when it is registered on this agent, else recurses into every registered child (grandchildren, depth 2), else returns `ErrNoSuchChild`. Non-blocking, safe from any goroutine — the TUI calls it from its program goroutine, as `AbortExchange` already is. Drain: in `(*Agent).Run` (`internal/agent/agent.go:540`), before every Step after the first, the driving goroutine pops the mailbox in order and calls `a.Interject(in)` (`internal/agent/interject.go:51` — legal here: same goroutine, between Steps); each delivered input emits `ChildInterjectionEvent{Landed: true}` through the shared sink; an `Interject` error stops the drain the way `deliverInterjections` does (`internal/tui/worker.go:202-210`) and emits `Landed: false` for the rest. On `runSubAgent` return, anything still in the mailbox is emitted `Landed: false`. The drain and the `ChildInterjectionEvent` apply only to agents with depth > 0: a top-level `Run` performs no drain and emits no such event. Top-level `Interject` behaviour and `ErrInputPending` rules are untouched. Standards: the registry and mailbox are two small types with their own files (`internal/agent/children.go`), not fields folded into `agent.go`; no goroutine is spawned.

**Regression guard.** State in What that the mailbox drain in `Run` and the `ChildInterjectionEvent` apply only to agents with depth > 0 (a top-level `Run` performs no drain and emits no such event) — this is the ADR 0025 supersession item 1 records. That supersedes `docs/adr/0025-*.md:208` (Run-drain rejected) for children only, by name in ADR 0063 D1; `Run`'s doc at `internal/agent/agent.go:531-533` ("no seam to interject at") is rewritten in this commit to say a child's `Run` drains its mailbox between Steps. Test (b): `scriptedResponder` discards the request (`internal/agent/statemachine_test.go:34`) and `sub` is local to `runSubAgent` (`subagent.go:121`), so use a request-recording responder (pattern: `internal/agent/autocompact_guard_test.go:37-45`), queue via `a.InterjectChild("c1", …)` from inside the responder's `Stream` of the child's FIRST Turn (the only window a synchronous `a.Run` leaves; the registry is populated before `sub.Run`), and assert the child's second request ends tool results → the queued user message.

**Files:** `internal/domain/errors.go` (or wherever `ErrNoOpenExchange` lives), `internal/domain/events.go`, `internal/agent/children.go`, `internal/agent/children_test.go`, `internal/agent/agent.go`, `internal/agent/subagent.go`.

**Tests.** `internal/agent/children_test.go`: (a) `InterjectChild` on an unknown id → `ErrNoSuchChild`; (b) a scripted child driven by a request-recording responder (pattern `autocompact_guard_test.go:37-45`) that runs ≥2 Steps: a message queued via `InterjectChild` from inside the first Turn's `Stream` appears as the last message of the child's second request (`RoleUser`, `Interjected: true`, after the tool results) and a `Landed: true` event with the child's depth + spawn id is seen on the sink; a top-level agent's `Run` emits no `ChildInterjectionEvent`; (c) a message queued after the child's last Step is emitted `Landed: false`; (d) a grandchild is reachable through the top-level agent; (e) `go test -race` on the package.

**Acceptance.** `go build ./...` · `go test -race ./internal/agent/ ./internal/domain/` · `go vet ./internal/agent/`.

**Commit.** `feat(agent): children are addressable — InterjectChild queues into a per-child mailbox drained between the child's Steps`

## 3. Engine: parent-notice trailer on a steered child's result — ✅ DONE (2026-08-30)

NOTES (2026-08-30): consequential edit — internal/agent/agent.go: made necessary by the `steered` counter the item specifies "on the child" — a Go struct field can only be declared on the Agent struct, which lives here.

NOTES (2026-08-30): consequential edit — internal/agent/children.go: made necessary by the item's own "incremented by the drain of item 2" — that drain (drainMailbox) lives here, so the increment does too.

NOTES (2026-08-30): the ONE append site is reached by extracting runSubAgent's post-Run outcome switch into `(*Agent).delegationResult` (same file, same behaviour) rather than by rewriting the switch in place: it keeps runSubAgent short and makes the two outcomes that cannot be scripted through `a.Run` — the loop-level Run error and the cancelled dispatch — directly testable, which the item's test list requires.

NOTES (2026-08-30): the item cites the trailer as ADR 0063 D5; the ratified ADR records it as **D3** (D5 is the run view). Code comments cite D3.

**What.** Depends on item 2. In `internal/agent/subagent.go`, the child counts landed interjections (`steered int` on the child, incremented by the drain of item 2). In `runSubAgent`, for every result that is not `dispatchCancelled` — success (`:187`), step-capped (`stepCapResultFormat`), faulted (`subAgentFaultPrefix`) and the Run-error `sub-agent failed:` path (`:143-148`) — at ONE site after the outcome switch, append `"\n\n" + userSteeredTrailer(n)` to `ToolResult.Content` when `n > 0`, where `userSteeredTrailer` renders exactly `(the user sent 1 message to this sub-agent while it ran)` / `(the user sent 2 messages to this sub-agent while it ran)`. The trailer is the result's final line: the structural clamp (`clampToolResult` in `appendToolResult`, `dispatch.go:1106`, after `runSubAgent` returns) is head/tail LINE elision with the tail kept, so it survives by shape — say so in a comment at the append site. `headless` stderr line (`docs/manual/headless.md:36-43`) is unchanged.

**Regression guard.** The only clamp on a delegation result is `clampToolResult` inside `appendToolResult` (`dispatch.go:1106`), which runs AFTER `runSubAgent` returns (serial path `:337` and `commitDelegation`); `clampToBound` (`:1155`) is that clamp's rendering, not an earlier pass — a trailer appended in `runSubAgent` is clamped WITH the body, so no "after the clamp" ordering exists or is documented. The clamp is head/tail line elision, tail kept (`internal/context/toolresult.go:41`), so the final-line trailer survives by shape: test it through the committed tool message / `ToolResultEvent` after `a.Run` with a child answer over `structuralFloor` (`:1141`), never on `runSubAgent`'s return value. The trailer is human-visible: the TUI paints the report body from the same Content (`internal/tui/toolregistry.go:418-422`, `outputDetail`), and a one-line child answer loses the single-line promotion (`:1338`) once `"\n\n"+trailer` is appended — accepted; item 10's finished-view golden (t18) pins it. The rule is "every result that is not `dispatchCancelled`", applied once after the outcome switch, so the Run-error `sub-agent failed: <err>` path (`subagent.go:143-148`) carries it too.

**Files:** `internal/agent/subagent.go`, `internal/agent/subagent_test.go`.

**Tests.** `internal/agent/subagent_test.go`: a steered child's result ends with the exact singular and plural trailer; an unsteered child's result is byte-identical to today's; a cancelled child carries no trailer; a Run-error (`sub-agent failed:`) result carries it; with a child answer over `structuralFloor`, the tool message committed by `a.Run` (or the `ToolResultEvent`) still ends with the trailer.

**Acceptance.** `go test -race ./internal/agent/ -run 'Steer|Trailer|SubAgent'` · `go vet ./internal/agent/`.

**Commit.** `feat(agent): a steered sub-agent's result tells the parent how many user messages it received`

## 4. TUI engine seam: `InterjectChild` on `tui.Engine`, fold the delivery event — ✅ DONE (2026-08-30)

NOTES (2026-08-30): the `Engine` doubles the grep found are exactly two — `fakeEngine` (`internal/tui/seam_test.go`) and the PRODUCTION `lateEngine` (`cmd/apogee/wire_engine.go`); `*apogee.Agent` already carried `InterjectChild` from item 2, and no other type in the repo satisfies `tui.Engine`.

NOTES (2026-08-30): `internal/tui/transcriptcodec.go` needed no change and is not in FILES — `toWireEntry`/`fromWireEntry` already carry `Depth` and `SpawnCallID` for every kind, so only the round-trip case was added (the item's own text says as much).

NOTES (2026-08-30): consequential edit — internal/tui/fold_test.go: made necessary by the new `apply` case; `TestFoldEventCoversEveryEventVariant` was already failing at HEAD because item 2 added `domain.ChildInterjectionEvent` with no `foldCases` row, and this item is where the row is owed (both fates: landed → one entry, not landed → one note).

NOTES (2026-08-30): the `apply` doc comment's variant arithmetic ("nine … of the twelve-variant Event set") was falsified by the new case and was already stale by one (`WireEvent`); it now reads "ten … of the fourteen-variant Event set" with `WireEvent` named in the inert list.

NOTES (2026-08-30): a run whose span holds only a delivered message reads as RUNNING rather than queued ("0 tool calls"), because `subAgentScheduled` yields to `subAgentFramed` the moment a run has a span. Not reachable as a wrong reading through the engine: `runSubAgent` registers a child in the mailbox registry only once it is actually driving it, so a delegation still queued behind the Parallel-agents cap refuses `InterjectChild` with `ErrNoSuchChild` and never gets a span this way. Pinned in the collapsed-run subtest.

NOTES (2026-08-30): `addUserAt` carries no `skillSpans` — the delivery event carries a bare `domain.UserInput` and the staged rows that hold the spans are item 9's state. A skill attached to a child message will highlight once item 9 wires the staging.

**What.** Depends on item 2. Add `InterjectChild(spawnCallID string, in domain.UserInput) error` to the `Engine` interface (`internal/tui/tui.go:580-640`) with a doc comment stating it is the one engine call legal from the program goroutine besides `AbortExchange` (non-blocking enqueue). Update every test double implementing `Engine` (grep `func (.*) Interject(` under `internal/tui/` and `cmd/apogee/`; enumerate in a NOTES line). Fold `domain.ChildInterjectionEvent` where `SubAgentPhaseEvent` is folded today (grep `SubAgentPhaseEvent` in `internal/tui/`): `Landed: true` → `transcript.addUserAt(depth, spawn, in)` — a new `transcript` method that commits, through `transcript.place`, an `entryUser` entry carrying `depth` and `spawnCallID` so it paints inside the run and regroups on replay (`transcriptcodec` already persists both fields; add a round-trip case) — and clears the matching staged child message (item 9's state; until then, no-op); `Landed: false` → a transcript note `<name> finished before your message landed` via `transcript.addNote` (the one `/undo` and refusals use). The `Bridge`/event sink needs no change: the event rides the shared sink.

**Regression guard.** `addUserAt` entries (depth > 0) never register a sticky `userBlock` (render.go:152, :550) — sticky user headers are top-level prompts only. The sticky-header rule at `internal/tui/transcript.go:614-619` (a delivered mid-Exchange remark never becomes the sticky header) stands; the depth exemption is how this item keeps it. The entry is committed through `transcript.place` (`transcript.go:367`) like every delegated entry — it lands at `runEnd(spawn)` and drops the paint cache from there — never appended: with two siblings live, an appended entry lands after the LAST run's stretch and `subAgentSpan` (`subagentblock.go:27-35`) attributes it to the sibling. The `Landed: false` notice is a transcript note via `transcript.addNote` (`undo.go:91`, `commandrun.go:30`; `addEphemeralNote` if it must not persist) — no "transient status notice" path exists (`m.flash`, `model.go:406`, is the mouse-copy flash). `lateEngine` (`cmd/apogee/wire_engine.go:212`) is the PRODUCTION `tui.Engine` wrapper, not a double: it forwards `InterjectChild` and answers `errNoServerBound` unbound, as every other forwarded method does.

**Files:** `internal/tui/tui.go`, `internal/tui/transcript.go`, `internal/tui/render.go`, `internal/tui/transcriptcodec.go`, `internal/tui/transcriptcodec_test.go`, `internal/tui/transcript_test.go`, `cmd/apogee/wire_engine.go`, `cmd/apogee/wire_engine_test.go`, every `Engine` double under `internal/tui/` and `cmd/apogee/` (grep), the fold site file (grep).

**Tests.** `internal/tui/transcript_test.go`: `addUserAt` entry lands with depth/spawn and paints inside the run (collapsed run → zero rows, per `render.go:466-468`); with two live siblings the entry lands inside its own run (`place`, not append); a depth > 0 user entry registers no `userBlock` (ctrl+↑/↓ stops unchanged); codec round-trip keeps depth+spawn for a user entry; fold of `Landed:false` adds a transcript note with the exact string. `cmd/apogee/wire_engine_test.go`: `InterjectChild` unbound → `errNoServerBound`.

**Acceptance.** `go build ./...` · `go test ./internal/tui/ -run 'Transcript|Codec|ChildInterjection'` · `go vet ./internal/tui/`.

**Commit.** `feat(tui): the engine seam addresses children — InterjectChild on Engine, delivery events fold into the run`

## 5. Per-run activity slots and the merged top-level phrase — ✅ DONE (2026-08-30)

NOTES (2026-08-30): `.act` producers/consumers moved — `activity.go` (`moveActivity` clock inheritance and write, `foldActivity`'s sticky-stopping guard, `setActivity`, `setToolActivity`), `model.go` (`stopWorker` :1711, `finishWorker`'s reset :1757, `statusLine`'s quiet gate :3146, `runningPhrase`'s clock and phrase :3203-3204), `commandrun.go` (4 `setActivity` calls: :91, :293, :311, :426); tests in `activity_test.go`, `fold_test.go`, `model_test.go`, `interject_test.go`.

NOTES (2026-08-30): `setToolActivity` was DELETED rather than moved — its only caller was `foldActivity`'s `ToolCallEvent` arm, which now writes through the new `foldSlot`, so keeping it would have left a one-caller pass-through. Its clock-key rule is stated at that arm and in `moveActivity`.

NOTES (2026-08-30): the depth-0 drop lives in `foldSlot` (the Event fold's write seat) and not in `moveActivity`, so the transitions no Event announces — submit, /compact, and the stop — drop nothing. That is what keeps the child slots standing while "stopping" is on the row, exactly as regression guard (a) describes; the drop therefore fires on depth-0 events that MOVE an activity rather than on every depth-0 event, which is the set `foldActivity` can name a run for.

NOTES (2026-08-30): `Model.lastEvent` was kept as the engine-wide stall clock and the top-level slot reads it, because every Event variant restamps it — including the ones no fold acts on — so the parent's guard keeps exactly the reach it had. A delegate slot reads its own `lastEvent` (stamped in `moveActivity`), which is the per-run half the item asks for: a busy sibling is no longer evidence that a quiet delegate is alive (`Model.quietClock`).

NOTES (2026-08-30): test spelling only — `m.act` readers now read through a new `shownAct(m)` helper (the top-level slot the row would render) and `silentFor`/the streamed-turn loop backdate through `backdateActivity`. `TestStatusPhraseDropsTheNameWhenTheParentResumes` and `TestFoldActivityDepthPrefixesSubAgent` keep their assertions unedited, as the regression guard requires.

NOTES (2026-08-30): consequential edit — internal/tui/doc.go: made necessary by the single `Model.act` slot becoming the per-run `Model.acts` board (the package narrative described one phrase per session).

NOTES (2026-08-30): consequential edit — internal/tui/commandrun.go: made necessary by the `setActivity` signature taking the run it writes.

NOTES (2026-08-30): consequential edit — internal/tui/interject_test.go: made necessary by the same `setActivity` signature change.

NOTES (2026-08-30): `IDEAS.md:18` is deliberately left standing — item 11 owns the doc sweep.

**What.** Closes `IDEAS.md:18`. Replace the single `Model.act activity` (`internal/tui/activity.go:49`, written by `foldActivity` `:276` / `setActivity` `:209` / `setToolActivity` `:217` / `moveActivity` `:239`) with `Model.acts runActivities` — a small type in `activity.go` keyed by spawn call-ID (`""` = top level) holding one `activity` per run plus its own `since`/`lastEvent`. `foldActivity` writes the slot named by `e.EventBase.CallID`+`Depth` (`spawn` from the event as today); `SubAgentFinished` deletes the child's slot. `Model.runningPhrase` (`model.go:3202`) composes from a `runRef` argument — the viewed run (item 7; top level until then): top level with exactly one live child → today's `<name> · <phrase>`; ≥2 live children → exactly `N sub-agents · working` with the elapsed clock of the oldest live child slot; no live child → the top-level slot as today. The stall qualifier (`activity.quiet`, `quietQualifier` `model.go:3219`) evaluates the slot being shown. Producers/consumers of `m.act` (grep `\.act\b` in `internal/tui/`) are all moved; enumerate in a NOTES line. `subAgentActivityName` fallback stays.

**Regression guard.** (a) stopping is run-wide — while the top-level slot is actStopping, `foldActivity` returns early for EVERY slot and `runningPhrase` renders "stopping" whatever the child slots hold (keeps the sticky "stopping" of activity.go:264-266 / model.go:1704-1706); (b) a depth-0 event drops every child slot — ADR 0013 D5 (a child runs atomically inside the parent's Turn, so a parent event means its children are over); phase events are stamped at the child's depth (internal/agent/dispatch.go:349 `base.Depth++`), so a sibling's SubAgentStarted never drops another child's slot; this keeps TestStatusPhraseDropsTheNameWhenTheParentResumes (activity_test.go:467) and TestFoldActivityDepthPrefixesSubAgent (:400) green unedited — the Tests line "existing tests keep passing" now holds. The item yields to the sticky-"stopping" rule at `internal/tui/activity.go:264-266` / `model.go:1704-1706` (stopping stays until `finishWorker`).

**Files:** `internal/tui/activity.go`, `internal/tui/activity_test.go`, `internal/tui/model.go`, any other `m.act` reader found by the grep.

**Tests.** `activity_test.go`: two children alternating events keep two slots and the top-level phrase reads exactly `2 sub-agents · working` without changing between the events; one child → `<name> · <phrase>` unchanged (existing tests keep passing); `SubAgentFinished` drops the slot and the phrase falls back; a depth-0 event after a child's event drops the child's slot while a sibling's `SubAgentStarted` does not; esc×2 while a child works: the phrase reads `stopping` and a later child event does not replace it; the clock does not restart when the other child emits.

**Acceptance.** `go test ./internal/tui/ -run 'Activity|Status|RunningPhrase'` · `go vet ./internal/tui/`.

**Commit.** `feat(tui): one activity slot per run — concurrent children no longer flicker the status line`

## 6. Render: paint a transcript rooted at one run, with a breadcrumb header — ✅ DONE (2026-08-30)

NOTES (2026-08-30): the breadcrumb names each run through `usageAgentName` (the call's name, else the task's first line, else "sub-agent") rather than `runName` alone, so an unnamed delegation is not a hole in the trail; "main" is `usageMainLabel`, one word for one thing across the two surfaces.

NOTES (2026-08-30): the root's depth is read off the head the paint just resolved (`head.depth+1`) rather than off the `runRef`'s own `depth` field; the two agree by construction (`transcript.closeRun`), and the head is the half a paint has in front of it.

NOTES (2026-08-30): a rooted paint opens with the run's TASK painted as a plain user row (the item's "shows its task as the first user row"), read off the head that is no longer painted; it registers no `userBlock`, per the item's regression guard.

NOTES (2026-08-30): consequential edit — internal/tui/paintcache_test.go: `coldRender`'s oracle transcript now copies `root`, made necessary by the paint now depending on it.

**What.** In `internal/tui/transcript.go` add `root runRef` (zero = whole transcript) and `func (t *transcript) setRoot(r runRef)`. `render` (`internal/tui/render.go`, block resolution `:373`) restricted to a root paints ONLY the root's head entry plus its span (`subAgentSpan`, `subagentblock.go:27`), with every row's depth shifted by the root's depth so the child's entries paint as top-level rows (no rail); the head itself is not painted as a row — it becomes the **sticky header** `← main › <name>` (`transcript.runName(spawn)`, `transcript.go:488`; nested roots chain names: `← main › planner › repo-scout`), the right slot reads `esc back`, ending two columns short like the status right slot (`layout.md:1173`). Rows inside the root: nested delegations paint collapsed exactly as at top level (their spans skipped), `insideCollapsedRun` (`subagentblock.go:83`) is evaluated relative to the root so the root's own live tail paints; the block cursor's stops (`blockcursor.go:49`) and `lineTargets` derive from the same rooted paint (`layout.md:117` one derivation). The paint cache (`paintcache.go`) keys on the root. Sticky header height counts against the transcript rows exactly as the existing sticky header does. Header row gets a `lineTarget` of a new kind `targetBreadcrumb` (item 7 handles the click).

**Regression guard.** In a rooted paint no `userBlock` is collected at all — the breadcrumb is the ONLY sticky header inside a view (the child's task row and landed child messages paint as plain user rows); settle the item-4/item-6 collision with these two sentences. The sticky header is a Model overlay (`internal/tui/model.go:2563` `stickyHeaderSpan`, `:2629` `applyStickyHeader`) over the `m.userBlocks` that `render.go:152` registers, not a render.go row: the rooted paint hands the overlay the breadcrumb (a `header` on the rendered transcript it draws first) and registers no `userBlock`; `internal/tui/model.go` joins Files.

**Files:** `internal/tui/transcript.go`, `internal/tui/render.go`, `internal/tui/model.go`, `internal/tui/subagentblock.go`, `internal/tui/paintcache.go`, `internal/tui/blocktarget.go`, `internal/tui/render_test.go`, `internal/tui/subagentblock_test.go`, `internal/tui/paintcache_test.go`.

**Tests.** `render_test.go`: a rooted paint of a running child shows its task as the first user row, its tool calls at depth 0, its streaming tail, and nothing from the parent or siblings; header text is exactly `← main › repo-scout` with `esc back` right-aligned and is drawn by the sticky-header overlay; a rooted paint registers zero `userBlock`s (the child's task row is a plain user row); a nested run inside the root paints as a collapsed row; root `runRef{}` paints byte-identically to today (golden-style compare against the unrooted paint); paint cache invalidates on `setRoot`.

**Acceptance.** `go test ./internal/tui/ -run 'Render|Root|Breadcrumb|PaintCache|SubAgent'` · `go vet ./internal/tui/`.

**Commit.** `feat(tui): the transcript can paint rooted at one run, under a breadcrumb header`

## 7. Model: open a run view on expand, `esc`/breadcrumb goes up, per-view scroll — ✅ DONE (2026-08-30)

NOTES (2026-08-30): the redirect is stated ONCE, in `toggleBlockAt` (mouse.go), rather than once per reach as the item's text reads: `toggleAtBlockCursor` already delegates to it, so a second copy would be exactly the drift that call's own doc comment says the single click map exists to prevent. `blockcursor.go` gains the sentence saying so.

NOTES (2026-08-30): `upRun` restores the level below's offset only where that level was DETACHED; a level that was following the tail gets the tail, which is where the conversation has grown to while the view was open rather than the row the tail stood on when it was left. The item says "restore offset and detached"; restoring an offset over a grown transcript would silently detach a reader who never scrolled.

NOTES (2026-08-30): the status right slot's `esc back` sits just above the state switch rather than only inside its `stateRunning` arm, so a view open at idle says what its one key does too; `stateErrored` keeps `enter dismiss`, and a pane that owns esc (ask/approval) keeps the stop hint, both through the claimant's own `runViewOwnsEsc` predicate.

NOTES (2026-08-30): `TestE2EFiringMarksAnAbandonedFinalTurn` (`cmd/apogee/e2e_schedule_test.go:55`) deliberately KEEPS `expandLastBlock`, against the item's enumeration of four callers: a Schedule Firing is not a delegation (`toolView.headsRun` is `sub_agent` only), so it never reaches the redirect and its body still opens in place. `e2e_outcome_test.go`'s two edit-diff callers keep it for the same reason.

NOTES (2026-08-30): T-04's step 6 no longer asserts the parent-side result envelope on the frame (`stepCapResult` deleted with it) — see FOLLOW-UP; the golden `t04-step-cap-block.txt` is regenerated to the collapsed row, and step 7's no-error-tone claim moves to that same frame, which is where the head's slot is now read.

NOTES (2026-08-30): `TestE2EOutcomeSlotsCarryTheToolsVerdict` presses `esc` after `openLastRun` — the rest of that session is about the parent's own blocks, and a view left open would be showing the delegate's run while they landed.

NOTES (2026-08-30): consequential edit — internal/tui/doc.go: made necessary by the new file (the package's file map must name it) and by esc's new meaning inside a view.

NOTES (2026-08-30): consequential edit — internal/tui/mouse_test.go: `TestTranscriptClickTogglesALiveBlockAcrossTheBlink` and `TestSubAgentGroupMemberClickOpensItsSpan` both click a FRAMED delegation, so both now assert the view the click opens; the second's "a second click closes it again" case becomes "the breadcrumb brings the list back", which is the same round trip through the shape a run actually has.

NOTES (2026-08-30): `internal/tui/blockcursor_test.go` needed no change — `TestBlockCursorEnterTogglesWhatItStandsOn` stands on a plain shell block, as the item says.

NOTES (2026-08-30): retry fix — `upRun` reads the restore guard off the stack entry (`if left.detached`), not off `m.detached`: the repaint runs while the VIEW's offset is still standing, and a view TALLER than the level below leaves that offset past the level's bottom, where `refreshViewport` clamps it and clears the flag on the way — so the reader's parked row was dropped and the conversation came back at its tail. The doc comment says why the field is not the one to ask, and `runview_test.go` gains "a view taller than the level below still hands that level's offset back", which fails on the old guard (offset 34, want 22) and passes on the new one.

**What.** Depends on items 5, 6. `Model` gains `viewStack []runView` (`runView{ref runRef; yOffset int; detached bool}`) — the stack of open views, empty = top level. `openRun(spawn)`: push the current `viewport.YOffset`/`detached`, exit the block cursor, `transcript.setRoot`, repaint, `GotoBottom`, `detached = false` (follows the latest line — D5 of ADR 0063). `upRun()`: pop, `setRoot` to the parent, repaint, restore offset and `detached`, exit the block cursor. Redirect expand: `toggleExpanded` on a sub-agent head with `span > 0 || !head.done` (item 8's predicate — a running child opens its view before its first entry lands) from both reaches — `toggleAtBlockCursor` (`blockcursor.go:239`) and `toggleBlockAt` (`mouse.go:595`, `targetHeader`) — calls `openRun` instead of flipping `entry.expanded`; an unframed/never-ran delegation and the `✦` umbrella keep today's inline toggle. A click on `targetBreadcrumb` → `upRun`. Keys: a `runViewClaimant` in `keyClaimOrder` (`model.go:1221`) placed AFTER the modal panes (settings/picker/report) and BEFORE the block cursor, claiming only `esc` → `upRun`; every other key falls through (block cursor, prompt). Because the claimant swallows `esc`, `m.lastEsc` never arms inside a view. Status right slot (`layout.md:1173`, `model.go:3119` `statusLeft`/right composer) shows `esc back` in place of `esc×2 stop` while a view is open; `runningPhrase` receives the viewed `runRef` (item 5). A run's `SubAgentFinished` does NOT close the view. One rule for the stack: it pops whenever a `transcript.reset()` (`/clear` → `resetSessionView`, `commandrun.go:179`; `/new`; restore at `sessions.go:560`) leaves `runHead(root.spawn)` unresolvable; `/continue`/`RestoreSession` replaying the same spawn id keep it. Height budget: none — the view is the transcript itself; panes (`/usage`, `/inspect`, settings) stack over it unchanged.

**Regression guard.** `openRun` exits the block cursor (the view opens clean, following its latest line). Item 7 OWNS the cmd/apogee follow-through because it is the item that breaks it: split `expandLastBlock` (cmd/apogee/e2e_support_test.go:591) — keep it for non-delegation blocks, add `openLastRun(drv)` (⌥↑ then ⏎, no esc) for framed runs; retarget every e2e test that expands a delegation (the three regression-3 names) to `openLastRun`, regenerate `cmd/apogee/testdata/frames/t04-step-cap-block.txt`; Files gains those paths, Acceptance gains `go test ./cmd/apogee/ -run 'TestE2EDelegation' -count=1`. (`grep -n expandLastBlock cmd/apogee/*_test.go` finds the callers: `TestE2EDelegationStepCap`, `TestJudgeDelegationStepCap`, `TestE2EOutcomeSlotsCarryTheToolsVerdict`, and `TestE2EFiringMarksAnAbandonedFinalTurn` — every one that opens a delegation moves.) `TestKeyClaimOrderMatchesTheDocumentedPrecedence` (`internal/tui/keyclaim_test.go:12-20`) pins the seven claimant names: add the new name between "inspector pane" and "block cursor" in its `want` list. The claimant does not open in `stateAwaitingAsk` / `stateAwaitingApproval`: the ask pane advertises `esc cancel` (`ask.go:317-325`) and the approval pane a `Cancel  esc` row (`approval.go:36`), so `esc` there keeps today's meaning — the item yields to `approval.go:41-43` (Esc under a live pane inherits the double-tap) — back out after answering; item 11 words the hint. `TestBlockCursorEnterTogglesWhatItStandsOn` (a plain shell block, `blockcursor_test.go:176`) and `TestPerChildBlockExpandsAlone` (direct `toggleExpanded`, `fanout_test.go:226`) stay as they are; `runview_test.go` covers the redirect.

**Files:** `internal/tui/model.go`, `internal/tui/runview.go` (new: `runView`, `openRun`, `upRun`, claimant), `internal/tui/runview_test.go`, `internal/tui/keyclaim_test.go`, `internal/tui/blockcursor.go`, `internal/tui/mouse.go`, `internal/tui/blockcursor_test.go`, `internal/tui/mouse_test.go`, `cmd/apogee/e2e_support_test.go` (`openLastRun`), `cmd/apogee/e2e_delegation_test.go`, `cmd/apogee/e2e_outcome_test.go`, `cmd/apogee/testdata/frames/t04-step-cap-block.txt` (regenerated).

**Tests.** `runview_test.go` (fakeEngine, `Model` reducer): enter on a framed run opens the view at bottom with `detached=false` and the block cursor exited; a running head with no entries yet opens the view too; `esc` returns with the prior offset and `detached`; breadcrumb click returns; nested run opens a second level and two `esc` unwind; `esc` inside a view does not arm the stop (`m.lastEsc` zero, worker untouched); status right slot reads exactly `esc back`; the umbrella row still expands inline; `esc` under an ask/approval pane inside a view keeps today's meaning (no `upRun`); a `transcript.reset()` that drops the root's head pops the stack, a replay of the same spawn id keeps it. `keyclaim_test.go`'s `want` gains the new name. `TestBlockCursorEnterTogglesWhatItStandsOn` and `TestPerChildBlockExpandsAlone` are left as they are. cmd/apogee: the delegation-expanding e2e tests use `openLastRun`; `t04-step-cap-block.txt` is regenerated to the collapsed row.

**Acceptance.** `go test ./internal/tui/ -run 'RunView|BlockCursor|Mouse|Esc|KeyClaim'` · `go vet ./internal/tui/` · `go test ./cmd/apogee/ -run 'TestE2EDelegation' -count=1`.

**Commit.** `feat(tui): expanding a delegation opens its run view; esc and the breadcrumb go one level up`

## 8. Remove the inline expanded sub-agent shape — ✅ DONE (2026-08-30)

NOTES (2026-08-30): consequential edit — internal/tui/toolbranch.go: made necessary by deleting the inline shape (`subAgentOpenMarker`'s doc claimed a delegation's open row wears it; no painter asks for it now, and the tests name it to assert a paint has NOT grown one back).

NOTES (2026-08-30): consequential edit — internal/tui/theme.go: made necessary by deleting the inline shape (`glyphRailCorner`, `glyphRailClose` and `subRail` named the frame an open delegation drew).

NOTES (2026-08-30): consequential edit — internal/tui/blockstate.go: made necessary by dropping the two `blockState.marker` producers (its doc named the ┌─┶ as "today's" marker; no caller names one now).

NOTES (2026-08-30): consequential edit — internal/tui/toolblock.go: made necessary by the same drop (one comment clause named the lone delegation's ┌─┶ as the marker field's user).

NOTES (2026-08-30): consequential edit — ISSUES.md: made necessary by deleting `subAgentPromptRows` (the still-open "grouped never-ran ▶" entry analysed the defect by naming it; the entry itself is untouched and stays open — the `:40` double-print entry is item 11's to remove).

NOTES (2026-08-30): `TestSubAgentCloserOnlyWhenAnotherGroupedMemberFollows` (subagentblock_test.go) was DELETED rather than rewritten: with no shape opening a span in place, `railJoin`'s `closes && prevDepth > depth` can no longer be reached, so the ┊ has no way to be drawn and the rule cannot be observed. The rule itself is left in `railJoin` and its doc says so.

NOTES (2026-08-30): rewritten tests kept their claim and lost the dead half, and four were renamed to match what they now assert — `TestLoneSubAgentRunOpensInTheGroupMembersFrame` → `…WearsTheGroupMembersRow`, `TestExpandedSubAgentKeepsItsTopLevelDetails` → `TestFramedSubAgentRowKeepsItsTopLevelDetails`, `TestNestedSubAgentRunStaysCollapsedInsideAnExpandedParent` → `…InsideItsParentsView`, `TestPerChildBlockExpandsAlone` → `TestPerChildRunOpensAlone`. `TestExpandedSubAgentOpensWithItsPrompt` and `TestExpandedSubAgentPromptOpensOnOneBlankRailLine` were deleted with their fixtures (`delegateWithPrompt`, `delegateAsked`): both assert the railed prompt `subAgentPromptRows` painted, which is deleted. `TestSubAgentStreamPreviewRailedWhenRunExpanded` and `TestSubAgentStreamFramesAnOpenGroupMember` were replaced by `TestSubAgentStreamSettlesWithoutMovingTheView` and `TestSubAgentStreamBelongsToTheChildThatIsTalking`, both against item 6's rooted paint.

NOTES (2026-08-30): two fixtures needed a `subAgentStarted` they had not needed before — a live child with no committed entry is UNFRAMED once nothing can expand it, so its row reads `scheduled` unless the engine's own started phase is in the fixture (which the engine always emits).

NOTES (2026-08-30): the delegation frame's whole drawing surface is now unreachable in production and nothing in this item's scope deletes it — `subAgentOpenMarker`, `blockState.marker` (no producer left), `resolvedBlock.closes` / `railJoin`'s closer arm, and every `railLines`/`railSpacer` call at depth > 0 (a rooted paint rebases its rows to the top level, and every run outside a view is collapsed). It is one coherent sweep, larger than this item and better taken as its own; the comments naming those pieces now say they are pre-ADR-0063.

**What.** Recast at the regression check (2026-08-30). Depends on item 7. Closes `ISSUES.md:40`: "Finished sub-agents print the sub-agent output twice" — the early, badly formatted copy IS `expandedSubAgentView` (`subagentblock.go:548`) painting the delegation's tool-result body (`head.tool`'s body is the report the delegation returned) above `subAgentPromptRows`, while the span's final assistant row is the formatted copy that stays. Delete `expandedSubAgentView` (`subagentblock.go:548`) and the `render.go:462-479` branch that walks into an expanded span; `resolveBlock` for a framed run always takes `collapsedSubAgentView`. Keep `unframedSubAgentView` (`:271`), `scheduledSubAgentView` (`:485`), `subAgentPromptRows` if still referenced (else delete), the group umbrella and `renderSubAgentMemberRows` — for the UNFRAMED member only: in `renderSubAgentGroup` drop the `spanned && m.head.expanded` arms (`subagentblock.go:383-384`, the `expandedSubAgentView` call, and `:398-400`, the `subAgentOpenMarker` swap) so a framed member takes `collapsedSubAgentView` and `branchMarker`, and in `renderSubAgentMemberRows` (`:521-532`) the `spanned` branch returns the one collapsed row regardless of `expanded`. `entry.expanded` on a run head with `span > 0 || !head.done` becomes meaningless: `setExpanded` (`transcript.go:1247`) refuses it (returns false) so replayed/stale state cannot reopen a rail; only a done head with no span keeps the inline `unframedSubAgentView` toggle. Retire the tests that asserted the inline expanded shape (`TestExpandedSubAgentKeepsItsTopLevelDetails`, `TestExpandedSubAgentOpensWithItsPrompt`, `TestLivePreviewPaintsInsideTheRunThatFilledIt` and every sibling found by `grep -ln "expanded\|subAgentOpenMarker\|expandedSubAgentView" internal/tui/*_test.go`) — each is either deleted or rewritten against the rooted paint of item 6, never left asserting a rail that no longer exists. Sub-agent summary slot rules (`layout.md:907-962`) are untouched: the collapsed row is the only row.

**Regression guard.** The retire grep covers every test file under internal/tui/: `grep -ln "expanded\|subAgentOpenMarker\|expandedSubAgentView" internal/tui/*_test.go`, never just the two named files (today it also reaches `transcript_test.go`, `wrap_test.go`, `toolbody_test.go`, `paintcache_test.go`, which join Files). The cmd/apogee follow-through (`openLastRun`, the retargeted e2e tests, the t04 golden) is item 7's. `subAgentFramed` is `span > 0 || (head.expanded && !head.done)` (`subagentblock.go:61`), so a running child with no entries is UNFRAMED while collapsed: refuse/redirect on `span > 0 || !head.done` (the predicate evaluated as if open) in both `setExpanded` and item 7's redirect, so it opens its run view before its first entry lands and never paints the deleted `renderSubAgentRun` branches (`:163-191`). `internal/tui/doc.go:88` names `expandedSubAgentView` (and `:91` `subAgentPromptRows`) in package prose — doc.go joins Files and its "an open head wears the very line…" passage is reworded to the two-shape rule. `collapsedSubAgentView` is defined as `expandedSubAgentView(head, span)` minus the body (`subagentblock.go:563-566`): inline it as `view := head.tool; view.Summary = subAgentSummary(head, span); view.Details = toolBody{}`. Owner decision (2026-08-30): this item is a FIX, not a refactor — it closes the open defect at `ISSUES.md:40` ("Finished sub-agents print the sub-agent output twice"): the early, badly formatted copy IS `expandedSubAgentView` painting the delegation's tool-result body above `subAgentPromptRows`; the span's final assistant row is the formatted copy that stays. The collapsed row's summary/outcome slot stays the place the result envelope (fault, step cap, trailer) shows. Re-check (2026-08-30): the grouped member paints the same rail — `renderSubAgentGroup` (`subagentblock.go:383-384`, `:398-400`) and `renderSubAgentMemberRows` (`:521-532`) are named in What and go with `expandedSubAgentView`, else `:384` fails to compile and a framed member still opens a rail. `internal/tui/blocktarget_test.go:246-262` ("an expanded sub-agent head stays clickable") `t.Fatal`s on `setExpanded(0, true)` = false and pins the `┌─┶` rail — the file joins Files and the case is rewritten to the collapsed row's single `targetHeader` mark (or deleted). `TestPerChildBlockExpandsAlone` (`fanout_test.go:213-238`) `t.Fatal`s on `toggleExpanded` = false at `:228`: it is green through item 7 and THIS item owns its rewrite against item 6's rooted paint (rooting at `s2` shows `beta.md` and not `alpha.go`) — item 7's "stays as it is" holds only until this item lands.

**Files:** `internal/tui/subagentblock.go`, `internal/tui/render.go`, `internal/tui/transcript.go`, `internal/tui/doc.go`, `internal/tui/subagentblock_test.go`, `internal/tui/fanout_test.go`, `internal/tui/render_test.go`, `internal/tui/transcript_test.go`, `internal/tui/wrap_test.go`, `internal/tui/toolbody_test.go`, `internal/tui/paintcache_test.go`, `internal/tui/blocktarget_test.go`.

**Tests.** `subagentblock_test.go`: `setExpanded` on a framed head returns false and paints the collapsed row; `setExpanded` on a running head with no span returns false too, and a done head with no span still toggles `unframedSubAgentView`; `resolveBlock` never yields `subAgentOpenMarker` (`toolbranch.go:312`) for a framed run at top level; the umbrella still expands to member rows. `grep -n "expandedSubAgentView" internal/tui/` is empty. Bite test (`ISSUES.md:40`): build a finished framed delegation whose report has ≥2 lines, call `transcript.setExpanded(head, true)` directly, paint at top level — the paint contains no `subAgentOpenMarker` and the report's SECOND line appears nowhere (the collapsed summary carries only the first line as the gist); this test fails on the pre-item tree (the rail opens with the body) and passes after. Second test on the rooted paint of item 6: the report text appears exactly once, as the last assistant row, and no row precedes the task row. The collapsed row's summary/outcome slot stays the place the result envelope (fault, step cap, trailer) shows. Grouped member: `setExpanded` on a framed member of a `✦ Sub-Agent (N)` umbrella returns false and the group paints the member as one collapsed row under `branchMarker` (no `subAgentOpenMarker`, no prompt rows). `blocktarget_test.go`'s "an expanded sub-agent head stays clickable" case is rewritten to the collapsed row's single `targetHeader` mark (or deleted). `TestPerChildBlockExpandsAlone` (`fanout_test.go`) is rewritten against item 6's rooted paint: rooting at `s2` shows `beta.md` and not `alpha.go`.

**Acceptance.** `go test ./internal/tui/` · `go vet ./internal/tui/` · `! grep -rn "expandedSubAgentView" internal/tui/`.

**Commit.** `fix(tui): a delegation shows its report once — the inline rail that repeated the tool-result body is gone`

## 9. Prompt box addresses the viewed child — ✅ DONE (2026-08-30)

NOTES (2026-08-30): the three-way lifecycle the box routes on (`childPhaseOf`, runview.go) reads
`subAgentReported` — `done || phase == finished` — for "over" rather than `entry.phase` alone. The
phase is view-only and unpersisted, so routing on it alone would read every delegation in a resumed
session as "has not started".

NOTES (2026-08-30): sites folded in beyond the item's named Files, all made necessary by this item's
own change. `internal/tui/runview.go` — the two view moves (`openRun`, `upRun`) set the box's
legend, and the file holds the legend funnel (`viewedChild`, `childPhase`/`childPhaseOf`,
`runLabel`, `legendFor`, `topLegend`). `internal/tui/ask.go` (2 sites), `internal/tui/commandrun.go`
(4 sites) and `internal/tui/model.go` (`finishWorker`) route their existing `setPlaceholder` calls
through `legendFor`, so a lifecycle transition in the conversation BELOW a view cannot re-label a
box that is addressing a child. `internal/tui/fold.go` — `foldChildDelivery` (the band's half of the
delivery report) and a legend re-resolve on the two events that move a run's lifecycle
(`SubAgentPhaseEvent`, `ToolResultEvent`), so the invitation is never a phase behind the run it
names.

NOTES (2026-08-30): consequential edit — internal/tui/doc.go: made necessary by the third prompt
legend and the legend funnel (the module map's placeholder paragraph said the swap was two legends
on the Exchange's lifecycle transitions; the runview.go paragraph did not name what the box needs to
address the run).

NOTES (2026-08-30): consequential edit — cmd/apogee/e2e_delegation_test.go: made necessary by ⏎
inside a view no longer submitting to the conversation. `TestE2EDelegationStepCap` submitted its
second prompt with the run view still open; it now presses `esc` to back out first. No assertion of
that test changed.

NOTES (2026-08-30): the top-level `interjectBox` is deliberately bypassed for a child message — the
engine mailbox IS that queue — so a child row lives on the display queue alone. It is off the band
before any terminal boundary can flush it: the delegation's deferred close accounts for every
message the mailbox still held before the child's report reaches the parent, and Events arrive in
order. The invariant is documented on `queuedInterjection.spawn` rather than guarded with a branch.

**What.** Depends on items 4, 7. While a view is open, `handleKey` `"enter"` (`model.go:1427-1453`) routes by the viewed child's phase (`entry.phase`, `transcript.go:247`, via `runHead` `subagentblock.go:101`): **running** → `stageChildMessage()` in `internal/tui/interject.go`: parse with `promptEditor.submitParse` exactly as `submit()`/`stageInterjection()` do (same `domain.UserInput` — text, file refs, skills; ADR 0005: no new privilege), `recordSend`, call `m.eng.InterjectChild(spawn, in)` from the program goroutine; success → the message shows in the staged band (`layout.md:1379`) with the label `queued for <name>` until item 4's `Landed` fold clears it; `ErrNoSuchChild` → the draft stays in the box and the notice `<name> has finished — message not sent` shows. **finished/scheduled/never-ran** → enter is a no-op with the notice `<name> is not running — go back to send a message`. Placeholders (`layout.md:1872` spelling, single source in `promptEditor`): running child `Message <name>…  ⏎ send · ↑ recall · esc back`; finished `<name> has finished · esc back`; scheduled `<name> has not started · esc back`. The top-level `interjectBox` is bypassed for child messages (the engine mailbox IS the queue). `Model.detached` follows as at top level: sending sets `detached = false`.

**Regression guard.** Gate first on state: only the `stateIdle` and `stateRunning` arms of the `enter` switch (`model.go:1427-1453`) consult the viewed child's phase; `stateAwaitingAsk`, `stateAwaitingApproval` and `stateErrored` keep today's handlers (`submitAnswer`, `resolveApproval`) unchanged — a child's ask/approval renders through the same pane (`approval.go:81`, `:267`; `ask.go:21`) and ⏎ must still answer it while a view is open. `stageChildMessage` keeps `stageInterjection`'s `kindCommand` → `commandRunnable`/`runCommand` and `kindUnknownSlash` → `refuseUnknownSlash` branches verbatim (`interject.go:163-185`, as `submit()` does at `model.go:1612-1626`) before the child mailbox; only `parsed.text != ""` inputs reach `InterjectChild`, so `/usage`, `/inspect`, `/schedule` … typed inside a view still run as commands.

**Files:** `internal/tui/model.go`, `internal/tui/interject.go`, `internal/tui/interject_test.go`, `internal/tui/prompteditor.go` (or the file holding the placeholder texts — grep `queue a message`), `internal/tui/runview_test.go`.

**Tests.** `interject_test.go` / `runview_test.go`: enter in a running child's view calls `fakeEngine.InterjectChild` with the spawn id and the parsed input, band shows `queued for repo-scout`; `Landed:true` clears the band and paints the user row inside the run; `ErrNoSuchChild` keeps the draft and shows the exact notice; finished/scheduled views show their exact placeholders and refuse enter; ⏎ under a child's approval/ask pane inside a view resolves the pane and never calls `InterjectChild`; `/usage` typed inside a view opens its pane and an unknown slash is refused, neither reaching `InterjectChild`; the top-level `interjectBox` stays empty throughout.

**Acceptance.** `go test ./internal/tui/ -run 'RunView|Interject|Placeholder'` · `go vet ./internal/tui/`.

**Commit.** `feat(tui): inside a run view the prompt box messages that sub-agent`

## 10. End-to-end: open, message, back, and the parent's trailer — ✅ DONE (2026-08-30)

NOTES (2026-08-30): the launch does NOT go through `launchTUIConfigured(t, drv, stub, parallelPin)`
as the item's regression guard names it. `parallel-agents:` sits INSIDE a `servers:` entry and
`appendHomeConfig` appends after the top-level `server:` key, which is not merely the wrong scope
but invalid YAML (`yaml: line 6: mapping values are not allowed in this context`, verified against
gopkg.in/yaml.v3). The home is written directly by a local `runViewHome` and launched with
`launchTUIOn`, which exists for exactly this case (its doc names the key-inside-an-entry problem);
the pin itself is still `parallelPin` from `e2e_parallel_test.go:38`, as the guard requires.

NOTES (2026-08-30): scene (1) opens the WORKING run with a local `openWorkingRun` rather than item
7's `openLastRun`. `openLastRun` ends on `drv.WaitQuiet(settled)`, which a working session can never
satisfy — its status line animates a spinner every frame — so it always ends in the wait's 5s
timeout (observed). `openWorkingRun` presses the same ⌥↑ ⏎ and waits on the view's own breadcrumb
instead. `openLastRun` is still used for scene (5)'s finished view, which is the state its settle
suits. See DEFER.

NOTES (2026-08-30): the child's mid-run window is a `token_delay:` on a TOOL-CALL turn (three
seconds between the call's two SSE fragments) rather than the item's "slow token stream" of text.
stubllm refuses a turn carrying both text and tool calls ("a turn is exactly one kind"), and a
sentence still arriving repaints the very frame the t17 golden pins; a `hang:` cannot serve either,
since it answers with nothing at all and a child whose run is over cannot be steered. The park turn
repeats, so a slow machine finds the child still working rather than already gone.

NOTES (2026-08-30): `t17-run-view` is redacted with `goldenRedactions(sess)` PLUS one `RedactPadded`
over the status line's whole live left slot (the spinner's braille phase, the phrase, the elapsed
clock) — a golden of a working view has an animation and a second counter on it, and only the whole
slot has a stable width. `t18-run-view-finished` uses `goldenRedactions(sess)` alone, as the item
says. What the slot says is pinned on cells instead (`scout · `, and never `sub-agents`).

NOTES (2026-08-30): scene (4) also pins the consequence item 3 accepted and pointed at t18 — but t18
is the run view, which never paints the collapsed row. The pair of member rows at the TOP level is
where it shows, so the assertion lives there: the steered child's row reads `tool calls · done` (its
report is no longer one line) while the unsteered sibling's still promotes `The first half is done.`

NOTES (2026-08-30): the parent-notice check is a `WaitFor` rather than a bare assertion — the
child's answer is on screen the moment the child says it, but the result carrying it only reaches
the wire once the parent has every delegate's report and asks its next question.

**What.** Depends on items 3, 5, 8, 9. Add `cmd/apogee/e2e_subagent_view_test.go` on the in-process `tuitest.Driver` (`docs/design/test-drivers.md:386,780` — no `t.Parallel`, waits on content never time) launched through `launchTUIConfigured(t, drv, stub, …)` with the `parallel-agents: 2` line (`parallelPin`, `cmd/apogee/e2e_parallel_test.go:38`), with a stubllm fixture under `cmd/apogee/testdata/stubllm/` scripting: parent delegates two children; child A runs ≥3 Steps with a slow token stream so the test can act mid-run; the parent's final reply echoes the tool result. Scenes: (1) `openLastRun` (item 7, `cmd/apogee/e2e_support_test.go`) opens the run view — `WaitText("← main › ")`, the child's task is the first row, the frame's bottom row is the child's latest line; (2) status line reads `2 sub-agents · working` at top level and `<name> · <phrase>` in the view; (3) type a message + `⏎` → `queued for <name>` band, then the user row inside the run, then stubllm's request log shows the message as a user message in child A's next request (`when:` match) and the parent's tool-result content ending with the exact trailer `(the user sent 1 message to this sub-agent while it ran)`; (4) `esc` returns to top level with the collapsed row; (5) a finished child's view shows `<name> has finished · esc back`. Golden frames via `tuitest.Golden(t, "t17-run-view", frame, goldenRedactions(sess)...)` and `t18-run-view-finished` alike.

**Regression guard.** Scene (1) uses `openLastRun` from item 7, not `expandLastBlock` (⏎ then esc — item 7's claimant would pop the view before `WaitText("← main › ")` sees the header). Scene (2) needs both children LIVE at once, but stubllm serves no `/props`, so the cap discovers `total_slots` absent and runs children serially (`internal/config/config.go:1290-1291`; `TestE2EParallelDelegationsStaySerialWithoutThePin`, `e2e_parallel_test.go:103`): launch with `launchTUIConfigured(t, drv, stub, …)` carrying the `parallelPin` line (`e2e_parallel_test.go:38`). The footer's workdir cell is redacted only by `goldenRedactions(sess)` (`e2e_delegation_test.go:389-394`; `docs/design/test-drivers.md:799-800`), which every frame golden uses — `sess.Redactions()...` alone churns on every box.

**Files:** `cmd/apogee/e2e_subagent_view_test.go`, `cmd/apogee/testdata/stubllm/<fixture>.yaml`, `cmd/apogee/testdata/frames/t17-run-view.txt`, `cmd/apogee/testdata/frames/t18-run-view-finished.txt`.

**Tests.** The file itself; each scene asserts on cells (`Frame.Find`/`Row`), never substrings of the whole screen.

**Acceptance.** `go test ./cmd/apogee/ -run 'TestE2ESubAgentView' -count=1` · `go test ./cmd/apogee/ -run 'TestE2EDelegation' -count=1` (existing delegation e2e still green).

**Commit.** `test(e2e): the run view opens, messages a running sub-agent, and the parent hears about it`

## 11. Docs: CONTEXT.md, layout specs, manual, IDEAS.md

**What.** Depends on item 10. `CONTEXT.md`: **Sub-agent** (`:106`) gains the addressing sentence (spawn call-ID → `InterjectChild`, mailbox drained between the child's Steps, trailer); **Interjection** (`:415`) notes the child form; new term **Run view** (Driver state; view, not fold state; never persisted) linking ADR 0063. `layout.md`: `:797` reworded — "Only a click changes a block's state" stays for folds, plus "expanding a framed delegation opens its **run view**, which opens on its latest line"; a `## Run view` section after `:742`'s block section with a frame sketch (breadcrumb header, child rows at depth 0, status `esc back`, placeholder `Message <name>…`), the status-line text for ≥2 children, and the band label `queued for <name>`. `docs/layout/tool-layout.md:98-109`: two states hold for the row; the run view is the third *surface*; key list adds `esc` back. `docs/manual/commands.md:66-92`: `⏎` on a delegation opens its run view, `esc` goes back, the prompt box then messages that sub-agent, `esc×2` stops only from the top level. `docs/manual/sessions.md`: a run view is not part of the record. `IDEAS.md`: items at `:14` and `:18` are removed (executed → `CHANGELOG.md` at closeout). `ISSUES.md`: the `## Open defects` entry at `:40` ("Finished sub-agents print the sub-agent output twice") is removed — closed by item 8; the closeout records it in `CHANGELOG.md`. Guard rule: every doc sentence or sketch that says a delegation "expands inline", "opens in place", names `expandedSubAgentView`, or draws or names the delegation frame is updated — find them with `grep -rn "expand" layout.md docs/layout docs/manual CONTEXT.md | grep -i "sub-agent\|delegat"` together with `grep -n "┌─┶\|┊\|drawn open\|rail" layout.md docs/layout/tool-layout.md`.

**Regression guard.** The acceptance grep is scoped to the doc targets: `! grep -rn "expandedSubAgentView" layout.md docs/layout docs/manual docs/design docs/adr CONTEXT.md` — `docs/plans/archived/2026-08-11 - 04 - tool-printout-fixes-plan.md:229`, `docs/plans/archived/2026-08-26 - 03 …:75` and this plan carry the name and are not targets. The "expand" grep misses the inline-rail prose that never says expand: `layout.md:42-43` (the top sketch's `┌─┶ survey the tests ✓ … ▼` row), `:729-731` (the frame "runs unbroken from its `┌─┶` header row"), `:841` (a `Sub-Agent` "deliberately drawn open"), `docs/layout/tool-layout.md:26/211` (the `┊` closer after an expanded member's span) — the widened rule above covers them; the top sketch's Sub-Agent row becomes the collapsed row (or the run-view sketch replaces it).

**Files:** `CONTEXT.md`, `layout.md`, `docs/layout/tool-layout.md`, `docs/manual/commands.md`, `docs/manual/sessions.md`, `IDEAS.md`, `ISSUES.md`.

**Tests.** None (docs). Acceptance greps below.

**Acceptance.** `grep -n "Run view" CONTEXT.md layout.md` non-empty · `grep -n "InterjectChild" CONTEXT.md` non-empty · `grep -n "esc back" docs/manual/commands.md layout.md` non-empty · `! grep -n "Navigating sub-agents\|flickers back and forth" IDEAS.md` · `! grep -rn "expandedSubAgentView" layout.md docs/layout docs/manual docs/design docs/adr CONTEXT.md` · `! grep -n "print the sug-agent output twice" ISSUES.md`.

**Commit.** `docs: the run view, child addressing and the merged activity line enter the specs and manual`

---

**Suggested version bump:** minor (`0.19.0` → `0.20.0`) — a new user-facing surface (run view, messaging a child) plus a new engine API (`InterjectChild`, `ChildInterjectionEvent`). The owner decides.

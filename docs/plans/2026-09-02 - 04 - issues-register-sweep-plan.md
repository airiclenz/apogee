# Plan: the ISSUES register sweep — close the open checkbox items

- **Goal:** close every open `- [ ]` item in `ISSUES.md`'s five run-residue sections (13 items) plus
  the two parked entries the owner selected as cheap and self-contained — a session retention policy
  and an optional per-server env allowlist for stdio MCP launches. Two are behaviour fixes
  (`fanOutWidthFor`'s seat cap, the wrap-up's whitespace guard); the rest are guards, single-sourcing,
  dead-code retirement, spec text and two additive, default-off features.
- **Date:** 2026-09-02 · **Status:** unexecuted
- **Sized for:** ~200k-context host
- **Authoritative sources:** `ISSUES.md` (the entries this plan retires);
  [ADR 0069](../adr/0069-the-top-level-model-picks-the-delegation-seat.md) decision 7 (item 7);
  [ADR 0031](../adr/0031-apogee-is-an-embeddable-engine-and-the-tui-is-its-first-driver.md) (the engine
  reads no config — item 8's shape); [ADR 0063](../adr/0063-sub-agent-runs-are-user-addressable-views.md)
  (the outcome envelope — item 4); [ADR 0022](../adr/0022-sessions-persist-per-turn-as-dual-representation-records.md)
  (the record — items 14–16); `internal/mcp/transport.go:137-145` (the L4 trust note — items 17–18);
  `cmd/apogee/wire.go:446-454` (`gcScratchDirs`, the boot-sweep precedent — item 16);
  `internal/tools/git.go:59-90` (`safeGitEnv`, the allowlist idiom — item 17).
  Verified against commit `4ba2c5c`.

**Ratified design calls** (owner, 2026-09-02, all via question at write time)
- **Scope:** the 13 residue checkbox items plus session retention and the stdio-MCP env allowlist; every
  other parked entry stays parked on its own recorded trigger.
- **Seat cap:** the code yields to ADR 0069 decision 7 — an all-`session` reply is sized by the session
  server's cap; the ADR is not amended.
- **Seat choice off the TUI — SUPERSEDED at the regression check (2026-09-02):** the earlier call
  (headless and daemon simply pass the gate into `registryWithMCP`) is replaced. A Firing latches no
  Delegation target, so the gate alone would publish a seat choice with nothing on the far side and
  emit a false `note: … the sub-agents server was unavailable`. Owner call: latch the target FIRST —
  `run.Spec` carries the target and seat (item 8), a Firing resolves and latches it (item 9), and the
  gate rides on top (item 10). Unchanged: no `apogee.Config` field, engine stays config-blind.
- **Step-cap row:** `layout.md` gains the missing sentence; the rendered row does not change.
- **Whitespace:** the latched wrap-up exit aligns on `strings.TrimSpace`.
- **Fallback note:** exported as `agent.SeatFallbackNote` and re-exported as `apogee.SeatFallbackNote`.
- **Retention:** both knobs — `sessions.max-age` and `sessions.max-count`, each absent = off; a silent,
  best-effort boot sweep in all three Drivers that never touches the session being resumed.
- **MCP env:** per-server `env-allowlist:`; absent = today's behaviour byte-for-byte, a named list =
  `ScopeEnv` over those keys plus `env:` appended last, an explicit `[]` = the platform floor alone.

**Regression check (2026-09-02, `4ba2c5c`):**
- Item 1: guard folded — the positive `system:`-only parse case moves out of the error table into its
  own `Parse` test; the What's false "never passes through `validate`" premise corrected.
- Item 2: guard folded — the two capped-path PARENT tests are not edited; the Exchange boundary is
  asserted on the directly driven child instead.
- Item 6: guard folded — ONE literal pin stays in `internal/agent/seat_test.go`, so the exported
  constant does not leave the sentence pinned only against itself.
- Item 7: guard folded — the new branch is gated on `publishesSeatChoice`; the doc comment at
  `internal/agent/dispatch.go:100-101` yields to the fix and is re-worded in the same edit.
- Items 8, 9, 10: recast — the drafted item 8 was a REGRESSION (a Firing latches no Delegation
  target, so the gate would publish a choice with nothing behind it and emit a false fallback note).
  On the owner's call it is replaced by three items — the `run.Spec` seam, the Firing's own target
  latch, then the gate — superseding the "Seat choice off the TUI" call above. Drafted items 9–17 are
  renumbered 11–19.
- Item 13: guard folded — the Acceptance grep is scoped to `internal/tui/` and `cmd/`; a repo-wide
  grep still hits eight archived plan docs and would fail a correct run.
- Item 14: guard folded — `Prune` must not trust a record's declared `Meta.ID` as its filename stem.
- Item 15: recast guard — `cmd/apogee/docs_env_test.go` pins nothing between the template and the
  manual; the manual section is a documentation obligation and `-run TestDocs` leaves Acceptance. Both
  registry rows are stated `Editable` with a `Validate` hook, which the registry guards require.
- Item 16: recast — the sweep cannot run at `cmd/apogee/wire.go:117`; it runs after `resolveResume`
  so `--continue` is resolved before anything is removed. Yields to the invariant item 15's manual
  paragraph states ("never removes the session being resumed").
- Item 18: guard folded — same false premise as item 15; the manual section has no automated guard
  and `-run 'Docs'` leaves Acceptance.
- Item 19: guard folded — the checkbox count after the sweep is 11, not 10: the Conventions legend
  line (`ISSUES.md:22`) matches the grep too.

Second round, same date and commit — the three recast items and item 16 re-checked as written:
- Item 8: guard folded — a latched seat is not observable from a `run.Once` test (no exported getter,
  and a `Tools`-nil run publishes no `run_on`), so the seat half asserts "carried, not routed" only.
- Item 9: decision applied — the `servers:` list and the model profiles are read off `in.opts`, never
  off new `firingInputs` fields; three guards folded — the live `sub-agents-server:` name is mirrored
  into the Firing projection (a `/sub-agents-server` retarget must not leave a `/schedule` Firing on
  the launch-time entry), `newSubAgentServer`'s built server carries the entry's Mechanism catalogue
  into `resolveDelegationTarget` and its refusal is a notice, and `newFiringNamer` gains the routed
  reader with `cmd/apogee/naming.go:47-49` re-worded in the same edit (ADR 0068 decision 2 honoured).
- Item 10: decision applied — no `seatChoice` field; the gate is read off `in.opts.SubAgentsChoice`,
  superseding the first round's field wording. Two guards folded — the composers are pinned
  field-by-field over `tools.HostTools` (tool NAMES cannot catch a dropped `URLGuard`, `SecretEnvVars`
  or read-root policy), and `cmd/apogee/wire_firing.go:90-92` is re-worded with the change.
- Item 16: guard folded — the `--continue` case is driven through `w.wireSession(ctx)`, the only path
  on which a wrongly placed sweep actually fails.

**Standing requirements:** `skills: coding-standards`. Every item's Acceptance is targeted; `make check`
runs once at closeout. Deviations land as a dated NOTES line under the item.

**Out of scope:** the `load_skill` collapsing defect; the eight driver-parity gaps; the hero tape's
knob 3; every other parked entry (each records its own trigger — a grill, bench evidence, hardware or
real demand); any VERSION or CHANGELOG release heading change.

---

## 1. `when.system` is compiled at parse time — ✅ DONE (2026-09-02)

NOTES (2026-09-02): none — item implemented as written, including the regression guard (the failing
row went into the Parse-error table, the positive `system:`-only case into its own `Parse` test,
`TestSystemOnlyWhenBlockParses`, beside `TestEmptyReplyTurnIsLegal`).

**What.** `Match.validate` (`internal/stubllm/script.go:363-375`) returns early when `LastMessage` is
empty, so a `when.system:` regexp that does not compile surfaces later from `newMatcher`
(`internal/stubllm/match.go:45-51`) with a turn index instead of from the YAML parse. Compile `System`
too: drop the early-out, and validate each of `LastMessage` and `System` that is set, reporting
`when.system is not a regexp: %w` in the same shape as the existing `when.last_message` message.
`newMatcher` keeps its own compile — it needs the compiled value. `Script.Validate()` DOES run on
every construction path (`internal/stubllm/script.go:218-221` — Parse, `New` and `Serve`), so after
this change `newMatcher`'s `when.system` compile is unreachable through the public API and stays only
as a defensive path. `TestMatcherRejectsAnUncompilableSystemRegexp`
(`internal/stubllm/match_test.go:98`) calls `newMatcher` directly, so it stays valid and unmoved.

**Regression guard.** Only the FAILING row belongs in `TestParseRejectsAnUnplayableScript`: its
harness fatals on a nil error (`internal/stubllm/script_test.go:165-167`, "parse accepted %q, want it
refused"), so a row proving a valid `system:`-only `when` block still parses would go red there. The
positive case is its own `Parse` test beside `TestEmptyReplyTurnIsLegal` (`:178`), asserting nil.

**Files:** `internal/stubllm/script.go`, `internal/stubllm/script_test.go`

**Tests.** Add a row to the Parse-error table (`internal/stubllm/script_test.go:93-97` is the
`last_message` sibling to copy): `when: {system: "(unclosed"}` → `"when.system is not a regexp"`. Then a
NEW `Parse` test beside `TestEmptyReplyTurnIsLegal` (`internal/stubllm/script_test.go:178`) — not a row
of the error table — proving a `system:`-only `when` block with a valid regexp parses with a nil error.

**Acceptance.** `go build ./... && go test ./internal/stubllm/...`

`fix(stubllm): compile when.system at parse time`

---

## 2. The latched wrap-up exit trims, and always closes the Exchange — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the doc comment above the latched exit (`internal/agent/loop.go:229-235`) was re-worded in the same edit — it said "a wrap-up reply with no text", which the trim change makes incomplete; it now names the whitespace-only case and states that the text committed and emitted is the raw, untrimmed reply.
NOTES (2026-09-02): `finishAtStepCap` was left untouched as the item directs; the Exchange-boundary invariant is pinned on the directly driven child in the new test (`res.Status == domain.StatusExchangeComplete`), and the two capped-path parent tests (`internal/agent/subagent_test.go:1042`, `:1109`) were not edited.
NOTES (2026-09-02): the new test was confirmed non-vacuous — reverting only the `loop.go` condition fails it on all three assertions (lastVisibleText, assistant-message count, MessageEvent), and the condition was restored before finishing.

**What.** Two defects on the same exit. (a) The latched final-answer branch tests
`if text := resp.Text(); text != ""` (`internal/agent/loop.go:235`) while `replyFault` tests
`strings.TrimSpace(resp.Text()) == ""` (`internal/agent/loop.go:553-563`), so a whitespace-only wrap-up
reply carrying a tool call commits an assistant message and emits a `MessageEvent` — the case the guard
exists to prevent. Change the condition to `strings.TrimSpace(text) != ""`; the text that is appended
and emitted stays the raw `resp.Text()`, so no normal reply changes. (b) `finishAtStepCap`'s completed
branch (`internal/agent/agent.go:656-676`) forces `StepCapped` on `err == nil && !res.Faulted` without
asserting the Exchange closed; it relies on the `len(calls) == 0 || a.wrapUp` invariant at
`internal/agent/loop.go:219`, which nothing pins. Do not change `finishAtStepCap` — pin the invariant
in tests instead, so a future edit to the latched exit that returns an open Turn fails loudly.

**Regression guard.** Leave the two capped-path PARENT tests alone: their `res` is the parent's
result, already asserting `StatusExchangeComplete` (`internal/agent/subagent_test.go:1069`, `:1131`),
and the assertion could not bite there anyway — an open-Turn child still reaches `delegationResult`'s
`res.StepCapped` branch (`internal/agent/subagent.go:390`; Status is read only for `StatusCancelled`,
`:366`). Assert the boundary on the DIRECTLY driven child instead.

**Files:** `internal/agent/loop.go`, `internal/agent/subagent_test.go`

**Tests.** A sibling of `TestWrapUpWithNoTextCommitsNothing`
(`internal/agent/subagent_test.go:1904`) whose wrap-up reply is `"  \n\t "` AND carries a tool call:
zero assistant messages, no `MessageEvent`, `lastVisibleText == ""`. The same sibling — built with
`wrapUpAgent` / `runWrapUpAgent` (`internal/agent/subagent_test.go:1777`, `:1796`), which return the
CHILD's own boundary — also asserts `res.Status == domain.StatusExchangeComplete`, never
`domain.StatusTurnComplete`, exactly as `:1876` and `:1910` already do. The two capped-path parent
tests (`:1042`, `:1109`) are not touched.

**Acceptance.** `go build ./... && go test ./internal/agent/ -run 'WrapUp|StepCap'`

`fix(agent): a whitespace-only wrap-up commits nothing`

---

## 3. Two delegation assertions that can go vacuous or spurious — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the live check's failure message is kept as written except for one plural — "the child's requests before the wrap-up offered no tools either" — because the claim is now over every earlier request, not the one immediately before; the `menus` dump is unchanged.

**What.** (a) `cmd/apogee/e2e_delegation_test.go:321-331` compares the raw `childReportWords` constant
(`:59-62`) against a flattened frame, while the sibling at `:279-281` flattens the needle first
(`flatten`, `cmd/apogee/e2e_support_test.go:636`). Identical today only because the fixture wording
holds no line break; flatten the needle in the `unwanted` loop so the capped and uncapped runs stay
tellable apart when it wraps. (b) `internal/agent/live_delegate_cap_test.go:312-324` reads
`menus[len(menus)-2]` as "the request before the wrap-up carried a menu", but `menus` is appended per
PRE-REQUEST hook call (`:103`), and an overflow fold re-runs those hooks inside one Turn
(`internal/agent/loop.go:211`) — two zero entries then fail a correct run. Replace the positional read
with "the last entry is 0 AND some earlier entry is non-zero", keeping the same failure message and the
`menus` dump.

**Files:** `cmd/apogee/e2e_delegation_test.go`, `internal/agent/live_delegate_cap_test.go`

**Tests.** The two edited assertions are the tests. The live one stays gated by `APOGEE_LIVE_ENDPOINT`
(`:171-175`) and must still skip cleanly without it.

**Acceptance.** `go vet ./cmd/apogee/ ./internal/agent/ && go test ./cmd/apogee/ -run TestE2EDelegation`
(the live test skips unset).

`test: flatten the delegation needle and de-flake the live menu-pair check`

---

## 4. `layout.md` says why the cap envelope keeps the collapsed slot — ✅ DONE (2026-09-02)

**What.** `layout.md:942` words the collapsed slot `· stopped at its step cap`, which is exactly what
the TUI emits (`delegationCappedVerdict`, `internal/tui/toolregistry.go:727`, reached through
`delegationVerdict` `:776-786`) — so the spec matches shipped behaviour. What neither says is that a
closing wrap-up report now follows the cap. Add one sentence to the paragraph at `layout.md:939-946`:
the outcome envelope deliberately takes the slot ahead of the report's first line (ADR 0063), and the
capped child's closing report is read one level down, inside the run view. The rendered row does not
change (owner call). Do not restate the envelope's other cell (`· steered by N`) — it is already there.

**Files:** `layout.md`

**Tests.** None — prose only; no code or rendered string changes.

**Acceptance.** `grep -n 'stopped at its step cap' layout.md` shows the row wording unchanged and the
new sentence adjacent; `go build ./...` (no code touched).

`docs(layout): say the step-cap envelope keeps the collapsed slot`

---

## 5. The status line names a run whose name arrived by rename — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the subtest asserts the phrase BEFORE the rename too (the generic `subAgentActivityName` word), so the rename is what the second assertion measures; confirmed non-vacuous by deleting only the `SubAgentNamedEvent` fold, which fails it with `"sub-agent · responding · 0s"`, and restoring it before finishing.
NOTES (2026-09-02): the fixture is the activity-test shape (`newTestModel` + a `sub_agent` `ToolCallEvent` + a depth-1 `TokenEvent`) rather than the enclosing test's `build` helper, because `runningPhrase` reads the Model's activity board, which a bare `*transcript` never fills.

**What.** `transcript.runName` (`internal/tui/transcript.go:509`) is asserted directly
(`internal/tui/transcript_test.go:3349`), and `activity.text` composes `name + " · " + phrase`
(`internal/tui/activity.go:250-256`), but no test composes the RENDERED status phrase for a run whose
name arrived as a `SubAgentNamedEvent` — the existing status-phrase tests
(`internal/tui/activity_test.go:445`, `:486`, `:553`) all take the name from the `sub_agent` call's
`{"name":…}` argument. Add the missing subtest; no production change.

**Files:** `internal/tui/subagentblock_test.go`

**Tests.** A subtest inside `TestGeneratedDelegationNameReachesEverySurface`
(`internal/tui/subagentblock_test.go:1585`, whose other subtests cover the collapsed row, breadcrumb,
run-view invitation and `/usage` row): fold a `domain.SubAgentNamedEvent` for a running delegation,
then assert the stripped `m.runningPhrase(...)` (`internal/tui/model.go:3430`) reads
`<generated name> · <phrase>` — the exact string the status line paints, taken from the render, not
from `runName`.

**Acceptance.** `go test ./internal/tui/ -run TestGeneratedDelegationNameReachesEverySurface`

`test(tui): the status line names a renamed run`

---

## 6. The seat fallback note has one source — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the sentence's value is unchanged byte for byte; only the identifier's case changed. The doc comment at `internal/agent/subagent.go:186` was re-worded in the same edit ("A package constant" → "An EXPORTED constant") because the export makes the old wording false.
NOTES (2026-09-02): both downstream sites keep their local identifier and now DEFINE it from the exported constant — `internal/tui/toolregistry_test.go`'s `note` becomes `"\n" + agent.SeatFallbackNote` (keeping its leading `"\n" +` as the item directs) and `cmd/apogee/e2e_seat_test.go`'s `seatFallbackNoteText` becomes `apogee.SeatFallbackNote` with its "cmd cannot import it" comment replaced by one naming the facade. The copied string LITERAL is gone from both; only the local name survives, which keeps the existing assertion sites and the file's `[seatFallbackNoteText]` doc reference intact.
NOTES (2026-09-02): consequential edit — cmd/apogee/e2e_seat_test.go:193: made necessary by the re-worded constant comment; the announced-line comment cited "[seatFallbackNoteText]'s reason" (that cmd cannot import the engine), which no longer holds, so it now states the reason directly.
NOTES (2026-09-02): the new pin `TestSeat_FallbackNoteIsTheSentenceItself` was confirmed non-vacuous by construction — it is the only literal in the repo besides the constant itself (`grep -rn 'ran on the session server' --include=*.go` shows exactly those two code lines), so any re-word of `internal/agent/subagent.go:188` fails it.

**What.** `seatFallbackNote` (`internal/agent/subagent.go:186`) is unexported and its sentence is
re-typed verbatim in `internal/tui/toolregistry_test.go:135` and `cmd/apogee/e2e_seat_test.go:61`, with
nothing tying the copies to the engine's constant. Export it as `SeatFallbackNote` (same value, byte for
byte — the sentence the model reads must not change), update its production use at
`internal/agent/subagent.go:428` and the same-package references at `internal/agent/seat_test.go:175`,
`:197`, `:417`, and re-export it from the root facade as `apogee.SeatFallbackNote` beside the existing
agent re-exports in `apogee.go` (owner call; additive to the public surface). Then delete both copies:
`internal/tui/toolregistry_test.go` imports `internal/agent` (its sibling tests already do,
`internal/tui/e2e_test.go:18`) and keeps its leading `"\n" +`; `cmd/apogee/e2e_seat_test.go` uses
`apogee.SeatFallbackNote` and its "cmd cannot import it" comment is replaced by one naming the facade.
`internal/agent/seat_test.go` GAINS one literal pin — an assertion that `SeatFallbackNote` equals the
sentence written out in the test — because `:175`, `:197` and `:417` all read the constant today and
would go on passing after a re-word of `internal/agent/subagent.go:186`.

**Regression guard.** The sentence must not end up pinned only against the constant — that is the
vacuous assertion item 11 of this plan fixes for the inspector. Keep ONE literal: the new assertion in
`internal/agent/seat_test.go`; `internal/tui/toolregistry_test.go` and `cmd/apogee/e2e_seat_test.go`
then reference the exported constant safely.

**Files:** `internal/agent/subagent.go`, `internal/agent/seat_test.go`, `apogee.go`,
`internal/tui/toolregistry_test.go`, `cmd/apogee/e2e_seat_test.go`

**Tests.** One literal pin, in `internal/agent/seat_test.go`: `SeatFallbackNote` compared to the
sentence written out there, so a re-word of `internal/agent/subagent.go:186` still fails a test. The
other three sites stop re-typing the sentence and read the constant; no test beyond that one.

**Acceptance.** `go build ./... && go test ./internal/agent/ -run Seat ./internal/tui/ -run
TestDelegationRecognisersReadThroughTheRoutingNote && go test ./cmd/apogee/ -run TestE2ESeat`

`refactor(agent): export SeatFallbackNote and single-source its sentence`

---

## 7. A single-seat reply keeps its seat's cap

**What.** ADR 0069 decision 7 says "a single-seat reply keeps its seat's cap", but `fanOutWidthFor`
(`internal/agent/dispatch.go:113-127`) sends every unsplit reply through `fanOutWidth` →
`delegationWidth` → `delegationCap` (`:211-216`), which returns the latched target's `ParallelAgents`
whenever a target exists. So an all-`run_on: "session"` reply in a routed session is sized by the
target's cap, never the session server's. Fix the code, not the ADR (owner call). Binding rule: in the
not-split branch, when a target IS latched and EVERY call explicitly asked for the session seat
(`askedSeat`, `internal/agent/dispatch.go:158` — an unparseable or absent seat is NOT an explicit ask),
size the batch by `min(a.parallelAgentsCap(), len(calls))`; every other case keeps today's path
byte-for-byte. Add the rule as one guarded branch, not a rewrite of `fanOutWidth`, whose own table
(`internal/agent/fanout_test.go:475-500`) must stay green. The doc comment at
`internal/agent/dispatch.go:100-101` states the old rule — a one-seat reply "keeps that seat's own
width, which is `fanOutWidth`'s rule verbatim" — which this fix falsifies for the session seat; that
comment yields and is re-worded in the same edit to name the ADR 0069 exception.

**Regression guard.** Gate the new branch on `publishesSeatChoice(a.tools)` beside the latched target,
exactly as `seatsAreSplit` gates itself (`internal/agent/dispatch.go:144`). Under
`sub-agents-choice: fixed` `run_on` is ignored (`internal/agent/subagent.go:227-228`), so an
all-`session` reply still runs every child on the TARGET and must keep the target's cap — the item's
own DISARMED row.

**Files:** `internal/agent/dispatch.go`, `internal/agent/fanout_test.go`

**Tests.** In `TestFanOutWidth_MixedSeatsTakeTheSmallerCap` (`:527`) flip the row at `:564-568` — "one
seat: every call on the session seat" with `sessionCap: 2`, `target ParallelAgents: 3` — from `want: 3`
to `want: 2`, and reword its name to state the ADR rule. Add rows for: every call on the TARGET seat
(target cap wins, unchanged); all-session with NO target latched (session cap, unchanged); seat choice
DISARMED with a target latched (target cap, unchanged).
`TestFanOutWidth_UnparseableSeatIsNotASplit` (`:608`) must stay green untouched.

**Acceptance.** `go build ./... && go test ./internal/agent/ -run TestFanOutWidth`

`fix(agent): size an all-session reply by the session server's cap`

---

## 8. `run.Spec` carries a Delegation target and seat

**What.** `run.Once` calls `agent.New(cfg)` (`internal/run/run.go:244`) and never exposes the constructed
Agent, and `run.Spec` (`internal/run/run.go:28-63`) has no field for routing — the only doors are
`Agent.SetDelegationTarget` / `SetDelegationSeat`, which no Driver but the TUI can reach. Add two
optional fields to `run.Spec`, of the exact types those two setters take, and have `run.Once` call each
setter on the constructed Agent when the field is non-nil, before the first Turn. Both nil ⇒ today's
behaviour byte-for-byte. The engine still reads no config (ADR 0031): the Driver resolves the target,
this seam only carries it.

**Regression guard.** A latched seat cannot be OBSERVED from a `run.Once` test: there is no exported
getter (only `Agent.SetDelegationSeat`, `internal/agent/delegationseat.go:60`), and `delegationSeats()`
returns "" unless `publishesSeatChoice(a.tools)` (`internal/agent/orientation.go:141-143`), which a
`Config.Tools`-nil run never satisfies — `hostTools` sets no `SubAgentSeatChoice`
(`internal/agent/construct.go:500-541`). The seat half of the Tests therefore asserts "carried, not
routed" and nothing more; the seat RENDERED is item 10's test, where the registry publishes `run_on`.

**Files:** `internal/run/run.go`, `internal/run/run_test.go`

**Tests.** With two `stubllm` servers (`internal/stubllm`): a Spec whose delegation target names the
second sends a delegated child's request to that server while the parent's requests go to the first;
with both fields nil every request goes to the session server, and the run is byte-for-byte as before.
A seat set without a target is carried but routes nothing: every request still reaches the session
server and none reaches the second.

**Acceptance.** `go build ./... && go test ./internal/run/`

`feat(run): a Spec can carry the Delegation target and seat`

---

## 9. A Firing resolves and latches its sub-agent seat

**Depends on item 8.**

**What.** `firingConfig` composes a run that routes nothing, stated verbatim at
`cmd/apogee/wire_firing.go:298-305` ("this composition routes nothing, so no child is ever routed and
the seat question never arises") — supersede that sentence by name in the comment that replaces it.
Give a Firing the routing the TUI has, without the TUI's live machinery: `firingConfig` reads the
`servers:` list and the model-profile entries `resolveModelProfile` needs off `in.opts`
(`config.Options.Servers`, `internal/config/options.go:85`; `ModelProfiles`, `:322`) rather than off
new `firingInputs` fields, and looks the `sub-agents-server:` name up with
`config.SubAgentsServerTarget` (used at `cmd/apogee/delegation.go:260`), resolves that entry's OWN key
through `in.keys`, takes ONE beat against it with `heartbeat.NewMonitor(...).Beat(ctx)` — the one-shot
precedent `discoverSlots` / `discoverDialect` already set (`cmd/apogee/headless.go:140`, `:156`) — and
builds the target with the existing `resolveDelegationTarget` (`cmd/apogee/delegation.go:803-860`),
returning it and `delegationSeatOf`'s seat (`cmd/apogee/delegation.go:284-294`) alongside the Config.
Reuse both functions; do not re-derive the pin-else-observe ladder. Also set `Config.ServerName` and
`Config.ServerDescription` from the bound entry (`cmd/apogee/wire_server.go:119-120` is the shape) so
the orientation bullet can name the session seat. Every failure is a NOTICE on the `[]string`
`firingConfig` already returns, never an error: no `sub-agents-server:` set, an unknown name
(`missingNameNotice`, `cmd/apogee/delegation.go:303`), a refused key, or an unreachable / model-less
beat (`resolveDelegationTarget` returns nil, `:810-820`) each leave the target nil and the run
unrouted — exactly today's behaviour. `firingConfig`'s signature grows the target and seat; update all
callers (`cmd/apogee/headless.go:411`, `cmd/apogee/daemonfire.go:217`, `cmd/apogee/schedule.go:126`,
`cmd/apogee/naming_test.go:418`), and headless and daemon pass what it returns into `run.Spec` through
item 8's seam.

**Regression guard.** Do NOT add `servers` or model-profile fields to `firingInputs` — `in.opts`
already carries both (`config.Options.Servers` at internal/config/options.go:85 and `ModelProfiles` at
:322). Read them off `in.opts` inside firingConfig, so a caller that fills `opts` cannot silently omit
the routing inputs and downgrade a routed run to an unrouted one with no compile error.

The `sub-agents-server:` NAME must be live for the session Driver too: `optionsLocked` mirrors no
`SubAgentsServer` (`cmd/apogee/wire_settings.go:744-786`), so a `/sub-agents-server` retarget would
leave a `/schedule` Firing routing to the LAUNCH-time entry — against `firingInputs`' own `opts`
contract (`cmd/apogee/wire_firing.go:22-24`). Mirror the live name into that projection at the
retarget seam (`delegationHost.Retarget`, `cmd/apogee/delegation.go:68`) and at the reload's `relist`
(`cmd/apogee/wire_settings.go:1829`), beside the `Servers` and `SubAgentsChoice` it already mirrors.

Build the target through `newSubAgentServer(entry, cfg)` (`cmd/apogee/delegation.go:326-336`), whose
refusal of a defective `mechanisms:` map (`subAgentCatalogue`, `:890-897`) is a NOTICE with the target
left nil, never an error, and pass the server it built on: `delegationSeatOf` takes that
`*subAgentServer` (`:284-294`) and its `catalogue` is `resolveDelegationTarget`'s fifth argument
(`:803-809`) — else a routed Firing child silently inherits the parent's Mechanisms instead of the
entry's `mechanisms:` map.

`newFiringNamer` hard-wires `routed = nil` (`cmd/apogee/naming.go:76-79`), so after this item every
routed child's auto-title call falls back to the session server — the extra call on the expensive box
ADR 0068 decision 2 exists to avoid (`cmd/apogee/naming.go:13-16`). Give `newFiringNamer` the routed
reader off the target this item resolves (constant for the run, like the session one) and re-word
`cmd/apogee/naming.go:47-49` ("an unattended Firing … has no Sub-agent server at all") in the same
edit; decision 2 itself is honoured, not superseded.

**Files:** `cmd/apogee/wire_firing.go`, `cmd/apogee/delegation.go`, `cmd/apogee/wire_settings.go`,
`cmd/apogee/naming.go`, `cmd/apogee/headless.go`, `cmd/apogee/daemonfire.go`,
`cmd/apogee/schedule.go`, `cmd/apogee/wire_firing_test.go`, `cmd/apogee/wire_settings_test.go`,
`cmd/apogee/naming_test.go`

**Tests.** A table in `cmd/apogee/wire_firing_test.go`: no `sub-agents-server:` ⇒ nil target, no notice,
and no beat fired; an unknown name ⇒ nil target plus the missing-name sentence; a reachable stub entry
⇒ a target carrying that entry's model and resolved cap, and a seat named for the entry; an unreachable
entry ⇒ nil target plus the unavailable sentence; an entry whose `mechanisms:` map this build refuses
⇒ nil target plus a notice, never an error; a reachable entry WITH a `mechanisms:` map ⇒ the target
carries that entry's catalogue rather than the parent's.
`TestFiringSourcesCarriesTheLiveSubAgentsServer` (`cmd/apogee/wire_settings_test.go`): after a
`/sub-agents-server` retarget, the Options `firingSources` hands back name the NEW entry rather than
the launch-time one. In `cmd/apogee/naming_test.go`, a routed child's naming call is built on the
TARGET's binding and an unrouted one on the run's own.
`TestFiringConfigSetsEveryUnattendedField` (`cmd/apogee/wire_firing_test.go:42`) gains `ServerName` /
`ServerDescription`.
`TestFiringConfigDefaultsItsSeams` (`:277`) swaps the probe package vars and must stay green — the new
beat fires only when `sub-agents-server:` is set, so the default path gains no third probe.

**Acceptance.** `go build ./... && go test ./cmd/apogee/ -run 'Firing|Naming'`

`feat(cmd): a Firing routes delegations to the sub-agents server`

---

## 10. Publish the seat where a Firing can honour it

**Depends on item 9.**

**What.** With a target latched, `sub-agents-choice: model` can be honoured off the TUI — the gap the
ISSUES entry names. Read the gate off the Options every Driver already fills —
`in.opts.SubAgentsChoice == config.SubAgentsChoiceModel` (`internal/config/options.go:103`) — inside
`firingConfig`, so no existing caller's argument list changes and no caller can fill `opts` while
leaving the gate off. Set
`cfg.Tools = registryWithMCP(in.roots.workspace, cfg, true, nil)` — nil MCP tools, a Firing reaching no
MCP server (ADR 0034) — **only** when the gate is armed. The fixed and absent paths keep `cfg.Tools`
nil byte-for-byte, so `run.Once`'s nil Asker / Presenter go on shaping the engine's own roster
(`internal/run/run.go:240-241`) exactly as today; this is the guard, not an optimisation. No
`apogee.Config` field for the gate (owner call — the previous plan's item-13 guard stands, and ADR 0031
keeps the engine config-blind). The two `HostTools` composers (`cmd/apogee/wire_tools.go:246-281` and
`internal/agent/construct.go:500-542`) are field-identical today except `SubAgentSeatChoice`; the
field-by-field pin below holds them that way, so this hand-assembly cannot silently drop a user
policy.

**Regression guard.** Do NOT add a `seatChoice bool` field to `firingInputs` — this supersedes the
earlier guard wording that proposed one. Read `in.opts.SubAgentsChoice == config.SubAgentsChoiceModel`
(internal/config/options.go:103) directly inside firingConfig, so no caller can fill `opts` and
silently leave the gate off. The rest of that guard stands unchanged: cfg.Tools is set ONLY when the
gate is armed, and the fixed/absent paths keep it nil byte-for-byte.

A tool-NAMES equivalence cannot bite for what it is invoked for: names depend only on
`Disabled`/`Enabled`/`ProfileRoster` and the three nil-gated delegates, so a dropped `URLGuard`,
`SecretEnvVars`, `ExtraReadRoots` or `VirtualReadRoots` — the very hazard `registryWithMCP`'s own
comments name (`cmd/apogee/wire_tools.go:243-281`) — leaves every name identical, and `defaultRoster`
is unexported (`internal/agent/construct.go:459`) with no `Agent.Tools()` accessor to read the other
side from. Pin the two composers FIELD-BY-FIELD instead: a reflect-over-`tools.HostTools` test in each
package that fails on any field left zero for a Config with every field set — in `cmd/apogee` over the
literal `registryWithMCP` composes (extracted into a helper the test can read), in `internal/agent`
over `hostTools(cfg)`, whose one permitted zero is `SubAgentSeatChoice` — so a field added to
`HostTools` and missed by either composer fails.

Re-word `cmd/apogee/wire_firing.go:90-92` ("Tools stays nil too because a Firing reaches no external
MCP server (ADR 0034), so the engine builds its own registry") and the same claim in the test comment
at `cmd/apogee/wire_firing_test.go:372-374` in this same edit: Tools stays nil EXCEPT under
`sub-agents-choice: model`, and a Firing still reaches no MCP server (nil `mcpTools`).

**Files:** `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_tools.go`,
`cmd/apogee/wire_firing_test.go`, `cmd/apogee/wire_tools_test.go`, `internal/agent/construct_test.go`

**Tests.** `TestFiringConfigLeavesTheDriverSeamsNil` (`cmd/apogee/wire_firing_test.go:375`) gains the
gate cases, each set through `opts.SubAgentsChoice` alone: `cfg.Tools` stays nil with the gate absent
or `fixed`, and is non-nil only with it on — its existing `Events`/`Approver`/`Asker`/`Presenter`
assertions stay unchanged. With the gate on: the `sub_agent` schema the registry actually returns
publishes `run_on` (read off the registry, never a fixture), and the orientation bullet names both
seats. In place of a names equivalence, the field-by-field pin: one reflect-over-`tools.HostTools`
test per composer — `cmd/apogee/wire_tools_test.go` over the literal `registryWithMCP` composes,
`internal/agent/construct_test.go` over `hostTools(cfg)` — each failing on any field left zero for a
Config with every field set, `SubAgentSeatChoice` excepted engine-side.

**Acceptance.** `go build ./... && go test ./cmd/apogee/ -run 'Firing|SeatChoice|HostTools' && go test ./internal/agent/ -run 'HostTools'`

`fix(cmd): headless and daemon honour sub-agents-choice: model`

---

## 11. The inspector's two sentences are pinned literally

**What.** Both halves of `TestInspectorScopedEmptyNamesEveryCause`
(`internal/tui/inspector_test.go:841-851`) compare the rendered row to `inspectorScopedEmptyRow` /
`inspectorDisarmedRow` rather than to the sentence, so a typo introduced in either constant
(`internal/tui/inspector.go:124`, `:138`) leaves the test green — and naming all three causes and the
way back is the scoped-empty row's whole job. Pin the literal sentences, matching the house convention
(`internal/tui/command_test.go:272`, `internal/tui/skill_test.go:1230`). Test-only; the constants and
`inspectorRows` (`internal/tui/inspector.go:603-613`) do not change.

**Files:** `internal/tui/inspector_test.go`

**Tests.** The two assertions compare against the sentences written out in the test. Cover the third
sibling `inspectorEmptyRow` (`internal/tui/inspector.go:128`) the same way if a test already renders it;
otherwise leave it.

**Acceptance.** `go test ./internal/tui/ -run TestInspectorScopedEmpty`

`test(tui): pin the inspector's scoped-empty and disarmed sentences`

---

## 12. The /settings toggle list applies the off-ramp floor

**What.** `ListMechanisms` (`cmd/apogee/wire_options.go:222-234`) unions
`mechanisms.OffRampFloor(enabled)` into the rows it answers, so an off-ramp whose key is simply absent
from the `mechanisms:` block reads ON in the pane (ADR 0070). The floor itself
(`internal/mechanisms/retired.go:60-72`) and the sibling startup projection `withOffRampFloor`
(`cmd/apogee/wire_live.go:440-449`, pinned at `cmd/apogee/wire_live_test.go:309-314`) are covered;
nothing asserts this list applies it, so a row could regress to reading OFF for an armed Mechanism.
Test-only; `cmd/apogee/wire_options_test.go` already exists (it covers the write half) and is the home.

**Files:** `cmd/apogee/wire_options_test.go`

**Tests.** Build the options seam over a config whose `mechanisms:` block does NOT name an off-ramp id,
call `ListMechanisms`, and assert every id in `mechanisms.OffRampFloor(nil)` comes back `Enabled: true`
while a non-off-ramp id absent from the block comes back `Enabled: false`. Model the setup on
`cmd/apogee/wire_live_test.go:295-314`.

**Acceptance.** `go test ./cmd/apogee/ -run 'TestListMechanisms|TestWriteMechanism'`

`test(cmd): pin the settings list's off-ramp floor`

---

## 13. Retire `toolCallRun`

**What.** With `resolveBlock`'s same-label branch gone, `toolCallRun`
(`internal/tui/toolbranch.go:346-363`) has no production caller — verified repo-wide: the only
non-comment references are four test call sites. Delete the function and its doc comment. Each of the
four tests uses it purely as a run-extent probe and translates 1:1 to `sameLabelRun`
(`internal/tui/transcript.go:1566`, which keeps its production callers at `:1609`, `:1615`): compare
against `1` at `internal/tui/render_test.go:216` and `0` at `:219`, `1` at
`internal/tui/toolblock_test.go:105`, `2` at `internal/tui/toolshape_test.go:550`, `0` at
`internal/tui/subagentblock_test.go:493`. Keep every assertion — they guard real run boundaries — and
adjust the messages to name `sameLabelRun`. The guard for the prose: every comment that cites
`toolCallRun` as a resolution helper must be re-worded or deleted; find them with
`grep -rn 'toolCallRun' internal/tui/` and leave zero hits outside this item's deletions (today they
sit at `internal/tui/render.go:309,477,548` and `internal/tui/blocktarget.go:70`).

**Regression guard.** Scope the Acceptance grep to the code this item governs — `internal/tui/` and
`cmd/` — never the repo: `grep -rn 'toolCallRun' .` still hits eight archived plan docs
(`docs/plans/archived/tool-call-layout-plan.md:189`,
`docs/plans/archived/2026-08-19 - 04 - tui-architecture-deepening-plan.md:938`, …) plus this plan file,
so a correct run would fail the gate and push an implementer to edit the archived record. `ISSUES.md`
and `docs/plans/` are untouched by this item.

**Files:** `internal/tui/toolbranch.go`, `internal/tui/render.go`, `internal/tui/blocktarget.go`,
`internal/tui/render_test.go`, `internal/tui/toolblock_test.go`, `internal/tui/toolshape_test.go`,
`internal/tui/subagentblock_test.go`

**Tests.** The four rewritten guards; no new test. Painted-output assertions that follow each guard
stay untouched.

**Acceptance.** `grep -rn 'toolCallRun' internal/tui/ cmd/ ; go build ./... && go test ./internal/tui/`
— the grep returns nothing.

`refactor(tui): retire toolCallRun`

---

## 14. The session store prunes by age and count

**What.** `internal/session.Store` never prunes (`internal/session/store.go:160-305` — no
Prune/GC/Count), so `~/.apogee/sessions/` grows unbounded. Add
`type Retention struct { MaxAge time.Duration; MaxCount int }` and
`func (s *Store) Prune(r Retention, keep ...string) (int, error)`, returning how many records it
removed. Binding rules: a zero `Retention` is a no-op returning `(0, nil)`; candidates come from
`List()` (`:235`), which already sorts by `UpdatedAt` descending and already skips corrupt files — a
file `List` cannot read is never deleted; `MaxAge` discards records whose `UpdatedAt` is older than
`s.now()` minus the duration; `MaxCount` then keeps the newest N by `UpdatedAt` and discards the rest;
an id in `keep` is never deleted but still occupies one of the N kept slots; deletion goes through
`Delete` (`:287`) so the existing lock and id validation apply; a single failed delete does not abort
the sweep — the error returned is the first one, with the count of what did go. No caller yet.

**Regression guard.** A record's declared `Meta.ID` need not be its filename stem — `decodeRecord`
validates the id but never compares it to the path (`internal/session/store.go:325-334`) — so
`Delete(m.ID)` over `List()` Metas would let a copied file (a hand-made `backup.json` carrying a live
record's id) delete `<that id>.json`, the newest record the sweep meant to keep. Enumerate the
directory in `Prune`, or drop any candidate whose file stem differs from its `Meta.ID`.

**Files:** `internal/session/store.go`, `internal/session/store_test.go`

**Tests.** In `internal/session/store_test.go` (helper `sampleRecord`, `:19`), with `s.now` stubbed:
zero Retention removes nothing; age-only removes exactly the records past the cut and leaves the rest
loadable; count-only keeps the newest N; both together; a `keep` id survives a rule that would have
removed it, and occupies a slot; a corrupt file in the directory is left untouched; and a copied file
whose stem differs from the id it declares never causes another record's deletion.

**Acceptance.** `go build ./... && go test ./internal/session/`

`feat(session): the store prunes by age and count`

---

## 15. `sessions.max-age` and `sessions.max-count`

**What.** Add the `sessions:` block, both keys absent-by-default so behaviour is unchanged out of the
box: `sessionsConfig{ MaxAge *string \`yaml:"max-age"\`; MaxCount *int \`yaml:"max-count"\` }` on
`fileConfig` (`internal/config/config.go:1101`, modelled on `uiConfig` `:2013`), a resolved
`SessionSettings{ MaxAge time.Duration; MaxCount int }` carried on `Options`
(`internal/config/options.go`), a `defaultSessionSettings()` returning both zero, a `toSessionSettings`
mapper, a `Validate` refusing a negative count or an unparseable/negative duration, the two `fromFile`
rows, and two `KeyRegistry` rows (`internal/config/registry.go:175`), both `Editable: true` with a
`Validate` hook each — `sessions.max-age` a `KindString` duration validated through
`toSessionSettings().Validate()` the way `validateStallAfter` serves `ui.stall-after`
(`internal/config/registry.go:523-527`), `sessions.max-count` a non-negative check on `present.port`'s
model. Registry order must match the template's, so place the `sessions:` block in
`internal/config/defaults/config.yaml` immediately after the `ui:` block, commented out (the
default-off idiom), and the two rows correspondingly. The manual gains its section in this same item —
a documentation obligation, not a test-enforced one: a
`## Keeping the session store bounded — \`sessions:\`` section in the file's existing shape (prose +
a fenced `# ~/.apogee/config.yaml` example), stating both keys are off by default, that the sweep runs
at startup, and that it never removes the session being resumed. No consumer yet.

**Regression guard.** `cmd/apogee/docs_env_test.go` does NOT pin the config template against
`docs/manual/configuration.md` — it pins `APOGEE_*` names and three url-safety strings — so that test
is not this item's guard and `-run TestDocs` proves nothing here. The manual section stays required as
a documentation obligation; the item's guards are `internal/config/defaults_test.go` (the template
still parses to defaults) plus the registry guards, which is why both rows must be stated `Editable`
with a hook: `TestRegistryValidateHooksSitOnEditableKeys` errors on a hook without `Editable`
(`internal/config/registry_test.go:471`) and on an editable non-bool row with no hook (`:473`).

**Files:** `internal/config/config.go`, `internal/config/options.go`, `internal/config/registry.go`,
`internal/config/defaults/config.yaml`, `internal/config/config_test.go`,
`docs/manual/configuration.md`

**Tests.** A parse round-trip for both keys (model on `TestApplyConfigMCPServers`,
`internal/config/config_test.go:2058`); absent block ⇒ both zero; invalid duration and negative count
are startup errors naming the key. The mechanical guards in `internal/config/registry_test.go`
(bijection `:24`, row invariants `:204`, projection `:258`, validate-hooks `:449`) must pass unchanged,
as must the editable-key sweep `TestSpliceScalarSettingRoundTripsEveryEditableKey`
(`internal/config/configwrite_scalar_test.go:243`) and `internal/config/defaults_test.go`.

**Acceptance.** `go build ./... && go test ./internal/config/`

`feat(config): sessions.max-age and sessions.max-count`

---

## 16. The boot sweep, in all three Drivers

**Depends on items 14 and 15.**

**What.** Recast at the regression check (2026-09-02). Wire the policy to the store. `gcScratchDirs`
(`cmd/apogee/wire.go:446-454`) is the precedent and the shape: silent, best-effort, ignoring every
error. Add a sibling beside it that calls `Store.Prune(session.Retention{…}, activeID)` and is called
once per boot from all three Drivers — the TUI path from `cmd/apogee/wire_live.go`, immediately AFTER
`resolveResume` (`:207`), where the config and the resolved record are both known (the scratch sweep at
`cmd/apogee/wire.go:108` runs earlier because it needs neither); `cmd/apogee/headless.go` (beside its
store construction at `:432`) and `cmd/apogee/daemonfire.go` (beside `:119`), each after its own resume
resolution where it has one. Both knobs zero ⇒ the call is a no-op and no directory is walked. The
resolved record's id is passed as the `keep` id so a `--resume`d or `--continue`d record is never swept
out from under the run; a fresh session has no id yet and passes none. Nothing is printed — a startup
notice is deliberately not added (the policy is opt-in, so its effect is not a surprise).
`docs/manual/sessions.md` gains a short paragraph beside the existing delete sentence
(`docs/manual/sessions.md:17`) saying the store is unbounded unless `sessions:` is configured, and
pointing at the configuration section item 15 wrote.

**Regression guard.** The sweep CANNOT run at `cmd/apogee/wire.go:117` — the `--continue` target is
resolved only inside `wireSession` (`cmd/apogee/wire_live.go:206-207`), so a record the policy would
sweep is deleted before it is read and `apogee --continue` dies on "no saved sessions for this
workspace" (`cmd/apogee/wire_session.go:344-347`). Run the sweep AFTER `resolveResume`
(`cmd/apogee/wire_live.go:207`), passing the resolved record's id as the keep id, so both
`--resume <id>` and `--continue` are known before anything is removed. Headless and daemon sweep beside
their own store construction, after their own resume resolution where they have one. The item's "never
removes the session being resumed or continued" promise — the invariant item 15's manual paragraph
states — is what this ordering exists to keep.

That ordering also has to be TESTED on the real path: `w.wireSession(ctx)` is already test-drivable
(`cmd/apogee/wire_live_test.go:70`, `:87`, `:132`), and a test that hand-composes `resolveResume`
followed by the sweep on the direct-call models passes identically with the sweep left at
`cmd/apogee/wire.go:108` — the failure this guard exists to catch. Drive the `--continue` case through `wireSession`, never
through the two helpers in hand-picked order.

**Files:** `cmd/apogee/wire.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/headless.go`,
`cmd/apogee/daemonfire.go`, `cmd/apogee/wire_session_test.go`, `cmd/apogee/wire_live_test.go`,
`docs/manual/sessions.md`

**Tests.** In `cmd/apogee/wire_session_test.go` (models at `:515`, `:524`): a temp home with records
either side of the cut is pruned to the configured shape; both knobs absent leaves every record; a
missing sessions directory is not an error; the id passed as active survives a policy that would
otherwise remove it; and a `--continue` whose target does not rank inside `max-count` store-wide still
resumes — driven through `w.wireSession(ctx)` (the real boot path, `cmd/apogee/wire_live_test.go:70`)
rather than through `resolveResume` and the sweep called in hand-picked order.

**Acceptance.** `go build ./... && go test ./cmd/apogee/ -run 'Session|Prune|Retention'`

`feat(cmd): prune sessions at boot under the retention policy`

---

## 17. An optional per-server env allowlist for stdio MCP launches

**What.** A configured stdio MCP server inherits apogee's full environment — deliberate, and stated in
the trust note at `internal/mcp/transport.go:137-145`. Add the opt-in the note parks: `EnvAllowlist
*[]string` on `mcp.ServerConfig` (`internal/mcp/transport.go:63-82`) and
`EnvAllowlist *[]string \`yaml:"env-allowlist"\`` on `mcpServerConfig`
(`internal/config/config.go:2140-2150`), mapped in `toServerConfig` (`:2152-2163`). The pointer is
load-bearing (owner call): nil ⇒ today's behaviour byte-for-byte — `cmd.Env` left unset, or
`cmd.Environ()` plus `cfg.Env` when `env:` is set; non-nil ⇒
`cmd.Env = append(host.ScopeEnv(workspaceRoot, *cfg.EnvAllowlist, nil), cfg.Env...)` at
`internal/mcp/transport.go:154-158`, so `env:` entries still win last, an explicit `[]` yields the
platform floor alone, and a named list gets PATH scoped to the workspace exactly as `safeGitEnv`
(`internal/tools/git.go:77-90`) and `goVetEnv` do. Reach the helper through a package-level
`var stdioHost platform.Host = platform.Current()` (the `internal/tools/exec_common.go:582` idiom);
`internal/mcp` already imports `platform`, and `ScopeEnv`'s nil lookup means no `os` import. `mcp-servers`
is a `KindStructured` registry row, so no new `KeyRegistry` row is needed — confirm the bijection test
still passes. Docs and the template are item 18's.

**Files:** `internal/mcp/transport.go`, `internal/config/config.go`, `internal/config/config_test.go`,
`internal/mcp/mcp_test.go`

**Tests.** Unit tests over the `*exec.Cmd` `buildStdioTransport` returns (the model is
`TestBuildStdioTransport_CancelArmsTheCmdsTeardown`, `internal/mcp/mcp_test.go:409`): nil allowlist ⇒
`cmd.Env` nil with no `env:`, and `cmd.Environ()+env` with one; a named allowlist ⇒ only those keys
(plus the platform floor) survive and the `env:` entries are last; an explicit `[]` ⇒ the floor alone.
Plus one end-to-end connect through `stdioServerConfig` (`:138-152`) with `EnvAllowlist: &[]string{}`
proving the fixture still launches (its selector rides `cfg.Env`, and the command is an absolute
`os.Executable()`). Config side: a parse round-trip for absent, empty and named lists.

**Acceptance.** `go build ./... && go test ./internal/mcp/ ./internal/config/`

`feat(mcp): an optional per-server env-allowlist for stdio launches`

---

## 18. Document `env-allowlist` and narrow the trust note

**Depends on item 17.**

**What.** The trust note at `internal/mcp/transport.go:137-145` states the full-environment launch is
the only behaviour and points at `ISSUES.md` L4 as parked; rewrite it to state the default unchanged
and name the new key as the opt-in, dropping the ISSUES pointer. Add the key to the commented
`mcp-servers:` block in `internal/config/defaults/config.yaml:406-429`, beside the existing `env:` line
and in its comment style. `docs/manual/configuration.md` has no `mcp-servers:` section — create one in
the file's existing shape (`## <prose> — \`key:\`` plus a fenced `# ~/.apogee/config.yaml` example),
covering the whole block and the trust statement. That section is a documentation obligation, not a
test-enforced one — nothing compares the template's keys with the manual. Update the sentence in
`docs/design/mcp-client.md:57-59` ("Its environment is unchanged — the full process environment plus
`cfg.Env`, the deliberate trust decision above (`ISSUES.md` L4)") to name the opt-in and drop the
ISSUES reference.

**Files:** `internal/mcp/transport.go`, `internal/config/defaults/config.yaml`,
`docs/manual/configuration.md`, `docs/design/mcp-client.md`

**Regression guard.** Same correction as item 15: `cmd/apogee/docs_env_test.go` pins nothing between
the config template and `docs/manual/configuration.md` (it pins `APOGEE_*` names and three url-safety
strings), so this item has NO automated guard on its prose. `internal/config/defaults_test.go` remains
the guard that the template still parses to defaults, and nothing more.

**Tests.** None of its own, and no automated guard ties the manual to the template —
`internal/config/defaults_test.go` only pins that the template still parses to defaults.

**Acceptance.** `go build ./... && go test ./internal/config/ -run 'DefaultConfig'`

`docs(mcp): document env-allowlist and narrow the trust note`

---

## 19. Retire the closed entries from `ISSUES.md`

**Depends on items 1–18.**

**What.** `ISSUES.md` holds open work only — a resolved item is REMOVED and its record travels to
`CHANGELOG.md`. Delete the five residue sections' now-closed checkbox items and the two parked entries
this plan closed: from "Capped-delegate wrap-up — residue" the six bullets (items 1–4); from "Sub-agent
naming and seat choice — residue" all four (items 5–10); the whole "Readable, scoping `/inspect` —
residue" section (item 11), "Structural feedback and pruning — residue" (item 12) and "Breadcrumb, gauge
and the Tools umbrella — residue" (item 13); the `[P2] Retention / pruning policy` bullet under
"Session system follow-ons" (items 14–16); and the `[L4 enhancement]` bullet under "Deferred
security-review Lows" (items 17–18). A section left with no bullets goes entirely, heading and Status
line included; a section that keeps bullets keeps its heading. Leave every other entry untouched — in
particular the `load_skill` open defect, the driver-parity gaps and the hero tape's knob 3 stay. Add no
"done" narration and no closed-items section.

**Regression guard.** `grep -c '^- \[ \]' ISSUES.md` is 24 today because the Conventions legend line
`- [ ] New/Open Items not handled yet` (`ISSUES.md:22`) matches too, so removing this plan's 13
checkbox bullets leaves 11, not 10 — and the nearest way to make a "10" gate pass is deleting a line
that must stay. Acceptance expects 11: the legend (`:22`), the open `load_skill` defect (`:28`), the
hero-tape knob (`:699`) and the eight driver-parity gaps (`:774-804`).

**Files:** `ISSUES.md`

**Tests.** None — register hygiene.

**Acceptance.** `grep -c '^- \[ \]' ISSUES.md` returns 11 (the Conventions legend line, the one open
defect, the hero-tape knob and the eight driver-parity gaps);
`grep -n 'toolCallRun\|env-allowlist\|Retention / pruning' ISSUES.md` returns nothing.

`docs(issues): retire the entries this plan closed`

---

**Suggested version bump:** a patch/minor micro-bump is warranted once this lands — two behaviour fixes
plus two additive, default-off config keys (`sessions:`, `env-allowlist:`) make it a **minor** bump
under this repo's 0.x practice. Not performed by any item; the owner cuts it.

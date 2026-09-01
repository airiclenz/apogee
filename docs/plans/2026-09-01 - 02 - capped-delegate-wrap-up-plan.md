# A step-capped delegate gets one tool-less Turn to report back

**Goal:** a delegate stopped at `delegate-max-steps` is currently cut off mid-tool-round and the
parent is handed whatever text the child happened to narrate alongside its last tool call — often
`(no visible text)`. Give it exactly one further Turn with the tool menu withdrawn, told why its
tools are gone and asked to report to the agent that delegated the task, so the partial result the
parent reads is authored rather than scavenged.

**Date:** 2026-09-01 · **Status:** unexecuted · **sized for:** ~200k-context host

**Sources (pinned at `23ff876b`):**
- `internal/agent/agent.go:338` (`stepCapErrFormat`), `:560-597` (`Run`, `endAtStepCap`)
- `internal/agent/subagent.go:49-66` (markers), `:205-232` (`delegationResult` StepCapped branch)
- `internal/agent/loop.go:212-229` (`step` tool-call branch), `:838-874` (`buildRequest`), `:1340-1362` (`toolMenu`)
- `CONTEXT.md` "Step cap"; ADR 0006 (Bypass floor), ADR 0013, ADR 0063

**Ratified design calls (owner, 2026-09-01):**
- **Cap math:** the wrap-up Turn is EXTRA and uncounted — `delegate-max-steps: 3` still buys 3 working Turns plus one tool-less reply.
- **Directive:** the child is told the cause, the prohibition and the ask (report to the delegating agent, including what is unfinished) — exact text in item 2.
- **Announced strings:** the human-facing `stepCapErrFormat` middle clause becomes `— asking it to sum up;`; the parent-facing `stepCapResultFormat` is UNCHANGED (what follows it is still the child's last visible text).
- **Fallback:** a wrap-up Turn that faults, errors or answers with a tool call falls back to today's partial result; a cap is never reported to the parent as a failure.
- **Mailbox:** unchanged — messages queued at the cap boundary stay undelivered and are reported so; the wrap-up Turn is the engine's, not a delivery point.
- **Classification:** structural (ADR 0006), like the cap itself — no config key, on under Bypass, never withdrawn by Adaptive Suppression.

**Standing requirements:** `skills: coding-standards`.

**Out of scope:** the cap's default or its config surface; telling a child its cap up front; the
fault and cancel result paths; a `tool_choice` field on the provider wire; TUI rendering (the
result envelope the TUI words its slot from is unchanged).

**Regression check (2026-09-01, 23ff876b):**
- 1: premise corrected, guard folded — the `when:` refusal wording and its pin move with the new member.
- 2: guard folded (owner decision) — the tool-less request's re-rendered history is accepted, not worked around.
- 3: guard folded — `Faulted` cleared on the swallowed path, the `:257` pin moved here, all three cap-test sites named; supersedes `turn.go:152`'s "the counter is left alone" reasoning, which the item rewrites.
- 4: guard folded — the second delegation's counts and the `.txt` golden path.
- 5: guard folded (owner decision) — the live child-Turn bound rises to `liveDelegateStepCap+1`.
- 6: guard folded — the finding rule and the Acceptance now use greps that fire.
- 2: rejected — "with `mechanisms: validate` on, a tool call in the wrap-up reply is an unknown-tool issue and validate returns ActionRetry"; `validateCall` raises that issue only `if len(tools) > 0` (`internal/mechanisms/validate.go:85`), so an EMPTY menu raises none.

## 1. `stubllm` gains a `when.system` matcher — ✅ DONE (2026-09-01)

NOTES (2026-09-01): consequential edit — internal/stubllm/script_test.go: made necessary by the new `Match.System` field — `TestScriptRoundTripsThroughYAML` pins that EVERY field survives both directions, so `System` joins the round-trip fixture's `Match` alongside the item's mandated refusal-wording pin move.

NOTES (2026-09-01): the refusal now reads "a when block sets last_message, tool_result, system, or any combination"; `Match`'s and `matches`'s doc comments moved from "both members" to "every member that is set", since the conjunction now spans three members.

NOTES (2026-09-01): `when.system` is compiled in `newMatcher` only, mirroring where the item says to compile it; `Match.validate` gained no `system:` compile check (it keeps the one `last_message` already had), so a bad `system:` regexp surfaces at construction with its turn index rather than at parse time.

**What.** A fixture cannot currently tell the wrap-up request from the Turn it follows — and NOT
because it "ends on the same tool result": the wrap-up request carries an EMPTY tool menu, so the
provider renders it with `hasTools=false` (`internal/provider/client.go:546`) and `formatMessage`
(`:693`) degrades the child's last tool result to a `user` message. `when.tool_result` therefore
cannot match the wrap-up request at all, and `when.last_message` sees text identical to the
preceding round's. That is why a third discriminator is needed, and `when.system` is the right one:
it matches the directive the engine actually announced on that request. `when:` today matches only
`last_message` and `tool_result`. Add
`When.System` — a regexp over the request's system messages, concatenated exactly as
`captures.from: system` already concatenates them (`systemText`, `internal/stubllm/log.go:104`) —
as a third optional member: all set members must match, and validation still refuses a `when:` block
that sets none. Compile it in `newMatcher` alongside `When.LastMessage` and report a bad regexp as
`stubllm: turn %d: when.system: %w`. Document it in the `when:` paragraph of
`docs/design/test-drivers.md:111`.

**Regression guard.** Accepting `system:` alone makes `Match.validate`'s refusal wording wrong — "a
when block sets last_message, tool_result, or both" (`internal/stubllm/script.go:344`), pinned
verbatim at `internal/stubllm/script_test.go:90`. Reword the refusal to name `system` too and move
that pin in this same commit.

**Files:** `internal/stubllm/script.go`, `internal/stubllm/match.go`, `internal/stubllm/match_test.go`, `internal/stubllm/script_test.go`, `docs/design/test-drivers.md`

**Tests.** A matcher test: two turns whose `when:` differ only in `system:` answer two requests that
differ only in their system text; a turn setting `system:` plus `tool_result:` matches only when
both hold; an uncompilable `system:` regexp fails at construction naming the turn index; a `when:`
block setting `system:` alone passes validation, while an empty `when: {}` still fails on a refusal
that now names all three members.

**Acceptance.** `go test ./internal/stubllm -count=1`

**Commit:** `feat(stubllm): when.system matches a request by its system text`

## 2. The tool-less wrap-up request shape — ✅ DONE (2026-09-01)

NOTES (2026-09-01): item's `%d` directive string is assembled from concatenated literals so the source line stays under the file's width; the rendered text is byte-identical to the plan's.

**What.** Give `Agent` a `wrapUp bool` latch (unexported, NOT serialized — transient like
`turns.exchangeTurns`) that makes ONE request tool-less and self-explaining. Three seams read it:

- `toolMenu()` (`loop.go:1340`) returns `nil` while latched, so `st.Tools` is empty; the wire seam
  already renders no tool-instruction block for an empty menu (`processing.InstructionsFor`) and
  sends no native array, which is what "tools withdrawn" means with no `tool_choice` on the wire.
- `buildRequest` stamps the directive through `req.AppendToSystem(wrapUpMarker, …)` at the same
  place and for the same reason `SetSampling` stamps the reply ceiling: after construction, before
  any pre-request hook, so it is the engine's own bound and holds under Bypass. `AppendToSystem`
  creates the system message when none exists, so a session with no configured prompt still carries
  it. Constants beside the other step-cap markers in `subagent.go`; `%d` is `a.stepCap`:

  `wrapUpMarker = "no further tool calls are possible"`

  `wrapUpDirectiveFormat = "You have reached the step limit for this delegation (%d steps) and no further tool calls are possible: the tools have been withdrawn for this final reply.\n\nReport back to the agent that delegated this task now: what you found, what you concluded, and what remains unfinished. This is your only remaining reply — anything you do not write here is lost."`
- `step()`'s branch at `loop.go:212` takes the final-answer exit while latched even when the reply
  carries tool calls: the calls are DROPPED undispatched (a withdrawn menu must not be reachable by
  a model that asks anyway), the assistant message is committed without them, and the Exchange ends
  through `endExchangeDone`. Commit nothing and emit no `MessageEvent` when the reply carries no
  text — the empty case is the fallback item 3 owns, and an empty assistant message would pollute
  the child's last visible text.

Nothing sets the latch yet; item 3 is its only writer.

**Regression guard.** Withdrawing the menu is the prohibition — it sets `hasTools=false` in the
provider (`client.go:546`), which re-renders the child's whole history for that ONE request (every
tool result becomes a `user` message; a tool-call-only assistant turn renders `content: null` with
its calls dropped). This is accepted, not a defect to work around: the tool OUTPUTS and any
narration the child wrote survive, which is what the report is written from, and text-format
profiles (markdown-fenced / custom-regex) already send exactly this shape on every request because
the wire seam suppresses their tools array. The alternative — keeping the menu and relying on the
engine's drop guard alone — was considered and rejected by the owner.

**Files:** `internal/agent/agent.go`, `internal/agent/loop.go`, `internal/agent/subagent.go`, `internal/agent/subagent_test.go`

**Tests.** With the latch set by hand (same package): the provider request carries zero tools and
its system text contains the directive with the cap's number; a reply of text + a tool call commits
the text, dispatches nothing (`fakeTool` records no call) and ends the Exchange; a reply of a tool
call with no text commits no assistant message and leaves `lastVisibleText()` empty; with the latch
CLEAR the request's tool menu and system text are byte-identical to today's.

**Acceptance.** `go build ./... && go test ./internal/agent -run 'WrapUp|SubAgent|ToolMenu' -count=1`

**Commit:** `feat(agent): a latched Turn sends no tools and says why`

## 3. `Run` asks the capped delegate for its closing report

Depends on item 2.

**What.** Split `endAtStepCap` (`agent.go:584`) into the ErrorEvent it emits and the Exchange it
closes, and put the wrap-up Turn between them in a new `finishAtStepCap(ctx, last)` that `Run`
calls in its place at `agent.go:573`:

1. Emit the cap `ErrorEvent` at the child's Depth as today, with `stepCapErrFormat`'s middle clause
   now reading `— asking it to sum up;` (head and tail unchanged, so the key that raises the bound
   still travels).
2. Latch `wrapUp`, run exactly one `a.step(ctx)`, unlatch on the way out. The Turn is NOT counted
   against the cap (`turns.exchangeTurns` is advanced only by `Run`'s loop, which this exit leaves)
   and cannot repeat: nothing loops here.
3. Route the outcome. A completed wrap-up returns that boundary with `StepCapped` forced true, so
   the parent still reads the partial marker rather than a finish that did not happen. A cancel
   returns as a cancel, exactly as a cancel inside any other child Turn does. A Go error or a
   faulted wrap-up is SWALLOWED: close the Exchange through the `endStepCapped` row when it is
   still open, and return a `StepCapped` boundary either way — `delegationResult`'s existing
   fallback then hands the parent today's result (`lastVisibleText()`, else `(no visible text)`).

`delegationResult` and `stepCapResultFormat` are untouched; only their comments gain the wrap-up.
Update the `endStepCapped` row's comment in `turn.go:152` — it is now reached after a tool-less
final Turn, not only after `endTurnDone`.

**Regression guard.** The swallowed path returns a boundary with `Faulted` CLEARED as well as
`StepCapped` set — `delegationResult` tests `res.Faulted` (`subagent.go:196`) BEFORE `res.StepCapped`
(`:211`), so a still-faulted boundary reaches the parent as the ERROR result the ratified fallback
forbids. Three cap tests move with the extra Turn: `subagent_test.go:1007` (`responder.calls`) and
`:1013` (`a.turns.index`) go cap→cap+1, and `TestSubAgent_StepCapMarksAWordlessDelegate`
(`:1080-1110`) needs one extra tool-call script per capped child fixture or the child eats its
`contentScript("parent done")` as the wrap-up. The `stepCapErrLead + " — returning what it has"` pin
at `cmd/apogee/e2e_delegation_test.go:257` moves in THIS commit (the file is listed below); T-04's
request counts follow in item 4, so this item's Acceptance stays on `./internal/agent`. This also
supersedes `turn.go:152`'s "the counter is left alone … an off-by-one a Snapshot stores and a resume
reads back" reasoning: the wrap-up Turn ends through `endExchangeDone`, which does `l.index++`
(`turn.go:104`), so a capped child now ends at cap+1 and the rewritten comment says so.

**Files:** `internal/agent/agent.go`, `internal/agent/turn.go`, `internal/agent/subagent.go`, `internal/agent/subagent_test.go`, `cmd/apogee/e2e_delegation_test.go`

**Tests.** Extend the cap tests at `subagent_test.go:975`: a child scripted to keep calling tools
makes `cap` tool requests plus ONE more, the extra request carries no tools, and the parent's
`ToolResult` is `IsError: false`, opens with `stepCapResultFormat` and ends with the wrap-up
reply's text; exactly one cap `ErrorEvent` is emitted (`countCapErrors`) and it says `asking it to
sum up`; a wrap-up that faults (a responder erroring on the extra request) still yields a non-error
`StepCapped` result carrying the pre-cap last visible text (its boundary's `Faulted` is false, or
`delegationResult`'s earlier `Faulted` case answers first); a lowered `max_steps` behaves the same.
The three cap-test sites the guard names move in this commit.

**Acceptance.** `go build ./... && go test ./internal/agent -run 'StepCap|WrapUp|Delegation' -count=1`

**Commit:** `feat(agent): a capped delegate gets one tool-less Turn to report back`

## 4. T-04 drives the closing report end to end

Depends on items 1 and 3.

**What.** Teach the T-04 fixture and test the new shape. In
`cmd/apogee/testdata/stubllm/delegate-cap.yaml` add a repeating turn matched by
`when: {system: 'no further tool calls are possible'}`, placed ABOVE the child's tool turns so it
wins the wrap-up request, answering with the child's closing report (a wording of its own, distinct
from `childFinalWords`, so the capped and uncapped runs stay tellable apart). In
`e2e_delegation_test.go`: the capped child now makes 4 requests (3 tool Turns + the wrap-up) at
`:212`, and the SECOND delegation's counts move with it — `before+3` becomes `before+4` at both
`:273` (the `WaitFor`) and `:276` (the assertion) — while the uncapped child still makes 4 and still
ends on `childFinalWords`; assert the closing report reaches the parent's conversation row's run
view AND that the child never ran a 4th tool. The `stepCapErrLead` assertion at `:257` already
carries the new clause, moved with item 3; leave it. Refresh
`t04-step-cap-block` only if the collapsed row actually moved (`-update`), and leave
`TestJudgeDelegationStepCap`'s oracles pointed at the same block.

**Regression guard.** `**Files:**` names the `.txt` frame, not a `.golden` one: no `.golden` file
exists — `tuitest.Golden` reads `testdata/frames/<name>.txt` (`internal/tuitest/golden.go:113`), and
the recorded frame is `cmd/apogee/testdata/frames/t04-step-cap-block.txt`.

**Files:** `cmd/apogee/e2e_delegation_test.go`, `cmd/apogee/testdata/stubllm/delegate-cap.yaml`, `cmd/apogee/testdata/frames/t04-step-cap-block.txt`

**Tests.** The two existing T-04 sub-tests, amended as above; the `max_steps: 50` edge still stops
at three tool Turns and still adds exactly one wrap-up request (so its child requests are
`before+4`).

**Acceptance.** `go test ./cmd/apogee -run 'TestE2EDelegationStepCap' -count=1`

**Commit:** `test(cmd): T-04 drives a capped delegate's closing report`

## 5. The live shakeout reads the closing report

Depends on item 3.

**What.** `internal/agent/live_delegate_cap_test.go` proves the cap against a real model; extend it
to prove the report. After the capped run, assert the parent's result carries the marker AND
non-empty text beyond it (a real model told to sum up says something), and that the extra request
the child made carried no tools. `liveParentGrowthCeiling` (4096) is the bound that says a capped
delegation stays cheap for the parent — keep it, and update its comment: what reaches the parent is
one marker line plus a report the child was asked to keep to its findings. Gated by
`APOGEE_LIVE_ENDPOINT` as the file already is; it skips without one.

**Regression guard.** Raise the child-Turn bound at `live_delegate_cap_test.go:271-273` to
`liveDelegateStepCap+1`: the wrap-up Turn is "extra and uncounted" only in `turns.exchangeTurns`. It
takes its own `turns.index` and emits its own non-maintenance `UsageEvent` (`loop.go:724`), so every
count derived from events or requests grows by one — the same arithmetic items 3 and 4 budget for at
the other count sites, not to be rediscovered there.

**Files:** `internal/agent/live_delegate_cap_test.go`

**Tests.** The file's own shakeout, extended — no new test file; `childTurns(events, 1)` is now
accepted up to `liveDelegateStepCap+1`.

**Acceptance.** `go vet ./internal/agent && go test ./internal/agent -run 'TestLive' -count=1` (skips without `APOGEE_LIVE_ENDPOINT`; the owner runs `make live-eval` for the live pass)

**Commit:** `test(agent): live shakeout asserts the capped delegate's closing report`

## 6. The documented promise

Depends on item 3.

**What.** `CONTEXT.md`'s "Step cap" entry currently ends the story at "followed by the child's last
visible text": say instead that the engine spends one further Turn with the tool menu withdrawn,
telling the delegate why and asking it to report to its orchestrator, and that this Turn is extra —
outside the cap — and falls back to the last visible text when it fails. Keep the entry's structural
classification (ADR 0006, enforced in `Agent.Run`) intact. `docs/manual/configuration.md:388-395`
gets the user-facing half in the same voice: apogee ends the delegation cleanly, but first asks the
sub-agent to sum up, so what your agent receives is a report rather than an interrupted sentence.

**Regression guard.** A `grep -rn "last visible text" CONTEXT.md docs/ layout.md README.md` rule
matches NOTHING at `23ff876b` — CONTEXT.md wraps the phrase across the line break (`:458` "the
child's last" / `:459` "visible text") — so it would pass on the untouched tree and locate neither
site. The rule and the Acceptance below replace it and accept on the NEW wording in both files.

**Files:** `CONTEXT.md`, `docs/manual/configuration.md`

**Tests.** None (prose). The rule for finding the two sites that name the old behaviour:
`grep -n "child's last$" CONTEXT.md` (`:458`) and `grep -n "marked as partial"
docs/manual/configuration.md` (`:394`).

**Acceptance.** `grep -n "tool menu withdrawn" CONTEXT.md` and `grep -n "sum up" docs/manual/configuration.md` each return a hit — the new wording is in both files — and neither file still ends the cap's story at the child's last visible text.

**Commit:** `docs(context): the step cap spends a tool-less Turn on a closing report`

---

**Suggested version bump:** a patch bump (`VERSION` micro) once items 1–6 land — a behaviour change
to what a capped delegation hands back, with no config surface. The owner decides; no plan item
touches `VERSION`.

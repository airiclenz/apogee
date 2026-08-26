# Delegate token runaway — step cap, child compaction, working window, honest accounting

**Goal:** a sub-agent run can no longer burn an unbounded number of tokens, and the
session records what its delegates actually spent.

**Date:** 2026-08-26 · **Status:** unexecuted · **sized for:** ~200k-context host

**Evidence (session `20260825T185936Z-0d2ba815`, `/code-audit` on `deepseek-v4-flash`,
advertised window 1,310,720):** coordinator 68 calls / 1.6M prompt tokens; **124 delegates,
5,524 calls, 1.037 billion prompt tokens** (lower bound — running delegates were not yet
saved). One lens delegate ran **633 steps** (346 distinct reads, 303 repo-wide greps) for a
10-file scope, context reaching **910K**, ~547K tokens re-sent per step ⇒ 346M tokens for
one delegation. Nothing in the engine bounded it: auto-compaction never fires inside an
Exchange (`ISSUES.md` "Mid-Exchange auto-compaction", parked 2026-07-05), no step cap exists
anywhere (`internal/agent/agent.go:455` `Run` loops until the model stops asking for tools),
every cost guard scales with the advertised window (`internal/mechanisms/toolresultcap.go:101`,
`internal/agent/compact.go:198`), and `meta.usage` in the session record counts the main
agent only (`internal/session/store.go:116`), so the sessions list said 1.7M when the truth
was 1.04B. Separately, one delegate's reply hit the output cap with visible text and was
accepted as its final answer (only an EMPTY capped reply faults today, `loop.go:518`).

**Authoritative sources:**
- `internal/agent/agent.go:455-462` (`Run`, the Exchange loop), `internal/agent/turn.go`
  (`turnLifecycle`, the `end()` rows), `internal/agent/subagent.go` (`runSubAgent`,
  `newChildAgent`), `internal/agent/compact.go:159-181` (`shouldAutoCompact`),
  `internal/agent/loop.go:493-530` (`reviewedOutcome`, `emptyReplyFault`),
  `internal/agent/loop.go:1113-1170` (`budget`, `maxOutputTokens`).
- `internal/config/registry.go:137-171` (the `Key` row + `KeyRegistry`; bijection with
  `fileConfig` enforced by `TestRegistryIsBijectionWithFileConfig`),
  `internal/config/config.go:1271-1289` (`ServerEntry`), `:1591` (`ResolveContextWindow`),
  `internal/config/defaults/config.yaml` (the seeded template), `docs/manual/configuration.md`.
- `internal/domain/config.go:289-312` (`ContextConfig`), `internal/domain/hooks.go:276`
  (`Budget`), `internal/domain/events.go:248-262` (`UsageEvent`).
- `internal/provider/wirejson.go:116-120` (`usageJSON`), `internal/provider/wire.go:154`
  (`Usage`), `internal/provider/stream.go:155-193` (usage pickup).
- `internal/session/store.go:116-133` (`Meta.Usage`, `Usage`), `internal/tui/sessionsave.go:45`,
  `cmd/apogee/wire_session.go:130,355`, `internal/tui/usage.go:173-261` (`/usage` rows, `usageSum`).
- ADR 0046 (output cap), ADR 0045 (sub-agent server / routing), ADR 0039 (fan-out),
  ADR 0013 (recursion point), ADR 0006 (structural floors vs Mechanisms).
- `CONTEXT.md` §"Turns and stepping" (:369 — Turn, Exchange, Step), §"Context and history"
  (:923 — Budget, Compaction), `**Sub-agent**` (:106).

**Ratified design calls (owner, 2026-08-26, via AskUserQuestion):**
1. Step cap is **delegates only**, key `delegate-max-steps`, **default 80**, `0` = off; the
   `sub_agent` tool gains an optional `max_steps` argument that can only LOWER the configured
   cap. On hit the child's Exchange ends cleanly (not faulted) and the parent receives a
   NON-error result: a step-cap marker line followed by the child's last visible text.
2. Turn-boundary auto-compaction under budget pressure is enabled for **child agents only**;
   the main loop keeps Exchange-boundary-only compaction (bench comparability untouched).
3. A new **`working-window`** key (top-level and per-server entry, like `context-window`;
   `0` = the advertised window) bounds the Budget every reducer and guard reads
   (allocation, `tool_result_cap`, compaction triggers). The advertised window still drives
   overflow detection and the output-cap derivation.
4. On a **child**, a reply that hits the output cap (`finish: length`) and carries NO tool
   call faults the run regardless of visible text; the parent gets an error result naming
   the cap. The main agent's behaviour is unchanged.
5. (Author, no user-visible alternative) the session record gains a `delegateUsage` sum
   beside `usage`; the sessions list shows the SESSION total (main + delegates). Cached
   prompt tokens are parsed from the OpenAI-shape `prompt_tokens_details.cached_tokens`
   and shown as their own `/usage` column; they are informational and change no budget.

**Standing requirements:**
- `skills: coding-standards`
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- No version identifier changes (see closing note).
- Every item's Acceptance is targeted; `make check` runs once at closeout.
- Config keys follow the registry contract: a new leaf key is a `fileConfig` field + a
  `KeyRegistry` row + a commented template line in `internal/config/defaults/config.yaml`
  + a `docs/manual/configuration.md` entry, or `TestRegistryIsBijectionWithFileConfig` fails.

**Out of scope:**
- A per-delegate TOKEN budget (steps are the lever that maps to what the model controls;
  tokens per step are the working window's job).
- Mid-Exchange compaction for the MAIN agent — the `ISSUES.md` entry stays parked for that
  half (item 4 rewrites it, does not remove it).
- The `/code-audit` skill itself (already fixed out-of-band 2026-08-26: 60-call budget,
  bounded out-of-scope reads).
- Headless per-run stderr accounting, cost/price estimation, provider-specific cache
  economics.
- Changing `parallel-agents` or fan-out width (ADR 0039).

---

## 1. `delegate-max-steps` config key → `domain.Config.Delegation.MaxSteps` — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the `fileConfig` leaf is a `*int`, not the plain int the item text names — an explicit `delegate-max-steps: 0` is the ratified spelling of "off", and a plain int cannot tell that value from an absent key (which resolves to 80). `KindInt` still describes it (the bijection walk derefs pointers), and the accessor is the `auto-compact` pointer idiom.
NOTES (2026-08-26): of the four `domain.Config` sites the item names, only `cmd/apogee/wire_boot.go` and `cmd/apogee/wire_firing.go` build one — the prescribed `grep CompactionEnabled:` finds exactly those two; `cmd/apogee/upstream.go` builds an `UpstreamSpec` and `cmd/apogee/wire_server.go` a `ServerEntry`, neither of which carries a `Delegation` field. Both real sites set it.
NOTES (2026-08-26): two files outside the item's list changed because the repo's own guards require it — `apogee.go` gained the `DelegationConfig` type alias the composition root references (the root package re-exports every domain construction type), and `cmd/apogee/wire_settings.go` gained a `settingsTable` entry, since `TestEveryEditableSettingKeyHasAnApply` fails for an `Editable` key with no apply. That apply takes `ui.inspector`'s write-alone posture (no engine seam exists yet), so the registry `Desc` ends with the startup-only contract sentence the pane header shows.

**What:** plumb the key, nothing enforces it yet.
- `internal/domain/config.go`: add `type DelegationConfig struct { MaxSteps int }` (doc
  comment: "Turns a child agent may take in its one Exchange before the engine ends it;
  0 = unbounded") and a `Delegation DelegationConfig` field on `Config`, next to `Context`.
- `internal/config/config.go` (`fileConfig`) + `internal/config/options.go` (`Options`):
  a top-level `delegate-max-steps` leaf (int). `internal/config/registry.go`: one row —
  `Path: "delegate-max-steps", Kind: KindInt, Default: "80", Editable: true`, a `Validate`
  that refuses negatives (mirror `validateContextWindow`'s shape), `Desc` in the style of
  the neighbouring rows, `Read: strconv.Itoa(o.DelegateMaxSteps)`. Env/flag naming follows
  the registry's existing derivation for editable top-level keys.
- Map `Options` → `domain.Config.Delegation.MaxSteps` at every site that builds a
  `domain.Config` from options: `cmd/apogee/wire_boot.go` (~:245), `cmd/apogee/wire_firing.go`
  (~:237), `cmd/apogee/upstream.go` (~:262), `cmd/apogee/wire_server.go` (~:43) — grep for
  `CompactionEnabled:` to find them all; every one of them sets the new field.
- `internal/config/defaults/config.yaml`: a commented `# delegate-max-steps: 80` line with
  a two-line explanation, placed after `auto-compact`.
- `docs/manual/configuration.md`: an entry after `auto-compact` (:109) — what it bounds,
  the default, `0` = off, and that `sub_agent`'s `max_steps` can only lower it (item 2
  ships that argument; the doc sentence lands here so the key has exactly one owning entry).

**Files:** `internal/domain/config.go`, `internal/config/config.go`,
`internal/config/options.go`, `internal/config/registry.go`,
`internal/config/defaults/config.yaml`, `cmd/apogee/wire_boot.go`,
`cmd/apogee/wire_firing.go`, `cmd/apogee/upstream.go`, `cmd/apogee/wire_server.go`,
`docs/manual/configuration.md`, `internal/config/registry_test.go`,
`internal/config/config_test.go`

**Tests:** the registry bijection/invariant tests pass unchanged with the new row; a table
case in `registry_test.go` (`TestSettingKeyValidators*`) refuses `-1` and accepts `0`
and `80`; a `config_test.go` case loads a YAML with `delegate-max-steps: 12` and reads
`Options.DelegateMaxSteps == 12`; the default resolves to 80 when the key is absent.

**Acceptance:** `go build ./... && go test ./internal/config/ ./internal/domain/ ./cmd/apogee/`

**Commit:** `feat(config): delegate-max-steps key bounds a child agent's Exchange (default 80)`

---

## 2. Enforce the step cap in the child's Exchange loop; `sub_agent` gains `max_steps` — ✅ DONE (2026-08-26)

NOTES (2026-08-26): `internal/domain/config.go` is not on the item's Files list but had to change — the item mandates that "the returned `StepResult` carries a new `StepCapped bool`", and `domain.StepResult` is declared there. No other domain type moved.
NOTES (2026-08-26): the `endStepCapped` row deliberately does NOT call `tracker.endTurn()`, which `endExchangeDone` does. `Run` reaches the row only AFTER `endTurnDone` already judged the Turn that just completed, so a second judge would rotate the self-regulator's pending set against an emptied scratch and silently lose a judgment (R3). All three pieces of bookkeeping the item text enumerates — `closeExchange`, index advance, `StatusExchangeComplete`, and not `Faulted` — are present, and `turn_test.go` pins both the three and the absent re-judge.
NOTES (2026-08-26): `finalMessageText` gained a `lastVisibleText()` seam beneath it and the capped path reads that instead. The item text says the marker is followed by `sub.finalMessageText()` "(or the literal `(no visible text)` when empty)", but `finalMessageText` can never return empty — it substitutes "(sub-agent completed with no final message)", which claims a completion a capped child did not reach. The seam returns "" when the child produced no text, which is what the `(no visible text)` fallback needs; `finalMessageText`'s own behaviour is byte-identical.
NOTES (2026-08-26): CONTEXT.md's **Turn** entry named "its four exits (complete, Exchange-complete, abandoned, cancelled)" — the sentence describes the very table this item adds a fifth row to, so it now reads "its five exits (…, step-capped)". Beyond that, the only CONTEXT.md change is the **Step cap** term the item calls for.

Depends on item 1.

**What:**
- `internal/agent/agent.go`: `Agent` gains `stepCap int` (0 = none). `Run` (`:455-462`)
  counts the Turns of the current Exchange; when a `step()` returns `StatusTurnComplete`
  and the count has reached `stepCap`, `Run` ends the Exchange instead of looping:
  emit one `domain.ErrorEvent{Source: "loop"}` reading exactly
  `delegate stopped at its step cap (N steps) — returning what it has; narrow the task or raise delegate-max-steps`
  (N = the cap), then close the Exchange through a NEW `turnLifecycle.end` row
  `endStepCapped` in `internal/agent/turn.go` — same bookkeeping as `endExchangeDone`
  (closeExchange, index advance, `StatusExchangeComplete`), **not** `Faulted`, and the
  returned `StepResult` carries a new `StepCapped bool`. The counter resets at every
  `openExchange`. The cap is checked AFTER the Turn's tool calls have been dispatched and
  their results appended (that is what `StatusTurnComplete` means), so the history the
  parent snapshots is alternation-clean.
- `internal/agent/subagent.go`: `newChildAgent` seeds `stepCap` from
  `cfg.Delegation.MaxSteps`; `runSubAgent` parses the optional `max_steps` argument
  (`internal/tools/sub_agent.go` schema + `SubAgentArgs.MaxSteps int`, described as
  "optional; lower cap for this delegation only; ignored when 0 or above the configured
  cap") and applies `min(configured, requested)` when both are > 0. On `res.StepCapped`
  the parent's tool result is **not** an error: `Content` =
  `[delegate stopped at its step cap (N steps); partial result — its last visible text follows]`
  + `"\n"` + `sub.finalMessageText()` (or the literal `(no visible text)` when empty).
- A routed child (ADR 0045) takes the SAME cap — the key is top-level, not per-server.
- `CONTEXT.md` §"Turns and stepping": add a `**Step cap**` term after `**Step**` (:390):
  the delegate-only Turn bound, its default, the marker result, and that it is structural
  (ADR 0006 — not a Mechanism, never withdrawn under Bypass).
- `docs/manual/configuration.md`: no change (item 1 owns the entry).

Binding standards: the cap lives in ONE place (`Run`), never in `step()`; the marker
strings are package constants with tests pinning their text; no new goroutines.

**Files:** `internal/agent/agent.go`, `internal/agent/turn.go`,
`internal/agent/subagent.go`, `internal/tools/sub_agent.go`, `internal/agent/subagent_test.go`,
`internal/agent/turn_test.go`, `internal/tools/sub_agent_test.go`, `CONTEXT.md`

**Tests:** `subagent_test.go` — a scripted child that asks for a tool every Turn is ended
at cap 3 with one ErrorEvent, `StepCapped` true, the parent receives the marker result
(non-error) and the parent Turn continues; cap 0 never ends it; `max_steps: 2` lowers a
configured 3 to 2, `max_steps: 9` does not raise it; the main agent (depth 0) is never
capped even with the key set. `turn_test.go` — `endStepCapped` clears `inExchange`,
advances the index, is not Faulted. `sub_agent_test.go` — schema lists `max_steps`.

**Acceptance:** `go build ./... && go test ./internal/agent/ ./internal/tools/`

**Commit:** `feat(agent): child Exchange ends at the step cap with a partial-result marker; sub_agent max_steps`

---

## 3. Auto-compaction at Turn boundaries for child agents — ✅ DONE (2026-08-26)

NOTES (2026-08-26): `autoCompact` gained one line the item text does not name — `a.turns.reanchorAfterShrink(res.Before - res.After)` after a fold that RAN — because the item makes the estimate-driven fold a third mid-Exchange conversation shrink, and the two existing ones each repair the cached `exchangeStart` (loop.go's S2 repair after a history rewrite, `anchorAtBridge` after the emergency fold). Without it the stale boundary sits BELOW the protected prefix and `AbortExchange` drops the first user message together with the summary — pinned by the new guard test, which fails with `conv.Len() = 0, want 1` when the line is removed. It is a no-op outside an Exchange, so the main agent's path is byte-identical.
NOTES (2026-08-26): the item names only `shouldAutoCompact`'s doc comment, but `autoCompact`'s and `emergencyFold`'s both asserted that the emergency fold is the ONE fold that may run mid-Exchange — false the moment a child folds there. Both were amended to say "on the MAIN agent" and to point at `midExchangeCompaction`; no behaviour rides on either comment.
NOTES (2026-08-26): CONTEXT.md's **Compaction** entry took the one added sentence the item asks for plus a two-clause repair of the sentences around it ("the only fold allowed to run mid-Exchange" → "… on the main agent"; "opts out of both" → "opts out of all of them"), which the added sentence would otherwise contradict on the same line.
NOTES (2026-08-26): the alternation assertion the item asks for needs the request the child sent AFTER its fold, so the shared `scriptedCompactResponder` gained a `requests` field rather than a second near-identical responder being added to `subagent_test.go`; the post-fold request is found by the canned summary text and checked with the package's existing `assertRequestTemplateLegal`.

**What:**
- `internal/agent/compact.go` `shouldAutoCompact` (:159-181): the `inExchange` gate (:170)
  is skipped when `a.midExchangeCompaction` is true. Everything else — the live
  `auto-compact` gate, `historyExceedsAllocation`, `compactSat` latching, the
  `a.compacting` guard — applies unchanged. The fold still runs at the top of `step()`
  (a quiescent Turn boundary: the previous Turn's tool results are already appended, so
  `internal/context/compact.go`'s alternation shaping holds; `compact.go:193`'s post-fold
  alternation assertion stays as the guard).
- `internal/agent/subagent.go` `newChildAgent`: set `midExchangeCompaction = true` on
  every child (routed or not). The main agent never sets it; there is no config key —
  this is the child's structural contract, not a Mechanism.
- `ISSUES.md` "Mid-Exchange auto-compaction": REWRITE the entry (do not delete): the
  child half is shipped (this item); what remains parked is the MAIN loop's Turn-boundary
  fold, still needing its own grill + bench evidence. Keep it under three sentences plus
  the existing rationale.
- `CONTEXT.md` `**Compaction**` (:1085): one added sentence — a child agent also folds at
  quiescent Turn boundaries under pressure; the main agent only at Exchange boundaries.
- Doc comment of `shouldAutoCompact` amended to name the child exception.

**Files:** `internal/agent/compact.go`, `internal/agent/subagent.go`,
`internal/agent/autocompact_guard_test.go`, `internal/agent/subagent_test.go`, `ISSUES.md`,
`CONTEXT.md`

**Tests:** `autocompact_guard_test.go` — the inExchange guard test (:69) gains a twin: with
`midExchangeCompaction` set and history over allocation, the fold fires mid-Exchange
exactly once and `compactSat` semantics are unchanged; the main-agent case still refuses.
`subagent_test.go` — a child whose scripted tool results push history past its
allocation folds mid-run, alternation holds (`compact.go:193` assertion never trips) and
the run completes; a child with `auto-compact` off never folds.

**Acceptance:** `go build ./... && go test ./internal/agent/ ./internal/context/`

**Commit:** `feat(agent): child agents auto-compact at Turn boundaries under budget pressure`

---

## 4. `working-window` config key (top-level + per-server entry) → `ContextConfig.WorkingWindow`

**What:** plumbing only; the Budget split is item 5.
- `internal/config/config.go`: `fileConfig` top-level `working-window` leaf; `ServerEntry`
  gains `WorkingWindow int \`yaml:"working-window"\`` (:1285 neighbourhood); a resolver
  `ResolveWorkingWindow(entry, top int) int` with exactly `ResolveContextWindow`'s
  precedence (entry ≥1 wins, else top-level ≥1, else 0). Validation: a negative value is
  refused like a negative `context-window` (same validator shape); a `working-window`
  larger than a pinned `context-window` on the same entry is refused with a one-line reason.
- `internal/config/options.go`: `Options.WorkingWindow`.
- `internal/config/registry.go`: row `Path: "working-window", Kind: KindInt, Default: "0",
  Editable: true, Validate: validateWorkingWindow, Read: strconv.Itoa(o.WorkingWindow)`,
  placed right after `context-window` (:355).
- `internal/domain/config.go` `ContextConfig`: `WorkingWindow int` (doc: "soft ceiling the
  Budget and every reducer read; 0 = the advertised window").
- `cmd/apogee/wire_server.go` (:126 neighbourhood): `cfg.Context.WorkingWindow =
  config.ResolveWorkingWindow(entry.WorkingWindow, cfg.Context.WorkingWindow)`; the other
  `domain.Config` builders (`wire_boot.go`, `wire_firing.go`, `upstream.go`) carry the
  option through the same way they carry `MaxContextTokens`.
- `internal/config/defaults/config.yaml`: commented `# working-window: 0` after
  `context-window` (top-level) and in the server-entry block, with a three-line
  explanation ("models advertising very large windows make every guard expensive;
  bound the working room here").
- `docs/manual/configuration.md`: entry after `context-window` (:115) and a line in the
  servers section (:228ff) — including the concrete advice: on a 1M-window model set
  `working-window: 200000`.

**Files:** `internal/config/config.go`, `internal/config/options.go`,
`internal/config/registry.go`, `internal/config/defaults/config.yaml`,
`internal/domain/config.go`, `cmd/apogee/wire_server.go`, `cmd/apogee/wire_boot.go`,
`cmd/apogee/wire_firing.go`, `cmd/apogee/upstream.go`, `docs/manual/configuration.md`,
`internal/config/registry_test.go`, `internal/config/config_test.go`

**Tests:** registry bijection/invariants pass; validator table refuses `-1`, accepts `0`;
`ResolveWorkingWindow` precedence table (entry wins, top-level fallback, both 0 → 0);
config load refuses `working-window: 300000` beside `context-window: 200000` on one entry;
`cmd/apogee` wiring test asserts the resolved value reaches `cfg.Context.WorkingWindow`.

**Acceptance:** `go build ./... && go test ./internal/config/ ./internal/domain/ ./cmd/apogee/`

**Commit:** `feat(config): working-window key bounds the room the Budget hands its reducers`

---

## 5. Budget split: `ContextLimit` = working room, new `Budget.Window` = advertised window

Depends on item 4.

**What:**
- `internal/domain/hooks.go` `Budget` (:276): add `Window int` — "the model's advertised
  context window (n_ctx); what overflow detection and the reply cap derive from".
  `ContextLimit` keeps its name and its readers but its meaning becomes "the working
  ceiling": `min(Window, WorkingWindow)` when `WorkingWindow > 0`, else `Window`. Update
  the field docs accordingly (`:276-282`).
- `internal/agent/loop.go` `budget()` (:1113): compute `window := cfg.Context.MaxContextTokens`,
  `limit := window; if ww := cfg.Context.WorkingWindow; ww > 0 && (window == 0 || ww < window)
  { limit = ww }`; `Allocate(limit, …)`; `Budget{Window: window, ContextLimit: limit, …}`.
  `requestExceedsWindow` (:901) measures against `Window − ResponseReserve` (the hard
  room) — it must not fold at the soft line (decision 7: a fold fires only when the request
  cannot fit). `maxOutputTokens()` (:1158) derives from the reserve of the WORKING room
  (the reserve already comes from `Allocate(limit)`), so a 200K working window on a 1.3M
  model yields a sane cap instead of the 32K ceiling every time — document this in the
  function comment.
- `internal/agent/subagent.go` routed block (:262): copy `target.WorkingWindow` the same
  way `ContextWindow` is copied (a `DelegationTarget` field added in
  `internal/agent/delegationtarget.go`, sourced from the entry via the existing
  `cmd/apogee` delegation wiring — grep `ContextWindow` in `cmd/apogee/delegation*.go` /
  `wire_server.go` for the two sites).
- The `UsageEvent.ContextWindow` stamp and the TUI fill gauge keep reporting the ADVERTISED
  window (no change) — the gauge answers "how full is the model's window".
- `internal/mechanisms/toolresultcap.go` and `guideddecomposition.go`, `library.go` read
  `ContextLimit` unchanged and thereby now honour the working window — add one sentence to
  `toolresultcap.go:94`'s comment.
- `CONTEXT.md` `**Budget**` (:928): two sentences on Window vs working ceiling and the
  `working-window` key.

**Files:** `internal/domain/hooks.go`, `internal/agent/loop.go`,
`internal/agent/subagent.go`, `internal/agent/delegationtarget.go`,
`cmd/apogee/delegation.go`, `internal/mechanisms/toolresultcap.go`, `CONTEXT.md`,
`internal/agent/budget_test.go`, `internal/agent/loop_test.go`,
`internal/agent/delegationtarget_test.go`, `internal/mechanisms/toolresultcap_test.go`

**Tests:** `budget_test.go` — with `MaxContextTokens: 1_310_720, WorkingWindow: 200_000`:
`Window == 1_310_720`, `ContextLimit == 200_000`, allocation sums to `200_000 −
ResponseReserve`; with `WorkingWindow: 0` both equal the advertised window; a working
window ABOVE the advertised one is ignored. `loop_test.go` — `requestExceedsWindow` does
not fire for a 400K request under a 200K working / 1.3M advertised pair; the derived
output cap under that pair sits inside `[minOutputTokenCap, maxOutputTokenCap]` and is
below 32768. `toolresultcap_test.go` — `capMaxChars` shrinks with the working window.
`delegationtarget_test.go` — a routed target's `working-window` reaches the child.

**Acceptance:** `go build ./... && go test ./internal/agent/ ./internal/mechanisms/ ./internal/domain/ ./cmd/apogee/`

**Commit:** `feat(agent): Budget distinguishes the advertised Window from the working ContextLimit`

---

## 6. Fault a child's capped reply that carries no tool call

**What:**
- `internal/agent/loop.go` `reviewedOutcome` (:493) / `emptyReplyFault` (:518): when
  `a.depth > 0` (a child), `resp.FinishReason() == domain.FinishLength` and the reply has
  no tool calls, fault the Turn even if visible text is present. New constant
  `cappedDelegateReplyErrFmt`:
  `delegate's reply hit the output cap apogee set (%d tokens) — a truncated answer is not a result; narrow the task or raise max-output-tokens: for this server`.
  The existing empty-reply path and wording stay for depth 0. The fault ends the child's
  Exchange through `endAbandoned` exactly as today, so `runSubAgent` (:126) converts it
  into the "sub-agent faulted" error result the parent already understands — extend THAT
  message to append the preceding error's text after a `: ` so the parent model reads the
  cause without scrolling (`errorToolResult(call.ID, "sub-agent faulted before finishing
  the delegated task: <cause>")`; the cause is the last loop ErrorEvent of the child run,
  which `runSubAgent` already observes via the child's event stream).
- A capped reply WITH tool calls is unchanged (the tools run; the loop continues).
- ADR 0046: dated addendum (≤ 6 lines) recording the child rule and why (a truncated
  delegate answer flowed back to a coordinator as a 223K-character "result" on 2026-08-25).
- `docs/manual/configuration.md` `max-output-tokens` entry (:131): one sentence on the
  child rule.

**Files:** `internal/agent/loop.go`, `internal/agent/subagent.go`,
`internal/agent/emptyreply_test.go`, `internal/agent/subagent_test.go`,
`docs/adr/0046-the-engine-bounds-every-reply-with-an-output-cap.md`,
`docs/manual/configuration.md`

**Tests:** `emptyreply_test.go` — depth-1 agent, scripted `finish: length` reply with text
and no tool calls ⇒ one ErrorEvent with the new wording, `StatusExchangeComplete`,
`Faulted`; depth 0 with the same reply ⇒ no fault (unchanged assertion). `subagent_test.go`
— the parent's tool result is `IsError` and its content ends with the child's cause text;
a capped child reply WITH tool calls continues the run.

**Acceptance:** `go build ./... && go test ./internal/agent/`

**Commit:** `fix(agent): a child's output-capped reply without a tool call faults instead of posing as its answer`

---

## 7. Cached prompt tokens: provider parse → `UsageEvent` → tally

**What:**
- `internal/provider/wirejson.go` `usageJSON` (:116): add
  `PromptTokensDetails *struct{ CachedTokens int \`json:"cached_tokens"\` } \`json:"prompt_tokens_details,omitempty"\``.
  `internal/provider/wire.go` `Usage` (:154): `CachedPromptTokens int`. The streaming
  pickup (`stream.go:155-193`) and the non-streaming path carry it through; absent ⇒ 0.
- `internal/domain/events.go` `UsageEvent` (:248): `CachedPromptTokens` and
  `CumulativeCachedPromptTokens`. `internal/agent/agent.go` `usageTally.record` (:301)
  sums it like the others. Doc comment: informational — the Budget never reads it (a cache
  hit is still context the model reads; only the bill differs).
- Headless (`cmd/apogee/headless.go` ~:538, the per-run usage line): append
  `cached=<n>` only when `> 0`.

**Files:** `internal/provider/wirejson.go`, `internal/provider/wire.go`,
`internal/provider/stream.go`, `internal/provider/client.go`, `internal/domain/events.go`,
`internal/agent/agent.go`, `cmd/apogee/headless.go`, `internal/provider/stream_test.go`,
`internal/agent/usagetally_test.go`, `cmd/apogee/headless_test.go`

**Tests:** `stream_test.go` — a usage chunk with `prompt_tokens_details.cached_tokens: 1200`
surfaces `CachedPromptTokens == 1200`; one without it yields 0 (round-trip test :53
extended, not duplicated). `usagetally_test.go` — cumulative cached sums across two
readings; a child's tally starts at zero (`TestSubAgentUsageIsChildLocal` extended).
`headless_test.go` — the usage line shows `cached=` only when non-zero.

**Acceptance:** `go build ./... && go test ./internal/provider/ ./internal/domain/ ./internal/agent/ ./cmd/apogee/`

**Commit:** `feat(provider): parse prompt_tokens_details.cached_tokens into the usage tally`

---

## 8. Session record: `delegateUsage` sum + cached column; sessions list shows the session total

Depends on item 7 (the `session.Usage` shape and the tui `usageTotals` shape both change here, once).

**What:**
- `internal/session/store.go` `Usage` (:127): add `CachedPromptTokens int \`json:"cachedPromptTokens,omitempty"\``.
  `Meta` (:123): add `DelegateUsage Usage \`json:"delegateUsage,omitzero"\`` — "the sum of
  the latest reading of every sub-agent run head at Save; zero when no delegate spent".
  Doc :116-122 amended: `Usage` stays main-agent-only; the session total is
  `Usage + DelegateUsage`.
- `internal/tui`: `usageTotals` (the type behind `model.go:342` / `transcript.go:189`)
  gains `CachedPromptTokens`; `transcript.applyUsage` (:835) and `foldStats` (`fold.go:104`)
  carry it; `usage.go`: the `/usage` pane gains a `cached` column between `prompt` and
  `completion` (blank when 0 for every row), `usageSum` (:255) sums it, and a new exported
  helper `(*Model).delegateUsageTotal() usageTotals` reuses `usageSubAgentRows`' walk.
  `sessionsave.go:45` passes both totals; `tui.go:48`'s `Save` signature gains
  `delegateUsage session.Usage`; `cmd/apogee/wire_session.go:130` stores it and `:355`
  restores it into `ResumedSession` (a resumed session's `/usage` total includes the
  restored delegate sum until live heads replace it — `sessions.go:574` neighbourhood).
- The sessions list (`internal/tui/sessions.go`, the row that renders `Meta.Usage` tokens):
  render `Usage.TotalTokens + DelegateUsage.TotalTokens` — the SESSION total — as one
  plain number; no second figure, no suffix.
- `docs/manual/sessions.md`: one sentence — the record stores main-agent and delegate
  spend separately; the list shows their sum. `docs/manual/commands.md:31` (`/usage`
  row): mention the cached column.

**Files:** `internal/session/store.go`, `internal/session/store_test.go`,
`internal/tui/usage.go`, `internal/tui/usage_test.go`, `internal/tui/transcript.go`,
`internal/tui/fold.go`, `internal/tui/model.go`, `internal/tui/sessionsave.go`,
`internal/tui/sessionsave_test.go`, `internal/tui/sessions.go`, `internal/tui/sessions_test.go`,
`internal/tui/tui.go`, `cmd/apogee/wire_session.go`, `docs/manual/sessions.md`,
`docs/manual/commands.md`

**Tests:** `store_test.go` — round-trip preserves `delegateUsage` and `cachedPromptTokens`;
a record without them decodes to zero (legacy test :172 extended). `usage_test.go` — two
delegate heads sum into the session row; the cached column appears only when non-zero.
`sessionsave_test.go` — Save receives the delegate sum of the transcript's heads.
`sessions_test.go` — the list row shows main + delegate total.

**Acceptance:** `go build ./... && go test ./internal/session/ ./internal/tui/ ./cmd/apogee/`

**Commit:** `feat(session): record delegate spend and cached prompt tokens; sessions list shows the session total`

---

## 9. Live-path shakeout against a large-window model (skippable without an endpoint)

Depends on items 2, 3, 5, 6.

**What:** a gated test, not a manual step. `internal/agent/live_delegate_cap_test.go`,
skipped unless `APOGEE_LIVE_ENDPOINT` is set (the repo's convention): spawn a child with
`delegate-max-steps: 5` and `working-window: 32768` on a task that invites many reads
("list every Go file under internal/ one read at a time and summarise each"); assert the
run ends with the step-cap marker within 5 Turns, that at least one `UsageEvent` from the
child reports `ContextWindow` = the advertised window while the child's Budget
`ContextLimit` = 32768, and that the parent's context grew by less than 4K tokens. Add the
test's name to `make live-eval`'s list if that target enumerates tests (check `Makefile`).

**Files:** `internal/agent/live_delegate_cap_test.go`, `Makefile`

**Tests:** the file itself; without the env var it skips (`go test -run LiveDelegateCap
./internal/agent/` prints SKIP).

**Acceptance:** `go vet ./internal/agent/ && go test -run LiveDelegateCap ./internal/agent/`

**Commit:** `test(agent): live shakeout for the delegate step cap and working window`

---

**Suggested version bump (not performed):** minor — `0.18.0`. Items 1, 4 and 8 add config
keys and a session-record field; items 2, 3 and 6 change a delegate's observable contract
(a run can now end at a cap, fold mid-run, or fault on a truncated reply). The bump is the
owner's call, after the run.

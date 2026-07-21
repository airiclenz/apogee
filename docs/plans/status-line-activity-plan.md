# Plan — Status line: a live activity status instead of the turn number

**Date:** 2026-07-21
**Status:** ready to implement — not started.
**Track:** post-`v1.4.0` TUI affordance + one **additive** Event variant. No freeze break:
per the CHANGELOG preamble (ADR 0001 §consequences as amended at the Phase-3 cut), a new
Event variant is a **minor** bump, not a breaking change.

**Authoritative sources (precedence):** if any item text disagrees with
[ADR 0011](../adr/0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md) (the TUI is
a thin renderer; the `Model` is value-copied) or `internal/tui/doc.go`, **the ADR and
`doc.go` win**. The status vocabulary and display decisions in "Decisions this plan
implements" are owner-ratified (2026-07-21) — implement them, do not re-litigate them; flag
`STATUS QUESTION` rather than deviate.

## Why

While a worker runs the status line shows `⣻ turn 5` (`internal/tui/model.go:1074`, fed by
`transcript.turn`, `internal/tui/transcript.go:29`). The turn index answers nothing the
human is actually asking — *is it reasoning, writing, running a tool, or stuck?* It is
replaced by a live activity phrase plus an elapsed clock:

```
⣻ thinking · 12s                                        16k 50% ██████
⣻ reading main.go · 3s                                  16k 50% ██████
⣻ running · npm test · 8s                               16k 50% ██████
⣻ responding · 4s · 42 tok/s                            16k 50% ██████
⣻ sub-agent · searching · 6s                            16k 50% ██████
```

One honesty problem blocks the nicest word in that set. Today the loop reveals **nothing**
while a model reasons: native reasoning deltas are accumulated silently
(`internal/agent/loop.go`, `case provider.DeltaThinking` in `streamResponse`) and inline
`<think>`/harmony spans are deliberately held off the live stream (`emitVisibleDelta`,
`loop.go:525`). So the TUI cannot tell "the server has not answered yet" from "the model is
reasoning hard", and `thinking` would be a guess. Items 1–2 make it a fact.

## Where things stand (grounded, verified 2026-07-21)

- **`statusLine`** (`internal/tui/model.go:1074`) builds `left` from `m.state`: running →
  `spinner + " " + "turn N" + throughputSuffix()`; the three blocked/errored states prefix
  their own words. `statusRight` (`:1104`) owns the gauge and the ctrl+c / flash hints — it
  is **not** touched by this plan.
- **`throughputSuffix`** (`:1064`) renders `· N tok/s` only at ≥ 1 tok/s. Keep as is.
- **`transcript.turn`** (`transcript.go:29`) is written by all eight cases of
  `transcript.apply` (`:85-110`) and read by exactly two places: `statusLine` and the
  redundant assertion at `e2e_test.go:430`.
- **Tool presentation** already exists: `toolPresenter` (`toolpresent.go:59`) and the open,
  name-keyed `toolRegistry` (`:74`) give every tool a friendly `label` and a `target`
  extractor; `presentToolCall` (`:192`) builds the `toolView` and falls back to the raw
  tool name for an unregistered (MCP) tool. `clipDetail` (`:400`) is the existing clipper.
- **In-flight tool calls are already knowable**: `addToolResult` (`transcript.go:186`) marks
  the matching call entry `done`, so an un-`done` `entryToolCall` is a running tool.
- **The spinner ticks at 10 fps while running** (`newBrailleSpinner`, `theme.go:71`), and
  the tick is re-armed after an approval (`model.go:458`) — an elapsed clock therefore
  re-renders for free, with no new `tea.Tick` plumbing.
- **`subAgentLabel = "sub-agent"`** already exists (`theme.go:65`) for the Depth > 0
  transcript rail — reuse that constant for the status prefix.
- **Reasoning is separable mid-stream**: `StripThinking` (`internal/processing/thinking.go:39`)
  routes an *unclosed* span's tail into `Reasoning` (`:56-59`), and `StripHarmony`
  (`harmony.go:65`) does the same for a trailing unterminated message (`:89-99`). Closed
  spans never change, so the reasoning accumulation is prefix-stable — the same property
  `emitVisibleDelta` already relies on for visible bytes.
- **Blast radius of a new Event variant is small**: the only type switches over
  `domain.Event` outside tests are `transcript.apply` (`transcript.go:85`, has a tolerant
  `default`) and `foldStats` (`model.go:1030`). `apogee.go:138-150` re-exports the variants.
- **`teaSink`** (`internal/tui/sink.go`) is lossless and async (`prog.send`); it needs no
  change for the extra event volume — the coalescing note there stays a future option.

## Decisions this plan implements (locked, owner-ratified 2026-07-21)

1. **Statuses are tool-aware, with the target**: `reading main.go`, `running · npm test`,
   `delegating · 3 sub-agents` — not a generic "working".
2. **`thinking` is honest**, backed by a new `ReasoningEvent` (items 1–2), not inferred
   from "no tokens yet".
3. **Idle renders nothing** in the left slot (the input box below already invites a
   message).
4. **Elapsed clock always while busy** (`thinking · 12s`), with `tok/s` keeping its place
   while responding.
5. **No new `uiState`.** `compacting` / `stopping` are activity kinds, not lifecycle
   states — the ADR 0011 state machine is untouched.

### The status vocabulary

| Phrase | Source signal |
|---|---|
| *(blank)* | `stateIdle` |
| `thinking` | running; request in flight, nothing revealed yet — **or** `ReasoningEvent`s arriving |
| `responding` | `TokenEvent` (visible text streaming) |
| `<verb> · <target>` | an open `ToolCallEvent` with no matching `ToolResultEvent` |
| `sub-agent · <phrase>` | any event with `Depth > 0` |
| `retrying` | `StreamResetEvent` (the loop re-streams the turn on `ActionRetry`) |
| `compacting` | the `/compact` worker (`startCompact`) |
| `stopping` | Esc pressed; cancel fired, worker not yet unwound |
| `approval needed` / `answer needed` | `stateAwaitingApproval` / `stateAwaitingAsk` (unchanged) |
| `error` | `stateErrored` (unchanged) |

---

## 1. Domain: the `ReasoningEvent` variant + facade alias — ✅ DONE (2026-07-21)

**What:** add to `internal/domain/events.go`, beside `TokenEvent`:

```go
// ReasoningEvent is one newly-revealed chunk of the model's reasoning channel …
type ReasoningEvent struct {
	EventBase
	Text string
}
```

Document it as **observation only**: it never changes history (the reasoning channel is
already preserved as `reasoning_content` by `assistantMessage`), it is emitted for both the
native reasoning channel and an inline `<think>`/harmony span, and its `Text` is untrusted
model output — any consumer that ever *displays* it must escape-strip exactly as
`transcript.appendToken` does (`transcript.go:118`). State plainly that a UI may use its
arrival alone as a liveness signal and ignore `Text`, which is what item 4 does.

Add `ReasoningEvent = domain.ReasoningEvent` to the alias block in `apogee.go:142`, keeping
the block's alignment.

**Tests:** none of its own beyond compilation — item 2 owns the behavioural tests. Confirm
`example_test.go`'s alias-presence pattern (`_ apogee.TokenEvent`, `:39`) still compiles.

**Acceptance:** `go build ./... && go vet ./...` clean; diff confined to
`internal/domain/events.go` + `apogee.go`. Commit:
`feat(domain): a ReasoningEvent variant for the model's reasoning channel`.

---

## 2. Engine: emit reasoning on both paths

**What:** in `internal/agent/loop.go`, `streamResponse`:

- **Native path** — in `case provider.DeltaThinking`, after the existing
  `thinking.WriteString(delta.Thinking)`, emit
  `domain.ReasoningEvent{EventBase: a.base(turn), Text: delta.Thinking}`.
- **Inline path** — add `emitReasoningDelta(turn int, acc string, reasoned int) int` as the
  exact mirror of `emitVisibleDelta` (`loop.go:525`), called from the same
  `case provider.DeltaContent` with a second counter local to `streamResponse`:

  ```go
  _, reasoning := a.stripper.Strip(acc)
  if len(reasoning) <= reasoned { return reasoned }   // the same prefix guard as the visible path
  a.cfg.Events.Emit(domain.ReasoningEvent{EventBase: a.base(turn), Text: reasoning[reasoned:]})
  return len(reasoning)
  ```

  Unlike `emitVisibleDelta` this must run **while** `IsMidChannel(acc)` is true — that is
  the whole point. The guard is what makes it safe against a non-monotonic strip result;
  never slice without it. Document the prefix-stability argument (unclosed span tails and
  closed spans, `thinking.go:56-59` / `harmony.go:89-99`) in the function's doc comment,
  matching the density of the comment above `emitVisibleDelta`.

**Nothing else changes:** the visible `TokenEvent` stream, `reply`, the committed assistant
message, and history must be byte-identical. This is purely a new observation.

**Tests:** extend the three existing tests in `internal/agent/streamsuppress_test.go` —
`TestStream_NativeIsByteIdentical`, `TestStream_DelimitedThinkingHeldOffLiveStream`,
`TestStream_HarmonyChannelsHeldOffLiveStream`:

- the emitted `TokenEvent` sequence is unchanged (the existing assertions stay verbatim);
- `ReasoningEvent`s now arrive on all three profiles — from `DeltaThinking` on the native
  one, from the held inline span on the other two;
- concatenating their `Text` reconstructs the reasoning the post-stream strip records, and
  no `ReasoningEvent` carries visible content;
- a start token split across two deltas (the recorded parity edge in `emitVisibleDelta`'s
  comment) does not panic and does not double-emit.

**Acceptance:** `go test ./internal/agent/... ./internal/processing/...` green; diff
confined to `internal/agent`. Commit:
`feat(agent): emit ReasoningEvent for native and inline reasoning channels`.

---

## 3. TUI: an active verb per tool in the existing registry

**What:** in `internal/tui/toolpresent.go`, add `verb string` to `toolPresenter` (`:59`) and
`Verb string` to `toolView` (`:45`), set by `presentToolCall` (`:192`). One verb per
registry entry — the per-tool knowledge stays in the one open, name-keyed registry rather
than growing a second parallel switch:

`read_file` → `reading` · `write_file` → `writing` · `list_dir` → `listing` · `grep` →
`searching` · `single_find_and_replace` / `multi_find_and_replace` / `edit_existing_file` →
`editing` · `view_diff` → `diffing` · `open_file` → `opening` · `terminal` → `running` ·
`python_exec` → `running python` · `git_branch` → `branching` · `git_commit` →
`committing` · `git_diff_range` → `diffing` · `diagnostics` → `checking` · `web_fetch` →
`fetching` · `http_request` → `requesting` · `web_search` → `searching the web` ·
`sub_agent` → `delegating` · `ask_user` → `asking`.

An unregistered tool (a dynamic MCP tool) falls back to `running <raw name>`, mirroring the
existing raw-label fallback. Verbs are lowercase present participles — the status line is a
sentence fragment, not a title.

**Tests:** extend `toolpresent_test.go`'s table (`TestPresentToolCall`) with the `Verb`
column, including the unregistered-tool fallback.

**Acceptance:** `go test ./internal/tui/...` green; diff confined to
`internal/tui/toolpresent.go` + its test. Commit:
`feat(tui): an active verb per tool in the presentation registry`.

---

## 4. TUI: the activity model and the new status line

**Depends on items 1 and 3.** This is the visible change.

**What (a) — a new `internal/tui/activity.go`,** pure and table-testable (no lipgloss, no
I/O — the `toolpresent.go` discipline):

```go
type activityKind int
const (
	actIdle activityKind = iota
	actThinking
	actResponding
	actTool
	actRetrying
	actCompacting
	actStopping
)

type activity struct {
	kind  activityKind
	label string    // actTool only: "<verb> · <clipped target>", or just the verb when there is none
	depth int       // > 0 → prefixed with subAgentLabel + " · "
	since time.Time // when THIS activity began — the elapsed clock
}

func (a activity) text() string                      // the status phrase, unstyled
func formatElapsed(d time.Duration) string           // "3s", "1m 04s"
```

`activity` is a plain value type — it is reached by the value-copied `Model`, so it must
never hold a `strings.Builder` or any self-pointer no-copy type (ADR 0011,
`internal/tui/doc.go`, `TestModelNoBuilderByValue`).

**What (b) — `foldActivity(e domain.Event) Model`** in the same file, called from the
`eventMsg` case in `Update` (`model.go:200`) right beside the existing `foldStats`:

| Event | Result |
|---|---|
| `ReasoningEvent` | `actThinking` |
| `TokenEvent` | `actResponding` |
| `StreamResetEvent` | `actRetrying` |
| `ToolCallEvent` | `actTool`, label from `presentToolCall(e.Call)` — `Verb` + `clipDetail`'d `Target` |
| `ToolResultEvent` | stay `actTool` while any un-`done` tool-call entry remains (a parallel batch); else `actThinking` |
| `MessageEvent` | `actThinking` (the loop may keep stepping; `finishWorker` decides idle) |
| `ErrorEvent`, `UsageEvent`, `AuditEvent`, `MechanismFiredEvent` | no change |

Rules that are easy to get wrong, so pin them:

- **`depth`**: any event with `Depth > 0` sets `depth`, rendering `sub-agent · reading
  main.go` via the existing `subAgentLabel` (`theme.go:65`). Depth returns to 0 naturally
  when the parent resumes.
- **Sticky `stopping`**: once `actStopping`, `foldActivity` must **not** overwrite it — the
  worker keeps emitting events until the quiescent boundary, and the human needs to see
  that the stop was registered. Only `finishWorker` clears it.
- **`since` resets only when `kind` *or* `label` changes** — a stream of `TokenEvent`s must
  keep one running clock, not restart it every chunk.

**What (c) — non-event transitions** set the activity directly: `submit` (`model.go:469`) →
`actThinking`; the `"compact"` branch of `runCommand` (`:556`) → `actCompacting`;
`stopWorker` (`:593`) → `actStopping`; `finishWorker` (`:613`) → `actIdle`.
`handleApprovalKey` (`:453`) returns to running and lets the next event re-derive the
phrase — no explicit set needed.

**What (d) — `statusLine`** (`model.go:1074`) becomes:

```go
case stateRunning:
	left = m.spinner.View() + m.th.statusBar.Render(" "+m.act.text()+" · "+formatElapsed(time.Since(m.act.since))) + m.throughputSuffix()
```

`stateIdle` renders an empty left slot; `stateAwaitingApproval` / `stateAwaitingAsk` /
`stateErrored` keep their current words, with **no** spinner and **no** clock. Clip the
tool target so `left` cannot crowd out the context gauge on a narrow window; the existing
`gap < 1` truncation stays as the floor. `statusRight` and `throughputSuffix` are untouched.

**Tests:** new `internal/tui/activity_test.go` —

- a table over `activity.text()`: every kind, `depth` 0 and 1, a tool with and without a
  target, the unregistered-tool fallback, and a target long enough to be clipped;
- `formatElapsed` boundaries (0s, 59s, 60s, 61s, 3600s+);
- a fold test driving a realistic sequence (reasoning → token → tool call → tool result →
  message) through `foldActivity`, asserting the phrase at each step, that `since` does
  **not** reset across consecutive `TokenEvent`s, that a `Depth: 1` event yields the
  `sub-agent · …` prefix, that a two-call batch stays `actTool` until the second result,
  and that events after `actStopping` do not overwrite it.

Plus, in `model_test.go`, a state-level test that the running view contains the phrase and
an elapsed suffix and that the idle view's left slot is empty.

**Acceptance:** `go test ./internal/tui/...` green — **including the one known
pre-existing break to fix: `TestModelStatusLine` (`model_test.go:826`) asserts `"turn"` in
the view; replace that expectation with the new phrase.** Diff confined to `internal/tui`.
Commit: `feat(tui): a live activity status with an elapsed clock in the status line`.

---

## 5. Retire the turn plumbing

**Depends on item 4** (nothing may still read it).

**What:** delete the `turn` field (`transcript.go:29`) and the eight `t.turn = e.Turn`
assignments in `transcript.apply` (`:85-110`), and adjust the `apply` doc comment, which
currently says each case "records the Turn index (it drives the status line)". Delete the
now-redundant assertion at `e2e_test.go:430` — the resumed turn index is already asserted
two lines above from `r2.TurnIndex` (`:428`), which is the authoritative source; keeping a
field alive for one test is dead state.

**Tests:** the existing suite is the test — no new ones.

**Acceptance:** `grep -rn "transcript.turn\|t\.turn" internal/` returns nothing;
`go test ./...` green. Diff confined to `internal/tui`. Commit:
`refactor(tui): drop the transcript turn counter with its last reader`.

---

## 6. Docs close-out (the one owning item for every doc edit)

**What:**

- `layout.md:29` — the sketch still shows `⣻ turn 5`; update it to the new form
  (`⣻ reading main.go · 3s`) so the layout reference matches what ships.
- `docs/design/technical-design.md:108` — "sealed `Event` + 8 variants" is already stale
  (there are 10: token, stream-reset, message, tool-call, tool-result, approval,
  mechanism-fired, error, usage, audit); correct the count **and** add reasoning → 11.
  Line `:160`'s variant list gets reasoning too.
- `CHANGELOG.md` — a new `[1.5.0]` section above `[1.4.0]`, **Added**: `domain.ReasoningEvent`
  (the observability seam for reasoning, re-exported on the facade) and the activity status
  line; **Removed**: the turn readout. Say plainly that this is additive/minor per the
  changelog's own Event rule and that the public facade only gains an alias.
- No new ADR: ADR 0001 already makes the Event set additively versioned, and ADR 0011's
  renderer contract is unchanged (no new lifecycle state, no agent logic in the TUI).

**Tests:** none (docs).

**Acceptance:** `go build ./... && go test ./...` green (docs-only diff, run as the final
gate for the whole plan); no `turn 5` left in `layout.md`. Commit:
`docs: record the activity status line and the ReasoningEvent variant`.

---

## Plan-wide gates

`go build ./...`, `go vet ./...`, `go test ./...` (plus whatever `make check` runs) after
every item. Two extra checks at the end of the plan:

1. **Manual walk-through** against the host endpoint (`http://192.168.64.1:1111`;
   `go run ./cmd/apogee`): ask something that needs a file read and watch the phrase walk
   `thinking → responding → reading <file> → thinking → responding` with the clock
   advancing; press Esc mid-run and confirm `stopping` appears and persists until the
   worker unwinds; run `/compact` and confirm `compacting`.
2. **Optional gated regression:** `APOGEE_LIVE_ENDPOINT=http://192.168.64.1:1111 go test
   -race -count=1 -run TestE2ELiveModel ./internal/tui/`.

## Explicitly NOT in this plan

- **Displaying reasoning text.** `ReasoningEvent.Text` is emitted and deliberately ignored
  by the TUI; a `/thinking` view or a collapsible reasoning block is a separate, additive
  feature that would also have to escape-strip the text on display.
- **Coalescing the event stream.** The extra `ReasoningEvent`s double the event rate on a
  reasoning-heavy model; `sink.go` already records coalescing (never dropping) as the
  option to take *if* queue pressure ever shows. Do not pre-optimise.
- **Progress detail inside a long tool call** (a percentage, a spinner per sub-agent) —
  needs tool-side signals that do not exist.
- **Reworking `statusRight`** (the gauge, the ctrl+c hint, the copy flash) or the footer.
- **Any change to the `uiState` machine**, to what the model receives, or to history.

## Critical files

**New:** `internal/tui/activity.go` (+ `activity_test.go`),
`docs/plans/status-line-activity-plan.md` (this file).
**Modified:** `internal/domain/events.go` (the variant), `apogee.go` (alias),
`internal/agent/loop.go` (both emission paths) + `internal/agent/streamsuppress_test.go`,
`internal/tui/toolpresent.go` (+ test), `internal/tui/model.go` (`Model.act`, the `eventMsg`
fold, `submit` / `runCommand` / `stopWorker` / `finishWorker`, `statusLine`) + `model_test.go`,
`internal/tui/transcript.go` (drop `turn`) + `internal/tui/e2e_test.go`, `layout.md`,
`docs/design/technical-design.md`, `CHANGELOG.md`.

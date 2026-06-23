# Handoff — ✅ Phase 1 COMPLETE (P1.6 + P1.7 landed); next is the live-model eval, then Phase 2

**Date:** 2026-06-23 · **Counter:** 11 · **Author:** session that landed P1.6 and P1.7.
**Supersedes:** [`10 - P1.2 turn-step-state-machine-complete`](2026-06-23%20-%2010%20-%20P1.2%20turn-step-state-machine-complete.md)
(archive it next session).

## Where we are

**Phase 1 is done — P1.0–P1.7 all landed.** The embeddable agent core is built and the bench
(apogee-sim) is re-armed on it. Status of record: [`docs/plans/phase-1-detail-plan.md`](../plans/phase-1-detail-plan.md)
(§ status banner = ✅ COMPLETE; §1 "all six hold"; §4 P1.6/P1.7 rows) and
[`docs/design/technical-design.md`](../design/technical-design.md) (Repo state, §5 Sessions row,
§8 #7, the next-session entry point). This session closed the last two tasks.

## What landed this session

### P1.6 — concrete v1 Session schema + restore `turnIndex` (apogee repo, committed `ee936b8`)

The documented P0.6 gap (resume re-zeroed `turnIndex`) is closed. Snapshot now serializes the
loop's **full** quiescent-boundary state, not just the message list.

- **New engine-state envelope** `internal/agent/state.go` (replaces the conversation-only
  `conversation.go`): `agentState{ Conversation, TurnIndex, InExchange, PendingInput }` wraps
  `domain.Conversation`. `Agent.encodeState` / `restoreState`; `Snapshot`/`resumeAgent` use them.
  Resume now **continues** an Exchange at the right Turn, rejects a mid-Exchange `Submit`, and
  keeps a `Submit`→`Snapshot`→`Resume` queued message.
- **`domain.Message` round-trips `Extra`** via its own `MarshalJSON`/`UnmarshalJSON`
  (`internal/domain/hooks.go`): known fields are canonical snake_case; unknown wire siblings
  (`reasoning_content`, `tool_choice`, …) flatten at the top level on encode and are collected
  back on decode. New `Message.WithExtra` builder; the loop now records the model's reasoning
  channel on the committed assistant message (`assistantMessage` in `loop.go`) — previously
  dropped. (Extra is recorded in history, NOT re-sent upstream — the provider seam drops it.)
- `Session.Version` future-version rejection kept. **The allow-for-session approval cache is
  deliberately NOT serialized** — re-confirmed on resume (the safer human-in-the-loop default).
- Tests (+7 funcs): `internal/agent/state_test.go` (turnIndex/inExchange/pendingInput
  continuation; reasoning_content survives end-to-end; future-version rejection),
  `internal/domain/session_test.go` (envelope encode/decode + rejection), `Message` Extra
  round-trip in `hooks_test.go`.

### P1.7 — point apogee-sim at the Go API (apogee-sim repo, **NOT committed** — see below)

New self-contained package **`apogee-sim/internal/coreagent`** drives the **real** library
through its **public API** (wired via `replace github.com/airiclenz/apogee => ../apogee` in
apogee-sim's `go.mod`):

- `Run(ctx, RunConfig)` constructs an `apogee.Agent` (Ask-Before mode, auto-approving writes)
  against an ephemeral `WorkspaceDir`, `Submit`s a file-edit task, `Step`s to the quiescent
  boundary until the Exchange completes, records every `apogee.Event` as a Go value, and reads
  the workspace back. `ScoreFileEdit(result, target, want)` judges the file. (`coreagent.go`,
  `score.go`.)
- **Hermetic acceptance** (`coreagent_test.go`, passing under `-race`): a scripted
  OpenAI-compatible `httptest` "model" drives the **real** provider client — the model asks for
  `write_file`, the loop dispatches it through Approval, the file lands in the sandbox, and the
  run scores a pass. This is the *same code path* a live model takes (the provider seam is
  internal; no fake is injected below the public surface — the only stand-in is the HTTP server).

## The live-model eval — the one open runtime step

A live model could not run from this build container (it does not route to the host's
services — connection refused sandboxed, timeout unsandboxed). The harness is ready: **point
`coreagent.RunConfig.Endpoint` at the local server `http://192.168.64.1:1111`** (OpenAI-compatible)
and run the file-edit task from the host. **Control the server** — switch/load a tool-capable
model, check status — via the **llama-launcher MCP control endpoint `http://192.168.61.1:7331/mcp`**.
These endpoints are documented in apogee-sim's `CLAUDE.md` (Running section + the `coreagent`
package entry). A real eval needs a model that emits native `tool_calls`.

## Commit state (READ THIS)

- **apogee repo:** committed to `main` (pre-production directive). `ee936b8` (P1.6 code+tests+docs)
  and this session's doc commit (plan/TDD/this handoff — Phase-1-complete).
- **apogee-sim repo:** **NOT committed.** apogee-sim carries the owner's unrelated in-progress
  pivot work (modified `cmd/`, `internal/sim`, untracked STRATEGIC-PIVOT handoffs). The P1.7
  changes are cleanly isolated — `go.mod` (the `replace` + `require`) and `internal/coreagent/`
  — so they can be staged and committed alone when the owner chooses, without touching the WIP.

## Verify gate (green at handoff)

- **apogee:** `gofmt -l .` empty · `go vet ./...` clean · `go build ./...` ok · `go test -race
  ./...` all pass · 6-target cross-build ok · `grep -rl '"github.com/airiclenz/apogee"' internal/`
  **empty** (ADR-0010 invariant) · `./apogee --help` exit 0 · zero new module deps (P1.6 added
  none — int counters, no `ulid`).
- **apogee-sim:** `go build ./...` ok with the `replace` · `go vet`/`gofmt` clean on
  `internal/coreagent` · `go test -race ./internal/coreagent/` pass.

## Judgment calls to ratify

1. **P1.6 serializes the full quiescent-boundary state** (turnIndex + inExchange + pendingInput),
   not just turnIndex — the robust, long-term-correct envelope. The approval cache is the one
   deliberate omission (re-confirm on resume). **Main call.**
2. **P1.6 v1 message schema is canonical snake_case with flattened Extra** (the OpenAI chat
   shape). `Session.State` is opaque engine storage, so this is free to be the clean shape;
   pre-production, no migration needed (no persisted snapshots in the wild).
3. **P1.7 acceptance is proven hermetically** (httptest model), matching P1.1's precedent. The
   live-model run is a config swap, not new code — it exercises the identical path. Treating
   Phase 1 as complete on the hermetic proof + ready harness is the call; the live run is the
   final runtime confirmation.
4. **apogee-sim was not committed** (owner WIP present). If you want it committed, stage only
   `go.mod` + `internal/coreagent/`.

## Next

1. **Live-model eval** (above) — the literal "a local model completes a file-edit task" run.
2. **Phase 2 — the TUI** (Bubble Tea), the first real consumer of the Phase-1 Events. The
   event stream, Approval gate, and snapshot/resume it needs are all in place.

## Suggested skills

- **`archive-handoffs`** — handoff 10 is superseded by this doc; retire it (and archive this one
  at the next session end).
- **`manage-llm-server`** / the MCP control endpoint — to load a tool-capable model before the
  live eval.
- **`run` / `verify`** — to drive the live `coreagent` eval against `192.168.64.1:1111` and
  confirm the file-edit deliverable end-to-end on a real model.

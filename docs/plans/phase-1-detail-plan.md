# Apogee — Phase-1 Detail Plan (P1)

**Date:** 2026-06-23 · **Status:** ✅ **COMPLETE** — all of P1.0–P1.7 landed; the
embeddable agent core is built and the bench is re-armed on it (P1.7). The one open
runtime step is the live-local-model eval run (the harness is wired and hermetically
verified — see §4 P1.7). This document remains the authoritative record of Phase-1
order & acceptance; Phase 2 (TUI) is the next phase.
**Parent:** [`implementation-plan-apogee-merge.md`](./implementation-plan-apogee-merge.md) §4
(Phase 1 is intentionally coarse there). **Design of record:**
[`../design/technical-design.md`](../design/technical-design.md) §5/§6/§8 P1 (#4–8) +
[ADR 0010](../adr/0010-package-layout-domain-core-and-thin-root-facade.md) (layout).
**Standing Requirements** (plan "⚠️ Standing requirements") apply to every task below — chiefly
**`/coding-standards` is mandatory for all new Go** (`coding-standards.go.md` +
`testing.go.md`), and **the module graph stays lean** (§3a: a pin is a decision; the dep is
added by the task that first needs it).

This document refines the broad plan's Phase 1 into numbered, acceptance-tested tasks and fixes
the **layout** the work lands into ([ADR 0010](../adr/0010-package-layout-domain-core-and-thin-root-facade.md),
realised by P1.0). It is authoritative for the *order and acceptance* of Phase-1 work; TDD §8
P1 points here. It supersedes the
[`2026-06-23 - 04 - Phase-1 entry`](../handoffs/2026-06-23%20-%2004%20-%20Phase-1%20entry.md)
handoff (which pointed into this phase before the detail existed).

> **Why a detail plan now.** Phase 1 is the first phase with *real* subsystem bodies and a hard
> external consumer (the bench). Two things must be settled before bodies land, or the work
> fights itself: the **package layout** (ADR 0010 — otherwise the provider/loop fight the
> import graph and every body has to be re-homed later) and the **precise Turn/Step control
> flow** (TDD §6 #6 — streaming + Approval interleave inside a Step). This doc makes those calls
> so each port is mechanical.

---

## 0. Phase-1 entry state (where the repo stands)

| Backlog | Deliverable | State |
|---|---|---|
| P0.1–P0.6 | Phase 0 — facade sketch, skeleton, detail plan + CI, `platform` seam, capstone harness | ✅ **complete** (HEAD `371061f` / `c6e9908`) |
| — | capstone path runs for real: construct→`Step`→`Snapshot`→`Resume`→`AddExperimental` over the internal `Responder` seam | ✅ 12 tests under `-race` |
| — | verify green: `gofmt -l .` · `go vet ./...` · `go build ./...` · `go test -race ./...` + 6-target cross-build | ✅ |

**The layout as inherited (the throwaway P0.6 shape ADR 0010 replaces):** the loop bodies live
in the **root** package (`loop.go`, `conversation.go`, `registry.go`); the provider seam
(`Responder` + root-type-free wire types) lives in `internal/agent`; `internal/platform`
imports root for the `Confiner` trio; off-capstone-path public methods in `apogee.go` are
`panic("sketch: not implemented")` stubs (~30 of them). [ADR 0010](../adr/0010-package-layout-domain-core-and-thin-root-facade.md)
resolves the target layout; **P1.0 realises it before any real body lands.**

---

## 1. Phase-1 deliverable & exit definition

Broad plan §4 Phase-1 deliverable, verbatim: *"a local model completes a simple file-edit task;
the bench drives, steps, snapshots, and scores it in-process via the library API."* Phase 1 is
**done** when all six hold:

> ✅ **All six hold (2026-06-23).** P1.0–P1.6 landed in the apogee repo (verify gate green);
> P1.7 landed in apogee-sim as `internal/coreagent` — it drives the real library through the
> public API against an ephemeral workspace, `Step`s a file-edit task to completion, observes
> Events as Go values, and scores the workspace, proven under `-race` by a hermetic
> OpenAI-compatible `httptest` model (the same code path a live model takes). The remaining
> runtime confirmation — driving an actual local model at `http://192.168.64.1:1111` (MCP
> control `http://192.168.64.1:7331/mcp`) — is a `RunConfig.Endpoint` swap run from the host;
> the build container does not route to that server. See §4 P1.7.

1. **Layout** is ADR-0010-shaped (P1.0): `internal/*` never imports root; the throwaway root
   loop is gone; verify green.
2. **Real provider** (P1.1): an OpenAI-compatible HTTP client implements the `Responder` port
   and reaches a live Upstream; an `httptest.Server` test exercises the wire path.
3. **Full Exchange** (P1.2): the loop runs stream → parse one tool-call format → post-response
   hooks → tool dispatch → Approval → post-tool-result → quiescent boundary, emitting typed
   Events; **pre-request hook mutations flow into the Upstream request** (P0.6 fires hooks but
   drops their mutations).
4. **Minimal tools** (P1.4): file read / write, directory-list, and a pure-Go grep execute
   behind the public registry — **no external programs required** for this slice (§3a).
5. **Sessions** (P1.6): snapshot/resume round-trips the concrete schema and restores loop
   counters (`turnIndex`), not just the message list (a documented P0.6 gap).
6. **Bench on the API** (P1.7): apogee-sim imports the module (`go.mod replace` → local path),
   constructs an `Agent` against isolated dirs, `Step`s it, and scores a file-edit task.

The public API stays **v0.x, no stability promise** (ADR 0001 §18); semver begins at the end of
Phase 3. The riskiest port (`processing/`, all formats + harmony/thinking channels) is **not**
finished here — Phase 1 parses **one** tool-call format end-to-end; the rest is Phase 3.

---

## 2. Dependency additions (pins already decided — phase-0 detail plan §1)

A pin is a decision; the dependency is added by the *task that first needs it*, so the graph
never carries a dep ahead of its use. The Phase-1 **library + bench core** needs the fewest:

| Module | Pin | Added by | Note |
|---|---|---|---|
| `github.com/oklog/ulid/v2` | `v2.1.1` | **P1.6** (if the schema uses ULIDs for session/turn IDs) | same pin apogee-sim carries |
| `gopkg.in/yaml.v3` | `v3.0.1` | **deferred** — file-based config is a CLI/TUI concern (likely Phase 2); the bench injects `Config` programmatically, so the core slice needs no YAML | |
| `github.com/spf13/cobra` | `v1.10.2` | **deferred** — the first real subcommand (optional `apogee headless`, else Phase-2 TUI); the library+bench deliverable needs no CLI | |

`net/http` (provider, web tools), `encoding/json`, `os/exec`, `io/fs`, `regexp`, `bufio` stay
the stdlib default (§3a). Net: the Phase-1 *core* may add only `ulid`. Each addition is
re-justified when its `go get` lands.

---

## 3. P1.0 — the layout refactor (ADR 0010; the prerequisite)

A **pure move**: relocate code to the ADR-0010 layout with **no behaviour change**. The
existing 12 tests are the regression net — they must still pass (adjusted only for import
paths / black-box vs white-box package). Land this **first**; everything else builds on it.

**Target tree (the parts P1.0 touches):**

```
internal/domain/     # the public types/interfaces/enums/errors + their pure logic
                     #   (CONTEXT.md as Go) — depends only on stdlib
internal/agent/      # the engine: Agent + New/Resume + Step/Run/Submit + loop + conversation
                     #   state; imports internal/domain (+ ports). Replaces the doc.go stub.
internal/provider/   # the Responder port (moved from internal/agent) + later the HTTP client
internal/platform/   # imports internal/domain (NOT root) for the Confiner trio
apogee.go            # thin facade: type aliases + re-exported consts/errors + forwarders
```

**Sub-tasks**

- **P1.0a — stand up `internal/domain`.** Move every public *type / interface / enum / sentinel
  error / hook working-value* out of `apogee.go` into `internal/domain` (the list is ADR 0010
  §Decision-1). Move the pure logic with its type: ordering-cycle detection (`registry.go`),
  `ConfinementCaps.AutoEligible`, conversation (de)serialization (`conversation.go`). `Confiner`
  + `ConfinementCaps` + `ConfinementBox` land here too (resolves §6.1).
- **P1.0b — the engine in `internal/agent`.** Move `Agent` + its methods (`Step` / `Run` /
  `Submit` / `Snapshot` / `Close` / `Mode`) and `New` / `Resume` here, plus the loop body
  (`loop.go` → `internal/agent`). `internal/agent` imports `internal/domain`. (`Run`'s real body
  is P1.2; here it keeps its current stub semantics, just re-homed.)
- **P1.0c — the provider seam to `internal/provider`.** Move `internal/agent/responder.go`
  (`Responder` + wire `Request` / `RawResponse` / `Message`) into `internal/provider`. The
  loop's `buildUpstreamRequest` translation moves with the engine and now targets
  `provider.Request`. The `placeholderResponder` moves to `internal/provider` (or stays beside
  the engine) — it errors until P1.1.
- **P1.0d — `internal/platform` imports `internal/domain`.** Swap
  `"github.com/airiclenz/apogee"` → `internal/domain` in `platform.go` / `platform_test.go`;
  `NewDenyConfiner()` returns `domain.Confiner`. **Removes the last wrong-way edge.**
- **P1.0e — the thin root facade + completeness guard.** `apogee.go` becomes aliases
  (`type Tool = domain.Tool`, …), re-exported consts/errors, and forwarders
  (`func New(cfg Config) (*Agent, error) { return agent.New(cfg) }`, `Resume`, `DecodeSession`,
  `NewToolRegistry`, `NewMechanismRegistry`). Add `example_test.go` (package `apogee_test`)
  naming the full public surface, so a forgotten alias fails the build.

**Acceptance (P1.0):**
- `go test -race ./...` — the 12 P0.6 tests pass unchanged in behaviour (import paths / package
  scope adjusted only as the move requires).
- `grep -rl '"github.com/airiclenz/apogee"' internal/` is **empty** — the invariant holds at the
  source level (no `internal/*` imports root).
- `gofmt -l .` empty; `go vet ./...` clean; 6-target cross-build green; `apogee --help` exit 0.
- The public surface is unchanged for an embedder: `apogee.New`, `apogee.Tool`,
  `apogee.ErrAutoUnavailable`, etc. all resolve (the `example_test` compiles).

---

## 4. Phase-1 task list (after P1.0)

IDs continue the `P1.x` scheme. **P1.0 blocks all.** Then the core subsystems fan out, and
**P1.2 is the convergence point** (it integrates provider + processing + tools + hook-mutation);
**P1.7 is last** (it needs the slice working end-to-end).

| ID | Task | Depends | New deps | Resolves |
|---|---|---|---|---|
| **P1.0** | Layout refactor (§3) | — | none | ADR 0010 / TDD §6 #7, §6.1 |
| **P1.1** | Real OpenAI-compatible provider (HTTP client, model discovery, ret/timeouts, server-process mgr) implementing `Responder` | P1.0 | none (`net/http`) | TDD §5 Provider / §8 #5 |
| **P1.2** | Full Turn/Step state machine (stream → parse → hooks → dispatch → Approval → boundary) | P1.0, P1.1, P1.3, P1.4, P1.5 | none | TDD §8 #4 / §6 #3, #6 |
| **P1.3** | `processing/` — parse **one** tool-call format end-to-end (TS oracle + ported vectors) | P1.0 | none | TDD §8 #6 (partial) |
| **P1.4** | Minimal tool set + real registry/executor (file read/write, dir-list, pure-Go grep) | P1.0 | none | TDD §5 Tools (partial) |
| **P1.5** | Hook-mutation API real bodies (`Request`/`Response`/`Conversation`) — wire pre-request mutations into the Upstream request | P1.0 | none | TDD §6.2 |
| **P1.6** ✅ | Concrete Session schema + versioning; restore `turnIndex` | P1.0 | none (`ulid` not needed — int counters) | TDD §8 #7 |
| **P1.7** ✅ | Point apogee-sim at the Go API (`go.mod replace`, construct/Step/score a file-edit task) | P1.2, P1.4, P1.6 | none (in the bench repo) | plan §4 Phase-1 deliverable |

### P1.1 — the real provider
`internal/provider`: an OpenAI-compatible chat-completions client implementing
`provider.Responder`, **non-streaming first**, then a streaming variant (token deltas). Model
discovery (`/v1/models`), bounded retries + timeouts, and a server-process manager (detect /
optionally launch the Upstream). The TS `openai-compatible-provider` / `model-discovery` /
`server-process-manager` are the **oracle** (port behaviour, not lines). Upgrade the capstone
harness with an `httptest.Server` so the wire path is exercised hermetically (no live Upstream
in CI). Wire types stay provider-local (P1.0c); the loop translates at the seam.
**Acceptance:** a table-driven `httptest` test drives request-shape + response-parse + a
retryable 5xx + a timeout; non-streaming and streaming both round-trip; zero new module deps.

### P1.2 — the Turn/Step state machine (the convergence)
Replace the P0.6 single-Turn slice with the real loop: build request (pre-request hooks +
their mutations, P1.5) → call Upstream (stream, P1.1) → parse (P1.3) → post-response hooks →
dispatch tools (P1.4) through Approval (Ask-Before) → post-tool-result hooks → continue or end
the Exchange — **every `Step()` returning at a quiescent boundary** (ADR 0007). Implement `Run`
(the Step-until-Exchange-complete wrapper, currently a stub). **Settle the two open control-flow
calls here** (§5): streaming + Approval interleave (§6 #6) and Event delivery / backpressure
(§6 #3). Keep recover-at-extension-boundary and ctx-cancellation (already proven by P0.6) intact
across the richer flow.
**Acceptance:** a fake `Responder` + a fake `Tool` drive a multi-Turn Exchange (model asks for
a tool, tool runs, model finishes) under `-race`; Approval is consulted in Ask-Before and
bypassed in Plan; cancellation mid-stream and mid-tool both yield `StatusCancelled` + a
resumable snapshot; a panicking tool yields an `ErrorEvent` and the loop survives.

### P1.3 — `processing/`, one format end-to-end
Port just enough of `processing/` to parse **one** tool-call format (the most common
native/JSON tool-call shape) from a (possibly streamed) assistant message into
`domain.ToolCall`s, plus thinking-channel stripping if the chosen format needs it. **TS as
oracle + ported test vectors are the gate** (plan principle 2 / §6 #24b) — extract golden
vectors from the TS parser and assert Go parity. All other formats + harmony channels are
**Phase 3**.
**Acceptance:** golden-file parity tests (ported TS vectors) for the one format pass; a
malformed call degrades to a parse-error path, not a panic.

### P1.4 — minimal tools + real registry
Real `ToolRegistry` (`NewToolRegistry` / `Register` / `Subset` — currently panic stubs) and a
tool executor wired into P1.2's dispatch. Tools: `read_file`, `write_file`, `list_dir`,
`grep` (**pure-Go** `fs.WalkDir` + `regexp`, ripgrep optional later — §3a). All honour
`ctx`, are stateless across Turns (ADR 0008), and surface a panic as an `ErrorEvent`.
**Acceptance:** each tool has a table test over a `t.TempDir()`; `Subset` narrows correctly;
`Register` rejects a duplicate name; a write goes through Approval in Ask-Before.

### P1.5 — hook-mutation API real bodies
Implement the `Request` / `Response` / `Conversation` / `LoopView` / `ConversationView` bodies
designed in [`../design/hook-mutation-api.md`](../design/hook-mutation-api.md) — the accessors
and the mutators (`AppendToSystem`, `InjectContext`, `SetTools`, `SetText`,
`SetToolCallArguments`, `RewriteHistory`, …). **Wire pre-request hook mutations into the
outgoing Upstream request** — the documented P0.6 gap (hooks fire but their mutations are
dropped). This is the biggest *public* surface gap (TDD §6.2).
**Acceptance:** a pre-request hook that `AppendToSystem`/`InjectContext` provably changes the
bytes the (fake) provider receives; a post-response `ActionDefer` survives a snapshot/resume
boundary and injects on the next request.

### P1.6 — concrete Session schema + versioning
Replace the throwaway `conversation{Messages}` with the real serialized state: full messages
(roles, tool calls, tool-call IDs, preserved `Extra` wire fields — load-bearing for resume),
deferred Response Actions, and **loop counters (`turnIndex`)** so resume continues rather than
re-runs. Keep `Session.Version` versioning + future-version rejection; document the v1 schema.
**Acceptance:** snapshot → resume → `Step` continues at the correct `turnIndex`; a round-trip
preserves tool-call/result pairing and `Extra` fields; an unknown future `Version` →
`ErrSessionVersion`.

### P1.7 — point apogee-sim at the Go API
In the **apogee-sim** repo (not here): `go.mod replace github.com/airiclenz/apogee => ../apogee`;
construct an `Agent` against ephemeral `LibraryDir` / `SessionsDir` (isolation — ADR 0001);
`Submit` a file-edit task; `Step` to completion; score the workspace. This re-arms the eval
loop on the real library for the rest of the build.
**Acceptance:** a local model completes a simple file-edit task driven entirely through the
public API; the bench observes Events as Go values and scores the result.
**✅ Done (apogee-sim `internal/coreagent`):** `Run(ctx, RunConfig)` constructs an
`apogee.Agent` (Ask-Before, auto-approving writes) against an ephemeral `WorkspaceDir`,
`Submit`s the task, `Step`s to the quiescent boundary until the Exchange completes, records
every `apogee.Event`, and reads the workspace back; `ScoreFileEdit` judges the target file.
The acceptance is proven under `-race` by a hermetic OpenAI-compatible `httptest` model that
drives the **real** provider client (the scripted model asks for `write_file`; the loop
dispatches it through Approval; the file lands in the sandbox; the run scores a pass) — the
same code path a live model takes. The **live-model run** against `http://192.168.64.1:1111`
(MCP control `http://192.168.64.1:7331/mcp`) is a `RunConfig.Endpoint` swap, run from the host
(the build container does not route to the server). The bench was not committed (apogee-sim
carries unrelated WIP); the apogee library it consumes is committed.

---

## 5. Open design calls to resolve *within* Phase 1

These are sub-decisions the bodies above force; record each as a short note / ADR amendment when
it lands (don't pre-decide in the abstract):

- **§6 #6 — streaming + Approval interleave inside a Step** (settled by **P1.2**): the control
  flow when a tool-call arrives mid-stream and Approval must be consulted synchronously; what
  the `EventSink` sees around the blocking `Approver` call.
- **§6 #3 — Event delivery & backpressure** (settled by **P1.2**): `EventSink.Emit` must not
  block the loop; define buffering / drop policy / sub-agent fan-in; a channel adapter for the
  Phase-2 TUI.
- **§6 #5 — `UserInput` / `FileRefs` → budgeted context** (touched by **P1.2/P1.4**): how file
  references become context. **Minimal in Phase 1** (resolve refs to file contents under a
  trivial budget); the full Budget allocation + generative Compaction algorithms (TDD §8 #8)
  are **not** Phase-1 deliverables — a simple, structural budget suffices for the slice and the
  full design is later.

---

## 6. Out of scope for Phase 1 (explicit non-goals — keep the slice vertical, not wide)

- **All tool-call formats + harmony/thinking channels** — Phase 3 finishes `processing/`; P1.3
  does **one** format.
- **Mechanism catalogue, descriptors, full deterministic topo-order, self-regulation** —
  Phase 4 (after the sim-trace catalogue-mapping session). Phase 1 keeps the cycle-check-only
  registry and the experimental-hook slots.
- **Confiner backends** (seatbelt / landlock / AppContainer) — Phase 3 + the Confinement design
  session. P1 keeps the deny-all stub; the Auto gate stays testable.
- **TUI** — Phase 2 (a consumer of the Phase-1 Events). **Agent modes beyond what the loop
  needs** (full Plan/Ask-Before/Auto wiring is Phase 3) and **sub-agents** — Phase 3.
- **Context reducers** — Budget allocation algorithm, generative Compaction trigger/strategy,
  tool-result capping, token counting (TDD §8 #8) — designed later; P1 uses a trivial budget.
- **MCP / web / network tools** — Phase 3; the `ExternalEffects` seam exists but P1's tool set
  is local-only.

---

## 7. Acceptance-criteria summary (quick gate)

A reviewer can check Phase 1 with:

```
gofmt -l .                          # empty
go vet ./...                        # clean
go build ./...                      # ok
go test -race ./...                 # provider httptest + loop + tools + hook-mutation + session
grep -rl '"github.com/airiclenz/apogee"' internal/   # empty (ADR-0010 invariant)
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build ./...   # + the other 5 cross targets
./apogee --help                     # prints usage, exit 0
```

…plus the **deliverable**: apogee-sim (with a `replace` directive) drives a local model through
a file-edit task in-process via the public API and scores it (P1.7).

---

## 8. Suggested skills

- **`/coding-standards`** — **mandatory** for every Go task here (`coding-standards.go.md` +
  `testing.go.md`); load before writing each body.
- **`run` / `verify`** — once P1.1/P1.2 land, to drive the slice + the `httptest` wire path and
  confirm a real Exchange.
- **`project-research`** — optional escalation only if a specific TS `processing/` behaviour
  proves ambiguous during P1.3 (plan §6 #24b: the TS-vector parity *is* the gate; no upfront
  research ceremony required).
- **`/handoff`** — at session end; **`archive-handoffs`** to retire the consumed Phase-1-entry
  handoff once P1.0 lands.
</content>
